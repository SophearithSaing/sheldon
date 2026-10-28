package ui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAssetsAreExactTypedFiles(t *testing.T) {
	files := fstest.MapFS{
		"index.html":        {Data: []byte("<title>Sheldon</title>")},
		"assets/app.js":     {Data: []byte("export {}")},
		"assets/app.css":    {Data: []byte("body {}")},
		"assets/font.ttf":   {Data: []byte("font")},
		"assets/OFL.txt":    {Data: []byte("license")},
		"assets/app.js.map": {Data: []byte("private source")},
		"instance.lock":     {Data: []byte("private credential")},
	}
	h := assetHandler(files)
	for _, tt := range []struct{ path, contentType string }{
		{"/", "text/html; charset=utf-8"},
		{"/index.html", "text/html; charset=utf-8"},
		{"/assets/app.js", "text/javascript; charset=utf-8"},
		{"/assets/app.css", "text/css; charset=utf-8"},
		{"/assets/font.ttf", "font/ttf"},
		{"/assets/OFL.txt", "text/plain; charset=utf-8"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != tt.contentType {
			t.Fatalf("%s: status %d, type %s", tt.path, w.Code, w.Header().Get("Content-Type"))
		}
		if strings.Contains(tt.contentType, "html") && w.Header().Get("Content-Security-Policy") != shellPolicy {
			t.Fatal("HTML missing shell policy")
		}
	}
	for _, path := range []string{"/api/unknown", "/assets", "/assets/", "/assets/app.js.map", "/assets/missing.js", "/src/main.ts", "/instance.lock", "/assets/../index.html", "/assets/%2e%2e/instance.lock", "/route"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 || strings.Contains(w.Body.String(), "private") {
			t.Fatalf("unexpected fallback or file exposure for %s: %d", path, w.Code)
		}
	}
}
