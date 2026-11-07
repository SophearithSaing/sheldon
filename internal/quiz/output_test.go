package quiz

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/git"
)

// nativeFixture supplies five distinct questions without trusting model-generated identities.
func nativeFixture(anchor Anchor) []nativeQuestion {
	questions := make([]nativeQuestion, 5)
	for i := range questions {
		path := "new 界.txt"
		if anchor.Side == "old" {
			path = "old name.txt"
		}
		questions[i] = nativeQuestion{Prompt: fmt.Sprintf("Question %d <literal>界?", i+1), Choices: []string{"First", "Second", "Third", "Fourth"}, CorrectChoiceID: string(rune('a' + i%4)), Explanation: "A concise explanation <not markup>.", Evidence: []nativeEvidence{{path, anchor.Side, anchor.Start, anchor.End}}}
	}
	return questions
}

// nativeJSON encodes synthetic native output without involving a harness.
func nativeJSON(t *testing.T, questions []nativeQuestion) []byte {
	t.Helper()
	data, err := json.Marshal(struct {
		Questions []nativeQuestion `json:"questions"`
	}{questions})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseDerivesLiteralEvidenceAndScopedIDsWithoutAliasing(t *testing.T) {
	t.Setenv("PATH", "")
	source, anchor := preparedSource(t)
	questions := nativeFixture(anchor)
	questions[0].Prompt = " \tLiteral <script> 界\r\n"
	questions[0].Choices[0] = "  Keep whitespace  "
	questions[1].Evidence[0].Side = "old"
	questions[1].Evidence[0].Path = "old name.txt"
	content, err := source.Parse(nativeJSON(t, questions))
	if err != nil || len(content.Questions) != 5 || content.Revision != source.diff.Revision || content.RevisionID != source.diff.RevisionID {
		t.Fatalf("content: %v", err)
	}
	for i, question := range content.Questions {
		if question.ID != fmt.Sprintf("q%d", i+1) || question.CorrectChoiceID != questions[i].CorrectChoiceID || question.Prompt != questions[i].Prompt || question.Explanation != questions[i].Explanation {
			t.Fatal("native text or backend-assigned identity changed")
		}
		for j, choice := range question.Choices {
			if choice.ID != string(rune('a'+j)) || choice.Text != questions[i].Choices[j] {
				t.Fatal("choice text/order changed")
			}
		}
		evidence := question.Evidence[0]
		want := source.diff.Files[0].Hunks[0].Lines[2]
		if i == 1 {
			want = source.diff.Files[0].Hunks[0].Lines[1]
		}
		if len(evidence.Selected) != 1 || evidence.Selected[0] != want || evidence.OldPath != "old name.txt" || evidence.NewPath != "new 界.txt" || evidence.Surrounding != nil {
			t.Fatal("evidence not derived from exact original source")
		}
	}
	encoded, _ := json.Marshal(content)
	if bytes.Contains(encoded, []byte(source.directory)) || strings.Contains(fmt.Sprintf("%+v %#v", content, content), "Literal") {
		t.Fatal("private location or diagnostic content exposed")
	}
	content.Questions[0].Evidence[0].Selected[0].Text = "edited"
	again, err := source.Parse(nativeJSON(t, questions))
	if err != nil || again.Questions[0].Evidence[0].Selected[0].Text == "edited" || content.Questions[2].Evidence[0].Selected[0].Text == "edited" {
		t.Fatal("returned evidence aliases source or sibling questions")
	}
}

func TestParseRejectsMalformedAmbiguousAndUnsupportedJSON(t *testing.T) {
	source, anchor := preparedSource(t)
	good := nativeJSON(t, nativeFixture(anchor))
	for name, data := range map[string][]byte{
		"empty":             nil,
		"truncated":         good[:len(good)-1],
		"trailing":          append(bytes.Clone(good), []byte(` {}`)...),
		"fenced":            append([]byte("```json\n"), good...),
		"array":             []byte(`[]`),
		"null":              []byte(`null`),
		"extra":             bytes.Replace(good, []byte(`{"questions":`), []byte(`{"sessionID":"secret","questions":`), 1),
		"duplicate":         bytes.Replace(good, []byte(`{"questions":`), []byte(`{"questions":[],"questions":`), 1),
		"escaped alias":     bytes.Replace(good, []byte(`{"questions":`), []byte(`{"\u0071uestions":[],"questions":`), 1),
		"case alias":        bytes.Replace(good, []byte(`"prompt":`), []byte(`"Prompt":`), 1),
		"key duplicate":     bytes.Replace(good, []byte(`"correctChoiceID":"a"`), []byte(`"correctChoiceID":"a","correctChoiceID":"b"`), 1),
		"key array":         bytes.Replace(good, []byte(`"correctChoiceID":"a"`), []byte(`"correctChoiceID":["a","b"]`), 1),
		"fraction":          bytes.Replace(good, []byte(`"start":2`), []byte(`"start":2.0`), 1),
		"invalid UTF-8":     append(bytes.Clone(good[:len(good)-1]), 0xff, '}'),
		"high surrogate":    bytes.Replace(good, []byte(`"First"`), []byte(`"\ud800"`), 1),
		"low surrogate":     bytes.Replace(good, []byte(`"First"`), []byte(`"\udc00"`), 1),
		"deep":              []byte(`{"questions":` + strings.Repeat(`[`, 17) + `0` + strings.Repeat(`]`, 17) + `}`),
		"native IDs":        bytes.Replace(good, []byte(`"side":"new"`), []byte(`"fileID":"invented","side":"new"`), 1),
		"revision override": bytes.Replace(good, []byte(`{"questions":`), []byte(`{"revisionID":"foreign","questions":`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := source.Parse(data)
			if !errors.Is(err, ErrOutput) || !reflect.DeepEqual(got, Content{}) {
				t.Fatalf("invalid JSON accepted/partial content returned: %v", err)
			}
		})
	}
	for _, replacement := range []string{`"\ud83d\ude00"`, `"\uFFFD"`, `"literal \\ud800"`} {
		if _, err := source.Parse(bytes.Replace(good, []byte(`"First"`), []byte(replacement), 1)); err != nil {
			t.Fatalf("valid Unicode/escaped literal rejected: %v", err)
		}
	}
}

func TestParseRejectsInvalidQuestionsAndReferences(t *testing.T) {
	for _, name := range []string{"four questions", "six questions", "empty prompt", "duplicate prompt", "empty choice", "three choices", "five choices", "duplicate choices", "casefold choices", "no key", "invalid key", "empty explanation", "NUL", "no evidence", "three evidence", "duplicate evidence", "foreign file", "wrong rename path", "retargeted path", "absolute path", "path alias", "context only", "cross hunk", "absent", "wrong side", "non-text", "zero", "reverse", "coordinate overflow", "line overflow"} {
		t.Run(name, func(t *testing.T) {
			source, anchor := preparedSource(t)
			questions := nativeFixture(anchor)
			item := &questions[0]
			switch name {
			case "four questions":
				questions = questions[:4]
			case "six questions":
				questions = append(questions, questions[0])
			case "empty prompt":
				item.Prompt = "\u2003 \t"
			case "duplicate prompt":
				item.Prompt = "\u2003" + strings.ToUpper(questions[1].Prompt) + "\t"
			case "empty choice":
				item.Choices[0] = " "
			case "three choices":
				item.Choices = item.Choices[:3]
			case "five choices":
				item.Choices = append(item.Choices, "Fifth")
			case "duplicate choices":
				item.Choices[0] = "  Second\t"
			case "casefold choices":
				item.Choices[0], item.Choices[1] = "K", "K"
			case "no key":
				item.CorrectChoiceID = ""
			case "invalid key":
				item.CorrectChoiceID = "e"
			case "empty explanation":
				item.Explanation = "\n"
			case "NUL":
				item.Explanation = "literal\x00secret"
			case "no evidence":
				item.Evidence = nil
			case "three evidence":
				item.Evidence = []nativeEvidence{item.Evidence[0], item.Evidence[0], item.Evidence[0]}
			case "duplicate evidence":
				item.Evidence = []nativeEvidence{item.Evidence[0], item.Evidence[0]}
			case "foreign file":
				item.Evidence[0].Path = "../foreign.txt"
			case "wrong rename path":
				item.Evidence[0].Path = "old name.txt"
			case "retargeted path":
				source.diff.Revision.BaseSHA = strings.Repeat("d", 40)
				source.diff.RevisionID, _ = source.diff.Revision.ID()
				source.diff.Files[0].NewPath = "foreign revision.txt"
			case "absolute path":
				item.Evidence[0].Path = "/new 界.txt"
			case "path alias":
				item.Evidence[0].Path = "./new 界.txt"
			case "context only":
				item.Evidence[0].Start, item.Evidence[0].End = 1, 1
			case "cross hunk":
				item.Evidence[0].End = 20
			case "absent":
				item.Evidence[0].Start, item.Evidence[0].End = 20, 20
			case "wrong side":
				item.Evidence[0].Side = "head"
			case "non-text":
				source.diff.Files[0].Kind = "binary"
			case "zero":
				item.Evidence[0].Start = 0
			case "reverse":
				item.Evidence[0].End = 1
			case "coordinate overflow":
				item.Evidence[0].End = 1_000_100_001
			case "line overflow":
				item.Evidence[0].End = item.Evidence[0].Start + maxEvidenceLines
			}
			got, err := source.Parse(nativeJSON(t, questions))
			if err == nil || !reflect.DeepEqual(got, Content{}) {
				t.Fatal("invalid output accepted or partial content returned")
			}
		})
	}
}

func TestParseTextLimitsAndOutputByteBudget(t *testing.T) {
	for _, name := range []string{"prompt", "choice", "explanation"} {
		t.Run(name, func(t *testing.T) {
			source, anchor := preparedSource(t)
			questions := nativeFixture(anchor)
			text, limit := &questions[0].Prompt, maxPromptText
			if name == "choice" {
				text, limit = &questions[0].Choices[0], maxChoiceText
			} else if name == "explanation" {
				text, limit = &questions[0].Explanation, maxExplanation
			}
			*text = strings.Repeat("界", limit/3) + strings.Repeat("x", limit%3)
			if _, err := source.Parse(nativeJSON(t, questions)); err != nil {
				t.Fatalf("exact UTF-8 field budget rejected: %v", err)
			}
			*text += "x"
			if _, err := source.Parse(nativeJSON(t, questions)); !errors.Is(err, ErrOutput) {
				t.Fatalf("field overflow accepted: %v", err)
			}
		})
	}
	source, anchor := preparedSource(t)
	data := nativeJSON(t, nativeFixture(anchor))
	data = append(data, bytes.Repeat([]byte(" "), MaxOutputBytes-len(data))...)
	if _, err := source.Parse(data); err != nil {
		t.Fatalf("exact native output budget rejected: %v", err)
	}
	if got, err := source.Parse(append(data, ' ')); !errors.Is(err, ErrLimit) || len(got.Questions) != 0 {
		t.Fatal("output overflow accepted/truncated")
	}
	for _, source := range []*Source{nil, {}} {
		if _, err := source.Parse(data); !errors.Is(err, ErrSource) {
			t.Fatal("unresolved source accepted")
		}
	}
}

// evidenceBudgetSource supplies two independent eligible excerpts with exact byte accounting.
func evidenceBudgetSource(t *testing.T) (*Source, Anchor, Anchor) {
	t.Helper()
	prepared, diff, anchor := sourceFixture(t)
	file := &diff.Files[0]
	file.OldPath, file.NewPath = "a", "a"
	hunk := &file.Hunks[0]
	hunk.Section = ""
	hunk.Old.Start, hunk.Old.Count = 0, 0
	hunk.New.Start, hunk.New.Count = 1, 1
	hunk.Lines = []git.DiffLine{{ID: strings.Repeat("a", 64), Sign: "+", Text: strings.Repeat("x", maxEvidenceBytes-3), NewLine: 1}}
	anchor.Start, anchor.End = 1, 1
	second := *file
	second.OldPath, second.NewPath = "b", "b"
	second.ID = strings.Repeat("b", 64)
	second.Hunks = []git.DiffHunk{*hunk}
	second.Hunks[0].ID = strings.Repeat("c", 64)
	diff.Files = []git.DiffFile{*file, second}
	source, err := sourceContext(prepared, diff, fixturePRURL)
	if err != nil {
		t.Fatal(err)
	}
	other := anchor
	other.FileID, other.HunkID = second.ID, second.Hunks[0].ID
	return source, anchor, other
}

func TestEvidenceExactAndOverflowByteAndAggregateLimits(t *testing.T) {
	source, anchor, other := evidenceBudgetSource(t)
	if _, size, err := source.evidence(anchor); err != nil || size != maxEvidenceBytes {
		t.Fatalf("exact evidence bytes: %d %v", size, err)
	}
	questions := nativeFixture(anchor)
	for i := range questions {
		questions[i].Evidence[0].Path = "a"
	}
	for i := range 3 {
		questions[i].Evidence = append(questions[i].Evidence, nativeEvidence{"b", other.Side, other.Start, other.End})
	}
	if _, err := source.Parse(nativeJSON(t, questions)); err != nil {
		t.Fatalf("exact 64 KiB aggregate evidence budget refused: %v", err)
	}
	questions[3].Evidence = append(questions[3].Evidence, nativeEvidence{"b", other.Side, other.Start, other.End})
	if got, err := source.Parse(nativeJSON(t, questions)); !errors.Is(err, ErrLimit) || len(got.Questions) != 0 {
		t.Fatal("aggregate evidence overflow accepted")
	}
	source.diff.Files[0].Hunks[0].Lines[0].Text += "x"
	if _, _, err := source.evidence(anchor); !errors.Is(err, ErrLimit) {
		t.Fatal("selected evidence overflow accepted")
	}
	source, anchor, _ = evidenceBudgetSource(t)
	source.diff.Files[0].Hunks[0].Lines = append(source.diff.Files[0].Hunks[0].Lines, git.DiffLine{Sign: " ", Text: strings.Repeat("界", maxEvidenceBytes), NewLine: 2, OldLine: 1})
	if evidence, size, err := source.evidence(anchor); err != nil || size != maxEvidenceBytes || evidence.Surrounding != nil || len(evidence.Selected) != 1 {
		t.Fatal("unselected hunk context inflated or entered evidence")
	}
}

func TestEvidenceExactLineAndCoordinateCeilings(t *testing.T) {
	source, anchor := preparedSource(t)
	hunk := &source.diff.Files[0].Hunks[0]
	hunk.Lines = make([]git.DiffLine, maxEvidenceLines)
	for i := range hunk.Lines {
		hunk.Lines[i] = git.DiffLine{Sign: "+", NewLine: i + 1, Text: "x"}
	}
	anchor.Start, anchor.End = 1, maxEvidenceLines
	if evidence, _, err := source.evidence(anchor); err != nil || len(evidence.Selected) != maxEvidenceLines {
		t.Fatalf("exact selected line ceiling rejected: %v", err)
	}
	anchor.Start, anchor.End = 1_000_100_000, 1_000_100_000
	hunk.Lines = []git.DiffLine{{Sign: "+", NewLine: anchor.Start, Text: "end"}}
	if _, _, err := source.evidence(anchor); err != nil {
		t.Fatalf("exact coordinate ceiling rejected: %v", err)
	}
}

func TestEvidenceRejectsCrossHunkAndEveryNonTextKind(t *testing.T) {
	for _, kind := range []string{"binary", "submodule", "symlink", "type-change", "mode-only", "metadata-only", "unsupported"} {
		source, anchor := preparedSource(t)
		source.diff.Files[0].Kind = kind
		if _, _, err := source.evidence(anchor); !errors.Is(err, ErrOutput) {
			t.Fatalf("%s admitted as text evidence", kind)
		}
	}
	source, anchor := preparedSource(t)
	second := source.diff.Files[0].Hunks[0]
	second.ID = strings.Repeat("e", 64)
	second.Lines = []git.DiffLine{{Sign: "+", NewLine: 20, Text: "other hunk"}}
	source.diff.Files[0].Hunks = append(source.diff.Files[0].Hunks, second)
	anchor.End = 20
	if _, _, err := source.evidence(anchor); !errors.Is(err, ErrOutput) {
		t.Fatal("selection crossed the original hunk boundary")
	}
	anchor.HunkID, anchor.Start = second.ID, 20
	if evidence, _, err := source.evidence(anchor); err != nil || evidence.Selected[0].Text != "other hunk" {
		t.Fatalf("independent hunk could not be selected: %v", err)
	}
}

func TestParseRetainsDistinctSelectionsFromLargeHunkWithoutContextMetadata(t *testing.T) {
	prepared, diff, anchor := sourceFixture(t)
	hunk := &diff.Files[0].Hunks[0]
	hunk.Section = ""
	hunk.Old.Start, hunk.Old.Count = 0, 0
	hunk.New.Start, hunk.New.Count = 1, 2000
	hunk.Lines = make([]git.DiffLine, 2000)
	for i := range hunk.Lines {
		hunk.Lines[i] = git.DiffLine{ID: fmt.Sprintf("%064x", i+1), Sign: "+", NewLine: i + 1}
	}
	source, err := sourceContext(prepared, diff, fixturePRURL)
	if err != nil {
		t.Fatal(err)
	}
	anchor.Start, anchor.End = 1, 1
	second := anchor
	second.Start, second.End = 2, 2
	questions := nativeFixture(anchor)
	for i := range questions {
		questions[i].Evidence = append(questions[i].Evidence, nativeEvidence{"new 界.txt", second.Side, second.Start, second.End})
	}
	if _, size, err := source.evidence(anchor); err != nil || size*10 > maxEvidenceTotal {
		t.Fatalf("raw evidence should fit: %d %v", size, err)
	}
	content, err := source.Parse(nativeJSON(t, questions))
	if err != nil || len(content.Questions) != 5 {
		t.Fatalf("large hunk prevented small selected evidence: %v", err)
	}
	for _, question := range content.Questions {
		if len(question.Evidence) != 2 {
			t.Fatal("lost independent evidence ranges")
		}
		for _, evidence := range question.Evidence {
			if len(evidence.Selected) != 1 || evidence.Surrounding != nil {
				t.Fatal("unselected hunk lines were retained")
			}
		}
	}
	saved := Saved{SchemaVersion: 1, PRID: diff.Revision.PRID, Version: 1, QuizID: strings.Repeat("e", 64), Content: content}
	data, err := saved.Encode()
	if err != nil || len(data) > 32<<10 || bytes.Contains(data, []byte(`"surrounding"`)) {
		t.Fatalf("compact result: %v", err)
	}
	if _, err := DecodeSaved(data, saved.PRID); err != nil {
		t.Fatal(err)
	}
}

func FuzzParseNativeOutput(f *testing.F) {
	f.Add([]byte(`{"questions":[]}`))
	f.Add([]byte(`{"questions":[{"prompt":"\ud800"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		source, _ := preparedSource(t)
		content, err := source.Parse(data)
		if err != nil && len(content.Questions) != 0 {
			t.Fatal("failed parse returned partial content")
		}
	})
}
