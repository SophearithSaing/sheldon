package question

import (
	"errors"
	"reflect"

	"github.com/SophearithSaing/sheldon/internal/opencode"
)

// Persistence reads and replaces complete private PR snapshots under app writer ownership.
type Persistence interface {
	ReadQuestions(string) (Saved, error)
	SaveQuestions(Saved, uint64) error
}

// persistenceError withholds storage diagnostics while preserving uncertain admission semantics.
func persistenceError(err error) error {
	if errors.Is(err, ErrSaveUncertain) {
		return ErrSaveUncertain
	}
	return ErrPersistence
}

// saved reads the latest generation without loading Git or starting native services.
func (r *Runtime) saved(prID string) (Saved, error) {
	if r.store == nil {
		return Saved{}, ErrPersistence
	}
	s, err := r.store.ReadQuestions(prID)
	if err != nil {
		return Saved{}, persistenceError(err)
	}
	return s, nil
}

// commitSaved replaces one generation under the persistence mutex, without implicit retries.
func (r *Runtime) commitSaved(s Saved) error {
	if s.Version == ^uint64(0) {
		return ErrCapacity
	}
	previous := s.Version
	s.Version++
	if s.Validate(s.PRID) != nil || s.admissionHeadroom() != nil {
		return ErrCapacity
	}
	if err := r.store.SaveQuestions(s, previous); err != nil {
		return persistenceError(err)
	}
	return nil
}

// prepareSavedBinding records initialization before native creation and refuses stored replacements.
func (r *Runtime) prepareSavedBinding(source Context, b *binding) error {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	s, err := r.saved(source.Revision.PRID)
	if err != nil {
		return err
	}
	for _, saved := range s.Bindings {
		if saved.RevisionID != source.RevisionID {
			continue
		}
		if b.session == nil || saved.State != "bound" || saved.Native == nil || saved.Revision != source.Revision || *saved.Native != b.session.Binding() {
			r.mu.Lock()
			b.broken = ErrSessionUnavailable
			r.mu.Unlock()
			return ErrSessionUnavailable
		}
		return nil
	}
	if b.session != nil {
		return ErrPersistence
	}
	if len(s.Bindings) >= maxBindings {
		return ErrCapacity
	}
	s.Bindings = append(s.Bindings, SavedBinding{Revision: source.Revision, RevisionID: source.RevisionID, State: "initializing"})
	if err := r.commitSaved(s); err != nil {
		if errors.Is(err, ErrSaveUncertain) {
			r.mu.Lock()
			b.broken = err
			r.mu.Unlock()
		}
		return err
	}
	return nil
}

// saveBoundSession commits the exact created identity before any turn may be admitted.
func (r *Runtime) saveBoundSession(source Context, session *opencode.Session) error {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	s, err := r.saved(source.Revision.PRID)
	if err != nil {
		return err
	}
	for n, b := range s.Bindings {
		if b.RevisionID != source.RevisionID {
			continue
		}
		if b.State != "initializing" || b.Revision != source.Revision {
			return ErrPersistence
		}
		descriptor := session.Binding()
		s.Bindings[n].State, s.Bindings[n].Native = "bound", &descriptor
		return r.commitSaved(s)
	}
	return ErrPersistence
}

// saveAdmission commits a new pending turn before runtime publication or native dispatch.
func (r *Runtime) saveAdmission(turn *Turn, followup bool) error {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	s, err := r.saved(turn.answer.PRID)
	if err != nil {
		return err
	}
	bound := false
	for _, b := range s.Bindings {
		if b.RevisionID == turn.answer.RevisionID && b.State == "bound" && b.Native != nil && *b.Native == turn.binding.session.Binding() {
			bound = true
		}
	}
	if !bound {
		return ErrPersistence
	}
	quoted := turn.source
	quoted.Directory = ""
	found := false
	for n, thread := range s.Threads {
		if thread.ID != turn.answer.ThreadID {
			continue
		}
		if !followup || !reflect.DeepEqual(thread.Context, quoted) {
			return ErrPersistence
		}
		s.Threads[n].Answers = append(s.Threads[n].Answers, turn.answer)
		found = true
		break
	}
	if !found {
		if followup {
			return ErrPersistence
		}
		s.Threads = append(s.Threads, SavedThread{ID: turn.answer.ThreadID, RevisionID: turn.answer.RevisionID, Context: quoted, Answers: []Answer{turn.answer}})
	}
	if err := r.commitSaved(s); err != nil {
		return err
	}
	turn.saveState, turn.savedVersion = "saved", s.Version+1
	return nil
}
