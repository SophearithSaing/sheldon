package opencode

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedBindingShapeAndPrivateFormatting(t *testing.T) {
	b := BindingRecord{SessionID: "ses_private", Directory: filepath.Join(t.TempDir(), "worktree"), Model: Model{ProviderID: "fixture", ID: "chat"}, Version: SupportedVersion, Protocol: "sheldon-opencode-http-v1", Policy: 1}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprint(b), fmt.Sprintf("%+v", b), fmt.Sprintf("%#v", b)} {
		if strings.Contains(out, b.SessionID) || strings.Contains(out, b.Directory) {
			t.Fatal("native binding leaked")
		}
	}
	for _, change := range []func(*BindingRecord){func(b *BindingRecord) { b.Directory = "relative" }, func(b *BindingRecord) { b.SessionID = "../foreign" }, func(b *BindingRecord) { b.Policy = 2 }, func(b *BindingRecord) { b.Model.ID = "" }, func(b *BindingRecord) { b.Version = "future" }, func(b *BindingRecord) { b.Directory += "\x00" }} {
		bad := b
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid binding accepted")
		}
	}
}
