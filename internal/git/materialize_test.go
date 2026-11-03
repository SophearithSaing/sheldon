package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// setSourceTree replaces the PR's source with a native fixture manifest.
func setSourceTree(t *testing.T, f *fixture, manifest string) {
	t.Helper()
	tree := native(t, f.upstream, manifest, "mktree", "--missing")
	head := native(t, f.upstream, "manifest head\n", "commit-tree", tree, "-p", f.base)
	native(t, f.upstream, "", "update-ref", "refs/pull/1/head", head)
	f.resolution.HeadSHA = head
}

func TestPrepareRawModesSubmodulesAndIndependentPRs(t *testing.T) {
	f := newFixture(t)
	content := native(t, f.upstream, "#!/bin/sh\nexit 0\n", "hash-object", "-w", "--stdin")
	module := native(t, f.upstream, "[submodule \"module\"]\n path = module\n url = ext::must-not-run\n", "hash-object", "-w", "--stdin")
	setSourceTree(t, &f, "100755 blob "+content+"\trun.sh\n100644 blob "+module+"\t.gitmodules\n160000 commit "+f.base+"\tmodule\n")
	c := openFixture(t, f)
	snapshot, first := fetchPrepared(t, c, f)
	if info, err := os.Stat(filepath.Join(first.Directory, "run.sh")); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatal("executable mode not preserved privately")
	}
	entries, err := os.ReadDir(filepath.Join(first.Directory, "module"))
	if err != nil || len(entries) != 0 {
		t.Fatal("submodule initialized instead of left empty")
	}
	other, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/2")
	otherSnapshot := snapshot
	otherSnapshot.PRID = other.ID
	second, err := c.Prepare(context.Background(), other, otherSnapshot)
	if err != nil || second.Directory == first.Directory {
		t.Fatalf("independent PR worktree: %v", err)
	}
	if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(first.Directory, "run.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(first.Directory, ".gitmodules")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(first.Directory, "module")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(first.Directory, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first.Directory); err != nil {
		t.Fatal(err)
	}
	third, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/3")
	otherSnapshot.PRID = third.ID
	if _, err := c.Prepare(context.Background(), third, otherSnapshot); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(f.paths.Cache, "repositories", snapshot.RepositoryID+".git")
	registrations, err := os.ReadDir(filepath.Join(repo, "worktrees"))
	if err != nil || len(registrations) != 3 {
		t.Fatal("new workspace pruned an earlier missing workspace registration")
	}
}

func TestPrepareRejectsUnsafeManifestAndCleansFailedMaterialization(t *testing.T) {
	for _, kind := range []string{"dotgit", "case files", "case directories", "reserved", "oversized", "foreign directory", "foreign marker"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			data := "source"
			if kind == "oversized" {
				data = strings.Repeat("x", (4<<20)+1)
			}
			blob := native(t, f.upstream, data, "hash-object", "-w", "--stdin")
			manifest := "100644 blob " + blob + "\tfile.txt\n"
			switch kind {
			case "dotgit":
				manifest = "100644 blob " + blob + "\t.Git\n"
			case "case files":
				manifest += "100644 blob " + blob + "\tFILE.txt\n"
			case "case directories":
				a := native(t, f.upstream, "100644 blob "+blob+"\ta\n", "mktree")
				b := native(t, f.upstream, "100644 blob "+blob+"\tb\n", "mktree")
				manifest = "040000 tree " + a + "\tDir\n040000 tree " + b + "\tdir\n"
			case "reserved":
				manifest = "100644 blob " + blob + "\tNUL.txt\n"
			}
			setSourceTree(t, &f, manifest)
			c := openFixture(t, f)
			snapshot, err := c.Fetch(context.Background(), f.resolution)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(f.paths.Cache, "worktrees", f.resolution.PR.ID, snapshot.HeadSHA)
			if strings.HasPrefix(kind, "foreign") {
				if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "foreign directory" {
					if err := os.Mkdir(root, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, "keep"), []byte("foreign"), 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(root+".json", []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err == nil || got != (Prepared{}) {
				t.Fatal("unsafe source accepted")
			}
			if kind == "foreign directory" {
				if data, err := os.ReadFile(filepath.Join(root, "keep")); err != nil || string(data) != "foreign" {
					t.Fatal("foreign directory changed")
				}
			} else if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("failed preparation left a worktree")
			}
			if kind == "foreign marker" {
				if data, err := os.ReadFile(root + ".json"); err != nil || string(data) != "foreign" {
					t.Fatal("foreign marker changed")
				}
			}
		})
	}
}

func TestSourcePathValidation(t *testing.T) {
	for _, name := range []string{"../file", "/file", "a/../../file", ".git/config", "a/.GIT/config", "git~1/config", "NUL.txt", "LPT1", "file.", "file ", "a\\b", "a:b", "a?b", "a*b", "a\nb", "a\u200db"} {
		if validSourcePath(name) {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{".gitattributes", ".gitmodules", "a/b.go", "日本語/source.txt"} {
		if !validSourcePath(name) {
			t.Errorf("rejected %q", name)
		}
	}
}
