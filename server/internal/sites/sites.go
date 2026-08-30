// Package sites manages the tracked websites.
package sites

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/chrisgreg/glance/server/internal/database"
	"github.com/chrisgreg/glance/server/internal/ids"
)

// ErrNotFound is returned when a site does not exist.
var ErrNotFound = errors.New("site not found")

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid site")

// Site is a tracked website.
type Site struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	HomeCountry string `json:"home_country"`
	HasFavicon  bool   `json:"has_favicon"`
	Position    int    `json:"position"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Input is the writable subset of a site.
type Input struct {
	Name        *string `json:"name"`
	Domain      *string `json:"domain"`
	HomeCountry *string `json:"home_country"`
}

// Store persists sites and keeps an in-memory index for the ingest path.
type Store struct {
	db *sql.DB

	mu    sync.RWMutex
	byID  map[string]Site
	ready bool
}

// New returns a Store.
func New(db *sql.DB) *Store { return &Store{db: db, byID: map[string]Site{}} }

const cols = `id, name, domain, home_country, favicon IS NOT NULL, position, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Site, error) {
	var s Site
	var fav int
	err := row.Scan(&s.ID, &s.Name, &s.Domain, &s.HomeCountry, &fav, &s.Position, &s.CreatedAt, &s.UpdatedAt)
	s.HasFavicon = fav == 1
	return s, err
}

var domainRe = regexp.MustCompile(`^(localhost|([a-z0-9-]+\.)+[a-z]{2,63})(:\d{1,5})?$`)

// NormaliseDomain lower-cases, strips scheme, path and a leading www.
func NormaliseDomain(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	d = strings.TrimPrefix(d, "www.")
	if d == "" {
		return "", fmt.Errorf("%w: domain is required", ErrInvalid)
	}
	if !domainRe.MatchString(d) {
		return "", fmt.Errorf("%w: %q is not a valid domain", ErrInvalid, d)
	}
	return d, nil
}

func validCountry(cc string) (string, error) {
	cc = strings.ToUpper(strings.TrimSpace(cc))
	if cc == "" {
		return "", nil
	}
	if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' {
		return "", fmt.Errorf("%w: home_country must be a two-letter country code", ErrInvalid)
	}
	return cc, nil
}

// Create inserts a site.
func (s *Store) Create(ctx context.Context, in Input) (Site, error) {
	if in.Domain == nil {
		return Site{}, fmt.Errorf("%w: domain is required", ErrInvalid)
	}
	d, err := NormaliseDomain(*in.Domain)
	if err != nil {
		return Site{}, err
	}
	site := Site{ID: ids.New("site"), Domain: d}
	if in.Name != nil {
		site.Name = strings.TrimSpace(*in.Name)
	}
	if site.Name == "" {
		site.Name = d
	}
	if len(site.Name) > 80 {
		return Site{}, fmt.Errorf("%w: name must be 80 characters or fewer", ErrInvalid)
	}
	if in.HomeCountry != nil {
		if site.HomeCountry, err = validCountry(*in.HomeCountry); err != nil {
			return Site{}, err
		}
	}
	now := ids.Now()
	site.CreatedAt, site.UpdatedAt = now, now
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM sites`).Scan(&site.Position); err != nil {
		return Site{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sites (id, name, domain, home_country, position, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		site.ID, site.Name, site.Domain, site.HomeCountry, site.Position, site.CreatedAt, site.UpdatedAt)
	if database.IsUniqueViolation(err) {
		return Site{}, fmt.Errorf("%w: %s is already tracked", ErrInvalid, d)
	}
	if err != nil {
		return Site{}, err
	}
	s.invalidate()
	return site, nil
}

// Update applies the non-nil fields of in.
func (s *Store) Update(ctx context.Context, id string, in Input) (Site, error) {
	site, err := s.Get(ctx, id)
	if err != nil {
		return Site{}, err
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 80 {
			return Site{}, fmt.Errorf("%w: name must be 1-80 characters", ErrInvalid)
		}
		site.Name = n
	}
	if in.Domain != nil {
		d, err := NormaliseDomain(*in.Domain)
		if err != nil {
			return Site{}, err
		}
		site.Domain = d
	}
	if in.HomeCountry != nil {
		if site.HomeCountry, err = validCountry(*in.HomeCountry); err != nil {
			return Site{}, err
		}
	}
	site.UpdatedAt = ids.Now()
	_, err = s.db.ExecContext(ctx, `UPDATE sites SET name=?, domain=?, home_country=?, updated_at=? WHERE id=?`, site.Name, site.Domain, site.HomeCountry, site.UpdatedAt, site.ID)
	if database.IsUniqueViolation(err) {
		return Site{}, fmt.Errorf("%w: %s is already tracked", ErrInvalid, site.Domain)
	}
	if err != nil {
		return Site{}, err
	}
	s.invalidate()
	return site, nil
}

// List returns every site in display order.
func (s *Store) List(ctx context.Context) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM sites ORDER BY position ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		st, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Get returns a site by id.
func (s *Store) Get(ctx context.Context, id string) (Site, error) {
	st, err := scan(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM sites WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	return st, err
}

// Lookup resolves a site id from the in-memory index; used on the ingest
// path so no query runs per event.
func (s *Store) Lookup(ctx context.Context, id string) (Site, bool) {
	s.mu.RLock()
	if s.ready {
		st, ok := s.byID[id]
		s.mu.RUnlock()
		return st, ok
	}
	s.mu.RUnlock()
	list, err := s.List(ctx)
	if err != nil {
		return Site{}, false
	}
	s.mu.Lock()
	s.byID = map[string]Site{}
	for _, st := range list {
		s.byID[st.ID] = st
	}
	s.ready = true
	st, ok := s.byID[id]
	s.mu.Unlock()
	return st, ok
}

func (s *Store) invalidate() {
	s.mu.Lock()
	s.ready = false
	s.mu.Unlock()
}

// Reorder sets display positions to follow idList.
func (s *Store) Reorder(ctx context.Context, idList []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE sites SET position = position + ?`, len(idList)); err != nil {
		return err
	}
	for i, id := range idList {
		if _, err := tx.ExecContext(ctx, `UPDATE sites SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Delete removes a site and all of its data.
func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	for _, q := range []string{`DELETE FROM events WHERE site_id = ?`, `DELETE FROM hourly_stats WHERE site_id = ?`, `DELETE FROM daily_stats WHERE site_id = ?`,
		`DELETE FROM google_connections WHERE site_id = ?`, `DELETE FROM search_terms WHERE site_id = ?`} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// SetFavicon stores the site's icon bytes.
func (s *Store) SetFavicon(ctx context.Context, id string, data []byte, ctype string) error {
	var blob any
	if len(data) > 0 {
		blob = data
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sites SET favicon=?, favicon_type=?, favicon_at=? WHERE id=?`, blob, ctype, ids.Now(), id)
	s.invalidate()
	return err
}

// Favicon returns the stored icon.
func (s *Store) Favicon(ctx context.Context, id string) (data []byte, ctype string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(favicon, X''), favicon_type FROM sites WHERE id = ?`, id).Scan(&data, &ctype)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return data, ctype, err
}

// StaleFavicons returns sites whose icon is missing or older than `before`.
func (s *Store) StaleFavicons(ctx context.Context, before string) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM sites WHERE favicon_at = '' OR favicon_at < ?`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		st, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
