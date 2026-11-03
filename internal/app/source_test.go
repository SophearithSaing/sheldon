package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/httpapi"
)

// sourceGit runs native, isolated offline setup without reading personal Git configuration.
func sourceGit(t *testing.T, executable, dir, input string, args ...string) string {
	t.Helper()
	cmd := exec.Command(executable, append([]string{"-c", "core.hooksPath=" + os.DevNull}, args...)...)
	cmd.Dir, cmd.Stdin = dir, strings.NewReader(input)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=f@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=f@example.invalid")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native fixture: %v %s", err, data)
	}
	return strings.TrimSpace(string(data))
}

func TestSourceRegistryComposesRealOfflineResolutionFetchWorktreeAndDiff(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bin := filepath.Join(root, "tools")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"git": "../git/testdata/git", "gh": "../github/testdata/gh"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(bin, name), path)
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture build: %v %s", err, data)
		}
	}
	upstream := filepath.Join(root, "upstream.git")
	sourceGit(t, realGit, root, "", "init", "--bare", upstream)
	tree := sourceGit(t, realGit, upstream, "", "mktree")
	base := sourceGit(t, realGit, upstream, "base\n", "commit-tree", tree)
	blob := sourceGit(t, realGit, upstream, "literal source\n", "hash-object", "-w", "--stdin")
	tree = sourceGit(t, realGit, upstream, "100644 blob "+blob+"\tsource.txt\n", "mktree")
	head := sourceGit(t, realGit, upstream, "head\n", "commit-tree", tree, "-p", base)
	for ref, sha := range map[string]string{"refs/heads/feat/go-rewrite": base, "refs/pull/31/head": head} {
		sourceGit(t, realGit, upstream, "", "update-ref", ref, sha)
	}
	body, err := os.ReadFile("../github/testdata/pull-request.json")
	if err != nil {
		t.Fatal(err)
	}
	response := strings.ReplaceAll(strings.ReplaceAll(string(body), strings.Repeat("a", 40), base), strings.Repeat("b", 40), head)
	for key, value := range map[string]string{
		"PATH": bin, "SHELDON_GIT_REAL": realGit, "SHELDON_GIT_UPSTREAM": upstream, "SHELDON_GIT_AUDIT": filepath.Join(root, "git-audit"), "SHELDON_GIT_MODE": "",
		"SHELDON_TEST_GH_AUDIT": filepath.Join(root, "gh-audit"), "SHELDON_TEST_GH_RESPONSE": "HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n" + response, "SHELDON_TEST_GH_MODE": "", "GH_TOKEN": "synthetic-auth-token",
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_SYSTEM": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
	} {
		t.Setenv(key, value)
	}
	paths := testPaths(t)
	i, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer i.close()
	if owned, err := i.acquire(); err != nil || !owned {
		t.Fatal("writer ownership unavailable")
	}
	pr, err := i.RegisterPR("https://github.com/owner/repo/pull/31")
	if err != nil {
		t.Fatal(err)
	}
	service := &sourceRegistry{instance: i, paths: paths}
	ready, err := service.LoadSource(context.Background(), pr, httpapi.SourceRequest{Action: "load"})
	if err != nil || ready.State != "ready" || ready.Diff == nil || !ready.Diff.Complete || len(ready.Diff.Files) != 1 {
		t.Fatalf("source preparation: %+v %v", ready, err)
	}
	if ready.PR.Title == nil || ready.Diff.Files[0].Hunks[0].Lines[0].Text != "literal source" {
		t.Fatal("metadata or actual source missing")
	}
	data, _ := json.Marshal(ready)
	if strings.Contains(string(data), paths.Cache) || strings.Contains(string(data), "Directory") {
		t.Fatal("backend source location leaked")
	}
	original := ready.Diff.Revision
	refresh := httpapi.SourceRequest{Action: "refresh", Revision: &original}
	unchanged, err := service.LoadSource(context.Background(), pr, refresh)
	if err != nil || unchanged.Diff.RevisionID != ready.Diff.RevisionID {
		t.Fatalf("unchanged refresh: %v", err)
	}
	originalPath := filepath.Join(paths.Cache, "worktrees", pr.ID, head)
	before, _ := os.Stat(originalPath)
	newBase := sourceGit(t, realGit, upstream, "advanced base\n", "commit-tree", tree, "-p", base)
	sourceGit(t, realGit, upstream, "", "update-ref", "refs/heads/feat/go-rewrite", newBase)
	currentMetadata := strings.ReplaceAll(response, base, newBase)
	t.Setenv("SHELDON_TEST_GH_RESPONSE", "HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n"+currentMetadata)
	baseOnly, err := service.LoadSource(context.Background(), pr, refresh)
	if err != nil || baseOnly.Diff.RevisionID == ready.Diff.RevisionID || baseOnly.Diff.Revision.HeadSHA != head {
		t.Fatalf("base-only refresh: %v", err)
	}
	after, _ := os.Stat(originalPath)
	if !os.SameFile(before, after) {
		t.Fatal("base-only refresh replaced the head worktree")
	}
	newBlob := sourceGit(t, realGit, upstream, "refreshed source\n", "hash-object", "-w", "--stdin")
	newTree := sourceGit(t, realGit, upstream, "100644 blob "+newBlob+"\tsource.txt\n", "mktree")
	newHead := sourceGit(t, realGit, upstream, "force pushed head\n", "commit-tree", newTree, "-p", base)
	sourceGit(t, realGit, upstream, "", "update-ref", "refs/pull/31/head", newHead)
	t.Setenv("SHELDON_TEST_GH_RESPONSE", "HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n"+strings.ReplaceAll(currentMetadata, head, newHead))
	newer, err := service.LoadSource(context.Background(), pr, httpapi.SourceRequest{Action: "refresh", Revision: &baseOnly.Diff.Revision})
	if err != nil || newer.Diff.Revision.HeadSHA != newHead {
		t.Fatalf("force-pushed refresh: %v", err)
	}
	if sourceGit(t, realGit, originalPath, "", "rev-parse", "HEAD") != head {
		t.Fatal("refresh moved the original detached HEAD")
	}
	if data, err := os.ReadFile(filepath.Join(originalPath, "source.txt")); err != nil || string(data) != "literal source\n" {
		t.Fatal("refresh changed original source")
	}
	t.Setenv("SHELDON_GIT_MODE", "fail")
	if result, err := service.LoadSource(context.Background(), pr, refresh); err == nil || result.Diff != nil {
		t.Fatal("failed refresh returned ready source")
	}
	read, err := i.cache.Diff(context.Background(), pr, original)
	if err != nil || !reflect.DeepEqual(read, *ready.Diff) {
		t.Fatalf("refresh failure damaged the original comparison: %v", err)
	}
	t.Setenv("SHELDON_GIT_MODE", "")
	t.Setenv("SHELDON_TEST_GH_MODE", "auth")
	fallback, err := service.LoadSource(context.Background(), pr, httpapi.SourceRequest{Action: "load"})
	if err != nil || fallback.State != "fallback-required" || fallback.Diff != nil {
		t.Fatal("metadata failure was presented as ready")
	}
	choices, err := service.LoadSource(context.Background(), pr, httpapi.SourceRequest{Action: "git-only"})
	if err != nil || choices.State != "base-selection-required" || len(choices.Candidates) != 1 {
		t.Fatalf("branch choice: %v", err)
	}
	selected := httpapi.SourceRequest{Action: "select-base", Branch: choices.Candidates[0].Branch, SHA: choices.Candidates[0].SHA}
	manual, err := service.LoadSource(context.Background(), pr, selected)
	if err != nil || manual.Diff == nil || manual.Diff.Revision.BaseSource != "user-selected" {
		t.Fatalf("manual source: %v", err)
	}
	manualRefresh, err := service.LoadSource(context.Background(), pr, httpapi.SourceRequest{Action: "refresh", Revision: &manual.Diff.Revision})
	if err != nil || manualRefresh.Diff.Revision.BaseBranch != manual.Diff.Revision.BaseBranch || manualRefresh.Diff.Revision.BaseSource != "user-selected" {
		t.Fatalf("manual refresh lost the explicit base choice: %v", err)
	}
	sourceGit(t, realGit, upstream, "", "update-ref", "refs/heads/main", newBase)
	sourceGit(t, realGit, upstream, "", "update-ref", "-d", "refs/heads/feat/go-rewrite")
	if _, err := service.LoadSource(context.Background(), pr, httpapi.SourceRequest{Action: "refresh", Revision: &manual.Diff.Revision}); !errors.Is(err, git.ErrRefreshRequired) {
		t.Fatal("manual refresh substituted another branch after target deletion")
	}
	sourceGit(t, realGit, upstream, "", "update-ref", "refs/heads/feat/go-rewrite", newBase)
	sourceGit(t, realGit, upstream, "", "update-ref", "refs/heads/feat/go-rewrite", head)
	if _, err := service.LoadSource(context.Background(), pr, selected); !errors.Is(err, git.ErrRefreshRequired) {
		t.Fatalf("moved branch was accepted: %v", err)
	}
}
