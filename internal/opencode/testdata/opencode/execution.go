package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

type executionFixture struct {
	mu      sync.Mutex
	mode    string
	done    <-chan struct{}
	events  chan string
	history map[string][]map[string]any
	active  map[string]chan struct{}
	next    int
}

// newExecutionFixture owns only synthetic native activity in the disposable executable.
func newExecutionFixture(mode string, done <-chan struct{}) *executionFixture {
	return &executionFixture{mode: mode, done: done, events: make(chan string, 256), history: make(map[string][]map[string]any), active: make(map[string]chan struct{})}
}

// event enqueues synthetic envelopes without writing hidden output into public answer fields.
func (f *executionFixture) event(id, kind, session, assistant, delta string) {
	b, _ := json.Marshal(map[string]any{"id": id, "type": kind, "data": map[string]any{"sessionID": session, "assistantMessageID": assistant, "delta": delta}})
	select {
	case f.events <- string(b):
	case <-f.done:
	}
}

// handle implements only the pinned prompt/history/interrupt fixture routes.
func (f *executionFixture) handle(w http.ResponseWriter, r *http.Request, body []byte) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/session/"), "/")
	if len(parts) != 2 || !strings.HasPrefix(r.URL.Path, "/api/session/") {
		return false
	}
	session, action := parts[0], parts[1]
	switch action {
	case "inbox":
		fmt.Fprint(w, `{"data":[]}`)
		return true
	case "message":
		f.mu.Lock()
		defer f.mu.Unlock()
		messages := f.history[session]
		if messages == nil {
			messages = []map[string]any{}
		}
		var cursor *string
		if f.mode == "answer-pagination" && len(messages) > 0 {
			next := "boundary"
			cursor = &next
			if r.URL.Query().Get("cursor") == "boundary" {
				messages = []map[string]any{}
				cursor = nil
			}
		}
		if f.mode == "answer-history-loop" && len(messages) > 0 {
			next := "loop"
			cursor = &next
		}
		json.NewEncoder(w).Encode(map[string]any{"data": messages, "cursor": map[string]any{"next": cursor}})
		return true
	case "interrupt":
		f.mu.Lock()
		stop := f.active[session]
		if stop != nil {
			close(stop)
			delete(f.active, session)
		}
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]bool{"interrupted": stop != nil})
		return true
	case "prompt":
		var input struct {
			Text   string `json:"text"`
			Resume bool   `json:"resume"`
		}
		if json.Unmarshal(body, &input) != nil || !input.Resume || input.Text == "" {
			w.WriteHeader(400)
			return true
		}
		if f.mode == "answer-admission-proof" && !savedAdmissionMatches(input.Text, session) {
			w.WriteHeader(409)
			return true
		}
		f.mu.Lock()
		f.next++
		n := f.next
		user, assistant := "msg_input_"+strconv.Itoa(n), "msg_assistant_"+strconv.Itoa(n)
		stop := make(chan struct{})
		f.active[session] = stop
		if f.mode != "answer-ambiguous-empty" {
			f.history[session] = append(f.history[session], map[string]any{"id": user, "type": "user", "text": input.Text})
		}
		f.mu.Unlock()
		if f.mode == "answer-ambiguous-empty" {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return true
		}
		go f.answer(session, user, assistant, stop, n)
		if f.mode == "answer-ambiguous" {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return true
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": user, "sessionID": session}})
		return true
	}
	return false
}

// savedAdmissionMatches checks real private disk state at the synthetic native dispatch boundary.
func savedAdmissionMatches(prompt, session string) bool {
	boundary := strings.Index(prompt, "\n\n")
	if boundary < 0 {
		return false
	}
	var input struct {
		ThreadID, AnswerID, Question string
		Source                       struct {
			RevisionID string
			Revision   struct{ PRID string }
		}
	}
	var generic struct{ Source any }
	if json.Unmarshal([]byte(prompt[boundary+2:]), &input) != nil || json.Unmarshal([]byte(prompt[boundary+2:]), &generic) != nil {
		return false
	}
	prID := input.Source.Revision.PRID
	if len(prID) != 64 || strings.Trim(prID, "0123456789abcdef") != "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("SHELDON_NATIVE_SAVED_ROOT"), "reviews", prID, "threads.json"))
	var saved struct {
		Bindings []struct {
			RevisionID, State string
			Native            struct{ SessionID string }
		}
		Threads []struct {
			ThreadID, RevisionID string
			Context              any
			Answers              []struct{ AnswerID, Question, State, Text string }
		}
	}
	if err != nil || json.Unmarshal(data, &saved) != nil {
		return false
	}
	bound := false
	for _, b := range saved.Bindings {
		if b.RevisionID == input.Source.RevisionID && b.State == "bound" && b.Native.SessionID == session {
			bound = true
		}
	}
	if !bound {
		return false
	}
	for _, thread := range saved.Threads {
		if thread.ThreadID != input.ThreadID || thread.RevisionID != input.Source.RevisionID || !reflect.DeepEqual(thread.Context, generic.Source) {
			continue
		}
		for _, a := range thread.Answers {
			if a.AnswerID == input.AnswerID && a.Question == input.Question && a.State == "pending" && a.Text == "" {
				audit(map[string]any{"admissionVerified": true, "threadID": input.ThreadID, "answerID": input.AnswerID})
				return true
			}
		}
	}
	return false
}

// answer produces labeled fixture text, hidden canaries, duplicate events and known terminal history.
func (f *executionFixture) answer(session, user, assistant string, stop <-chan struct{}, n int) {
	f.event("evt_start_"+strconv.Itoa(n), "session.execution.started", session, assistant, "")
	f.event("evt_foreign_"+strconv.Itoa(n), "session.text.delta", "ses_unrelated", "msg_foreign", "DO NOT DISPLAY FOREIGN TEXT")
	f.event("evt_reasoning_"+strconv.Itoa(n), "session.reasoning.delta", session, assistant, "DO NOT DISPLAY HIDDEN REASONING")
	text := "Synthetic fixture answer <script>literal</script>."
	chunks := []string{"Synthetic fixture ", "answer <script>literal</script>."}
	if f.mode == "answer-overflow" {
		text = strings.Repeat("x", 300<<10)
		chunks = []string{strings.Repeat("x", 65<<10)}
	}
	partial := ""
	for i, chunk := range chunks {
		partial += chunk
		f.event(fmt.Sprintf("evt_text_%d_%d", n, i), "session.text.delta", session, assistant, chunk)
		if i == 0 && f.mode == "answer-browser" {
			outcome := f.browserOutcome(stop, n)
			if outcome == "" {
				return
			}
			if outcome != "succeeded" {
				f.terminal(session, user, assistant, partial, outcome, n)
				return
			}
		}
		if f.mode == "answer-duplicate" {
			f.event(fmt.Sprintf("evt_text_%d_%d", n, i), "session.text.delta", session, assistant, chunk)
		}
		if i == 0 && f.mode == "answer-feed-loss" {
			select {
			case f.events <- "close-feed":
			case <-f.done:
			}
		}
		if i == 0 && (f.mode == "answer-wait" || f.mode == "answer-delayed" || f.mode == "answer-target-stop" && session == "ses_fixture") {
			wait := 300 * time.Millisecond
			if f.mode == "answer-wait" || f.mode == "answer-target-stop" {
				wait = time.Hour
			}
			timer := time.NewTimer(wait)
			select {
			case <-stop:
				timer.Stop()
				f.terminal(session, user, assistant, partial, "interrupted", n)
				return
			case <-f.done:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
	outcome := "succeeded"
	if f.mode == "answer-failed" {
		outcome = "failed"
	}
	f.terminal(session, user, assistant, text, outcome, n)
}

// browserOutcome waits for explicit test-owned release, targeted Stop or owned-service shutdown.
func (f *executionFixture) browserOutcome(stop <-chan struct{}, input int) string {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return "interrupted"
		case <-f.done:
			return ""
		case <-tick.C:
			data, err := os.ReadFile(os.Getenv("SHELDON_NATIVE_CONTROL"))
			var control struct {
				Input   int    `json:"input"`
				Outcome string `json:"outcome"`
			}
			if err == nil && json.Unmarshal(data, &control) == nil && control.Input == input && (control.Outcome == "succeeded" || control.Outcome == "failed") {
				return control.Outcome
			}
		}
	}
}

// terminal stores visible text separately from synthetic reasoning/tool/provider secret canaries.
func (f *executionFixture) terminal(session, user, assistant, text, outcome string, n int) {
	f.mu.Lock()
	f.history[session] = append(f.history[session], map[string]any{"id": assistant, "type": "assistant", "content": []map[string]string{{"type": "reasoning", "text": "HIDDEN REASONING SECRET"}, {"type": "tool", "text": "RAW TOOL SECRET"}, {"type": "text", "text": text}}}, map[string]any{"id": "msg_idle_" + strconv.Itoa(n), "type": "idle", "outcome": outcome, "error": map[string]string{"message": "RAW PROVIDER SECRET"}})
	delete(f.active, session)
	f.mu.Unlock()
	kind := "session.execution.succeeded"
	if outcome == "failed" {
		kind = "session.execution.failed"
	} else if outcome == "interrupted" {
		kind = "session.execution.interrupted"
	}
	f.event("evt_end_"+strconv.Itoa(n), kind, session, assistant, "")
	if f.mode == "answer-duplicate" {
		f.event("evt_end_"+strconv.Itoa(n), kind, session, assistant, "")
	}
}
