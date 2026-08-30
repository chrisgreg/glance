package enrich

import (
	"regexp"
	"strings"
)

// Devices.
const (
	DeviceDesktop = "Desktop"
	DeviceMobile  = "Mobile"
	DeviceTablet  = "Tablet"
)

// UA is what we keep from a user agent string.
type UA struct {
	Browser string
	OS      string
	Device  string
	Bot     bool
}

var botRe = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|headless|lighthouse|pingdom|uptime|monitor|curl/|wget/|python-requests|go-http-client|facebookexternalhit|preview`)

// Ordered: the first match wins, so more specific tokens come first.
var browsers = []struct{ token, name string }{
	{"Edg/", "Edge"}, {"EdgA/", "Edge"}, {"EdgiOS/", "Edge"},
	{"OPR/", "Opera"}, {"Opera", "Opera"},
	{"SamsungBrowser/", "Samsung Internet"},
	{"Vivaldi/", "Vivaldi"}, {"Brave", "Brave"}, {"Arc/", "Arc"},
	{"DuckDuckGo/", "DuckDuckGo"},
	{"FxiOS/", "Firefox"}, {"Firefox/", "Firefox"},
	{"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Chromium/", "Chromium"},
	{"Safari/", "Safari"},
}

// ParseUA extracts browser, OS and device from a user agent. Unknown values
// are "Other".
func ParseUA(ua string, screenWidth int) UA {
	out := UA{Browser: "Other", OS: "Other", Device: DeviceDesktop}
	if ua == "" {
		return out
	}
	if botRe.MatchString(ua) {
		out.Bot = true
	}
	for _, b := range browsers {
		if strings.Contains(ua, b.token) {
			out.Browser = b.name
			break
		}
	}
	// Safari's token appears in every WebKit UA; only count it when nothing
	// more specific matched and it really is Safari.
	if out.Browser == "Safari" && !strings.Contains(ua, "Version/") {
		out.Browser = "Other"
	}
	switch {
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPod"):
		out.OS, out.Device = "iOS", DeviceMobile
	case strings.Contains(ua, "iPad"):
		out.OS, out.Device = "iPadOS", DeviceTablet
	case strings.Contains(ua, "Android"):
		out.OS = "Android"
		if strings.Contains(ua, "Mobile") {
			out.Device = DeviceMobile
		} else {
			out.Device = DeviceTablet
		}
	case strings.Contains(ua, "Windows"):
		out.OS = "Windows"
	case strings.Contains(ua, "CrOS"):
		out.OS = "ChromeOS"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		out.OS = "macOS"
		// iPadOS Safari reports as a Mac; a touch-sized screen gives it away.
		if screenWidth > 0 && screenWidth <= 1024 && strings.Contains(ua, "Safari/") && !strings.Contains(ua, "Chrome/") && screenWidth < 1100 {
			out.OS, out.Device = "iPadOS", DeviceTablet
		}
	case strings.Contains(ua, "Linux"):
		out.OS = "Linux"
	}
	// Screen width refines the device class for anything not already mobile.
	if out.Device == DeviceDesktop && screenWidth > 0 {
		switch {
		case screenWidth < 600:
			out.Device = DeviceMobile
		case screenWidth < 1024 && out.OS != "Windows" && out.OS != "macOS" && out.OS != "Linux" && out.OS != "ChromeOS":
			out.Device = DeviceTablet
		}
	}
	return out
}
