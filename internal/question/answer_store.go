package question

import (
	"context"
	"reflect"
	"strconv"
)

// snapshotLocked adds live saving status without persisting self-referential file versions.
func (t *Turn) snapshotLocked() Answer {
	answer := t.answer
	answer.Persistence = t.saveState
	if t.savedVersion != 0 {
		answer.SavedVersion = strconv.FormatUint(t.savedVersion, 10)
	}
	return answer
}

// dirtyLocked records a normalized change while retaining a sticky save failure until explicit retry.
func (t *Turn) dirtyLocked() {
	t.sequence++
	if t.runtime.store != nil && t.saveState != "failed" {
		t.saveState = "saving"
	}
	t.notify()
}

// flush serializes exact-answer replacement with admissions; it never sends native input.
func (t *Turn) flush(retry bool) error {
	r := t.runtime
	if r.store == nil {
		return nil
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrRuntimeClosed
	}
	if t.saveState == "failed" && !retry {
		r.mu.Unlock()
		return ErrAnswerSave
	}
	if t.savedSequence == t.sequence && t.saveState == "saved" {
		r.mu.Unlock()
		return nil
	}
	answer, sequence := t.answer, t.sequence
	t.saveState = "saving"
	t.notify()
	r.mu.Unlock()
	version, err := r.saveAnswerSnapshot(answer)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		t.saveState = "failed"
		t.binding.saveFailed = true
		t.notify()
		return ErrAnswerSave
	}
	t.savedSequence, t.savedVersion = sequence, version
	if sequence == t.sequence {
		t.saveState = "saved"
		if t.binding.active == t {
			t.binding.saveFailed = false
			if t.finished {
				t.binding.busy, t.binding.active = false, nil
			}
		}
	}
	t.notify()
	return nil
}

// saveAnswerSnapshot merges only one existing answer into the latest checked private file.
func (r *Runtime) saveAnswerSnapshot(answer Answer) (uint64, error) {
	s, err := r.saved(answer.PRID)
	if err != nil || s.Version == ^uint64(0) {
		return 0, ErrAnswerSave
	}
	for n, thread := range s.Threads {
		if thread.ID != answer.ThreadID || thread.RevisionID != answer.RevisionID {
			continue
		}
		for k, previous := range thread.Answers {
			if previous.ID != answer.ID {
				continue
			}
			if previous.Question != answer.Question || previous.Reference != answer.Reference {
				return 0, ErrAnswerSave
			}
			if reflect.DeepEqual(previous, answer) {
				return s.Version, nil
			}
			s.Threads[n].Answers[k] = answer
			previousVersion := s.Version
			s.Version++
			if err := r.store.SaveQuestions(s, previousVersion); err != nil {
				return 0, ErrAnswerSave
			}
			return s.Version, nil
		}
	}
	return 0, ErrAnswerSave
}

// SaveAnswer explicitly retries retained runtime text only, without source loading or inference.
func (r *Runtime) SaveAnswer(ctx context.Context, prID, threadID, answerID string, input AnswerRequest) (Answer, error) {
	turn, err := r.answerTurn(prID, threadID, answerID, input)
	if err != nil {
		return Answer{}, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return Answer{}, ErrRuntimeClosed
	}
	r.work.Add(1)
	failed := turn.saveState == "failed"
	r.mu.Unlock()
	defer r.work.Done()
	if err := ctx.Err(); err != nil {
		return Answer{}, err
	}
	if !failed {
		return turn.Snapshot(), nil
	}
	err = turn.flush(true)
	return turn.Snapshot(), err
}
