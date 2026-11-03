package question

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

type fixtureRegistry struct {
	pr    githubpr.PullRequest
	err   error
	calls int
}

func (r *fixtureRegistry) ReadPR(id string) (githubpr.PullRequest, error) {
	r.calls++
	if r.err != nil {
		return githubpr.PullRequest{}, r.err
	}
	if id != r.pr.ID {
		return githubpr.PullRequest{}, os.ErrNotExist
	}
	return r.pr, nil
}

func fixture(t *testing.T) (*fixtureRegistry, InitialRequest, ThreadContext, AnswerContext) {
	t.Helper()
	pr, err := githubpr.ParseURL("https://github.com/acme/platform/pull/31")
	if err != nil {
		t.Fatal(err)
	}
	revision := githubpr.Revision{PRID: pr.ID, BaseBranch: "release/target", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("c", 40)}
	input := InitialRequest{Revision: revision, Question: "Why return the old session?\nKeep it concise.", Anchor: Anchor{Kind: "lines", FileID: strings.Repeat("d", 64), HunkID: strings.Repeat("e", 64), Side: "old", Start: 27, End: 30}}
	thread := ThreadContext{ID: strings.Repeat("f", 64), Revision: revision}
	answer := AnswerContext{ID: strings.Repeat("1", 64), Thread: thread}
	return &fixtureRegistry{pr: pr}, input, thread, answer
}

func TestInitialRequest(t *testing.T) {
	registry, input, _, _ := fixture(t)
	t.Setenv("PATH", "")
	for _, side := range []string{"old", "new"} {
		input.Anchor.Side = side
		if err := input.Validate(registry.pr.ID, registry); err != nil {
			t.Fatal(err)
		}
	}
	input.Anchor = Anchor{Kind: "hunk", FileID: input.Anchor.FileID, HunkID: input.Anchor.HunkID}
	if err := input.Validate(registry.pr.ID, registry); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded InitialRequest
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != input {
		t.Fatalf("round trip: %v, %+v", err, decoded)
	}
}

func TestInitialRequestRejectsInvalidContext(t *testing.T) {
	cases := map[string]func(*InitialRequest){
		"other PR":        func(r *InitialRequest) { r.Revision.PRID = strings.Repeat("2", 64) },
		"invalid commit":  func(r *InitialRequest) { r.Revision.HeadSHA = "main" },
		"unknown base":    func(r *InitialRequest) { r.Revision.BaseSource = "default" },
		"empty question":  func(r *InitialRequest) { r.Question = "" },
		"blank question":  func(r *InitialRequest) { r.Question = " \t\n\u2003" },
		"oversize":        func(r *InitialRequest) { r.Question = strings.Repeat("x", MaxQuestionBytes+1) },
		"UTF8 byte bound": func(r *InitialRequest) { r.Question = strings.Repeat("界", MaxQuestionBytes/3+1) },
		"invalid UTF8":    func(r *InitialRequest) { r.Question = string([]byte{0xff}) },
		"NUL":             func(r *InitialRequest) { r.Question = "why\x00?" },
		"file scope":      func(r *InitialRequest) { r.Anchor.Kind = "file" },
		"PR scope":        func(r *InitialRequest) { r.Anchor.Kind = "pr" },
		"multi-range":     func(r *InitialRequest) { r.Anchor.Kind = "ranges" },
		"missing file":    func(r *InitialRequest) { r.Anchor.FileID = "" },
		"path as file":    func(r *InitialRequest) { r.Anchor.FileID = "../secret" },
		"missing hunk":    func(r *InitialRequest) { r.Anchor.HunkID = "" },
		"zero reference":  func(r *InitialRequest) { r.Anchor.HunkID = strings.Repeat("0", 64) },
		"wrong side":      func(r *InitialRequest) { r.Anchor.Side = "both" },
		"zero line":       func(r *InitialRequest) { r.Anchor.Start = 0 },
		"negative line":   func(r *InitialRequest) { r.Anchor.Start = -1 },
		"reversed range":  func(r *InitialRequest) { r.Anchor.End = r.Anchor.Start - 1 },
		"long range":      func(r *InitialRequest) { r.Anchor.End = r.Anchor.Start + 1000 },
		"large line":      func(r *InitialRequest) { r.Anchor.Start = 1_000_100_001; r.Anchor.End = r.Anchor.Start },
		"mixed hunk":      func(r *InitialRequest) { r.Anchor.Kind = "hunk" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			registry, input, _, _ := fixture(t)
			mutate(&input)
			if input.Validate(registry.pr.ID, registry) == nil {
				t.Fatal("accepted invalid initial request")
			}
		})
	}
}

func TestRegistryBoundary(t *testing.T) {
	registry, input, thread, answer := fixture(t)
	revisionID, _ := input.Revision.ID()
	checks := []func(string, Registry) error{
		func(id string, r Registry) error { return input.Validate(id, r) },
		func(id string, r Registry) error {
			return (FollowupRequest{RevisionID: revisionID, Question: "Why?"}).Validate(id, thread.ID, r, thread)
		},
		func(id string, r Registry) error {
			return (AnswerRequest{RevisionID: revisionID}).Validate(id, thread.ID, answer.ID, r, answer)
		},
	}
	for _, check := range checks {
		for _, id := range []string{"", "../prs.json", strings.Repeat("0", 64), strings.Repeat("A", 64)} {
			before := registry.calls
			if check(id, registry) == nil || registry.calls != before {
				t.Fatal("invalid route reached registry or was accepted")
			}
		}
		if check(strings.Repeat("2", 64), registry) == nil {
			t.Fatal("unknown PR accepted")
		}
		if check(registry.pr.ID, nil) == nil {
			t.Fatal("missing registry accepted")
		}
		registry.err = errors.New("private storage secret /host/path")
		if err := check(registry.pr.ID, registry); err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "/host") {
			t.Fatalf("unsafe registry error: %v", err)
		}
		registry.err = nil
		wrong := &fixtureRegistry{pr: registry.pr}
		wrong.pr.ID = strings.Repeat("3", 64)
		if check(wrong.pr.ID, wrong) == nil {
			t.Fatal("corrupt registered identity accepted")
		}
	}
}

func TestFollowupAndAnswerScopes(t *testing.T) {
	registry, input, thread, answer := fixture(t)
	revisionID, _ := input.Revision.ID()
	followup := FollowupRequest{RevisionID: revisionID, Question: "What about exactly ten?"}
	target := AnswerRequest{RevisionID: revisionID}
	if err := followup.Validate(registry.pr.ID, thread.ID, registry, thread); err != nil {
		t.Fatal(err)
	}
	if err := target.Validate(registry.pr.ID, thread.ID, answer.ID, registry, answer); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "../revision", strings.Repeat("0", 64), strings.Repeat("A", 64), strings.Repeat("4", 64)} {
		wrongFollowup := followup
		wrongFollowup.RevisionID = id
		wrongTarget := AnswerRequest{RevisionID: id}
		if wrongFollowup.Validate(registry.pr.ID, thread.ID, registry, thread) == nil || wrongTarget.Validate(registry.pr.ID, thread.ID, answer.ID, registry, answer) == nil {
			t.Fatal("invalid or mismatched revision ID accepted")
		}
	}
	for _, id := range []string{"", "native-session-id", strings.Repeat("4", 64)} {
		if followup.Validate(registry.pr.ID, id, registry, thread) == nil || target.Validate(registry.pr.ID, id, answer.ID, registry, answer) == nil {
			t.Fatal("wrong thread accepted")
		}
		if target.Validate(registry.pr.ID, thread.ID, id, registry, answer) == nil {
			t.Fatal("wrong answer accepted")
		}
	}
	for _, change := range []func(*githubpr.Revision){
		func(r *githubpr.Revision) { r.BaseSHA = strings.Repeat("5", 40) },
		func(r *githubpr.Revision) { r.HeadSHA = strings.Repeat("6", 40) },
		func(r *githubpr.Revision) { r.PRID = strings.Repeat("7", 64) },
	} {
		other := thread
		change(&other.Revision)
		otherAnswer := answer
		otherAnswer.Thread = other
		if followup.Validate(registry.pr.ID, thread.ID, registry, other) == nil || target.Validate(registry.pr.ID, thread.ID, answer.ID, registry, otherAnswer) == nil {
			t.Fatal("cross-revision/PR context accepted")
		}
	}
	for _, text := range []string{" ", strings.Repeat("x", MaxQuestionBytes+1), string([]byte{0xff})} {
		followup.Question = text
		if followup.Validate(registry.pr.ID, thread.ID, registry, thread) == nil {
			t.Fatal("invalid follow-up text accepted")
		}
	}
}

func TestQuestionLimitsAndLiteralText(t *testing.T) {
	registry, input, _, _ := fixture(t)
	for _, text := range []string{strings.Repeat("x", MaxQuestionBytes), "  <script>alert('literal')</script>\nWhy?  ", strings.Repeat("界", MaxQuestionBytes/3)} {
		input.Question = text
		input.Anchor.End = input.Anchor.Start + 999
		if err := input.Validate(registry.pr.ID, registry); err != nil || input.Question != text {
			t.Fatalf("literal/boundary text changed or rejected: %v", err)
		}
	}
}
