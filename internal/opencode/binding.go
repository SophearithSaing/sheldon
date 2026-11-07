package opencode

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// BindingRecord is the private saved identity of an exact restricted native session.
// It contains no service credentials and does not itself grant permission to resume.
type BindingRecord struct {
	SessionID string `json:"sessionID"`
	Directory string `json:"directory"`
	Model     Model  `json:"model"`
	Version   string `json:"version"`
	Protocol  string `json:"protocol"`
	Policy    int    `json:"policy"`
}

// Binding returns the private immutable identity of this already-validated native session.
func (s *Session) Binding() BindingRecord {
	policy := 1
	if s.service.semantic {
		policy = 2
	}
	return BindingRecord{SessionID: s.info.ID, Directory: s.info.Location.Directory, Model: s.info.Model, Version: SupportedVersion, Protocol: "sheldon-opencode-http-v1", Policy: policy}
}

// Validate checks current adapter compatibility without contacting a native service.
func (b BindingRecord) Validate() error {
	if b.ValidateSaved() != nil || !validModel(b.Model) || b.Version != SupportedVersion || b.Protocol != "sheldon-opencode-http-v1" || b.Policy != 1 {
		return errors.New("invalid or unsupported saved native binding")
	}
	return nil
}

// ValidateSaved checks bounded private record shape while keeping incompatible sessions' history readable.
func (b BindingRecord) ValidateSaved() error {
	model := b.Model
	model.Variant = ""
	if !validNativeID(b.SessionID) || len(b.Directory) > 4096 || !utf8.ValidString(b.Directory) || strings.ContainsRune(b.Directory, 0) || !filepath.IsAbs(b.Directory) || filepath.Clean(b.Directory) != b.Directory || !validModel(model) || b.Model.Variant != "" && !validNativeID(b.Model.Variant) || b.Version == "" || len(b.Version) > 64 || !utf8.ValidString(b.Version) || strings.ContainsRune(b.Version, 0) || !validNativeID(b.Protocol) || b.Policy <= 0 {
		return errors.New("invalid saved native binding")
	}
	return nil
}

// String keeps native identities and host paths out of ordinary diagnostics.
func (b BindingRecord) String() string {
	return "saved restricted native binding (private details withheld)"
}

// GoString keeps debug formatting private as well.
func (b BindingRecord) GoString() string { return b.String() }
