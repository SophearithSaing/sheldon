package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// sourceFixture gives the PR a literal file and a base tip ahead of its fork point.
func sourceFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)
	blob := native(t, f.upstream, "pinned source\n", "hash-object", "-w", "--stdin")
	tree := native(t, f.upstream, "100644 blob "+blob+"\tsource.txt\n", "mktree")
	head := native(t, f.upstream, "source head\n", "commit-tree", tree, "-p", f.base)
	base := native(t, f.upstream, "advanced base\n", "commit-tree", f.tree, "-p", f.base)
	native(t, f.upstream, "", "update-ref", "refs/pull/1/head", head)
	native(t, f.upstream, "", "update-ref", "refs/heads/feat/target", base)
	f.resolution.HeadSHA = head
	f.resolution.Base.SHA = base
	return f
}

// fetchPrepared obtains an actual verified fetch before preparing source.
func fetchPrepared(t *testing.T, c *Cache, f fixture) (Snapshot, Prepared) {
	t.Helper()
	snapshot, err := c.Fetch(context.Background(), f.resolution)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := c.Prepare(context.Background(), f.resolution.PR, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, prepared
}

func TestPreparePinsMergeBaseReusesHeadAndRetainsOldRevisions(t *testing.T) {
	f := sourceFixture(t)
	c := openFixture(t, f)
	snapshot, first := fetchPrepared(t, c, f)
	if first.Revision.MergeBaseSHA != f.base || first.Revision.BaseSHA == f.base || first.Revision.HeadSHA != snapshot.HeadSHA {
		t.Fatalf("wrong comparison: %+v", first.Revision)
	}
	want := filepath.Join(f.paths.Cache, "worktrees", f.resolution.PR.ID, snapshot.HeadSHA)
	if first.Directory != want {
		t.Fatal("unexpected worktree layout")
	}
	if native(t, want, "", "rev-parse", "HEAD") != snapshot.HeadSHA {
		t.Fatal("wrong detached head")
	}
	data, err := os.ReadFile(filepath.Join(want, "source.txt"))
	if err != nil || string(data) != "pinned source\n" {
		t.Fatal("source not materialized")
	}
	encoded, _ := json.Marshal(first)
	if bytes.Contains(encoded, []byte(want)) || bytes.Contains(encoded, []byte("Directory")) {
		t.Fatal("backend path leaked into JSON")
	}
	before, _ := os.Stat(want)
	again, err := c.Prepare(context.Background(), f.resolution.PR, snapshot)
	after, _ := os.Stat(want)
	if err != nil || again != first || !os.SameFile(before, after) {
		t.Fatalf("worktree not reused: %v", err)
	}
	newBase := native(t, f.upstream, "new base\n", "commit-tree", f.tree, "-p", snapshot.BaseSHA)
	native(t, f.upstream, "", "update-ref", "refs/heads/feat/target", newBase)
	f.resolution.Base.SHA = newBase
	_, second := fetchPrepared(t, c, f)
	firstID, _ := first.Revision.ID()
	secondID, _ := second.Revision.ID()
	if firstID == secondID || first.Directory != second.Directory || second.Revision.MergeBaseSHA != f.base {
		t.Fatal("base-only refresh did not reuse head with a new revision")
	}
	newHead := native(t, f.upstream, "new head\n", "commit-tree", f.tree, "-p", f.base)
	native(t, f.upstream, "", "update-ref", "refs/pull/1/head", newHead)
	f.resolution.HeadSHA = newHead
	_, third := fetchPrepared(t, c, f)
	if third.Directory == first.Directory {
		t.Fatal("new head reused old source directory")
	}
	id, _ := f.resolution.PR.RepositoryID()
	repo := filepath.Join(f.paths.Cache, "repositories", id+".git")
	native(t, repo, "", "reflog", "expire", "--expire=now", "--all")
	native(t, repo, "", "gc", "--prune=now")
	for name, sha := range map[string]string{"base": snapshot.BaseSHA, "head": snapshot.HeadSHA, "merge-base": f.base} {
		if native(t, repo, "", "rev-parse", "refs/sheldon/reviews/"+firstID+"/"+name) != sha {
			t.Fatal("old comparison was not retained")
		}
	}
	if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err != nil {
		t.Fatalf("old source lost after GC: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openFixture(t, f)
	if _, err := reopened.Prepare(context.Background(), f.resolution.PR, snapshot); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

func TestPreparePreservesChangedAndUnidentifiedWorktrees(t *testing.T) {
	for _, kind := range []string{"dirty", "untracked", "directory", "missing", "index", "attached", "gitfile", "backlink", "marker"} {
		t.Run(kind, func(t *testing.T) {
			f := sourceFixture(t)
			c := openFixture(t, f)
			snapshot, prepared := fetchPrepared(t, c, f)
			root := prepared.Directory
			admin, err := worktreeAdmin(filepath.Join(f.paths.Cache, "repositories", snapshot.RepositoryID+".git"), root)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "source.txt")
			switch kind {
			case "dirty":
				err = os.WriteFile(file, []byte("keep changes"), 0600)
			case "untracked":
				err = os.WriteFile(filepath.Join(root, "notes"), []byte("keep notes"), 0600)
			case "directory":
				err = os.Mkdir(filepath.Join(root, "empty"), 0700)
			case "missing":
				err = os.Remove(file)
			case "index":
				native(t, root, "", "read-tree", f.base)
			case "attached":
				err = os.WriteFile(filepath.Join(admin, "HEAD"), []byte("ref: refs/heads/main\n"), 0600)
			case "gitfile":
				err = os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /foreign\n"), 0600)
			case "backlink":
				err = os.WriteFile(filepath.Join(admin, "gitdir"), []byte("/foreign/.git\n"), 0600)
			case "marker":
				err = os.WriteFile(root+".json", []byte(`{"version":9}`), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(file)
			got, err := c.Prepare(context.Background(), f.resolution.PR, snapshot)
			if err == nil || got != (Prepared{}) {
				t.Fatal("unsafe worktree accepted")
			}
			after, _ := os.ReadFile(file)
			if !bytes.Equal(before, after) {
				t.Fatal("source changed on refusal")
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal("existing worktree removed")
			}
		})
	}
}

func TestPrepareRejectsIncompleteUnrelatedAndAmbiguousHistory(t *testing.T) {
	for _, kind := range []string{"missing commit", "missing parent", "shallow", "unrelated", "criss-cross"} {
		t.Run(kind, func(t *testing.T) {
			f := sourceFixture(t)
			want := ErrHistoryUnavailable
			if kind == "unrelated" {
				f.resolution.HeadSHA = native(t, f.upstream, "unrelated\n", "commit-tree", f.tree)
				want = ErrUnrelatedHistory
			}
			if kind == "criss-cross" {
				a := native(t, f.upstream, "a\n", "commit-tree", f.tree, "-p", f.base)
				b := native(t, f.upstream, "b\n", "commit-tree", f.tree, "-p", f.base)
				f.resolution.HeadSHA = native(t, f.upstream, "ab\n", "commit-tree", f.tree, "-p", a, "-p", b)
				f.resolution.Base.SHA = native(t, f.upstream, "ba\n", "commit-tree", f.tree, "-p", b, "-p", a)
				want = ErrAmbiguousMergeBase
			}
			native(t, f.upstream, "", "update-ref", "refs/pull/1/head", f.resolution.HeadSHA)
			native(t, f.upstream, "", "update-ref", "refs/heads/feat/target", f.resolution.Base.SHA)
			c := openFixture(t, f)
			snapshot, err := c.Fetch(context.Background(), f.resolution)
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(f.paths.Cache, "repositories", snapshot.RepositoryID+".git")
			switch kind {
			case "missing commit":
				snapshot.HeadSHA = strings.Repeat("a", 40)
			case "missing parent":
				if err := os.Remove(filepath.Join(repo, "objects", f.base[:2], f.base[2:])); err != nil {
					t.Fatal(err)
				}
			case "shallow":
				if err := os.WriteFile(filepath.Join(repo, "shallow"), []byte(f.base+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := c.Prepare(context.Background(), f.resolution.PR, snapshot)
			if !errors.Is(err, want) || got != (Prepared{}) {
				t.Fatalf("history: %v; expected %v", err, want)
			}
			if _, err := os.Stat(filepath.Join(f.paths.Cache, "worktrees")); !os.IsNotExist(err) {
				t.Fatal("invalid history created a workspace")
			}
		})
	}
}

func TestPrepareCancellationCleansOnlyNewWorkspace(t *testing.T) {
	for _, mode := range []string{"worktree-fail", "materialize-hang"} {
		t.Run(mode, func(t *testing.T) {
			f := sourceFixture(t)
			c := openFixture(t, f)
			snapshot, first := fetchPrepared(t, c, f)
			newHead := native(t, f.upstream, "next\n", "commit-tree", native(t, f.upstream, "", "rev-parse", snapshot.HeadSHA+"^{tree}"), "-p", snapshot.HeadSHA)
			native(t, f.upstream, "", "update-ref", "refs/pull/1/head", newHead)
			f.resolution.HeadSHA = newHead
			next, err := c.Fetch(context.Background(), f.resolution)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("SHELDON_GIT_MODE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := c.Prepare(ctx, f.resolution.PR, next)
			if err == nil || got != (Prepared{}) {
				t.Fatal("failed preparation reported success")
			}
			if mode == "materialize-hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost cancellation: %v", err)
			}
			root := filepath.Join(filepath.Dir(first.Directory), newHead)
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("new failed worktree was not cleaned")
			}
			repo := filepath.Join(f.paths.Cache, "repositories", snapshot.RepositoryID+".git")
			registrations, err := os.ReadDir(filepath.Join(repo, "worktrees"))
			if err != nil || len(registrations) != 1 {
				t.Fatal("cleanup changed existing or left failed registration")
			}
			t.Setenv("SHELDON_GIT_MODE", "")
			if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err != nil {
				t.Fatalf("existing source lost: %v", err)
			}
			if _, err := c.Prepare(context.Background(), f.resolution.PR, next); err != nil {
				t.Fatalf("retry: %v", err)
			}
		})
	}
}

func TestPrepareFirstCreationFailureCanRetry(t *testing.T) {
	f := sourceFixture(t)
	c := openFixture(t, f)
	snapshot, err := c.Fetch(context.Background(), f.resolution)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELDON_GIT_MODE", "worktree-fail")
	if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err == nil {
		t.Fatal("fixture failure was ignored")
	}
	t.Setenv("SHELDON_GIT_MODE", "")
	if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err != nil {
		t.Fatalf("initial failed creation prevented retry: %v", err)
	}
}

func TestPrepareConcurrentEmptySourceAndInvalidSnapshots(t *testing.T) {
	f := newFixture(t)
	c := openFixture(t, f)
	snapshot, err := c.Fetch(context.Background(), f.resolution)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	for _, kind := range []string{"PR", "repository", "branch", "commit", "source"} {
		invalid := snapshot
		switch kind {
		case "PR":
			invalid.PRID = "../invalid"
		case "repository":
			invalid.RepositoryID = "invalid"
		case "branch":
			invalid.BaseBranch = "--invalid"
		case "commit":
			invalid.HeadSHA = "invalid"
		case "source":
			invalid.BaseSource = "invalid"
		}
		if got, err := c.Prepare(context.Background(), f.resolution.PR, invalid); err == nil || got != (Prepared{}) {
			t.Errorf("invalid %s accepted", kind)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Prepare(ctx, f.resolution.PR, snapshot); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancelled preparation ran")
	}
}
