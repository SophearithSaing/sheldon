package question

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAdmissionReservesEscapedAnswerAndStatusHeadroom(t *testing.T) {
	s := savedFixture()
	a := &s.Threads[0].Answers[0]
	a.State = Pending
	a.Text = ""
	if err := s.admissionHeadroom(); err != nil {
		t.Fatal(err)
	}
	a.State = Interrupted
	a.ExecutionUnknown = true
	a.StopRequested = true
	a.FailureCode = "execution-uncertain"
	a.Text = strings.Repeat("\x01", maxAnswerBytes)
	if err := s.Validate(s.PRID); err != nil {
		t.Fatalf("reserved terminal snapshot does not fit: %v", err)
	}
	encoded, _ := json.Marshal(s)
	if len(encoded) > MaxSavedBytes {
		t.Fatal("escaped text exceeded reservation")
	}
}

func TestAdmissionRefusesInsufficientThreadHeadroom(t *testing.T) {
	s := savedFixture()
	first := s.Threads[0].Answers[0]
	s.Threads[0].Answers = nil
	for n := range 26 {
		a := first
		a.ID = fmt.Sprintf("%064x", n+10000)
		a.Text = strings.Repeat("x", maxAnswerBytes)
		s.Threads[0].Answers = append(s.Threads[0].Answers, a)
	}
	last := first
	last.State = Pending
	last.Text = ""
	s.Threads[0].Answers = append(s.Threads[0].Answers, last)
	if err := s.Validate(s.PRID); err != nil {
		t.Fatalf("invalid headroom fixture: %v", err)
	}
	if s.admissionHeadroom() == nil {
		t.Fatal("admission allowed unpersistable future answer")
	}
}
