package githubpr

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalPullRequest(t *testing.T) {
	t.Setenv("PATH", "")
	const id = "05754fd3451b548ff4a285f148f0df785149a9715a3e2704dcca75dba60d03c4"
	const repositoryID = "620c3a937ce854b7477cb2104a95692bbb64e64a90184e0d37d48ddfc20ec6f8"
	for _, raw := range []string{
		"https://github.com/owner/repo/pull/1",
		"https://GITHUB.COM/OWNER/Repo/pull/1/?tab=files#discussion",
	} {
		pr, err := ParseURL(raw)
		if err != nil || pr.Validate() != nil {
			t.Fatalf("parse %q: %+v, %v", raw, pr, err)
		}
		if pr.ID != id || pr.URL != "https://github.com/owner/repo/pull/1" || pr.Owner != "owner" || pr.Repository != "repo" || pr.Number != 1 || pr.Title != nil || pr.Author != nil {
			t.Fatalf("unexpected record: %+v", pr)
		}
		if got, err := pr.RepositoryID(); err != nil || got != repositoryID {
			t.Fatalf("repository ID: %q, %v", got, err)
		}
		if got, err := ReviewPath(pr.ID); err != nil || got != "/reviews/"+id {
			t.Fatalf("route: %q, %v", got, err)
		}
	}
	first, _ := ParseURL("https://github.com/owner/repo/pull/1")
	second, _ := ParseURL("https://github.com/owner/repo/pull/2")
	other, _ := ParseURL("https://github.com/owner/other/pull/1")
	firstRepo, _ := first.RepositoryID()
	secondRepo, _ := second.RepositoryID()
	otherRepo, _ := other.RepositoryID()
	if first.ID == second.ID || firstRepo != secondRepo || first.ID == other.ID || firstRepo == otherRepo || first.ID == firstRepo {
		t.Fatal("PR and repository identities were conflated")
	}
}

func TestPullRequestJSON(t *testing.T) {
	pr, err := ParseURL("https://github.com/owner/repo/pull/18446744073709551615")
	if err != nil {
		t.Fatal(err)
	}
	for _, known := range []bool{false, true} {
		if known {
			title, author := "Explain <source> changes", "example[bot]"
			pr.Title, pr.Author = &title, &author
		}
		data, err := json.Marshal(pr)
		if err != nil || pr.Validate() != nil {
			t.Fatalf("marshal valid PR: %v", err)
		}
		if !strings.Contains(string(data), `"number":"18446744073709551615"`) {
			t.Fatalf("PR number lost precision: %s", data)
		}
		if !known && (!strings.Contains(string(data), `"title":null`) || !strings.Contains(string(data), `"author":null`)) {
			t.Fatalf("unknown metadata not explicit: %s", data)
		}
		var restored PullRequest
		if err := json.Unmarshal(data, &restored); err != nil || restored.Validate() != nil || !reflect.DeepEqual(pr, restored) {
			t.Fatalf("round trip failed: %+v, %v", restored, err)
		}
	}
}

func TestIndependentPullRequestMetadata(t *testing.T) {
	pr, _ := ParseURL("https://github.com/owner/repo/pull/1")
	title, author := "A title", "example[bot]"
	for _, fields := range []struct{ title, author *string }{
		{&title, nil}, {nil, &author},
	} {
		pr.Title, pr.Author = fields.title, fields.author
		if err := pr.Validate(); err != nil {
			t.Fatalf("independently unknown metadata: %v", err)
		}
	}
	for _, value := range []string{"", "\t", strings.Repeat("a", 257), "name\x00", "\xff"} {
		pr.Author = &value
		if pr.Validate() == nil {
			t.Fatal("accepted invalid author")
		}
	}
	title, author = strings.Repeat("a", 512), strings.Repeat("a", 256)
	pr.Title, pr.Author = &title, &author
	if err := pr.Validate(); err != nil {
		t.Fatalf("metadata boundary rejected: %v", err)
	}
}

func TestInvalidPullRequestRecords(t *testing.T) {
	pr, _ := ParseURL("https://github.com/owner/repo/pull/1")
	for name, mutate := range map[string]func(*PullRequest){
		"id":         func(p *PullRequest) { p.ID = strings.Repeat("a", 64) },
		"url alias":  func(p *PullRequest) { p.URL += "?tab=files" },
		"url case":   func(p *PullRequest) { p.URL = "https://github.com/Owner/repo/pull/1" },
		"owner":      func(p *PullRequest) { p.Owner = "OWNER" },
		"repository": func(p *PullRequest) { p.Repository = "other" },
		"number":     func(p *PullRequest) { p.Number = 2 },
		"empty title": func(p *PullRequest) {
			value := " "
			p.Title = &value
		},
		"long title": func(p *PullRequest) {
			value := strings.Repeat("x", 513)
			p.Title = &value
		},
		"control author": func(p *PullRequest) {
			value := "name\nsecret"
			p.Author = &value
		},
		"invalid utf8": func(p *PullRequest) {
			value := "\xff"
			p.Title = &value
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := pr
			mutate(&invalid)
			if invalid.Validate() == nil {
				t.Fatal("accepted invalid record")
			}
			if id, err := invalid.RepositoryID(); err == nil || id != "" {
				t.Fatal("invalid record produced a repository ID")
			}
		})
	}
	if (PullRequest{}).Validate() == nil {
		t.Fatal("accepted zero record")
	}
	for _, id := range []string{"", "../review", strings.Repeat("0", 64), strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		if ValidID(id) {
			t.Errorf("accepted ID %q", id)
		}
		if path, err := ReviewPath(id); err == nil || path != "" {
			t.Errorf("invalid ID produced route %q", path)
		}
	}
}

func TestParseURLRejectsInvalidInput(t *testing.T) {
	t.Setenv("PATH", "")
	for _, raw := range []string{
		"", "http://github.com/owner/repo/pull/1", "https://github.com:443/owner/repo/pull/1",
		"https://user@github.com/owner/repo/pull/1", "https://github.com/owner/repo/pull/01",
		"https://github.com/owner/repo/pull/0", "https://github.com/owner/repo/pull/18446744073709551616",
		"https://github.com/owner/repo/pull/%31", "https://github.com/owner/repo/pull/1/files",
		"https://github.com/owner/repo/pull/$(id)", "https://github.com/owner/../pull/1",
	} {
		if pr, err := ParseURL(raw); err == nil || pr != (PullRequest{}) {
			t.Errorf("invalid input returned usable PR: %+v, %v", pr, err)
		}
	}
}

func TestReviewRouteShape(t *testing.T) {
	pr, _ := ParseURL("https://github.com/owner/repo/pull/1")
	path, _ := ReviewPath(pr.ID)
	if id, ok := ReviewID(path); !ok || id != pr.ID {
		t.Fatal("valid review route not recognized")
	}
	for _, path := range []string{"/", "/reviews/invalid", "/reviews/" + pr.ID + "/", "/reviews/" + pr.ID + "?token=secret", "/reviews/" + pr.ID + "#token=secret", "https://attacker.example/reviews/" + pr.ID, "//reviews/" + pr.ID} {
		if _, ok := ReviewID(path); ok {
			t.Errorf("accepted route %q", path)
		}
	}
}
