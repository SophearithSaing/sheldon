package git

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// writeWorktreeMarker removes only its own incomplete marker on a write failure.
func writeWorktreeMarker(name string, marker worktreeMarker) (resultErr error) {
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	owned, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	defer func() {
		if resultErr != nil {
			if current, err := os.Lstat(name); err == nil && os.SameFile(owned, current) {
				resultErr = errors.Join(resultErr, os.Remove(name))
			}
		}
	}()
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

// regularBytes reads small metadata without following a linked or special leaf.
func regularBytes(name string, limit int64) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("invalid worktree metadata file")
	}
	return os.ReadFile(name)
}

// registrationNames records existing entries so cleanup never removes earlier registrations.
func registrationNames(repo string) (map[string]bool, error) {
	names := make(map[string]bool)
	entries, err := os.ReadDir(filepath.Join(repo, "worktrees"))
	if errors.Is(err, os.ErrNotExist) {
		return names, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	return names, nil
}

// worktreeAdmin verifies both directions of the native worktree registration.
func worktreeAdmin(repo, directory string) (string, error) {
	data, err := regularBytes(filepath.Join(directory, ".git"), 4096)
	if err != nil {
		return "", err
	}
	value, ok := strings.CutPrefix(strings.TrimSuffix(string(data), "\n"), "gitdir: ")
	admin := filepath.Clean(filepath.FromSlash(value))
	if !ok || !filepath.IsAbs(admin) || filepath.Dir(admin) != filepath.Join(repo, "worktrees") {
		return "", errors.New("worktree is not registered with the managed repository")
	}
	info, err := os.Lstat(admin)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe worktree registration directory")
	}
	backlink, err := regularBytes(filepath.Join(admin, "gitdir"), 4096)
	if err != nil || filepath.Clean(filepath.FromSlash(strings.TrimSuffix(string(backlink), "\n"))) != filepath.Join(directory, ".git") {
		return "", errors.New("worktree registration points to a different directory")
	}
	return admin, nil
}

// makeRegistrationPrivate adjusts only metadata created by this worktree-add operation.
func makeRegistrationPrivate(directory, admin string, before map[string]bool) error {
	if before[filepath.Base(admin)] {
		return errors.New("refusing to change an existing worktree registration")
	}
	if err := os.Chmod(filepath.Join(directory, ".git"), 0600); err != nil {
		return err
	}
	return filepath.WalkDir(admin, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil || !entry.IsDir() && !info.Mode().IsRegular() {
			return errors.New("unsafe new worktree registration")
		}
		mode := os.FileMode(0600)
		if entry.IsDir() {
			mode = 0700
		}
		if err := os.Chmod(name, mode); err != nil {
			return err
		}
		return checkEntry(name, entry.IsDir())
	})
}

// removeOwnedWorktree removes only a new directory and matching new registration.
func (c *Cache) removeOwnedWorktree(repo, directory string, before map[string]bool) error {
	if err := c.validateRoot(); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(repo, "worktrees"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		if before[entry.Name()] {
			continue
		}
		admin := filepath.Join(repo, "worktrees", entry.Name())
		backlink, err := regularBytes(filepath.Join(admin, "gitdir"), 4096)
		if err == nil && filepath.Clean(filepath.FromSlash(strings.TrimSuffix(string(backlink), "\n"))) == filepath.Join(directory, ".git") {
			if err := os.RemoveAll(admin); err != nil {
				return err
			}
		}
	}
	return os.RemoveAll(directory)
}
