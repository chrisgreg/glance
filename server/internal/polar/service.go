package polar

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chrisgreg/glance/server/internal/enrich"
	"github.com/chrisgreg/glance/server/internal/ids"
)

// Service runs the connect check, the pull and the webhook.
type Service struct {
	Store  *Store
	Client *Client
	Log    *slog.Logger
	Now    func() time.Time
	// Domain resolves a site id to its domain, so attribution referrers
	// are normalised the same way page views are.
	Domain func(ctx context.Context, siteID string) string
}

// NewService returns a Service.
func NewService(store *Store, client *Client, log *slog.Logger) *Service {
	return &Service{Store: store, Client: client, Log: log, Now: time.Now}
}

// Input is what the settings panel sends.
type Input struct {
	AccessToken   *string `json:"access_token"`
	Server        *string `json:"server"`
	ProductIDs    *string `json:"product_ids"`
	WebhookSecret *string `json:"webhook_secret"`
}

// Connect validates the token against Polar and saves the connection. On
// an existing connection, omitted secrets are kept.
func (s *Service) Connect(ctx context.Context, siteID string, in Input) (Connection, error) {
	c, err := s.Store.Get(ctx, siteID)
	if errors.Is(err, ErrNotConnected) {
		c = Connection{SiteID: siteID, Server: DefaultServer}
	} else if err != nil {
		return Connection{}, err
	}
	if in.AccessToken != nil && strings.TrimSpace(*in.AccessToken) != "" {
		c.AccessToken = strings.TrimSpace(*in.AccessToken)
	}
	if c.AccessToken == "" {
		return Connection{}, fmt.Errorf("%w: an organization access token is required", ErrInvalid)
	}
	if in.Server != nil {
		srv := strings.TrimRight(strings.TrimSpace(*in.Server), "/")
		if srv == "" {
			srv = DefaultServer
		}
		u, err := url.Parse(srv)
		local := err == nil && u.Scheme == "http" && enrich.LocalHost(u.Hostname())
		if err != nil || u.Host == "" || (u.Scheme != "https" && !local) {
			return Connection{}, fmt.Errorf("%w: server must be an https URL", ErrInvalid)
		}
		c.Server = srv
	}
	if in.ProductIDs != nil {
		var clean []string
		for _, p := range strings.Split(*in.ProductIDs, ",") {
			if p = strings.TrimSpace(p); p != "" {
				clean = append(clean, p)
			}
		}
		c.ProductIDs = strings.Join(clean, ",")
	}
	if in.WebhookSecret != nil {
		c.WebhookSecret = strings.TrimSpace(*in.WebhookSecret)
	}
	if err := s.Client.Check(ctx, c.Server, c.AccessToken); err != nil {
		return Connection{}, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	if err := s.Store.Save(ctx, c); err != nil {
		return Connection{}, err
	}
	go s.syncDetached(siteID)
	return s.Store.Get(ctx, siteID)
}

// backfill is how far the first pull reaches.
const backfill = 2 * 365 * 24 * time.Hour

// overlap is re-pulled on every sync so refunds and status changes on
// recent orders are picked up even without the webhook.
const overlap = 30 * 24 * time.Hour

// Sync pulls orders for one site and records the outcome.
func (s *Service) Sync(ctx context.Context, siteID, domain string) error {
	c, err := s.Store.Get(ctx, siteID)
	if err != nil {
		return err
	}
	err = s.pull(ctx, c, domain)
	msg := ""
	if err != nil {
		msg = err.Error()
		s.Log.Warn("polar.sync_failed", "site", siteID, "error", msg)
	} else {
		s.Log.Info("polar.synced", "site", siteID)
	}
	if merr := s.Store.MarkSynced(ctx, siteID, s.Now(), msg); merr != nil {
		return merr
	}
	return err
}

func (s *Service) pull(ctx context.Context, c Connection, domain string) error {
	since := s.Now().Add(-backfill)
	if latest, _ := s.Store.LatestOrderAt(ctx, c.SiteID); !latest.IsZero() {
		since = latest.Add(-overlap)
	}
	raw, err := s.Client.Orders(ctx, c.Server, c.AccessToken, c.Products(), since)
	if err != nil {
		return err
	}
	orders := make([]Order, 0, len(raw))
	for _, r := range raw {
		if o, ok := ParseOrder(r, domain); ok {
			orders = append(orders, o)
		}
	}
	return s.Store.UpsertOrders(ctx, c.SiteID, orders)
}

func (s *Service) syncDetached(siteID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, err := s.Store.Get(ctx, siteID)
	if err != nil {
		return
	}
	_ = s.Sync(ctx, c.SiteID, s.domainOf(siteID))
}

func (s *Service) domainOf(siteID string) string {
	if s.Domain == nil {
		return ""
	}
	return s.Domain(context.Background(), siteID)
}

// SyncStale pulls every connected site not refreshed within maxAge.
func (s *Service) SyncStale(ctx context.Context, maxAge time.Duration) {
	list, err := s.Store.List(ctx)
	if err != nil {
		return
	}
	for _, c := range list {
		if t, err := ids.Parse(c.SyncedAt); err == nil && s.Now().Sub(t) < maxAge {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		_ = s.Sync(sctx, c.SiteID, s.domainOf(c.SiteID))
		cancel()
		if ctx.Err() != nil {
			return
		}
	}
}

// ParseOrder turns a raw Polar order into Glance's shape. Attribution comes
// from the checkout metadata keys attr_ref and attr_landing, which the
// site's snippet recorded on first touch and the site passed to checkout;
// they are normalised with the same rules as page views so the revenue
// breakdowns line up with the traffic ones.
func ParseOrder(r RawOrder, domain string) (Order, bool) {
	id := r.str("id")
	if id == "" {
		return Order{}, false
	}
	created, err := time.Parse(time.RFC3339Nano, r.str("created_at"))
	if err != nil {
		return Order{}, false
	}
	o := Order{OrderID: id, CreatedAt: created.UTC(), Status: r.str("status"), Paid: r.boolean("paid"), Currency: r.str("currency")}
	if o.Status == "paid" || o.Status == "refunded" || o.Status == "partially_refunded" {
		o.Paid = true
	}
	o.NetAmount, _ = r.num("net_amount")
	if n, ok := r.num("refunded_amount"); ok {
		o.RefundedAmount = n
	} else if o.Status == "refunded" {
		o.RefundedAmount = o.NetAmount
	}
	if o.RefundedAmount > o.NetAmount {
		o.RefundedAmount = o.NetAmount
	}
	o.Country = strings.ToUpper(firstOf(r.str("billing_address", "country"), r.str("customer", "billing_address", "country")))
	o.Product = firstOf(r.str("product", "name"), r.str("product_id"))
	if len(o.Product) > 80 {
		o.Product = o.Product[:80]
	}
	landing := r.str("metadata", "attr_landing")
	o.Ref = enrich.Referrer(r.str("metadata", "attr_ref"), domain)
	o.Source, o.Campaign = enrich.UTM(landing)
	if u, err := url.Parse(landing); err == nil && landing != "" {
		o.Landing = u.Path
		if o.Landing == "" {
			o.Landing = "/"
		}
		if len(o.Landing) > 200 {
			o.Landing = o.Landing[:200]
		}
	}
	return o, true
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- webhook ----

// ErrSignature is returned when a webhook fails verification.
var ErrSignature = errors.New("webhook signature invalid")

// timestampTolerance is generous because Polar resends failed deliveries
// with the original timestamp.
const timestampTolerance = 24 * time.Hour

// VerifyWebhook checks a Standard Webhooks signature: HMAC-SHA256 over
// "id.timestamp.body". Providers disagree on whether the secret is raw or
// base64 (with or without a whsec_ prefix), so every reading is tried.
func VerifyWebhook(secret, msgID, timestamp, sigHeader string, body []byte, now time.Time) error {
	if secret == "" {
		return fmt.Errorf("%w: no webhook secret saved for this site", ErrSignature)
	}
	if msgID == "" || timestamp == "" || sigHeader == "" {
		return fmt.Errorf("%w: missing webhook headers", ErrSignature)
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || now.Sub(time.Unix(ts, 0)).Abs() > timestampTolerance {
		return fmt.Errorf("%w: timestamp out of tolerance", ErrSignature)
	}
	msg := []byte(msgID + "." + timestamp + ".")
	msg = append(msg, body...)
	var expected []string
	for _, key := range candidateKeys(secret) {
		mac := hmac.New(sha256.New, key)
		mac.Write(msg)
		expected = append(expected, base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	}
	for _, versioned := range strings.Fields(sigHeader) {
		v, sig, ok := strings.Cut(versioned, ",")
		if !ok || v != "v1" {
			continue
		}
		for _, e := range expected {
			if subtle.ConstantTimeCompare([]byte(sig), []byte(e)) == 1 {
				return nil
			}
		}
	}
	return ErrSignature
}

func candidateKeys(secret string) [][]byte {
	trimmed := strings.TrimSpace(secret)
	stripped := strings.TrimPrefix(trimmed, "whsec_")
	keys := [][]byte{[]byte(trimmed)}
	if stripped != trimmed {
		keys = append(keys, []byte(stripped))
	}
	if k, err := base64.StdEncoding.DecodeString(stripped); err == nil {
		keys = append(keys, k)
	} else if k, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(stripped, "=")); err == nil {
		keys = append(keys, k)
	}
	return keys
}

// HandleWebhook applies one verified event. Orders for products outside
// the site's filter are ignored; every other event type is a no-op.
func (s *Service) HandleWebhook(ctx context.Context, c Connection, domain string, body []byte) (string, error) {
	var ev struct {
		Type string   `json:"type"`
		Data RawOrder `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return "", fmt.Errorf("%w: body is not JSON", ErrInvalid)
	}
	if !strings.HasPrefix(ev.Type, "order.") {
		return ev.Type, nil
	}
	if filter := c.Products(); len(filter) > 0 && !anyIn(ev.Data.ProductIDs(), filter) {
		return ev.Type, nil
	}
	o, ok := ParseOrder(ev.Data, domain)
	if !ok {
		return ev.Type, fmt.Errorf("%w: order payload is missing id or created_at", ErrInvalid)
	}
	return ev.Type, s.Store.UpsertOrders(ctx, c.SiteID, []Order{o})
}

func anyIn(have, want []string) bool {
	for _, h := range have {
		for _, w := range want {
			if h == w {
				return true
			}
		}
	}
	return false
}
