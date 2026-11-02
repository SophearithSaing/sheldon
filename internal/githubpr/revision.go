package githubpr

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// BaseSource identifies how the upstream target branch was selected.
type BaseSource string

const (
	GitHubBase       BaseSource = "github"
	UserSelectedBase BaseSource = "user-selected"
)

// Revision describes pinned comparison identities, not verified source readiness.
type Revision struct {
	PRID         string     `json:"prID"`
	BaseBranch   string     `json:"baseBranch"`
	BaseSource   BaseSource `json:"baseSource"`
	BaseSHA      string     `json:"baseSHA"`
	HeadSHA      string     `json:"headSHA"`
	MergeBaseSHA string     `json:"mergeBaseSHA"`
}

// Validate checks commit shapes and association with the expected registered PR.
func (r Revision) Validate(prID string) error {
	if !ValidID(prID) || r.PRID != prID {
		return errors.New("invalid revision pull request identity")
	}
	if !validBranch(r.BaseBranch) || r.BaseSource != GitHubBase && r.BaseSource != UserSelectedBase {
		return errors.New("invalid revision base selection")
	}
	size := len(r.BaseSHA)
	if size != 40 && size != 64 || !validHex(r.BaseSHA, size) || !validHex(r.HeadSHA, size) || !validHex(r.MergeBaseSHA, size) {
		return errors.New("invalid revision commit identities")
	}
	return nil
}

// ID returns the comparison identity without consulting mutable branch tips.
func (r Revision) ID() (string, error) {
	if err := r.Validate(r.PRID); err != nil {
		return "", err
	}
	return digest(strings.Join([]string{r.PRID, r.BaseSHA, r.HeadSHA, r.MergeBaseSHA}, "\n")), nil
}

// validBranch checks a short upstream branch name without invoking Git.
func validBranch(branch string) bool {
	if branch == "" || len(branch) > 1024 || !utf8.ValidString(branch) || branch == "@" || strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.HasSuffix(branch, ".") {
		return false
	}
	for _, c := range branch {
		if c <= ' ' || c == 127 || strings.ContainsRune("~^:?*[\\", c) {
			return false
		}
	}
	for part := range strings.SplitSeq(branch, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
