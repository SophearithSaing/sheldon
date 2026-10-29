package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestHostOriginAndFetchMetadata(t *testing.T) {
	s, client := newTestServer(t, "")
	for _, tt := range []struct {
		name    string
		headers map[string]string
		status  int
	}{
		{"valid bearer", nil, http.StatusOK},
		{"same origin", map[string]string{"Origin": s.URL(), "Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"foreign host", map[string]string{"Host": "attacker.example"}, http.StatusForbidden},
		{"DNS rebinding", map[string]string{"Host": "attacker.example:" + strconv.Itoa(s.listener.Addr().(*net.TCPAddr).Port)}, http.StatusForbidden},
		{"host suffix", map[string]string{"Host": s.authority + ".attacker.example"}, http.StatusForbidden},
		{"foreign origin", map[string]string{"Origin": "https://attacker.example"}, http.StatusForbidden},
		{"null origin", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"origin prefix", map[string]string{"Origin": s.URL() + ".attacker.example"}, http.StatusForbidden},
		{"origin trailing slash", map[string]string{"Origin": s.URL() + "/"}, http.StatusForbidden},
		{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"other local origin", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"forwarded host cannot bypass", map[string]string{"Host": "attacker.example", "X-Forwarded-Host": s.authority, "X-Forwarded-For": "127.0.0.1"}, http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{"Authorization": "Bearer " + s.APIToken()}
			for k, v := range tt.headers {
				headers[k] = v
			}
			r := request(t, s, client, "GET", "/api/health", "", headers)
			if r.StatusCode != tt.status {
				t.Fatalf("status=%d, want %d", r.StatusCode, tt.status)
			}
			if r.Header.Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("unexpected CORS allowance")
			}
			if r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("X-Content-Type-Options") != "nosniff" || r.Header.Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("missing response protections")
			}
		})
	}
}

func TestRejectNonLoopbackPeerAndProxyRequest(t *testing.T) {
	s, _ := newTestServer(t, "")
	for _, remote := range []string{"192.0.2.1:1234", "malformed"} {
		r := httptest.NewRequest("GET", "/api/health", nil)
		r.Host = s.authority
		r.RemoteAddr = remote
		r.Header.Set("Authorization", "Bearer "+s.APIToken())
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		s.handle(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatal("forwarded metadata overrode a non-loopback peer")
		}
	}
	r := httptest.NewRequest("GET", s.URL()+"/api/health", nil)
	r.Host = s.authority
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer "+s.APIToken())
	w := httptest.NewRecorder()
	s.handle(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("absolute proxy-form request accepted")
	}
}

func TestMutationsRequireBearerAndRejectForeignOrigin(t *testing.T) {
	s, client := newTestServer(t, "")
	// A fixture route exercises middleware without adding a fake product action.
	s.mux.HandleFunc("POST /api/test", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	token := s.APIToken()
	for _, tt := range []struct {
		name, token, origin string
		status              int
	}{
		{"missing bearer", "", s.URL(), http.StatusUnauthorized},
		{"invalid bearer", "invalid", s.URL(), http.StatusUnauthorized},
		{"non-browser request", token, "", http.StatusNoContent},
		{"wrong origin", token, "https://attacker.example", http.StatusForbidden},
		{"valid", token, s.URL(), http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{"Origin": tt.origin}
			if tt.token != "" {
				headers["Authorization"] = "Bearer " + tt.token
			}
			r := request(t, s, client, "POST", "/api/test", "", headers)
			if r.StatusCode != tt.status {
				t.Fatalf("status=%d, want %d", r.StatusCode, tt.status)
			}
		})
	}
}

func TestNoPermissivePreflight(t *testing.T) {
	s, client := newTestServer(t, "")
	r := request(t, s, client, "OPTIONS", "/api/health", "", map[string]string{"Origin": "https://attacker.example", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "Authorization"})
	if r.StatusCode != http.StatusForbidden || r.Header.Get("Access-Control-Allow-Origin") != "" || r.Header.Get("Access-Control-Allow-Headers") != "" {
		t.Fatal("cross-origin preflight was allowed")
	}
}

func TestRejectDuplicateSecurityHeaders(t *testing.T) {
	s, client := newTestServer(t, "")
	token := s.APIToken()
	for _, tt := range []struct {
		name, method, path, header, value string
		status                            int
	}{
		{"origin", "GET", "/api/health", "Origin", s.URL(), http.StatusForbidden},
		{"authorization", "GET", "/api/health", "Authorization", "Bearer " + s.APIToken(), http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := http.NewRequest(tt.method, s.URL()+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Origin", s.URL())
			r.Header.Set(tt.header, tt.value)
			r.Header.Add(tt.header, tt.value)
			response, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tt.status {
				t.Fatalf("status=%d, want %d", response.StatusCode, tt.status)
			}
		})
	}
}
