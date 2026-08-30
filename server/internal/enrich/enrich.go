// Package enrich derives country, device, browser, OS and referrer from a
// request without any external database or service.
package enrich

import (
	"net/http"
	"net/url"
	"strings"
)

// Country resolves a country code: a proxy/CDN header first, then the
// visitor's IANA time zone. Returns "" when unknown.
func Country(h http.Header, tz string) string {
	for _, k := range []string{"CF-IPCountry", "X-Vercel-IP-Country", "X-Country-Code", "X-Geo-Country", "CloudFront-Viewer-Country"} {
		if v := strings.ToUpper(strings.TrimSpace(h.Get(k))); len(v) == 2 && v != "XX" && v != "T1" {
			return v
		}
	}
	if cc, ok := tzCountry[strings.TrimSpace(tz)]; ok {
		return cc
	}
	return ""
}

// Region derives a coarse place name from an IANA time zone: the last path
// segment with underscores as spaces ("America/New_York" -> "New York").
// It is the best location signal available without a GeoIP database.
func Region(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" || !strings.Contains(tz, "/") {
		return ""
	}
	seg := tz[strings.LastIndex(tz, "/")+1:]
	if seg == "" || strings.HasPrefix(seg, "GMT") || strings.HasPrefix(seg, "UTC") || strings.HasPrefix(seg, "Etc") {
		return ""
	}
	return strings.ReplaceAll(seg, "_", " ")
}

// UTM extracts campaign tags from a page URL. source falls back to the
// common ?ref= and ?source= conventions.
func UTM(raw string) (source, campaign string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ""
	}
	q := u.Query()
	source = strings.TrimSpace(q.Get("utm_source"))
	if source == "" {
		source = strings.TrimSpace(q.Get("ref"))
	}
	if source == "" {
		source = strings.TrimSpace(q.Get("source"))
	}
	campaign = strings.TrimSpace(q.Get("utm_campaign"))
	if len(source) > 80 {
		source = source[:80]
	}
	if len(campaign) > 80 {
		campaign = campaign[:80]
	}
	return strings.ToLower(source), campaign
}

// ReferrerAliases turns well-known hosts into the names people expect.
var ReferrerAliases = map[string]string{
	"t.co": "x.com", "twitter.com": "x.com", "l.facebook.com": "facebook.com", "lm.facebook.com": "facebook.com",
	"m.facebook.com": "facebook.com", "com.google.android.gm": "gmail.com", "android-app://com.google.android.googlequicksearchbox": "google.com",
	"www.google.com": "google.com", "www.bing.com": "bing.com", "duckduckgo.com": "duckduckgo.com", "com.linkedin.android": "linkedin.com",
	"www.linkedin.com": "linkedin.com", "old.reddit.com": "reddit.com", "www.reddit.com": "reddit.com", "out.reddit.com": "reddit.com",
	"news.ycombinator.com": "news.ycombinator.com", "www.youtube.com": "youtube.com", "m.youtube.com": "youtube.com",
}

// Referrer returns the referrer host, or "" for direct or same-site traffic.
func Referrer(ref, siteDomain string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if a, ok := ReferrerAliases[host]; ok {
		host = a
	}
	host = strings.TrimPrefix(host, "www.")
	if SameSite(host, siteDomain) {
		return ""
	}
	return host
}

// LocalHost reports whether host is a local development address, which is
// accepted for every site so the snippet can be tested before deploying.
func LocalHost(host string) bool {
	host = strings.ToLower(host)
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".test") ||
		strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.")
}

// SameSite reports whether host is the site domain or one of its subdomains.
func SameSite(host, domain string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	domain = strings.ToLower(strings.TrimPrefix(domain, "www."))
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// Path extracts a normalised path from a page URL: no query, no fragment,
// trailing slash trimmed, capped in length.
func Path(raw string) (path, host string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "/", ""
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}
	if len(p) > 200 {
		p = p[:200]
	}
	return p, strings.ToLower(u.Hostname())
}
