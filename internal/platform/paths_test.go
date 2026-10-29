package platform

import (
	"path/filepath"
	"testing"
)

func TestValidatePaths(t *testing.T) {
	base := t.TempDir()
	valid := Paths{Cache: filepath.Join(base, "cache"), Data: filepath.Join(base, "data"), Runtime: filepath.Join(base, "cache", "runtime")}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Paths{
		{Cache: "relative", Data: valid.Data, Runtime: valid.Runtime},
		{Cache: valid.Cache, Data: valid.Cache, Runtime: valid.Runtime},
		{Cache: valid.Cache, Data: filepath.Join(valid.Cache, "history"), Runtime: valid.Runtime},
		{Cache: valid.Cache, Data: valid.Data, Runtime: valid.Data},
		{Cache: valid.Cache, Data: valid.Data, Runtime: filepath.Join(valid.Data, "runtime")},
	} {
		if err := p.Validate(); err == nil {
			t.Errorf("accepted overlapping/relative paths: %+v", p)
		}
	}
}
