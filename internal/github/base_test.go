package github

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

func TestExplicitManualBaseAgainstLocalFetchedBranches(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("native Git is required for the offline branch fixture")
	}
	root := t.TempDir()
	upstream, cache := filepath.Join(root, "upstream.git"), filepath.Join(root, "cache.git")
	gitFixture(t, git, root, "", "init", "--bare", upstream)
	tree := gitFixture(t, git, upstream, "", "mktree")
	base := gitFixture(t, git, upstream, "default branch\n", "commit-tree", tree)
	target := gitFixture(t, git, upstream, "nondefault target\n", "commit-tree", tree, "-p", base)
	gitFixture(t, git, upstream, "", "update-ref", "refs/heads/main", base)
	gitFixture(t, git, upstream, "", "update-ref", "refs/heads/feat/go-rewrite", target)
	gitFixture(t, git, root, "", "init", "--bare", cache)
	gitFixture(t, git, cache, "", "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", upstream, "+refs/heads/*:refs/sheldon/bases/*")
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/31")
	r := Resolution{PR: pr, State: FallbackRequired, Failure: &MetadataError{Code: "cli-missing", ExitCode: -1}}
	candidates := fetchedFixtureCandidates(t, git, cache, pr)
	if _, err := r.SelectBase(candidates, "feat/go-rewrite", target); err == nil {
		t.Fatal("selection did not require fallback consent")
	}
	manual, err := r.ChooseGitOnly()
	if err != nil {
		t.Fatal(err)
	}
	if manual.Base != nil || manual.HeadSHA != "" {
		t.Fatal("fallback chose a default branch/head")
	}
	selected, err := manual.SelectBase(candidates, "feat/go-rewrite", target)
	if err != nil || selected.Base == nil || selected.Base.Branch != "feat/go-rewrite" || selected.Base.SHA != target || selected.Base.Source != githubpr.UserSelectedBase || selected.HeadSHA != "" || selected.State != Resolved {
		t.Fatalf("manual base: %+v, %v", selected, err)
	}
	if manual.Base != nil || r.State != FallbackRequired {
		t.Fatal("selection mutated prior unresolved result")
	}
	if _, err := selected.SelectBase(candidates, "main", base); err == nil {
		t.Fatal("resolved base silently rebound")
	}
	newTarget := gitFixture(t, git, upstream, "moved target\n", "commit-tree", tree, "-p", target)
	gitFixture(t, git, upstream, "", "update-ref", "refs/heads/feat/go-rewrite", newTarget)
	gitFixture(t, git, cache, "", "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", upstream, "+refs/heads/*:refs/sheldon/bases/*")
	current := fetchedFixtureCandidates(t, git, cache, pr)
	if _, err := manual.SelectBase(current, "feat/go-rewrite", target); err == nil {
		t.Fatal("stale selection accepted")
	}
	if selected.Base.SHA != target {
		t.Fatal("upstream movement changed the pinned manual selection")
	}
	if _, err := manual.SelectBase(current, "feat/go-rewrite", newTarget); err != nil {
		t.Fatal("explicit refreshed selection rejected")
	}
	gitFixture(t, git, cache, "", "update-ref", "-d", "refs/sheldon/bases/feat/go-rewrite")
	if _, err := manual.SelectBase(fetchedFixtureCandidates(t, git, cache, pr), "feat/go-rewrite", newTarget); err == nil {
		t.Fatal("missing branch selection accepted")
	}
}

func TestManualBaseRejectsInvalidCandidates(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/31")
	repositoryID, _ := pr.RepositoryID()
	r, _ := (Resolution{PR: pr, State: FallbackRequired, Failure: &MetadataError{Code: "cli-missing"}}).ChooseGitOnly()
	valid := BaseCandidate{RepositoryID: repositoryID, Branch: "feat/target", SHA: strings.Repeat("a", 40)}
	for name, candidates := range map[string][]BaseCandidate{
		"empty":            nil,
		"wrong repository": {{RepositoryID: strings.Repeat("f", 64), Branch: valid.Branch, SHA: valid.SHA}},
		"bad branch":       {{RepositoryID: repositoryID, Branch: "../escape", SHA: valid.SHA}},
		"bad commit":       {{RepositoryID: repositoryID, Branch: valid.Branch, SHA: strings.Repeat("0", 40)}},
		"duplicate":        {valid, valid},
		"mixed formats":    {valid, {RepositoryID: repositoryID, Branch: "other", SHA: strings.Repeat("b", 64)}},
		"too many":         make([]BaseCandidate, 1001),
	} {
		t.Run(name, func(t *testing.T) {
			if result, err := r.SelectBase(candidates, valid.Branch, valid.SHA); err == nil || result.State != "" {
				t.Fatal("invalid candidates produced a resolved base")
			}
		})
	}
	if _, err := r.SelectBase([]BaseCandidate{valid}, "--help", valid.SHA); err == nil {
		t.Fatal("invalid selected branch accepted")
	}
	if _, err := (Resolution{}).ChooseGitOnly(); err == nil {
		t.Fatal("zero resolution allowed fallback")
	}
}

func TestInvalidMetadataDoesNotBecomeResolvedBase(t *testing.T) {
	pr, _ := metadataFixture(t)
	t.Setenv("SHELDON_TEST_GH_RESPONSE", "HTTP/2.0 200 OK\r\n\r\n{}")
	r, err := Resolve(context.Background(), pr)
	if err != nil || r.State != FallbackRequired || r.Base != nil || r.HeadSHA != "" || r.PR.ID != pr.ID {
		t.Fatal("invalid metadata invented source context")
	}
}

// gitFixture runs isolated local Git fixture commands without user configuration.
func gitFixture(t *testing.T, executable, directory, input string, args ...string) string {
	t.Helper()
	command := exec.Command(executable, append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false"}, args...)...)
	command.Dir = directory
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GIT_") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	command.Stdin = strings.NewReader(input)
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	output, err := command.Output()
	if err != nil {
		t.Fatalf("offline Git fixture: %v: %s", err, diagnostic.String())
	}
	return strings.TrimSpace(string(output))
}

// fetchedFixtureCandidates reads real fixture refs, not a production cache service.
func fetchedFixtureCandidates(t *testing.T, executable, directory string, pr githubpr.PullRequest) []BaseCandidate {
	t.Helper()
	repositoryID, _ := pr.RepositoryID()
	output := gitFixture(t, executable, directory, "", "for-each-ref", "--format=%(refname)%00%(objectname)", "refs/sheldon/bases/")
	var candidates []BaseCandidate
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		ref, sha, found := strings.Cut(line, "\x00")
		if !found || gitFixture(t, executable, directory, "", "cat-file", "-t", sha) != "commit" {
			t.Fatal("invalid offline fetched commit fixture")
		}
		candidates = append(candidates, BaseCandidate{RepositoryID: repositoryID, Branch: strings.TrimPrefix(ref, "refs/sheldon/bases/"), SHA: sha})
	}
	return candidates
}
