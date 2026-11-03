package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
)

type questionRegistryFixture struct {
	*testPRRegistry
	answer question.Answer
	calls  atomic.Int32
	err    error
}

// AskQuestion returns synthetic admission state without starting native work.
func (f *questionRegistryFixture) AskQuestion(context.Context, string, question.InitialRequest) (question.Answer, error) {
	f.calls.Add(1)
	return f.answer, f.err
}

// AskFollowup records explicit follow-up admission for route checks.
func (f *questionRegistryFixture) AskFollowup(context.Context, string, string, question.FollowupRequest) (question.Answer, error) {
	f.calls.Add(1)
	return f.answer, f.err
}

// ObserveAnswer emits one synthetic snapshot without dispatching a prompt.
func (f *questionRegistryFixture) ObserveAnswer(ctx context.Context, pr, thread, answer string, input question.AnswerRequest, emit func(question.Answer) error) error {
	f.calls.Add(1)
	if f.err != nil {
		return f.err
	}
	return emit(f.answer)
}

// StopAnswer records an explicit Stop operation for route checks.
func (f *questionRegistryFixture) StopAnswer(string, string, string, question.AnswerRequest) (question.Answer, error) {
	f.calls.Add(1)
	return f.answer, f.err
}

func TestQuestionRoutesEnforceAuthenticationScopeAndBounds(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	revision := githubpr.Revision{PRID: pr.ID, BaseBranch: "main", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("a", 40)}
	revisionID, _ := revision.ID()
	anchor := question.Anchor{Kind: "hunk", FileID: strings.Repeat("c", 64), HunkID: strings.Repeat("d", 64)}
	f := &questionRegistryFixture{testPRRegistry: &testPRRegistry{prs: map[string]githubpr.PullRequest{pr.ID: pr}}, answer: question.Answer{ID: strings.Repeat("e", 64), ThreadID: strings.Repeat("f", 64), PRID: pr.ID, RevisionID: revisionID, Question: "Fixture question.", Text: "<script>literal</script>", State: question.Completed, Reference: question.Reference{RevisionID: revisionID, Anchor: anchor, NewPath: "source.go"}}}
	s, err := Listen("", "", f)
	if err != nil {
		t.Fatal(err)
	}
	client := startTestServer(t, s)
	initial, _ := json.Marshal(question.InitialRequest{Revision: revision, Anchor: anchor, Question: "Fixture question."})
	base := "/api/reviews/" + pr.ID
	scope := "{\"revisionID\":\"" + revisionID + "\"}"
	answerPath := base + "/threads/" + f.answer.ThreadID + "/answers/" + f.answer.ID
	for _, tt := range []struct {
		path, body, token, media string
		status                   int
	}{
		{base + "/questions", string(initial), "", "application/json", 401},
		{base + "/questions", string(initial), s.APIToken(), "text/plain", 415},
		{base + "/questions?latest=1", string(initial), s.APIToken(), "application/json", 404},
		{base + "/questions", `{"question":"","unknown":true}`, s.APIToken(), "application/json", 400},
		{base + "/questions", string(initial) + " {}", s.APIToken(), "application/json", 400},
		{base + "/questions", strings.Repeat("x", 25<<10), s.APIToken(), "application/json", 413},
		{base + "/questions", string(initial), s.APIToken(), "application/json", 202},
		{base + "/threads/" + f.answer.ThreadID + "/answers", `{"revisionID":"` + revisionID + `","question":"  "}`, s.APIToken(), "application/json", 400},
		{answerPath + "/events", scope, s.APIToken(), "application/json", 200},
		{answerPath + "/stop", scope, s.APIToken(), "application/json", 202},
		{answerPath + "/stop", strings.Repeat("x", 1025), s.APIToken(), "application/json", 413},
	} {
		r := request(t, s, client, "POST", tt.path, tt.body, map[string]string{"Authorization": "Bearer " + tt.token, "Content-Type": tt.media})
		data, _ := io.ReadAll(r.Body)
		if r.StatusCode != tt.status {
			t.Fatalf("%s: %d %s", tt.path, r.StatusCode, data)
		}
		if strings.HasSuffix(tt.path, "/events") && tt.status == 200 {
			if !strings.Contains(string(data), "event: answer") || strings.Contains(string(data), "<script>") {
				t.Fatal("SSE source text was not safely JSON encoded")
			}
		}
	}
	if f.calls.Load() != 3 {
		t.Fatal("invalid requests reached question provider")
	}
	r := request(t, s, client, "POST", base+"/questions", string(initial), map[string]string{"Authorization": "Bearer " + s.APIToken(), "Content-Type": "application/json", "Origin": "https://attacker.invalid"})
	if r.StatusCode != http.StatusForbidden || f.calls.Load() != 3 {
		t.Fatal("foreign Origin invoked inference provider")
	}
	f.err = question.ErrBusy
	r = request(t, s, client, "POST", base+"/questions", string(initial), map[string]string{"Authorization": "Bearer " + s.APIToken(), "Content-Type": "application/json"})
	if r.StatusCode != 409 {
		t.Fatal("busy admission was not rejected")
	}
	f.err = errors.New("RAW PROVIDER SECRET /private/worktree")
	r = request(t, s, client, "POST", answerPath+"/events", scope, map[string]string{"Authorization": "Bearer " + s.APIToken(), "Content-Type": "application/json"})
	data, _ := io.ReadAll(r.Body)
	if strings.Contains(string(data), "SECRET") || strings.Contains(string(data), "/private") {
		t.Fatal("provider diagnostics leaked into API")
	}
}
