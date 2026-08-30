package enrich

import (
	"net/http"
	"testing"
)

func TestParseUA(t *testing.T) {
	cases := []struct {
		ua                  string
		width               int
		browser, os, device string
		bot                 bool
	}{
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", 1440, "Chrome", "macOS", "Desktop", false},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", 1440, "Safari", "macOS", "Desktop", false},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0", 1920, "Edge", "Windows", "Desktop", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", 390, "Safari", "iOS", "Mobile", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/126.0.0.0 Mobile/15E148 Safari/604.1", 390, "Chrome", "iOS", "Mobile", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/127.0 Mobile/15E148 Safari/605.1.15", 390, "Firefox", "iOS", "Mobile", false},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36", 412, "Chrome", "Android", "Mobile", false},
		{"Mozilla/5.0 (Linux; Android 14; SM-X910) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", 1280, "Chrome", "Android", "Tablet", false},
		{"Mozilla/5.0 (Linux; Android 13; SM-S911B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/24.0 Chrome/117.0.0.0 Mobile Safari/537.36", 360, "Samsung Internet", "Android", "Mobile", false},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0", 1920, "Firefox", "Linux", "Desktop", false},
		{"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", 1366, "Chrome", "ChromeOS", "Desktop", false},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 OPR/112.0.0.0", 1920, "Opera", "Windows", "Desktop", false},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", 0, "Other", "Other", "Desktop", true},
		{"curl/8.4.0", 0, "Other", "Other", "Desktop", true},
	}
	for _, c := range cases {
		got := ParseUA(c.ua, c.width)
		if got.Browser != c.browser || got.OS != c.os || got.Device != c.device || got.Bot != c.bot {
			t.Errorf("%s\n  want %s/%s/%s bot=%v\n  got  %s/%s/%s bot=%v", c.ua, c.browser, c.os, c.device, c.bot, got.Browser, got.OS, got.Device, got.Bot)
		}
	}
}

func TestCountryAndReferrer(t *testing.T) {
	h := http.Header{}
	if Country(h, "Europe/London") != "GB" || Country(h, "America/New_York") != "US" || Country(h, "Asia/Kolkata") != "IN" || Country(h, "Nope/Where") != "" {
		t.Fatal("timezone mapping")
	}
	h.Set("CF-IPCountry", "de")
	if Country(h, "Europe/London") != "DE" {
		t.Fatal("header should win")
	}
	if Referrer("https://www.google.com/search?q=x", "example.com") != "google.com" {
		t.Fatal("google")
	}
	if Referrer("https://t.co/abc", "example.com") != "x.com" {
		t.Fatal("alias")
	}
	if Referrer("https://blog.example.com/post", "example.com") != "" || Referrer("", "example.com") != "" {
		t.Fatal("same-site or empty should be direct")
	}
	p, host := Path("https://Example.com/docs/intro/?utm=1#top")
	if p != "/docs/intro" || host != "example.com" {
		t.Fatalf("path: %q %q", p, host)
	}
	for _, h := range []string{"localhost", "127.0.0.1", "app.localhost", "mymac.local", "192.168.1.20", ""} {
		if !LocalHost(h) {
			t.Fatalf("%q should count as local", h)
		}
	}
	if LocalHost("example.com") || LocalHost("localhost.evil.com") {
		t.Fatal("public hosts are not local")
	}
	if Region("America/New_York") != "New York" || Region("Europe/London") != "London" || Region("UTC") != "" || Region("Etc/GMT+1") != "" {
		t.Fatal("region")
	}
	if src, camp := UTM("https://example.com/?utm_source=Newsletter&utm_campaign=launch"); src != "newsletter" || camp != "launch" {
		t.Fatalf("utm: %q %q", src, camp)
	}
	if src, _ := UTM("https://example.com/?ref=producthunt"); src != "producthunt" {
		t.Fatalf("ref fallback: %q", src)
	}
	if p, _ := Path("https://example.com/"); p != "/" {
		t.Fatalf("root path: %q", p)
	}
}
