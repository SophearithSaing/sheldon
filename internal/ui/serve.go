package ui

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

const shellPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// assetHandler serves embedded files with explicit MIME types.
func assetHandler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		contentType := ""
		if name == "index.html" {
			contentType = "text/html; charset=utf-8"
		} else if strings.HasPrefix(name, "assets/") && fs.ValidPath(name) && !strings.Contains(name, "\\") {
			switch path.Ext(name) {
			case ".js":
				contentType = "text/javascript; charset=utf-8"
			case ".css":
				contentType = "text/css; charset=utf-8"
			case ".ttf":
				contentType = "font/ttf"
			case ".txt":
				contentType = "text/plain; charset=utf-8"
			}
		}
		if contentType == "" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		if name == "index.html" {
			w.Header().Set("Content-Security-Policy", shellPolicy)
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
