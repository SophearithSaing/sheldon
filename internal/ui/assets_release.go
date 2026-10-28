//go:build release

// Package ui serves the release shell, or development guidance without generated assets.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var assets embed.FS

// Handler serves only the embedded shell and its bundled assets, never source files.
func Handler() http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return assetHandler(root)
}
