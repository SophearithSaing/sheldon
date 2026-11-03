package opencode

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// investigationRules preserves the spike's explicit deny-by-default session permission order.
func investigationRules() []rule {
	return []rule{{"*", "*", "deny"}, {"read", "*", "allow"}, {"glob", "*", "allow"}, {"grep", "*", "allow"}, {"read", "*.env", "deny"}, {"read", "*.env.*", "deny"}, {"external_directory", "*", "deny"}}
}

// investigationConfig preserves the audited startup controls without changing provider configuration.
func investigationConfig() map[string]any {
	return map[string]any{
		"formatter": false, "lsp": false, "snapshots": false, "warming": false,
		"policies": []rule{{"tool.use", "*", "deny"}, {"tool.use", "read:*", "allow"}, {"tool.use", "glob:*", "allow"}, {"tool.use", "grep:*", "allow"}, {"tool.use", "external_directory:*", "deny"}, {"integration.use", "mcp:*", "deny"}, {"integration.use", "skill:*", "deny"}},
	}
}

// Model is the effective native chat-model identity pinned when a session is created.
type Model struct {
	ProviderID string `json:"providerID"`
	ID         string `json:"id"`
	Variant    string `json:"variant,omitempty"`
}

type nativeSession struct {
	ID       string `json:"id"`
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
	Permissions []rule `json:"permissions"`
	Model       Model  `json:"model"`
}

// Session retains backend-only native identity, location and effective model without browser DTOs.
type Session struct {
	service *Service
	info    nativeSession
}

// Model returns the immutable effective model, not the current harness default.
func (s *Session) Model() Model { return s.info.Model }

// String withholds native session identities and source locations.
func (s *Session) String() string { return "restricted OpenCode session (native details withheld)" }

// GoString keeps diagnostic formatting free of native session/location details.
func (s *Session) GoString() string { return s.String() }

// CreateSession resolves the existing native default and pins it explicitly without fallback.
// The caller must first verify directory as the exact managed worktree using the Git source service.
func (s *Service) CreateSession(ctx context.Context, directory string) (*Session, error) {
	if err := s.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, ErrRestricted
	}
	if info, err := os.Lstat(directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrRestricted
	}
	var selected struct {
		Data Model `json:"data"`
	}
	if err := s.call(ctx, "GET", "/api/model/default?location[directory]="+url.QueryEscape(directory), nil, &selected); err != nil {
		return nil, errors.Join(ErrModelUnavailable, err)
	}
	if !validModel(selected.Data) {
		return nil, ErrModelUnavailable
	}
	var result struct {
		Data nativeSession `json:"data"`
	}
	if err := s.call(ctx, "POST", "/api/session", map[string]any{"title": "Sheldon pinned-source investigation", "agent": "build", "location": map[string]string{"directory": directory}, "permissions": investigationRules(), "model": selected.Data}, &result); err != nil {
		return nil, err
	}
	expectedModel := selected.Data
	if expectedModel.Variant == "" && result.Data.Model.Variant == "default" {
		expectedModel.Variant = "default"
	}
	if !validNativeID(result.Data.ID) || result.Data.Location.Directory != directory || result.Data.Model != expectedModel || !slices.Equal(result.Data.Permissions, investigationRules()) {
		s.cancel(ErrRestricted)
		return nil, ErrRestricted
	}
	s.sessionMu.Lock()
	if s.sessionIDs == nil {
		s.sessionIDs = make(map[string]bool)
	}
	duplicate := s.sessionIDs[result.Data.ID]
	s.sessionIDs[result.Data.ID] = true
	s.sessionMu.Unlock()
	if duplicate {
		s.cancel(ErrRestricted)
		return nil, ErrRestricted
	}
	session := &Session{service: s, info: result.Data}
	if err := s.ValidateSession(ctx, session); err != nil {
		return nil, err
	}
	for _, probe := range []rule{
		{"read", "__sheldon_policy_probe__.go", "allow"}, {"glob", "**/*.go", "allow"}, {"grep", "__sheldon_policy_probe__", "allow"},
		{"read", "secrets.env", "deny"}, {"read", "secrets.env.local", "deny"}, {"external_directory", "*", "deny"},
		{"edit", "*", "deny"}, {"shell", "*", "deny"}, {"execute", "*", "deny"}, {"subagent", "*", "deny"}, {"skill", "*", "deny"},
		{"question", "*", "deny"}, {"webfetch", "*", "deny"}, {"websearch", "*", "deny"}, {"unknown-plugin-action", "*", "deny"},
	} {
		var response struct {
			Data struct {
				Effect string `json:"effect"`
			} `json:"data"`
		}
		if err := s.call(ctx, "POST", "/api/session/"+url.PathEscape(session.info.ID)+"/permission", map[string]any{"action": probe.Action, "resources": []string{probe.Resource}}, &response); err != nil {
			s.cancel(ErrRestricted)
			return nil, err
		}
		if response.Data.Effect != probe.Effect {
			s.cancel(ErrRestricted)
			return nil, ErrRestricted
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return session, nil
}

// ValidateSession verifies exact native binding without consulting defaults or creating a replacement.
func (s *Service) ValidateSession(ctx context.Context, session *Session) error {
	if session == nil || session.service != s {
		return ErrRestricted
	}
	var result struct {
		Data nativeSession `json:"data"`
	}
	if err := s.call(ctx, "GET", "/api/session/"+url.PathEscape(session.info.ID), nil, &result); err != nil {
		return err
	}
	if result.Data.ID != session.info.ID || result.Data.Location != session.info.Location || result.Data.Model != session.info.Model || !slices.Equal(result.Data.Permissions, investigationRules()) {
		s.cancel(ErrRestricted)
		return ErrRestricted
	}
	return nil
}

// validNativeID admits only bounded opaque path-component identities, never arbitrary routes.
func validNativeID(value string) bool {
	if len(value) == 0 || len(value) > 200 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// validModel rejects unresolved identities and unsupported variants rather than guessing a model.
func validModel(model Model) bool {
	for _, value := range []string{model.ProviderID, model.ID} {
		if value == "" || len(value) > 200 || !utf8.ValidString(value) || strings.Contains(value, "://") || strings.ContainsFunc(value, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) {
			return false
		}
	}
	return model.Variant == "" || model.Variant == "default"
}
