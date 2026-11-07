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

// savedFixture supplies a complete private set without source/native/storage operations.
func savedFixture(t *testing.T) Saved {
	t.Helper()
	source, anchor := preparedSource(t)
	content, err := source.Parse(nativeJSON(t, nativeFixture(anchor)))
	if err != nil {
		t.Fatal(err)
	}
	for i := range content.Questions {
		hunk := source.diff.Files[0].Hunks[0]
		hunk.Lines = append([]git.DiffLine(nil), hunk.Lines...)
		content.Questions[i].Evidence[0].Surrounding = &hunk
	}
	return Saved{SchemaVersion: 1, PRID: content.Revision.PRID, Version: 1, QuizID: strings.Repeat("a", 64), Content: content}
}

func TestSavedStrictRoundTripAndPrivateFormatting(t *testing.T) {
	t.Setenv("PATH", "")
	saved := savedFixture(t)
	data, err := saved.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSaved(data, saved.PRID)
	if err != nil || !reflect.DeepEqual(got, saved) || data[len(data)-1] != '\n' {
		t.Fatalf("saved round trip: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", got, got), "literal") {
		t.Fatal("diagnostic exposed saved text/key")
	}
	got.Questions[0].Evidence[0].Selected[0].Text = "edit"
	if saved.Questions[0].Evidence[0].Selected[0].Text == "edit" {
		t.Fatal("decoded quotes alias input")
	}
	if _, err := DecodeSaved(data, strings.Repeat("b", 64)); !errors.Is(err, ErrStore) {
		t.Fatal("cross-PR saved data accepted")
	}
}

func TestCompactAndLegacyEvidenceReopenAndGradeWithoutSource(t *testing.T) {
	t.Setenv("PATH", "")
	for _, compact := range []bool{false, true} {
		s := savedFixture(t)
		if compact {
			for i := range s.Questions {
				s.Questions[i].Evidence[0].Surrounding = nil
			}
		}
		data, err := s.Encode()
		if err != nil || bytes.Contains(data, []byte(`"surrounding"`)) == compact {
			t.Fatalf("evidence encoding: %v", err)
		}
		reopened, err := DecodeSaved(data, s.PRID)
		if err != nil {
			t.Fatal(err)
		}
		view, err := reopened.Public()
		if err != nil {
			t.Fatal(err)
		}
		public, _ := json.Marshal(view)
		for _, private := range []string{"correctChoiceID", "explanation", "evidence", "surrounding", "selected"} {
			if bytes.Contains(public, []byte(private)) {
				t.Fatalf("unchecked view exposed %s", private)
			}
		}
		for _, choice := range []string{"a", "b"} {
			checked, err := reopened.Check(s.QuizID, "q1", s.RevisionID, choice)
			if err != nil || checked.Validate(s.PRID) != nil || checked.Correct != (choice == s.Questions[0].CorrectChoiceID) || !reflect.DeepEqual(checked.Evidence[0].Selected, s.Questions[0].Evidence[0].Selected) {
				t.Fatalf("grading changed: %v", err)
			}
			checked.Evidence[0].Selected[0].Text = "caller edit"
			if checked.Evidence[0].Surrounding != nil {
				checked.Evidence[0].Surrounding.Section = "caller edit"
			}
			if s.Questions[0].Evidence[0].Selected[0].Text == "caller edit" || reopened.Questions[0].Evidence[0].Surrounding != nil && reopened.Questions[0].Evidence[0].Surrounding.Section == "caller edit" {
				t.Fatal("checked evidence aliases saved state")
			}
		}
	}
}

func TestCompactEvidenceRejectsInvalidCoordinatesAndConflictingQuotes(t *testing.T) {
	for _, name := range []string{"missing", "duplicate ID", "gap", "wrong sign", "opposite coordinate", "nonmonotonic context", "text", "ID conflict", "coordinate conflict", "legacy conflict", "oversized"} {
		t.Run(name, func(t *testing.T) {
			s := savedFixture(t)
			for i := range s.Questions {
				s.Questions[i].Evidence[0].Surrounding = nil
			}
			e := &s.Questions[0].Evidence[0]
			switch name {
			case "missing":
				e.Selected = nil
			case "duplicate ID":
				e.Anchor.End++
				e.Selected = append(e.Selected, e.Selected[0])
				e.Selected[1].NewLine++
			case "gap":
				e.Selected[0].NewLine++
			case "wrong sign":
				e.Selected[0].Sign = "-"
			case "opposite coordinate":
				e.Selected[0].OldLine = 5
			case "nonmonotonic context":
				e.Anchor.Start = 1
				e.Anchor.End = 3
				e.Selected = []git.DiffLine{{ID: strings.Repeat("b", 64), Sign: " ", NewLine: 1, OldLine: 3, Text: "context"}, e.Selected[0], {ID: strings.Repeat("c", 64), Sign: " ", NewLine: 3, OldLine: 1, Text: "context"}}
			case "text":
				e.Selected[0].Text = "bad\x00"
			case "ID conflict":
				e.Selected[0].Text += "contradiction"
			case "coordinate conflict":
				e.Selected[0].ID = strings.Repeat("f", 64)
			case "legacy conflict":
				legacy := savedFixture(t)
				s.Questions[1].Evidence[0] = legacy.Questions[1].Evidence[0]
				e.Selected[0].Text += "contradiction"
			case "oversized":
				e.Selected[0].Text = strings.Repeat("x", maxEvidenceBytes)
			}
			if _, err := s.Encode(); !errors.Is(err, ErrStore) {
				t.Fatal("invalid compact evidence accepted")
			}
		})
	}
}

func TestSavedRejectsInvalidGraphs(t *testing.T) {
	for _, name := range []string{"schema", "zero version", "PR", "quiz ID", "revision", "digest", "count", "question ID", "duplicate prompt", "choice ID", "duplicate choice", "key", "explanation", "evidence count", "duplicate evidence", "selection", "coordinates", "line ID", "hunk count", "non-text side", "text UTF-8", "quote NUL", "line ceiling", "bytes", "inconsistent hunk", "inconsistent paths"} {
		t.Run(name, func(t *testing.T) {
			s := savedFixture(t)
			item := &s.Questions[0]
			e := &item.Evidence[0]
			switch name {
			case "schema":
				s.SchemaVersion++
			case "zero version":
				s.Version = 0
			case "PR":
				s.PRID = strings.Repeat("b", 64)
			case "quiz ID":
				s.QuizID = "../foreign"
			case "revision":
				s.Revision.HeadSHA = "invalid"
			case "digest":
				s.RevisionID = strings.Repeat("b", 64)
			case "count":
				s.Questions = s.Questions[:4]
			case "question ID":
				item.ID = "q2"
			case "duplicate prompt":
				item.Prompt = strings.ToUpper(s.Questions[1].Prompt)
			case "choice ID":
				item.Choices[0].ID = "d"
			case "duplicate choice":
				item.Choices[0].Text = " Second "
			case "key":
				item.CorrectChoiceID = "ab"
			case "explanation":
				item.Explanation = " "
			case "evidence count":
				item.Evidence = nil
			case "duplicate evidence":
				item.Evidence = append(item.Evidence, *e)
			case "selection":
				e.Selected[0].Text = "invented quote"
			case "coordinates":
				e.Anchor.End++
			case "line ID":
				e.Surrounding.Lines[0].ID = "not a digest"
			case "hunk count":
				e.Surrounding.Old.Count++
			case "non-text side":
				e.Anchor.Side = "binary"
			case "text UTF-8":
				item.Prompt = "bad\xff"
			case "quote NUL":
				e.Surrounding.Lines[0].Text = "bad\x00"
			case "line ceiling":
				e.Anchor.End = e.Anchor.Start + 200
			case "bytes":
				e.Surrounding.Lines[0].Text = strings.Repeat("x", maxEvidenceBytes)
			case "inconsistent hunk":
				e.Surrounding.Section += "different"
			case "inconsistent paths":
				e.NewPath += "different"
			}
			if data, err := s.Encode(); !errors.Is(err, ErrStore) || data != nil {
				t.Fatal("invalid saved graph accepted")
			}
		})
	}
}

func TestSavedRejectsAmbiguousCorruptAndOverBudgetJSON(t *testing.T) {
	s := savedFixture(t)
	good, _ := s.Encode()
	for name, data := range map[string][]byte{
		"broken": []byte(`{`), "trailing": append(bytes.Clone(good), '{'), "unknown": bytes.Replace(good, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"selectedAnswer":"b"`), 1),
		"duplicate":         bytes.Replace(good, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":1,"schemaVersion":1`), 1),
		"alias":             bytes.Replace(good, []byte(`"quizID":`), []byte(`"QuizID":`), 1),
		"escaped duplicate": bytes.Replace(good, []byte(`"version":1`), []byte(`"version":1,"\u0076ersion":1`), 1),
		"surrogate":         bytes.Replace(good, []byte(`"First"`), []byte(`"\ud800"`), 1),
		"nesting":           []byte(`{"questions":` + strings.Repeat(`[`, 17) + `0` + strings.Repeat(`]`, 17) + `}`),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := DecodeSaved(data, s.PRID); !errors.Is(err, ErrStore) || !reflect.DeepEqual(got, Saved{}) {
				t.Fatal("corrupt saved data became healthy history")
			}
		})
	}
	data := append(bytes.Clone(good), bytes.Repeat([]byte(" "), MaxContentBytes-len(good))...)
	if _, err := DecodeSaved(data, s.PRID); err != nil {
		t.Fatal("exact read budget refused")
	}
	if _, err := DecodeSaved(append(data, ' '), s.PRID); !errors.Is(err, ErrStore) {
		t.Fatal("read overflow accepted")
	}
}

func TestSavedExactEncodedBudgetIncludesEnvelopeAndNewline(t *testing.T) {
	s := savedFixture(t)
	lines := make([]git.DiffLine, 8000)
	for i := range lines {
		lines[i] = git.DiffLine{ID: fmt.Sprintf("%064x", i+1), Sign: "+", NewLine: i + 1}
	}
	setCount := func(n int) {
		for i := range s.Questions {
			e := &s.Questions[i].Evidence[0]
			e.Anchor.Start, e.Anchor.End = 1, 1
			e.Selected = []git.DiffLine{lines[0]}
			e.Surrounding.Section = ""
			e.Surrounding.Old.Start, e.Surrounding.Old.Count = 0, 0
			e.Surrounding.New.Start, e.Surrounding.New.Count = 1, n
			e.Surrounding.Lines = lines[:n]
		}
	}
	low, high := 1, len(lines)
	for low < high {
		mid := (low + high + 1) / 2
		setCount(mid)
		data, _ := json.Marshal(s)
		if len(data)+1 <= MaxContentBytes {
			low = mid
		} else {
			high = mid - 1
		}
	}
	setCount(low)
	data, _ := json.Marshal(s)
	s.Questions[0].Explanation += strings.Repeat("x", MaxContentBytes-len(data)-1)
	data, err := s.Encode()
	if err != nil || len(data) != MaxContentBytes {
		t.Fatalf("exact encoded budget: %v", err)
	}
	s.Questions[0].Explanation += "x"
	if _, err := s.Encode(); !errors.Is(err, ErrStore) {
		t.Fatal("encoded envelope overflow accepted")
	}
}
