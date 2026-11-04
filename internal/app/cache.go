package app

import (
	"errors"
	"path/filepath"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/platform"
)

// openCache lazily attaches one cache to the existing data-root writer lifetime.
func (i *instance) openCache(paths platform.Paths) (*git.Cache, error) {
	return i.attachCache(paths, false)
}

// openExistingCache attaches source for explicit continuation without recreating missing cache state.
func (i *instance) openExistingCache(paths platform.Paths) (*git.Cache, error) {
	return i.attachCache(paths, true)
}

// attachCache checks app ownership and keeps one matching Git cache for its writer lifetime.
func (i *instance) attachCache(paths platform.Paths, existing bool) (*git.Cache, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := paths.Validate(); err != nil {
		return nil, err
	}
	if !i.owned || i.closing || filepath.Dir(i.file.Name()) != paths.Data {
		return nil, errors.New("matching app writer ownership is required for Git cache access")
	}
	if i.cache != nil && i.cacheRoot != paths.Cache {
		return nil, errors.New("an owning instance cannot change its active Git cache root")
	}
	if i.cache == nil {
		var cache *git.Cache
		var err error
		if existing {
			cache, err = git.OpenExisting(paths)
		} else {
			cache, err = git.Open(paths)
		}
		if err != nil {
			return nil, err
		}
		i.cache = cache
		i.cacheRoot = paths.Cache
	}
	return i.cache, nil
}
