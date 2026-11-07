package semantic

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// decode rejects ambiguous JSON keys, invalid Unicode and unknown typed properties.
func decode(data []byte, out any) bool {
	if !utf8.Valid(data) || !json.Valid(data) || !validEscapes(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if !jsonValue(d, 0) {
		return false
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil && d.Decode(new(any)) == io.EOF
}

// jsonValue rejects duplicate/case-alias properties and excessive nesting before typed decoding.
func jsonValue(d *json.Decoder, depth int) bool {
	if depth > 24 {
		return false
	}
	t, err := d.Token()
	if err != nil {
		return false
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return true
	}
	if delim != '{' && delim != '[' {
		return false
	}
	keys := map[string]bool{}
	for d.More() {
		if delim == '{' {
			t, err := d.Token()
			k, ok := t.(string)
			lower := strings.ToLower(k)
			if err != nil || !ok || keys[lower] || !canonicalKey(k) {
				return false
			}
			keys[lower] = true
		}
		if !jsonValue(d, depth+1) {
			return false
		}
	}
	end, err := d.Token()
	return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
}

// canonicalKey refuses encoding/json's case-insensitive aliases in native, saved and request records.
func canonicalKey(k string) bool {
	switch k {
	case "groups", "title", "explanation", "ranges", "groupID", "fileID", "hunkID", "side", "start", "end", "diff", "revision", "revisionID", "complete", "files", "id", "status", "oldPath", "newPath", "oldMode", "newMode", "oldObject", "newObject", "similarity", "kind", "reason", "hunks", "old", "new", "count", "section", "lines", "sign", "text", "oldLine", "newLine", "noNewline", "prID", "baseBranch", "baseSource", "baseSHA", "headSHA", "mergeBaseSHA", "schemaVersion", "version", "reviewID", "expectedVersion":
		return true
	}
	return false
}

// validEscapes rejects unpaired surrogates rather than accepting lossy JSON Unicode replacement.
func validEscapes(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		n, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, _ := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
