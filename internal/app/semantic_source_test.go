package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/semantic"
)

func TestSemanticSourceUsesPinnedGitAndRefusesRepair(t *testing.T) {
	f := newQuestionFixture(t, "")
	source, err := semantic.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, f.input.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a := semantic.Anchor{FileID: f.input.Anchor.FileID, HunkID: f.input.Anchor.HunkID, Side: "old", Start: 1, End: 1}
	data, _ := json.Marshal(map[string]any{"groups": []map[string]any{{"title": "Removed behavior", "explanation": "Synthetic grouping, not inference.", "ranges": []map[string]any{{"path": "source.txt", "side": "old", "start": 1, "end": 1}}}}})
	c, err := source.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := c.Lines(a)
	if err != nil || lines[0].Text != "old literal" || c.Diff.Revision.MergeBaseSHA != f.base || c.Diff.Revision.BaseSHA == f.base {
		t.Fatalf("wrong merge-base source: %v", err)
	}
	if len(nativeQuestionAudit(t, f.audit)) != 0 || f.owner.questions != nil {
		t.Fatal("source read started native questions")
	}
	path := filepath.Join(source.Directory(), "source.txt")
	if err := os.WriteFile(path, []byte("preserve edits\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := semantic.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, f.input.Revision); !errors.Is(err, semantic.ErrSource) || got != nil {
		t.Fatal("dirty source accepted")
	}
	if b, _ := os.ReadFile(path); string(b) != "preserve edits\n" {
		t.Fatal("source repaired")
	}
	if err := os.Rename(source.Directory(), source.Directory()+".unavailable"); err != nil {
		t.Fatal(err)
	}
	if got, err := semantic.Resolve(context.Background(), f.pr.ID, f.owner, f.owner.cache, f.input.Revision); !errors.Is(err, semantic.ErrSource) || got != nil {
		t.Fatal("missing source accepted")
	}
	if _, err := os.Lstat(source.Directory()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source recreated")
	}
}
