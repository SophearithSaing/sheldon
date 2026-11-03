package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

func TestClientVerifiesThenForwards(t *testing.T) {
	s, httpClient := newTestServer(t, "")
	c, err := NewClient(s.URL(), s.APIToken())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Open(context.Background(), "https://github.com/owner/repo/pull/1"); err == nil {
		t.Fatal("unverified client forwarded a token")
	}
	if _, err := c.BrowserURL("/"); err == nil {
		t.Fatal("unverified client exposed a token")
	}
	if err := c.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	opened, err := c.Open(context.Background(), "https://github.com/owner/repo/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	r := request(t, s, httpClient, "GET", "/api/reviews/"+opened.PRID, "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
	var state map[string]string
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state["url"] != "https://github.com/owner/repo/pull/1" {
		t.Fatal("open intent was not forwarded")
	}
	u, err := c.BrowserURL(opened.Route)
	if err != nil || u != s.URL()+opened.Route+"#token="+s.APIToken() {
		t.Fatal("incorrect launch URL")
	}
	for _, text := range []string{fmt.Sprint(c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(text, s.APIToken()) {
			t.Fatal("client formatting leaked credential")
		}
	}
}

func TestClientRejectsUnsafeDiscovery(t *testing.T) {
	token, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{
		"https://127.0.0.1:1234", "http://localhost:1234", "http://0.0.0.0:1234", "http://192.0.2.1:1234",
		"http://[::]:1234", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:0123",
		"http://user@127.0.0.1:1234", "http://127.0.0.1:1234/", "http://127.0.0.1:1234?token=x",
		"http://127.0.0.1:1234#token=x", "http://[::ffff:127.0.0.1]:1234", "http://[::1%25lo]:1234",
	} {
		if _, err := NewClient(origin, token); err == nil {
			t.Errorf("unsafe origin accepted: %s", origin)
		}
	}
	if _, err := NewClient("http://127.0.0.1:1234", "invalid"); err == nil {
		t.Fatal("invalid credential accepted")
	}
}

func TestVerificationNeverSendsBearerToUnrelatedServer(t *testing.T) {
	token, _ := newSecret()
	for _, mode := range []string{"wrong proof", "redirect", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || strings.Contains(r.RequestURI, token) {
					t.Error("verification leaked credential")
				}
				if r.URL.Path != "/instance-proof" {
					t.Error("redirect followed or API contacted")
				}
				switch mode {
				case "redirect":
					http.Redirect(w, r, "/api/open", http.StatusFound)
				case "oversized":
					io.WriteString(w, strings.Repeat("x", 2048))
				default:
					io.WriteString(w, `{"proof":"wrong"}`)
				}
			}))
			defer target.Close()
			c, err := NewClient(target.URL, token)
			if err != nil {
				t.Fatal(err)
			}
			if c.Verify(context.Background()) == nil {
				t.Fatal("unrelated listener verified")
			}
			if _, err := c.Open(context.Background(), "https://github.com/owner/repo/pull/1"); err == nil {
				t.Fatal("unverified open accepted")
			}
		})
	}
}

func TestVerificationProofCannotBeReplayed(t *testing.T) {
	token, _ := newSecret()
	var saved string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if saved == "" {
			saved = instanceProof(token, "http://"+r.Host, r.Header.Get("X-Sheldon-Challenge"))
		}
		writeJSON(w, http.StatusOK, map[string]string{"proof": saved})
	}))
	defer target.Close()
	c, err := NewClient(target.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Verify(context.Background()) == nil {
		t.Fatal("stale proof verified")
	}
	if _, err := c.BrowserURL("/"); err == nil {
		t.Fatal("failed reverification left client trusted")
	}
}

func TestShellWithholdsToken(t *testing.T) {
	s, c := newTestServer(t, "")
	r := request(t, s, c, "GET", "/", "", nil)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusOK || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") || strings.Contains(string(body), s.APIToken()) {
		t.Fatal("unsafe runtime page")
	}
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal("runtime page lacks CSP")
	}
}

func TestClientRejectsWrongRegistrationRoutes(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	route, _ := githubpr.ReviewPath(pr.ID)
	for _, body := range []string{
		`{}`, `{"prID":"` + pr.ID + `","route":"https://attacker.example"}`,
		`{"prID":"` + strings.Repeat("a", 64) + `","route":"` + route + `"}`,
		`{"prID":"` + pr.ID + `","route":"` + route + `#token=secret"}`,
		strings.Repeat("x", 4097),
	} {
		t.Run(body[:min(len(body), 24)], func(t *testing.T) {
			token, _ := newSecret()
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/instance-proof" {
					writeJSON(w, 200, map[string]string{"proof": instanceProof(token, "http://"+r.Host, r.Header.Get("X-Sheldon-Challenge"))})
					return
				}
				w.WriteHeader(202)
				io.WriteString(w, body)
			}))
			defer target.Close()
			c, _ := NewClient(target.URL, token)
			if err := c.Verify(context.Background()); err != nil {
				t.Fatal(err)
			}
			if result, err := c.Open(context.Background(), pr.URL); err == nil || result != (OpenResult{}) {
				t.Fatal("accepted invalid registration response")
			}
			for _, invalid := range []string{"//attacker.example", route + "?token=x", route + "#token=x", "/reviews/invalid"} {
				if _, err := c.BrowserURL(invalid); err == nil {
					t.Fatal("accepted unsafe browser route")
				}
			}
		})
	}
}
