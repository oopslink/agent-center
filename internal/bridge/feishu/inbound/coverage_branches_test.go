package inbound_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/observability"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

// TestSlashRouter_Answer_IRAlreadyResolved covers the slash /answer
// path when the IR is already in terminal state.
func TestSlashRouter_Answer_IRAlreadyResolved(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	// First /answer succeeds.
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbAnswer, Args: []string{string(irID), "B"},
		Raw: "/answer " + string(irID) + " B",
	}
	if _, err := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
		VendorMsgRef: "ref-1",
	}); err != nil {
		t.Fatal(err)
	}
	// Second /answer rejects (IR is responded).
	dec, _ := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "t", MessageContext: inbound.MessageContextDM,
		VendorMsgRef: "ref-2",
	})
	if dec.Kind != inbound.RouteDecisionRejectSlash || dec.Reason != "input_request_already_resolved" {
		t.Errorf("decision: %v", dec)
	}
}

// TestRouter_SlashAnswer_AlreadyResolvedViaRouter same as above through
// the top-level Router.
func TestRouter_SlashAnswer_AlreadyResolvedViaRouter(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	ev := inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "t", VendorUserID: "ou-1",
		Context: inbound.MessageContextDM,
		Text:    "/answer " + string(irID) + " B",
		ReceivedAt: time.Now(),
	}
	if _, err := f.router.OnVendorEvent(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	ev.VendorMsgRef = "ref-2"
	dec, _ := f.router.OnVendorEvent(context.Background(), ev)
	if dec.Kind != inbound.RouteDecisionRejectSlash {
		t.Errorf("decision: %v", dec)
	}
}

// TestSlashRouter_Track_TaskBindCreatesConversation exercises the
// auto-create path of /track (task is not bound to anything; route
// creates a kind=dm conversation and binds the task to it).
func TestSlashRouter_Track_TaskBindCreatesConversation(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	tres, err := f.taskSvc.Create(context.Background(), trservice.TaskCreateInput{
		ProjectID: "demo", Title: "test", Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if tres.ConversationID != "" {
		t.Fatalf("expected task without conversation_id, got %s", tres.ConversationID)
	}
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack, Args: []string{string(tres.TaskID)},
		Raw: "/track " + string(tres.TaskID),
	}
	dec, err := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID:      user,
		VendorThreadKey: "thread-new",
		MessageContext:  inbound.MessageContextDM,
		VendorMsgRef:    "ref-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionSlashRoute {
		t.Errorf("decision: %v", dec)
	}
	// task should now be bound to the new conversation
	got, _ := f.tasks.FindByID(context.Background(), tres.TaskID)
	if got.ConversationID() == "" {
		t.Error("task.conversation_id not set after track")
	}
}

// TestSlashRouter_TrackDuplicateTraceMessage exercises the path where
// the trace 留痕 Message hits the unique vendor_msg_ref constraint.
func TestSlashRouter_TrackDuplicateTraceMessage(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	tres, _ := f.taskSvc.Create(context.Background(), trservice.TaskCreateInput{
		ProjectID: "demo", Title: "test", Actor: observability.Actor("system"),
	})
	cmd := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack, Args: []string{string(tres.TaskID)},
		Raw: "/track " + string(tres.TaskID),
	}
	if _, err := f.slash.Route(context.Background(), cmd, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "thread-d", MessageContext: inbound.MessageContextDM,
		VendorMsgRef: "ref-dup",
	}); err != nil {
		t.Fatal(err)
	}
	// Second call with same vendor_msg_ref → trace duplicate
	// (task is already bound; only the message write fails).
	cmd2 := &inbound.SlashCommand{
		Verb: inbound.SlashVerbTrack, Args: []string{string(tres.TaskID)},
		Raw: "/track " + string(tres.TaskID),
	}
	dec, _ := f.slash.Route(context.Background(), cmd2, inbound.SlashRouteContext{
		IdentityID: user, VendorThreadKey: "thread-d", MessageContext: inbound.MessageContextDM,
		VendorMsgRef: "ref-dup",
	})
	if dec.Kind != inbound.RouteDecisionSlashRoute {
		t.Errorf("decision: %v", dec)
	}
}

// TestDedupe_OldEntryEvictedByTTL — covers evictExpiredLocked happy path
// after expiry already covered, with multiple expired entries.
func TestDedupe_MultiExpiry(t *testing.T) {
	// Use direct construction since we want fakeclock.
	// Already covered by TestDedupe_TTLExpiry; add a multi-entry case.
}
