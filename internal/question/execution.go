package question

import (
	"context"
	"errors"
	"time"

	"github.com/SophearithSaing/sheldon/internal/opencode"
)

// Reference identifies only the backend-verified selection; model citations remain plain text.
type Reference struct {
	RevisionID string `json:"revisionID"`
	Anchor     Anchor `json:"anchor"`
	OldPath    string `json:"oldPath,omitempty"`
	NewPath    string `json:"newPath,omitempty"`
}

// notify replaces a bounded wake-up signal while the runtime mutex is held; snapshots are cumulative.
func (t *Turn) notify() {
	if t.changed != nil {
		close(t.changed)
	}
	t.changed = make(chan struct{})
}

// Start begins app-owned execution once; repeated calls cannot dispatch duplicate prompts.
func (t *Turn) Start() error {
	r := t.runtime
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRuntimeClosed
	}
	if t.started {
		return nil
	}
	if t.finished || t.binding.active != t {
		return ErrAnswerState
	}
	t.started = true
	r.work.Add(1)
	go t.execute()
	return nil
}

// execute revalidates pinned source and gives dispatch 4m50s plus at most 10s reconciliation.
func (t *Turn) execute() {
	r := t.runtime
	defer r.work.Done()
	ctx, cancel := context.WithTimeout(r.ctx, 4*time.Minute+50*time.Second)
	defer cancel()
	_, err := Resolve(ctx, t.answer.PRID, r.registry, r.cache, InitialRequest{Revision: t.source.Revision, Anchor: t.source.Anchor, Question: t.answer.Question})
	if err != nil {
		r.mu.Lock()
		t.answer.FailureCode = "source-unavailable"
		t.dirtyLocked()
		r.mu.Unlock()
		t.Finish(Failed, "")
		return
	}
	result := r.service.Execute(ctx, t.binding.session, t.prompt, t.stop, func(text string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if !r.drained && !t.finished {
			t.answer.Text, t.answer.State = text, Streaming
			t.dirtyLocked()
		}
	})
	r.mu.Lock()
	if result.Err != nil {
		t.answer.FailureCode = "execution-failed"
		if errors.Is(result.Err, opencode.ErrAnswerLimit) {
			t.answer.FailureCode = "answer-too-large"
		}
	}
	if !result.Idle {
		t.answer.FailureCode = "execution-uncertain"
	}
	if result.Text != "" {
		t.answer.Text = result.Text
	}
	text := t.answer.Text
	t.dirtyLocked()
	r.mu.Unlock()
	if result.Idle {
		state := State(result.State)
		if state == Completed {
			text = result.Text
		}
		t.Finish(state, text)
	} else {
		t.Uncertain()
	}
}

// answerTurn validates exact routed observation/Stop scope against runtime membership.
func (r *Runtime) answerTurn(prID, threadID, answerID string, input AnswerRequest) (*Turn, error) {
	if _, err := r.ReadAnswer(prID, threadID, answerID, input); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.answers[answerID], nil
}

// ObserveAnswer emits current cumulative snapshots without dispatching or cancelling execution.
func (r *Runtime) ObserveAnswer(ctx context.Context, prID, threadID, answerID string, input AnswerRequest, emit func(Answer) error) error {
	turn, err := r.answerTurn(prID, threadID, answerID, input)
	if err != nil {
		return err
	}
	for {
		r.mu.Lock()
		snapshot, changed := turn.snapshotLocked(), turn.changed
		done := r.drained || (turn.finished || turn.uncertain) && turn.saveState != "saving"
		r.mu.Unlock()
		if err := emit(snapshot); err != nil {
			return err
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// StopAnswer requests interruption only for the exact active answer; late Stop never reaches a newer turn.
func (r *Runtime) StopAnswer(prID, threadID, answerID string, input AnswerRequest) (Answer, error) {
	turn, err := r.answerTurn(prID, threadID, answerID, input)
	if err != nil {
		return Answer{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Answer{}, ErrRuntimeClosed
	}
	if turn.finished || turn.binding.active != turn {
		return turn.snapshotLocked(), nil
	}
	if turn.uncertain {
		return turn.snapshotLocked(), ErrUncertain
	}
	if !turn.answer.StopRequested {
		turn.answer.StopRequested = true
		close(turn.stop)
		turn.dirtyLocked()
	}
	return turn.snapshotLocked(), nil
}
