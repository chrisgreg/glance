package searchconsole

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

// Service ties the store and client together: the connect handshake,
// property matching and the daily pull.
type Service struct {
	Store  *Store
	Client *Client
	Log    *slog.Logger
	Now    func() time.Time

	mu     sync.Mutex
	states map[string]pending
}

type pending struct {
	siteID  string
	expires time.Time
}

// NewService returns a Service.
func NewService(store *Store, client *Client, log *slog.Logger) *Service {
	return &Service{Store: store, Client: client, Log: log, Now: time.Now, states: map[string]pending{}}
}

// Configured reports whether an OAuth client is available.
func (s *Service) Configured() bool { return s != nil && s.Client.Configured() }

// BeginConnect mints a one-shot state for a site and returns the Google
// consent URL. The state is the only thing tying the callback to a site,
// so the callback needs no login of its own.
func (s *Service) BeginConnect(siteID, redirectURI string) string {
	state := ids.Random(24)
	s.mu.Lock()
	for k, p := range s.states {
		if s.Now().After(p.expires) {
			delete(s.states, k)
		}
	}
	s.states[state] = pending{siteID: siteID, expires: s.Now().Add(10 * time.Minute)}
	s.mu.Unlock()
	return s.Client.ConsentURL(redirectURI, state)
}

// PeekState returns the site a state belongs to without consuming it.
func (s *Service) PeekState(state string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.states[state]
	if !ok || s.Now().After(p.expires) {
		return "", false
	}
	return p.siteID, true
}

// consumeState returns the site a state was minted for, once.
func (s *Service) consumeState(state string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, p := range s.states {
		if subtle.ConstantTimeCompare([]byte(k), []byte(state)) == 1 {
			delete(s.states, k)
			if s.Now().After(p.expires) {
				return "", false
			}
			return p.siteID, true
		}
	}
	return "", false
}

// Status is what the settings panel shows.
type Status struct {
	Configured          bool        `json:"configured"`
	Connected           bool        `json:"connected"`
	Connection          *Connection `json:"connection,omitempty"`
	AvailableProperties []string    `json:"available_properties,omitempty"`
	NeedsReconnect      bool        `json:"needs_reconnect"`
	LatestDay           string      `json:"latest_day,omitempty"`
}

// Status describes a site's connection.
func (s *Service) Status(ctx context.Context, siteID string) (Status, error) {
	st := Status{Configured: s.Configured()}
	c, err := s.Store.Get(ctx, siteID)
	if err == ErrNotConnected {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Connected = true
	st.Connection = &c
	st.NeedsReconnect = c.SyncError == ErrReconnect.Error()
	st.LatestDay, _ = s.Store.LatestDay(ctx, siteID)
	if c.Property == "" && !st.NeedsReconnect {
		st.AvailableProperties, _ = s.properties(ctx, c.RefreshToken)
	}
	return st, nil
}

// CompleteConnect finishes the OAuth callback: validates the state,
// exchanges the code, picks the property matching the site's domain and
// saves the connection. Returns the site id so the caller can redirect.
func (s *Service) CompleteConnect(ctx context.Context, state, code, redirectURI, domain string) (string, error) {
	siteID, ok := s.consumeState(state)
	if !ok {
		return "", errStale
	}
	grant, err := s.Client.Exchange(ctx, code, redirectURI)
	if err != nil {
		return siteID, err
	}
	conn := Connection{SiteID: siteID, Email: grant.Email, RefreshToken: grant.RefreshToken}
	if props, err := s.Client.Properties(ctx, grant.AccessToken); err == nil {
		conn.Property = MatchProperty(domain, props)
	}
	if err := s.Store.Save(ctx, conn); err != nil {
		return siteID, err
	}
	if conn.Property != "" {
		go s.syncDetached(siteID)
	}
	return siteID, nil
}

type staleError struct{}

func (staleError) Error() string { return "this connect link has expired; try again" }

var errStale = staleError{}

// IsStale reports whether err came from an expired or reused state.
func IsStale(err error) bool { _, ok := err.(staleError); return ok }

// MatchProperty picks the Search Console property for a domain: the
// domain property first, then URL-prefix properties with and without www.
func MatchProperty(domain string, props []string) string {
	domain = strings.ToLower(strings.TrimPrefix(domain, "www."))
	want := []string{
		"sc-domain:" + domain,
		"https://" + domain + "/", "https://www." + domain + "/",
		"http://" + domain + "/", "http://www." + domain + "/",
	}
	for _, w := range want {
		for _, p := range props {
			if strings.EqualFold(p, w) {
				return p
			}
		}
	}
	return ""
}

// SetProperty changes the property and refreshes from it.
func (s *Service) SetProperty(ctx context.Context, siteID, property string) error {
	if err := s.Store.SetProperty(ctx, siteID, property); err != nil {
		return err
	}
	go s.syncDetached(siteID)
	return nil
}

// Disconnect revokes the grant and forgets everything.
func (s *Service) Disconnect(ctx context.Context, siteID string) error {
	c, err := s.Store.Get(ctx, siteID)
	if err != nil {
		return err
	}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s.Client.Revoke(rctx, c.RefreshToken)
	return s.Store.Delete(ctx, siteID)
}

func (s *Service) properties(ctx context.Context, refreshToken string) ([]string, error) {
	tok, err := s.Client.AccessToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return s.Client.Properties(ctx, tok)
}

// backfillDays is how far the first pull reaches; Search Console keeps 16 months.
const backfillDays = 16 * 30

// overlapDays are re-pulled on every sync because Google revises recent days.
const overlapDays = 3

// Sync pulls new rows for one site and records the outcome.
func (s *Service) Sync(ctx context.Context, siteID string) error {
	c, err := s.Store.Get(ctx, siteID)
	if err != nil {
		return err
	}
	if c.Property == "" {
		return nil
	}
	err = s.pull(ctx, c)
	msg := ""
	if err != nil {
		msg = err.Error()
		s.Log.Warn("searchconsole.sync_failed", "site", siteID, "error", msg)
	} else {
		s.Log.Info("searchconsole.synced", "site", siteID, "property", c.Property)
	}
	if merr := s.Store.MarkSynced(ctx, siteID, s.Now(), msg); merr != nil {
		return merr
	}
	return err
}

func (s *Service) pull(ctx context.Context, c Connection) error {
	tok, err := s.Client.AccessToken(ctx, c.RefreshToken)
	if err != nil {
		return err
	}
	today := s.Now().UTC()
	from := today.AddDate(0, 0, -backfillDays)
	if latest, _ := s.Store.LatestDay(ctx, c.SiteID); latest != "" {
		if d, err := time.Parse("2006-01-02", latest); err == nil {
			from = d.AddDate(0, 0, -overlapDays)
		}
	}
	rows, err := s.Client.Query(ctx, tok, c.Property, from.Format("2006-01-02"), today.Format("2006-01-02"))
	if err != nil {
		return err
	}
	// The property may have been switched while the pull ran; rows from
	// the old one must not land under the new one.
	if cur, err := s.Store.Get(ctx, c.SiteID); err != nil || cur.Property != c.Property {
		return nil
	}
	return s.Store.UpsertTerms(ctx, c.SiteID, rows)
}

func (s *Service) syncDetached(siteID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_ = s.Sync(ctx, siteID)
}

// SyncStale pulls every connected site not refreshed within maxAge.
func (s *Service) SyncStale(ctx context.Context, maxAge time.Duration) {
	list, err := s.Store.List(ctx)
	if err != nil {
		return
	}
	for _, c := range list {
		if c.Property == "" || c.SyncError == ErrReconnect.Error() {
			continue
		}
		if t, err := ids.Parse(c.SyncedAt); err == nil && s.Now().Sub(t) < maxAge {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		_ = s.Sync(sctx, c.SiteID)
		cancel()
		if ctx.Err() != nil {
			return
		}
	}
}
