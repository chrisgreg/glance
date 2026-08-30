package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSettingsAndTokens(t *testing.T) {
	s := newServer(t, "chris", "correct-horse")
	h := s.Handler()
	admin := func(method, path string, body any) *httptestRecorder {
		return doAs(t, h, method, path, body, "chris", "correct-horse")
	}

	// Theme is public; settings are not.
	if rr := do(t, h, "GET", "/api/v1/theme", nil, nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "#7C83E8") {
		t.Fatalf("theme: %d %s", rr.Code, rr.Body)
	}
	if rr := do(t, h, "GET", "/api/v1/settings", nil, nil); rr.Code != 401 {
		t.Fatalf("settings without login: %d", rr.Code)
	}
	rr := admin("PATCH", "/api/v1/settings", map[string]any{"accent": "#5fbf9f", "title": "Acme", "retention_days": 14})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"accent":"#5FBF9F"`) || !strings.Contains(rr.Body.String(), `"retention_days":14`) {
		t.Fatalf("patch: %d %s", rr.Code, rr.Body)
	}
	if rr := admin("PATCH", "/api/v1/settings", map[string]any{"accent": "red"}); rr.Code != 422 {
		t.Fatalf("bad accent: %d", rr.Code)
	}
	if rr := do(t, h, "GET", "/api/v1/theme", nil, nil); !strings.Contains(rr.Body.String(), "Acme") {
		t.Fatalf("theme not updated: %s", rr.Body)
	}

	// Mint a token: usable for GET and MCP, refused for writes, gone after revoke.
	rr = admin("POST", "/api/v1/tokens", map[string]any{"name": "Claude"})
	if rr.Code != 201 {
		t.Fatalf("mint: %d %s", rr.Code, rr.Body)
	}
	var minted struct {
		Token  struct{ ID, Prefix string }
		Secret string
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &minted)
	if !strings.HasPrefix(minted.Secret, "glance_tok_") || !strings.HasPrefix(minted.Secret, minted.Token.Prefix) {
		t.Fatalf("secret: %+v", minted)
	}
	bearer := map[string]string{"Authorization": "Bearer " + minted.Secret}
	if rr := do(t, h, "GET", "/api/v1/sites", nil, bearer); rr.Code != 200 {
		t.Fatalf("token GET: %d %s", rr.Code, rr.Body)
	}
	if rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"domain": "x.example"}, bearer); rr.Code != 403 {
		t.Fatalf("token POST should be read-only: %d", rr.Code)
	}
	if rr := do(t, h, "POST", "/mcp", `{}`, bearer); rr.Code == 401 {
		t.Fatalf("token should open /mcp: %d %s", rr.Code, rr.Body)
	}
	if rr := admin("GET", "/api/v1/tokens", nil); !strings.Contains(rr.Body.String(), `"name":"Claude"`) || strings.Contains(rr.Body.String(), minted.Secret) {
		t.Fatalf("list must show the token without its secret: %s", rr.Body)
	}
	// MCP switch.
	if rr := admin("PATCH", "/api/v1/settings", map[string]any{"mcp_enabled": false}); rr.Code != 200 {
		t.Fatalf("disable mcp: %d", rr.Code)
	}
	if rr := do(t, h, "POST", "/mcp", `{}`, bearer); rr.Code != 404 {
		t.Fatalf("mcp should be off: %d", rr.Code)
	}
	admin("PATCH", "/api/v1/settings", map[string]any{"mcp_enabled": true})
	if rr := admin("DELETE", "/api/v1/tokens/"+minted.Token.ID, nil); rr.Code != 204 {
		t.Fatalf("revoke: %d", rr.Code)
	}
	if rr := do(t, h, "GET", "/api/v1/sites", nil, bearer); rr.Code != 401 {
		t.Fatalf("revoked token still works: %d", rr.Code)
	}
}
