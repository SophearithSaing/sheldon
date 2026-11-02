package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/process"
)

type testSourceRegistry struct {
	*testPRRegistry
	load func(context.Context, githubpr.PullRequest, SourceRequest) (SourceResult, error)
}

func (s *testSourceRegistry) LoadSource(ctx context.Context, pr githubpr.PullRequest, input SourceRequest) (SourceResult, error) {
	return s.load(ctx, pr, input)
}

func TestSourceActionsAreAuthenticatedBoundedAndPRScoped(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	calls := 0
	registry := &testSourceRegistry{testPRRegistry: &testPRRegistry{prs: map[string]githubpr.PullRequest{pr.ID: pr}}}
	registry.load = func(ctx context.Context, got githubpr.PullRequest, input SourceRequest) (SourceResult, error) {
		calls++
		if got != pr || ctx.Err() != nil {
			t.Error("wrong PR or cancelled context")
		}
		return SourceResult{State: "fallback-required", PR: got}, nil
	}
	s, err := Listen("", "", registry)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	path := "/api/reviews/" + pr.ID + "/source"
	for _, tt := range []struct {
		name, path, body, media, token string
		status                         int
	}{
		{"unauthenticated", path, `{"action":"load"}`, "application/json", "", 401},
		{"invalid path", "/api/reviews/no/source", `{"action":"load"}`, "application/json", s.APIToken(), 404},
		{"query", path + "?latest=true", `{"action":"load"}`, "application/json", s.APIToken(), 404},
		{"missing PR", "/api/reviews/" + strings.Repeat("e", 64) + "/source", `{"action":"load"}`, "application/json", s.APIToken(), 404},
		{"media", path, `{"action":"load"}`, "text/plain", s.APIToken(), 415},
		{"unknown field", path, `{"action":"load","remote":"file:///foreign"}`, "application/json", s.APIToken(), 400},
		{"unknown action", path, `{"action":"refresh"}`, "application/json", s.APIToken(), 400},
		{"selection missing", path, `{"action":"select-base"}`, "application/json", s.APIToken(), 400},
		{"unselected branch", path, `{"action":"load","branch":"main"}`, "application/json", s.APIToken(), 400},
		{"extra JSON", path, `{"action":"load"} {}`, "application/json", s.APIToken(), 400},
		{"too large", path, `{"action":"` + strings.Repeat("x", 5000) + `"}`, "application/json", s.APIToken(), 413},
		{"load", path, `{"action":"load"}`, "application/json", s.APIToken(), 200},
		{"consent", path, `{"action":"git-only"}`, "application/json", s.APIToken(), 200},
		{"selection", path, `{"action":"select-base","branch":"feat/target","sha":"` + strings.Repeat("a", 40) + `"}`, "application/json", s.APIToken(), 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := request(t, s, client, "POST", tt.path, tt.body, map[string]string{"Authorization": "Bearer " + tt.token, "Content-Type": tt.media})
			data, _ := io.ReadAll(r.Body)
			if r.StatusCode != tt.status {
				t.Fatalf("%d: %s", r.StatusCode, data)
			}
			if strings.Contains(string(data), "file://") {
				t.Fatal("raw input leaked")
			}
		})
	}
	if calls != 3 {
		t.Fatalf("invalid requests reached source provider: %d", calls)
	}
	r := request(t, s, client, "POST", path, `{"action":"load"}`, map[string]string{"Authorization": "Bearer " + s.APIToken(), "Content-Type": "application/json", "Origin": "https://attacker.invalid"})
	if r.StatusCode != 403 || calls != 3 {
		t.Fatal("hostile origin invoked source")
	}
}

func TestSourceErrorsHideRawDetailsAndNoReadyOnFailure(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	for _, tt := range []struct {
		err    error
		code   string
		status int
	}{
		{errors.New("synthetic-secret /private/cache path"), "source-unavailable", 502},
		{git.ErrRefreshRequired, "refresh-required", 409},
		{process.ErrOutputLimit, "source-too-large", 422},
		{git.ErrHistoryUnavailable, "history-unavailable", 422},
		{context.DeadlineExceeded, "source-timeout", 504},
	} {
		t.Run(tt.code, func(t *testing.T) {
			registry := &testSourceRegistry{testPRRegistry: &testPRRegistry{prs: map[string]githubpr.PullRequest{pr.ID: pr}}, load: func(context.Context, githubpr.PullRequest, SourceRequest) (SourceResult, error) {
				return SourceResult{}, tt.err
			}}
			s, err := Listen("", "", registry)
			if err != nil {
				t.Fatal(err)
			}
			client := startTestServer(t, s)
			r := request(t, s, client, "POST", "/api/reviews/"+pr.ID+"/source", `{"action":"load"}`, map[string]string{"Authorization": "Bearer " + s.APIToken(), "Content-Type": "application/json"})
			data, _ := io.ReadAll(r.Body)
			var body map[string]string
			if json.Unmarshal(data, &body) != nil || r.StatusCode != tt.status || body["code"] != tt.code || strings.Contains(string(data), "synthetic-secret") || strings.Contains(string(data), "private/cache") {
				t.Fatalf("unsafe failure: %d %s", r.StatusCode, data)
			}
		})
	}
}

func TestSourceResultRequiresCompleteRoutedRevision(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	revision := githubpr.Revision{PRID: pr.ID, BaseBranch: "main", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("a", 40)}
	id, _ := revision.ID()
	ready := SourceResult{State: "ready", PR: pr, Diff: &git.Diff{Revision: revision, RevisionID: id, Complete: true, Files: []git.DiffFile{}}}
	if !validSourceResult(ready, pr.ID) {
		t.Fatal("complete pinned result rejected")
	}
	ready.Diff.Complete = false
	if validSourceResult(ready, pr.ID) {
		t.Fatal("incomplete diff accepted")
	}
	ready.Diff.Complete = true
	ready.Diff.RevisionID = strings.Repeat("c", 64)
	if validSourceResult(ready, pr.ID) {
		t.Fatal("mismatched revision digest accepted")
	}
	if validSourceResult(SourceResult{State: "fallback-required", PR: pr, Diff: ready.Diff}, pr.ID) {
		t.Fatal("fallback mixed with source")
	}
	if validSourceResult(SourceResult{State: "base-selection-required", PR: pr, Candidates: []SourceCandidate{{Branch: "--option", SHA: revision.BaseSHA}}}, pr.ID) {
		t.Fatal("unsafe candidate accepted")
	}
}

func TestSourceRequestCancellationReachesProvider(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	started, cancelled := make(chan struct{}), make(chan struct{})
	registry := &testSourceRegistry{testPRRegistry: &testPRRegistry{prs: map[string]githubpr.PullRequest{pr.ID: pr}}, load: func(ctx context.Context, _ githubpr.PullRequest, _ SourceRequest) (SourceResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return SourceResult{}, ctx.Err()
	}}
	s, err := Listen("", "", registry)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", s.URL()+"/api/reviews/"+pr.ID+"/source", strings.NewReader(`{"action":"load"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+s.APIToken())
	r.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		response, _ := client.Do(r)
		if response != nil {
			response.Body.Close()
		}
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("source provider did not start")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(4 * time.Second):
		t.Fatal("source provider was not cancelled")
	}
	<-done
}
