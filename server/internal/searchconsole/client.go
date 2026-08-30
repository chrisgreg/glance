package searchconsole

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Scope grants read-only Search Console access; openid+email identify the
// Google account so the settings page can say who is connected.
const Scope = "https://www.googleapis.com/auth/webmasters.readonly openid email"

// Client talks to Google's OAuth and Search Console endpoints with plain
// net/http. The URLs are fields so tests can point them at a fake.
type Client struct {
	ClientID     string
	ClientSecret string
	HTTP         *http.Client

	AuthURL   string // https://accounts.google.com/o/oauth2/v2/auth
	TokenURL  string // https://oauth2.googleapis.com/token
	RevokeURL string // https://oauth2.googleapis.com/revoke
	APIURL    string // https://searchconsole.googleapis.com
}

// NewClient returns a Client against Google's real endpoints.
func NewClient(id, secret string) *Client {
	return &Client{
		ClientID: id, ClientSecret: secret,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:  "https://oauth2.googleapis.com/token",
		RevokeURL: "https://oauth2.googleapis.com/revoke",
		APIURL:    "https://searchconsole.googleapis.com",
	}
}

// Configured reports whether an OAuth client is available.
func (c *Client) Configured() bool { return c != nil && c.ClientID != "" && c.ClientSecret != "" }

// ConsentURL is where the browser goes to approve access. access_type=offline
// and prompt=consent make Google return a refresh token every time, not only
// on the first approval.
func (c *Client) ConsentURL(redirectURI, state string) string {
	q := url.Values{
		"client_id":     {c.ClientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {Scope},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return c.AuthURL + "?" + q.Encode()
}

// Grant is what a code exchange yields.
type Grant struct {
	AccessToken  string
	RefreshToken string
	Email        string
}

// Exchange swaps an authorisation code for tokens.
func (c *Client) Exchange(ctx context.Context, code, redirectURI string) (Grant, error) {
	form := url.Values{
		"code": {code}, "client_id": {c.ClientID}, "client_secret": {c.ClientSecret},
		"redirect_uri": {redirectURI}, "grant_type": {"authorization_code"},
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := c.postForm(ctx, c.TokenURL, form, &body); err != nil {
		return Grant{}, err
	}
	if body.RefreshToken == "" {
		return Grant{}, errors.New("google did not return a refresh token; remove Glance from your Google account's third-party access and connect again")
	}
	return Grant{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, Email: emailFromIDToken(body.IDToken)}, nil
}

// AccessToken trades a refresh token for a short-lived access token.
func (c *Client) AccessToken(ctx context.Context, refreshToken string) (string, error) {
	form := url.Values{
		"refresh_token": {refreshToken}, "client_id": {c.ClientID}, "client_secret": {c.ClientSecret},
		"grant_type": {"refresh_token"},
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.postForm(ctx, c.TokenURL, form, &body); err != nil {
		if strings.Contains(err.Error(), "invalid_grant") {
			return "", ErrReconnect
		}
		return "", err
	}
	return body.AccessToken, nil
}

// Revoke tells Google to forget the grant. Best effort.
func (c *Client) Revoke(ctx context.Context, refreshToken string) {
	_ = c.postForm(ctx, c.RevokeURL, url.Values{"token": {refreshToken}}, nil)
}

// Properties lists the Search Console properties the account can read.
func (c *Client) Properties(ctx context.Context, accessToken string) ([]string, error) {
	var body struct {
		SiteEntry []struct {
			SiteURL         string `json:"siteUrl"`
			PermissionLevel string `json:"permissionLevel"`
		} `json:"siteEntry"`
	}
	if err := c.get(ctx, accessToken, c.APIURL+"/webmasters/v3/sites", &body); err != nil {
		return nil, err
	}
	var out []string
	for _, e := range body.SiteEntry {
		if e.PermissionLevel != "siteUnverifiedUser" {
			out = append(out, e.SiteURL)
		}
	}
	return out, nil
}

// maxRows is Search Console's per-request ceiling.
const maxRows = 25000

// Query pulls every (day, query) row between two days inclusive, paging
// until Google runs out.
func (c *Client) Query(ctx context.Context, accessToken, property, fromDay, toDay string) ([]DayTerm, error) {
	endpoint := c.APIURL + "/webmasters/v3/sites/" + url.PathEscape(property) + "/searchAnalytics/query"
	var out []DayTerm
	for start := 0; ; start += maxRows {
		req := map[string]any{
			"startDate": fromDay, "endDate": toDay,
			"dimensions": []string{"date", "query"},
			"rowLimit":   maxRows, "startRow": start,
			"dataState": "final",
		}
		var body struct {
			Rows []struct {
				Keys        []string `json:"keys"`
				Clicks      float64  `json:"clicks"`
				Impressions float64  `json:"impressions"`
				Position    float64  `json:"position"`
			} `json:"rows"`
		}
		if err := c.postJSON(ctx, accessToken, endpoint, req, &body); err != nil {
			return nil, err
		}
		for _, r := range body.Rows {
			if len(r.Keys) != 2 {
				continue
			}
			out = append(out, DayTerm{Day: r.Keys[0], Query: r.Keys[1], Clicks: int(r.Clicks), Impressions: int(r.Impressions), Position: r.Position})
		}
		if len(body.Rows) < maxRows {
			return out, nil
		}
	}
}

// ---- transport ----

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, out)
}

func (c *Client) postJSON(ctx context.Context, token, endpoint string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return c.do(req, out)
}

func (c *Client) get(ctx context.Context, token, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
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
		return fmt.Errorf("google returned %d: %s", res.StatusCode, googleError(body))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

// googleError pulls the readable part out of Google's error envelopes.
func googleError(body []byte) string {
	var e struct {
		Error any    `json:"error"`
		Desc  string `json:"error_description"`
	}
	if json.Unmarshal(body, &e) == nil {
		switch v := e.Error.(type) {
		case string:
			if e.Desc != "" {
				return v + ": " + e.Desc
			}
			return v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				return m
			}
		}
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// emailFromIDToken reads the email claim from an ID token. The token came
// straight from Google over TLS in the code exchange, so its signature need
// not be checked here.
func emailFromIDToken(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(payload, &claims)
	return claims.Email
}
