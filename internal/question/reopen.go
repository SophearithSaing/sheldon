package question

import (
	"reflect"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// savedPrompts rebuilds exact prior inputs for one shared revision, without inference or current defaults.
func savedPrompts(s Saved, revisionID string) ([]string, error) {
	prompts := []string{}
	size := 0
	for _, saved := range s.Threads {
		if saved.RevisionID != revisionID {
			continue
		}
		previous := &thread{}
		for _, answer := range saved.Answers {
			if answer.State == Pending || answer.State == Streaming {
				return nil, ErrBusy
			}
			prompt, err := questionPrompt(saved.Context, saved.ID, answer.ID, answer.Question, previous)
			if err != nil {
				return nil, err
			}
			size += len(prompt)
			if size > 8<<20 {
				return nil, ErrPromptLimit
			}
			prompts = append(prompts, prompt)
			previous.answers = append(previous.answers, &Turn{answer: answer})
		}
	}
	return prompts, nil
}

// restoreThread imports only saved terminal transcripts, keeping native readiness unverified until admission.
func (r *Runtime) restoreThread(prID, threadID string) (*thread, error) {
	if !githubpr.ValidID(prID) || !githubpr.ValidID(threadID) {
		return nil, ErrNotFound
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrRuntimeClosed
	}
	r.work.Add(1)
	r.mu.Unlock()
	defer r.work.Done()
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	if r.store == nil {
		return nil, ErrNotFound
	}
	s, err := r.saved(prID)
	if err != nil {
		return nil, err
	}
	var selected *SavedThread
	for n := range s.Threads {
		if s.Threads[n].ID == threadID {
			selected = &s.Threads[n]
			break
		}
	}
	if selected == nil {
		return nil, ErrNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrRuntimeClosed
	}
	if existing := r.threads[threadID]; existing != nil {
		if existing.context.Revision.PRID != prID {
			return nil, ErrNotFound
		}
		return existing, nil
	}
	key := revisionKey{prID, selected.RevisionID}
	b := r.bindings[key]
	if b != nil && b.busy {
		return nil, ErrBusy
	}
	newThreads, newAnswers := 0, 0
	for _, saved := range s.Threads {
		if saved.RevisionID != selected.RevisionID {
			continue
		}
		if existing := r.threads[saved.ID]; existing != nil {
			if existing.context.Revision != saved.Context.Revision {
				return nil, ErrNotFound
			}
			continue
		}
		newThreads++
		newAnswers += len(saved.Answers)
		for _, answer := range saved.Answers {
			if answer.State == Pending || answer.State == Streaming {
				return nil, ErrBusy
			}
			if r.answers[answer.ID] != nil {
				return nil, ErrNotFound
			}
		}
	}
	if r.threadCount+newThreads > maxThreads || r.answerCount+newAnswers > maxAnswers || b == nil && len(r.bindings) >= maxBindings {
		return nil, ErrCapacity
	}
	if b == nil {
		b = &binding{}
		r.bindings[key] = b
	}
	for _, saved := range s.Threads {
		if saved.RevisionID != selected.RevisionID || r.threads[saved.ID] != nil {
			continue
		}
		existing := &thread{context: ThreadContext{ID: saved.ID, Revision: saved.Context.Revision}, anchor: saved.Context.Anchor}
		for _, answer := range saved.Answers {
			turn := &Turn{runtime: r, binding: b, source: saved.Context, answer: answer, finished: true, started: true, stop: make(chan struct{}), changed: make(chan struct{}), saveState: "saved", savedVersion: s.Version}
			existing.answers = append(existing.answers, turn)
			r.answers[answer.ID] = turn
		}
		r.threads[saved.ID] = existing
	}
	r.threadCount += newThreads
	r.answerCount += newAnswers
	return r.threads[threadID], nil
}

// sameSavedSource verifies quoted context as well as the original revision before native continuation.
func sameSavedSource(source, saved Context) bool {
	source.Directory, saved.Directory = "", ""
	return reflect.DeepEqual(source, saved)
}
