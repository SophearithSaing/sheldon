package app

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
)

var (
	errQuestionStore    = errors.New("cannot read or save private question history; existing data was preserved")
	errQuestionConflict = errors.New("saved question history changed; read it before updating")
	errQuestionReplace  = question.ErrSaveUncertain
)

// ReadQuestions supplies private saved snapshots to the question runtime, without native work.
func (i *instance) ReadQuestions(prID string) (question.Saved, error) { return i.readQuestions(prID) }

// SaveQuestions commits one checked predecessor generation before native prompt dispatch.
func (i *instance) SaveQuestions(next question.Saved, expected uint64) error {
	return i.saveQuestions(next, expected)
}

// readQuestions reads saved history without opening source caches or native services.
func (i *instance) readQuestions(prID string) (question.Saved, error) {
	i.questionStoreMu.Lock()
	defer i.questionStoreMu.Unlock()
	root, err := i.questionDirectory(prID, false)
	if err != nil {
		return question.Saved{}, err
	}
	if root == nil {
		return question.EmptySaved(prID), nil
	}
	defer root.Close()
	saved, err := readQuestionFile(root, prID)
	if err == nil {
		delete(i.questionStoreUncertain, prID)
	}
	return saved, err
}

// saveQuestions replaces one complete generation only if its predecessor still matches.
func (i *instance) saveQuestions(next question.Saved, expected uint64) error {
	i.questionStoreMu.Lock()
	defer i.questionStoreMu.Unlock()
	if i.questionStoreUncertain[next.PRID] {
		return errQuestionReplace
	}
	if next.Validate(next.PRID) != nil || expected == ^uint64(0) || next.Version != expected+1 {
		return errQuestionConflict
	}
	root, err := i.questionDirectory(next.PRID, false)
	if err != nil {
		return err
	}
	previous := question.EmptySaved(next.PRID)
	if root != nil {
		defer root.Close()
		previous, err = readQuestionFile(root, next.PRID)
		if err != nil {
			return err
		}
	}
	if previous.Version != expected || next.ValidateUpdate(previous) != nil {
		return errQuestionConflict
	}
	data, err := json.Marshal(next)
	if err != nil || len(data)+1 > question.MaxSavedBytes {
		return errQuestionStore
	}
	if root == nil {
		root, err = i.questionDirectory(next.PRID, true)
		if err != nil {
			return err
		}
		defer root.Close()
	}
	err = replaceQuestionFile(root, append(data, '\n'))
	if errors.Is(err, errQuestionReplace) {
		if i.questionStoreUncertain == nil {
			i.questionStoreUncertain = make(map[string]bool)
		}
		i.questionStoreUncertain[next.PRID] = true
	}
	return err
}

// questionDirectory verifies writer membership and each private managed parent before access.
func (i *instance) questionDirectory(prID string, create bool) (*os.Root, error) {
	if !githubpr.ValidID(prID) {
		return nil, errQuestionStore
	}
	if _, err := i.ReadPR(prID); err != nil {
		return nil, errQuestionStore
	}
	dataDir := filepath.Dir(i.file.Name())
	info, err := os.Lstat(dataDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errQuestionStore
	}
	if err := privateDirectory(dataDir); err != nil {
		return nil, errQuestionStore
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, errQuestionStore
	}
	defer root.Close()
	actualRoot, rootErr := root.Stat(".")
	if rootErr != nil || !os.SameFile(info, actualRoot) {
		return nil, errQuestionStore
	}
	lock, err := root.Lstat("instance.lock")
	original, statErr := i.file.Stat()
	if err != nil || statErr != nil || !os.SameFile(lock, original) {
		return nil, errQuestionStore
	}
	parent, err := privateQuestionChild(root, "reviews", create)
	if err != nil || parent == nil {
		return nil, err
	}
	defer parent.Close()
	return privateQuestionChild(parent, prID, create)
}

// privateQuestionChild refuses linked or unsafe managed parents, including dangling links.
func privateQuestionChild(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, nil
		}
		if err := privateDirectory(filepath.Join(parent.Name(), name)); err != nil {
			return nil, errQuestionStore
		}
		info, err = parent.Lstat(name)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errQuestionStore
	}
	if err := privateDirectory(filepath.Join(parent.Name(), name)); err != nil {
		return nil, errQuestionStore
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, errQuestionStore
	}
	actual, err := child.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		child.Close()
		return nil, errQuestionStore
	}
	return child, nil
}

// readQuestionFile reads a whole checked generation and preserves malformed or future bytes.
func readQuestionFile(root *os.Root, prID string) (question.Saved, error) {
	info, err := root.Lstat("threads.json")
	if errors.Is(err, os.ErrNotExist) {
		return question.EmptySaved(prID), nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return question.Saved{}, errQuestionStore
	}
	f, err := root.Open("threads.json")
	if err != nil {
		return question.Saved{}, errQuestionStore
	}
	defer f.Close()
	actual, statErr := f.Stat()
	if statErr != nil || !os.SameFile(info, actual) || checkPrivateFile(f) != nil {
		return question.Saved{}, errQuestionStore
	}
	data, err := io.ReadAll(io.LimitReader(f, question.MaxSavedBytes+1))
	if err != nil || len(data) > question.MaxSavedBytes || !utf8.Valid(data) || !uniqueQuestionJSON(data) {
		return question.Saved{}, errQuestionStore
	}
	var saved question.Saved
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&saved) != nil || d.Decode(new(any)) != io.EOF || saved.Validate(prID) != nil {
		return question.Saved{}, errQuestionStore
	}
	return saved, nil
}

// replaceQuestionFile installs only an exclusively created, flushed private temporary file.
func replaceQuestionFile(root *os.Root, data []byte) error {
	name := ".threads-" + rand.Text() + ".tmp"
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errQuestionStore
	}
	defer root.Remove(name)
	if checkPrivateFile(f) != nil {
		f.Close()
		return errQuestionStore
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errQuestionStore
	}
	if err := root.Rename(name, "threads.json"); err != nil {
		return errQuestionReplace
	}
	return nil
}

// uniqueQuestionJSON rejects duplicate or case-aliased keys before Go field matching.
func uniqueQuestionJSON(data []byte) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	return questionJSONValue(d, 0) && func() bool { _, err := d.Token(); return err == io.EOF }()
}

// questionJSONValue bounds nesting and checks every object, including escaped key aliases.
func questionJSONValue(d *json.Decoder, depth int) bool {
	if depth > 32 {
		return false
	}
	token, err := d.Token()
	if err != nil {
		return false
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return true
	}
	if delim != '{' && delim != '[' {
		return false
	}
	keys := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			text, ok := key.(string)
			if err != nil || !ok || len(keys) >= 32 {
				return false
			}
			for previous := range keys {
				if strings.EqualFold(previous, text) {
					return false
				}
			}
			keys[text] = true
		}
		if !questionJSONValue(d, depth+1) {
			return false
		}
	}
	end, err := d.Token()
	return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
}
