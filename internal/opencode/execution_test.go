package opencode

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecuteVisibleOutputOnceAndHistoryReconciliation(t *testing.T) {
	for _, mode := range []string{"answer-fragmented", "answer-duplicate", "answer-pagination", "answer-ambiguous", "answer-feed-loss", "answer-failed", "answer-overflow"} {
		t.Run(mode, func(t *testing.T) {
			audit := fixture(t, mode)
			s, err := Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			session, err := s.CreateSession(context.Background(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			updates := []string{}
			result := s.Execute(ctx, session, "Synthetic fixture question.", make(chan struct{}), func(text string) { updates = append(updates, text) })
			if !result.Idle {
				t.Fatalf("fixture terminal outcome was not reconciled: %+v", result)
			}
			if mode == "answer-overflow" {
				if result.State != "failed" || !errors.Is(result.Err, ErrAnswerLimit) {
					t.Fatal("output limit became success")
				}
			} else if mode == "answer-failed" {
				if result.State != "failed" {
					t.Fatal("native failure became success")
				}
			} else if result.State != "completed" || result.Text != "Synthetic fixture answer <script>literal</script>." {
				t.Fatalf("wrong reconciled fixture answer: %+v", result)
			}
			for _, text := range append(updates, result.Text) {
				for _, secret := range []string{"FOREIGN", "HIDDEN", "RAW TOOL", "RAW PROVIDER"} {
					if strings.Contains(text, secret) {
						t.Fatal("unrelated/hidden native output leaked")
					}
				}
				if strings.Contains(text, "Synthetic fixture Synthetic") {
					t.Fatal("duplicate native event appended twice")
				}
			}
			prompts := 0
			for _, entry := range readAudit(t, audit) {
				if strings.HasSuffix(entry.Path, "/prompt") {
					prompts++
				}
			}
			if prompts != 1 {
				t.Fatal("native prompt was retried or not dispatched")
			}
		})
	}
}

func TestExecuteStopAndUnknownAcceptance(t *testing.T) {
	for _, mode := range []string{"answer-wait", "answer-ambiguous-empty", "answer-history-loop"} {
		t.Run(mode, func(t *testing.T) {
			audit := fixture(t, mode)
			lifetime, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s, err := Start(lifetime)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			session, err := s.CreateSession(lifetime, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			stop := make(chan struct{})
			stopped := false
			result := s.Execute(lifetime, session, "Synthetic stop/ambiguity fixture.", stop, func(string) {
				if !stopped && mode == "answer-wait" {
					close(stop)
					stopped = true
				}
			})
			if mode == "answer-wait" {
				if result.State != "interrupted" || !result.Idle || result.Text != "Synthetic fixture " {
					t.Fatalf("targeted Stop: %+v", result)
				}
			} else if result.Idle || result.State != "interrupted" {
				t.Fatal("uncertain admission/history was labeled idle")
			}
			prompts := 0
			for _, entry := range readAudit(t, audit) {
				if strings.HasSuffix(entry.Path, "/prompt") {
					prompts++
				}
			}
			if prompts != 1 {
				t.Fatal("uncertain delivery caused redispatch")
			}
		})
	}
}

func TestHistoryBoundaryRejectsOtherInputsAndKeepsTerminalSemantics(t *testing.T) {
	baseline := []historyMessage{{ID: "old_idle", Type: "idle", Outcome: "succeeded"}}
	valid := append(append([]historyMessage{}, baseline...), historyMessage{ID: "input", Type: "user", UserText: "prompt"}, historyMessage{ID: "assistant", Type: "assistant", Text: "visible"}, historyMessage{ID: "idle", Type: "idle", Outcome: "succeeded"})
	if result, terminal, err := executionResult(baseline, valid, "input", "prompt"); err != nil || !terminal || result.State != "completed" || result.Text != "visible" {
		t.Fatal("valid history boundary rejected")
	}
	for _, kind := range []string{"wrong input", "second input", "lost prefix", "missing idle", "wrong unknown text"} {
		messages := append([]historyMessage{}, valid...)
		inputID := "input"
		switch kind {
		case "wrong input":
			messages[1].ID = "other"
		case "second input":
			messages = append(messages[:3], historyMessage{ID: "other", Type: "user"}, messages[3])
		case "lost prefix":
			messages[0].ID = "replaced"
		case "missing idle":
			messages = messages[:3]
		case "wrong unknown text":
			inputID = ""
			messages[1].UserText = "other prompt"
		}
		_, terminal, _ := executionResult(baseline, messages, inputID, "prompt")
		if terminal {
			t.Fatalf("unrelated or incomplete history accepted: %s", kind)
		}
	}
}
