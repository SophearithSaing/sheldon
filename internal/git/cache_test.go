package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/github"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/platform"
	"github.com/SophearithSaing/sheldon/internal/process"
)

var realGit, fixtureBin string

func TestMain(m *testing.M) {
	var err error
	realGit, err = exec.LookPath("git")
	if err != nil {
		fmt.Fprintln(os.Stderr, "native Git is required for offline cache tests")
		os.Exit(1)
	}
	fixtureBin, err = os.MkdirTemp("", "sheldon-git-tool-")
	if err != nil {
		os.Exit(1)
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	command := exec.Command("go", "build", "-o", filepath.Join(fixtureBin, name), "./testdata/git")
	if output, err := command.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "fixture build: %v %s", err, output)
		os.RemoveAll(fixtureBin)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(fixtureBin)
	os.Exit(code)
}

type fixture struct {
	paths                                   platform.Paths
	upstream, audit, base, head, fork, tree string
	resolution                              github.Resolution
}

// newFixture creates local objects and a fake HTTPS transport, never a live remote.
func newFixture(t *testing.T) fixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "cache fixture 日本語")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	upstream := filepath.Join(root, "upstream.git")
	native(t, root, "", "init", "--bare", upstream)
	tree := native(t, upstream, "", "mktree")
	base := native(t, upstream, "base\n", "commit-tree", tree)
	head := native(t, upstream, "head\n", "commit-tree", tree, "-p", base)
	fork := native(t, upstream, "fork head\n", "commit-tree", tree, "-p", base)
	for ref, sha := range map[string]string{"refs/heads/main": base, "refs/heads/feat/target": base, "refs/pull/1/head": head, "refs/pull/2/head": fork} {
		native(t, upstream, "", "update-ref", ref, sha)
	}
	paths := platform.Paths{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Runtime: filepath.Join(root, "runtime")}
	if err := privateDirectory(paths.Data); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fixtureBin)
	t.Setenv("SHELDON_GIT_REAL", realGit)
	t.Setenv("SHELDON_GIT_UPSTREAM", upstream)
	t.Setenv("SHELDON_GIT_TREE", tree)
	t.Setenv("SHELDON_GIT_AUDIT", filepath.Join(root, "audit.jsonl"))
	t.Setenv("SHELDON_GIT_MODE", "")
	t.Setenv("GIT_SSH_COMMAND", "preserved-fixture-ssh-route")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	return fixture{paths: paths, upstream: upstream, audit: filepath.Join(root, "audit.jsonl"), base: base, head: head, fork: fork, tree: tree,
		resolution: github.Resolution{PR: pr, State: github.Resolved, Base: &github.Base{Branch: "feat/target", SHA: base, Source: githubpr.GitHubBase}, HeadSHA: head}}
}

// native runs isolated native Git fixture setup, with no global auth/config reads.
func native(t *testing.T, dir, input string, args ...string) string {
	t.Helper()
	command := exec.Command(realGit, append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false"}, args...)...)
	command.Dir, command.Stdin = dir, strings.NewReader(input)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GIT_") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	output, err := command.Output()
	if err != nil {
		t.Fatalf("native fixture: %v %s", err, diagnostic.String())
	}
	return strings.TrimSpace(string(output))
}

// openFixture opens a caller-exclusively-owned test cache and arranges its cleanup.
func openFixture(t *testing.T, f fixture) *Cache {
	t.Helper()
	c, err := Open(f.paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func TestFetchReusesRepositoryAndPinsUpstreamForkHeads(t *testing.T) {
	f := newFixture(t)
	c := openFixture(t, f)
	first, err := c.Fetch(context.Background(), f.resolution)
	if err != nil || first.BaseSHA != f.base || first.HeadSHA != f.head || first.BaseSource != githubpr.GitHubBase {
		t.Fatalf("fetch: %+v %v", first, err)
	}
	id, _ := f.resolution.PR.RepositoryID()
	dir := filepath.Join(f.paths.Cache, "repositories", id+".git")
	before, _ := os.Stat(dir)
	other := f.resolution
	other.PR, _ = githubpr.ParseURL("https://github.com/OWNER/REPO/pull/2")
	other.HeadSHA = f.fork
	second, err := c.Fetch(context.Background(), other)
	if err != nil || second.RepositoryID != first.RepositoryID || second.PRID == first.PRID || second.HeadSHA != f.fork {
		t.Fatalf("fork fetch: %+v %v", second, err)
	}
	after, _ := os.Stat(dir)
	if !os.SameFile(before, after) {
		t.Fatal("second PR replaced its shared repository")
	}
	native(t, dir, "", "pack-refs", "--all")
	native(t, dir, "", "repack", "-ad")
	if _, err := os.Stat(filepath.Join(dir, "FETCH_HEAD")); !os.IsNotExist(err) {
		t.Fatal("fetch wrote unneeded transport metadata")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openFixture(t, f)
	if _, err := reopened.Fetch(context.Background(), f.resolution); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(f.audit)
	initializations := 0
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var audit struct {
			Operation                     string
			Safe, AuthenticationPreserved bool
		}
		if json.Unmarshal(line, &audit) != nil || !audit.Safe || !audit.AuthenticationPreserved {
			t.Fatal("unsafe command or changed authentication routing")
		}
		if audit.Operation == "init" {
			initializations++
		}
	}
	if initializations != 1 {
		t.Fatalf("repository initialized %d times", initializations)
	}
}

func TestFetchedFallbackSelectionAndMovedCommits(t *testing.T) {
	f := newFixture(t)
	c := openFixture(t, f)
	fallback := github.Resolution{PR: f.resolution.PR, State: github.FallbackRequired, Failure: &github.MetadataError{Code: "cli-missing"}}
	if _, err := c.FetchBases(context.Background(), fallback); err == nil {
		t.Fatal("fallback fetch did not require consent")
	}
	manual, _ := fallback.ChooseGitOnly()
	candidates, err := c.FetchBases(context.Background(), manual)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates: %+v %v", candidates, err)
	}
	selected, err := c.SelectBase(context.Background(), manual, "feat/target", f.base)
	if err != nil || selected.Base.Source != githubpr.UserSelectedBase || selected.HeadSHA != "" {
		t.Fatalf("manual selection: %+v %v", selected, err)
	}
	snapshot, err := c.Fetch(context.Background(), selected)
	if err != nil || snapshot.HeadSHA != f.head || snapshot.BaseSource != githubpr.UserSelectedBase {
		t.Fatalf("manual fetch: %+v %v", snapshot, err)
	}
	native(t, f.upstream, "", "update-ref", "refs/heads/feat/target", f.head)
	if snapshot, err := c.Fetch(context.Background(), f.resolution); !errors.Is(err, ErrRefreshRequired) || snapshot != (Snapshot{}) {
		t.Fatal("moved base produced a mixed snapshot")
	}
	if snapshot, err := c.Fetch(context.Background(), selected); !errors.Is(err, ErrRefreshRequired) || snapshot != (Snapshot{}) {
		t.Fatal("stale manual base produced a snapshot")
	}
	if _, err := c.SelectBase(context.Background(), manual, "feat/target", f.base); !errors.Is(err, ErrRefreshRequired) {
		t.Fatal("stale cached selection accepted")
	}
	if _, err := c.FetchBases(context.Background(), manual); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SelectBase(context.Background(), manual, "feat/target", f.head); err != nil {
		t.Fatal(err)
	}
}

func TestFetchFailureLimitsCancellationAndNoPartialSuccess(t *testing.T) {
	for _, mode := range []string{"fail", "overflow", "hang", "noncommit"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			c := openFixture(t, f)
			if _, err := c.Fetch(context.Background(), f.resolution); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SHELDON_GIT_MODE", mode)
			ctx := context.Background()
			if mode == "hang" {
				bounded, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				ctx = bounded
			}
			resolution := f.resolution
			if mode == "noncommit" {
				resolution.HeadSHA = f.tree
			}
			snapshot, err := c.Fetch(ctx, resolution)
			if err == nil || snapshot != (Snapshot{}) {
				t.Fatal("failed fetch reported a partial snapshot")
			}
			if mode == "overflow" && !errors.Is(err, process.ErrOutputLimit) {
				t.Fatalf("lost output limit: %v", err)
			}
			if mode == "hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost cancellation: %v", err)
			}
			for _, value := range []string{err.Error(), fmt.Sprintf("%#v", err)} {
				if strings.Contains(value, "synthetic-secret") || strings.Contains(value, "secret@") {
					t.Fatal("native diagnostics leaked")
				}
			}
			t.Setenv("SHELDON_GIT_MODE", "")
			if _, err := c.Fetch(context.Background(), f.resolution); err != nil {
				t.Fatalf("failed fetch prevented later explicit retry: %v", err)
			}
		})
	}
}

func TestConcurrentFetchesAndIndependentRepositories(t *testing.T) {
	f := newFixture(t)
	c := openFixture(t, f)
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Go(func() {
			if _, err := c.Fetch(context.Background(), f.resolution); err != nil {
				t.Error(err)
			}
		})
	}
	other := f.resolution
	other.PR, _ = githubpr.ParseURL("https://github.com/owner/other/pull/1")
	wg.Go(func() {
		if _, err := c.Fetch(context.Background(), other); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	entries, err := os.ReadDir(filepath.Join(f.paths.Cache, "repositories"))
	if err != nil || len(entries) != 2 {
		t.Fatal("independent repositories were conflated or initialization leaked")
	}
}

func TestCacheOwnershipAndInvalidInputs(t *testing.T) {
	f := newFixture(t)
	c := openFixture(t, f)
	other := f.paths
	other.Data = filepath.Join(filepath.Dir(f.paths.Data), "another-data")
	if err := privateDirectory(other.Data); err != nil {
		t.Fatal(err)
	}
	if otherCache, err := Open(other); err == nil {
		otherCache.Close()
		t.Fatal("different data roots shared a cache")
	}
	invalid := f.resolution
	invalid.PR.ID = "../escape"
	if _, err := c.Fetch(context.Background(), invalid); err == nil {
		t.Fatal("invalid PR accepted")
	}
	invalid = f.resolution
	invalid.Base = &github.Base{Branch: "--help", SHA: f.base, Source: githubpr.GitHubBase}
	if _, err := c.Fetch(context.Background(), invalid); err == nil {
		t.Fatal("option-like base accepted")
	}
	if _, err := os.Stat(f.audit); !os.IsNotExist(err) {
		t.Fatal("invalid input invoked Git")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Fetch(context.Background(), f.resolution); err == nil {
		t.Fatal("closed cache used")
	}
}

func TestFetchPreservesExistingDetachedWorkspace(t *testing.T) {
	f := newFixture(t)
	c := openFixture(t, f)
	if _, err := c.Fetch(context.Background(), f.resolution); err != nil {
		t.Fatal(err)
	}
	id, _ := f.resolution.PR.RepositoryID()
	dir := filepath.Join(f.paths.Cache, "repositories", id+".git")
	worktree := filepath.Join(f.paths.Cache, "worktrees", f.resolution.PR.ID, f.head)
	native(t, dir, "", "worktree", "add", "--detach", worktree, f.head)
	// Step 6 owns private registration creation; this fixture supplies that precondition.
	if err := filepath.WalkDir(filepath.Join(dir, "worktrees"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if entry.IsDir() {
			mode = 0700
		}
		return os.Chmod(path, mode)
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(worktree, "reader-notes.txt")
	if err := os.WriteFile(path, []byte("keep reader content"), 0600); err != nil {
		t.Fatal(err)
	}
	before := native(t, worktree, "", "rev-parse", "HEAD")
	native(t, f.upstream, "", "update-ref", "refs/pull/1/head", f.fork)
	if snapshot, err := c.Fetch(context.Background(), f.resolution); !errors.Is(err, ErrRefreshRequired) || snapshot != (Snapshot{}) {
		t.Fatalf("moved head was not reported: %v", err)
	}
	current := f.resolution
	current.HeadSHA = f.fork
	if _, err := c.Fetch(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "keep reader content" || native(t, worktree, "", "rev-parse", "HEAD") != before {
		t.Fatal("fetch changed an existing detached workspace")
	}
}
