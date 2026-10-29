package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestShellSecurityAndNoAPIFallback(t *testing.T) {
	s, client := newTestServer(t, "")
	for _, path := range []string{"/", "/index.html", "/assets/missing.js"} {
		for _, headers := range []map[string]string{
			{"Host": "attacker.example"},
			{"Origin": "https://attacker.example"},
			{"Origin": "http://127.0.0.1:5173"},
			{"Sec-Fetch-Site": "cross-site"},
		} {
			r := request(t, s, client, "GET", path, "", headers)
			if r.StatusCode != http.StatusForbidden {
				t.Fatalf("unprotected shell path %s", path)
			}
		}
	}
	for _, path := range []string{"/api/unknown", "/api", "/assets/missing.js", "/src/main.ts", "/instance.lock", "/assets/../index.html", "/%2e%2e/index.html", "//index.html", "/assets/%5cindex.html"} {
		r := request(t, s, client, "GET", path, "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
		body, _ := io.ReadAll(r.Body)
		if r.StatusCode != http.StatusNotFound || strings.Contains(string(body), "<!doctype") || r.Header.Get("Location") != "" {
			t.Fatalf("fallback or redirect for %s: %d", path, r.StatusCode)
		}
	}
	r := request(t, s, client, "HEAD", "/", "", nil)
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || len(body) != 0 || r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("unsafe shell HEAD response")
	}
	r = request(t, s, client, "POST", "/", "", nil)
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal("shell accepted a mutation")
	}
}
