package main

import (
	"encoding/json"
	"strings"

	"github.com/SophearithSaing/sheldon/internal/git"
)

// semanticFixtureAnswer derives synthetic cross-file references from the supplied manifest only.
func semanticFixtureAnswer(prompt string) (string, bool) {
	var input struct {
		Instructions string   `json:"instructions"`
		Source       git.Diff `json:"source"`
	}
	if json.Unmarshal([]byte(prompt), &input) != nil || !strings.Contains(input.Instructions, "purpose-based groups") || !input.Source.Complete {
		return "", false
	}
	ranges := []map[string]any{}
	for _, file := range input.Source.Files {
		if file.Kind != "text" {
			continue
		}
		found := false
		for _, h := range file.Hunks {
			for _, l := range h.Lines {
				side, n := "new", l.NewLine
				if l.Sign == "-" {
					side, n = "old", l.OldLine
				} else if l.Sign != "+" {
					continue
				}
				ranges = append(ranges, map[string]any{"fileID": file.ID, "hunkID": h.ID, "side": side, "start": n, "end": n})
				found = true
				break
			}
			if found {
				break
			}
		}
		if len(ranges) == 2 {
			break
		}
	}
	groups := []map[string]any{}
	if len(ranges) > 0 {
		groups = append(groups, map[string]any{"title": "Synthetic related change", "explanation": "Synthetic organizing hypothesis, not live inference.", "ranges": ranges})
	}
	data, _ := json.Marshal(map[string]any{"groups": groups})
	return string(data), true
}
