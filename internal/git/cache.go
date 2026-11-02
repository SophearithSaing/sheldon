package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/platform"
)

// Cache serializes repository mutations; its caller must retain the app writer lock.
// Use one Cache per owning app instance. Different data roots cannot share its cache root.
type Cache struct {
	life       sync.RWMutex
	mu         sync.Mutex
	gates      map[string]chan struct{}
	paths      platform.Paths
	owner      string
	rootInfo   os.FileInfo
	controls   string
	executable string
	closed     bool
}

type cacheOwner struct {
	Version  int    `json:"version"`
	DataRoot string `json:"dataRoot"`
}

type repositoryMarker struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Remote     string `json:"remote"`
	ConfigHash string `json:"configHash"`
}

// Open creates cache ownership/control files, never durable review records or a checkout.
func Open(paths platform.Paths) (*Cache, error) {
	if err := paths.Validate(); err != nil {
		return nil, err
	}
	executable, err := gitExecutable()
	if err != nil {
		return nil, err
	}
	data, err := filepath.EvalSymlinks(paths.Data)
	if err != nil {
		return nil, errors.New("existing app-owned data directory required")
	}
	if err := checkEntry(paths.Data, true); err != nil {
		return nil, err
	}
	cache, err := physicalPath(paths.Cache)
	if err != nil {
		return nil, err
	}
	physical := paths
	physical.Cache, physical.Data = cache, data
	if err := physical.Validate(); err != nil {
		return nil, err
	}
	if err := privateDirectory(paths.Cache); err != nil {
		return nil, err
	}
	owner := cacheOwner{Version: 1, DataRoot: data}
	ownerPath := filepath.Join(paths.Cache, "owner.json")
	if _, err := os.Lstat(ownerPath); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(paths.Cache)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Name() != "runtime" || paths.Runtime != filepath.Join(paths.Cache, "runtime") {
				return nil, errors.New("unidentified cache contents; choose an empty cache root")
			}
		}
		if err := createJSON(ownerPath, owner); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	var saved cacheOwner
	if readJSON(ownerPath, &saved) != nil || saved != owner {
		return nil, errors.New("cache belongs to another data root or has invalid ownership metadata; use a separate cache")
	}
	if err := privateDirectory(filepath.Join(paths.Cache, "repositories")); err != nil {
		return nil, err
	}
	controls, err := os.MkdirTemp(paths.Cache, ".git-controls-")
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Join(controls, "hooks"), 0700); err != nil {
		os.RemoveAll(controls)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(controls, "attributes"), nil, 0600); err != nil {
		os.RemoveAll(controls)
		return nil, err
	}
	info, err := os.Lstat(paths.Cache)
	if err != nil {
		os.RemoveAll(controls)
		return nil, err
	}
	return &Cache{paths: paths, owner: data, rootInfo: info, controls: controls, executable: executable, gates: make(map[string]chan struct{})}, nil
}

// Close waits for bounded operations, then removes only its own temporary controls.
func (c *Cache) Close() error {
	c.life.Lock()
	defer c.life.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if err := c.validateRoot(); err != nil {
		return err
	}
	return os.RemoveAll(c.controls)
}

// repository holds the per-repository gate and prepares a verified bare cache.
func (c *Cache) repository(ctx context.Context, pr githubpr.PullRequest) (string, func(), error) {
	if err := pr.Validate(); err != nil {
		return "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	c.life.RLock()
	if c.closed {
		c.life.RUnlock()
		return "", nil, errors.New("Git cache is closed")
	}
	id, _ := pr.RepositoryID()
	c.mu.Lock()
	gate := c.gates[id]
	if gate == nil {
		gate = make(chan struct{}, 1)
		c.gates[id] = gate
	}
	c.mu.Unlock()
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		c.life.RUnlock()
		return "", nil, ctx.Err()
	}
	release := func() { <-gate; c.life.RUnlock() }
	if err := c.validateRoot(); err != nil {
		release()
		return "", nil, err
	}
	dir := filepath.Join(c.paths.Cache, "repositories", id+".git")
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		stage, err := os.MkdirTemp(filepath.Dir(dir), ".repository-")
		if err != nil {
			release()
			return "", nil, err
		}
		defer os.RemoveAll(stage)
		if _, err := c.run(ctx, c.paths.Cache, "init", "--bare", "--shared=0600", "--object-format=sha1", "--template="+filepath.Join(c.controls, "hooks"), stage); err != nil {
			release()
			return "", nil, err
		}
		config, err := os.ReadFile(filepath.Join(stage, "config"))
		if err != nil {
			release()
			return "", nil, err
		}
		marker := repositoryMarker{Version: 1, ID: id, Remote: remoteURL(pr), ConfigHash: configHash(config)}
		if err := createJSON(filepath.Join(stage, "sheldon.json"), marker); err != nil {
			release()
			return "", nil, err
		}
		if err := os.Rename(stage, dir); err != nil {
			release()
			return "", nil, err
		}
	}
	if err := c.validateRepository(ctx, dir, pr); err != nil {
		release()
		return "", nil, err
	}
	return dir, release, nil
}

// validateRoot refuses changed ownership, unsafe namespaces and root replacement.
func (c *Cache) validateRoot() error {
	info, err := os.Lstat(c.paths.Cache)
	if err != nil || !os.SameFile(info, c.rootInfo) {
		return errors.New("Git cache root changed; reopen it")
	}
	for _, dir := range []string{c.paths.Cache, filepath.Join(c.paths.Cache, "repositories")} {
		if err := checkEntry(dir, true); err != nil {
			return err
		}
	}
	var owner cacheOwner
	if readJSON(filepath.Join(c.paths.Cache, "owner.json"), &owner) != nil || owner != (cacheOwner{Version: 1, DataRoot: c.owner}) {
		return errors.New("Git cache ownership changed")
	}
	return nil
}

// validateRepository rejects foreign configuration, link escapes and nonbare state.
func (c *Cache) validateRepository(ctx context.Context, dir string, pr githubpr.PullRequest) error {
	if err := checkEntry(dir, true); err != nil {
		return err
	}
	count := 0
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot inspect managed repository")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 250000 {
			return errors.New("managed repository has too many filesystem entries")
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if rel == "config.worktree" || rel == "info/grafts" || rel == "objects/info/alternates" || rel == "objects/info/http-alternates" || rel == "refs/replace" || rel == ".git" || strings.HasSuffix(rel, ".promisor") {
			return errors.New("unsupported managed repository state")
		}
		if err := checkEntry(path, entry.IsDir()); err != nil {
			return err
		}
		if strings.HasPrefix(rel, "refs/") && !entry.IsDir() {
			info, err := entry.Info()
			if err != nil || info.Size() > 128 {
				return errors.New("unsafe loose Git ref")
			}
			data, err := os.ReadFile(path)
			if err != nil || !githubpr.ValidCommit(strings.TrimSuffix(string(data), "\n")) {
				return errors.New("managed Git refs must be direct object identities, not symbolic refs or locks")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	id, _ := pr.RepositoryID()
	var marker repositoryMarker
	if readJSON(filepath.Join(dir, "sheldon.json"), &marker) != nil || marker.Version != 1 || marker.ID != id || marker.Remote != remoteURL(pr) {
		return errors.New("unidentified or mismatched managed repository")
	}
	info, err := os.Lstat(filepath.Join(dir, "config"))
	if err != nil || info.Size() > 64<<10 {
		return errors.New("invalid managed repository configuration size")
	}
	config, err := os.ReadFile(filepath.Join(dir, "config"))
	if err != nil || marker.ConfigHash != configHash(config) {
		return errors.New("managed repository configuration changed; preserve it and use a clean cache")
	}
	bare, err := c.run(ctx, dir, "rev-parse", "--is-bare-repository")
	if err != nil || string(bare) != "true\n" {
		return errors.New("managed repository is not bare")
	}
	actual, err := c.run(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	got, err := os.Stat(strings.TrimSpace(string(actual)))
	want, wantErr := os.Stat(dir)
	if err != nil || wantErr != nil || !os.SameFile(got, want) {
		return errors.New("managed Git directory association changed")
	}
	return nil
}

// remoteURL constructs the canonical upstream, never a fork-provided remote.
func remoteURL(pr githubpr.PullRequest) string {
	return "https://github.com/" + pr.Owner + "/" + pr.Repository + ".git"
}

// configHash binds a cache to its original local Git configuration.
func configHash(data []byte) string { hash := sha256.Sum256(data); return hex.EncodeToString(hash[:]) }

// createJSON creates bounded private ownership metadata without replacing existing state.
func createJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	return errors.Join(writeErr, syncErr, f.Close())
}

// readJSON reads only checked small ownership metadata, preserving malformed files.
func readJSON(path string, target any) error {
	if err := checkEntry(path, false); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Size() > 64<<10 {
		return errors.New("invalid cache metadata size")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// operation bounds the entire fetch/validation flow, including gate acquisition.
func operation(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 30*time.Second)
}

// physicalPath resolves existing ancestors before creating disposable storage.
func physicalPath(path string) (string, error) {
	current := path
	var missing []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for n := len(missing) - 1; n >= 0; n-- {
				resolved = filepath.Join(resolved, missing[n])
			}
			return resolved, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("cannot resolve cache ancestors")
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
