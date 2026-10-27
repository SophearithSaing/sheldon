//go:build linux || darwin

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Resolve returns the current user's cache, data, and runtime paths for Sheldon.
func Resolve() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return resolveUnix(runtime.GOOS, home)
}

// resolveUnix returns Sheldon storage paths for the given operating system and home directory.
func resolveUnix(goos, home string) (Paths, error) {
	if !filepath.IsAbs(home) {
		return Paths{}, errors.New("an absolute user home directory is required")
	}
	var p Paths
	if goos == "darwin" {
		p.Cache = filepath.Join(home, "Library", "Caches", "sheldon")
		p.Data = filepath.Join(home, "Library", "Application Support", "sheldon")
	} else {
		cache := os.Getenv("XDG_CACHE_HOME")
		if cache == "" {
			cache = filepath.Join(home, ".cache")
		}
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		run := os.Getenv("XDG_RUNTIME_DIR")
		for _, base := range []string{cache, data, run} {
			if base != "" && !filepath.IsAbs(base) {
				return Paths{}, errors.New("XDG storage overrides must be absolute")
			}
		}
		p.Cache, p.Data = filepath.Join(cache, "sheldon"), filepath.Join(data, "sheldon")
		if run != "" {
			p.Runtime = filepath.Join(run, "sheldon")
		}
	}
	if p.Runtime == "" {
		// Private, per-user fallback: no shared /tmp name, and no durable history.
		p.Runtime = filepath.Join(p.Cache, "runtime")
	}
	return p, p.Validate()
}
