package airuntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type catalogRepoStub struct {
	cat Catalog
}

func (r *catalogRepoStub) GetCatalog(context.Context, string) (Catalog, error) {
	return r.cat, nil
}

func (r *catalogRepoStub) CreateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error) {
	panic("unused")
}

func (r *catalogRepoStub) UpdateCLI(_ context.Context, cli CLIDefinition, _ int64, _ AuditEvent) (int64, error) {
	for i := range r.cat.CLIs {
		if r.cat.CLIs[i].ID == cli.ID {
			r.cat.CLIs[i] = cli
			return r.cat.Revision + 1, nil
		}
	}
	return 0, ErrNotFound
}

func (r *catalogRepoStub) CreateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error) {
	panic("unused")
}

func (r *catalogRepoStub) UpdateModel(_ context.Context, model ModelDefinition, _ int64, _ AuditEvent) (int64, error) {
	for i := range r.cat.Models {
		if r.cat.Models[i].ID == model.ID {
			r.cat.Models[i] = model
			return r.cat.Revision + 1, nil
		}
	}
	return 0, ErrNotFound
}

func (r *catalogRepoStub) CreateProfile(context.Context, RuntimeProfile, int64, AuditEvent) (int64, error) {
	panic("unused")
}

func (r *catalogRepoStub) UpdateProfile(context.Context, RuntimeProfile, int64, AuditEvent) (int64, error) {
	panic("unused")
}

func (r *catalogRepoStub) SetDefaultProfile(context.Context, string, string, int64, AuditEvent) (int64, error) {
	panic("unused")
}

func TestUpdateCLICascadeRejectsEnabledProfileInvalidatedBySchema(t *testing.T) {
	oldSchema := json.RawMessage(`{"type":"object","properties":{"temperature":{"type":"number"}},"additionalProperties":false}`)
	repo := &catalogRepoStub{cat: Catalog{
		OrgID:    "org",
		Revision: 7,
		CLIs: []CLIDefinition{{
			ID: "cli-1", OrgID: "org", Key: "codex", DisplayName: "Codex", Executable: "codex",
			ParameterSchema: oldSchema, Enabled: true,
		}},
		Models: []ModelDefinition{{
			ID: "model-1", OrgID: "org", Key: "gpt-5", ModelKey: "gpt-5",
			CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{"temperature": 0.7}, Enabled: true,
		}},
		Profiles: []RuntimeProfile{
			{ID: "profile-1", OrgID: "org", Key: "coding", Name: "Coding", CLIKey: "codex", ModelKey: "gpt-5", Parameters: map[string]any{}, Enabled: true},
			{ID: "profile-2", OrgID: "org", Key: "disabled", Name: "Disabled", CLIKey: "codex", ModelKey: "gpt-5", Parameters: map[string]any{"temperature": 2.0}, Enabled: false},
		},
	}}
	svc := NewService(repo, func() string { return "audit-1" })
	updated := repo.cat.CLIs[0]
	updated.ParameterSchema = json.RawMessage(`{"type":"object","properties":{"temperature":{"type":"number","maximum":0.5}},"additionalProperties":false}`)

	_, _, err := svc.UpdateCLI(context.Background(), "org", "user:owner", 7, updated)
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != ReasonParametersInvalid {
		t.Fatalf("UpdateCLI error = %v, want %s", err, ReasonParametersInvalid)
	}
	if runtimeErr.Details["profile_key"] != "coding" {
		t.Fatalf("diagnostic details = %#v, want profile_key=coding", runtimeErr.Details)
	}
}

func TestUpdateModelCascadeRejectsEnabledProfileInvalidatedByCompatibility(t *testing.T) {
	repo := &catalogRepoStub{cat: Catalog{
		OrgID:    "org",
		Revision: 9,
		CLIs: []CLIDefinition{
			{ID: "cli-1", OrgID: "org", Key: "codex", DisplayName: "Codex", Executable: "codex", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true},
			{ID: "cli-2", OrgID: "org", Key: "claude-code", DisplayName: "Claude Code", Executable: "claude", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true},
		},
		Models: []ModelDefinition{{
			ID: "model-1", OrgID: "org", Key: "gpt-5", ModelKey: "gpt-5",
			CompatibleCLIKeys: []string{"codex", "claude-code"}, DefaultParameters: map[string]any{}, Enabled: true,
		}},
		Profiles: []RuntimeProfile{
			{ID: "profile-1", OrgID: "org", Key: "coding", Name: "Coding", CLIKey: "codex", ModelKey: "gpt-5", Parameters: map[string]any{}, Enabled: true},
			{ID: "profile-2", OrgID: "org", Key: "disabled", Name: "Disabled", CLIKey: "codex", ModelKey: "gpt-5", Parameters: map[string]any{}, Enabled: false},
		},
	}}
	svc := NewService(repo, func() string { return "audit-1" })
	updated := repo.cat.Models[0]
	updated.CompatibleCLIKeys = []string{"claude-code"}

	_, _, err := svc.UpdateModel(context.Background(), "org", "user:owner", 9, updated)
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != ReasonIncompatible {
		t.Fatalf("UpdateModel error = %v, want %s", err, ReasonIncompatible)
	}
	if runtimeErr.Details["profile_key"] != "coding" {
		t.Fatalf("diagnostic details = %#v, want profile_key=coding", runtimeErr.Details)
	}
}
