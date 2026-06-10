package cli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// These tests guard the PRODUCTION outbox-relay wiring (App.outboxProjectors) — the
// single source runWebConsole uses. They exist because of the #266 P1 headline
// blocker: PlanParticipantProjector consumes pm.plan.created → creates the Plan's
// 1:1 conversation + binds conversation_id, but it was wired ONLY in service/handler
// tests (which hand-build their own relay) and NEVER registered in the production
// relay → in the real server a Plan created via HTTP got no conversation →
// `advance` 500 "no conversation to dispatch into" → the whole headline (advance →
// @mention → wake agent) was dead. A hand-wired relay can never catch that; these
// run against App.outboxProjectors.

// productionRelay builds the relay exactly as runWebConsole does (same deps, same
// single-source projector list), returning the outbox repo so the test can emit.
func productionRelay(t *testing.T, app *App) (*outbox.Relay, *outboxsql.OutboxRepo) {
	t.Helper()
	outboxRepo := outboxsql.NewOutboxRepo(app.DB)
	appliedRepo := outboxsql.NewAppliedRepo(app.DB)
	controlLog := environment.NewControlLog(envsql.NewControlEventRepo(app.DB), app.IDGen, app.Clock)
	projectors, _ := app.outboxProjectors(outboxRepo, appliedRepo, controlLog)
	return outbox.NewRelay(outboxRepo, appliedRepo, app.Clock, projectors...), outboxRepo
}

// TestOutboxProjectors_RegistersEventConsumers is the deterministic class-guard:
// every outbox event emitted by a Service MUST have its consuming projector
// registered in the production relay list. Dropping any (as #266 dropped the
// plan-participant projector) makes this FAIL — cheaply, in CI, with no async.
func TestOutboxProjectors_RegistersEventConsumers(t *testing.T) {
	app := newTestApp(t)
	outboxRepo := outboxsql.NewOutboxRepo(app.DB)
	appliedRepo := outboxsql.NewAppliedRepo(app.DB)
	controlLog := environment.NewControlLog(envsql.NewControlEventRepo(app.DB), app.IDGen, app.Clock)
	projectors, _ := app.outboxProjectors(outboxRepo, appliedRepo, controlLog)

	got := map[string]bool{}
	for _, p := range projectors {
		got[p.Name()] = true
	}
	// The consumers the production relay must register. pm-plan-participant-sync is
	// the #266 regression: it consumes pm.plan.created → binds the Plan conversation.
	for _, required := range []string{
		"pm-plan-participant-sync", // #266 — was missing → headline dead
		"pm-participant-sync",
		"pm-workitem-sync",
		"pm-task-status-sync",
		"env-agent-control",
		"conv-agent-wake",
	} {
		if !got[required] {
			t.Errorf("App.outboxProjectors() does not register %q — the outbox events it consumes would have no consumer in the production relay (this is exactly how #266 broke the Plan headline)", required)
		}
	}
}

// TestProductionRelay_PlanCreated_BindsConversation is the behavioral guard: a
// pm.plan.created event drained through the PRODUCTION relay must create the
// Plan's 1:1 conversation and bind conversation_id back onto the Plan. Before the
// #266 fix this stayed "" (no consumer); after, it binds.
func TestProductionRelay_PlanCreated_BindsConversation(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	planRepo := pmsql.NewPlanRepo(app.DB)

	plan, err := pm.NewPlan(pm.NewPlanInput{
		ID:         pm.PlanID("plan-reltest"),
		ProjectID:  pm.ProjectID("proj-reltest"),
		Name:       "v3.0",
		CreatorRef: pm.IdentityRef("user:reltester"),
		CreatedAt:  app.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	if err := planRepo.Save(ctx, plan); err != nil {
		t.Fatalf("Save plan: %v", err)
	}
	if plan.ConversationID() != "" {
		t.Fatalf("precondition: new Plan should have empty conversation_id, got %q", plan.ConversationID())
	}

	payload, _ := json.Marshal(map[string]any{
		"plan_id":         "plan-reltest",
		"project_id":      "proj-reltest",
		"organization_id": "org-reltest",
		"owner_ref":       "pm://plans/plan-reltest",
		"creator_ref":     "user:reltester",
		"participants":    []string{"user:reltester"},
	})

	relay, outboxRepo := productionRelay(t, app)
	if err := outboxRepo.Append(ctx, outbox.Event{
		ID:        app.IDGen.NewULID(),
		EventType: "pm.plan.created",
		Refs:      `{"plan_id":"plan-reltest","project_id":"proj-reltest"}`,
		Payload:   string(payload),
	}); err != nil {
		t.Fatalf("append pm.plan.created: %v", err)
	}

	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatalf("relay.RunOnce: %v", err)
	}

	bound, err := planRepo.FindByID(ctx, pm.PlanID("plan-reltest"))
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if bound.ConversationID() == "" {
		t.Fatalf("production relay did not bind conversation_id after pm.plan.created — the PlanParticipantProjector is not consuming it in the production list (#266 regression)")
	}
}
