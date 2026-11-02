//go:build linux || darwin

package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/github"
	"golang.org/x/sys/unix"
)

func TestUnsafeManagedStateIsPreserved(t *testing.T) {
	for _, kind := range []string{"config", "symlink", "hardlink", "fifo", "public directory", "marker", "alternates", "symbolic ref"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			c := openFixture(t, f)
			if _, err := c.Fetch(context.Background(), f.resolution); err != nil {
				t.Fatal(err)
			}
			id, _ := f.resolution.PR.RepositoryID()
			dir := filepath.Join(f.paths.Cache, "repositories", id+".git")
			target := filepath.Join(t.TempDir(), "untouched")
			if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(f.audit)
			var err error
			switch kind {
			case "config":
				err = os.WriteFile(filepath.Join(dir, "config"), []byte("[core]\n bare = true\n fsmonitor = malicious\n"), 0600)
			case "symlink":
				err = os.Symlink(target, filepath.Join(dir, "objects", "escape"))
			case "hardlink":
				err = os.Link(target, filepath.Join(dir, "objects", "escape"))
			case "fifo":
				err = unix.Mkfifo(filepath.Join(dir, "objects", "fifo"), 0600)
			case "public directory":
				err = os.Chmod(filepath.Join(dir, "objects"), 0755)
			case "marker":
				err = os.WriteFile(filepath.Join(dir, "sheldon.json"), []byte(`{"version":2}`), 0600)
			case "alternates":
				err = os.WriteFile(filepath.Join(dir, "objects", "info", "alternates"), []byte("/never/read"), 0600)
			case "symbolic ref":
				native(t, dir, "", "symbolic-ref", "refs/sheldon/pr/1/head", "refs/heads/foreign")
			}
			if err != nil {
				t.Fatal(err)
			}
			if snapshot, err := c.Fetch(context.Background(), f.resolution); err == nil || snapshot != (Snapshot{}) {
				t.Fatal("unsafe cache accepted")
			}
			after, _ := os.ReadFile(f.audit)
			if bytes.Count(before, []byte(`"Operation":"fetch"`)) != bytes.Count(after, []byte(`"Operation":"fetch"`)) {
				t.Fatal("unsafe cache reached a fetch")
			}
			data, _ := os.ReadFile(target)
			if string(data) != "keep" {
				t.Fatal("unsafe state changed its external target")
			}
		})
	}
}

func TestCacheRejectsForeignRepositoryAndPhysicalDataOverlap(t *testing.T) {
	f := newFixture(t)
	c := openFixture(t, f)
	id, _ := f.resolution.PR.RepositoryID()
	dir := filepath.Join(f.paths.Cache, "repositories", id+".git")
	native(t, f.paths.Cache, "", "init", "--bare", dir)
	if _, err := c.Fetch(context.Background(), f.resolution); err == nil {
		t.Fatal("foreign bare repository adopted")
	}
	if _, err := os.Stat(filepath.Join(dir, "sheldon.json")); !os.IsNotExist(err) {
		t.Fatal("foreign repository was marked as managed")
	}
	alias := filepath.Join(filepath.Dir(f.paths.Data), "alias")
	if err := os.Symlink(f.paths.Data, alias); err != nil {
		t.Fatal(err)
	}
	paths := f.paths
	paths.Cache = filepath.Join(alias, "disposable")
	if opened, err := Open(paths); err == nil {
		opened.Close()
		t.Fatal("physical data/cache overlap accepted")
	}
	if _, err := os.Stat(filepath.Join(f.paths.Data, "disposable")); !os.IsNotExist(err) {
		t.Fatal("overlap rejection created disposable storage under durable data")
	}
}

func TestGitEnvironmentAndGlobalConfigurationCannotEnableExecution(t *testing.T) {
	f := newFixture(t)
	root := filepath.Dir(f.upstream)
	hooks := filepath.Join(root, "configured-hooks")
	if err := os.Mkdir(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "hook-executed")
	script := "#!/bin/sh\nprintf 'executed' > \"$SHELDON_GIT_CANARY\"\n"
	if err := os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELDON_GIT_CANARY", marker)
	native(t, f.upstream, "", "-c", "core.hooksPath="+hooks, "update-ref", "refs/heads/positive-control", f.base)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("hook positive control did not execute")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(root, "global.gitconfig")
	for key, value := range map[string]string{"core.hooksPath": hooks, "core.fsmonitor": "untrusted-monitor", "fetch.prune": "true", "fetch.pruneTags": "true", "fetch.writeCommitGraph": "true", "maintenance.auto": "true", "gc.auto": "1", "trace2.eventTarget": filepath.Join(root, "trace")} {
		native(t, root, "", "config", "--file", global, key, value)
	}
	before, _ := os.ReadFile(global)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	for key, value := range map[string]string{"GIT_DIR": "/untrusted", "GIT_INDEX_FILE": "/untrusted-index", "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.hooksPath", "GIT_CONFIG_VALUE_0": hooks, "GIT_CONFIG_PARAMETERS": "'protocol.file.allow=always'", "GIT_ALLOW_PROTOCOL": "file:ext:http", "GIT_EXEC_PATH": "/untrusted-tools", "GIT_TRACE2_EVENT": filepath.Join(root, "inherited-trace")} {
		t.Setenv(key, value)
	}
	c := openFixture(t, f)
	if _, err := c.Fetch(context.Background(), f.resolution); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{marker, filepath.Join(root, "trace"), filepath.Join(root, "inherited-trace")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unwanted execution/tracing: %s", path)
		}
	}
	after, _ := os.ReadFile(global)
	if !bytes.Equal(before, after) {
		t.Fatal("global configuration changed")
	}
	if os.Getenv("GIT_DIR") != "/untrusted" {
		t.Fatal("parent environment changed")
	}
}

func TestNativeUnsafeTransportRewriteRemainsDenied(t *testing.T) {
	f := newFixture(t)
	global := filepath.Join(filepath.Dir(f.upstream), "rewrite.gitconfig")
	native(t, filepath.Dir(f.upstream), "", "config", "--file", global, "url."+f.upstream+".insteadOf", remoteURL(f.resolution.PR))
	native(t, filepath.Dir(f.upstream), "", "config", "--file", global, "protocol.file.allow", "always")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("SHELDON_GIT_MODE", "native-transport")
	c := openFixture(t, f)
	if snapshot, err := c.Fetch(context.Background(), f.resolution); err == nil || snapshot != (Snapshot{}) {
		t.Fatal("global rewrite enabled a forbidden transport")
	}
}

func TestCheckoutFilterIsNeverExecutedByFetch(t *testing.T) {
	f := newFixture(t)
	root := filepath.Dir(f.upstream)
	attributes := native(t, f.upstream, "file.txt filter=fixture\n", "hash-object", "-w", "--stdin")
	content := native(t, f.upstream, "literal source\n", "hash-object", "-w", "--stdin")
	tree := native(t, f.upstream, "100644 blob "+attributes+"\t.gitattributes\n100644 blob "+content+"\tfile.txt\n", "mktree")
	head := native(t, f.upstream, "filter source\n", "commit-tree", tree, "-p", f.base)
	native(t, f.upstream, "", "update-ref", "refs/pull/1/head", head)
	f.resolution.HeadSHA = head
	marker := filepath.Join(root, "filter-executed")
	script := filepath.Join(root, "filter.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'executed' > \"$SHELDON_GIT_FILTER\"\n/bin/cat\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELDON_GIT_FILTER", marker)
	filterCommand := "'" + strings.ReplaceAll(script, "'", "'\\''") + "'"
	worktree := filepath.Join(root, "positive-checkout")
	if err := os.Mkdir(worktree, 0700); err != nil {
		t.Fatal(err)
	}
	native(t, f.upstream, "", "--work-tree="+worktree, "-c", "filter.fixture.smudge="+filterCommand, "checkout", "--force", head, "--", ".")
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("filter positive control did not execute")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(root, "filter.gitconfig")
	native(t, root, "", "config", "--file", global, "filter.fixture.smudge", filterCommand)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	c := openFixture(t, f)
	if _, err := c.Fetch(context.Background(), f.resolution); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("fetch executed a source checkout filter")
	}
}

func TestMissingRemoteRefsReturnNoSnapshot(t *testing.T) {
	f := newFixture(t)
	c := openFixture(t, f)
	native(t, f.upstream, "", "update-ref", "-d", "refs/pull/1/head")
	if snapshot, err := c.Fetch(context.Background(), f.resolution); err == nil || snapshot != (Snapshot{}) {
		t.Fatal("missing PR head returned a snapshot")
	}
	if _, err := c.Fetch(context.Background(), github.Resolution{PR: f.resolution.PR, State: github.FallbackRequired}); err == nil {
		t.Fatal("unresolved comparison was fetched")
	}
}
