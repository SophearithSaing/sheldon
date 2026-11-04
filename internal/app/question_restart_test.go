package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/question"
)

func TestQuestionRestartClassifiesOnceAndPreservesEveryTerminalState(t *testing.T) {
	for _, state := range []question.State{question.Pending, question.Streaming, question.Completed, question.Failed, question.Interrupted} {
		t.Run(string(state), func(t *testing.T) {
			paths := testPaths(t)
			i := ownedPRInstance(t, paths.Data)
			s := questionTranscriptFixture(t, i, paths.Cache)
			s.Threads[0].Answers[0].State = state
			if state == question.Pending {
				s.Threads[0].Answers[0].Text = ""
			}
			if err := i.SaveQuestions(s, 0); err != nil {
				t.Fatal(err)
			}
			current, err := i.ReadQuestions(s.PRID)
			if err != nil || !reflect.DeepEqual(current, s) {
				t.Fatal("classified a current-run answer")
			}
			if err := i.close(); err != nil {
				t.Fatal(err)
			}
			i = ownedPRInstance(t, paths.Data)
			expected, changed := s.InterruptUnfinished()
			if changed {
				expected.Version++
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					got, err := i.ReadQuestions(s.PRID)
					if err != nil || !reflect.DeepEqual(got, expected) {
						t.Errorf("cold/concurrent history mismatch: %v", err)
					}
				})
			}
			wg.Wait()
			path := filepath.Join(paths.Data, "reviews", s.PRID, "threads.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := i.close(); err != nil {
				t.Fatal(err)
			}
			i = ownedPRInstance(t, paths.Data)
			got, err := i.ReadQuestions(s.PRID)
			if err != nil || !reflect.DeepEqual(got, expected) {
				t.Fatalf("second restart: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("repeated restart rewrote history")
			}
		})
	}
}

func TestQuestionRestartMissingAndCorruptHistory(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	s := questionStoreFixture(t, i)
	got, err := i.ReadQuestions(s.PRID)
	if err != nil || got.Version != 0 {
		t.Fatal("missing history was not empty")
	}
	if _, err := os.Stat(filepath.Join(paths.Data, "reviews")); !os.IsNotExist(err) {
		t.Fatal("empty read created storage")
	}
	if err := i.SaveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	i.close()
	i = ownedPRInstance(t, paths.Data)
	path := filepath.Join(paths.Data, "reviews", s.PRID, "threads.json")
	broken := []byte("{invalid saved history")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := i.ReadQuestions(s.PRID); err == nil || got.Version != 0 {
		t.Fatal("corrupt history was exposed as healthy")
	}
	if i.questionsLoaded[s.PRID] {
		t.Fatal("failed read marked history loaded")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, broken) {
		t.Fatal("failed read changed corrupt bytes")
	}
}

func TestQuestionRestartGenerationOverflowPreservesUnfinishedHistory(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	s := questionTranscriptFixture(t, i, paths.Cache)
	if err := i.SaveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.Data, "reviews", s.PRID, "threads.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"version":1,`), []byte(`"version":18446744073709551615,`), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	i.close()
	i = ownedPRInstance(t, paths.Data)
	if _, err := i.ReadQuestions(s.PRID); err == nil {
		t.Fatal("classification wrapped generation")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatal("overflow changed unfinished history")
	}
}
