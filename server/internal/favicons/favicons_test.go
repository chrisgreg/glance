package favicons

import (
	"net"
	"net/url"
	"testing"
)

func TestAllowed(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "172.16.0.5", "169.254.169.254", "0.0.0.0", "::1", "fe80::1", "fc00::1", "100.64.0.1"}
	for _, s := range blocked {
		if Allowed(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	for _, s := range []string{"93.184.216.34", "1.1.1.1", "2606:4700:4700::1111"} {
		if !Allowed(net.ParseIP(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func TestParseIconLinks(t *testing.T) {
	base, _ := url.Parse("https://example.com/blog/post")
	html := `<html><head>
	<link rel="stylesheet" href="/a.css">
	<link rel="shortcut icon" href="/favicon.ico">
	<link rel='icon' type='image/png' sizes='32x32' href='/icons/32.png'>
	<LINK REL="apple-touch-icon" HREF="//cdn.example.com/apple.png">
	<link rel="icon" href="javascript:alert(1)">
	</head></html>`
	got := ParseIconLinks(base, html)
	want := []string{"https://example.com/icons/32.png", "https://example.com/favicon.ico", "https://cdn.example.com/apple.png", "https://example.com/favicon.ico"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if got := ParseIconLinks(base, "<html></html>"); len(got) != 1 || got[0] != "https://example.com/favicon.ico" {
		t.Fatalf("fallback: %v", got)
	}
}
