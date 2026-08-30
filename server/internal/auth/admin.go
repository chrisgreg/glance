// Package auth guards the admin UI and API with a single username/password
// taken from the environment.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

// SessionCookie is the name of the admin session cookie.
const SessionCookie = "glance_session"

// SessionTTL is how long an admin login lasts.
const SessionTTL = 30 * 24 * time.Hour

// Admin guards /admin and the admin endpoints. When Enabled is false
// everything is open (only sensible behind your own proxy or VPN).
//
// Sessions are kept in memory and, when a SessionStore is set, in SQLite so
// they survive restarts. A session is only valid for the password it was
// created with, so changing the password logs everyone out.
type Admin struct {
	username string
	password string // stored hashed
	enabled  bool
	store    *SessionStore

	mu       sync.Mutex
	sessions map[string]time.Time // token hash -> expiry (cache)
	now      func() time.Time
}

// NewAdmin returns an Admin; it is enabled only when both values are non-empty.
func NewAdmin(username, password string, store *SessionStore) *Admin {
	a := &Admin{sessions: map[string]time.Time{}, now: time.Now, store: store}
	if username != "" && password != "" {
		a.enabled = true
		a.username = username
		a.password = Hash(password)
	}
	return a
}

// Enabled reports whether admin authentication is configured.
func (a *Admin) Enabled() bool { return a != nil && a.enabled }

// Check verifies a username/password pair in constant time.
func (a *Admin) Check(username, password string) bool {
	if !a.Enabled() {
		return false
	}
	u := subtle.ConstantTimeCompare([]byte(Hash(username)), []byte(Hash(a.username))) == 1
	p := subtle.ConstantTimeCompare([]byte(Hash(password)), []byte(a.password)) == 1
	return u && p
}

// Login creates a session and returns its raw token.
func (a *Admin) Login(ctx context.Context, username, password string) (string, bool) {
	if !a.Check(username, password) {
		return "", false
	}
	tok := "glance_sess_" + ids.Random(20)
	exp := a.now().Add(SessionTTL)
	a.mu.Lock()
	a.sessions[Hash(tok)] = exp
	a.mu.Unlock()
	if a.store != nil {
		_ = a.store.Save(ctx, Hash(tok), a.password, exp)
	}
	return tok, true
}

// Logout revokes a session token.
func (a *Admin) Logout(ctx context.Context, token string) {
	a.mu.Lock()
	delete(a.sessions, Hash(token))
	a.mu.Unlock()
	if a.store != nil {
		_ = a.store.Delete(ctx, Hash(token))
	}
}

// Valid reports whether a raw session token is live.
func (a *Admin) Valid(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	h := Hash(token)
	a.mu.Lock()
	exp, ok := a.sessions[h]
	a.mu.Unlock()
	if !ok && a.store != nil {
		pw, storedExp, found, err := a.store.Lookup(ctx, h)
		if err != nil || !found || !Equal(pw, a.password) {
			return false
		}
		exp, ok = storedExp, true
		a.mu.Lock()
		a.sessions[h] = exp
		a.mu.Unlock()
	}
	if !ok {
		return false
	}
	if a.now().After(exp) {
		a.mu.Lock()
		delete(a.sessions, h)
		a.mu.Unlock()
		return false
	}
	return true
}

// Authorized reports whether r carries a valid session cookie or HTTP Basic
// credentials. Always true when auth is not enabled.
func (a *Admin) Authorized(r *http.Request) bool {
	if !a.Enabled() {
		return true
	}
	if c, err := r.Cookie(SessionCookie); err == nil && a.Valid(r.Context(), c.Value) {
		return true
	}
	if u, p, ok := r.BasicAuth(); ok && a.Check(u, p) {
		return true
	}
	return false
}

// SetCookie writes the session cookie for token onto w.
func (a *Admin) SetCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), MaxAge: int(SessionTTL.Seconds()),
	})
}

// ClearCookie removes the session cookie.
func (a *Admin) ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r), MaxAge: -1})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// Hash returns the hex SHA-256 of s.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Equal compares two hashes in constant time.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
