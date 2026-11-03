package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/process"
)

type diffSource struct{ mode, data, object string }

func TestDiffDoesNotInitializeMissingRepository(t *testing.T) {
	f, c, revision := diffFixture(t, nil, map[string]diffSource{"source": {data: "literal\n"}})
	id, _ := f.resolution.PR.RepositoryID()
	repo := filepath.Join(f.paths.Cache, "repositories", id+".git")
	if err := os.Rename(repo, repo+".saved"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Rename(repo+".saved", repo); err != nil {
			t.Error(err)
		}
	}()
	got, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if !errors.Is(err, ErrIncompleteDiff) || got.Complete {
		t.Fatal("missing repository returned a complete diff")
	}
	if _, err := os.Stat(repo); !os.IsNotExist(err) {
		t.Fatal("diff initialized an empty replacement repository")
	}
}

// diffTree builds a native tree with literal top-level names, including unusual bytes.
func diffTree(t *testing.T, f fixture, files map[string]diffSource) string {
	t.Helper()
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var manifest strings.Builder
	for _, name := range names {
		file := files[name]
		if file.mode == "" {
			file.mode = "100644"
		}
		kind := "blob"
		if file.mode == "160000" {
			kind = "commit"
		}
		if file.object == "" {
			file.object = native(t, f.upstream, file.data, "hash-object", "-w", "--stdin")
		}
		fmt.Fprintf(&manifest, "%s %s %s\t%s\x00", file.mode, kind, file.object, name)
	}
	return native(t, f.upstream, manifest.String(), "mktree", "-z", "--missing")
}

// diffFixture fetches divergent tips so the base tip cannot be mistaken for the merge base.
func diffFixture(t *testing.T, oldFiles, newFiles map[string]diffSource) (fixture, *Cache, githubpr.Revision) {
	t.Helper()
	f := newFixture(t)
	base := native(t, f.upstream, "comparison base\n", "commit-tree", diffTree(t, f, oldFiles), "-p", f.base)
	head := native(t, f.upstream, "comparison head\n", "commit-tree", diffTree(t, f, newFiles), "-p", base)
	baseTip := native(t, f.upstream, "base only\n", "commit-tree", f.tree, "-p", base)
	native(t, f.upstream, "", "update-ref", "refs/heads/feat/target", baseTip)
	native(t, f.upstream, "", "update-ref", "refs/pull/1/head", head)
	f.resolution.Base.SHA, f.resolution.HeadSHA = baseTip, head
	c := openFixture(t, f)
	if _, err := c.Fetch(context.Background(), f.resolution); err != nil {
		t.Fatal(err)
	}
	return f, c, githubpr.Revision{PRID: f.resolution.PR.ID, BaseBranch: f.resolution.Base.Branch, BaseSource: f.resolution.Base.Source, BaseSHA: baseTip, HeadSHA: head, MergeBaseSHA: base}
}

func TestDiffLiteralHunksCoordinatesAndPinnedIdentity(t *testing.T) {
	var old, new strings.Builder
	for n := 1; n <= 30; n++ {
		fmt.Fprintf(&old, "line %d\n", n)
		if n == 2 || n == 25 {
			fmt.Fprintf(&new, "<script>literal %d</script>\r\n", n)
		} else {
			fmt.Fprintf(&new, "line %d\n", n)
		}
	}
	f, c, revision := diffFixture(t, map[string]diffSource{"source.txt": {data: old.String()}}, map[string]diffSource{"source.txt": {data: new.String()}})
	got, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete || got.Revision != revision || got.Revision.BaseSHA == got.Revision.MergeBaseSHA || len(got.Files) != 1 {
		t.Fatalf("comparison identity: %+v", got)
	}
	file := got.Files[0]
	if file.Status != "modified" || file.Kind != "text" || len(file.Hunks) != 2 {
		t.Fatalf("file: %+v", file)
	}
	for _, hunk := range file.Hunks {
		oldCount, newCount := 0, 0
		for _, line := range hunk.Lines {
			if line.OldLine != 0 && line.Text != fmt.Sprintf("line %d", line.OldLine) {
				t.Fatal("old text is not at its fixture coordinate")
			}
			if line.Sign == "+" && line.Text != fmt.Sprintf("<script>literal %d</script>\r", line.NewLine) {
				t.Fatal("new text is not at its fixture coordinate")
			}
			if line.Sign != "+" {
				if line.OldLine != hunk.Old.Start+oldCount {
					t.Fatal("wrong old coordinate")
				}
				oldCount++
			} else if line.OldLine != 0 {
				t.Fatal("addition has old coordinate")
			}
			if line.Sign != "-" {
				if line.NewLine != hunk.New.Start+newCount {
					t.Fatal("wrong new coordinate")
				}
				newCount++
			} else if line.NewLine != 0 {
				t.Fatal("deletion has new coordinate")
			}
			if line.Sign == "+" && (!strings.HasPrefix(line.Text, "<script>") || !strings.HasSuffix(line.Text, "\r")) {
				t.Fatal("literal text was transformed")
			}
		}
		if oldCount != hunk.Old.Count || newCount != hunk.New.Count {
			t.Fatal("wrong hunk counts")
		}
	}
	again, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("unchanged diff references are unstable")
	}
	native(t, f.upstream, "", "update-ref", "refs/pull/1/head", f.fork)
	root := filepath.Join(f.paths.Cache, "worktrees", revision.PRID, revision.HeadSHA)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("unrelated worktree edit"), 0600); err != nil {
		t.Fatal(err)
	}
	again, err = c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("moving refs or local source changed pinned diff")
	}
	other := revision
	other.BaseSHA = revision.MergeBaseSHA
	changed, err := c.Diff(context.Background(), f.resolution.PR, other)
	if err != nil || changed.RevisionID == got.RevisionID || changed.Files[0].ID == file.ID || changed.Files[0].Hunks[0].Lines[0].ID == file.Hunks[0].Lines[0].ID {
		t.Fatal("references are not revision scoped")
	}
}

func TestDiffAddsDeletesRenamesEmptyAndNoFinalNewline(t *testing.T) {
	old := map[string]diffSource{"deleted": {data: "removed\n"}, "old name": {data: "unchanged rename\n"}, "no newline": {data: "before"}, "empty deleted": {}}
	new := map[string]diffSource{"added": {data: "added\n"}, "new name": {data: "unchanged rename\n"}, "no newline": {data: "after"}, "empty added": {}}
	f, c, revision := diffFixture(t, old, new)
	got, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]int{}
	for _, file := range got.Files {
		statuses[file.Status]++
		switch file.NewPath {
		case "new name":
			if file.OldPath != "old name" || file.Similarity != 100 || file.Kind != "metadata-only" {
				t.Fatalf("rename: %+v", file)
			}
		case "no newline":
			lines := file.Hunks[0].Lines
			if len(lines) != 2 || !lines[0].NoNewline || !lines[1].NoNewline || lines[0].Text != "before" || lines[1].Text != "after" {
				t.Fatal("no-final-newline markers lost")
			}
		case "added":
			if file.Hunks[0].Old.Start != 0 || file.Hunks[0].Old.Count != 0 || file.Hunks[0].New.Start != 1 || file.Hunks[0].New.Count != 1 {
				t.Fatal("wrong added range")
			}
		}
		if file.Status == "deleted" && file.OldPath == "deleted" && file.Hunks[0].New.Count != 0 {
			t.Fatal("wrong deleted range")
		}
	}
	if statuses["added"] != 1 || statuses["deleted"] != 1 || statuses["renamed"] != 2 || statuses["modified"] != 1 {
		t.Fatalf("unexpected changes: %+v", statuses)
	}
	empty := revision
	empty.BaseSHA, empty.HeadSHA, empty.MergeBaseSHA = revision.HeadSHA, revision.HeadSHA, revision.HeadSHA
	blank, err := c.Diff(context.Background(), f.resolution.PR, empty)
	if err != nil || !blank.Complete || len(blank.Files) != 0 {
		t.Fatalf("empty diff: %+v %v", blank, err)
	}
	encoded, _ := json.Marshal(blank)
	if !strings.Contains(string(encoded), `"files":[]`) {
		t.Fatal("empty files must serialize as an array")
	}
}

func TestDiffNonTextModeAndTypeChanges(t *testing.T) {
	f := newFixture(t)
	old := map[string]diffSource{"binary": {data: "old\x00bytes"}, "mode": {data: "same\n"}, "link": {mode: "120000", data: "old"}, "module": {mode: "160000", object: f.base}, "type": {data: "file\n"}, "encoding": {data: "old\n"}}
	new := map[string]diffSource{"binary": {data: "new\x00bytes"}, "mode": {mode: "100755", data: "same\n"}, "link": {mode: "120000", data: "new"}, "module": {mode: "160000", object: f.head}, "type": {mode: "120000", data: "target"}, "encoding": {data: "invalid \xff\n"}}
	f, c, revision := diffFixture(t, old, new)
	got, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{"binary": "binary", "mode": "mode-only", "link": "symlink", "module": "submodule", "type": "type-change", "encoding": "unsupported"}
	for _, file := range got.Files {
		if file.Kind != kinds[file.NewPath] || len(file.Hunks) != 0 {
			t.Fatalf("non-text change invented text: %+v", file)
		}
	}
	if len(got.Files) != len(kinds) {
		t.Fatal("non-text file missing")
	}
}

func TestDiffUnusualPathsAreLiteralDisplayData(t *testing.T) {
	names := []string{"space name", "日本語.go", "quote\"name", "tab\tname", "line\nname", "slash\\name", ":(glob)*", "<script>.txt", "trailing "}
	old, new := map[string]diffSource{}, map[string]diffSource{}
	for _, name := range names {
		old[name] = diffSource{data: "old\n"}
		new[name] = diffSource{data: "new\n"}
	}
	f, c, revision := diffFixture(t, old, new)
	got, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range got.Files {
		if _, ok := old[file.OldPath]; !ok || file.NewPath != file.OldPath || len(file.Hunks) != 1 {
			t.Fatalf("path changed: %+v", file)
		}
	}
	if len(got.Files) != len(names) {
		t.Fatal("unusual path lost")
	}
}

func TestDiffFailureNeverReturnsPartialSuccess(t *testing.T) {
	f, c, revision := diffFixture(t, map[string]diffSource{"file": {data: "old\n"}}, map[string]diffSource{"file": {data: "new\n"}})
	for _, mode := range []string{"diff-fail", "diff-overflow", "diff-hang", "diff-truncated"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SHELDON_GIT_MODE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := c.Diff(ctx, f.resolution.PR, revision)
			if !errors.Is(err, ErrIncompleteDiff) || got.Complete || got.Files != nil || got.RevisionID != "" {
				t.Fatal("failed diff returned partial success")
			}
			if mode == "diff-overflow" && !errors.Is(err, process.ErrOutputLimit) {
				t.Fatal("output-limit evidence lost")
			}
			if mode == "diff-hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("cancellation evidence lost")
			}
			if strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("native diagnostics leaked")
			}
		})
	}
	wrong := revision
	wrong.MergeBaseSHA = revision.HeadSHA
	if _, err := c.Diff(context.Background(), f.resolution.PR, wrong); !errors.Is(err, ErrIncompleteDiff) {
		t.Fatal("forged merge base accepted")
	}
}

func TestDiffNativeOutputLimitAndInvalidUTF8Paths(t *testing.T) {
	t.Run("native output", func(t *testing.T) {
		f, c, revision := diffFixture(t, map[string]diffSource{"large": {data: strings.Repeat("a", (2<<20)+100) + "\n"}}, map[string]diffSource{"large": {data: strings.Repeat("b", (2<<20)+100) + "\n"}})
		got, err := c.Diff(context.Background(), f.resolution.PR, revision)
		if !errors.Is(err, ErrIncompleteDiff) || !errors.Is(err, process.ErrOutputLimit) || got.Complete || got.Files != nil {
			t.Fatal("native oversized diff reported success")
		}
	})
	t.Run("non UTF8 path", func(t *testing.T) {
		f, c, revision := diffFixture(t, nil, map[string]diffSource{"invalid\xff": {data: "new\n"}})
		got, err := c.Diff(context.Background(), f.resolution.PR, revision)
		if !errors.Is(err, ErrIncompleteDiff) || got.Complete || got.Files != nil {
			t.Fatal("unrepresentable path was silently altered")
		}
	})
}

func TestDiffEditedRenameBinaryPathsAndSingleSidedFinalLF(t *testing.T) {
	old := map[string]diffSource{"old\nname": {data: "one\ntwo\nthree\nfour\nfive\n"}, "old binary 日本語": {data: "old\x00binary"}, "old LF": {data: "same"}, "new LF": {data: "same\n"}, "reverse type": {mode: "120000", data: "target"}}
	new := map[string]diffSource{"new\tname": {data: "one\ntwo\nCHANGED\nfour\nfive\n"}, "new binary \"name": {data: "new\x00binary"}, "old LF": {data: "same\n"}, "new LF": {data: "same"}, "reverse type": {data: "regular\n"}}
	f, c, revision := diffFixture(t, old, new)
	got, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil {
		t.Fatal(err)
	}
	renamed := false
	for _, file := range got.Files {
		if file.NewPath == "new\tname" {
			if file.Status != "renamed" || file.OldPath != "old\nname" || file.Similarity == 100 || len(file.Hunks) != 1 {
				t.Fatalf("edited rename: %+v", file)
			}
			renamed = true
		}
		if file.NewPath == "old LF" || file.NewPath == "new LF" {
			lines := file.Hunks[0].Lines
			if len(lines) != 2 || lines[0].NoNewline == (file.NewPath == "new LF") || lines[1].NoNewline == (file.NewPath == "old LF") {
				t.Fatal("single-sided no-newline marker lost")
			}
		}
	}
	if !renamed {
		t.Fatal("edited rename was not detected")
	}
}
