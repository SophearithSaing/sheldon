//go:build linux || darwin

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareContainedSymlinksAndRejectsUnsafeLinks(t *testing.T) {
	for _, target := range []string{"file.txt", "../escape", "/absolute", ".git/config", "missing", "link"} {
		t.Run(target, func(t *testing.T) {
			f := newFixture(t)
			blob := native(t, f.upstream, "literal\n", "hash-object", "-w", "--stdin")
			link := native(t, f.upstream, target, "hash-object", "-w", "--stdin")
			setSourceTree(t, &f, "100644 blob "+blob+"\tfile.txt\n120000 blob "+link+"\tlink\n")
			c := openFixture(t, f)
			snapshot, err := c.Fetch(context.Background(), f.resolution)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Prepare(context.Background(), f.resolution.PR, snapshot)
			if target != "file.txt" {
				if err == nil || got != (Prepared{}) {
					t.Fatal("unsafe symlink accepted")
				}
				root := filepath.Join(f.paths.Cache, "worktrees", f.resolution.PR.ID, snapshot.HeadSHA)
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatal("unsafe symlink failure left source")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if value, err := os.Readlink(filepath.Join(got.Directory, "link")); err != nil || value != target {
				t.Fatal("symlink not preserved")
			}
			if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(got.Directory, "link")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("missing", filepath.Join(got.Directory, "link")); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err == nil {
				t.Fatal("modified symlink reused")
			}
		})
	}
}

func TestPrepareNeverExecutesHooksOrAttributeFilters(t *testing.T) {
	f := newFixture(t)
	root := filepath.Dir(f.upstream)
	hooks := filepath.Join(root, "hooks")
	if err := os.Mkdir(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "executed")
	t.Setenv("SHELDON_WORKTREE_CANARY", marker)
	script := "#!/bin/sh\nprintf 'executed' > \"$SHELDON_WORKTREE_CANARY\"\n/bin/cat\n"
	for _, name := range []string{"post-checkout", "reference-transaction"} {
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	filter := filepath.Join(root, "filter.sh")
	if err := os.WriteFile(filter, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	command := "'" + strings.ReplaceAll(filter, "'", "'\\''") + "'"
	attributes := native(t, f.upstream, "file.txt filter=canary\n", "hash-object", "-w", "--stdin")
	content := native(t, f.upstream, "literal\n", "hash-object", "-w", "--stdin")
	setSourceTree(t, &f, "100644 blob "+attributes+"\t.gitattributes\n100644 blob "+content+"\tfile.txt\n")
	control := filepath.Join(root, "control")
	native(t, f.upstream, "", "-c", "core.hooksPath="+hooks, "-c", "filter.canary.smudge="+command, "worktree", "add", "--detach", control, f.resolution.HeadSHA)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("positive control did not execute")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(root, "global.gitconfig")
	for key, value := range map[string]string{"core.hooksPath": hooks, "filter.canary.smudge": command, "filter.canary.clean": command, "filter.canary.required": "true", "core.autocrlf": "true"} {
		native(t, root, "", "config", "--file", global, key, value)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	c := openFixture(t, f)
	snapshot, prepared := fetchPrepared(t, c, f)
	if data, err := os.ReadFile(filepath.Join(prepared.Directory, "file.txt")); err != nil || string(data) != "literal\n" {
		t.Fatal("source was transformed")
	}
	if _, err := c.Prepare(context.Background(), f.resolution.PR, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("preparation/reuse executed a hook or filter")
	}
}
