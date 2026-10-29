//go:build linux || darwin

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnixPaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Home with spaces 日本語")
	for _, tt := range []struct {
		name, goos string
		env        map[string]string
		want       Paths
	}{
		{"linux defaults", "linux", nil, Paths{filepath.Join(home, ".cache", "sheldon"), filepath.Join(home, ".local", "share", "sheldon"), filepath.Join(home, ".cache", "sheldon", "runtime")}},
		{"macOS ignores XDG", "darwin", map[string]string{"XDG_DATA_HOME": "relative"}, Paths{filepath.Join(home, "Library", "Caches", "sheldon"), filepath.Join(home, "Library", "Application Support", "sheldon"), filepath.Join(home, "Library", "Caches", "sheldon", "runtime")}},
		{"XDG overrides", "linux", map[string]string{"XDG_CACHE_HOME": filepath.Join(home, "custom-cache"), "XDG_DATA_HOME": filepath.Join(home, "custom-data"), "XDG_RUNTIME_DIR": filepath.Join(home, "custom-run")}, Paths{filepath.Join(home, "custom-cache", "sheldon"), filepath.Join(home, "custom-data", "sheldon"), filepath.Join(home, "custom-run", "sheldon")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{"XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"} {
				t.Setenv(key, tt.env[key])
			}
			got, err := resolveUnix(tt.goos, home)
			if err != nil || got != tt.want {
				t.Fatalf("paths=%+v error=%v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestRejectRelativeXDG(t *testing.T) {
	for _, key := range []string{"XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"} {
		t.Run(key, func(t *testing.T) {
			for _, name := range []string{"XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"} {
				t.Setenv(name, "")
			}
			t.Setenv(key, "relative")
			_, err := resolveUnix("linux", t.TempDir())
			if err == nil {
				t.Fatal("relative XDG path accepted")
			}
		})
	}
}

func TestResolveUsesUserEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !within(p.Data, home) || !within(p.Cache, home) || !within(p.Runtime, home) {
		t.Fatalf("paths escaped fake home: %+v", p)
	}
	for _, path := range []string{p.Data, p.Cache, p.Runtime} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("path resolution had filesystem side effects")
		}
	}
}
