package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/chrisgreg/glance/server/internal/polar"
	"github.com/chrisgreg/glance/server/internal/stats"
)

func (s *Server) polarWebhookURL(r *http.Request, siteID string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.TrustProxy {
		if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
			scheme = strings.ToLower(strings.TrimSpace(strings.Split(p, ",")[0]))
		}
	}
	return scheme + "://" + r.Host + "/api/v1/polar/webhook/" + siteID
}

type polarStatus struct {
	Connected  bool              `json:"connected"`
	Connection *polar.Connection `json:"connection,omitempty"`
	WebhookURL string            `json:"webhook_url"`
	Orders     int               `json:"orders"`
}

func (s *Server) writePolarStatus(w http.ResponseWriter, r *http.Request, siteID string) {
	st := polarStatus{WebhookURL: s.polarWebhookURL(r, siteID)}
	c, err := s.Polar.Store.Get(r.Context(), siteID)
	switch {
	case errors.Is(err, polar.ErrNotConnected):
	case err != nil:
		s.fail(w, err)
		return
	default:
		st.Connected = true
		st.Connection = &c
		_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM polar_orders WHERE site_id = ?`, siteID).Scan(&st.Orders)
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) polarGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writePolarStatus(w, r, st.ID)
}

// polarConnect saves the token and settings. A PUT with a body, so
// read-only tokens are already refused by adminAuth.
func (s *Server) polarConnect(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	var in polar.Input
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := s.Polar.Connect(r.Context(), st.ID, in); err != nil {
		s.fail(w, err)
		return
	}
	s.writePolarStatus(w, r, st.ID)
}

func (s *Server) polarDisconnect(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.Polar.Store.Delete(r.Context(), st.ID); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) polarSync(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.Polar.Sync(r.Context(), st.ID, st.Domain); err != nil {
		s.fail(w, err)
		return
	}
	s.writePolarStatus(w, r, st.ID)
}

// polarWebhook receives Polar's events. Public by necessity; the signature
// against the site's saved secret is the authentication.
func (s *Server) polarWebhook(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	c, err := s.Polar.Store.Get(r.Context(), st.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body must be 1 MB or smaller")
		return
	}
	if err := polar.VerifyWebhook(c.WebhookSecret, r.Header.Get("webhook-id"), r.Header.Get("webhook-timestamp"), r.Header.Get("webhook-signature"), body, s.Now()); err != nil {
		s.Log.Warn("polar.webhook_rejected", "site", st.ID, "error", err.Error())
		writeError(w, http.StatusUnauthorized, "signature", err.Error())
		return
	}
	kind, err := s.Polar.HandleWebhook(r.Context(), c, st.Domain, body)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("polar.webhook", "site", st.ID, "type", kind)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "type": kind})
}

// Revenue is the revenue view for a range.
type Revenue struct {
	Range      string                 `json:"range"`
	Currency   string                 `json:"currency"`
	Totals     polar.Totals           `json:"totals"`
	Previous   polar.Totals           `json:"previous"`
	Series     []polar.Point          `json:"series"`
	Breakdowns map[string][]polar.Row `json:"breakdowns"`
}

func (s *Server) siteRevenue(w http.ResponseWriter, r *http.Request) {
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
	limit := 10
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	if _, err := s.Polar.Store.Get(r.Context(), st.ID); err != nil {
		s.fail(w, err)
		return
	}
	rev, err := s.revenue(r, st.ID, rng, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

func (s *Server) revenue(r *http.Request, siteID, rng string, limit int) (Revenue, error) {
	ctx := r.Context()
	from, to, bucket := stats.Window(rng, s.Now())
	out := Revenue{Range: rng, Breakdowns: map[string][]polar.Row{}}
	var err error
	if out.Currency, err = s.Polar.Store.Currency(ctx, siteID); err != nil {
		return out, err
	}
	if out.Series, err = s.Polar.Store.Series(ctx, siteID, from, to, bucket); err != nil {
		return out, err
	}
	if out.Totals, err = s.Polar.Store.Totals(ctx, siteID, from, to); err != nil {
		return out, err
	}
	if out.Previous, err = s.Polar.Store.Totals(ctx, siteID, from.Add(-to.Sub(from)), from); err != nil {
		return out, err
	}
	for _, dim := range polar.Dims {
		if out.Breakdowns[dim], err = s.Polar.Store.Breakdown(ctx, siteID, dim, from, to, limit); err != nil {
			return out, err
		}
	}
	return out, nil
}
