package git

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

type sourceEntry struct{ mode, sha, name string }

// sourceEntries reads a bounded, portable manifest from the pinned Git tree.
func (c *Cache) sourceEntries(ctx context.Context, repo, head string) ([]sourceEntry, error) {
	output, err := c.run(ctx, repo, "ls-tree", "-r", "-z", "--full-tree", head)
	if err != nil {
		return nil, err
	}
	var entries []sourceEntry
	paths := make(map[string]bool)
	spellings := make(map[string]string)
	for len(output) != 0 {
		record, rest, found := bytes.Cut(output, []byte{0})
		if !found {
			return nil, errors.New("incomplete pinned source manifest")
		}
		output = rest
		header, name, found := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(header))
		if !found || len(fields) != 3 || !validSourcePath(string(name)) || !githubpr.ValidCommit(fields[2]) || len(entries) >= 10000 {
			return nil, errors.New("invalid, nonportable or excessive pinned source paths")
		}
		mode := fields[0]
		if mode != "100644" && mode != "100755" && mode != "120000" && mode != "160000" || mode == "160000" && fields[1] != "commit" || mode != "160000" && fields[1] != "blob" {
			return nil, errors.New("unsupported pinned source mode")
		}
		parts := strings.Split(string(name), "/")
		for n := range parts {
			key := strings.ToLower(strings.Join(parts[:n+1], "/"))
			spelling := strings.Join(parts[:n+1], "/")
			if previous, ok := spellings[key]; ok && previous != spelling {
				return nil, errors.New("case-colliding pinned source paths")
			}
			spellings[key] = spelling
			leaf := n == len(parts)-1
			if existing, ok := paths[key]; ok && (leaf || existing) {
				return nil, errors.New("colliding pinned source paths")
			}
			paths[key] = leaf
		}
		entries = append(entries, sourceEntry{mode: mode, sha: fields[2], name: string(name)})
	}
	return entries, nil
}

// validSourcePath rejects traversal and platform aliases of Git administrative paths.
func validSourcePath(name string) bool {
	if name == "" || !utf8.ValidString(name) || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:*?\"<>|") || strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimRight(part, " .") != part {
			return false
		}
		lower := strings.ToLower(part)
		if lower == ".git" || strings.HasPrefix(lower, ".git~") || strings.HasPrefix(lower, "git~") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	return true
}

// materialize writes raw blobs through a rooted handle, never checkout/filter commands.
func (c *Cache) materialize(ctx context.Context, repo, directory string, entries []sourceEntry) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	links := make(map[string]string)
	total := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.FromSlash(path.Dir(entry.name)), 0700); err != nil {
			return err
		}
		if entry.mode == "160000" {
			if err := root.Mkdir(filepath.FromSlash(entry.name), 0700); err != nil {
				return err
			}
			continue
		}
		data, err := c.run(ctx, repo, "cat-file", "blob", entry.sha)
		if err != nil {
			return err
		}
		total += len(data)
		if total > 64<<20 {
			return errors.New("pinned source exceeds the 64 MiB materialization limit")
		}
		if blobID(data, len(entry.sha)) != entry.sha {
			return errors.New("pinned blob content does not match its identity")
		}
		if entry.mode == "120000" {
			target := string(data)
			if !utf8.ValidString(target) || target == "" || strings.ContainsAny(target, "\x00\\:") || strings.HasPrefix(target, "/") || !validSourcePath(path.Clean(path.Join(path.Dir(entry.name), target))) {
				return errors.New("unsafe source symlink target")
			}
			links[entry.name] = target
			continue
		}
		mode := os.FileMode(0600)
		if entry.mode == "100755" {
			mode = 0700
		}
		file, err := root.OpenFile(filepath.FromSlash(entry.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
	}
	for name, target := range links {
		if err := root.Symlink(filepath.FromSlash(target), filepath.FromSlash(name)); err != nil {
			return errors.New("cannot create source symlink on this platform")
		}
	}
	for name := range links {
		if _, err := root.Stat(filepath.FromSlash(name)); err != nil {
			return errors.New("source symlink escapes the worktree, is cyclic or has a missing target")
		}
	}
	return nil
}

// validateWorktree checks source/index integrity without resetting or running clean filters.
func (c *Cache) validateWorktree(ctx context.Context, repo, directory, head string, entries []sourceEntry) error {
	if err := checkEntry(directory, true); err != nil {
		return err
	}
	admin, err := worktreeAdmin(repo, directory)
	if err != nil {
		return err
	}
	if err := checkEntry(admin, true); err != nil {
		return err
	}
	if data, err := regularBytes(filepath.Join(admin, "HEAD"), 128); err != nil || string(data) != head+"\n" {
		return errors.New("review worktree is not detached at its pinned head")
	}
	if data, err := regularBytes(filepath.Join(admin, "commondir"), 4096); err != nil || filepath.Clean(filepath.Join(admin, filepath.FromSlash(strings.TrimSuffix(string(data), "\n")))) != repo {
		return errors.New("review worktree has a different common repository")
	}
	index, err := c.run(ctx, directory, "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}
	expected := make(map[string]sourceEntry, len(entries))
	directories := map[string]bool{".": true}
	for _, entry := range entries {
		expected[entry.name] = entry
		for dir := path.Dir(entry.name); dir != "."; dir = path.Dir(dir) {
			directories[dir] = true
		}
		if entry.mode == "160000" {
			directories[entry.name] = true
		}
	}
	indexed := make(map[string]bool)
	for len(index) != 0 {
		record, rest, found := bytes.Cut(index, []byte{0})
		index = rest
		header, name, split := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(header))
		entry, ok := expected[string(name)]
		if !found || !split || len(fields) != 3 || !ok || indexed[string(name)] || fields[0] != entry.mode || fields[1] != entry.sha || fields[2] != "0" {
			return errors.New("review worktree index changed or is incomplete")
		}
		indexed[string(name)] = true
	}
	if len(indexed) != len(expected) {
		return errors.New("review worktree index is incomplete")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := make(map[string]bool)
	total := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot inspect review worktree source")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == ".git" {
			return checkEntry(filepath.Join(directory, ".git"), false)
		}
		if entry.IsDir() {
			if !directories[name] {
				return errors.New("untracked review worktree directory; existing source was not changed")
			}
			if expected[name].mode == "160000" {
				seen[name] = true
			}
			return checkEntry(filepath.Join(directory, filepath.FromSlash(name)), true)
		}
		want, ok := expected[name]
		if !ok || want.mode == "160000" {
			return errors.New("untracked or modified review worktree source; existing source was not changed")
		}
		seen[name] = true
		if want.mode == "120000" {
			info, err := root.Lstat(filepath.FromSlash(name))
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				return errors.New("review symlink type changed")
			}
			target, err := root.Readlink(filepath.FromSlash(name))
			if err != nil || blobID([]byte(filepath.ToSlash(target)), len(want.sha)) != want.sha {
				return errors.New("review symlink content changed")
			}
			if _, err := root.Stat(filepath.FromSlash(name)); err != nil {
				return errors.New("review symlink is unsafe or unavailable")
			}
			return nil
		}
		if err := checkEntry(filepath.Join(directory, filepath.FromSlash(name)), false); err != nil {
			return err
		}
		info, err := root.Lstat(filepath.FromSlash(name))
		if err != nil || info.Size() > 4<<20 {
			return errors.New("review source file is unavailable or oversized")
		}
		if runtime.GOOS != "windows" && (info.Mode().Perm()&0100 != 0) != (want.mode == "100755") {
			return errors.New("review source executable mode changed")
		}
		data, err := root.ReadFile(filepath.FromSlash(name))
		total += len(data)
		if err != nil || total > 64<<20 || blobID(data, len(want.sha)) != want.sha {
			return errors.New("review source content changed or exceeds materialization limits")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return errors.New("review source is incomplete")
	}
	return nil
}

// blobID checks literal source bytes against Git's immutable blob identity.
func blobID(data []byte, length int) string {
	content := append([]byte(fmt.Sprintf("blob %d\x00", len(data))), data...)
	if length == 64 {
		sum := sha256.Sum256(content)
		return hex.EncodeToString(sum[:])
	}
	sum := sha1.Sum(content)
	return hex.EncodeToString(sum[:])
}
