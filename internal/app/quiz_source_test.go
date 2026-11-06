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
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/github"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/quiz"
)

// quizResult encodes synthetic structured output with references, never model-written source quotes.
func quizResult(t *testing.T, anchors []quiz.Anchor) []byte {
	t.Helper()
	questions := []map[string]any{}
	for i := range 5 {
		questions = append(questions, map[string]any{
			"prompt": fmt.Sprintf("What does changed boundary %d do?", i+1), "choices": []string{"First", "Second", "Third", "Fourth"},
			"correctChoiceID": "b", "explanation": "A synthetic explanation for structural validation only.", "evidence": []quiz.Anchor{anchors[i%len(anchors)]},
		})
	}
	data, err := json.Marshal(map[string]any{"questions": questions})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// assertQuizSourceReadOnly checks the offline transport audit and absence of native startup/storage.
func assertQuizSourceReadOnly(t *testing.T, f questionFixture, offset int) {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("SHELDON_GIT_AUDIT"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(data[offset:]), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Operation string
			Safe      bool
		}
		if json.Unmarshal(line, &entry) != nil || !entry.Safe {
			t.Fatal("unsafe quiz source Git call")
		}
		switch entry.Operation {
		case "rev-parse", "for-each-ref", "cat-file", "rev-list", "merge-base", "ls-tree", "ls-files", "diff":
		default:
			t.Fatalf("quiz source fetched/mutated Git: %s", entry.Operation)
		}
	}
	if len(nativeQuestionAudit(t, f.audit)) != 0 || f.owner.questions != nil {
		t.Fatal("quiz source started native/question runtime work")
	}
	for _, name := range []string{"quiz.json", "threads.json"} {
		if _, err := os.Lstat(filepath.Join(f.paths.Data, "reviews", f.pr.ID, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("source-only quiz operation wrote %s", name)
		}
	}
}

func TestQuizSourceRetainsOriginalRevisionAcrossRefreshAndRefusesLostSource(t *testing.T) {
	f := newQuestionFixture(t, "")
	anchor := quiz.Anchor{FileID: f.input.Anchor.FileID, HunkID: f.input.Anchor.HunkID, Side: "old", Start: 1, End: 1}
	audit, _ := os.ReadFile(os.Getenv("SHELDON_GIT_AUDIT"))
	source, err := quiz.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, f.input.Revision)
	if err != nil {
		t.Fatal(err)
	}
	content, err := source.Parse(quizResult(t, []quiz.Anchor{anchor}))
	if err != nil || content.Questions[0].Evidence[0].Selected[0].Text != "old literal" || content.Revision.MergeBaseSHA != f.base || content.Revision.BaseSHA == f.base {
		t.Fatalf("quiz old side did not use merge base: %v", err)
	}
	assertQuizSourceReadOnly(t, f, len(audit))
	newBase := sourceGit(t, f.realGit, f.upstream, "base-only\n", "commit-tree", f.tree, "-p", f.input.Revision.BaseSHA)
	baseInput := prepareQuestionRevision(t, f, f.pr, newBase, f.head)
	newHead := sourceGit(t, f.realGit, f.upstream, "head-only\n", "commit-tree", f.tree, "-p", f.base)
	headInput := prepareQuestionRevision(t, f, f.pr, newBase, newHead)
	for _, input := range []githubpr.Revision{baseInput.Revision, headInput.Revision} {
		newSource, err := quiz.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := newSource.Parse(quizResult(t, []quiz.Anchor{anchor})); !errors.Is(err, quiz.ErrOutput) {
			t.Fatal("old evidence accepted on refreshed revision")
		}
	}
	audit, _ = os.ReadFile(os.Getenv("SHELDON_GIT_AUDIT"))
	original, err := quiz.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, f.input.Revision)
	if err != nil || original.Prompt() != source.Prompt() || original.Directory() != source.Directory() {
		t.Fatalf("original quiz source rebound after refresh: %v", err)
	}
	again, err := original.Parse(quizResult(t, []quiz.Anchor{anchor}))
	if err != nil || !reflect.DeepEqual(again, content) {
		t.Fatal("original quotes changed after refresh")
	}
	assertQuizSourceReadOnly(t, f, len(audit))
	if sourceGit(t, f.realGit, source.Directory(), "", "rev-parse", "HEAD") != f.head {
		t.Fatal("quiz source moved original HEAD")
	}
	file := filepath.Join(source.Directory(), "source.txt")
	if err := os.WriteFile(file, []byte("keep edits\n"), 0600); err != nil {
		t.Fatal(err)
	}
	audit, _ = os.ReadFile(os.Getenv("SHELDON_GIT_AUDIT"))
	if got, err := quiz.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, f.input.Revision); !errors.Is(err, quiz.ErrSource) || got != nil {
		t.Fatal("dirty source accepted/repaired")
	}
	if data, _ := os.ReadFile(file); string(data) != "keep edits\n" {
		t.Fatal("quiz resolution overwrote edits")
	}
	assertQuizSourceReadOnly(t, f, len(audit))
	if err := os.Rename(source.Directory(), source.Directory()+".saved"); err != nil {
		t.Fatal(err)
	}
	if got, err := quiz.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, f.input.Revision); !errors.Is(err, quiz.ErrSource) || got != nil {
		t.Fatal("missing source accepted/recreated")
	}
	if _, err := os.Lstat(source.Directory()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing original worktree recreated")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := quiz.Resolve(ctx, f.pr.ID, f.owner, f.owner.cache, f.input.Revision); !errors.Is(err, context.Canceled) {
		t.Fatalf("source cancellation cause lost: %v", err)
	}
}

func TestQuizEvidenceUsesNativeAddedDeletedRenamedAndNonTextInventory(t *testing.T) {
	f := newQuestionFixture(t, "")
	oldText := strings.Repeat("same context\n", 40) + "old boundary\n"
	oldBlob := sourceGit(t, f.realGit, f.upstream, oldText, "hash-object", "-w", "--stdin")
	deletedBlob := sourceGit(t, f.realGit, f.upstream, "removed <literal>\r\n", "hash-object", "-w", "--stdin")
	baseTree := sourceGit(t, f.realGit, f.upstream, "100644 blob "+oldBlob+"\told name.txt\n100644 blob "+deletedBlob+"\tremoved.txt\n", "mktree")
	base := sourceGit(t, f.realGit, f.upstream, "quiz base\n", "commit-tree", baseTree, "-p", f.head)
	newBlob := sourceGit(t, f.realGit, f.upstream, strings.Repeat("same context\n", 40)+"new boundary\n", "hash-object", "-w", "--stdin")
	addedBlob := sourceGit(t, f.realGit, f.upstream, "added <script>界\r", "hash-object", "-w", "--stdin")
	binaryBlob := sourceGit(t, f.realGit, f.upstream, "binary\x00bytes", "hash-object", "-w", "--stdin")
	headTree := sourceGit(t, f.realGit, f.upstream, "100644 blob "+newBlob+"\tnew name.txt\n100644 blob "+addedBlob+"\tadded.txt\n100644 blob "+binaryBlob+"\timage.bin\n", "mktree")
	head := sourceGit(t, f.realGit, f.upstream, "quiz head\n", "commit-tree", headTree, "-p", base)
	baseTip := sourceGit(t, f.realGit, f.upstream, "advanced base\n", "commit-tree", f.tree, "-p", base)
	sourceGit(t, f.realGit, f.upstream, "", "update-ref", "refs/heads/quiz-base", baseTip)
	sourceGit(t, f.realGit, f.upstream, "", "update-ref", "refs/pull/31/head", head)
	resolution := github.Resolution{PR: f.pr, State: github.Resolved, Base: &github.Base{Branch: "quiz-base", SHA: baseTip, Source: githubpr.GitHubBase}, HeadSHA: head}
	snapshot, err := f.owner.cache.Fetch(context.Background(), resolution)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := f.owner.cache.Prepare(context.Background(), f.pr, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := f.owner.cache.Diff(context.Background(), f.pr, prepared.Revision)
	if err != nil || len(diff.Files) != 4 {
		t.Fatalf("native diff fixture: %v", err)
	}
	audit, _ := os.ReadFile(os.Getenv("SHELDON_GIT_AUDIT"))
	source, err := quiz.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, prepared.Revision)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]bool{}
	anchors := []quiz.Anchor{}
	wanted := []git.DiffLine{}
	var binary quiz.Anchor
	for _, file := range diff.Files {
		statuses[file.Status] = true
		if file.Kind != "text" {
			binary = quiz.Anchor{FileID: file.ID, HunkID: strings.Repeat("a", 64), Side: "new", Start: 1, End: 1}
			continue
		}
		for _, hunk := range file.Hunks {
			for _, line := range hunk.Lines {
				anchor := quiz.Anchor{FileID: file.ID, HunkID: hunk.ID, Side: "new", Start: line.NewLine, End: line.NewLine}
				if line.Sign == "-" {
					anchor.Side, anchor.Start, anchor.End = "old", line.OldLine, line.OldLine
				} else if line.Sign != "+" {
					continue
				}
				anchors = append(anchors, anchor)
				wanted = append(wanted, line)
			}
		}
	}
	if !statuses["renamed"] || !statuses["deleted"] || !statuses["added"] || len(anchors) != 4 {
		t.Fatal("fixture lacks expected native change kinds")
	}
	content, err := source.Parse(quizResult(t, anchors))
	if err != nil || content.Revision.MergeBaseSHA != base || content.Revision.BaseSHA != baseTip {
		t.Fatalf("native quiz content: %v", err)
	}
	for i, question := range content.Questions {
		if len(question.Evidence[0].Selected) != 1 || question.Evidence[0].Selected[0] != wanted[i%len(wanted)] {
			t.Fatal("literal native source/coordinates/final-LF changed")
		}
	}
	if !strings.Contains(source.Prompt(), `"kind":"binary"`) {
		t.Fatal("prompt dropped non-text source inventory")
	}
	if _, err := source.Parse(quizResult(t, []quiz.Anchor{binary})); !errors.Is(err, quiz.ErrOutput) {
		t.Fatal("binary file accepted as line evidence")
	}
	assertQuizSourceReadOnly(t, f, len(audit))
}
