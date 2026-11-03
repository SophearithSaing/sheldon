//go:build linux || darwin

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffDisablesExternalDiffAndTextconvWithPositiveControls(t *testing.T) {
	f, c, revision := diffFixture(t, map[string]diffSource{"file": {data: "old\n"}}, map[string]diffSource{"file": {data: "new\n"}})
	root := filepath.Dir(f.upstream)
	marker := filepath.Join(root, "diff-executed")
	script := filepath.Join(root, "diff-script")
	t.Setenv("SHELDON_DIFF_CANARY", marker)
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf executed > \"$SHELDON_DIFF_CANARY\"\nprintf 'converted\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	command := "'" + strings.ReplaceAll(script, "'", "'\\''") + "'"
	id, _ := f.resolution.PR.RepositoryID()
	repo := filepath.Join(f.paths.Cache, "repositories", id+".git")
	if err := privateDirectory(filepath.Join(repo, "info")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "info", "attributes"), []byte("file diff=canary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-c", "diff.external=" + command, "diff", "--ext-diff", revision.MergeBaseSHA, revision.HeadSHA, "--"},
		{"-c", "diff.canary.textconv=" + command, "diff", "--textconv", revision.MergeBaseSHA, revision.HeadSHA, "--"},
	} {
		native(t, repo, "", args...)
		if _, err := os.Stat(marker); err != nil {
			t.Fatal("execution positive control did not run")
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
	}
	global := filepath.Join(root, "global.gitconfig")
	for key, value := range map[string]string{
		"diff.external": command, "diff.canary.command": command, "diff.canary.textconv": command,
		"core.quotePath": "false", "diff.noprefix": "true", "diff.mnemonicPrefix": "true", "diff.relative": "true",
		"diff.suppressBlankEmpty": "true", "diff.context": "0", "diff.interHunkContext": "1000", "diff.algorithm": "histogram",
		"diff.renames": "copies", "diff.renameEmpty": "false", "diff.ignoreSubmodules": "all", "color.ui": "always",
	} {
		native(t, root, "", "config", "--file", global, key, value)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_EXTERNAL_DIFF", command)
	t.Setenv("GIT_DIFF_OPTS", "--unified=0")
	got, err := c.Diff(context.Background(), f.resolution.PR, revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Hunks[0].Lines[0].Text != "old" || got.Files[0].Hunks[0].Lines[1].Text != "new" {
		t.Fatal("external output replaced literal source")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("restricted diff executed a driver")
	}
}
