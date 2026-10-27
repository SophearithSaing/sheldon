package platform

import (
	"path/filepath"
	"testing"
)

func TestWindowsPaths(t *testing.T) {
	base := filepath.Join(t.TempDir(), "App Data 日本語")
	t.Setenv("LOCALAPPDATA", base)
	got, err := Resolve()
	want := Paths{filepath.Join(base, "sheldon", "cache"), filepath.Join(base, "sheldon", "data"), filepath.Join(base, "sheldon", "runtime")}
	if err != nil || got != want {
		t.Fatalf("paths=%+v error=%v; want %+v", got, err, want)
	}
	t.Setenv("LOCALAPPDATA", "relative")
	if _, err := Resolve(); err == nil {
		t.Fatal("relative LOCALAPPDATA accepted")
	}
}
