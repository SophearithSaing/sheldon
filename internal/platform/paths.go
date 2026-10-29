// Package platform resolves Sheldon-owned storage, never relative to a checkout.
package platform

import (
	"errors"
	"path/filepath"
	"strings"
)

type Paths struct {
	Cache   string
	Data    string
	Runtime string
}

// Validate prevents relative paths and overlapping durable/disposable storage.
func (p Paths) Validate() error {
	for _, path := range []string{p.Cache, p.Data, p.Runtime} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("application storage requires clean absolute paths")
		}
	}
	for _, disposable := range []string{p.Cache, p.Runtime} {
		if within(p.Data, disposable) || within(disposable, p.Data) {
			return errors.New("durable data must be separate from cache and runtime storage")
		}
	}
	return nil
}

func within(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
