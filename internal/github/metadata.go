package github

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/process"
)

// MetadataError exposes only fixed failure categories and numeric native evidence.
type MetadataError struct {
	Code       string
	HTTPStatus int
	ExitCode   int
	cause      error
}

// Error returns a diagnostic without native output, URLs or credential values.
func (e *MetadataError) Error() string {
	return "GitHub metadata unavailable: " + e.Code
}

// Unwrap exposes only safe cancellation and output-limit causes.
func (e *MetadataError) Unwrap() error { return e.cause }

// metadata requests one PR through a bounded, noninteractive native gh command.
func metadata(ctx context.Context, pr githubpr.PullRequest) (Resolution, error) {
	executable, err := exec.LookPath("gh")
	if err != nil {
		code := "cli-unavailable"
		if errors.Is(err, exec.ErrNotFound) {
			code = "cli-missing"
		}
		return Resolution{}, &MetadataError{Code: code, ExitCode: -1}
	}
	ext := strings.ToLower(filepath.Ext(executable))
	if !filepath.IsAbs(executable) || ext == ".cmd" || ext == ".bat" || ext == ".ps1" || runtime.GOOS != "windows" && ext == ".exe" {
		return Resolution{}, &MetadataError{Code: "unsupported-executable", ExitCode: -1}
	}
	if !filepath.IsAbs(os.TempDir()) {
		return Resolution{}, &MetadataError{Code: "temporary-storage", ExitCode: -1}
	}
	dir, err := os.MkdirTemp("", "sheldon-gh-")
	if err != nil {
		return Resolution{}, &MetadataError{Code: "temporary-storage", ExitCode: -1}
	}
	defer os.RemoveAll(dir)
	request, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, runErr := process.Run(request, process.Command{
		Name: executable, Dir: dir,
		Args:        []string{"api", "--hostname", "github.com", "--method", "GET", "--include", "--header", "Accept: application/vnd.github+json", "repos/" + pr.Owner + "/" + pr.Repository + "/pulls/" + strconv.FormatUint(pr.Number, 10)},
		Env:         []string{"GH_HOST=github.com", "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "PAGER=cat", "GH_DEBUG=", "GH_FORCE_TTY=", "NO_COLOR=1", "CLICOLOR=0", "GH_NO_UPDATE_NOTIFIER=1", "GH_NO_EXTENSION_UPDATE_NOTIFIER=1"},
		OutputLimit: 1 << 20, GracePeriod: 100 * time.Millisecond,
	})
	status, body, parseErr := responseBody(result.Stdout)
	if ctx.Err() != nil {
		return Resolution{}, ctx.Err()
	}
	failure := &MetadataError{ExitCode: result.ExitCode, HTTPStatus: status}
	switch {
	case errors.Is(runErr, process.ErrOutputLimit):
		failure.Code, failure.cause = "output-limit", process.ErrOutputLimit
	case errors.Is(runErr, context.DeadlineExceeded):
		failure.Code, failure.cause = "timeout", context.DeadlineExceeded
	case result.ExitCode == 4 || status == 401:
		failure.Code = "authentication-required"
	case status == 403 || status == 429:
		failure.Code = "access-or-usage-restricted"
	case status == 404:
		failure.Code = "not-found-or-inaccessible"
	case status >= 500 && status <= 599:
		failure.Code = "service-unavailable"
	case runErr != nil:
		failure.Code = "cli-failed"
	case parseErr != nil || status != 200:
		failure.Code = "invalid-response"
	default:
		resolved, err := parseMetadata(body, pr)
		if err == nil {
			return resolved, nil
		}
		failure.Code = "invalid-metadata"
	}
	return Resolution{}, failure
}

// responseBody separates native --include headers from the already decoded body.
func responseBody(data []byte) (int, []byte, error) {
	end, separator := bytes.Index(data, []byte("\r\n\r\n")), 4
	if lf := bytes.Index(data, []byte("\n\n")); lf >= 0 && (end < 0 || lf < end) {
		end, separator = lf, 2
	}
	if end < 0 || end > 16<<10 {
		return 0, nil, errors.New("invalid metadata headers")
	}
	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader(data[:end+separator])))
	line, err := reader.ReadLine()
	fields := strings.Fields(line)
	if err != nil || len(fields) < 2 || (fields[0] != "HTTP/1.0" && fields[0] != "HTTP/1.1" && fields[0] != "HTTP/2.0") {
		return 0, nil, errors.New("invalid metadata status")
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil || len(fields[1]) != 3 || status < 100 || status > 599 {
		return 0, nil, errors.New("invalid metadata status")
	}
	if _, err := reader.ReadMIMEHeader(); err != nil {
		return 0, nil, errors.New("invalid metadata headers")
	}
	return status, data[end+separator:], nil
}

// parseMetadata validates PR identity and pinned reported commit shapes.
func parseMetadata(body []byte, expected githubpr.PullRequest) (Resolution, error) {
	var value struct {
		Number uint64  `json:"number"`
		URL    string  `json:"html_url"`
		Title  *string `json:"title"`
		User   *struct {
			Login *string `json:"login"`
		} `json:"user"`
		Base struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if !utf8.Valid(body) || decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return Resolution{}, errors.New("invalid metadata JSON")
	}
	pr, err := githubpr.ParseURL(value.URL)
	if err != nil || pr.ID != expected.ID || value.Number != expected.Number || !strings.EqualFold(value.Base.Repo.FullName, expected.Owner+"/"+expected.Repository) || !githubpr.ValidBranch(value.Base.Ref) || !githubpr.ValidCommit(value.Base.SHA) || !githubpr.ValidCommit(value.Head.SHA) || len(value.Base.SHA) != len(value.Head.SHA) {
		return Resolution{}, errors.New("invalid metadata identity")
	}
	pr.Title = value.Title
	if value.User != nil {
		pr.Author = value.User.Login
	}
	if pr.Validate() != nil {
		return Resolution{}, errors.New("invalid display metadata")
	}
	return Resolution{PR: pr, State: Resolved, Base: &Base{Branch: value.Base.Ref, SHA: value.Base.SHA, Source: githubpr.GitHubBase}, HeadSHA: value.Head.SHA}, nil
}
