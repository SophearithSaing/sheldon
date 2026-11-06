package quiz

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPublicQuizWithholdsKeysAndCopiesChoices(t *testing.T) {
	s := savedFixture(t)
	s.Version = ^uint64(0)
	view, err := s.Public()
	if err != nil || view.Validate(s.PRID) != nil || view.Version != "18446744073709551615" {
		t.Fatal("invalid lossless public projection")
	}
	data, _ := json.Marshal(view)
	for _, private := range []string{"correctChoiceID", "explanation", "evidence", "selected", "surrounding", "schemaVersion", "native", "directory"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("unanswered result exposed %s", private)
		}
	}
	view.Quiz.Questions[0].Choices[0].Text = "edited"
	if s.Questions[0].Choices[0].Text == "edited" {
		t.Fatal("public choices alias private record")
	}
	if empty, err := EmptySaved(s.PRID).Public(); err != nil || empty.Quiz != nil || empty.Version != "0" || empty.Validate(s.PRID) != nil {
		t.Fatal("empty storage projection invalid")
	}
	broken := s
	broken.Version = 0
	if _, err := broken.Public(); !errors.Is(err, ErrStore) {
		t.Fatal("corrupt version zero exposed as empty")
	}
}

func TestCheckedQuizIsExactScopedAndDetached(t *testing.T) {
	s := savedFixture(t)
	for _, choice := range []string{"a", s.Questions[0].CorrectChoiceID} {
		checked, err := s.Check(s.QuizID, "q1", s.RevisionID, choice)
		if err != nil || checked.Validate(s.PRID) != nil || checked.Correct != (choice == s.Questions[0].CorrectChoiceID) || checked.CorrectChoiceID != s.Questions[0].CorrectChoiceID || checked.Explanation != s.Questions[0].Explanation {
			t.Fatal("wrong explicit saved grade")
		}
		checked.Evidence[0].Selected[0].Text = "edit"
		checked.Evidence[0].Surrounding.Lines[0].Text = "edit"
		if s.Questions[0].Evidence[0].Selected[0].Text == "edit" || s.Questions[0].Evidence[0].Surrounding.Lines[0].Text == "edit" {
			t.Fatal("checked evidence aliases saved content")
		}
	}
	for _, tc := range []struct {
		qid, question, revision, choice string
		want                            error
	}{
		{strings.Repeat("b", 64), "q1", s.RevisionID, "a", ErrConflict},
		{s.QuizID, "q1", strings.Repeat("b", 64), "a", ErrConflict},
		{s.QuizID, "q6", s.RevisionID, "a", ErrNotFound},
		{s.QuizID, "q1", s.RevisionID, "e", ErrInvalid},
	} {
		if _, err := s.Check(tc.qid, tc.question, tc.revision, tc.choice); !errors.Is(err, tc.want) {
			t.Fatalf("scope not enforced: %v", err)
		}
	}
}

func TestPublicFeedbackRejectsInvalidVersionsAndRawFailures(t *testing.T) {
	for _, value := range []string{"", "01", "-1", "1.0", "1e2", " 1", "18446744073709551616"} {
		if _, err := ParseVersion(value); err == nil {
			t.Fatalf("noncanonical version %q accepted", value)
		}
	}
	r, g := runtimeFixture(t)
	if g.status.Validate(g.status.PRID) != nil {
		t.Fatal("pending status invalid")
	}
	status := g.status
	status.SavedVersion = "1"
	if status.Validate(status.PRID) == nil {
		t.Fatal("unsaved pending feedback claimed saved version")
	}
	status = g.status
	status.State = Completed
	status.SavedVersion = "1"
	if status.Validate(status.PRID) != nil {
		t.Fatal("valid completion refused")
	}
	status.FailureCode = "PRIVATE provider diagnostic"
	if status.Validate(status.PRID) == nil {
		t.Fatal("raw failure permitted")
	}
	status = g.status
	status.State = Unknown
	status.ExecutionUnknown = true
	status.FailureCode = "execution-uncertain"
	if status.Validate(status.PRID) != nil {
		t.Fatal("valid uncertainty refused")
	}
	status.ExecutionUnknown = false
	if status.Validate(status.PRID) == nil {
		t.Fatal("unknown outcome lost uncertainty")
	}
	latest := r.Latest(g.status.PRID)
	latest.FailureCode = "caller edit"
	if got, _ := r.Read(g.status.PRID, g.status.GenerationID); got.FailureCode != "" {
		t.Fatal("latest feedback aliases runtime")
	}
}

func TestQuizRequestJSONRejectsDuplicateAliasesAndExtraFields(t *testing.T) {
	var request struct {
		RevisionID string `json:"revisionID"`
		ChoiceID   string `json:"choiceID"`
	}
	for _, data := range []string{`null`, `[]`, `{"revisionID":"a","RevisionID":"b","choiceID":"a"}`, `{"revisionID":"a","revisionID":"b","choiceID":"a"}`, `{"revisionID":"a","choiceID":"a","model":"private"}`, `{"revisionID":"a","choiceID":"a"} {}`} {
		if DecodeRequest([]byte(data), &request) == nil {
			t.Fatal("ambiguous request accepted")
		}
	}
}
