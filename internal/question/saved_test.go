package question

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/opencode"
)

func savedFixture() Saved {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	r := githubpr.Revision{PRID: pr.ID, BaseBranch: "target", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("a", 40)}
	id, _ := r.ID()
	a := Anchor{Kind: "hunk", FileID: strings.Repeat("c", 64), HunkID: strings.Repeat("d", 64)}
	line := git.DiffLine{ID: strings.Repeat("e", 64), Sign: "+", Text: "literal <source>", NewLine: 1}
	c := Context{Revision: r, RevisionID: id, Anchor: a, NewPath: "file.go", Selected: []git.DiffLine{line}, Surrounding: git.DiffHunk{ID: a.HunkID, Old: git.DiffRange{ID: strings.Repeat("f", 64)}, New: git.DiffRange{ID: strings.Repeat("1", 64), Start: 1, Count: 1}, Lines: []git.DiffLine{line}}}
	return Saved{SchemaVersion: 1, PRID: pr.ID, Version: 1, Bindings: []SavedBinding{{Revision: r, RevisionID: id, State: "bound", Native: &opencode.BindingRecord{SessionID: "ses_fixture", Directory: filepath.Join(os.TempDir(), "private-worktree"), Model: opencode.Model{ProviderID: "fixture", ID: "chat"}, Version: "2.1.4", Protocol: "sheldon-opencode-http-v1", Policy: 1}}}, Threads: []SavedThread{{ID: strings.Repeat("2", 64), RevisionID: id, Context: c, Answers: []Answer{{ID: strings.Repeat("3", 64), ThreadID: strings.Repeat("2", 64), PRID: pr.ID, RevisionID: id, Question: "Explain this", Text: "A concise answer", State: Completed, Reference: Reference{RevisionID: id, Anchor: a, NewPath: "file.go"}}}}}}
}

func cloneSaved(t *testing.T, s Saved) Saved {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var out Saved
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSavedGraphRoundTrip(t *testing.T) {
	s := savedFixture()
	if err := s.Validate(s.PRID); err != nil {
		t.Fatal(err)
	}
	got := cloneSaved(t, s)
	if !reflect.DeepEqual(s, got) {
		t.Fatal("round trip changed history")
	}
	for _, state := range []State{Pending, Streaming, Failed, Interrupted} {
		got := cloneSaved(t, s)
		got.Threads[0].Answers[0].State = state
		got.Threads[0].Answers[0].Text = ""
		if err := got.Validate(got.PRID); err != nil {
			t.Fatalf("%s: %v", state, err)
		}
	}
}

func TestSavedGraphRejectsInvalidState(t *testing.T) {
	for name, mutate := range map[string]func(*Saved){
		"version":           func(s *Saved) { s.Version = 0 },
		"schema":            func(s *Saved) { s.SchemaVersion = 2 },
		"pr":                func(s *Saved) { s.PRID = strings.Repeat("a", 64) },
		"native":            func(s *Saved) { s.Bindings[0].Native.Policy = 2 },
		"initializing":      func(s *Saved) { s.Bindings[0].State = "initializing" },
		"duplicate binding": func(s *Saved) { s.Bindings = append(s.Bindings, s.Bindings[0]) },
		"duplicate thread":  func(s *Saved) { s.Threads = append(s.Threads, s.Threads[0]) },
		"duplicate answer":  func(s *Saved) { s.Threads[0].Answers = append(s.Threads[0].Answers, s.Threads[0].Answers[0]) },
		"quote":             func(s *Saved) { s.Threads[0].Context.Selected[0].Text = "forged" },
		"directory":         func(s *Saved) { s.Threads[0].Context.Directory = "/host" },
		"coordinates":       func(s *Saved) { s.Threads[0].Context.Surrounding.New.Count = 2 },
		"empty success":     func(s *Saved) { s.Threads[0].Answers[0].Text = " " },
		"unknown success":   func(s *Saved) { s.Threads[0].Answers[0].ExecutionUnknown = true },
		"answer size":       func(s *Saved) { s.Threads[0].Answers[0].Text = strings.Repeat("x", maxAnswerBytes+1) },
		"question size":     func(s *Saved) { s.Threads[0].Answers[0].Question = strings.Repeat("x", MaxQuestionBytes+1) },
		"context size":      func(s *Saved) { s.Threads[0].Context.Surrounding.Section = strings.Repeat("x", MaxContextBytes) },
	} {
		t.Run(name, func(t *testing.T) {
			s := cloneSaved(t, savedFixture())
			mutate(&s)
			if s.Validate(s.PRID) == nil {
				t.Fatal("accepted invalid graph")
			}
		})
	}
}

func TestSavedUpdatePreservesCommittedHistory(t *testing.T) {
	old := savedFixture()
	next := cloneSaved(t, old)
	next.Version++
	if err := next.ValidateUpdate(old); err != nil {
		t.Fatal(err)
	}
	next.Threads[0].Answers[0].Text = "rewritten"
	if next.ValidateUpdate(old) == nil {
		t.Fatal("terminal answer rewritten")
	}
	next = cloneSaved(t, old)
	next.Version++
	next.Bindings[0].Native.SessionID = "replacement"
	if next.ValidateUpdate(old) == nil {
		t.Fatal("binding replaced")
	}
	next = cloneSaved(t, old)
	next.Version++
	next.Threads[0].Context.NewPath = "other"
	if next.ValidateUpdate(old) == nil {
		t.Fatal("context retargeted")
	}
}

func TestSavedCountAndTextBoundaries(t *testing.T) {
	for _, kind := range []string{"bindings", "threads", "answers"} {
		t.Run(kind, func(t *testing.T) {
			s := savedFixture()
			switch kind {
			case "bindings":
				for n := 1; n < maxBindings; n++ {
					b := s.Bindings[0]
					b.Revision.HeadSHA = fmt.Sprintf("%040x", n+100)
					b.RevisionID, _ = b.Revision.ID()
					b.State = "initializing"
					b.Native = nil
					s.Bindings = append(s.Bindings, b)
				}
			case "threads":
				for n := 1; n < maxThreads; n++ {
					thread := cloneSaved(t, savedFixture()).Threads[0]
					thread.ID = fmt.Sprintf("%064x", n+1000)
					thread.Answers[0].ID = fmt.Sprintf("%064x", n+10000)
					thread.Answers[0].ThreadID = thread.ID
					s.Threads = append(s.Threads, thread)
				}
			case "answers":
				for n := 1; n < maxAnswers; n++ {
					a := s.Threads[0].Answers[0]
					a.ID = fmt.Sprintf("%064x", n+10000)
					s.Threads[0].Answers = append(s.Threads[0].Answers, a)
				}
			}
			if err := s.Validate(s.PRID); err != nil {
				t.Fatalf("exact limit: %v", err)
			}
			switch kind {
			case "bindings":
				s.Bindings = append(s.Bindings, s.Bindings[0])
			case "threads":
				s.Threads = append(s.Threads, s.Threads[0])
			case "answers":
				s.Threads[0].Answers = append(s.Threads[0].Answers, s.Threads[0].Answers[0])
			}
			if s.Validate(s.PRID) == nil {
				t.Fatal("overflow accepted")
			}
		})
	}
	s := savedFixture()
	s.Threads[0].Answers[0].Question = strings.Repeat("x", MaxQuestionBytes)
	s.Threads[0].Answers[0].Text = strings.Repeat("x", maxAnswerBytes)
	if err := s.Validate(s.PRID); err != nil {
		t.Fatalf("exact text limits: %v", err)
	}
}

func TestSavedSharedBindingAndBusyInvariant(t *testing.T) {
	s := savedFixture()
	thread := cloneSaved(t, s).Threads[0]
	thread.ID = strings.Repeat("4", 64)
	thread.Answers[0].ID = strings.Repeat("5", 64)
	thread.Answers[0].ThreadID = thread.ID
	s.Threads = append(s.Threads, thread)
	if err := s.Validate(s.PRID); err != nil {
		t.Fatal(err)
	}
	s.Threads[0].Answers[0].State = Streaming
	s.Threads[1].Answers[0].State = Pending
	if s.Validate(s.PRID) == nil {
		t.Fatal("two active answers admitted")
	}
	s.Threads[1].Answers[0].State = Completed
	if err := s.Validate(s.PRID); err != nil {
		t.Fatal(err)
	}
	s.Threads[0].Answers[0].State = Interrupted
	s.Threads[0].Answers[0].ExecutionUnknown = true
	if err := s.Validate(s.PRID); err != nil {
		t.Fatal(err)
	}
}

func TestSavedEncodedThreadLimit(t *testing.T) {
	s := savedFixture()
	first := s.Threads[0].Answers[0]
	s.Threads[0].Answers = nil
	for n := range 32 {
		a := first
		a.ID = fmt.Sprintf("%064x", n+10000)
		a.Text = strings.Repeat("x", maxAnswerBytes)
		s.Threads[0].Answers = append(s.Threads[0].Answers, a)
	}
	encoded, _ := json.Marshal(s.Threads[0])
	excess := len(encoded) - MaxSavedThreadBytes
	if excess <= 0 || excess >= maxAnswerBytes {
		t.Fatal("invalid boundary fixture")
	}
	last := &s.Threads[0].Answers[31]
	last.Text = last.Text[:len(last.Text)-excess]
	if err := s.Validate(s.PRID); err != nil {
		t.Fatalf("exact encoded thread: %v", err)
	}
	last.Text += "x"
	if s.Validate(s.PRID) == nil {
		t.Fatal("encoded thread overflow accepted")
	}
}

func TestSavedEncodedFileLimit(t *testing.T) {
	s := savedFixture()
	prototype := s.Threads[0]
	s.Threads = nil
	text := strings.Repeat("x", maxAnswerBytes)
	for n := range 16 {
		thread := prototype
		thread.ID = fmt.Sprintf("%064x", n+1000)
		thread.Answers = nil
		for k := range 8 {
			a := prototype.Answers[0]
			a.ThreadID = thread.ID
			a.ID = fmt.Sprintf("%064x", n*8+k+10000)
			a.Text = text
			thread.Answers = append(thread.Answers, a)
		}
		s.Threads = append(s.Threads, thread)
	}
	if s.Validate(s.PRID) == nil {
		t.Fatal("encoded file overflow accepted")
	}
	encoded, _ := json.Marshal(s)
	excess := len(encoded) + 1 - MaxSavedBytes
	if excess <= 0 || excess >= maxAnswerBytes {
		t.Fatal("invalid file boundary fixture")
	}
	last := &s.Threads[15].Answers[7]
	last.Text = last.Text[:len(last.Text)-excess]
	if err := s.Validate(s.PRID); err != nil {
		t.Fatalf("exact encoded file: %v", err)
	}
	last.Text += "x"
	if s.Validate(s.PRID) == nil {
		t.Fatal("one-byte file overflow accepted")
	}
}

func TestSavedOldAndNewLineQuotes(t *testing.T) {
	for _, side := range []string{"old", "new"} {
		t.Run(side, func(t *testing.T) {
			s := savedFixture()
			c := &s.Threads[0].Context
			c.Anchor.Kind = "lines"
			c.Anchor.Side = side
			c.Anchor.Start = 1
			c.Anchor.End = 1
			c.OldPath = "file.go"
			removed := git.DiffLine{ID: strings.Repeat("6", 64), Sign: "-", Text: "old\r", OldLine: 1, NoNewline: true}
			added := git.DiffLine{ID: strings.Repeat("7", 64), Sign: "+", Text: "new", NewLine: 1, NoNewline: true}
			c.Surrounding.Old.Start = 1
			c.Surrounding.Old.Count = 1
			c.Surrounding.Lines = []git.DiffLine{removed, added}
			c.Selected = []git.DiffLine{added}
			if side == "old" {
				c.Selected = []git.DiffLine{removed}
			}
			a := &s.Threads[0].Answers[0]
			a.Reference.Anchor = c.Anchor
			a.Reference.OldPath = c.OldPath
			if err := s.Validate(s.PRID); err != nil {
				t.Fatal(err)
			}
			c.Selected[0].Text = "rewritten"
			if s.Validate(s.PRID) == nil {
				t.Fatal("altered quote accepted")
			}
		})
	}
}
