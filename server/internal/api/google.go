package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chrisgreg/glance/server/internal/stats"
)

const googleCallbackPath = "/api/v1/google/callback"

// googleRedirectURI is the exact redirect URI registered with Google,
// rebuilt from the request so it follows whatever host the proxy serves.
func (s *Server) googleRedirectURI(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.TrustProxy {
		if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
			scheme = strings.ToLower(strings.TrimSpace(strings.Split(p, ",")[0]))
		}
	}
	return scheme + "://" + r.Host + googleCallbackPath
}

func (s *Server) googleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	status, err := s.Google.Status(r.Context(), st.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "redirect_uri": s.googleRedirectURI(r)})
}

// googleConnect is the auth boundary: only a logged-in admin can mint a
// state, and the browser is then sent to Google.
func (s *Server) googleConnect(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if !s.Google.Configured() {
		writeError(w, http.StatusConflict, "not_configured", "set GLANCE_GOOGLE_CLIENT_ID and GLANCE_GOOGLE_CLIENT_SECRET to connect Google")
		return
	}
	// A GET, so adminAuth lets read-only API tokens through; connecting a
	// site to a Google account is a write.
	if bearer(r) != "" {
		writeError(w, http.StatusForbidden, "read_only", "API tokens are read-only")
		return
	}
	http.Redirect(w, r, s.Google.BeginConnect(st.ID, s.googleRedirectURI(r)), http.StatusFound)
}

// googleCallback is reached by a top-level redirect from Google. It is
// deliberately outside adminAuth; the one-shot state proves the request
// began from googleConnect.
func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state, code := q.Get("state"), q.Get("code")
	target, domain := "/", ""
	if siteID, ok := s.Google.PeekState(state); ok {
		target = "/s/" + url.PathEscape(siteID)
		if st, err := s.Sites.Get(r.Context(), siteID); err == nil {
			domain = st.Domain
		}
	}
	if e := q.Get("error"); e != "" {
		http.Redirect(w, r, target+"?google_error="+url.QueryEscape(e), http.StatusFound)
		return
	}
	siteID, err := s.Google.CompleteConnect(r.Context(), state, code, s.googleRedirectURI(r), domain)
	if err != nil {
		target := "/"
		if siteID != "" {
			target = "/s/" + url.PathEscape(siteID)
		}
		s.Log.Warn("google.connect_failed", "error", err.Error())
		http.Redirect(w, r, target+"?google_error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/s/"+url.PathEscape(siteID)+"?google=connected", http.StatusFound)
}

func (s *Server) googleSetProperty(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	var in struct {
		Property string `json:"property"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Property = strings.TrimSpace(in.Property)
	if in.Property == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "property is required")
		return
	}
	if err := s.Google.SetProperty(r.Context(), st.ID, in.Property); err != nil {
		s.fail(w, err)
		return
	}
	s.googleStatus(w, r)
}

func (s *Server) googleDisconnect(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.Google.Disconnect(r.Context(), st.ID); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) googleSync(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.Google.Sync(r.Context(), st.ID); err != nil {
		s.fail(w, err)
		return
	}
	s.googleStatus(w, r)
}

// searchTerms lists the queries Google sent to a site over a range.
// Search Console trails real time by a few days, so the window is the same
// calendar days the other breakdowns use; short ranges may be empty.
func (s *Server) searchTerms(w http.ResponseWriter, r *http.Request) {
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
	if !stats.ValidRange(rng) {
		writeError(w, http.StatusBadRequest, "invalid", "range must be one of "+strings.Join(stats.Ranges, ", "))
		return
	}
	limit := 500
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	from, to, _ := stats.Window(rng, s.Now())
	rows, err := s.Google.Store.Terms(r.Context(), st.ID, from.Format("2006-01-02"), to.Add(-time.Second).Format("2006-01-02"), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rng, "rows": rows})
}
