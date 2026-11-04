package question

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/opencode"
)

const (
	// MaxSavedBytes bounds the encoded per-PR private file, including its final newline.
	MaxSavedBytes = 32 << 20
	// MaxSavedThreadBytes bounds one encoded retained thread.
	MaxSavedThreadBytes = 8 << 20
)

// Saved records one PR's complete private conversation snapshot, not a live runtime.
type Saved struct {
	SchemaVersion int            `json:"schemaVersion"`
	PRID          string         `json:"prID"`
	Version       uint64         `json:"version"`
	Bindings      []SavedBinding `json:"bindings"`
	Threads       []SavedThread  `json:"threads"`
}

// SavedBinding shares one native identity across every thread on a revision.
type SavedBinding struct {
	Revision   githubpr.Revision       `json:"revision"`
	RevisionID string                  `json:"revisionID"`
	State      string                  `json:"state"`
	Native     *opencode.BindingRecord `json:"native,omitempty"`
}

// SavedThread retains original backend-derived quotes and ordered submitted turns.
type SavedThread struct {
	ID         string   `json:"threadID"`
	RevisionID string   `json:"revisionID"`
	Context    Context  `json:"context"`
	Answers    []Answer `json:"answers"`
}

// EmptySaved returns an absent-file snapshot without creating storage.
func EmptySaved(prID string) Saved {
	return Saved{SchemaVersion: 1, PRID: prID, Bindings: []SavedBinding{}, Threads: []SavedThread{}}
}

// InterruptUnfinished copies unfinished saved turns as restart interruptions, never native-idle proof.
// The caller must validate the snapshot and persist a new generation before exposing changes.
func (s Saved) InterruptUnfinished() (Saved, bool) {
	next := s
	changed := false
	for n, thread := range s.Threads {
		for k, answer := range thread.Answers {
			if answer.State != Pending && answer.State != Streaming {
				continue
			}
			if !changed {
				next.Threads = append([]SavedThread(nil), s.Threads...)
				changed = true
			}
			next.Threads[n].Answers = append([]Answer(nil), thread.Answers...)
			answer.State, answer.ExecutionUnknown, answer.FailureCode = Interrupted, true, "runtime-restarted"
			next.Threads[n].Answers[k] = answer
		}
	}
	return next, changed
}

// Validate checks the complete saved graph without accessing Git or native history.
func (s Saved) Validate(prID string) error {
	invalid := errors.New("invalid saved question graph")
	if !githubpr.ValidID(prID) || s.PRID != prID || s.SchemaVersion != 1 || s.Version == 0 || s.Bindings == nil || s.Threads == nil || len(s.Bindings) > maxBindings || len(s.Threads) > maxThreads {
		return invalid
	}
	bindings := make(map[string]SavedBinding)
	nativeIDs := make(map[string]bool)
	for _, b := range s.Bindings {
		id, err := b.Revision.ID()
		if err != nil || b.Revision.Validate(prID) != nil || id != b.RevisionID {
			return invalid
		}
		if _, exists := bindings[id]; exists {
			return invalid
		}
		if b.State == "initializing" {
			if b.Native != nil {
				return invalid
			}
		} else if b.State != "bound" || b.Native == nil || b.Native.Validate() != nil || nativeIDs[b.Native.SessionID] {
			return invalid
		}
		if b.Native != nil {
			nativeIDs[b.Native.SessionID] = true
		}
		bindings[id] = b
	}
	header := s
	header.Threads = []SavedThread{}
	encoded, err := json.Marshal(header)
	if err != nil || len(encoded)+1 > MaxSavedBytes {
		return invalid
	}
	totalBytes := len(encoded) + 1
	ids, active := make(map[string]bool), make(map[string]bool)
	count := 0
	for threadIndex, thread := range s.Threads {
		binding, exists := bindings[thread.RevisionID]
		if !exists || binding.State != "bound" || !githubpr.ValidID(thread.ID) || ids[thread.ID] || thread.Context.Revision != binding.Revision || thread.Context.RevisionID != thread.RevisionID || validateSavedContext(thread.Context) != nil || len(thread.Answers) == 0 {
			return invalid
		}
		ids[thread.ID] = true
		textBytes := 0
		for n, answer := range thread.Answers {
			count++
			if count > maxAnswers || answer.Persistence != "" || answer.SavedVersion != "" || !githubpr.ValidID(answer.ID) || ids[answer.ID] || answer.PRID != prID || answer.ThreadID != thread.ID || answer.RevisionID != thread.RevisionID || !ValidText(answer.Question) || len(answer.Text) > maxAnswerBytes || !utf8.ValidString(answer.Text) || strings.ContainsRune(answer.Text, 0) || answer.Reference != (Reference{RevisionID: thread.RevisionID, Anchor: thread.Context.Anchor, OldPath: thread.Context.OldPath, NewPath: thread.Context.NewPath}) {
				return invalid
			}
			ids[answer.ID] = true
			textBytes += len(answer.Question) + len(answer.Text) + len(answer.Reference.OldPath) + len(answer.Reference.NewPath)
			if textBytes > MaxSavedThreadBytes {
				return invalid
			}
			switch answer.State {
			case Pending, Streaming, Completed, Failed, Interrupted:
			default:
				return invalid
			}
			if answer.State == Completed && (strings.TrimSpace(answer.Text) == "" || answer.ExecutionUnknown) || answer.ExecutionUnknown && answer.State != Interrupted {
				return invalid
			}
			if answer.FailureCode == "runtime-restarted" && (answer.State != Interrupted || !answer.ExecutionUnknown) {
				return invalid
			}
			switch answer.FailureCode {
			case "", "source-unavailable", "execution-failed", "answer-too-large", "execution-uncertain", "runtime-restarted":
			default:
				return invalid
			}
			if answer.State == Pending || answer.State == Streaming || answer.ExecutionUnknown {
				if n != len(thread.Answers)-1 || active[thread.RevisionID] {
					return invalid
				}
				active[thread.RevisionID] = true
			}
		}
		data, err := json.Marshal(thread)
		if err != nil || len(data) > MaxSavedThreadBytes {
			return invalid
		}
		totalBytes += len(data)
		if threadIndex > 0 {
			totalBytes++
		}
		if totalBytes > MaxSavedBytes {
			return invalid
		}
	}
	return nil
}

// ValidateUpdate prevents stale callers from rewriting immutable history or native bindings.
func (s Saved) ValidateUpdate(previous Saved) error {
	invalid := errors.New("conflicting saved question update")
	if s.Validate(previous.PRID) != nil || previous.Version == ^uint64(0) || s.Version != previous.Version+1 || len(s.Bindings) < len(previous.Bindings) || len(s.Threads) < len(previous.Threads) {
		return invalid
	}
	for n, old := range previous.Bindings {
		next := s.Bindings[n]
		if old.Revision != next.Revision || old.RevisionID != next.RevisionID || old.State == "bound" && !reflect.DeepEqual(old, next) {
			return invalid
		}
	}
	for n, old := range previous.Threads {
		next := s.Threads[n]
		if old.ID != next.ID || old.RevisionID != next.RevisionID || !reflect.DeepEqual(old.Context, next.Context) || len(next.Answers) < len(old.Answers) {
			return invalid
		}
		for k, answer := range old.Answers {
			updated := next.Answers[k]
			if answer.ID != updated.ID || answer.Question != updated.Question || answer.Reference != updated.Reference || answer.StopRequested && !updated.StopRequested {
				return invalid
			}
			if answer.State != Pending && answer.State != Streaming && !answer.ExecutionUnknown && !reflect.DeepEqual(answer, updated) {
				return invalid
			}
		}
	}
	return nil
}

// String withholds the saved transcripts and private native records.
func (s Saved) String() string { return "saved question snapshot (private details withheld)" }

// GoString keeps debug formatting private as well.
func (s Saved) GoString() string { return s.String() }
