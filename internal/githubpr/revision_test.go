package githubpr

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRevisionIdentity(t *testing.T) {
	t.Setenv("PATH", "")
	pr, _ := ParseURL("https://github.com/owner/repo/pull/1")
	r := Revision{PRID: pr.ID, BaseBranch: "feat/nondefault", BaseSource: GitHubBase,
		BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("c", 40)}
	if err := r.Validate(pr.ID); err != nil {
		t.Fatal(err)
	}
	id, err := r.ID()
	if err != nil || id != "be970a1636ccf4780f070aedc64cb2d61cee90ef0c214dffe76575fc424c1547" {
		t.Fatalf("revision ID: %q, %v", id, err)
	}
	for name, mutate := range map[string]func(*Revision){
		"base tip":   func(r *Revision) { r.BaseSHA = strings.Repeat("d", 40) },
		"head":       func(r *Revision) { r.HeadSHA = strings.Repeat("d", 40) },
		"merge base": func(r *Revision) { r.MergeBaseSHA = strings.Repeat("d", 40) },
		"PR":         func(r *Revision) { r.PRID = strings.Repeat("d", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := r
			mutate(&changed)
			if got, err := changed.ID(); err != nil || got == id {
				t.Fatalf("changed comparison reused identity: %q, %v", got, err)
			}
		})
	}
	manual := r
	manual.BaseSource, manual.BaseBranch = UserSelectedBase, "another/target"
	if got, err := manual.ID(); err != nil || got != id {
		t.Fatalf("provenance changed comparison identity: %q, %v", got, err)
	}
	other, _ := ParseURL("https://github.com/owner/repo/pull/2")
	if r.Validate(other.ID) == nil {
		t.Fatal("accepted cross-PR revision")
	}
	for _, size := range []int{40, 64} {
		r.BaseSHA, r.HeadSHA, r.MergeBaseSHA = strings.Repeat("a", size), strings.Repeat("a", size), strings.Repeat("a", size)
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var restored Revision
		if err := json.Unmarshal(data, &restored); err != nil || restored.Validate(pr.ID) != nil || !reflect.DeepEqual(r, restored) {
			t.Fatalf("revision round trip: %+v, %v", restored, err)
		}
	}
}

func TestInvalidRevision(t *testing.T) {
	pr, _ := ParseURL("https://github.com/owner/repo/pull/1")
	r := Revision{PRID: pr.ID, BaseBranch: "main", BaseSource: GitHubBase,
		BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("c", 40)}
	for name, mutate := range map[string]func(*Revision){
		"missing PR":     func(r *Revision) { r.PRID = "" },
		"missing source": func(r *Revision) { r.BaseSource = "" },
		"unknown source": func(r *Revision) { r.BaseSource = "default-branch" },
		"zero base":      func(r *Revision) { r.BaseSHA = strings.Repeat("0", 40) },
		"short head":     func(r *Revision) { r.HeadSHA = "a81c2f4" },
		"uppercase":      func(r *Revision) { r.HeadSHA = strings.Repeat("A", 40) },
		"nonhex":         func(r *Revision) { r.HeadSHA = strings.Repeat("g", 40) },
		"missing merge":  func(r *Revision) { r.MergeBaseSHA = "" },
		"mixed formats":  func(r *Revision) { r.HeadSHA = strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := r
			mutate(&invalid)
			if invalid.Validate(pr.ID) == nil {
				t.Fatal("accepted invalid revision")
			}
			if id, err := invalid.ID(); err == nil || id != "" {
				t.Fatal("invalid revision produced an ID")
			}
		})
	}
	if (Revision{}).Validate("") == nil {
		t.Fatal("accepted zero revision")
	}
}

func TestRevisionBranchNames(t *testing.T) {
	pr, _ := ParseURL("https://github.com/owner/repo/pull/1")
	r := Revision{PRID: pr.ID, BaseSource: UserSelectedBase,
		BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), MergeBaseSHA: strings.Repeat("c", 40)}
	for _, branch := range []string{"main", "feat/go-rewrite", "修正/変更", "feature/$literal;name"} {
		r.BaseBranch = branch
		if err := r.Validate(pr.ID); err != nil {
			t.Errorf("rejected literal branch %q: %v", branch, err)
		}
	}
	for _, branch := range []string{
		"", "@", "--help", "/main", "main/", "feat//x", ".hidden", "feat/.hidden", "main.lock",
		"feat.lock/x", "main.", "a..b", "a@{b", "a b", "a\nb", "a\x00b", "a\x7fb", "a~b", "a^b",
		"a:b", "a?b", "a*b", "a[b", "a\\b", "\xff", strings.Repeat("a", 1025),
	} {
		r.BaseBranch = branch
		if r.Validate(pr.ID) == nil {
			t.Errorf("accepted invalid branch %q", branch)
		}
	}
}
