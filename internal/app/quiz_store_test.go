package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/quiz"
)

// quizStoreFixture supplies readable saved evidence with no original cache/native state.
func quizStoreFixture(t *testing.T, i *instance, url string) quiz.Saved {
	t.Helper()
	pr, err := i.RegisterPR(url)
	if err != nil {
		t.Fatal(err)
	}
	r := githubpr.Revision{PRID: pr.ID, BaseBranch: "target", BaseSource: githubpr.GitHubBase, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("a", 40)}
	id, _ := r.ID()
	a := quiz.Anchor{FileID: strings.Repeat("c", 64), HunkID: strings.Repeat("d", 64), Side: "new", Start: 1, End: 1}
	line := git.DiffLine{ID: strings.Repeat("e", 64), Sign: "+", NewLine: 1, Text: "<literal source>界\r", NoNewline: true}
	e := quiz.Evidence{Anchor: a, NewPath: "file.go", Selected: []git.DiffLine{line}, Surrounding: git.DiffHunk{ID: a.HunkID, Old: git.DiffRange{ID: strings.Repeat("f", 64)}, New: git.DiffRange{ID: strings.Repeat("1", 64), Start: 1, Count: 1}, Lines: []git.DiffLine{line}}}
	s := quiz.Saved{SchemaVersion: 1, PRID: pr.ID, Version: 1, QuizID: strings.Repeat("2", 64), Content: quiz.Content{Revision: r, RevisionID: id, Questions: []quiz.Question{}}}
	for n := range 5 {
		s.Questions = append(s.Questions, quiz.Question{ID: fmt.Sprintf("q%d", n+1), Prompt: fmt.Sprintf("Boundary %d?", n+1), Choices: []quiz.Choice{{ID: "a", Text: "First"}, {ID: "b", Text: "Second"}, {ID: "c", Text: "Third"}, {ID: "d", Text: "Fourth"}}, CorrectChoiceID: "b", Explanation: "Concise explanation.", Evidence: []quiz.Evidence{e}})
	}
	if _, err := s.Encode(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestQuizStoreReopensWithoutSourceOrNativeAndPreservesSiblingFiles(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
	if empty, err := i.ReadQuiz(s.PRID); err != nil || empty.Version != 0 {
		t.Fatal("missing quiz not empty")
	}
	if _, err := os.Stat(filepath.Join(paths.Data, "reviews")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created parents")
	}
	if err := i.SaveQuiz(s, 0); err != nil {
		t.Fatal(err)
	}
	other := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/2")
	if err := i.SaveQuiz(other, 0); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(paths.Data, "reviews", s.PRID)
	orphan := filepath.Join(dir, ".quiz-unrecognized.tmp")
	if err := os.WriteFile(orphan, []byte("keep diagnostic"), 0600); err != nil {
		t.Fatal(err)
	}
	thread := questionStoreFixture(t, i)
	if err := i.SaveQuestions(thread, 0); err != nil {
		t.Fatal(err)
	}
	threadPath := filepath.Join(dir, "threads.json")
	before, _ := os.ReadFile(threadPath)
	i.close()
	i = ownedPRInstance(t, paths.Data)
	t.Setenv("PATH", "")
	got, err := i.ReadQuiz(s.PRID)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("reopen: %v", err)
	}
	got.Questions[0].Choices[0].Text = "caller edit"
	again, err := i.ReadQuiz(s.PRID)
	if err != nil || again.Questions[0].Choices[0].Text == "caller edit" {
		t.Fatal("read aliases saved data")
	}
	if _, err := i.ReadQuiz(other.PRID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(threadPath)
	if !bytes.Equal(before, after) {
		t.Fatal("quiz read changed question history")
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatal("unknown temporary removed")
	}
	if i.cache != nil || i.questions != nil || i.quizzes != nil {
		t.Fatal("read initialized source/native state")
	}
	if _, err := os.Stat(paths.Cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created source")
	}
}

func TestQuizStoreSerializesWholeReplacementAndVersions(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
	if err := i.SaveQuiz(s, 0); err != nil {
		t.Fatal(err)
	}
	next := s
	next.Version = 2
	next.QuizID = strings.Repeat("3", 64)
	var wg sync.WaitGroup
	wins := make(chan bool, 12)
	for range 12 {
		wg.Go(func() {
			err := i.SaveQuiz(next, 1)
			wins <- err == nil
			if err != nil && !errors.Is(err, quiz.ErrConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(wins)
	count := 0
	for win := range wins {
		if win {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d replacements won", count)
	}
	if err := i.CheckQuiz(s.PRID, 1); !errors.Is(err, quiz.ErrConflict) {
		t.Fatal("stale admission allowed")
	}
	if err := i.CheckQuiz(s.PRID, 2); err != nil {
		t.Fatal(err)
	}
	if err := i.SaveQuiz(next, 2); !errors.Is(err, quiz.ErrConflict) {
		t.Fatal("same generation accepted")
	}
	for n := uint64(3); n < 9; n++ {
		next.Version = n
		next.QuizID = fmt.Sprintf("%064x", n+100)
		if err := i.SaveQuiz(next, n-1); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(i.file.Name()), "reviews", s.PRID, "quiz.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := quiz.DecodeSaved(data, s.PRID); err != nil || got.Version != n {
			t.Fatal("partial generation visible")
		}
	}
}

func TestQuizStorePreservesBadFilesOwnershipAndUncertainty(t *testing.T) {
	for _, kind := range []string{"corrupt", "future", "alias", "oversized", "public", "directory"} {
		t.Run(kind, func(t *testing.T) {
			i := ownedPRInstance(t, testPaths(t).Data)
			s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
			if err := i.SaveQuiz(s, 0); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(filepath.Dir(i.file.Name()), "reviews", s.PRID, "quiz.json")
			data, _ := s.Encode()
			switch kind {
			case "corrupt":
				data = []byte("{broken")
			case "future":
				data = bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":2`), 1)
			case "alias":
				data = bytes.Replace(data, []byte(`"quizID":`), []byte(`"QuizID":`), 1)
			case "oversized":
				data = bytes.Repeat([]byte(" "), quiz.MaxContentBytes+1)
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "directory" {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "public" {
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "public" && runtime.GOOS == "windows" {
				t.Skip("Unix mode-specific case")
			}
			i.quizStoreUncertain = map[string]bool{s.PRID: true}
			if _, err := i.ReadQuiz(s.PRID); err == nil {
				t.Fatal("unsafe state read as healthy")
			}
			if !i.quizStoreUncertain[s.PRID] {
				t.Fatal("invalid read cleared storage uncertainty")
			}
			if err := i.CheckQuiz(s.PRID, 1); err == nil {
				t.Fatal("unsafe state admitted generation")
			}
			s.Version++
			s.QuizID = strings.Repeat("4", 64)
			if err := i.SaveQuiz(s, 1); err == nil {
				t.Fatal("overwrote unsafe state")
			}
			if kind != "directory" {
				after, _ := os.ReadFile(path)
				if !bytes.Equal(data, after) {
					t.Fatal("diagnostic bytes changed")
				}
			}
		})
	}
	i := ownedPRInstance(t, testPaths(t).Data)
	s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
	if err := i.SaveQuiz(s, 0); err != nil {
		t.Fatal(err)
	}
	i.quizStoreUncertain = map[string]bool{s.PRID: true}
	if err := i.CheckQuiz(s.PRID, 1); !errors.Is(err, quiz.ErrSaveUncertain) {
		t.Fatal("admission cleared uncertainty")
	}
	next := s
	next.Version = 2
	next.QuizID = strings.Repeat("5", 64)
	if err := i.SaveQuiz(next, 1); !errors.Is(err, quiz.ErrSaveUncertain) {
		t.Fatal("uncertainty allowed mutation")
	}
	if _, err := i.ReadQuiz(s.PRID); err != nil {
		t.Fatal(err)
	}
	if err := i.SaveQuiz(next, 1); err != nil {
		t.Fatal("explicit checked read did not release storage gate")
	}
	for _, id := range []string{"../private", strings.Repeat("f", 64)} {
		if _, err := i.ReadQuiz(id); err == nil {
			t.Fatal("invalid/unregistered read allowed")
		}
	}
	contender, err := openInstance(filepath.Dir(i.file.Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer contender.close()
	if _, err := contender.ReadQuiz(s.PRID); err == nil {
		t.Fatal("non-owner read allowed")
	}
	i.close()
	if err := i.SaveQuiz(next, 1); err == nil {
		t.Fatal("closed owner saved")
	}
}

func TestQuizStoreRawReadersSeeOnlyWholeGenerations(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
	if err := i.SaveQuiz(s, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(i.file.Name()), "reviews", s.PRID, "quiz.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 1)
	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		defer close(done)
		close(started)
		for ctx.Err() == nil {
			data, err := os.ReadFile(path)
			if err == nil {
				_, err = quiz.DecodeSaved(data, s.PRID)
			}
			if err != nil {
				errs <- err
				return
			}
		}
	}()
	<-started
	for version := uint64(2); version < 12; version++ {
		s.Version = version
		s.QuizID = fmt.Sprintf("%064x", version+100)
		if err := i.SaveQuiz(s, version-1); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	<-done
	select {
	case err := <-errs:
		t.Fatalf("raw reader saw partial data: %v", err)
	default:
	}
}

func TestQuizVersionOverflowDoesNotWrite(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	s := quizStoreFixture(t, i, "https://github.com/owner/repo/pull/1")
	s.Version = ^uint64(0)
	data, _ := s.Encode()
	root, err := i.questionDirectory(s.PRID, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := replaceQuizFile(root, data); err != nil {
		t.Fatal(err)
	}
	if err := i.CheckQuiz(s.PRID, s.Version); !errors.Is(err, quiz.ErrLimit) {
		t.Fatal("version overflow admitted")
	}
	got, err := i.ReadQuiz(s.PRID)
	if err != nil || got.Version != s.Version {
		t.Fatal("max version could not reopen")
	}
	after, _ := root.ReadFile("quiz.json")
	if !bytes.Equal(data, after) {
		t.Fatal("overflow changed saved bytes")
	}
	var raw map[string]any
	if json.Unmarshal(after, &raw) != nil {
		t.Fatal("invalid saved JSON")
	}
}
