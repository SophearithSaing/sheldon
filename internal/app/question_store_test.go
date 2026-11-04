package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/opencode"
	"github.com/SophearithSaing/sheldon/internal/question"
)

func questionStoreFixture(t *testing.T, i *instance) question.Saved {
	t.Helper()
	pr, err := i.RegisterPR("https://github.com/owner/repo/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	r := githubpr.Revision{PRID: pr.ID, BaseBranch: "target", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("a", 40)}
	id, _ := r.ID()
	s := question.EmptySaved(pr.ID)
	s.Version = 1
	s.Bindings = append(s.Bindings, question.SavedBinding{Revision: r, RevisionID: id, State: "initializing"})
	return s
}

func TestQuestionStoreColdReopenAndConflict(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private 日本語")
	i := ownedPRInstance(t, dir)
	s := questionStoreFixture(t, i)
	missing, err := i.readQuestions(s.PRID)
	if err != nil || missing.Version != 0 {
		t.Fatalf("empty: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reviews")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created storage")
	}
	if err := i.saveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "reviews", s.PRID, "threads.json")
	before, _ := os.ReadFile(path)
	if err := i.saveQuestions(s, 0); !errors.Is(err, errQuestionConflict) {
		t.Fatalf("stale save: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("conflict changed bytes")
	}
	if err := i.close(); err != nil {
		t.Fatal(err)
	}
	i = ownedPRInstance(t, dir)
	got, err := i.readQuestions(s.PRID)
	if err != nil || got.Version != 1 || len(got.Bindings) != 1 {
		t.Fatalf("reopen: %v", err)
	}
}

func TestQuestionStoreConcurrentGeneration(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := questionStoreFixture(t, i)
	if err := i.saveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	s.Version = 2
	var mu sync.Mutex
	wins := 0
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			err := i.saveQuestions(s, 1)
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, errQuestionConflict) {
				t.Errorf("save: %v", err)
			}
		})
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d writers won", wins)
	}
}

func TestQuestionStorePreservesInvalidFiles(t *testing.T) {
	for name, data := range map[string][]byte{
		"malformed": []byte("{broken"), "future": []byte(`{"schemaVersion":2}`), "utf8": []byte("{\xff}"),
		"duplicate": []byte(`{"schemaVersion":1,"schemaVersion":1}`),
		"alias":     []byte(`{"schemaVersion":1,"SchemaVersion":1}`),
		"oversized": bytes.Repeat([]byte(" "), question.MaxSavedBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			i := ownedPRInstance(t, testPaths(t).Data)
			s := questionStoreFixture(t, i)
			if err := i.saveQuestions(s, 0); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(filepath.Dir(i.file.Name()), "reviews", s.PRID, "threads.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := i.readQuestions(s.PRID); err == nil {
				t.Fatal("corrupt history read as healthy")
			}
			s.Version++
			if i.saveQuestions(s, 1) == nil {
				t.Fatal("overwrote corrupt history")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(data, after) {
				t.Fatal("corrupt bytes changed")
			}
		})
	}
}

func TestQuestionStoreOwnershipAndInvalidIDs(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := questionStoreFixture(t, i)
	for _, id := range []string{"../escape", strings.Repeat("f", 64)} {
		if _, err := i.readQuestions(id); err == nil {
			t.Fatal("unregistered id accepted")
		}
	}
	contender, err := openInstance(filepath.Dir(i.file.Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer contender.close()
	if contender.saveQuestions(s, 0) == nil {
		t.Fatal("contender saved")
	}
	if _, err := contender.readQuestions(s.PRID); err == nil {
		t.Fatal("contender read")
	}
	i.close()
	if i.saveQuestions(s, 0) == nil {
		t.Fatal("closed writer saved")
	}
}

func TestQuestionJSONRejectsAliasesAndNesting(t *testing.T) {
	for _, data := range []string{`{"a":1,"\u0061":2}`, `{"s":1,"ſ":2}`, strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34), `{} {}`} {
		if uniqueQuestionJSON([]byte(data)) {
			t.Fatalf("accepted %q", data)
		}
	}
	data, _ := json.Marshal(question.EmptySaved(strings.Repeat("a", 64)))
	if !uniqueQuestionJSON(data) {
		t.Fatal("valid object rejected")
	}
}

func TestQuestionStorePreservesSiblingPRAndCacheIndependence(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	s := questionStoreFixture(t, i)
	if err := i.saveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	pr, err := i.RegisterPR("https://github.com/owner/repo/pull/2")
	if err != nil {
		t.Fatal(err)
	}
	other := question.EmptySaved(pr.ID)
	other.Version = 1
	if err := i.saveQuestions(other, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.Data, "reviews", pr.ID, "threads.json")
	before, _ := os.ReadFile(path)
	s.Version++
	if err := i.saveQuestions(s, 1); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("sibling changed")
	}
	if _, err := os.Stat(paths.Cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("history initialized cache")
	}
	if i.questions != nil {
		t.Fatal("history initialized question runtime")
	}
	i.close()
	i = ownedPRInstance(t, paths.Data)
	if got, err := i.readQuestions(s.PRID); err != nil || got.Version != 2 {
		t.Fatalf("cache-free reopen: %v", err)
	}
}

func TestQuestionStoreExactReadByteLimit(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := questionStoreFixture(t, i)
	if err := i.saveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(i.file.Name()), "reviews", s.PRID, "threads.json")
	data, _ := json.Marshal(s)
	data = append(data, bytes.Repeat([]byte(" "), question.MaxSavedBytes-len(data))...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := i.readQuestions(s.PRID); err != nil {
		t.Fatalf("exact byte limit: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(" ")
	f.Close()
	if _, err := i.readQuestions(s.PRID); err == nil {
		t.Fatal("byte overflow accepted")
	}
}

func TestQuestionStoreExplicitReadClearsUncertainReplacement(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := questionStoreFixture(t, i)
	if err := i.saveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	i.questionStoreUncertain = map[string]bool{s.PRID: true}
	s.Version++
	if err := i.saveQuestions(s, 1); !errors.Is(err, errQuestionReplace) {
		t.Fatalf("uncertain save: %v", err)
	}
	if _, err := i.readQuestions(s.PRID); err != nil {
		t.Fatal(err)
	}
	if err := i.saveQuestions(s, 1); err != nil {
		t.Fatalf("checked retry: %v", err)
	}
}

func TestQuestionStoreRawReadersSeeWholeGenerations(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := questionStoreFixture(t, i)
	if err := i.saveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(i.file.Name()), "reviews", s.PRID, "threads.json")
	done := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		defer close(done)
		for range 100 {
			data, err := os.ReadFile(path)
			if err != nil {
				errs <- err
				return
			}
			var got question.Saved
			if json.Unmarshal(data, &got) != nil || got.Validate(s.PRID) != nil {
				errs <- fmt.Errorf("reader saw incomplete generation")
				return
			}
		}
	}()
	for version := uint64(2); version <= 15; version++ {
		next := s
		next.Version = version
		if err := i.saveQuestions(next, version-1); err != nil {
			t.Fatal(err)
		}
	}
	<-done
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}

// questionTranscriptFixture supplies saved quotes with a deliberately missing original worktree.
func questionTranscriptFixture(t *testing.T, i *instance, cache string) question.Saved {
	t.Helper()
	s := questionStoreFixture(t, i)
	b := &s.Bindings[0]
	b.State = "bound"
	b.Native = &opencode.BindingRecord{SessionID: "ses_fixture", Directory: filepath.Join(cache, "missing-worktree"), Model: opencode.Model{ProviderID: "fixture", ID: "chat"}, Version: "2.1.4", Protocol: "sheldon-opencode-http-v1", Policy: 1}
	anchor := question.Anchor{Kind: "hunk", FileID: strings.Repeat("c", 64), HunkID: strings.Repeat("d", 64)}
	line := git.DiffLine{ID: strings.Repeat("e", 64), Sign: "+", Text: "<literal source>", NewLine: 1}
	context := question.Context{Revision: b.Revision, RevisionID: b.RevisionID, Anchor: anchor, NewPath: "file.go", Selected: []git.DiffLine{line}, Surrounding: git.DiffHunk{ID: anchor.HunkID, Old: git.DiffRange{ID: strings.Repeat("f", 64)}, New: git.DiffRange{ID: strings.Repeat("1", 64), Start: 1, Count: 1}, Lines: []git.DiffLine{line}}}
	thread := question.SavedThread{ID: strings.Repeat("2", 64), RevisionID: b.RevisionID, Context: context}
	thread.Answers = []question.Answer{{ID: strings.Repeat("3", 64), ThreadID: thread.ID, PRID: s.PRID, RevisionID: b.RevisionID, Question: "Explain literal source", Text: "Partial fixture answer", State: question.Streaming, Reference: question.Reference{RevisionID: b.RevisionID, Anchor: anchor, NewPath: "file.go"}}}
	s.Threads = append(s.Threads, thread)
	return s
}

func TestQuestionStoreReopensTranscriptWithoutCacheOrNativeState(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	s := questionTranscriptFixture(t, i, paths.Cache)
	if err := i.saveQuestions(s, 0); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(paths.Data, "reviews", s.PRID)
	orphan := filepath.Join(dir, ".threads-unrecognized.tmp")
	if err := os.WriteFile(orphan, []byte("preserve diagnostic"), 0600); err != nil {
		t.Fatal(err)
	}
	i.close()
	i = ownedPRInstance(t, paths.Data)
	t.Setenv("PATH", "")
	got, err := i.ReadQuestions(s.PRID)
	if err != nil || len(got.Threads) != 1 || got.Threads[0].Answers[0].Text != "Partial fixture answer" || got.Threads[0].Answers[0].State != question.Interrupted || !got.Threads[0].Answers[0].ExecutionUnknown || got.Threads[0].Answers[0].FailureCode != "runtime-restarted" {
		t.Fatalf("cold transcript: %v", err)
	}
	got.Threads[0].Answers[0].Text = "caller mutation"
	again, err := i.ReadQuestions(s.PRID)
	if err != nil || again.Threads[0].Answers[0].Text != "Partial fixture answer" {
		t.Fatal("returned slices alias storage")
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatal("unrecognized temporary removed")
	}
	if i.questions != nil || i.cache != nil {
		t.Fatal("history read initialized runtime/cache")
	}
	if _, err := os.Stat(paths.Cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("history read recreated source")
	}
}
