package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/question"
)

// instance holds one stable OS-locked discovery file per data root.
type instance struct {
	mu                     sync.Mutex
	file                   *os.File
	owned                  bool
	cache                  *git.Cache
	cacheRoot              string
	questions              *question.Runtime
	questionStoreMu        sync.Mutex
	questionStoreUncertain map[string]bool
	questionsLoaded        map[string]bool
	closing                bool
	closeOnce              sync.Once
	closeErr               error
}

type record struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	DevOrigin string `json:"devOrigin,omitempty"`
}

// openInstance opens a private lock/discovery file without claiming ownership.
func openInstance(dataDir string) (*instance, error) {
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return nil, errors.New("instance directory must be a clean absolute path")
	}
	if err := privateDirectory(dataDir); err != nil {
		return nil, err
	}
	f, err := privateFile(filepath.Join(dataDir, "instance.lock"))
	if err != nil {
		return nil, err
	}
	return &instance{file: f}, nil
}

// acquire tries the OS lock; it never decides ownership from a PID or timestamp.
func (i *instance) acquire() (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closing {
		return false, errors.New("instance is closing")
	}
	if i.owned {
		return true, nil
	}
	owned, err := lockFile(i.file)
	i.owned = owned
	return owned, err
}

// publish replaces stale discovery bytes only while holding the writer lock.
func (i *instance) publish(r record) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.owned || i.closing {
		return errors.New("writer lock required")
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := i.file.Truncate(0); err != nil {
		return err
	}
	if _, err := i.file.WriteAt(append([]byte{'\n'}, data...), 0); err != nil {
		return err
	}
	return i.file.Sync()
}

// read reads bounded discovery bytes without touching Windows' locked byte zero.
func (i *instance) read() (record, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	var r record
	data, err := io.ReadAll(io.NewSectionReader(i.file, 1, 2049))
	if err != nil || len(data) > 2048 || json.Unmarshal(data, &r) != nil {
		return record{}, errors.New("instance record is unavailable")
	}
	return r, nil
}

// close drains owned questions/cache before clearing discovery and releasing the OS lock.
func (i *instance) close() error {
	i.closeOnce.Do(func() {
		i.mu.Lock()
		i.closing = true
		questions := i.questions
		i.mu.Unlock()
		if questions != nil {
			i.closeErr = questions.Close()
		}
		i.questionStoreMu.Lock()
		defer i.questionStoreMu.Unlock()
		i.mu.Lock()
		defer i.mu.Unlock()
		if i.cache != nil {
			i.closeErr = errors.Join(i.closeErr, i.cache.Close())
		}
		if i.owned {
			i.closeErr = errors.Join(i.closeErr, i.file.Truncate(0))
		}
		i.owned = false
		i.closeErr = errors.Join(i.closeErr, i.file.Close())
	})
	return i.closeErr
}
