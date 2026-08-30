// Package tokens mints and checks API tokens for agents. The raw token is
// shown once; only its hash is stored.
package tokens

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chrisgreg/glance/server/internal/auth"
	"github.com/chrisgreg/glance/server/internal/ids"
)

// ErrNotFound is returned when a token does not exist.
var ErrNotFound = errors.New("token not found")

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid token")

// Prefix every minted token starts with.
const Prefix = "glance_tok_"

// Token is a minted API token (never carries the secret after creation).
type Token struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// Store persists tokens.
type Store struct {
	db *sql.DB
	mu sync.Mutex
	// touched throttles last_used_at writes to once a minute per token.
	touched map[string]time.Time
}

// New returns a Store.
func New(db *sql.DB) *Store { return &Store{db: db, touched: map[string]time.Time{}} }

// Create mints a token and returns it with the raw secret.
func (s *Store) Create(ctx context.Context, name string) (Token, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "API token"
	}
	if len(name) > 60 {
		return Token{}, "", fmt.Errorf("%w: name must be 60 characters or fewer", ErrInvalid)
	}
	raw := Prefix + ids.Random(20)
	t := Token{ID: ids.New("tok"), Name: name, Prefix: raw[:len(Prefix)+6], CreatedAt: ids.Now()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (id, name, token_hash, prefix, created_at) VALUES (?,?,?,?,?)`, t.ID, t.Name, auth.Hash(raw), t.Prefix, t.CreatedAt)
	if err != nil {
		return Token{}, "", err
	}
	return t, raw, nil
}

// List returns every token, newest first.
func (s *Store) List(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, created_at, last_used_at FROM api_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Delete revokes a token.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Authenticate resolves a raw token, recording its use.
func (s *Store) Authenticate(ctx context.Context, raw string) (Token, bool) {
	if !strings.HasPrefix(raw, Prefix) {
		return Token{}, false
	}
	var t Token
	err := s.db.QueryRowContext(ctx, `SELECT id, name, prefix, created_at, last_used_at FROM api_tokens WHERE token_hash = ?`, auth.Hash(raw)).
		Scan(&t.ID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsedAt)
	if err != nil {
		return Token{}, false
	}
	s.mu.Lock()
	last, seen := s.touched[t.ID]
	now := time.Now()
	if !seen || now.Sub(last) > time.Minute {
		s.touched[t.ID] = now
		s.mu.Unlock()
		_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, ids.Format(now), t.ID)
	} else {
		s.mu.Unlock()
	}
	return t, true
}
