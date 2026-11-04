package question

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPublicHistoryIsBoundedDetachedAndPrivate(t *testing.T) {
	s := savedFixture()
	s.Version = ^uint64(0)
	s.Threads[0].Answers[0].Question = strings.Repeat(" ", 300) + strings.Repeat("日", 100)
	list := s.PublicList()
	if list.Version != "18446744073709551615" || len(list.Threads[0].Question) > 256 || !utf8.ValidString(list.Threads[0].Question) || strings.TrimSpace(list.Threads[0].Question) == "" {
		t.Fatal("excerpt/version lost its bounded literal representation")
	}
	detail, err := s.PublicThread(s.Threads[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{s.Bindings[0].Native.SessionID, s.Bindings[0].Native.Directory, `"native"`, `"directory"`, `"permissions"`, `"model"`} {
		if strings.Contains(string(data), secret) {
			t.Fatal("private native state escaped history projection")
		}
	}
	detail.Context.Selected[0].Text = "mutated"
	detail.Context.Surrounding.Lines[0].Text = "mutated"
	detail.Answers[0].Text = "mutated"
	if s.Threads[0].Context.Selected[0].Text == "mutated" || s.Threads[0].Context.Surrounding.Lines[0].Text == "mutated" || s.Threads[0].Answers[0].Text == "mutated" {
		t.Fatal("public data aliased private snapshot")
	}
	s.Bindings[0].Native.Policy++
	detail, err = s.PublicThread(s.Threads[0].ID)
	if err != nil || detail.Session.State != "unavailable" || detail.Session.Reason != "incompatible-binding" {
		t.Fatal("incompatible history claimed native readiness")
	}
}
