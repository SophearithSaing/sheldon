// Package environment inspects local CLI metadata without starting agent sessions.
package environment

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/SophearithSaing/sheldon/internal/process"
)

// Status describes evidence from a check, not authorization to run an agent.
type Status string

const (
	Available   Status = "available"
	Missing     Status = "missing"
	Unsupported Status = "unsupported"
	Unknown     Status = "unknown"
)

// Finding contains a fixed public explanation, never raw subprocess output.
type Finding struct {
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Capabilities separates advertised CLI features from unverified runtime safety.
type Capabilities struct {
	Protocol       Finding `json:"protocol"`
	Service        Finding `json:"service"`
	Policy         Finding `json:"policy"`
	Authentication Finding `json:"authentication"`
	Entitlement    Finding `json:"entitlement"`
	Compatibility  Finding `json:"compatibility"`
}

// Tool reports installation metadata and external setup guidance for a known CLI.
type Tool struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Optional      bool          `json:"optional"`
	Status        Status        `json:"status"`
	Version       string        `json:"version,omitempty"`
	Detail        string        `json:"detail"`
	Guidance      string        `json:"guidance"`
	Documentation string        `json:"documentation"`
	Capabilities  *Capabilities `json:"capabilities,omitempty"`
}

// Report is a point-in-time, nonpersistent snapshot; no tool is selected automatically.
type Report struct {
	Tools []Tool `json:"tools"`
}

type toolSpec struct {
	id, name, documentation, guidance string
	optional                          bool
	version                           *regexp.Regexp
	help                              []string
	flags                             []string
	policyHelp                        []string
	policyFlags                       []string
}

// Numeric versions are the only subprocess text allowed into the report. Unknown
// banners/prereleases remain unknown instead of exposing paths, accounts or secrets.
const numericVersion = `([0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6})`

var tools = []toolSpec{
	{
		id: "git", name: "Git", documentation: "https://git-scm.com/downloads",
		guidance: "If Git is missing or broken, install or repair it externally and make it available on PATH. Repository access and Git authentication are not checked.",
		version:  regexp.MustCompile(`^git version ` + numericVersion + `(?:[ .][A-Za-z0-9(). -]+)?$`),
	},
	{
		id: "gh", name: "GitHub CLI", optional: true, documentation: "https://cli.github.com/manual/",
		guidance: "gh is optional. Install or repair it externally if desired; use its normal external sign-in flow if repository access later reports an authentication problem.",
		version:  regexp.MustCompile(`^gh version ` + numericVersion + `(?: .*)?$`),
	},
	{
		id: "opencode", name: "OpenCode", documentation: "https://opencode.ai/v2/docs/cli/",
		guidance: "If you choose OpenCode, use an externally installed V2. For service diagnostics, run opencode service status externally. " +
			"Resolve any reported login or quota problem in OpenCode using your existing provider/subscription route; Sheldon does not change it or manage the shared service.",
		version: regexp.MustCompile(`^v?` + numericVersion + `$`),
		help:    []string{"serve", "--help"}, flags: []string{"--hostname", "--port"},
	},
	{
		id: "codex", name: "Codex", documentation: "https://developers.openai.com/codex/app-server",
		guidance: "If you choose Codex and it is missing or broken, install or repair it externally. Use its normal external sign-in/setup if it reports an authentication problem; preserve your existing provider/subscription route.",
		version:  regexp.MustCompile(`^codex-cli ` + numericVersion + `$`),
		help:     []string{"app-server", "--help"}, flags: []string{"--listen"},
		policyHelp: []string{"--help"}, policyFlags: []string{"--sandbox", "--ask-for-approval"},
	},
	{
		id: "claude", name: "Claude Code", documentation: "https://code.claude.com/docs/en/setup",
		guidance: "If you choose Claude Code and it is missing or broken, install or repair it externally. " +
			"Use its normal external sign-in/setup if it reports an authentication problem; do not switch billing routes or use bare mode as a subscription-login workaround.",
		version: regexp.MustCompile(`^` + numericVersion + ` \(Claude Code\)$`),
		help:    []string{"--help"}, flags: []string{"--print", "--output-format", "--include-partial-messages", "--verbose", "--resume"},
		policyFlags: []string{"--permission-mode", "--disallowedTools"},
	},
}

// Inspect runs only fixed version/help commands in a private, nonrepository directory.
// It never reads harness state/config/credential files, probes a service, or dispatches
// prompts. Installed executables are trusted user software, not sandboxed by Sheldon.
func Inspect(ctx context.Context) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	dir, err := os.MkdirTemp("", "sheldon-environment-")
	if err != nil {
		return Report{}, errors.New("environment checks require a writable OS temporary directory")
	}
	defer os.RemoveAll(dir)
	report := Report{Tools: make([]Tool, len(tools))}
	var checks sync.WaitGroup
	for index, spec := range tools {
		checks.Go(func() { report.Tools[index] = inspectTool(ctx, dir, spec) })
	}
	checks.Wait()
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	return report, nil
}

// inspectTool resolves a CLI on PATH and inspects only its allowlisted metadata surface.
func inspectTool(ctx context.Context, dir string, spec toolSpec) Tool {
	tool := Tool{ID: spec.id, Name: spec.name, Optional: spec.optional, Status: Unknown, Guidance: spec.guidance, Documentation: spec.documentation}
	if len(spec.help) > 0 {
		tool.Capabilities = &Capabilities{
			Protocol:       Finding{Unknown, "Protocol metadata was not verified."},
			Service:        Finding{Unknown, "No service was started, discovered or contacted. Service health is unknown."},
			Policy:         Finding{Unknown, "Read-only restrictions and startup integrations have not been validated by a production adapter."},
			Authentication: Finding{Unknown, "Login state was not checked; no credential store was inspected."},
			Entitlement:    Finding{Unknown, "Subscription, model access and remaining quota were not checked."},
			Compatibility:  Finding{Unknown, "No production adapter is validated yet. CLI metadata alone does not prove compatibility."},
		}
	}
	name, err := exec.LookPath(spec.id)
	if errors.Is(err, exec.ErrNotFound) {
		tool.Status, tool.Detail = Missing, "Executable was not found on this process's PATH."
		return tool
	}
	if err != nil || !filepath.IsAbs(name) {
		tool.Detail = "Executable lookup failed or uses a relative PATH entry. Use a trusted absolute PATH entry."
		return tool
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		tool.Detail = "Executable could not be resolved. Check its external installation and permissions."
		return tool
	}
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext == ".cmd" || ext == ".bat" || (runtime.GOOS != "windows" && ext == ".exe") {
		tool.Status, tool.Detail = Unsupported, "Windows command-shell shims and cross-environment Windows executables are not run. Use a native executable in the same environment as Sheldon."
		return tool
	}
	output, finding := probe(ctx, dir, name, []string{"--version"})
	if finding.Status != Available {
		tool.Detail = finding.Detail
		return tool
	}
	firstLine, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	version := spec.version.FindStringSubmatch(strings.TrimSpace(firstLine))
	if len(version) != 2 {
		tool.Detail = "Executable was found, but its version format is not recognized. Raw output is withheld."
		return tool
	}
	tool.Status, tool.Version, tool.Detail = Available, version[1], "Executable and numeric version detected; authentication and runtime compatibility are not implied."
	if tool.Capabilities == nil {
		return tool
	}
	if spec.id == "opencode" && !strings.HasPrefix(tool.Version, "2.") {
		state, detail := Unknown, "This OpenCode major version has not been evaluated. Only V2 metadata checks are implemented."
		if strings.HasPrefix(tool.Version, "0.") || strings.HasPrefix(tool.Version, "1.") {
			state, detail = Unsupported, "OpenCode V1 is not supported; Sheldon targets the V2 native HTTP/SSE interface."
		}
		tool.Capabilities.Protocol = Finding{state, detail}
		tool.Capabilities.Compatibility = Finding{state, detail}
		return tool
	}
	output, finding = probe(ctx, dir, name, spec.help)
	tool.Capabilities.Protocol = finding
	if finding.Status != Available {
		return tool
	}
	for _, flag := range spec.flags {
		if !advertises(output, flag) {
			tool.Capabilities.Protocol = Finding{Unknown, "Help did not advertise every expected protocol option. Hidden flags or changed help formats are possible; support is unknown."}
			return tool
		}
	}
	tool.Capabilities.Protocol = Finding{Available, "Expected native protocol options are advertised in CLI help; no protocol handshake or inference was performed."}
	if spec.id == "opencode" && advertises(output, "--service") {
		tool.Capabilities.Service.Detail = "CLI help advertises service mode. No service was started or contacted; readiness remains unknown."
	}
	if len(spec.policyHelp) > 0 {
		output, finding = probe(ctx, dir, name, spec.policyHelp)
		if finding.Status != Available {
			tool.Capabilities.Policy = finding
			return tool
		}
	}
	if len(spec.policyFlags) > 0 {
		for _, flag := range spec.policyFlags {
			if !advertises(output, flag) {
				return tool
			}
		}
		tool.Capabilities.Policy.Detail = "CLI help advertises permission/approval controls. Their read-only enforcement, inherited settings and startup integrations remain unverified."
	}
	return tool
}

// advertises recognizes a complete help option, not a substring of a different flag.
func advertises(output []byte, flag string) bool {
	return regexp.MustCompile(`(?m)(?:^|[\s,])` + regexp.QuoteMeta(flag) + `(?:[\s=,]|$)`).Match(output)
}

// probe runs a bounded noninteractive metadata command and withholds all raw errors.
func probe(ctx context.Context, dir, name string, args []string) ([]byte, Finding) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	result, err := process.Run(ctx, process.Command{
		Name: name, Args: args, Dir: dir, OutputLimit: 64 << 10, GracePeriod: 100 * time.Millisecond,
		Env: []string{"GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "TERM=dumb"},
	})
	if errors.Is(err, process.ErrOutputLimit) {
		return nil, Finding{Unknown, "Metadata output exceeded its limit. Raw output is withheld."}
	}
	if ctx.Err() != nil {
		return nil, Finding{Unknown, "Metadata check timed out or was cancelled. No readiness claim can be made."}
	}
	if err != nil {
		return nil, Finding{Unknown, "Metadata command failed. Check the external installation; raw diagnostics are withheld."}
	}
	return result.Stdout, Finding{Available, "Metadata command completed."}
}
