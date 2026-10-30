//go:build !release

// Package ui serves the release shell, or development guidance without generated assets.
package ui

import (
	"io"
	"net/http"
)

// Handler serves development guidance without generated assets.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/clear-fragment.js" {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			io.WriteString(w, "history.replaceState(null, '', location.pathname + location.search)")
			return
		}
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", shellPolicy)
		io.WriteString(w, `<!doctype html><html lang="en"><meta charset="utf-8"><title>Sheldon</title><script src="/clear-fragment.js"></script><h1>Sheldon</h1><p>This development binary has no embedded UI. Use Vite with SHELDON_DEV_ORIGIN, or run npm run build for the release shell.</p></html>`)
	})
}
