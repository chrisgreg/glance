// Package searchconsole connects a site to Google Search Console and pulls
// the search terms Google sends to it. The referrer never carries the
// query, so this is the only way to see keywords.
package searchconsole

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

// ErrNotConnected is returned when a site has no Google connection.
var ErrNotConnected = errors.New("site is not connected to Google Search Console")

// ErrReconnect means Google no longer accepts the stored refresh token
// (revoked, or expired because the OAuth app is still in testing mode).
var ErrReconnect = errors.New("google connection expired; connect again")

// Connection is a site's Google Search Console link.
type Connection struct {
	SiteID       string `json:"site_id"`
	Property     string `json:"property"`
	Email        string `json:"email"`
	RefreshToken string `json:"-"`
	ConnectedAt  string `json:"connected_at"`
	SyncedAt     string `json:"synced_at"`
	SyncError    string `json:"sync_error"`
}

// Term is one search query's totals over a range.
type Term struct {
	Query       string  `json:"query"`
	Clicks      int     `json:"clicks"`
	Impressions int     `json:"impressions"`
	Position    float64 `json:"position"`
}

// DayTerm is one query on one day, as Google reports it.
type DayTerm struct {
	Day         string
	Query       string
	Clicks      int
	Impressions int
	Position    float64
}

// Store persists connections and search terms.
type Store struct{ db *sql.DB }

// NewStore returns a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const connCols = `site_id, property, email, refresh_token, connected_at, synced_at, sync_error`

func scanConn(row interface{ Scan(...any) error }) (Connection, error) {
	var c Connection
	err := row.Scan(&c.SiteID, &c.Property, &c.Email, &c.RefreshToken, &c.ConnectedAt, &c.SyncedAt, &c.SyncError)
	return c, err
}

// Get returns the site's connection or ErrNotConnected.
func (s *Store) Get(ctx context.Context, siteID string) (Connection, error) {
	c, err := scanConn(s.db.QueryRowContext(ctx, `SELECT `+connCols+` FROM google_connections WHERE site_id = ?`, siteID))
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotConnected
	}
	return c, err
}

// List returns every connection.
func (s *Store) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+connCols+` FROM google_connections ORDER BY connected_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		c, err := scanConn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Save inserts or replaces a site's connection and clears its terms, since
// a new connection may point at a different property.
func (s *Store) Save(ctx context.Context, c Connection) error {
	if c.ConnectedAt == "" {
		c.ConnectedAt = ids.Now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_terms WHERE site_id = ?`, c.SiteID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO google_connections (`+connCols+`) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(site_id) DO UPDATE SET property=excluded.property, email=excluded.email, refresh_token=excluded.refresh_token,
		connected_at=excluded.connected_at, synced_at='', sync_error=''`,
		c.SiteID, c.Property, c.Email, c.RefreshToken, c.ConnectedAt, "", "")
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SetProperty changes which Search Console property feeds the site and
// drops terms pulled from the previous one.
func (s *Store) SetProperty(ctx context.Context, siteID, property string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE google_connections SET property=?, synced_at='', sync_error='' WHERE site_id=?`, property, siteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotConnected
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_terms WHERE site_id = ?`, siteID); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes the connection and every term pulled through it.
func (s *Store) Delete(ctx context.Context, siteID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM google_connections WHERE site_id = ?`, siteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotConnected
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_terms WHERE site_id = ?`, siteID); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkSynced records a sync outcome. An empty errMsg means success.
func (s *Store) MarkSynced(ctx context.Context, siteID string, at time.Time, errMsg string) error {
	syncedAt := ""
	if errMsg == "" {
		syncedAt = ids.Format(at)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE google_connections SET synced_at = CASE WHEN ? = '' THEN synced_at ELSE ? END, sync_error = ? WHERE site_id = ?`,
		syncedAt, syncedAt, errMsg, siteID)
	return err
}

// UpsertTerms writes a batch of daily rows in one transaction.
func (s *Store) UpsertTerms(ctx context.Context, siteID string, terms []DayTerm) error {
	if len(terms) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO search_terms (site_id, day, query, clicks, impressions, position) VALUES (?,?,?,?,?,?)
		ON CONFLICT(site_id, day, query) DO UPDATE SET clicks=excluded.clicks, impressions=excluded.impressions, position=excluded.position`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range terms {
		if _, err := stmt.ExecContext(ctx, siteID, t.Day, t.Query, t.Clicks, t.Impressions, t.Position); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LatestDay returns the most recent day with data, or "" when none.
func (s *Store) LatestDay(ctx context.Context, siteID string) (string, error) {
	var day sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT MAX(day) FROM search_terms WHERE site_id = ?`, siteID).Scan(&day)
	return day.String, err
}

// Terms aggregates queries between two days inclusive, most clicked first.
// Position is impression-weighted, matching Search Console's own report.
func (s *Store) Terms(ctx context.Context, siteID, fromDay, toDay string, limit int) ([]Term, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT query, SUM(clicks), SUM(impressions),
		SUM(position * impressions) / MAX(SUM(impressions), 1)
		FROM search_terms WHERE site_id = ? AND day >= ? AND day <= ?
		GROUP BY query ORDER BY SUM(clicks) DESC, SUM(impressions) DESC, query ASC LIMIT ?`, siteID, fromDay, toDay, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Term{}
	for rows.Next() {
		var t Term
		if err := rows.Scan(&t.Query, &t.Clicks, &t.Impressions, &t.Position); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
