package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInitialIsUnconfirmedAndDoesNotInventModels(t *testing.T) {
	p := Initial()
	if p.Validate() != nil || p.SetupConfirmed || p.DefaultHarness != "" || p.UpdatedAt != nil {
		t.Fatal("initial preferences selected or confirmed something")
	}
	if _, _, err := p.NewConversationChoice(); err == nil {
		t.Fatal("unconfirmed setup selected a harness")
	}
	data, err := json.Marshal(p)
	if err != nil || strings.Contains(string(data), `"model":`) || strings.Contains(string(data), `providerID`) {
		t.Fatalf("default selection should omit model references: %s", data)
	}
	p.Models[OpenCode] = ModelSelection{Mode: ExplicitModel, Model: ModelRef{ProviderID: "fixture-provider", ID: "fixture-a"}}
	if Initial().Models[OpenCode].Mode != HarnessDefault {
		t.Fatal("initial preferences share mutable map state")
	}
}

func TestPreferencesAffectNewConversationsNotPinnedBindings(t *testing.T) {
	p := Initial()
	p.SetupConfirmed, p.DefaultHarness = true, OpenCode
	p.Models[OpenCode] = ModelSelection{Mode: ExplicitModel, Model: ModelRef{ProviderID: "fixture-provider", ID: "fixture-a"}}
	h, selection, err := p.NewConversationChoice()
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{Harness: h, HarnessVersion: "fixture-1", Protocol: "fixture-native", NativeSessionID: "fixture-existing-session", WorkspaceID: "fixture-workspace", Requested: selection, Effective: selection.Model}
	p.Models[OpenCode] = ModelSelection{Mode: ExplicitModel, Model: ModelRef{ProviderID: "fixture-other-provider", ID: "fixture-b"}}
	p.Models[Codex] = ModelSelection{Mode: ExplicitModel, Model: ModelRef{ID: "fixture/codex-native-id"}}
	p.DefaultHarness = Codex
	newHarness, newSelection, err := p.NewConversationChoice()
	if err != nil || newHarness != Codex || newSelection.Model.ID != "fixture/codex-native-id" || newSelection.Model.ProviderID != "" {
		t.Fatal("native identifier was converted or harness choice was ignored")
	}
	if binding.Requested.Model.ID != "fixture-a" || binding.Effective.ProviderID != "fixture-provider" {
		t.Fatal("existing binding was mutated by preferences")
	}
	if binding.CheckContinuation(OpenCode, "fixture-existing-session", ModelRef{ProviderID: "fixture-provider", ID: "fixture-a"}) != nil {
		t.Fatal("pinned continuation consulted changed preferences")
	}
	for _, candidate := range []struct {
		harness Harness
		session string
		model   ModelRef
	}{
		{Codex, binding.NativeSessionID, binding.Effective},
		{OpenCode, "fixture-replacement-session", binding.Effective},
		{OpenCode, binding.NativeSessionID, ModelRef{ProviderID: "fixture-other-provider", ID: "fixture-a"}},
		{OpenCode, binding.NativeSessionID, ModelRef{ProviderID: "fixture-provider", ID: "fixture-b"}},
	} {
		if binding.CheckContinuation(candidate.harness, candidate.session, candidate.model) == nil {
			t.Fatal("native mismatch silently substituted a session/harness/model/provider")
		}
	}
}

func TestDefaultAndAliasResolutionAreSeparateFromEffectiveIdentity(t *testing.T) {
	for _, requested := range []ModelSelection{{Mode: HarnessDefault}, {Mode: ExplicitModel, Model: ModelRef{ID: "fixture-alias"}}} {
		binding := Binding{Harness: Claude, NativeSessionID: "fixture-session", Requested: requested, Effective: ModelRef{ID: "fixture-canonical-v1"}}
		if binding.CheckContinuation(Claude, "fixture-session", ModelRef{ID: "fixture-canonical-v1"}) != nil {
			t.Fatal("resolved default/alias was not preserved")
		}
		if binding.CheckContinuation(Claude, "fixture-session", ModelRef{ID: "fixture-canonical-v2"}) == nil {
			t.Fatal("a changed harness default replaced the pinned effective model")
		}
	}
}

func TestShapeValidationRejectsAmbiguityWithoutClaimingModelAvailability(t *testing.T) {
	for _, selection := range []ModelSelection{
		{Mode: "auto"},
		{Mode: HarnessDefault, Model: ModelRef{ID: "fixture-a"}},
		{Mode: ExplicitModel},
		{Mode: ExplicitModel, Model: ModelRef{ID: "fixture-provider/fixture-a"}}, // No guessed provider split.
		{Mode: ExplicitModel, Model: ModelRef{ProviderID: "fixture-provider", ID: "bad model name"}},
		{Mode: ExplicitModel, Model: ModelRef{ProviderID: "fixture-provider", ID: "https://user:secret@example.invalid"}},
	} {
		if selection.Validate(OpenCode) == nil {
			t.Fatalf("invalid selection accepted: %+v", selection)
		}
	}
	if (ModelSelection{Mode: ExplicitModel, Model: ModelRef{ProviderID: "fixture-provider", ID: "fixture-a"}}).Validate(Codex) == nil {
		t.Fatal("OpenCode model namespace was reused for Codex")
	}
	p := Initial()
	p.DefaultHarness = OpenCode
	c := Confirmation{SchemaVersion: SchemaVersion, Confirm: true, DefaultHarness: OpenCode, Models: p.Models}
	if c.Validate() != nil {
		t.Fatal("confirming a default preference required a Test answer or known effective model")
	}
	c.Confirm = false
	if c.Validate() == nil {
		t.Fatal("implicit confirmation accepted")
	}
	c.Confirm, c.SchemaVersion = true, 99
	if c.Validate() == nil {
		t.Fatal("unknown schema version accepted")
	}
	c.SchemaVersion, c.DefaultHarness = SchemaVersion, "unknown"
	if c.Validate() == nil {
		t.Fatal("unknown harness accepted")
	}
	delete(p.Models, OpenCode)
	if p.Validate() == nil {
		t.Fatal("missing selected harness model was silently defaulted")
	}
}

func TestModelAndTestContractsAreHonestAboutUnavailableCapabilities(t *testing.T) {
	state := Scaffold()
	if state.Persistence.Status != "unsupported" || state.TestAnswer.Status != "unsupported" {
		t.Fatal("scaffold claimed save or test support")
	}
	for _, h := range []Harness{OpenCode, Codex, Claude} {
		models := Undiscovered(h)
		if models.Harness != h || models.Catalog.Status != "unknown" || models.ConfiguredDefault.Status != "unknown" || models.DefaultModel != nil || models.Models == nil || len(models.Models) != 0 || models.ExplicitSelection.Status != "unsupported" {
			t.Fatal("invented model/default or override capability")
		}
	}
	r := TestRequest{ClientRequestID: "fixture-request", Harness: Codex, Selection: ModelSelection{Mode: HarnessDefault}, ConfirmQuota: true}
	if r.Validate() != nil {
		t.Fatal("valid independent test request rejected")
	}
	r.ConfirmQuota = false
	if r.Validate() == nil {
		t.Fatal("test did not require quota consent")
	}
	r.ConfirmQuota, r.ClientRequestID = true, ""
	if r.Validate() == nil {
		t.Fatal("test did not require request identity")
	}
}
