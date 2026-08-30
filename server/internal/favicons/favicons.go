// Package favicons fetches and caches site icons without any third-party
// service. Every outbound request goes through an SSRF guard that refuses
// private, loopback and link-local addresses, including after redirects.
package favicons

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

const (
	maxHTML  = 256 << 10
	maxIcon  = 64 << 10
	timeout  = 4 * time.Second
	negative = 7 * 24 * time.Hour // how long a failed lookup is remembered
)

// ErrBlocked is returned for addresses the guard refuses.
var ErrBlocked = errors.New("address not allowed")

// Fetcher fetches icons.
type Fetcher struct {
	client *http.Client
	db     *sql.DB
	now    func() time.Time
}

// New returns a Fetcher with the SSRF-guarded transport.
func New(db *sql.DB) *Fetcher {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ipList, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ipList {
				if !allowed(ip.IP) {
					return nil, fmt.Errorf("%w: %s resolves to %s", ErrBlocked, host, ip.IP)
				}
			}
			// Dial the checked address, not the name, so DNS cannot swap it.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ipList[0].IP.String(), port))
		},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &Fetcher{db: db, now: time.Now, client: &http.Client{
		Transport: transport,
		Timeout:   timeout * 2,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return ErrBlocked
			}
			return nil
		},
	}}
}

// allowed reports whether an IP is a public unicast address.
func allowed(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// Carrier-grade NAT and other reserved ranges.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
		if v4[0] == 0 || v4[0] >= 240 {
			return false
		}
	}
	return true
}

// Allowed is exported for tests and for checking a host before fetching.
func Allowed(ip net.IP) bool { return allowed(ip) }

var linkRe = regexp.MustCompile(`(?is)<link\b[^>]*>`)
var attrRe = regexp.MustCompile(`(?is)([a-z-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)

// ParseIconLinks returns candidate icon URLs from an HTML head, best first.
func ParseIconLinks(base *url.URL, html string) []string {
	var apple, icons []string
	for _, tag := range linkRe.FindAllString(html, -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			v := m[2] + m[3] + m[4]
			attrs[strings.ToLower(m[1])] = v
		}
		rel := strings.ToLower(attrs["rel"])
		href := strings.TrimSpace(attrs["href"])
		if href == "" {
			continue
		}
		u, err := base.Parse(href)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		switch {
		case strings.Contains(rel, "apple-touch-icon"):
			apple = append(apple, u.String())
		case strings.Contains(rel, "icon"):
			// Prefer PNG/SVG over ICO when several are listed.
			if strings.Contains(strings.ToLower(href), ".ico") {
				icons = append(icons, u.String())
			} else {
				icons = append([]string{u.String()}, icons...)
			}
		}
	}
	out := append(icons, apple...)
	out = append(out, base.ResolveReference(&url.URL{Path: "/favicon.ico"}).String())
	return out
}

func (f *Fetcher) get(ctx context.Context, rawURL string, limit int64) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout*2)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Glance/1.0 (+https://github.com/chrisgreg/glance; favicon fetcher)")
	req.Header.Set("Accept", "image/*,text/html;q=0.8,*/*;q=0.5")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > limit {
		return nil, "", errors.New("too large")
	}
	return b, resp.Header.Get("Content-Type"), nil
}

// ForDomain finds the best icon for a domain: the page's declared icons,
// then /favicon.ico. Returns the bytes and content type.
func (f *Fetcher) ForDomain(ctx context.Context, domain string) ([]byte, string, error) {
	base, err := url.Parse("https://" + domain + "/")
	if err != nil {
		return nil, "", err
	}
	candidates := []string{base.ResolveReference(&url.URL{Path: "/favicon.ico"}).String()}
	if html, ctype, err := f.get(ctx, base.String(), maxHTML); err == nil && strings.Contains(ctype, "html") {
		candidates = ParseIconLinks(base, string(html))
	}
	var lastErr error = errors.New("no icon found")
	for _, c := range candidates {
		b, ctype, err := f.get(ctx, c, maxIcon)
		if err != nil {
			lastErr = err
			continue
		}
		if !isImage(ctype, b) {
			lastErr = errors.New("not an image")
			continue
		}
		return b, imageType(ctype, b), nil
	}
	return nil, "", lastErr
}

func isImage(ctype string, b []byte) bool {
	if strings.HasPrefix(ctype, "image/") {
		return true
	}
	return imageType("", b) != ""
}

func imageType(ctype string, b []byte) string {
	switch {
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(b) >= 4 && string(b[:4]) == "\x00\x00\x01\x00":
		return "image/x-icon"
	case len(b) >= 3 && string(b[:3]) == "\xff\xd8\xff":
		return "image/jpeg"
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	case strings.Contains(strings.ToLower(string(b[:min(len(b), 512)])), "<svg"):
		return "image/svg+xml"
	}
	if strings.HasPrefix(ctype, "image/") {
		if i := strings.Index(ctype, ";"); i > 0 {
			ctype = ctype[:i]
		}
		return ctype
	}
	return ""
}

// Cached returns a referrer host's icon, fetching and caching it on first
// use. Failures are cached too so a dead host is not hammered.
func (f *Fetcher) Cached(ctx context.Context, host string) ([]byte, string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || strings.ContainsAny(host, "/:?#@ ") {
		return nil, "", errors.New("bad host")
	}
	var data []byte
	var ctype, fetchedAt string
	var failed int
	err := f.db.QueryRowContext(ctx, `SELECT COALESCE(data, X''), type, fetched_at, failed FROM favicons WHERE host = ?`, host).Scan(&data, &ctype, &fetchedAt, &failed)
	if err == nil {
		at, _ := ids.Parse(fetchedAt)
		fresh := f.now().Sub(at) < negative
		if failed == 0 && len(data) > 0 {
			return data, ctype, nil
		}
		if fresh {
			return nil, "", errors.New("no icon")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, "", err
	}
	data, ctype, ferr := f.ForDomain(ctx, host)
	failedInt := 0
	if ferr != nil {
		failedInt, data, ctype = 1, nil, ""
	}
	var blob any
	if len(data) > 0 {
		blob = data
	}
	_, _ = f.db.ExecContext(ctx, `INSERT INTO favicons (host, data, type, fetched_at, failed) VALUES (?,?,?,?,?)
		ON CONFLICT(host) DO UPDATE SET data=excluded.data, type=excluded.type, fetched_at=excluded.fetched_at, failed=excluded.failed`,
		host, blob, ctype, ids.Format(f.now()), failedInt)
	if ferr != nil {
		return nil, "", ferr
	}
	return data, ctype, nil
}
