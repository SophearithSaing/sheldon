package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/semantic"
)

func TestSemanticPrivateSaveReopenAndFailure(t *testing.T) {
	f := newQuestionFixture(t, "semantic-valid")
	empty, err := f.owner.ReadSemantic(f.pr.ID)
	if err != nil || empty.Version != 0 || f.owner.semantics != nil {
		t.Fatal("empty read initialized work")
	}
	admitted, err := f.service.GenerateSemantic(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	end := waitSemantic(t, f.owner.semantics, admitted)
	if end.State != "completed" || end.SavedVersion != "1" {
		t.Fatalf("save: %+v", end)
	}
	saved, err := f.owner.ReadSemantic(f.pr.ID)
	if err != nil || saved.ReviewID != admitted.ReviewID {
		t.Fatalf("saved: %v", err)
	}
	path := filepath.Join(f.paths.Data, "reviews", f.pr.ID, "semantic-review.json")
	before, _ := os.ReadFile(path)
	if err := f.owner.SaveSemanticContent(f.pr.ID, strings.Repeat("d", 64), saved.Content, 0); !errors.Is(err, semantic.ErrConflict) {
		t.Fatal("stale write accepted")
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, before) {
		t.Fatal("conflict changed prior bytes")
	}
	if err := f.owner.close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	owner := ownedPRInstance(t, f.paths.Data)
	again, err := owner.ReadSemantic(f.pr.ID)
	if err != nil || again.ReviewID != saved.ReviewID || owner.cache != nil || owner.semantics != nil {
		t.Fatalf("source-free reopen: %v", err)
	}
	if err := os.WriteFile(path, []byte("corrupt preserved"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ReadSemantic(f.pr.ID); !errors.Is(err, semantic.ErrStore) {
		t.Fatal("corrupt storage healthy")
	}
	if b, _ := os.ReadFile(path); string(b) != "corrupt preserved" {
		t.Fatal("corrupt bytes reset")
	}
}

func TestSemanticUnsafeLeafAndReplacementUncertainty(t *testing.T) {
	f := newQuestionFixture(t, "semantic-valid")
	a, err := f.service.GenerateSemantic(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	if end := waitSemantic(t, f.owner.semantics, a); end.State != "completed" {
		t.Fatalf("generation: %+v", end)
	}
	saved, err := f.owner.ReadSemantic(f.pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.paths.Data, "reviews", f.pr.ID, "semantic-review.json")
	retained := path + ".retained"
	if err := os.Rename(path, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ReadSemantic(f.pr.ID); !errors.Is(err, semantic.ErrStore) {
		t.Fatal("directory read accepted")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	data, _ := saved.Encode()
	if err := replaceSemanticFile(root, data); !errors.Is(err, semantic.ErrSaveUncertain) {
		t.Fatal("rename failure not uncertain")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, path); err != nil {
		t.Fatal(err)
	}
	f.owner.semanticStoreUncertain = map[string]bool{f.pr.ID: true}
	if err := f.owner.CheckSemantic(f.pr.ID, 1); !errors.Is(err, semantic.ErrSaveUncertain) {
		t.Fatal("admission cleared uncertainty")
	}
	if _, err := f.owner.ReadSemantic(f.pr.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.CheckSemantic(f.pr.ID, 1); err != nil {
		t.Fatal("explicit read did not resolve storage uncertainty")
	}
}
