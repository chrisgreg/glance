// Package api exposes the Glance HTTP API: the public collect endpoint and
// snippet, and the admin API behind the login.
package api

import (
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chrisgreg/glance/server/internal/auth"
	"github.com/chrisgreg/glance/server/internal/enrich"
	"github.com/chrisgreg/glance/server/internal/events"
	"github.com/chrisgreg/glance/server/internal/favicons"
	"github.com/chrisgreg/glance/server/internal/mcp"
	"github.com/chrisgreg/glance/server/internal/rollup"
	"github.com/chrisgreg/glance/server/internal/settings"
	"github.com/chrisgreg/glance/server/internal/sites"
	"github.com/chrisgreg/glance/server/internal/stats"
	"github.com/chrisgreg/glance/server/internal/tokens"
)

// Version is the server version, overridden at build time via -ldflags.
var Version = "0.1.0"

//go:embed glance.js
var snippet []byte

var snippetETag = func() string {
	sum := sha256.Sum256(snippet)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}()

// Server holds every dependency the handlers need.
type Server struct {
	DB       *sql.DB
	Log      *slog.Logger
	Sites    *sites.Store
	Settings *settings.Store
	Writer   *events.Writer
	Stats    *stats.Store
	Favicons *favicons.Fetcher
	Admin    *auth.Admin
	Web      http.Handler
	Now      func() time.Time
	// TrustProxy reads the client IP from X-Forwarded-For (set by Traefik).
	TrustProxy bool
	// MCPToken grants read-only access to /mcp when set.
	MCPToken string
	Tokens   *tokens.Store
	// Retention defaults and whether the environment pins them.
	RetentionDays    int
	RetentionFromEnv bool
	StartedAt        time.Time
	DatabasePath     string
}

// Handler builds the router.
func (s *Server) Handler() http.Handler {
	if s.Now == nil {
		s.Now = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)

	// Public.
	mux.HandleFunc("GET /glance.js", s.script)
	mux.HandleFunc("GET /api/v1/theme", s.theme)
	mux.HandleFunc("POST /api/v1/collect", s.collect)
	mux.HandleFunc("OPTIONS /api/v1/collect", s.collectOptions)

	// Admin session.
	mux.HandleFunc("GET /api/v1/auth/me", s.authMe)
	mux.HandleFunc("POST /api/v1/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)

	// Admin.
	mux.Handle("GET /api/v1/sites", s.adminAuth(s.listSites))
	mux.Handle("POST /api/v1/sites", s.adminAuth(s.createSite))
	mux.Handle("POST /api/v1/sites/reorder", s.adminAuth(s.reorderSites))
	mux.Handle("GET /api/v1/sites/{id}", s.adminAuth(s.getSite))
	mux.Handle("PATCH /api/v1/sites/{id}", s.adminAuth(s.updateSite))
	mux.Handle("DELETE /api/v1/sites/{id}", s.adminAuth(s.deleteSite))
	mux.Handle("GET /api/v1/sites/{id}/stats", s.adminAuth(s.siteStats))
	mux.Handle("GET /api/v1/sites/{id}/breakdown", s.adminAuth(s.siteBreakdown))
	mux.Handle("GET /api/v1/sites/{id}/live", s.adminAuth(s.siteLive))
	mux.Handle("POST /api/v1/sites/{id}/refresh-favicon", s.adminAuth(s.refreshFavicon))
	mux.Handle("GET /api/v1/sites/{id}/favicon", s.adminAuth(s.siteFavicon))
	mux.Handle("GET /api/v1/favicon", s.adminAuth(s.refFavicon))
	mux.Handle("GET /api/v1/status", s.adminAuth(s.status))
	mux.Handle("GET /api/v1/settings", s.adminAuth(s.getSettings))
	mux.Handle("PATCH /api/v1/settings", s.adminAuth(s.updateSettings))
	mux.Handle("GET /api/v1/tokens", s.adminAuth(s.listTokens))
	mux.Handle("POST /api/v1/tokens", s.adminAuth(s.createToken))
	mux.Handle("DELETE /api/v1/tokens/{id}", s.adminAuth(s.deleteToken))
	mux.Handle("POST /api/v1/rollup", s.adminAuth(s.rollupNow))
	mux.Handle("GET /api/v1/export", s.adminAuth(s.export))

	// MCP (read-only) for AI agents: Streamable HTTP at /mcp.
	mux.Handle("/mcp", s.mcpAuth(mcp.Handler(mcp.NewServer(mcp.Stores{Sites: s.Sites, Stats: s.Stats, Now: s.Now}, Version), s.Log)))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	if s.Web != nil {
		mux.Handle("/", s.Web)
	}
	return s.logging(mux)
}

// ---- middleware ----

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/collect" {
			s.Log.Debug("http.request", "method", r.Method, "path", r.URL.Path, "status", rw.status, "ms", time.Since(start).Milliseconds())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) adminAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok := bearer(r); tok != "" && s.Tokens != nil {
			if _, ok := s.Tokens.Authenticate(r.Context(), tok); ok {
				if r.Method != http.MethodGet {
					writeError(w, http.StatusForbidden, "read_only", "API tokens are read-only")
					return
				}
				next(w, r)
				return
			}
		}
		if !s.Admin.Authorized(r) {
			writeError(w, http.StatusUnauthorized, "login_required", "sign in to Glance")
			return
		}
		next(w, r)
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// mcpAuth accepts the configured GLANCE_MCP_TOKEN as a bearer token, or an
// admin session / HTTP Basic login (or nothing when admin auth is off).
func (s *Server) mcpAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g, err := s.Settings.General(r.Context(), s.RetentionDays, s.RetentionFromEnv); err == nil && !g.MCPEnabled {
			writeError(w, http.StatusNotFound, "mcp_disabled", "the MCP endpoint is turned off in Settings")
			return
		}
		if tok := bearer(r); tok != "" {
			if s.MCPToken != "" && auth.Equal(auth.Hash(tok), auth.Hash(s.MCPToken)) {
				next.ServeHTTP(w, r)
				return
			}
			if s.Tokens != nil {
				if _, ok := s.Tokens.Authenticate(r.Context(), tok); ok {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid MCP token")
			return
		}
		if !s.Admin.Authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "an MCP token (GLANCE_MCP_TOKEN) or admin login is required: Authorization: Bearer ...")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- public: snippet and collect ----

func (s *Server) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", snippetETag)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Header.Get("If-None-Match") == snippetETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(snippet)
}

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

func (s *Server) collectOptions(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.WriteHeader(http.StatusNoContent)
}

type collectBody struct {
	Site     string          `json:"s"`
	Name     string          `json:"n"`
	URL      string          `json:"u"`
	Referrer string          `json:"r"`
	Width    int             `json:"w"`
	TZ       string          `json:"tz"`
	Props    json.RawMessage `json:"x"`
}

// collect accepts one event. It always answers 202 so the snippet cannot
// be used to probe which site ids exist; invalid events are dropped.
func (s *Server) collect(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Cache-Control", "no-store")
	var in collectBody
	b, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil || json.Unmarshal(b, &in) != nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	site, ok := s.Sites.Lookup(r.Context(), strings.TrimSpace(in.Site))
	if !ok {
		s.Log.Debug("collect.dropped", "reason", "unknown site", "site", in.Site)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	path, host := enrich.Path(in.URL)
	if !enrich.LocalHost(host) && !enrich.SameSite(host, site.Domain) {
		s.Log.Debug("collect.dropped", "reason", "host does not match site domain", "host", host, "domain", site.Domain)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	ua := enrich.ParseUA(r.UserAgent(), in.Width)
	if ua.Bot {
		s.Log.Debug("collect.dropped", "reason", "bot user agent", "ua", r.UserAgent())
		w.WriteHeader(http.StatusAccepted)
		return
	}
	now := s.Now()
	salt, err := s.Settings.Salt(r.Context(), now)
	if err != nil {
		s.Log.Error("collect.salt_failed", "error", err.Error())
		w.WriteHeader(http.StatusAccepted)
		return
	}
	utmSrc, utmCamp := enrich.UTM(in.URL)
	ev := events.Event{
		SiteID: site.ID, At: now, Kind: events.KindPageview, Path: path,
		RefHost: enrich.Referrer(in.Referrer, site.Domain), Country: enrich.Country(r.Header, in.TZ), Region: enrich.Region(in.TZ),
		Device: ua.Device, Browser: ua.Browser, OS: ua.OS, UTMSrc: utmSrc, UTMCamp: utmCamp,
		Visitor: events.VisitorHash(salt, site.ID, s.clientIP(r), r.UserAgent()),
	}
	if name := strings.TrimSpace(in.Name); name != "" && name != "pageview" {
		if len(name) > 60 {
			name = name[:60]
		}
		ev.Kind, ev.Name = events.KindEvent, name
	}
	s.Writer.Enqueue(ev)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) clientIP(r *http.Request) string {
	if s.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i > 0 {
				xff = xff[:i]
			}
			return strings.TrimSpace(xff)
		}
		if rip := r.Header.Get("X-Real-IP"); rip != "" {
			return strings.TrimSpace(rip)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- auth ----

func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"auth_required": s.Admin.Enabled(), "authenticated": s.Admin.Authorized(r)})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Admin.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"auth_required": false, "authenticated": true})
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	tok, ok := s.Admin.Login(r.Context(), in.Username, in.Password)
	if !ok {
		time.Sleep(400 * time.Millisecond)
		s.Log.Warn("auth.login_failed", "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "bad_credentials", "wrong username or password")
		return
	}
	s.Admin.SetCookie(w, r, tok)
	writeJSON(w, http.StatusOK, map[string]any{"auth_required": true, "authenticated": true})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		s.Admin.Logout(r.Context(), c.Value)
	}
	s.Admin.ClearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// ---- sites (admin) ----

type siteView struct {
	sites.Site
	Card stats.SiteCard `json:"card"`
	Live int            `json:"live"`
}

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	list, err := s.Sites.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	now := s.Now()
	out := make([]siteView, 0, len(list))
	for _, st := range list {
		card, err := s.Stats.Card(r.Context(), st.ID, now)
		if err != nil {
			s.fail(w, err)
			return
		}
		live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, now)
		out = append(out, siteView{Site: st, Card: card, Live: live})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": out})
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request) {
	var in sites.Input
	if !readJSON(w, r, &in) {
		return
	}
	st, err := s.Sites.Create(r.Context(), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("site.created", "id", st.ID, "domain", st.Domain)
	go s.fetchSiteFavicon(st)
	writeJSON(w, http.StatusCreated, siteView{Site: st})
}

// fetchSiteFavicon runs in the background after create or on request.
func (s *Server) fetchSiteFavicon(st sites.Site) {
	ctx, cancel := bgContext()
	defer cancel()
	data, ctype, err := s.Favicons.ForDomain(ctx, st.Domain)
	if err != nil {
		s.Log.Debug("favicon.not_found", "domain", st.Domain, "error", err.Error())
		_ = s.Sites.SetFavicon(ctx, st.ID, nil, "")
		return
	}
	if err := s.Sites.SetFavicon(ctx, st.ID, data, ctype); err != nil {
		s.Log.Error("favicon.store_failed", "site", st.ID, "error", err.Error())
	}
}

func (s *Server) getSite(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	card, err := s.Stats.Card(r.Context(), st.ID, s.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, s.Now())
	writeJSON(w, http.StatusOK, siteView{Site: st, Card: card, Live: live})
}

func (s *Server) updateSite(w http.ResponseWriter, r *http.Request) {
	var in sites.Input
	if !readJSON(w, r, &in) {
		return
	}
	st, err := s.Sites.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	if in.Domain != nil {
		go s.fetchSiteFavicon(st)
	}
	writeJSON(w, http.StatusOK, siteView{Site: st})
}

func (s *Server) deleteSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Sites.Delete(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("site.deleted", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderSites(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Sites.Reorder(r.Context(), in.IDs); err != nil {
		s.fail(w, err)
		return
	}
	s.listSites(w, r)
}

func (s *Server) siteStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "7d"
	}
	if !stats.ValidRange(rng) {
		writeError(w, http.StatusBadRequest, "invalid", "range must be one of "+strings.Join(stats.Ranges, ", "))
		return
	}
	sum, err := s.Stats.Summary(r.Context(), st.ID, rng, s.Now(), 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, s.Now())
	writeJSON(w, http.StatusOK, map[string]any{"site": st, "live": live, "stats": sum})
}

func (s *Server) siteLive(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	live, err := s.Stats.LiveSnapshot(r.Context(), st.ID, s.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, live)
}

func (s *Server) siteBreakdown(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	q := r.URL.Query()
	rng := q.Get("range")
	if rng == "" {
		rng = "7d"
	}
	dim := q.Get("dim")
	if !stats.ValidRange(rng) || !stats.ValidDim(dim) {
		writeError(w, http.StatusBadRequest, "invalid", "dim must be one of "+strings.Join(stats.Dims, ", ")+" and range one of "+strings.Join(stats.Ranges, ", "))
		return
	}
	limit := 500
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	rows, err := s.Stats.Breakdown(r.Context(), st.ID, dim, rng, s.Now(), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dim": dim, "range": rng, "rows": rows})
}

func (s *Server) refreshFavicon(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.fetchSiteFavicon(st)
	st, _ = s.Sites.Get(r.Context(), st.ID)
	writeJSON(w, http.StatusOK, siteView{Site: st})
}

func (s *Server) siteFavicon(w http.ResponseWriter, r *http.Request) {
	data, ctype, err := s.Sites.Favicon(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	serveIcon(w, r, data, ctype)
}

func (s *Server) refFavicon(w http.ResponseWriter, r *http.Request) {
	data, ctype, err := s.Favicons.Cached(r.Context(), r.URL.Query().Get("host"))
	if err != nil {
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	serveIcon(w, r, data, ctype)
}

func serveIcon(w http.ResponseWriter, r *http.Request, data []byte, ctype string) {
	if len(data) == 0 {
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=604800")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

// ---- settings, tokens, theme ----

func (s *Server) theme(w http.ResponseWriter, r *http.Request) {
	g, err := s.Settings.General(r.Context(), s.RetentionDays, s.RetentionFromEnv)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, map[string]string{"accent": g.Accent, "title": g.Title})
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	g, err := s.Settings.General(r.Context(), s.RetentionDays, s.RetentionFromEnv)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var p settings.Patch
	if !readJSON(w, r, &p) {
		return
	}
	g, err := s.Settings.Apply(r.Context(), p, s.RetentionDays, s.RetentionFromEnv)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.Tokens.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": list, "env_token_set": s.MCPToken != ""})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	t, raw, err := s.Tokens.Create(r.Context(), in.Name)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("token.created", "id", t.ID, "name", t.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": raw})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	if err := s.Tokens.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- status & export ----

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	var sitesN, eventsN, dailyN int
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sites`).Scan(&sitesN)
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM events`).Scan(&eventsN)
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM daily_stats`).Scan(&dailyN)
	var dbBytes int64
	if s.DatabasePath != "" {
		if fi, err := os.Stat(s.DatabasePath); err == nil {
			dbBytes = fi.Size()
		}
	}
	uptime := int64(0)
	if !s.StartedAt.IsZero() {
		uptime = int64(time.Since(s.StartedAt).Seconds())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "sites": sitesN, "raw_events": eventsN, "daily_rows": dailyN, "db_bytes": dbBytes, "uptime_seconds": uptime,
		"written": s.Writer.Written.Load(), "dropped": s.Writer.Dropped(), "admin_auth": s.Admin.Enabled(), "env_token_set": s.MCPToken != "",
	})
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	list, err := s.Sites.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT site_id, day, dim, key, pageviews, visitors FROM daily_stats ORDER BY site_id, day, dim, key`)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	type dayRow struct {
		SiteID    string `json:"site_id"`
		Day       string `json:"day"`
		Dim       string `json:"dim"`
		Key       string `json:"key"`
		Pageviews int    `json:"pageviews"`
		Visitors  int    `json:"visitors"`
	}
	daily := []dayRow{}
	for rows.Next() {
		var d dayRow
		if err := rows.Scan(&d.SiteID, &d.Day, &d.Dim, &d.Key, &d.Pageviews, &d.Visitors); err != nil {
			s.fail(w, err)
			return
		}
		daily = append(daily, d)
	}
	now := s.Now().UTC()
	w.Header().Set("Content-Disposition", `attachment; filename="glance-export-`+now.Format("20060102")+`.json"`)
	writeJSON(w, http.StatusOK, map[string]any{"version": Version, "exported_at": now.Format(time.RFC3339), "sites": list, "daily_stats": daily})
}

// rollupNow flushes the queue and rebuilds today's and yesterday's rollups
// so the dashboard reflects the last few minutes immediately.
func (s *Server) rollupNow(w http.ResponseWriter, r *http.Request) {
	if err := s.Writer.Flush(); err != nil {
		s.fail(w, err)
		return
	}
	if err := rollup.Run(r.Context(), s.DB, s.Log, s.Now()); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body must be 1 MB or smaller")
		return false
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a JSON object")
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sites.ErrNotFound), errors.Is(err, tokens.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, sites.ErrInvalid), errors.Is(err, tokens.ErrInvalid), errors.Is(err, settings.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
	default:
		s.Log.Error("http.error", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "database": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": Version, "queued_drops": strconv.FormatInt(s.Writer.Dropped(), 10)})
}
