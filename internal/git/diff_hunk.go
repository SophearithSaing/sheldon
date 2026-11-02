package git

import (
	"errors"
	"strconv"
	"strings"
)

// parseRange decodes a bounded Git range, whose omitted count means one line.
func parseRange(value string) (DiffRange, error) {
	start, count, hasCount := strings.Cut(value, ",")
	result := DiffRange{Count: 1}
	var err error
	result.Start, err = strconv.Atoi(start)
	if err != nil || result.Start < 0 || result.Start > 1000000000 {
		return DiffRange{}, errors.New("invalid diff range start")
	}
	if hasCount {
		result.Count, err = strconv.Atoi(count)
		if err != nil || result.Count < 0 || result.Count > 100000 {
			return DiffRange{}, errors.New("invalid or excessive diff range count")
		}
	}
	if result.Count > 0 && result.Start == 0 {
		return DiffRange{}, errors.New("nonempty diff range begins at zero")
	}
	return result, nil
}

// parseHunk preserves signs, literal text, both coordinates and no-final-LF markers.
func parseHunk(lines []string, fileID string, ordinal int) (DiffHunk, int, error) {
	header, section, ok := strings.Cut(lines[0], " @@")
	fields := strings.Fields(header)
	if !ok || len(fields) != 3 || fields[0] != "@@" || !strings.HasPrefix(fields[1], "-") || !strings.HasPrefix(fields[2], "+") {
		return DiffHunk{}, 0, errors.New("invalid unified hunk header")
	}
	old, err := parseRange(fields[1][1:])
	if err != nil {
		return DiffHunk{}, 0, err
	}
	new, err := parseRange(fields[2][1:])
	if err != nil {
		return DiffHunk{}, 0, err
	}
	if old.Count == 0 && new.Count == 0 {
		return DiffHunk{}, 0, errors.New("empty unified hunk")
	}
	hunk := DiffHunk{ID: diffID(fileID, "hunk", strconv.Itoa(ordinal), header), Old: old, New: new, Section: section, Lines: []DiffLine{}}
	hunk.Old.ID, hunk.New.ID = diffID(hunk.ID, "old-range"), diffID(hunk.ID, "new-range")
	oldUsed, newUsed, cursor := 0, 0, 1
	for cursor < len(lines) {
		line := lines[cursor]
		if line == `\ No newline at end of file` {
			if len(hunk.Lines) == 0 || hunk.Lines[len(hunk.Lines)-1].NoNewline {
				return DiffHunk{}, 0, errors.New("orphan or duplicate no-newline marker")
			}
			previous := hunk.Lines[len(hunk.Lines)-1]
			if previous.Sign != "+" && oldUsed != old.Count || previous.Sign != "-" && newUsed != new.Count {
				return DiffHunk{}, 0, errors.New("no-newline marker occurs before the end of its source range")
			}
			hunk.Lines[len(hunk.Lines)-1].NoNewline = true
			cursor++
			continue
		}
		if oldUsed == old.Count && newUsed == new.Count {
			break
		}
		if len(line) == 0 {
			return DiffHunk{}, 0, errors.New("missing diff line sign")
		}
		parsed := DiffLine{ID: diffID(hunk.ID, "line", strconv.Itoa(len(hunk.Lines))), Sign: line[:1], Text: line[1:]}
		switch line[0] {
		case ' ':
			parsed.OldLine, parsed.NewLine = old.Start+oldUsed, new.Start+newUsed
			oldUsed++
			newUsed++
		case '-':
			parsed.OldLine = old.Start + oldUsed
			oldUsed++
		case '+':
			parsed.NewLine = new.Start + newUsed
			newUsed++
		default:
			return DiffHunk{}, 0, errors.New("invalid diff line sign")
		}
		if oldUsed > old.Count || newUsed > new.Count {
			return DiffHunk{}, 0, errors.New("diff lines exceed their hunk ranges")
		}
		hunk.Lines = append(hunk.Lines, parsed)
		cursor++
	}
	if oldUsed != old.Count || newUsed != new.Count {
		return DiffHunk{}, 0, errors.New("diff hunk is incomplete")
	}
	return hunk, cursor, nil
}
