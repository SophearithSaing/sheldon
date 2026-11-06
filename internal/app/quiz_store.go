package app

import (
	"crypto/rand"
	"errors"
	"io"
	"os"

	"github.com/SophearithSaing/sheldon/internal/quiz"
)

// ReadQuiz explicitly reads private saved state and resolves only valid storage uncertainty.
func (i *instance) ReadQuiz(prID string) (quiz.Saved, error) {
	i.quizStoreMu.Lock()
	defer i.quizStoreMu.Unlock()
	saved, err := i.readQuizLocked(prID)
	if err == nil {
		delete(i.quizStoreUncertain, prID)
	}
	return saved, err
}

// CheckQuiz verifies healthy predecessor storage without clearing uncertain replacement state.
func (i *instance) CheckQuiz(prID string, expected uint64) error {
	i.quizStoreMu.Lock()
	defer i.quizStoreMu.Unlock()
	if i.quizStoreUncertain[prID] {
		return quiz.ErrSaveUncertain
	}
	saved, err := i.readQuizLocked(prID)
	if err != nil {
		return err
	}
	if expected == ^uint64(0) {
		return quiz.ErrLimit
	}
	if saved.Version != expected {
		return quiz.ErrConflict
	}
	return nil
}

// readQuizLocked reuses checked review parents without initializing source or native state.
func (i *instance) readQuizLocked(prID string) (quiz.Saved, error) {
	root, err := i.questionDirectory(prID, false)
	if err != nil {
		return quiz.Saved{}, quiz.ErrStore
	}
	if root == nil {
		return quiz.EmptySaved(prID), nil
	}
	defer root.Close()
	return readQuizFile(root, prID)
}

// SaveQuiz validates and replaces one complete set only against its expected predecessor.
func (i *instance) SaveQuiz(next quiz.Saved, expected uint64) error {
	i.quizStoreMu.Lock()
	defer i.quizStoreMu.Unlock()
	if i.quizStoreUncertain[next.PRID] {
		return quiz.ErrSaveUncertain
	}
	data, err := next.Encode()
	if err != nil {
		return quiz.ErrStore
	}
	if expected == ^uint64(0) || next.Version != expected+1 {
		return quiz.ErrConflict
	}
	root, err := i.questionDirectory(next.PRID, false)
	if err != nil {
		return quiz.ErrStore
	}
	previous := quiz.EmptySaved(next.PRID)
	if root != nil {
		defer root.Close()
		previous, err = readQuizFile(root, next.PRID)
		if err != nil {
			return err
		}
	}
	if previous.Version != expected || previous.QuizID == next.QuizID {
		return quiz.ErrConflict
	}
	if root == nil {
		root, err = i.questionDirectory(next.PRID, true)
		if err != nil {
			return quiz.ErrStore
		}
		defer root.Close()
	}
	err = replaceQuizFile(root, data)
	if errors.Is(err, quiz.ErrSaveUncertain) {
		if i.quizStoreUncertain == nil {
			i.quizStoreUncertain = make(map[string]bool)
		}
		i.quizStoreUncertain[next.PRID] = true
	}
	return err
}

// readQuizFile refuses unsafe leaves and bounds decoding without changing diagnostic bytes.
func readQuizFile(root *os.Root, prID string) (quiz.Saved, error) {
	info, err := root.Lstat("quiz.json")
	if errors.Is(err, os.ErrNotExist) {
		return quiz.EmptySaved(prID), nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return quiz.Saved{}, quiz.ErrStore
	}
	f, err := root.Open("quiz.json")
	if err != nil {
		return quiz.Saved{}, quiz.ErrStore
	}
	defer f.Close()
	actual, statErr := f.Stat()
	if statErr != nil || !os.SameFile(info, actual) || checkPrivateFile(f) != nil {
		return quiz.Saved{}, quiz.ErrStore
	}
	data, err := io.ReadAll(io.LimitReader(f, quiz.MaxContentBytes+1))
	if err != nil {
		return quiz.Saved{}, quiz.ErrStore
	}
	return quiz.DecodeSaved(data, prID)
}

// replaceQuizFile syncs/closes only an operation-owned private temporary before replacement.
func replaceQuizFile(root *os.Root, data []byte) error {
	name := ".quiz-" + rand.Text() + ".tmp"
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return quiz.ErrStore
	}
	defer root.Remove(name)
	if checkPrivateFile(f) != nil {
		f.Close()
		return quiz.ErrStore
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return quiz.ErrStore
	}
	if root.Rename(name, "quiz.json") != nil {
		return quiz.ErrSaveUncertain
	}
	return nil
}
