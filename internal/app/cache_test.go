package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/github"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

func TestGitCacheBelongsToTheOwningInstance(t *testing.T) {
	paths := testPaths(t)
	i, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer i.close()
	if _, err := i.openCache(paths); err == nil {
		t.Fatal("unowned instance opened Git cache")
	}
	if _, err := os.Stat(paths.Cache); !os.IsNotExist(err) {
		t.Fatal("unowned instance created cache storage")
	}
	if owned, err := i.acquire(); err != nil || !owned {
		t.Fatal("could not acquire app writer ownership")
	}
	cache, err := i.openCache(paths)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := i.openCache(paths); err != nil || again != cache {
		t.Fatal("instance created a competing cache handle")
	}
	other := paths
	other.Cache = filepath.Join(filepath.Dir(paths.Cache), "other-cache")
	if _, err := i.openCache(other); err == nil {
		t.Fatal("owning instance changed its active cache root")
	}
	if err := i.close(); err != nil {
		t.Fatal(err)
	}
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	if _, err := cache.FetchBases(context.Background(), github.Resolution{PR: pr, State: github.BaseSelectionRequired, Failure: &github.MetadataError{Code: "cli-missing"}}); err == nil {
		t.Fatal("released writer retained a usable cache")
	}
}
