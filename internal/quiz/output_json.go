package quiz

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// decodeOutput rejects malformed or ambiguous quiz JSON before strict typed decoding.
func decodeOutput(data []byte, out any) bool {
	if !utf8.Valid(data) || !json.Valid(data) || !validEscapes(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if !outputValue(d, 0) {
		return false
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil && d.Decode(new(any)) == io.EOF
}

// outputValue bounds nesting and rejects duplicate or noncanonical property names.
func outputValue(d *json.Decoder, depth int) bool {
	if depth > 16 {
		return false
	}
	token, err := d.Token()
	if err != nil {
		return false
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return true
	}
	if delim != '{' && delim != '[' {
		return false
	}
	keys := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || keys[name] || !outputKey(name) {
				return false
			}
			keys[name] = true
		}
		if !outputValue(d, depth+1) {
			return false
		}
	}
	end, err := d.Token()
	return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
}

// outputKey permits canonical native/saved spellings, not case-insensitive field aliases.
func outputKey(name string) bool {
	switch name {
	case "questions", "prompt", "choices", "correctChoiceID", "explanation", "evidence", "fileID", "hunkID", "side", "start", "end":
		return true
	case "schemaVersion", "prID", "version", "quizID", "revision", "revisionID", "baseBranch", "baseSource", "baseSHA", "headSHA", "mergeBaseSHA", "questionID", "choiceID", "text", "anchor", "oldPath", "newPath", "selected", "surrounding", "id", "old", "new", "count", "section", "lines", "sign", "oldLine", "newLine", "noNewline":
		return true
	}
	return false
}

// validEscapes refuses unpaired UTF-16 surrogates that encoding/json would silently replace.
func validEscapes(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		value, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value < 0xd800 || value > 0xdbff {
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
