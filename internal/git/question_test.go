package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/process"
)

// questionAudit checks that context resolution executed only read operations.
func questionAudit(t *testing.T, f fixture, offset int) {
	t.Helper()
	data, err := os.ReadFile(f.audit)
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
			t.Fatal("unsafe question Git invocation")
		}
		switch entry.Operation {
		case "rev-parse", "for-each-ref", "cat-file", "rev-list", "merge-base", "ls-tree", "ls-files", "diff":
		default:
			t.Fatalf("question source mutated/fetched: %s", entry.Operation)
		}
	}
}

func TestQuestionSourceReadsOriginalAfterBaseAndHeadRefresh(t *testing.T) {
	f, c, revision := diffFixture(t, map[string]diffSource{"source.txt": {data: "old version\n"}, "deleted.txt": {data: "removed\n"}}, map[string]diffSource{"source.txt": {data: "new version\n"}, "added.txt": {data: "added\n"}})
	_, original := fetchPrepared(t, c, f)
	if original.Revision != revision {
		t.Fatal("fixture comparison changed")
	}
	want, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil {
		t.Fatal(err)
	}
	newBase := native(t, f.upstream, "base refresh\n", "commit-tree", f.tree, "-p", revision.BaseSHA)
	native(t, f.upstream, "", "update-ref", "refs/heads/feat/target", newBase)
	f.resolution.Base.SHA = newBase
	_, baseOnly := fetchPrepared(t, c, f)
	if baseOnly.Directory != original.Directory || baseOnly.Revision == revision {
		t.Fatal("base-only comparison did not retain original source")
	}
	newHead := native(t, f.upstream, "head refresh\n", "commit-tree", f.tree, "-p", revision.MergeBaseSHA)
	native(t, f.upstream, "", "update-ref", "refs/pull/1/head", newHead)
	f.resolution.HeadSHA = newHead
	_, headOnly := fetchPrepared(t, c, f)
	if headOnly.Directory == original.Directory {
		t.Fatal("head refresh reused old worktree")
	}
	other := f.resolution
	other.PR, _ = githubpr.ParseURL("https://github.com/owner/repo/pull/2")
	other.HeadSHA = f.fork
	snapshot, err := c.Fetch(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prepare(context.Background(), other.PR, snapshot); err != nil {
		t.Fatal(err)
	}
	audit, _ := os.ReadFile(f.audit)
	prepared, got, err := c.QuestionSource(context.Background(), f.resolution.PR, revision)
	if err != nil || prepared != original || !reflect.DeepEqual(got, want) {
		t.Fatalf("original source not retained: %v", err)
	}
	questionAudit(t, f, len(audit))
	if native(t, original.Directory, "", "rev-parse", "HEAD") != revision.HeadSHA {
		t.Fatal("question read moved HEAD")
	}
	if data, err := os.ReadFile(filepath.Join(original.Directory, "source.txt")); err != nil || string(data) != "new version\n" {
		t.Fatal("question read changed source")
	}
	for _, file := range got.Files {
		for _, hunk := range file.Hunks {
			for _, line := range hunk.Lines {
				if line.Sign == "-" && line.Text != "old version" && line.Text != "removed" {
					t.Fatal("old excerpt came from base tip instead of merge base")
				}
			}
		}
	}
}

func TestQuestionSourceRefusesMissingOrUnsafeStateWithoutRepair(t *testing.T) {
	for _, kind := range []string{"dirty", "untracked", "missing source", "missing worktree", "missing marker", "wrong marker", "index", "HEAD", "backlink", "missing pin", "wrong pin", "missing repository", "missing cache", "wrong revision", "wrong PR", "diff failure", "diff limit", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := sourceFixture(t)
			c := openFixture(t, f)
			_, original := fetchPrepared(t, c, f)
			revision := original.Revision
			pr := f.resolution.PR
			repoID, _ := pr.RepositoryID()
			repo := filepath.Join(f.paths.Cache, "repositories", repoID+".git")
			admin, err := worktreeAdmin(repo, original.Directory)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(original.Directory, "source.txt")
			ctx := context.Background()
			switch kind {
			case "dirty":
				err = os.WriteFile(file, []byte("keep edits\n"), 0600)
			case "untracked":
				err = os.WriteFile(filepath.Join(original.Directory, "notes"), []byte("keep notes"), 0600)
			case "missing source":
				err = os.Remove(file)
			case "missing worktree", "missing repository", "missing cache":
				path := original.Directory
				if kind == "missing repository" {
					path = repo
				} else if kind == "missing cache" {
					path = f.paths.Cache
				}
				err = os.Rename(path, path+".saved")
				defer func() {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Error("question recreated missing source")
					}
					if err := os.Rename(path+".saved", path); err != nil {
						t.Error(err)
					}
				}()
			case "missing marker":
				err = os.Remove(original.Directory + ".json")
			case "wrong marker":
				err = os.WriteFile(original.Directory+".json", []byte(`{"version":9}`), 0600)
			case "index":
				native(t, original.Directory, "", "read-tree", f.base)
			case "HEAD":
				err = os.WriteFile(filepath.Join(admin, "HEAD"), []byte("ref: refs/heads/main\n"), 0600)
			case "backlink":
				err = os.WriteFile(filepath.Join(admin, "gitdir"), []byte("/foreign/.git\n"), 0600)
			case "missing pin", "wrong pin":
				id, _ := revision.ID()
				ref := "refs/sheldon/reviews/" + id + "/head"
				if kind == "missing pin" {
					native(t, repo, "", "update-ref", "-d", ref)
				} else {
					native(t, repo, "", "update-ref", ref, f.base)
				}
			case "wrong revision":
				revision.MergeBaseSHA = revision.BaseSHA
			case "wrong PR":
				pr, _ = githubpr.ParseURL("https://github.com/owner/repo/pull/2")
			case "diff failure":
				t.Setenv("SHELDON_GIT_MODE", "diff-truncated")
			case "diff limit":
				t.Setenv("SHELDON_GIT_MODE", "diff-overflow")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(file)
			audit, _ := os.ReadFile(f.audit)
			prepared, diff, err := c.QuestionSource(ctx, pr, revision)
			if !errors.Is(err, ErrQuestionSourceUnavailable) || prepared != (Prepared{}) || diff.Complete || len(diff.Files) != 0 {
				t.Fatalf("unsafe context accepted: %v", err)
			}
			if strings.Contains(err.Error(), f.paths.Cache) || strings.Contains(err.Error(), "foreign") {
				t.Fatal("private path leaked")
			}
			if kind == "cancelled" && !errors.Is(err, context.Canceled) || kind == "diff limit" && !errors.Is(err, process.ErrOutputLimit) {
				t.Fatal("bounded execution cause lost")
			}
			after, _ := os.ReadFile(file)
			if !bytes.Equal(before, after) {
				t.Fatal("refusal changed existing source")
			}
			questionAudit(t, f, len(audit))
		})
	}
}
