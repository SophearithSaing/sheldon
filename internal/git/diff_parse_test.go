package git

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// parserFixture returns a complete native-format raw record and matching text patch.
func parserFixture() (githubpr.Revision, string) {
	pr, _ := githubpr.ParseURL("https://github.com/owner/repo/pull/1")
	old, new := strings.Repeat("a", 40), strings.Repeat("b", 40)
	revision := githubpr.Revision{PRID: pr.ID, BaseBranch: "main", BaseSource: githubpr.GitHubBase, BaseSHA: old, HeadSHA: new, MergeBaseSHA: old}
	output := ":100644 100644 " + old + " " + new + " M\x00file\x00\x00diff --git a/file b/file\nindex " + old + ".." + new + " 100644\n--- a/file\n+++ b/file\n@@ -1 +1 @@ section\n-old\n+new\n"
	return revision, output
}

func TestDiffParserRejectsMalformedAndIncompleteOutput(t *testing.T) {
	revision, valid := parserFixture()
	if _, err := parseDiff([]byte(valid), revision); err != nil {
		t.Fatal(err)
	}
	broken := map[string]string{
		"truncated":           strings.TrimSuffix(valid, "\n"),
		"raw header":          strings.Replace(valid, ":100644", ":bad", 1),
		"unknown status":      strings.Replace(valid, " M\x00", " X\x00", 1),
		"identity":            strings.Replace(valid, strings.Repeat("a", 40), "not-a-sha", 1),
		"missing separator":   strings.Replace(valid, "file\x00\x00", "file\x00", 1),
		"missing patch":       strings.Split(valid, "diff --git")[0],
		"wrong patch path":    strings.Replace(valid, "diff --git a/file", "diff --git a/foreign", 1),
		"wrong label":         strings.Replace(valid, "--- a/file", "--- a/foreign", 1),
		"wrong index":         strings.Replace(valid, "index "+strings.Repeat("a", 40), "index "+strings.Repeat("c", 40), 1),
		"missing lines":       strings.TrimSuffix(valid, "+new\n"),
		"too many lines":      valid + "+extra\n",
		"wrong count":         strings.Replace(valid, "@@ -1 +1 @@", "@@ -1,2 +1 @@", 1),
		"invalid range":       strings.Replace(valid, "@@ -1 +1 @@", "@@ -0 +1 @@", 1),
		"extra file":          valid + "diff --git a/extra b/extra\n",
		"orphan marker":       strings.Replace(valid, "-old\n", "\\ No newline at end of file\n-old\n", 1),
		"duplicate marker":    valid + "\\ No newline at end of file\n\\ No newline at end of file\n",
		"missing content":     strings.Split(valid, "--- a/file")[0],
		"unexpected metadata": valid + "old mode 100644\n",
		"non UTF8 path":       strings.Replace(valid, "file\x00", "invalid\xff\x00", 1),
	}
	for name, output := range broken {
		t.Run(name, func(t *testing.T) {
			got, err := parseDiff([]byte(output), revision)
			if err == nil || got.Complete || got.Files != nil {
				t.Fatal("malformed diff accepted or returned partially")
			}
		})
	}
}

func TestDiffParserLimits(t *testing.T) {
	revision, valid := parserFixture()
	if _, err := parseDiff([]byte(strings.Repeat("x", (4<<20)+1)), revision); err == nil {
		t.Fatal("output bound ignored")
	}
	var manifest strings.Builder
	for n := 0; n <= 1000; n++ {
		fmt.Fprintf(&manifest, ":100644 100644 %s %s M\x00file-%d\x00", revision.BaseSHA, revision.HeadSHA, n)
	}
	if _, err := parseDiff([]byte(manifest.String()), revision); err == nil || !strings.Contains(err.Error(), "1000") {
		t.Fatal("file bound ignored")
	}
	tooMany := strings.Split(valid, "diff --git")[0] + strings.Repeat("\n", 100001)
	if _, err := parseDiff([]byte(tooMany), revision); err == nil || !strings.Contains(err.Error(), "100000") {
		t.Fatal("patch line bound ignored")
	}
	for _, value := range []string{"-1", "1,-1", "1,100001", "1000000001", "999999999999999999999", "0,1", "1,2,3"} {
		if _, err := parseRange(value); err == nil {
			t.Errorf("invalid range accepted: %s", value)
		}
	}
}

func FuzzParseDiff(f *testing.F) {
	revision, valid := parserFixture()
	f.Add([]byte(valid))
	f.Add([]byte{})
	f.Add([]byte(":100644\x00"))
	f.Fuzz(func(t *testing.T, output []byte) {
		got, err := parseDiff(output, revision)
		if err != nil {
			if got.Complete || got.Files != nil {
				t.Fatal("partial result on failure")
			}
			return
		}
		if !got.Complete || got.Revision != revision {
			t.Fatal("unbound successful diff")
		}
		for _, file := range got.Files {
			if !utf8.ValidString(file.OldPath) || !utf8.ValidString(file.NewPath) {
				t.Fatal("lossy path")
			}
			for _, hunk := range file.Hunks {
				for _, line := range hunk.Lines {
					if !utf8.ValidString(line.Text) {
						t.Fatal("lossy text")
					}
				}
			}
		}
	})
}
