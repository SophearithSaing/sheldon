package app

import (
	"crypto/rand"
	"errors"
	"io"
	"os"

	"github.com/SophearithSaing/sheldon/internal/semantic"
)

// ReadSemantic performs an explicit checked read, clearing only proven storage uncertainty.
func (i *instance) ReadSemantic(prID string) (semantic.Saved, error) {
	i.semanticStoreMu.Lock()
	defer i.semanticStoreMu.Unlock()
	s, err := i.readSemanticLocked(prID)
	if err == nil {
		delete(i.semanticStoreUncertain, prID)
	}
	return s, err
}

// CheckSemantic checks predecessor health without clearing storage/native uncertainty.
func (i *instance) CheckSemantic(prID string, expected uint64) error {
	i.semanticStoreMu.Lock()
	defer i.semanticStoreMu.Unlock()
	if i.semanticStoreUncertain[prID] {
		return semantic.ErrSaveUncertain
	}
	s, err := i.readSemanticLocked(prID)
	if err != nil {
		return err
	}
	if expected == ^uint64(0) {
		return semantic.ErrLimit
	}
	if s.Version != expected {
		return semantic.ErrConflict
	}
	return nil
}

// readSemanticLocked validates private review parents without initializing source/native state.
func (i *instance) readSemanticLocked(prID string) (semantic.Saved, error) {
	root, err := i.questionDirectory(prID, false)
	if err != nil {
		return semantic.Saved{}, semantic.ErrStore
	}
	if root == nil {
		return semantic.EmptySaved(prID), nil
	}
	defer root.Close()
	return readSemanticFile(root, prID)
}

// SaveSemanticContent replaces one complete result against its exact saved predecessor.
func (i *instance) SaveSemanticContent(prID, reviewID string, content semantic.Content, expected uint64) error {
	i.semanticStoreMu.Lock()
	defer i.semanticStoreMu.Unlock()
	if i.semanticStoreUncertain[prID] {
		return semantic.ErrSaveUncertain
	}
	if expected == ^uint64(0) {
		return semantic.ErrLimit
	}
	next := semantic.Saved{SchemaVersion: 1, PRID: prID, Version: expected + 1, ReviewID: reviewID, Content: content}
	data, err := next.Encode()
	if err != nil {
		return err
	}
	root, err := i.questionDirectory(prID, false)
	if err != nil {
		return semantic.ErrStore
	}
	previous := semantic.EmptySaved(prID)
	if root != nil {
		defer root.Close()
		previous, err = readSemanticFile(root, prID)
		if err != nil {
			return err
		}
	}
	if previous.Version != expected || previous.ReviewID == reviewID {
		return semantic.ErrConflict
	}
	if root == nil {
		root, err = i.questionDirectory(prID, true)
		if err != nil {
			return semantic.ErrStore
		}
		defer root.Close()
	}
	if err := replaceSemanticFile(root, data); err != nil {
		if errors.Is(err, semantic.ErrSaveUncertain) {
			if i.semanticStoreUncertain == nil {
				i.semanticStoreUncertain = map[string]bool{}
			}
			i.semanticStoreUncertain[prID] = true
		}
		return err
	}
	return nil
}

// readSemanticFile refuses unsafe leaves and bounds bytes before strict graph validation.
func readSemanticFile(root *os.Root, prID string) (semantic.Saved, error) {
	info, err := root.Lstat("semantic-review.json")
	if errors.Is(err, os.ErrNotExist) {
		return semantic.EmptySaved(prID), nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return semantic.Saved{}, semantic.ErrStore
	}
	f, err := root.Open("semantic-review.json")
	if err != nil {
		return semantic.Saved{}, semantic.ErrStore
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) || checkPrivateFile(f) != nil {
		return semantic.Saved{}, semantic.ErrStore
	}
	data, err := io.ReadAll(io.LimitReader(f, semantic.MaxContentBytes+1))
	if err != nil {
		return semantic.Saved{}, semantic.ErrStore
	}
	return semantic.DecodeSaved(data, prID)
}

// replaceSemanticFile syncs and closes only a private operation-owned temporary before rename.
func replaceSemanticFile(root *os.Root, data []byte) error {
	name := ".semantic-review-" + rand.Text() + ".tmp"
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return semantic.ErrStore
	}
	defer root.Remove(name)
	if checkPrivateFile(f) != nil {
		f.Close()
		return semantic.ErrStore
	}
	n, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || n != len(data) || syncErr != nil || closeErr != nil {
		return semantic.ErrStore
	}
	if root.Rename(name, "semantic-review.json") != nil {
		return semantic.ErrSaveUncertain
	}
	return nil
}
