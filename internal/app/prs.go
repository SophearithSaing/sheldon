package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

const prIndexLimit = 4 << 20

// prIndex is the complete versioned registry, separate from disposable source caches.
type prIndex struct {
	SchemaVersion int                    `json:"schemaVersion"`
	PRs           []githubpr.PullRequest `json:"prs"`
}

// RegisterPR saves or reuses a PR while holding the existing app writer ownership.
func (i *instance) RegisterPR(raw string) (githubpr.PullRequest, error) {
	pr, err := githubpr.ParseURL(raw)
	if err != nil {
		return githubpr.PullRequest{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	index, err := i.readPRIndex()
	if err != nil {
		return githubpr.PullRequest{}, err
	}
	for _, saved := range index.PRs {
		if saved.ID == pr.ID {
			return saved, nil
		}
	}
	index.PRs = append(index.PRs, pr)
	if err := i.savePRIndex(index); err != nil {
		return githubpr.PullRequest{}, err
	}
	return pr, nil
}

// ReadPR looks up a validated ID without using caller data as a filesystem path.
func (i *instance) ReadPR(id string) (githubpr.PullRequest, error) {
	if !githubpr.ValidID(id) {
		return githubpr.PullRequest{}, errors.New("invalid pull request ID")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	index, err := i.readPRIndex()
	if err != nil {
		return githubpr.PullRequest{}, err
	}
	for _, pr := range index.PRs {
		if pr.ID == id {
			return pr, nil
		}
	}
	return githubpr.PullRequest{}, os.ErrNotExist
}

// readPRIndex reads the bounded registry under the instance mutex and writer lock.
func (i *instance) readPRIndex() (prIndex, error) {
	if !i.owned || i.closing {
		return prIndex{}, errors.New("PR index requires app writer ownership")
	}
	f, err := openPrivateFile(filepath.Join(filepath.Dir(i.file.Name()), "prs.json"), false)
	if errors.Is(err, os.ErrNotExist) {
		return prIndex{SchemaVersion: 1, PRs: []githubpr.PullRequest{}}, nil
	}
	if err != nil {
		return prIndex{}, errors.New("cannot open private PR index; existing data was not changed")
	}
	data, readErr := io.ReadAll(io.LimitReader(f, prIndexLimit+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || len(data) > prIndexLimit {
		return prIndex{}, errors.New("cannot read PR index within size limit; existing data was not changed")
	}
	var index prIndex
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if !utf8.Valid(data) || decoder.Decode(&index) != nil || decoder.Decode(new(any)) != io.EOF {
		return prIndex{}, errors.New("invalid PR index JSON; preserve prs.json and correct it before retrying")
	}
	if index.SchemaVersion != 1 || index.PRs == nil {
		return prIndex{}, errors.New("unsupported or incomplete PR index; preserve prs.json before retrying")
	}
	seen := make(map[string]bool, len(index.PRs))
	for _, pr := range index.PRs {
		if pr.Validate() != nil || seen[pr.ID] {
			return prIndex{}, errors.New("invalid or duplicate PR index record; preserve prs.json before retrying")
		}
		seen[pr.ID] = true
	}
	return index, nil
}

// savePRIndex atomically replaces the registry under the instance mutex and writer lock.
func (i *instance) savePRIndex(index prIndex) error {
	data, err := json.Marshal(index)
	if err != nil || len(data)+1 > prIndexLimit {
		return errors.New("PR index exceeds size limit; registration was not saved")
	}
	dir := filepath.Dir(i.file.Name())
	f, err := os.CreateTemp(dir, ".prs-*.tmp")
	if err != nil {
		return errors.New("cannot create private PR index temporary file; registration was not saved")
	}
	defer os.Remove(f.Name())
	if err := checkPrivateFile(f); err != nil {
		f.Close()
		return errors.New("PR index temporary file is not private; registration was not saved")
	}
	_, writeErr := f.Write(append(data, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("cannot write PR index temporary file; registration was not saved")
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, "prs.json")); err != nil {
		return errors.New("cannot replace PR index; registration was not saved")
	}
	return nil
}
