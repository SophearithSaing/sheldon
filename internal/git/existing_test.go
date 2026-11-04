package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenExistingNeverRecreatesMissingCacheOwnership(t *testing.T) {
	for _, missing := range []string{"root", "owner.json", "repositories"} {
		t.Run(missing, func(t *testing.T) {
			f := newFixture(t)
			path := f.paths.Cache
			if missing != "root" {
				cache, err := Open(f.paths)
				if err != nil {
					t.Fatal(err)
				}
				if err := cache.Close(); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, missing)
				if err := os.Rename(path, path+"-removed"); err != nil {
					t.Fatal(err)
				}
			}
			if cache, err := OpenExisting(f.paths); err == nil || cache != nil {
				if cache != nil {
					cache.Close()
				}
				t.Fatal("existing-only cache accepted missing ownership/source")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("existing-only cache recreated missing state")
			}
		})
	}
}
