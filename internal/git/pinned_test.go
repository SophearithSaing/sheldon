package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPreparedRequiresOriginalRetainedUnmodifiedSource(t *testing.T) {
	for _, kind := range []string{"valid", "dirty", "missing marker", "missing retention", "missing repository", "missing cache", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := sourceFixture(t)
			c := openFixture(t, f)
			_, prepared := fetchPrepared(t, c, f)
			id, _ := prepared.Revision.ID()
			repositoryID, _ := f.resolution.PR.RepositoryID()
			repo := filepath.Join(f.paths.Cache, "repositories", repositoryID+".git")
			ctx := context.Background()
			switch kind {
			case "dirty":
				if err := os.WriteFile(filepath.Join(prepared.Directory, "source.txt"), []byte("keep edit"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing marker":
				if err := os.Rename(prepared.Directory+".json", prepared.Directory+".saved"); err != nil {
					t.Fatal(err)
				}
			case "missing retention":
				native(t, repo, "", "update-ref", "-d", "refs/sheldon/reviews/"+id+"/base")
			case "missing repository":
				if err := os.Rename(repo, repo+".saved"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.Rename(repo+".saved", repo); err != nil {
						t.Error(err)
					}
				}()
			case "missing cache":
				if err := os.Rename(f.paths.Cache, f.paths.Cache+".saved"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.Rename(f.paths.Cache+".saved", f.paths.Cache); err != nil {
						t.Error(err)
					}
				}()
			case "cancelled":
				bounded, cancel := context.WithCancel(ctx)
				cancel()
				ctx = bounded
			}
			got, err := c.ReadPrepared(ctx, f.resolution.PR, prepared.Revision)
			if kind == "valid" {
				if err != nil || got != prepared {
					t.Fatalf("pinned read changed identity: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrSnapshotUnavailable) || got != (Prepared{}) {
				t.Fatalf("unsafe pinned read: %v", err)
			}
			if kind == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation")
			}
			if kind == "dirty" {
				data, _ := os.ReadFile(filepath.Join(prepared.Directory, "source.txt"))
				if string(data) != "keep edit" {
					t.Fatal("historical read reset source")
				}
			}
			if kind == "missing retention" && native(t, repo, "", "for-each-ref", "--format=%(refname)", "refs/sheldon/reviews/"+id+"/base") != "" {
				t.Fatal("historical read recreated missing retention")
			}
			if kind == "missing repository" {
				if _, err := os.Stat(repo); !os.IsNotExist(err) {
					t.Fatal("historical read created a repository")
				}
			}
			if kind == "missing cache" {
				if _, err := os.Stat(f.paths.Cache); !os.IsNotExist(err) {
					t.Fatal("historical read recreated cache root")
				}
			}
		})
	}
}

func TestReadPreparedRejectsWrongRevisionAndNeverFetches(t *testing.T) {
	f := sourceFixture(t)
	c := openFixture(t, f)
	_, prepared := fetchPrepared(t, c, f)
	before, _ := os.ReadFile(f.audit)
	wrong := prepared.Revision
	wrong.BaseSHA = strings.Repeat("a", 40)
	if _, err := c.ReadPrepared(context.Background(), f.resolution.PR, wrong); !errors.Is(err, ErrSnapshotUnavailable) {
		t.Fatal("unretained revision accepted")
	}
	after, _ := os.ReadFile(f.audit)
	if strings.Count(string(before), `"Operation":"fetch"`) != strings.Count(string(after), `"Operation":"fetch"`) {
		t.Fatal("historical read fetched latest")
	}
}
