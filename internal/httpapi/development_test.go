package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

func TestDevelopmentOriginIsExplicitAndExact(t *testing.T) {
	const development = "http://127.0.0.1:5173"
	s, err := Listen("", development)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	s.mux.HandleFunc("POST /api/test", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, tt := range []struct {
		origin, token string
		status        int
	}{
		{development, s.APIToken(), http.StatusNoContent},
		{development, "", http.StatusUnauthorized},
		{"http://127.0.0.1:5174", s.APIToken(), http.StatusForbidden},
		{"http://localhost:5173", s.APIToken(), http.StatusForbidden},
		{development + "/", s.APIToken(), http.StatusForbidden},
		{"null", s.APIToken(), http.StatusForbidden},
	} {
		headers := map[string]string{"Origin": tt.origin, "Sec-Fetch-Site": "same-origin"}
		if tt.token != "" {
			headers["Authorization"] = "Bearer " + tt.token
		}
		r := request(t, s, client, "POST", "/api/test", "", headers)
		if r.StatusCode != tt.status {
			t.Fatalf("origin %q: status %d, want %d", tt.origin, r.StatusCode, tt.status)
		}
		if r.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("development enabled CORS")
		}
	}
	release, releaseClient := newTestServer(t, "")
	r := request(t, release, releaseClient, "GET", "/api/health", "", map[string]string{"Origin": development, "Authorization": "Bearer " + release.APIToken()})
	if r.StatusCode != http.StatusForbidden {
		t.Fatal("release server accepted a development origin")
	}
}

func TestDevelopmentOriginRejectsUnsafeConfiguration(t *testing.T) {
	for _, origin := range []string{"http://localhost:5173", "http://0.0.0.0:5173", "https://127.0.0.1:5173", "http://127.0.0.1:5173/", "http://127.0.0.1:5173?token=x", "http://user@127.0.0.1:5173", "http://192.0.2.1:5173"} {
		if s, err := Listen("", origin); err == nil {
			s.Shutdown(context.Background())
			t.Fatalf("unsafe origin accepted: %s", origin)
		}
	}
}

func TestDevelopmentURLVerifiesTheProxyBeforeExposingToken(t *testing.T) {
	s, _ := newTestServer(t, "")
	target, _ := url.Parse(s.URL())
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) { director(r); r.Host = target.Host }
	frontend := httptest.NewServer(proxy)
	defer frontend.Close()
	c, err := NewClient(s.URL(), s.APIToken())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DevelopmentURL(context.Background(), frontend.URL); err == nil {
		t.Fatal("unverified backend exposed token")
	}
	if err := c.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, err := c.DevelopmentURL(context.Background(), frontend.URL)
	if err != nil || u != frontend.URL+"/#token="+s.APIToken() {
		t.Fatal("verified proxy did not produce a development URL")
	}
	unrelated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("proxy verification leaked bearer token")
		}
		writeJSON(w, http.StatusOK, map[string]string{"proof": "wrong"})
	}))
	defer unrelated.Close()
	if _, err := c.DevelopmentURL(context.Background(), unrelated.URL); err == nil {
		t.Fatal("unrelated listener received launch credential")
	}
	if _, err := c.DevelopmentURL(context.Background(), "https://attacker.example"); err == nil {
		t.Fatal("external development URL accepted")
	}
}
