package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

// SessionStore persists admin sessions so a restart does not log the admin out.
type SessionStore struct{ db *sql.DB }

// NewSessionStore returns a SessionStore over db.
func NewSessionStore(db *sql.DB) *SessionStore { return &SessionStore{db: db} }

// Save records a session token hash bound to the password it was created with.
func (s *SessionStore) Save(ctx context.Context, tokenHash, passwordHash string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, password_hash, expires_at, created_at) VALUES (?,?,?,?)`,
		tokenHash, passwordHash, ids.Format(expires), ids.Now())
	return err
}

// Lookup returns the password hash and expiry for a token hash.
func (s *SessionStore) Lookup(ctx context.Context, tokenHash string) (passwordHash string, expires time.Time, ok bool, err error) {
	var exp string
	err = s.db.QueryRowContext(ctx, `SELECT password_hash, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&passwordHash, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	expires, _ = ids.Parse(exp)
	return passwordHash, expires, true, nil
}

// Delete removes a session.
func (s *SessionStore) Delete(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// Prune removes expired sessions.
func (s *SessionStore) Prune(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, ids.Format(now))
	return err
}
