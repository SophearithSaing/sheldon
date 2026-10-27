package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// instance holds one stable OS-locked file per data root. Never unlink it: a
// replacement inode could let two processes both believe they own the lock.
// Byte zero is reserved for Windows' mandatory byte-range lock; JSON starts at 1.
type instance struct {
	file  *os.File
	owned bool
}

type record struct {
	URL   string `json:"url"`
	Token string `json:"token"`
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
	if i.owned {
		return true, nil
	}
	owned, err := lockFile(i.file)
	i.owned = owned
	return owned, err
}

// publish replaces stale discovery bytes only while holding the writer lock.
func (i *instance) publish(r record) error {
	if !i.owned {
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
	var r record
	data, err := io.ReadAll(io.NewSectionReader(i.file, 1, 2049))
	if err != nil || len(data) > 2048 || json.Unmarshal(data, &r) != nil {
		return record{}, errors.New("instance record is unavailable")
	}
	return r, nil
}

// close clears an owned record and releases the OS lock, leaving its inode intact.
func (i *instance) close() error {
	var err error
	if i.owned {
		err = i.file.Truncate(0)
	}
	return errors.Join(err, i.file.Close())
}
