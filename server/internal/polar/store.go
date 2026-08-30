// Package polar links a site to a Polar organisation so revenue sits next
// to traffic: orders are pulled through the API and pushed by webhook, and
// each order carries the first-touch attribution the site put into its
// checkout metadata.
package polar

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

// ErrNotConnected is returned when a site has no Polar connection.
var ErrNotConnected = errors.New("site is not connected to Polar")

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid polar settings")

// DefaultServer is Polar's production API.
const DefaultServer = "https://api.polar.sh"

// Connection is a site's Polar link. Secrets never serialise.
type Connection struct {
	SiteID           string `json:"site_id"`
	AccessToken      string `json:"-"`
	Server           string `json:"server"`
	ProductIDs       string `json:"product_ids"`
	WebhookSecret    string `json:"-"`
	HasWebhookSecret bool   `json:"has_webhook_secret"`
	ConnectedAt      string `json:"connected_at"`
	SyncedAt         string `json:"synced_at"`
	SyncError        string `json:"sync_error"`
}

// Products splits the comma-separated filter.
func (c Connection) Products() []string {
	var out []string
	for _, p := range strings.Split(c.ProductIDs, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Order is one Polar order as Glance keeps it.
type Order struct {
	OrderID        string
	CreatedAt      time.Time
	Status         string
	Paid           bool
	NetAmount      int
	RefundedAmount int
	Currency       string
	Country        string
	Product        string
	Ref            string
	Source         string
	Campaign       string
	Landing        string
}

// Store persists connections and orders.
type Store struct{ db *sql.DB }

// NewStore returns a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const connCols = `site_id, access_token, server, product_ids, webhook_secret, connected_at, synced_at, sync_error`

func scanConn(row interface{ Scan(...any) error }) (Connection, error) {
	var c Connection
	err := row.Scan(&c.SiteID, &c.AccessToken, &c.Server, &c.ProductIDs, &c.WebhookSecret, &c.ConnectedAt, &c.SyncedAt, &c.SyncError)
	c.HasWebhookSecret = c.WebhookSecret != ""
	return c, err
}

// Get returns the site's connection or ErrNotConnected.
func (s *Store) Get(ctx context.Context, siteID string) (Connection, error) {
	c, err := scanConn(s.db.QueryRowContext(ctx, `SELECT `+connCols+` FROM polar_connections WHERE site_id = ?`, siteID))
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotConnected
	}
	return c, err
}

// List returns every connection.
func (s *Store) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+connCols+` FROM polar_connections ORDER BY connected_at`)
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

// Save inserts or replaces a connection. Orders are cleared when the
// server or product filter changes, since they may no longer belong.
func (s *Store) Save(ctx context.Context, c Connection) error {
	if c.ConnectedAt == "" {
		c.ConnectedAt = ids.Now()
	}
	if c.Server == "" {
		c.Server = DefaultServer
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	prev, err := scanConn(tx.QueryRowContext(ctx, `SELECT `+connCols+` FROM polar_connections WHERE site_id = ?`, c.SiteID))
	if err == nil && (prev.Server != c.Server || prev.ProductIDs != c.ProductIDs) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM polar_orders WHERE site_id = ?`, c.SiteID); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO polar_connections (`+connCols+`) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(site_id) DO UPDATE SET access_token=excluded.access_token, server=excluded.server, product_ids=excluded.product_ids,
		webhook_secret=excluded.webhook_secret, synced_at='', sync_error=''`,
		c.SiteID, c.AccessToken, c.Server, c.ProductIDs, c.WebhookSecret, c.ConnectedAt, "", "")
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes the connection and every order.
func (s *Store) Delete(ctx context.Context, siteID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM polar_connections WHERE site_id = ?`, siteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotConnected
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM polar_orders WHERE site_id = ?`, siteID); err != nil {
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
	_, err := s.db.ExecContext(ctx, `UPDATE polar_connections SET synced_at = CASE WHEN ? = '' THEN synced_at ELSE ? END, sync_error = ? WHERE site_id = ?`,
		syncedAt, syncedAt, errMsg, siteID)
	return err
}

// UpsertOrders writes a batch of orders in one transaction.
func (s *Store) UpsertOrders(ctx context.Context, siteID string, orders []Order) error {
	if len(orders) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO polar_orders
		(site_id, order_id, created_at, status, paid, net_amount, refunded_amount, currency, country, product, ref, source, campaign, landing)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(site_id, order_id) DO UPDATE SET status=excluded.status, paid=excluded.paid, net_amount=excluded.net_amount,
		refunded_amount=excluded.refunded_amount, currency=excluded.currency, country=excluded.country, product=excluded.product,
		ref=excluded.ref, source=excluded.source, campaign=excluded.campaign, landing=excluded.landing`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, o := range orders {
		paid := 0
		if o.Paid {
			paid = 1
		}
		if _, err := stmt.ExecContext(ctx, siteID, o.OrderID, o.CreatedAt.UTC().Format(time.RFC3339), o.Status, paid, o.NetAmount, o.RefundedAmount,
			strings.ToLower(o.Currency), o.Country, o.Product, o.Ref, o.Source, o.Campaign, o.Landing); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LatestOrderAt returns the newest order's creation time, or zero.
func (s *Store) LatestOrderAt(ctx context.Context, siteID string) (time.Time, error) {
	var at sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM polar_orders WHERE site_id = ?`, siteID).Scan(&at); err != nil {
		return time.Time{}, err
	}
	if !at.Valid || at.String == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, at.String)
}

// revenueExpr is what an order is worth: paid, net of discounts and tax,
// less anything refunded. Refunds land on the order's day, not the
// refund's.
const revenueExpr = `CASE WHEN paid = 1 THEN net_amount - refunded_amount ELSE 0 END`

// Point is one bucket of revenue.
type Point struct {
	T       string `json:"t"`
	Revenue int    `json:"revenue"` // cents
	Orders  int    `json:"orders"`
}

// Totals for a window.
type Totals struct {
	Revenue int `json:"revenue"` // cents
	Orders  int `json:"orders"`
}

// Row is one attribution breakdown entry.
type Row struct {
	Key     string `json:"key"`
	Revenue int    `json:"revenue"` // cents
	Orders  int    `json:"orders"`
}

// Series buckets revenue over [from, to) by hour or day, zero-filled.
func (s *Store) Series(ctx context.Context, siteID string, from, to time.Time, bucket string) ([]Point, error) {
	step, layout, trunc := 24*time.Hour, "2006-01-02", "%Y-%m-%d"
	if bucket == "hour" {
		step, layout, trunc = time.Hour, "2006-01-02T15", "%Y-%m-%dT%H"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT strftime('`+trunc+`', created_at), SUM(`+revenueExpr+`), SUM(paid)
		FROM polar_orders WHERE site_id = ? AND created_at >= ? AND created_at < ? GROUP BY 1`,
		siteID, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[string]Point{}
	for rows.Next() {
		var k string
		var p Point
		if err := rows.Scan(&k, &p.Revenue, &p.Orders); err != nil {
			return nil, err
		}
		got[k] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Point{}
	for t := from; t.Before(to); t = t.Add(step) {
		p := got[t.UTC().Format(layout)]
		p.T = t.Format(time.RFC3339)
		out = append(out, p)
	}
	return out, nil
}

// Totals sums revenue and paid orders over [from, to).
func (s *Store) Totals(ctx context.Context, siteID string, from, to time.Time) (Totals, error) {
	var t Totals
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(`+revenueExpr+`), 0), COALESCE(SUM(paid), 0)
		FROM polar_orders WHERE site_id = ? AND created_at >= ? AND created_at < ?`,
		siteID, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339)).Scan(&t.Revenue, &t.Orders)
	return t, err
}

// Dims are the attribution breakdowns.
var Dims = []string{"ref", "source", "campaign", "landing", "country", "product"}

// ValidDim reports whether dim is a breakdown dimension.
func ValidDim(dim string) bool {
	for _, d := range Dims {
		if d == dim {
			return true
		}
	}
	return false
}

// Breakdown sums revenue per key of one dimension over [from, to).
func (s *Store) Breakdown(ctx context.Context, siteID, dim string, from, to time.Time, limit int) ([]Row, error) {
	if !ValidDim(dim) {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+dim+`, SUM(`+revenueExpr+`), SUM(paid) FROM polar_orders
		WHERE site_id = ? AND created_at >= ? AND created_at < ? AND paid = 1
		GROUP BY `+dim+` ORDER BY 2 DESC, 3 DESC, 1 ASC LIMIT ?`,
		siteID, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.Key, &r.Revenue, &r.Orders); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Currency returns the currency most orders were charged in.
func (s *Store) Currency(ctx context.Context, siteID string) (string, error) {
	var cur sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT currency FROM polar_orders WHERE site_id = ? GROUP BY currency ORDER BY COUNT(*) DESC LIMIT 1`, siteID).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return cur.String, err
}
