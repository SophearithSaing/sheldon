// Package githubpr validates GitHub pull-request references without network access.
package githubpr

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// NormalizeURL accepts an HTTPS github.com PR URL and drops query/fragment data.
// It does not fetch, resolve or check access to the pull request.
func NormalizeURL(raw string) (string, error) {
	invalid := errors.New("expected https://github.com/<owner>/<repo>/pull/<positive-number>")
	if len(raw) > 2048 || strings.ContainsAny(raw, "\\\r\n\t ") {
		return "", invalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.Opaque != "" || u.EscapedPath() != u.Path {
		return "", invalid
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "" || !ownerPattern.MatchString(parts[1]) || !repoPattern.MatchString(parts[2]) || parts[2] == "." || parts[2] == ".." || parts[3] != "pull" {
		return "", invalid
	}
	n, err := strconv.ParseUint(parts[4], 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != parts[4] {
		return "", invalid
	}
	return "https://github.com/" + parts[1] + "/" + parts[2] + "/pull/" + parts[4], nil
}
