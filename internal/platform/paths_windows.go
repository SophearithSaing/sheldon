package platform

import (
	"errors"
	"os"
	"path/filepath"
)

// Resolve uses non-roaming, per-user application storage on Windows.
func Resolve() (Paths, error) {
	base := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(base) {
		return Paths{}, errors.New("an absolute LOCALAPPDATA directory is required")
	}
	base = filepath.Join(base, "sheldon")
	p := Paths{
		Cache: filepath.Join(base, "cache"), Data: filepath.Join(base, "data"),
		Runtime: filepath.Join(base, "runtime"),
	}
	return p, p.Validate()
}
