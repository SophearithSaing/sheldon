//go:build release

package ui

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleaseBundleIncludesShellFontAndLicense(t *testing.T) {
	var shell, font, license bool
	err := fs.WalkDir(assets, "dist", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", strings.TrimPrefix(name, "dist"), nil))
		if w.Code != 200 {
			t.Errorf("embedded asset %s: %d", name, w.Code)
		}
		shell = shell || name == "dist/index.html"
		font = font || strings.HasSuffix(name, ".ttf")
		license = license || strings.Contains(w.Body.String(), "SIL OPEN FONT LICENSE Version 1.1")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !shell || !font || !license {
		t.Fatal("incomplete embedded shell, font or license")
	}
}
