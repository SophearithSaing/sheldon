package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/process"
)

// wildcardMatch models the pinned permission endpoint's whole-command wildcard matching in offline tests.
func wildcardMatch(pattern, value string) bool {
	expression := regexp.QuoteMeta(pattern)
	expression = strings.ReplaceAll(strings.ReplaceAll(expression, `\*`, `.*`), `\?`, `.`)
	if strings.HasSuffix(pattern, " *") {
		expression = strings.TrimSuffix(expression, " .*") + "(?: .*)?"
	}
	matched, _ := regexp.MatchString("^"+expression+"$", value)
	return matched
}

// semanticGit runs actual local Git inspection with fixed argv, never provider inference or shell interpretation.
func semanticGit(directory string, args ...string) ([]byte, error) {
	executable := os.Getenv("SHELDON_GIT_REAL")
	if executable == "" {
		executable, _ = exec.LookPath("git")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	audit(map[string]any{"semanticGit": args, "directory": directory, "pid": os.Getpid()})
	result, err := process.Run(ctx, process.Command{Name: executable, Dir: directory, Args: args, OutputLimit: 4 << 20, GracePeriod: 100 * time.Millisecond})
	return result.Stdout, err
}

// fixtureRanges inspects real pinned Git for explicit synthetic generation prompts, not embedded source.
func fixtureRanges(prompt, directory, marker string) ([]map[string]any, bool) {
	var input struct {
		Instructions string            `json:"instructions"`
		Revision     githubpr.Revision `json:"revision"`
	}
	if json.Unmarshal([]byte(prompt), &input) != nil || !strings.Contains(input.Instructions, marker) {
		return nil, false
	}
	if input.Revision.Validate(input.Revision.PRID) != nil {
		return nil, true
	}
	base, head := input.Revision.MergeBaseSHA, input.Revision.HeadSHA
	manifest, err := semanticGit(directory, "--no-pager", "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", "--find-renames=50%", base, head, "--", ".")
	if err != nil {
		return nil, true
	}
	paths := strings.Split(strings.TrimSuffix(string(manifest), "\x00"), "\x00")
	ranges := []map[string]any{}
	header := regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	for len(paths) >= 2 {
		status, oldPath, newPath := paths[0], paths[1], paths[1]
		paths = paths[2:]
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if len(paths) == 0 {
				break
			}
			newPath, paths = paths[0], paths[1:]
		}
		patch, err := semanticGit(directory, "--no-pager", "diff", "--no-ext-diff", "--no-textconv", "--unified=3", "--find-renames=50%", base, head, "--", oldPath, newPath)
		if err != nil {
			return nil, true
		}
		old, next := 0, 0
		for _, line := range strings.Split(string(patch), "\n") {
			if match := header.FindStringSubmatch(line); match != nil {
				old, _ = strconv.Atoi(match[1])
				next, _ = strconv.Atoi(match[2])
				continue
			}
			if old == 0 && next == 0 || len(line) == 0 {
				continue
			}
			if line[0] == ' ' {
				old++
				next++
				continue
			}
			if line[0] == '-' {
				ranges = append(ranges, map[string]any{"path": oldPath, "side": "old", "start": old, "end": old})
				break
			}
			if line[0] == '+' {
				ranges = append(ranges, map[string]any{"path": newPath, "side": "new", "start": next, "end": next})
				break
			}
		}
		if len(ranges) == 2 {
			break
		}
	}
	return ranges, true
}

// semanticFixtureAnswer derives synthetic groups by inspecting real pinned Git, not an embedded manifest.
func semanticFixtureAnswer(prompt, directory string) (string, bool) {
	ranges, matched := fixtureRanges(prompt, directory, "purpose-based groups")
	if !matched {
		return "", false
	}
	groups := []map[string]any{}
	if len(ranges) > 0 {
		groups = append(groups, map[string]any{"title": "Synthetic related change", "explanation": "Synthetic organizing hypothesis, not live inference.", "ranges": ranges})
	}
	data, _ := json.Marshal(map[string]any{"groups": groups})
	return string(data), true
}
