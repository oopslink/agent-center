package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// TestProductionRelay_PlanDeleted_DeletesConversation is the behavioral relay-guard
// for EvtPlanDeleted (PD-requested, completing the #266-pattern family alongside
// TestProductionRelay_AutoAdvance_OnTaskDone + _Dispatch_DeliversWorkToAgent).
//
// The registration class-guard (TestOutboxProjectors_RegistersEventConsumers) proves
// the PlanParticipantProjector is REGISTERED in the production relay, but NOT that its
// switch still HANDLES EvtPlanDeleted — drop the EvtPlanDeleted branch from the
// projector and the name-guard still passes while the plan conversation leaks. This
// drives DeletePlan → EvtPlanDeleted through the PRODUCTION relay (App.outboxProjectors)
// and asserts the plan's 1:1 conversation is actually hard-deleted.
func TestProductionRelay_PlanDeleted_DeletesConversation(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	svc := app.PMService
	planRepo := pmsql.NewPlanRepo(app.DB)
	convRepo := convsql.NewConversationRepo(app.DB)

	pid, err := svc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: "org-rel", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	relay, _ := productionRelay(t, app)
	planID, err := svc.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "del", CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	// Drain → the Plan's 1:1 conversation is created + bound (EvtPlanCreated → projector).
	drainRelay(t, relay)

	plan, err := planRepo.FindByID(ctx, planID)
	if err != nil {
		t.Fatalf("FindByID plan: %v", err)
	}
	convID := plan.ConversationID()
	if convID == "" {
		t.Fatalf("precondition: plan conversation not bound after drain")
	}
	if _, err := convRepo.FindByID(ctx, conversation.ConversationID(convID)); err != nil {
		t.Fatalf("precondition: conversation %s should exist before delete, got %v", convID, err)
	}

	// DeletePlan → emits EvtPlanDeleted → the PRODUCTION relay's PlanParticipantProjector
	// must hard-delete the conversation. If the projector's EvtPlanDeleted branch is
	// dropped (the registration class-guard still passes), this FAILS.
	if err := svc.DeletePlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	drainRelay(t, relay)

	if _, err := convRepo.FindByID(ctx, conversation.ConversationID(convID)); !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("EvtPlanDeleted did NOT hard-delete the conversation via the production relay (projector not handling EvtPlanDeleted?) — FindByID err=%v, want ErrConversationNotFound", err)
	}
}
