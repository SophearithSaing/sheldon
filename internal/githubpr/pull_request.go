package githubpr

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PullRequest identifies a registered PR independently of source availability.
type PullRequest struct {
	ID         string  `json:"id"`
	URL        string  `json:"url"`
	Owner      string  `json:"owner"`
	Repository string  `json:"repository"`
	Number     uint64  `json:"number,string"`
	Title      *string `json:"title"`
	Author     *string `json:"author"`
}

// ParseURL builds a canonical PR record without filesystem or network access.
func ParseURL(raw string) (PullRequest, error) {
	normalized, err := NormalizeURL(raw)
	if err != nil {
		return PullRequest{}, err
	}
	parts := strings.Split(strings.TrimPrefix(normalized, "https://github.com/"), "/")
	number, _ := strconv.ParseUint(parts[3], 10, 64)
	identity := "github.com/" + strings.ToLower(parts[0]) + "/" + strings.ToLower(parts[1]) + "/pull/" + parts[3]
	return PullRequest{
		ID: digest(identity), URL: "https://" + identity,
		Owner: strings.ToLower(parts[0]), Repository: strings.ToLower(parts[1]), Number: number,
	}, nil
}

// Validate checks canonical identity and optional display metadata.
func (p PullRequest) Validate() error {
	canonical, err := ParseURL(p.URL)
	if err != nil || p.ID != canonical.ID || p.URL != canonical.URL || p.Owner != canonical.Owner || p.Repository != canonical.Repository || p.Number != canonical.Number {
		return errors.New("invalid pull request identity")
	}
	if !validDescription(p.Title, 512) || !validDescription(p.Author, 256) {
		return errors.New("invalid pull request metadata")
	}
	return nil
}

// RepositoryID returns the shared repository identity for a valid PR record.
func (p PullRequest) RepositoryID() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return digest("github.com/" + p.Owner + "/" + p.Repository), nil
}

// ReviewPath returns a credential-free route; callers must also check index membership.
func ReviewPath(id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("invalid pull request ID")
	}
	return "/reviews/" + id, nil
}

// ValidID checks a lowercase SHA-256 identifier, not its existence or ownership.
func ValidID(id string) bool {
	return validHex(id, 64)
}

// digest returns the lowercase SHA-256 digest of an exact canonical identity.
func digest(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

// validHex checks full lowercase nonzero hexadecimal identities.
func validHex(value string, size int) bool {
	if len(value) != size || strings.Trim(value, "0") == "" {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// validDescription permits unknown or bounded, nonblank display text.
func validDescription(value *string, limit int) bool {
	if value == nil {
		return true
	}
	return len(*value) <= limit && utf8.ValidString(*value) && strings.TrimSpace(*value) != "" && !strings.ContainsFunc(*value, unicode.IsControl)
}
