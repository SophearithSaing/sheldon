package semantic

import (
	"bytes"
	"strings"
	"testing"
)

func TestSavedStrictRoundTrip(t *testing.T) {
	s, a := fixture(t)
	c, err := s.Parse(native(t, nativeGroup{"Purpose", "Why", []Anchor{a}}))
	if err != nil {
		t.Fatal(err)
	}
	saved := Saved{SchemaVersion: 1, PRID: c.Diff.Revision.PRID, Version: 1, ReviewID: strings.Repeat("f", 64), Content: c}
	b, err := saved.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := DecodeSaved(b, saved.PRID)
	if err != nil || len(again.Ungrouped()) != 2 {
		t.Fatalf("roundtrip: %v", err)
	}
	for _, bad := range [][]byte{append(b, []byte(`{}`)...), bytes.Replace(b, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":2`), 1), bytes.Replace(b, []byte(`"version":1`), []byte(`"version":0`), 1), bytes.Replace(b, []byte(`"groups":`), []byte(`"Groups":`), 1)} {
		if _, err := DecodeSaved(bad, saved.PRID); err == nil {
			t.Fatal("corrupt storage accepted")
		}
	}
}
