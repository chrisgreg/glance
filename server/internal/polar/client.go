package polar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to the Polar API with an organisation access token.
type Client struct {
	HTTP *http.Client
}

// NewClient returns a Client.
func NewClient() *Client { return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}} }

// pageSize is Polar's list maximum.
const pageSize = 100

// RawOrder is an order as Polar sends it, kept loose so field drift in the
// API never breaks parsing.
type RawOrder map[string]any

// Orders lists orders created after `since`, newest first, paging until
// done. productIDs narrows to the site's own products.
func (c *Client) Orders(ctx context.Context, server, token string, productIDs []string, since time.Time) ([]RawOrder, error) {
	var out []RawOrder
	for page := 1; ; page++ {
		q := url.Values{"limit": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}, "sorting": {"-created_at"}}
		if !since.IsZero() {
			q.Set("created_after", since.UTC().Format(time.RFC3339))
		}
		for _, id := range productIDs {
			q.Add("product_id", id)
		}
		var body struct {
			Items      []RawOrder `json:"items"`
			Pagination struct {
				MaxPage int `json:"max_page"`
			} `json:"pagination"`
		}
		if err := c.get(ctx, token, strings.TrimRight(server, "/")+"/v1/orders/?"+q.Encode(), &body); err != nil {
			return nil, err
		}
		out = append(out, body.Items...)
		if len(body.Items) < pageSize || page >= body.Pagination.MaxPage {
			return out, nil
		}
	}
}

// Order fetches one order by id.
func (c *Client) Order(ctx context.Context, server, token, id string) (RawOrder, error) {
	var o RawOrder
	err := c.get(ctx, token, strings.TrimRight(server, "/")+"/v1/orders/"+url.PathEscape(id), &o)
	return o, err
}

// Check verifies a token can read orders.
func (c *Client) Check(ctx context.Context, server, token string) error {
	var body struct{}
	return c.get(ctx, token, strings.TrimRight(server, "/")+"/v1/orders/?limit=1", &body)
}

func (c *Client) get(ctx context.Context, token, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("polar returned %d: %s", res.StatusCode, polarError(body))
	}
	return json.Unmarshal(body, out)
}

func polarError(body []byte) string {
	var e struct {
		Error  string `json:"error"`
		Detail any    `json:"detail"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.Error != "" {
			return e.Error
		}
		if d, ok := e.Detail.(string); ok && d != "" {
			return d
		}
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// ---- field access on raw orders ----

func (o RawOrder) str(path ...string) string {
	v := o.walk(path...)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (o RawOrder) num(path ...string) (int, bool) {
	switch v := o.walk(path...).(type) {
	case float64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	}
	return 0, false
}

func (o RawOrder) boolean(path ...string) bool {
	b, _ := o.walk(path...).(bool)
	return b
}

func (o RawOrder) walk(path ...string) any {
	var cur any = map[string]any(o)
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

// ProductIDs lists every product id an order names.
func (o RawOrder) ProductIDs() []string {
	var out []string
	if id := o.str("product_id"); id != "" {
		out = append(out, id)
	}
	if id := o.str("product", "id"); id != "" {
		out = append(out, id)
	}
	if items, ok := o.walk("items").([]any); ok {
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				if id, _ := m["product_id"].(string); id != "" {
					out = append(out, id)
				}
			}
		}
	}
	return out
}
