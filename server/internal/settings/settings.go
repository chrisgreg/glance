// Package settings stores small key/value settings, including the daily
// visitor salt.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

// Store reads and writes settings.
type Store struct {
	db *sql.DB

	mu      sync.Mutex
	salt    string
	saltDay string
}

// New returns a Store.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Get returns a value or "".
func (s *Store) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Set writes a value.
func (s *Store) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Keys for the settings page.
const (
	KeyAccent        = "accent"         // hex colour, e.g. #7C83E8
	KeyTitle         = "title"          // wordmark shown in the header
	KeyMCPEnabled    = "mcp_enabled"    // "false" turns /mcp off; unset = on
	KeyRetentionDays = "retention_days" // raw event retention; env overrides
)

// General is the settings-page view.
type General struct {
	Accent           string `json:"accent"`
	Title            string `json:"title"`
	MCPEnabled       bool   `json:"mcp_enabled"`
	RetentionDays    int    `json:"retention_days"`
	RetentionFromEnv bool   `json:"retention_from_env"`
}

// Patch is a partial update to General.
type Patch struct {
	Accent        *string `json:"accent"`
	Title         *string `json:"title"`
	MCPEnabled    *bool   `json:"mcp_enabled"`
	RetentionDays *int    `json:"retention_days"`
}

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid setting")

// DefaultAccent is the design system's periwinkle.
const DefaultAccent = "#7C83E8"

// General reads the settings-page values. defRetention is the configured
// default; fromEnv says the environment pins it.
func (s *Store) General(ctx context.Context, defRetention int, fromEnv bool) (General, error) {
	g := General{Accent: DefaultAccent, Title: "Glance", MCPEnabled: true, RetentionDays: defRetention, RetentionFromEnv: fromEnv}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings WHERE key IN (?,?,?,?)`, KeyAccent, KeyTitle, KeyMCPEnabled, KeyRetentionDays)
	if err != nil {
		return g, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return g, err
		}
		switch k {
		case KeyAccent:
			if v != "" {
				g.Accent = v
			}
		case KeyTitle:
			if v != "" {
				g.Title = v
			}
		case KeyMCPEnabled:
			g.MCPEnabled = v != "false"
		case KeyRetentionDays:
			if !fromEnv {
				if n, err := strconv.Atoi(v); err == nil && n >= 2 {
					g.RetentionDays = n
				}
			}
		}
	}
	return g, rows.Err()
}

var hexRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Apply validates and saves p.
func (s *Store) Apply(ctx context.Context, p Patch, defRetention int, fromEnv bool) (General, error) {
	if p.Accent != nil {
		a := strings.TrimSpace(*p.Accent)
		if a == "" {
			a = DefaultAccent
		}
		if !hexRe.MatchString(a) {
			return General{}, fmt.Errorf("%w: accent must be a hex colour like #7C83E8", ErrInvalid)
		}
		if err := s.Set(ctx, KeyAccent, strings.ToUpper(a)); err != nil {
			return General{}, err
		}
	}
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if len(t) > 40 {
			return General{}, fmt.Errorf("%w: title must be 40 characters or fewer", ErrInvalid)
		}
		if err := s.Set(ctx, KeyTitle, t); err != nil {
			return General{}, err
		}
	}
	if p.MCPEnabled != nil {
		if err := s.Set(ctx, KeyMCPEnabled, strconv.FormatBool(*p.MCPEnabled)); err != nil {
			return General{}, err
		}
	}
	if p.RetentionDays != nil {
		if fromEnv {
			return General{}, fmt.Errorf("%w: GLANCE_RETENTION_DAYS is set in the environment and overrides this", ErrInvalid)
		}
		if *p.RetentionDays < 2 || *p.RetentionDays > 365 {
			return General{}, fmt.Errorf("%w: retention_days must be between 2 and 365", ErrInvalid)
		}
		if err := s.Set(ctx, KeyRetentionDays, strconv.Itoa(*p.RetentionDays)); err != nil {
			return General{}, err
		}
	}
	return s.General(ctx, defRetention, fromEnv)
}

// Salt returns the visitor-hash salt for the UTC day containing t. The salt
// rotates daily, so a visitor hash never links two days together, and it is
// persisted so a restart does not split a day's uniques.
func (s *Store) Salt(ctx context.Context, t time.Time) (string, error) {
	day := t.UTC().Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saltDay == day && s.salt != "" {
		return s.salt, nil
	}
	storedDay, err := s.Get(ctx, "salt_day")
	if err != nil {
		return "", err
	}
	if storedDay == day {
		salt, err := s.Get(ctx, "salt")
		if err != nil {
			return "", err
		}
		if salt != "" {
			s.salt, s.saltDay = salt, day
			return salt, nil
		}
	}
	salt := ids.Random(32)
	if err := s.Set(ctx, "salt", salt); err != nil {
		return "", err
	}
	if err := s.Set(ctx, "salt_day", day); err != nil {
		return "", err
	}
	s.salt, s.saltDay = salt, day
	return salt, nil
}
