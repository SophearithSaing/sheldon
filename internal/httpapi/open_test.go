package httpapi

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestOpenIntentValidationAndState(t *testing.T) {
	s, client := newTestServer(t, "")
	for _, tt := range []struct {
		name, body, media, token string
		status                   int
	}{
		{"unauthenticated", `{"url":"https://github.com/owner/repo/pull/1"}`, "application/json", "", 401},
		{"form", "url=anything", "application/x-www-form-urlencoded", s.APIToken(), 415},
		{"malformed", "{", "application/json", s.APIToken(), 400},
		{"unknown field", `{"url":"anything","extra":1}`, "application/json", s.APIToken(), 400},
		{"trailing JSON", `{"url":"anything"} {}`, "application/json", s.APIToken(), 400},
		{"invalid PR", `{"url":"https://attacker.example/owner/repo/pull/1"}`, "application/json", s.APIToken(), 400},
		{"oversized", `{"url":"` + strings.Repeat("x", 5000) + `"}`, "application/json", s.APIToken(), 413},
		{"oversized trailing", `{"url":"anything"}` + strings.Repeat(" ", 5000), "application/json", s.APIToken(), 413},
		{"accepted", `{"url":"https://github.com/owner/repo/pull/1#discussion"}`, "application/json", s.APIToken(), 202},
	} {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{"Content-Type": tt.media}
			if tt.token != "" {
				headers["Authorization"] = "Bearer " + tt.token
			}
			r := request(t, s, client, "POST", "/api/open", tt.body, headers)
			if r.StatusCode != tt.status {
				b, _ := io.ReadAll(r.Body)
				t.Fatalf("status %d, want %d: %s", r.StatusCode, tt.status, b)
			}
		})
	}
	r := request(t, s, client, "GET", "/api/open", "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
	var state map[string]string
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state["url"] != "https://github.com/owner/repo/pull/1" {
		t.Fatalf("state %v", state)
	}
	if err := s.SetOpenURL("https://github.com/owner/repo/pull/2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOpenURL("invalid"); err == nil {
		t.Fatal("invalid intent accepted")
	}
	r = request(t, s, client, "GET", "/api/open", "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
	json.NewDecoder(r.Body).Decode(&state)
	if state["url"] != "https://github.com/owner/repo/pull/2" {
		t.Fatal("invalid intent overwrote latest request")
	}
}
