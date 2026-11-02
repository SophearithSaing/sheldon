package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

type testPRRegistry struct {
	mu  sync.Mutex
	prs map[string]githubpr.PullRequest
	err error
}

func (s *testPRRegistry) RegisterPR(raw string) (githubpr.PullRequest, error) {
	if s.err != nil {
		return githubpr.PullRequest{}, s.err
	}
	pr, err := githubpr.ParseURL(raw)
	if err != nil {
		return pr, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prs[pr.ID] = pr
	return pr, nil
}

func (s *testPRRegistry) ReadPR(id string) (githubpr.PullRequest, error) {
	if s.err != nil {
		return githubpr.PullRequest{}, s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pr, ok := s.prs[id]
	if !ok {
		return githubpr.PullRequest{}, os.ErrNotExist
	}
	return pr, nil
}

func TestOpenRegistrationAndLookup(t *testing.T) {
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
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	r := request(t, s, client, "GET", "/api/reviews/"+pr.ID, "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
	var state githubpr.PullRequest
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.URL != pr.URL || state.ID != pr.ID {
		t.Fatalf("state %v", state)
	}
	for path, status := range map[string]int{
		"/api/open": 404,
		"/api/reviews/" + strings.Repeat("a", 64): 404,
		"/api/reviews/invalid":                    404,
		"/api/reviews/" + pr.ID + "?latest=true":  404,
	} {
		if got := request(t, s, client, "GET", path, "", map[string]string{"Authorization": "Bearer " + s.APIToken()}).StatusCode; got != status {
			t.Errorf("%s: %d, want %d", path, got, status)
		}
	}
}

func TestRegistryFailuresAreNotRegistrationSuccess(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	for _, registry := range []PRRegistry{nil, &testPRRegistry{err: errors.New("private fixture contents")}} {
		s, err := Listen("", "", registry)
		if err != nil {
			t.Fatal(err)
		}
		client := startTestServer(t, s)
		for _, method := range []string{"POST", "GET"} {
			path, body := "/api/reviews/"+pr.ID, ""
			if method == "POST" {
				path, body = "/api/open", `{"url":"`+pr.URL+`"}`
			}
			r := request(t, s, client, method, path, body, map[string]string{"Authorization": "Bearer " + s.APIToken(), "Content-Type": "application/json"})
			data, _ := io.ReadAll(r.Body)
			if r.StatusCode < 500 || strings.Contains(string(data), "private fixture contents") || strings.Contains(string(data), `"route"`) {
				t.Fatalf("registry failure produced unsafe success: %d %s", r.StatusCode, data)
			}
		}
	}
}

func TestReviewRoutesRetainAuthenticationAndAuthorityChecks(t *testing.T) {
	s, client := newTestServer(t, "")
	pr, err := s.prs.RegisterPR("https://github.com/owner/repo/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	route, _ := githubpr.ReviewPath(pr.ID)
	lookup := "/api/reviews/" + pr.ID
	if got := request(t, s, client, "GET", lookup, "", nil).StatusCode; got != 401 {
		t.Fatalf("lookup without bearer: %d", got)
	}
	for _, path := range []string{route, lookup} {
		for key, value := range map[string]string{"Host": "attacker.example", "Origin": "https://attacker.example"} {
			if got := request(t, s, client, "GET", path, "", map[string]string{"Authorization": "Bearer " + s.APIToken(), key: value}).StatusCode; got != 403 {
				t.Errorf("%s bypassed %s: %d", path, key, got)
			}
		}
		response := request(t, s, client, "HEAD", path, "", map[string]string{"Authorization": "Bearer " + s.APIToken()})
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 || len(body) != 0 {
			t.Fatalf("HEAD %s: %d", path, response.StatusCode)
		}
	}
}
