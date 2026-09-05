package api

import (
	"encoding/json"
	"testing"
)

// Sites carry two preferences of their own: an accent that overrides the
// account-wide colour, and the range their dashboard opens on.
func TestSitePreferences(t *testing.T) {
	s := newServer(t, "chris", "correct-horse")
	h := s.Handler()
	admin := func(method, path string, body any) *httptestRecorder {
		return doAs(t, h, method, path, body, "chris", "correct-horse")
	}

	rr := admin("POST", "/api/v1/sites", map[string]any{"domain": "example.com"})
	if rr.Code != 201 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body)
	}
	var site struct {
		ID           string `json:"id"`
		Accent       string `json:"accent"`
		DefaultRange string `json:"default_range"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &site); err != nil {
		t.Fatal(err)
	}
	if site.Accent != "" || site.DefaultRange != "" {
		t.Fatalf("new site should follow the global settings, got %+v", site)
	}

	// Both are saved, and the accent is normalised like the account-wide one.
	rr = admin("PATCH", "/api/v1/sites/"+site.ID, map[string]any{"accent": "#45b5b0", "default_range": "48h"})
	if err := json.Unmarshal(rr.Body.Bytes(), &site); rr.Code != 200 || err != nil {
		t.Fatalf("patch: %d %s", rr.Code, rr.Body)
	}
	if site.Accent != "#45B5B0" || site.DefaultRange != "48h" {
		t.Fatalf("prefs not stored: %+v", site)
	}
	if rr := admin("GET", "/api/v1/sites/"+site.ID, nil); rr.Code != 200 {
		t.Fatalf("get: %d %s", rr.Code, rr.Body)
	} else if err := json.Unmarshal(rr.Body.Bytes(), &site); err != nil || site.DefaultRange != "48h" {
		t.Fatalf("prefs not read back: %+v", site)
	}

	if rr := admin("PATCH", "/api/v1/sites/"+site.ID, map[string]any{"accent": "reddish"}); rr.Code != 422 {
		t.Fatalf("bad accent: %d %s", rr.Code, rr.Body)
	}
	if rr := admin("PATCH", "/api/v1/sites/"+site.ID, map[string]any{"default_range": "12h"}); rr.Code != 422 {
		t.Fatalf("bad default range: %d %s", rr.Code, rr.Body)
	}

	// A request that names no range gets the site's default, so the dashboard
	// opens on it without asking first.
	rangeOf := func(path string) string {
		rr := admin("GET", path, nil)
		if rr.Code != 200 {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
		var out struct {
			Stats struct {
				Range  string `json:"range"`
				Bucket string `json:"bucket"`
			} `json:"stats"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Stats.Range + "/" + out.Stats.Bucket
	}
	if got := rangeOf("/api/v1/sites/" + site.ID + "/stats"); got != "48h/hour" {
		t.Fatalf("default range not used: %s", got)
	}
	// An explicit range still wins, including the new long one.
	if got := rangeOf("/api/v1/sites/" + site.ID + "/stats?range=180d"); got != "180d/day" {
		t.Fatalf("explicit range: %s", got)
	}
	// Clearing the preference falls back to the server default.
	if rr := admin("PATCH", "/api/v1/sites/"+site.ID, map[string]any{"accent": "", "default_range": ""}); rr.Code != 200 {
		t.Fatalf("clear: %d %s", rr.Code, rr.Body)
	}
	if got := rangeOf("/api/v1/sites/" + site.ID + "/stats"); got != "7d/hour" {
		t.Fatalf("cleared default: %s", got)
	}
	if rr := admin("GET", "/api/v1/sites/"+site.ID+"/stats?range=nope", nil); rr.Code != 400 {
		t.Fatalf("invalid range: %d %s", rr.Code, rr.Body)
	}
}
