package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

func boolPtr(b bool) *bool { return &b }

// T728: the emit() gate — description text only when the switch is on.
func TestPromptDescription_Gate(t *testing.T) {
	cases := []struct {
		name string
		p    agent.Profile
		want string
	}{
		{"on + desc", agent.Profile{Description: "hi", IncludeDescriptionInSystemPrompt: true}, "hi"},
		{"on + trims", agent.Profile{Description: "  hi  ", IncludeDescriptionInSystemPrompt: true}, "hi"},
		{"off + desc", agent.Profile{Description: "hi", IncludeDescriptionInSystemPrompt: false}, ""},
		{"on + empty desc", agent.Profile{Description: "  ", IncludeDescriptionInSystemPrompt: true}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := promptDescription(c.p); got != c.want {
				t.Fatalf("promptDescription = %q, want %q", got, c.want)
			}
		})
	}
}

// T728: CreateAgent defaults the switch to true (nil), honours an explicit false.
func TestCreateAgent_IncludeDescriptionDefaultAndExplicit(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		in   *bool
		want bool
	}{
		{"nil defaults true", nil, true},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.seedWorker(t, testWorker, testOrg)
			id, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
				OrganizationID: testOrg, Name: "coder", Description: "d", Model: "claude",
				CLI: "claude-code", WorkerID: testWorker, CreatedBy: "user:hayang",
				IncludeDescriptionInSystemPrompt: c.in,
			})
			if err != nil {
				t.Fatalf("CreateAgent: %v", err)
			}
			a, err := f.svc.GetAgent(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if a.Profile().IncludeDescriptionInSystemPrompt != c.want {
				t.Fatalf("IncludeDescriptionInSystemPrompt = %v, want %v", a.Profile().IncludeDescriptionInSystemPrompt, c.want)
			}
		})
	}
}

// T728: UpdateAgentConfig preserves the switch when the field is omitted (nil), and
// flips it when explicitly sent.
func TestUpdateAgentConfig_IncludeDescriptionPreserveAndOverride(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seedWorker(t, testWorker, testOrg)
	// Create opted-out so we can watch preserve (still false) then override → true.
	id, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
		OrganizationID: testOrg, Name: "coder", Description: "d", Model: "claude",
		CLI: "claude-code", WorkerID: testWorker, CreatedBy: "user:hayang",
		IncludeDescriptionInSystemPrompt: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	base := UpdateAgentConfigCommand{Model: "claude", CLI: "claude-code"}

	// nil → preserve (stays false).
	if err := f.svc.UpdateAgentConfig(ctx, id, base); err != nil {
		t.Fatalf("UpdateAgentConfig(nil): %v", err)
	}
	if a, _ := f.svc.GetAgent(ctx, id); a.Profile().IncludeDescriptionInSystemPrompt {
		t.Fatal("nil PATCH must preserve the existing false, but it flipped to true")
	}
	// explicit true → override.
	cmd := base
	cmd.IncludeDescriptionInSystemPrompt = boolPtr(true)
	if err := f.svc.UpdateAgentConfig(ctx, id, cmd); err != nil {
		t.Fatalf("UpdateAgentConfig(true): %v", err)
	}
	if a, _ := f.svc.GetAgent(ctx, id); !a.Profile().IncludeDescriptionInSystemPrompt {
		t.Fatal("explicit true PATCH must override to true")
	}
}

// T728: the lifecycle_changed event carries the gated prompt_description so the
// projector/daemon can inject it — description text when on, "" when off.
func TestEmit_CarriesPromptDescription(t *testing.T) {
	ctx := context.Background()
	extract := func(t *testing.T, include *bool) string {
		t.Helper()
		f := newFixture(t)
		f.seedWorker(t, testWorker, testOrg)
		id, err := f.svc.CreateAgent(ctx, CreateAgentCommand{
			OrganizationID: testOrg, Name: "coder", Description: "persona text", Model: "claude",
			CLI: "claude-code", WorkerID: testWorker, CreatedBy: "user:hayang",
			IncludeDescriptionInSystemPrompt: include,
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
		if err := f.svc.StartAgent(ctx, id); err != nil {
			t.Fatalf("StartAgent: %v", err)
		}
		evs := f.outboxEvents(t)
		last := evs[len(evs)-1]
		if last.EventType != EvtAgentLifecycleChanged {
			t.Fatalf("last event = %s, want %s", last.EventType, EvtAgentLifecycleChanged)
		}
		var pl struct {
			PromptDescription string `json:"prompt_description"`
		}
		if err := json.Unmarshal([]byte(last.Payload), &pl); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		return pl.PromptDescription
	}

	if got := extract(t, boolPtr(true)); got != "persona text" {
		t.Fatalf("on: prompt_description = %q, want %q", got, "persona text")
	}
	if got := extract(t, boolPtr(false)); got != "" {
		t.Fatalf("off: prompt_description = %q, want empty", got)
	}
}
