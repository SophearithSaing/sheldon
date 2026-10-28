// Package settings defines local preference and native-model binding contracts.
// Persistence and production model/test adapters are deliberately not implemented here.
package settings

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/SophearithSaing/sheldon/internal/environment"
)

const SchemaVersion = 1

// Harness identifies an existing native CLI, not a provider or billing account.
type Harness string

const (
	OpenCode Harness = "opencode"
	Codex    Harness = "codex"
	Claude   Harness = "claude"
)

// Valid reports whether the harness has a defined Sheldon contract.
func (h Harness) Valid() bool { return h == OpenCode || h == Codex || h == Claude }

// ModelRef retains native identifier components without interpreting a slash as a
// universal provider separator. OpenCode uses providerID + id; the other CLIs use id.
type ModelRef struct {
	ProviderID string `json:"providerID,omitempty"`
	ID         string `json:"id"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,199}$`)
var provider = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Validate checks identifier shape only, not model availability, login or entitlement.
func (m ModelRef) Validate(h Harness) error {
	if !h.Valid() || !identifier.MatchString(m.ID) || strings.Contains(m.ID, "://") {
		return errors.New("invalid native model identifier")
	}
	if (h == OpenCode && !provider.MatchString(m.ProviderID)) || (h != OpenCode && m.ProviderID != "") {
		return errors.New("invalid native provider identifier for this harness")
	}
	return nil
}

// ModelSelection is a preference for a new session, never its effective model.
// Value fields avoid sharing mutable model references with existing bindings.
type ModelSelection struct {
	Mode  SelectionMode `json:"mode"`
	Model ModelRef      `json:"model,omitzero"`
}

// SelectionMode distinguishes an unresolved harness default from an explicit native choice.
type SelectionMode string

const (
	HarnessDefault SelectionMode = "default"
	ExplicitModel  SelectionMode = "explicit"
)

// Validate rejects contradictory default/explicit selections without guessing a model.
func (s ModelSelection) Validate(h Harness) error {
	if !h.Valid() {
		return errors.New("invalid harness")
	}
	switch s.Mode {
	case HarnessDefault:
		if s.Model != (ModelRef{}) {
			return errors.New("harness-default selection must not include a model")
		}
		return nil
	case ExplicitModel:
		return s.Model.Validate(h)
	default:
		return errors.New("invalid model-selection mode")
	}
}

// Preferences is the versioned, secret-free record intended for durable local storage.
// UpdatedAt is assigned by a future store, not accepted from a confirmation request.
type Preferences struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	SetupConfirmed bool                       `json:"setupConfirmed"`
	DefaultHarness Harness                    `json:"defaultHarness"`
	Models         map[Harness]ModelSelection `json:"models"`
	UpdatedAt      *time.Time                 `json:"updatedAt,omitempty"`
}

// Initial returns unconfirmed defaults without selecting a harness or inventing a model.
func Initial() Preferences {
	return Preferences{SchemaVersion: SchemaVersion, Models: map[Harness]ModelSelection{
		OpenCode: {Mode: HarnessDefault}, Codex: {Mode: HarnessDefault}, Claude: {Mode: HarnessDefault},
	}}
}

// Validate checks the complete record; native support must be checked separately.
func (p Preferences) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return errors.New("unsupported preferences schema version")
	}
	if p.DefaultHarness != "" && !p.DefaultHarness.Valid() {
		return errors.New("invalid default harness")
	}
	if p.SetupConfirmed && !p.DefaultHarness.Valid() {
		return errors.New("confirmed preferences require a harness")
	}
	for h, selection := range p.Models {
		if err := selection.Validate(h); err != nil {
			return err
		}
	}
	if p.DefaultHarness.Valid() {
		if _, ok := p.Models[p.DefaultHarness]; !ok {
			return errors.New("selected harness requires a model selection")
		}
	}
	return nil
}

// NewConversationChoice snapshots a confirmed preference by value. It does not create
// a session or resolve the harness default; adapters must verify the effective model.
func (p Preferences) NewConversationChoice() (Harness, ModelSelection, error) {
	if err := p.Validate(); err != nil {
		return "", ModelSelection{}, err
	}
	if !p.SetupConfirmed {
		return "", ModelSelection{}, errors.New("setup is not confirmed")
	}
	return p.DefaultHarness, p.Models[p.DefaultHarness], nil
}

// Binding pins native identity and the resolved model independently of later preferences.
// An explicit native alias may resolve to a canonical Effective identifier at creation.
type Binding struct {
	Harness         Harness        `json:"harness"`
	HarnessVersion  string         `json:"harnessVersion"`
	Protocol        string         `json:"protocol"`
	NativeSessionID string         `json:"nativeSessionID"`
	WorkspaceID     string         `json:"workspaceID"`
	Requested       ModelSelection `json:"requested"`
	Effective       ModelRef       `json:"effective"`
}

// CheckContinuation rejects native session/model changes instead of consulting new
// preferences, switching providers or silently replacing a conversation.
func (b Binding) CheckContinuation(h Harness, sessionID string, effective ModelRef) error {
	if b.Requested.Validate(b.Harness) != nil || b.Effective.Validate(b.Harness) != nil ||
		b.NativeSessionID == "" || h != b.Harness || sessionID != b.NativeSessionID || effective != b.Effective {
		return errors.New("native binding mismatch; do not substitute a session, harness or model")
	}
	return nil
}

// Confirmation accepts only user-owned preference fields and explicit confirmation.
type Confirmation struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	Confirm        bool                       `json:"confirm"`
	DefaultHarness Harness                    `json:"defaultHarness"`
	Models         map[Harness]ModelSelection `json:"models"`
}

// Validate requires explicit confirmation but does not require a quota-consuming test.
func (c Confirmation) Validate() error {
	if !c.Confirm {
		return errors.New("explicit setup confirmation is required")
	}
	return (Preferences{SchemaVersion: c.SchemaVersion, SetupConfirmed: true, DefaultHarness: c.DefaultHarness, Models: c.Models}).Validate()
}

// State exposes the scaffold's actual capabilities rather than simulated saved settings.
type State struct {
	Preferences Preferences         `json:"preferences"`
	Persistence environment.Finding `json:"persistence"`
	TestAnswer  environment.Finding `json:"testAnswer"`
}

// Scaffold returns unconfirmed defaults with persistence and inference explicitly unavailable.
func Scaffold() State {
	return State{Preferences: Initial(),
		Persistence: environment.Finding{Status: environment.Unsupported, Detail: "Saving preferences is not implemented yet. Drafts are not persisted or applied to native sessions."},
		TestAnswer:  environment.Finding{Status: environment.Unsupported, Detail: "Real test answers and cancellation require a production adapter; no model request will be sent."},
	}
}

// ModelDiscovery separates an unknown catalog/default from an empty supported catalog.
type ModelDiscovery struct {
	Harness           Harness             `json:"harness"`
	Catalog           environment.Finding `json:"catalog"`
	ConfiguredDefault environment.Finding `json:"configuredDefault"`
	DefaultModel      *ModelRef           `json:"defaultModel,omitempty"`
	Models            []ModelRef          `json:"models"`
	ExplicitSelection environment.Finding `json:"explicitSelection"`
}

// Undiscovered reports missing native discovery capability without starting a service
// or looking into harness configuration, credentials or provider APIs.
func Undiscovered(h Harness) ModelDiscovery {
	return ModelDiscovery{Harness: h, Models: []ModelRef{},
		Catalog:           environment.Finding{Status: environment.Unknown, Detail: "Safe native model discovery is not implemented. An empty list does not mean no models are available."},
		ConfiguredDefault: environment.Finding{Status: environment.Unknown, Detail: "The harness's configured/default model has not been read or resolved."},
		ExplicitSelection: environment.Finding{Status: environment.Unsupported, Detail: "Explicit native model selection is not validated for a production adapter yet."},
	}
}

// TestRequest describes an explicitly authorized independent fixture session. It
// accepts no prompt, PR context, credentials, provider configuration or existing session ID.
type TestRequest struct {
	ClientRequestID string         `json:"clientRequestID"`
	Harness         Harness        `json:"harness"`
	Selection       ModelSelection `json:"selection"`
	ConfirmQuota    bool           `json:"confirmQuota"`
}

var requestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Validate requires a stable request identity and separate quota acknowledgement.
func (r TestRequest) Validate() error {
	if !requestID.MatchString(r.ClientRequestID) || !r.ConfirmQuota {
		return errors.New("test answer requires request identity and quota confirmation")
	}
	return r.Selection.Validate(r.Harness)
}
