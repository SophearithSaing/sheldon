package git

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// parseDiff checks the NUL-delimited manifest and every corresponding patch block.
func parseDiff(output []byte, revision githubpr.Revision) (Diff, error) {
	if len(output) > 4<<20 {
		return Diff{}, errors.New("diff output exceeds the parsing limit")
	}
	id, err := revision.ID()
	if err != nil {
		return Diff{}, err
	}
	result := Diff{Revision: revision, RevisionID: id, Files: []DiffFile{}}
	seen := make(map[string]bool)
	for len(output) > 0 && output[0] == ':' {
		if len(result.Files) >= 1000 {
			return Diff{}, errors.New("diff exceeds 1000 changed files")
		}
		header, rest, ok := bytes.Cut(output, []byte{0})
		if !ok {
			return Diff{}, errors.New("incomplete raw diff header")
		}
		fields := strings.Fields(string(header[1:]))
		if len(fields) != 5 {
			return Diff{}, errors.New("invalid raw diff header")
		}
		file := DiffFile{OldMode: fields[0], NewMode: fields[1], OldObject: fields[2], NewObject: fields[3], Hunks: []DiffHunk{}}
		if !validDiffMode(file.OldMode) || !validDiffMode(file.NewMode) || !validDiffObject(file.OldObject, len(revision.HeadSHA)) || !validDiffObject(file.NewObject, len(revision.HeadSHA)) {
			return Diff{}, errors.New("invalid raw diff identities")
		}
		name, rest, ok := bytes.Cut(rest, []byte{0})
		if !ok || len(name) == 0 || !utf8.Valid(name) {
			return Diff{}, errors.New("unavailable or non-UTF-8 diff path")
		}
		file.OldPath, file.NewPath = string(name), string(name)
		status := fields[4]
		switch status {
		case "A":
			file.Status, file.OldPath = "added", ""
		case "D":
			file.Status, file.NewPath = "deleted", ""
		case "M":
			file.Status = "modified"
		case "T":
			file.Status = "type-changed"
		default:
			if len(status) < 2 || status[0] != 'R' && status[0] != 'C' {
				return Diff{}, errors.New("unsupported raw diff status")
			}
			file.Similarity, err = strconv.Atoi(status[1:])
			if err != nil || file.Similarity < 0 || file.Similarity > 100 {
				return Diff{}, errors.New("invalid rename similarity")
			}
			file.Status = "renamed"
			if status[0] == 'C' {
				file.Status = "copied"
			}
			name, rest, ok = bytes.Cut(rest, []byte{0})
			if !ok || len(name) == 0 || !utf8.Valid(name) {
				return Diff{}, errors.New("incomplete rename paths")
			}
			file.NewPath = string(name)
		}
		if (file.OldMode == "000000") != (file.OldPath == "") || (file.NewMode == "000000") != (file.NewPath == "") || (strings.Trim(file.OldObject, "0") == "") != (file.OldPath == "") || (strings.Trim(file.NewObject, "0") == "") != (file.NewPath == "") {
			return Diff{}, errors.New("raw diff status contradicts its objects")
		}
		file.ID = diffID(id, "file", file.OldPath, file.NewPath)
		if seen[file.ID] {
			return Diff{}, errors.New("duplicate raw diff file")
		}
		seen[file.ID] = true
		result.Files = append(result.Files, file)
		output = rest
	}
	if len(result.Files) == 0 {
		if len(output) != 0 {
			return Diff{}, errors.New("diff has no corresponding manifest")
		}
		result.Complete = true
		return result, nil
	}
	if len(output) == 0 || output[0] != 0 {
		return Diff{}, errors.New("diff patch separator is missing")
	}
	output = output[1:]
	var lines []string
	for len(output) != 0 {
		line, rest, ok := bytes.Cut(output, []byte{'\n'})
		if !ok || len(lines) >= 100000 {
			return Diff{}, errors.New("diff is unterminated or exceeds 100000 patch lines")
		}
		lines = append(lines, string(line))
		output = rest
	}
	for i := range result.Files {
		if len(lines) == 0 {
			return Diff{}, errors.New("diff is missing a file patch")
		}
		end := 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "diff --git ") {
			end++
		}
		if result.Files[i].Status == "type-changed" {
			second := end + 1
			for second < len(lines) && !strings.HasPrefix(lines[second], "diff --git ") {
				second++
			}
			if end == len(lines) {
				return Diff{}, errors.New("type change is missing its added-side patch")
			}
			deleted, added := result.Files[i], result.Files[i]
			deleted.Status, deleted.NewPath, deleted.NewMode, deleted.NewObject = "deleted", "", "000000", strings.Repeat("0", len(revision.HeadSHA))
			added.Status, added.OldPath, added.OldMode, added.OldObject = "added", "", "000000", strings.Repeat("0", len(revision.HeadSHA))
			if err := parseFilePatch(&deleted, lines[:end]); err != nil {
				return Diff{}, err
			}
			if err := parseFilePatch(&added, lines[end:second]); err != nil {
				return Diff{}, err
			}
			result.Files[i].Kind = "type-change"
			result.Files[i].Reason = "File type changes have no unified text view."
			lines = lines[second:]
			continue
		}
		if err := parseFilePatch(&result.Files[i], lines[:end]); err != nil {
			return Diff{}, err
		}
		lines = lines[end:]
	}
	if len(lines) != 0 {
		return Diff{}, errors.New("diff has unexpected extra file patches")
	}
	result.Complete = true
	return result, nil
}

// validDiffMode accepts Git's six-digit octal modes, including unsupported file types.
func validDiffMode(mode string) bool {
	if len(mode) != 6 {
		return false
	}
	for _, digit := range mode {
		if digit < '0' || digit > '7' {
			return false
		}
	}
	return true
}

// validDiffObject accepts a full object identity or the absent-side zero identity.
func validDiffObject(value string, length int) bool {
	return len(value) == length && (githubpr.ValidCommit(value) || strings.Trim(value, "0") == "")
}

// quoteDiffPath reproduces Git's forced core.quotePath C-style pathname encoding.
func quoteDiffPath(name string) string {
	var encoded strings.Builder
	quote := false
	for _, b := range []byte(name) {
		switch b {
		case '\a':
			encoded.WriteString(`\a`)
			quote = true
		case '\b':
			encoded.WriteString(`\b`)
			quote = true
		case '\t':
			encoded.WriteString(`\t`)
			quote = true
		case '\n':
			encoded.WriteString(`\n`)
			quote = true
		case '\v':
			encoded.WriteString(`\v`)
			quote = true
		case '\f':
			encoded.WriteString(`\f`)
			quote = true
		case '\r':
			encoded.WriteString(`\r`)
			quote = true
		case '\\', '"':
			encoded.WriteByte('\\')
			encoded.WriteByte(b)
			quote = true
		default:
			if b < 32 || b >= 127 {
				fmt.Fprintf(&encoded, `\%03o`, b)
				quote = true
			} else {
				encoded.WriteByte(b)
			}
		}
	}
	if quote {
		return `"` + encoded.String() + `"`
	}
	return encoded.String()
}

// patchPath decodes an old/new patch label without interpreting it as a host path.
func patchPath(label string) (string, error) {
	if strings.HasPrefix(label, `"`) {
		return strconv.Unquote(label)
	}
	return strings.TrimSuffix(label, "\t"), nil
}

// parseFilePatch binds native patch headers and counted hunks to a raw manifest entry.
func parseFilePatch(file *DiffFile, lines []string) error {
	oldName, newName := file.OldPath, file.NewPath
	if oldName == "" {
		oldName = newName
	}
	if newName == "" {
		newName = oldName
	}
	if lines[0] != "diff --git "+quoteDiffPath("a/"+oldName)+" "+quoteDiffPath("b/"+newName) {
		return errors.New("patch and raw diff paths disagree")
	}
	oldLabel, newLabel := "a/"+file.OldPath, "b/"+file.NewPath
	if file.OldPath == "" {
		oldLabel = "/dev/null"
	}
	if file.NewPath == "" {
		newLabel = "/dev/null"
	}
	labels, binary := false, false
	metadata := make(map[string]bool)
	for cursor := 1; cursor < len(lines); {
		line := lines[cursor]
		switch {
		case strings.HasPrefix(line, "--- "):
			if labels || cursor+1 >= len(lines) || !strings.HasPrefix(lines[cursor+1], "+++ ") {
				return errors.New("incomplete or duplicate patch labels")
			}
			old, oldErr := patchPath(line[4:])
			new, newErr := patchPath(lines[cursor+1][4:])
			if oldErr != nil || newErr != nil || old != oldLabel || new != newLabel {
				return errors.New("patch labels disagree with pinned paths")
			}
			labels = true
			cursor += 2
		case strings.HasPrefix(line, "@@ "):
			if !labels || binary || len(file.Hunks) >= 10000 {
				return errors.New("invalid or excessive diff hunks")
			}
			hunk, consumed, err := parseHunk(lines[cursor:], file.ID, len(file.Hunks))
			if err != nil {
				return err
			}
			if n := len(file.Hunks); n > 0 {
				previous := file.Hunks[n-1]
				if hunk.Old.Start < previous.Old.Start+previous.Old.Count || hunk.New.Start < previous.New.Start+previous.New.Count {
					return errors.New("overlapping diff hunks")
				}
			}
			file.Hunks = append(file.Hunks, hunk)
			cursor += consumed
		case strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ"):
			if binary || labels || len(file.Hunks) != 0 {
				return errors.New("contradictory binary patch")
			}
			if line != "Binary files "+quoteDiffPath(oldLabel)+" and "+quoteDiffPath(newLabel)+" differ" {
				return errors.New("binary patch paths disagree")
			}
			binary = true
			cursor++
		case strings.HasPrefix(line, "index "), strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "new mode "), strings.HasPrefix(line, "new file mode "), strings.HasPrefix(line, "deleted file mode "), strings.HasPrefix(line, "similarity index "), strings.HasPrefix(line, "rename from "), strings.HasPrefix(line, "rename to "), strings.HasPrefix(line, "copy from "), strings.HasPrefix(line, "copy to "):
			if labels || binary {
				return errors.New("unexpected metadata after diff content")
			}
			if err := validatePatchMetadata(file, line, metadata); err != nil {
				return err
			}
			cursor++
		default:
			return errors.New("unrecognized or incomplete file patch")
		}
	}
	if file.Status == "added" && !metadata["new file mode "] || file.Status == "deleted" && !metadata["deleted file mode "] || file.Status == "renamed" && (!metadata["rename from "] || !metadata["rename to "] || !metadata["similarity index "]) || file.Status == "copied" && (!metadata["copy from "] || !metadata["copy to "] || !metadata["similarity index "]) {
		return errors.New("required file metadata is missing")
	}
	if file.OldPath != "" && file.NewPath != "" && file.OldMode != file.NewMode && (!metadata["old mode "] || !metadata["new mode "]) {
		return errors.New("mode change headers are missing")
	}
	if (binary || len(file.Hunks) > 0) && !metadata["index "] {
		return errors.New("content patch is missing its object header")
	}
	if !binary && len(file.Hunks) == 0 && file.OldObject != file.NewObject {
		empty := blobID(nil, len(file.NewObject))
		if !(file.Status == "added" && file.NewObject == empty || file.Status == "deleted" && file.OldObject == empty) {
			return errors.New("changed objects have no complete patch content")
		}
	}
	file.Kind = "text"
	switch {
	case file.OldMode == "160000" || file.NewMode == "160000":
		file.Kind = "submodule"
	case file.OldMode == "120000" || file.NewMode == "120000":
		file.Kind = "symlink"
	case file.OldMode != "000000" && file.OldMode != "100644" && file.OldMode != "100755" || file.NewMode != "000000" && file.NewMode != "100644" && file.NewMode != "100755":
		file.Kind = "unsupported"
		file.Reason = "Unsupported file mode has no unified text view."
	case binary:
		file.Kind = "binary"
	case !utf8.ValidString(strings.Join(lines, "\n")):
		file.Kind = "unsupported"
		file.Reason = "Non-UTF-8 text has no lossless text view."
	case len(file.Hunks) == 0:
		if file.OldMode != file.NewMode && file.Status == "modified" {
			file.Kind = "mode-only"
		} else {
			file.Kind = "metadata-only"
		}
	}
	if labels && len(file.Hunks) == 0 {
		return errors.New("text patch labels have no hunks")
	}
	if file.Kind != "text" {
		file.Hunks = []DiffHunk{}
	}
	return nil
}

// validatePatchMetadata binds patch identities and extended headers to the raw record.
func validatePatchMetadata(file *DiffFile, line string, seen map[string]bool) error {
	for _, prefix := range []string{"index ", "old mode ", "new mode ", "new file mode ", "deleted file mode ", "similarity index ", "rename from ", "rename to ", "copy from ", "copy to "} {
		value, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		if seen[prefix] {
			return errors.New("duplicate patch metadata")
		}
		seen[prefix] = true
		valid := false
		switch prefix {
		case "index ":
			want := file.OldObject + ".." + file.NewObject
			valid = value == want || file.OldMode == file.NewMode && value == want+" "+file.OldMode || file.OldPath == "" && value == want+" "+file.NewMode || file.NewPath == "" && value == want+" "+file.OldMode
		case "old mode ":
			valid = value == file.OldMode && file.OldPath != "" && file.NewPath != "" && file.OldMode != file.NewMode
		case "new mode ":
			valid = value == file.NewMode && file.OldPath != "" && file.NewPath != "" && file.OldMode != file.NewMode
		case "new file mode ":
			valid = file.Status == "added" && value == file.NewMode
		case "deleted file mode ":
			valid = file.Status == "deleted" && value == file.OldMode
		case "similarity index ":
			valid = (file.Status == "renamed" || file.Status == "copied") && value == strconv.Itoa(file.Similarity)+"%"
		case "rename from ", "copy from ":
			name, err := patchPath(value)
			valid = err == nil && name == file.OldPath && (prefix == "rename from " && file.Status == "renamed" || prefix == "copy from " && file.Status == "copied")
		case "rename to ", "copy to ":
			name, err := patchPath(value)
			valid = err == nil && name == file.NewPath && (prefix == "rename to " && file.Status == "renamed" || prefix == "copy to " && file.Status == "copied")
		}
		if !valid {
			return errors.New("patch metadata disagrees with pinned raw identities")
		}
		return nil
	}
	return errors.New("unknown patch metadata")
}
