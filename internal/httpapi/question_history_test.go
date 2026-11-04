package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
)

type historyRegistryFixture struct {
	*testPRRegistry
	list   question.ThreadList
	detail question.ThreadDetail
	err    error
	calls  atomic.Int32
}

// ListThreads returns only synthetic navigation, with no native execution capability.
func (f *historyRegistryFixture) ListThreads(string) (question.ThreadList, error) {
	f.calls.Add(1)
	return f.list, f.err
}

// ReadThread supplies synthetic saved display data for boundary validation.
func (f *historyRegistryFixture) ReadThread(string, string) (question.ThreadDetail, error) {
	f.calls.Add(1)
	return f.detail, f.err
}

// historyRegistry creates a public-only read provider without implementing question execution methods.
func historyRegistry(t *testing.T) *historyRegistryFixture {
	t.Helper()
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	revision := githubpr.Revision{PRID: pr.ID, BaseBranch: "main", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("a", 40)}
	id, _ := revision.ID()
	anchor := question.Anchor{Kind: "hunk", FileID: strings.Repeat("c", 64), HunkID: strings.Repeat("d", 64)}
	line := git.DiffLine{ID: strings.Repeat("e", 64), Sign: "+", Text: "literal <source>", NewLine: 1}
	context := question.Context{Revision: revision, RevisionID: id, Anchor: anchor, NewPath: "file.go", Selected: []git.DiffLine{line}, Surrounding: git.DiffHunk{ID: anchor.HunkID, Old: git.DiffRange{ID: strings.Repeat("1", 64)}, New: git.DiffRange{ID: strings.Repeat("2", 64), Start: 1, Count: 1}, Lines: []git.DiffLine{line}}}
	threadID := strings.Repeat("f", 64)
	answer := question.Answer{ID: strings.Repeat("3", 64), ThreadID: threadID, PRID: pr.ID, RevisionID: id, Question: "Fixture question", Text: "Literal <answer>", State: question.Completed, Reference: question.Reference{RevisionID: id, Anchor: anchor, NewPath: "file.go"}}
	return &historyRegistryFixture{testPRRegistry: &testPRRegistry{prs: map[string]githubpr.PullRequest{pr.ID: pr}}, list: question.ThreadList{PRID: pr.ID, Version: "18446744073709551615", Threads: []question.ThreadSummary{{ID: threadID, RevisionID: id, Question: answer.Question, NewPath: "file.go", Anchor: anchor, AnswerCount: 1, State: question.Completed, Persistence: "saved"}}}, detail: question.ThreadDetail{PRID: pr.ID, Version: "18446744073709551615", ID: threadID, RevisionID: id, Context: context, Answers: []question.Answer{answer}, Persistence: "saved", Session: question.SessionStatus{State: "unverified", Reason: "continuation-requires-verification"}}}
}

func TestHistoryRoutesProtectScopeMethodsBodiesAndCredentials(t *testing.T) {
	f := historyRegistry(t)
	s, err := Listen("", "", f)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	base := "/api/reviews/" + f.list.PRID + "/threads"
	for _, route := range []string{base, base + "/" + f.detail.ID} {
		for _, tc := range []struct {
			method, path, body, credential, origin, host string
			status                                       int
		}{
			{"GET", route, "", "", "", "", 401},
			{"GET", route, "", "wrong", "", "", 401},
			{"GET", route, "", s.APIToken(), "http://evil.invalid", "", 403},
			{"GET", route, "", s.APIToken(), "", "evil.invalid", 403},
			{"HEAD", route, "", s.APIToken(), "", "", 405},
			{"POST", route, "", s.APIToken(), "", "", 404},
			{"GET", route + "?token=secret", "", s.APIToken(), "", "", 404},
			{"GET", route + "?", "", s.APIToken(), "", "", 404},
			{"GET", route, "{}", s.APIToken(), "", "", 400},
			{"GET", route, "", s.APIToken(), "", "", 200},
		} {
			before := f.calls.Load()
			r, err := http.NewRequest(tc.method, s.URL()+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if tc.credential != "" {
				r.Header.Set("Authorization", "Bearer "+tc.credential)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.host != "" {
				r.Host = tc.host
			}
			response, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != tc.status || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("%s %s: status %d, want %d", tc.method, tc.path, response.StatusCode, tc.status)
			}
			if tc.status != 200 && f.calls.Load() != before {
				t.Fatal("rejected request read/classified storage")
			}
			if strings.Contains(string(data), s.APIToken()) {
				t.Fatal("read reflected bearer credential")
			}
		}
	}
	for _, route := range []string{"/api/reviews/" + strings.Repeat("9", 64) + "/threads", base + "/not-an-id", strings.Replace(base, "/threads", "/%74hreads", 1)} {
		r, _ := http.NewRequest("GET", s.URL()+route, nil)
		r.Header.Set("Authorization", "Bearer "+s.APIToken())
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatal("invalid or unregistered route was accepted")
		}
	}
}

func TestHistoryErrorsAndEncodedResponsesAreBoundedBeforeWriting(t *testing.T) {
	f := historyRegistry(t)
	s, err := Listen("", "", f)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	base := "/api/reviews/" + f.list.PRID + "/threads"
	for _, tc := range []struct {
		err    error
		status int
	}{{question.ErrNotFound, 404}, {errors.New("PRIVATE /secret provider diagnostics"), 500}} {
		f.err = tc.err
		r, _ := http.NewRequest("GET", s.URL()+base+"/"+f.detail.ID, nil)
		r.Header.Set("Authorization", "Bearer "+s.APIToken())
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status || strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "/secret") {
			t.Fatal("history error was not scoped/sanitized")
		}
	}
	f.err = nil
	first := f.detail.Answers[0]
	f.detail.Answers = nil
	for n := range 32 {
		a := first
		a.ID = fmt.Sprintf("%064x", n+100)
		a.Text = strings.Repeat("x", 256<<10)
		f.detail.Answers = append(f.detail.Answers, a)
	}
	r, _ := http.NewRequest("GET", s.URL()+base+"/"+f.detail.ID, nil)
	r.Header.Set("Authorization", "Bearer "+s.APIToken())
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 500 || len(data) > 1024 {
		t.Fatal("oversized detail was partially serialized")
	}
	firstSummary := f.list.Threads[0]
	f.list.Threads = nil
	for n := range 128 {
		item := firstSummary
		item.ID = fmt.Sprintf("%064x", n+1000)
		item.NewPath = strings.Repeat("x", 65536)
		f.list.Threads = append(f.list.Threads, item)
	}
	r, _ = http.NewRequest("GET", s.URL()+base, nil)
	r.Header.Set("Authorization", "Bearer "+s.APIToken())
	response, err = client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 500 || len(data) > 1024 {
		t.Fatal("oversized list was partially serialized")
	}
}
