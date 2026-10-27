package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, address string) (*Server, *http.Client) {
	t.Helper()
	s, err := Listen(address)
	if err != nil {
		var network *net.OpError
		if strings.HasPrefix(address, "[::1]") && errors.As(err, &network) {
			t.Skipf("IPv6 loopback unavailable: %v", err)
		}
		t.Fatal(err)
	}
	return s, startTestServer(t, s)
}

func startTestServer(t *testing.T, s *Server) *http.Client {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Serve() }()
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("server did not stop")
		}
	})
	return client
}

func request(t *testing.T, s *Server, client *http.Client, method, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	r, err := http.NewRequest(method, s.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		if value == "" {
			continue
		}
		if key == "Host" {
			r.Host = value
		} else {
			r.Header.Set(key, value)
		}
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestListenOnlyOnNumericLoopback(t *testing.T) {
	for _, address := range []string{":0", "0.0.0.0:0", "[::]:0", "192.0.2.1:0", "localhost:0", "example.com:80", "127.0.0.1", "127.0.0.1:65536", "127.0.0.1:+1"} {
		t.Run(address, func(t *testing.T) {
			s, err := Listen(address)
			if err == nil {
				s.Shutdown(context.Background())
				t.Fatal("accepted unsafe address")
			}
		})
	}
	for _, address := range []string{"", "127.0.0.1:0", "[::1]:0"} {
		t.Run("valid/"+address, func(t *testing.T) {
			s, client := newTestServer(t, address)
			r := request(t, s, client, "GET", "/api/health", "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
			if r.StatusCode != http.StatusOK {
				t.Fatalf("health status %d", r.StatusCode)
			}
		})
	}
}

func TestProtectedAPIRequiresBearer(t *testing.T) {
	s, client := newTestServer(t, "")
	for _, path := range []string{"/api", "/api/health", "/api/fixture/events", "/api/not-found"} {
		r := request(t, s, client, "GET", path, "", nil)
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unprotected %s: %d", path, r.StatusCode)
		}
		if r.Header.Get("WWW-Authenticate") != "Bearer" {
			t.Fatal("missing authentication challenge")
		}
	}
	r := request(t, s, client, "GET", "/api/health?token="+s.APIToken(), "", nil)
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatal("query parameter authenticated a request")
	}
	token := s.APIToken()
	r = request(t, s, client, "GET", "/api/health", "", map[string]string{"Authorization": "Bearer " + token})
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusOK || body["status"] != "ok" || len(body) != 1 || len(r.Cookies()) != 0 {
		t.Fatal("API bearer credential did not authenticate")
	}
	r = request(t, s, client, "GET", "/api/health", "", map[string]string{"Cookie": "sheldon_session=" + token, "Authorization": "Bearer invalid"})
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatal("invalid bearer silently fell back to a cookie")
	}
	r = request(t, s, client, "GET", "/api/health", "", map[string]string{"Cookie": "sheldon_session=" + token})
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatal("ambient cookie authenticated a request")
	}
}

func TestRemovedAuthenticationRoutes(t *testing.T) {
	s, client := newTestServer(t, "")
	for _, tt := range []struct{ method, path string }{
		{"POST", "/api/bootstrap"},
		{"GET", "/api/session"},
	} {
		for _, token := range []string{"", s.APIToken()} {
			headers := map[string]string{}
			status := http.StatusUnauthorized
			if token != "" {
				headers["Authorization"] = "Bearer " + token
				status = http.StatusNotFound
			}
			r := request(t, s, client, tt.method, tt.path, "", headers)
			if r.StatusCode != status {
				t.Fatalf("%s %s: status %d, want %d", tt.method, tt.path, r.StatusCode, status)
			}
		}
	}
}

func TestAuthenticationErrorsWithholdCredentials(t *testing.T) {
	s, client := newTestServer(t, "")
	const invalidToken = "invalid-submitted-secret"
	r := request(t, s, client, "GET", "/api/health", "", map[string]string{"Authorization": "Bearer " + invalidToken})
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusUnauthorized || strings.Contains(string(b), invalidToken) || strings.Contains(string(b), s.APIToken()) {
		t.Fatal("authentication error exposed a credential or accepted an invalid token")
	}
}

func TestCredentialsArePerServerAndNotFormatted(t *testing.T) {
	s, client := newTestServer(t, "")
	other, _ := newTestServer(t, "")
	if len(s.APIToken()) != 43 || s.APIToken() == other.APIToken() {
		t.Fatal("credentials reused between servers")
	}
	r := request(t, s, client, "GET", "/api/health", "", map[string]string{"Authorization": "Bearer " + other.APIToken()})
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatal("another server's token was accepted")
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{s.URL(), fmt.Sprint(s), fmt.Sprintf("%#v", s), string(b)} {
		if strings.Contains(text, s.APIToken()) {
			t.Fatal("server formatting or URL exposed a credential")
		}
	}
}

func TestShutdownWithoutServe(t *testing.T) {
	s, err := Listen("")
	if err != nil {
		t.Fatal(err)
	}
	address := s.listener.Addr().String()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("shutdown left an unused listener open")
	}
}
