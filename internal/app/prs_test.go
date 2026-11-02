package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

func ownedPRInstance(t *testing.T, dir string) *instance {
	t.Helper()
	i, err := openInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	if owned, err := i.acquire(); err != nil || !owned {
		i.close()
		t.Fatalf("acquire writer: %v", err)
	}
	t.Cleanup(func() { i.close() })
	return i
}

func TestPRIndexRegisterReopenAndCacheRemoval(t *testing.T) {
	paths := testPaths(t)
	i := ownedPRInstance(t, paths.Data)
	id, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	if _, err := i.ReadPR(id.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing PR lookup: %v", err)
	}
	path := filepath.Join(paths.Data, "prs.json")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lookup created the index")
	}
	pr, err := i.RegisterPR("https://github.com/Owner/Repo/pull/1?tab=files#discussion")
	if err != nil || !reflect.DeepEqual(pr, id) {
		t.Fatalf("register: %+v, %v", pr, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{pr.URL, "https://GITHUB.COM/OWNER/REPO/pull/1/"} {
		got, err := i.RegisterPR(raw)
		if err != nil || !reflect.DeepEqual(got, pr) {
			t.Fatalf("alias reopen: %+v, %v", got, err)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("duplicate registration rewrote index")
	}
	if err := os.MkdirAll(paths.Cache, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Cache, "fixture"), []byte("disposable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := i.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(paths.Cache); err != nil {
		t.Fatal(err)
	}
	reopened := ownedPRInstance(t, paths.Data)
	got, err := reopened.ReadPR(pr.ID)
	if err != nil || !reflect.DeepEqual(got, pr) {
		t.Fatalf("restart/cache removal lost PR: %+v, %v", got, err)
	}
	if _, err := reopened.ReadPR(strings.Repeat("f", 64)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unregistered ID accepted: %v", err)
	}
}

func TestConcurrentPRRegistration(t *testing.T) {
	i := ownedPRInstance(t, filepath.Join(t.TempDir(), "private data 日本語"))
	var wg sync.WaitGroup
	for n := 1; n <= 24; n++ {
		for _, spelling := range []string{"owner/repo", "OWNER/REPO"} {
			wg.Go(func() {
				_, err := i.RegisterPR(fmt.Sprintf("https://github.com/%s/pull/%d", spelling, n))
				if err != nil {
					t.Errorf("register: %v", err)
				}
			})
		}
	}
	wg.Wait()
	i.mu.Lock()
	index, err := i.readPRIndex()
	i.mu.Unlock()
	if err != nil || len(index.PRs) != 24 {
		t.Fatalf("concurrent index has %d records: %v", len(index.PRs), err)
	}
	for n := 1; n <= 24; n++ {
		pr, _ := githubpr.ParseURL(fmt.Sprintf("https://github.com/owner/repo/pull/%d", n))
		if _, err := i.ReadPR(pr.ID); err != nil {
			t.Fatalf("lost PR %d: %v", n, err)
		}
	}
}

func TestPRIndexRequiresWriterOwnership(t *testing.T) {
	paths := testPaths(t)
	owner := ownedPRInstance(t, paths.Data)
	pr, err := owner.RegisterPR("https://github.com/owner/repo/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	contender, err := openInstance(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer contender.close()
	if owned, err := contender.acquire(); err != nil || owned {
		t.Fatalf("competing writer admitted: %v", err)
	}
	for _, i := range []*instance{contender, owner} {
		if i == owner {
			if err := owner.close(); err != nil {
				t.Fatal(err)
			}
		}
		if got, err := i.RegisterPR(pr.URL); err == nil || got != (githubpr.PullRequest{}) {
			t.Fatal("unowned/closed instance registered PR")
		}
		if got, err := i.ReadPR(pr.ID); err == nil || got != (githubpr.PullRequest{}) {
			t.Fatal("unowned/closed instance read index")
		}
	}
	if owned, err := contender.acquire(); err != nil || !owned {
		t.Fatalf("could not acquire released owner: %v", err)
	}
	if _, err := contender.RegisterPR("https://github.com/owner/repo/pull/2"); err != nil {
		t.Fatal(err)
	}
}

func TestPRIndexRejectsInvalidInputBeforeStorage(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	for _, raw := range []string{"", "https://attacker.example/owner/repo/pull/1", "../../escape", "https://github.com/owner/repo/pull/01"} {
		if pr, err := i.RegisterPR(raw); err == nil || pr != (githubpr.PullRequest{}) {
			t.Fatal("invalid URL registered")
		}
	}
	for _, id := range []string{"", "../instance.lock", "/tmp/escape", strings.Repeat("A", 64)} {
		if pr, err := i.ReadPR(id); err == nil || pr != (githubpr.PullRequest{}) {
			t.Fatal("invalid lookup ID accepted")
		}
	}
	entries, err := os.ReadDir(filepath.Dir(i.file.Name()))
	if err != nil || len(entries) != 1 || entries[0].Name() != "instance.lock" {
		t.Fatalf("invalid input touched record storage: %v, %v", entries, err)
	}
}

func TestPRIndexPreservesInvalidFiles(t *testing.T) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	valid, _ := json.Marshal(prIndex{SchemaVersion: 1, PRs: []githubpr.PullRequest{pr}})
	for name, data := range map[string][]byte{
		"empty": {}, "malformed": []byte("{broken"),
		"future":          []byte(`{"schemaVersion":2,"prs":[]}`),
		"missing version": []byte(`{"prs":[]}`), "null records": []byte(`{"schemaVersion":1,"prs":null}`),
		"unknown field": []byte(`{"schemaVersion":1,"prs":[],"token":"synthetic-secret"}`),
		"trailing JSON": append(bytes.Clone(valid), []byte(" {}")...),
		"invalid utf8":  []byte("{\xff}"), "oversized": bytes.Repeat([]byte(" "), prIndexLimit+1),
		"duplicate":    fmt.Appendf(nil, `{"schemaVersion":1,"prs":[%s,%s]}`, mustPRJSON(t, pr), mustPRJSON(t, pr)),
		"bad identity": bytes.Replace(valid, []byte(`"owner":"owner"`), []byte(`"owner":"other"`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			i := ownedPRInstance(t, testPaths(t).Data)
			path := filepath.Join(filepath.Dir(i.file.Name()), "prs.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if got, err := i.RegisterPR(pr.URL); err == nil || got != (githubpr.PullRequest{}) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("invalid index falsely registered or leaked data: %v", err)
			}
			if got, err := i.ReadPR(pr.ID); err == nil || got != (githubpr.PullRequest{}) {
				t.Fatal("invalid index read as healthy")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, after) {
				t.Fatal("invalid index was changed")
			}
		})
	}
}

func mustPRJSON(t *testing.T, pr githubpr.PullRequest) []byte {
	t.Helper()
	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPRIndexReplacementFailure(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	dir := filepath.Dir(i.file.Name())
	path := filepath.Join(dir, "prs.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	i.mu.Lock()
	err := i.savePRIndex(prIndex{SchemaVersion: 1, PRs: []githubpr.PullRequest{}})
	i.mu.Unlock()
	if err == nil {
		t.Fatal("replacement failure reported success")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary file leaked: %v, %v", entries, err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("failed replacement changed existing target")
	}
}

func TestPRIndexReopenPreservesMetadata(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	title, author := "Saved display title", "example[bot]"
	pr.Title, pr.Author = &title, &author
	data, err := json.Marshal(prIndex{SchemaVersion: 1, PRs: []githubpr.PullRequest{pr}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(i.file.Name()), "prs.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := i.RegisterPR("https://github.com/OWNER/REPO/pull/1")
	if err != nil || !reflect.DeepEqual(got, pr) {
		t.Fatalf("reopen replaced saved metadata: %+v, %v", got, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("reopen rewrote saved metadata")
	}
}

func TestPRIndexAtomicVisibility(t *testing.T) {
	i := ownedPRInstance(t, testPaths(t).Data)
	if _, err := i.RegisterPR("https://github.com/owner/repo/pull/1"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(i.file.Name()), "prs.json")
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 100 {
			data, err := os.ReadFile(path)
			var index prIndex
			if err != nil || json.Unmarshal(data, &index) != nil || index.SchemaVersion != 1 || len(index.PRs) == 0 {
				t.Error("raw reader observed a partial/missing index")
				return
			}
		}
	})
	for n := 2; n <= 30; n++ {
		if _, err := i.RegisterPR(fmt.Sprintf("https://github.com/owner/repo/pull/%d", n)); err != nil {
			t.Errorf("register: %v", err)
		}
	}
	wg.Wait()
}
