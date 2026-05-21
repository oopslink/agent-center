package inbound_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

// TestRouter_CardCallback_AlreadyRespondedSilent exercises the silent
// ack branch of the card-callback path via the top-level Router.
func TestRouter_CardCallback_AlreadyRespondedSilent(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	// First click resolves the IR.
	ev := inbound.VendorEvent{
		Kind:         inbound.VendorEventCardActionTrigger,
		VendorMsgRef: "card-ref-1",
		VendorUserID: "ou-1",
		CardAction: inbound.CardActionEvent{
			CardMessageID: "om-1",
			ActionValue: map[string]any{
				"action":           "input_request_respond",
				"input_request_id": string(irID),
				"option_text":      "A",
			},
		},
		ReceivedAt: time.Now(),
	}
	if _, err := f.router.OnVendorEvent(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	// Second click — different vendor_msg_ref to bypass dedupe.
	ev.VendorMsgRef = "card-ref-2"
	dec, err := f.router.OnVendorEvent(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionCardCallback || dec.Reason != "already_responded" {
		t.Errorf("decision: %v", dec)
	}
}

// TestRouter_SlashAnswerHappyViaRouter exercises the routeAnswer happy
// path through the top-level Router with a real pending IR.
func TestRouter_SlashAnswerHappyViaRouter(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	_, _, irID, _ := f.seedTaskWithIR(t)
	dec, err := f.router.OnVendorEvent(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "thread",
		VendorUserID: "ou-1", Context: inbound.MessageContextDM,
		Text: "/answer " + string(irID) + " B",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionSlashRoute {
		t.Errorf("decision: %v", dec)
	}
}

// TestRouter_SlashTrackHappyViaRouter exercises the routeTrack happy
// path with a fresh task through the top-level Router.
func TestRouter_SlashTrackHappyViaRouter(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	tres, err := f.taskSvc.Create(context.Background(), trservice.TaskCreateInput{
		ProjectID: "demo", Title: "test", Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := f.router.OnVendorEvent(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "thread-track",
		VendorUserID: "ou-1", Context: inbound.MessageContextDM,
		Text: "/track " + string(tres.TaskID),
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionSlashRoute {
		t.Errorf("decision: %v", dec)
	}
}

// TestRouter_SlashDispatchDeferredViaRouter exercises the /dispatch
// reject path through the top-level Router.
func TestRouter_SlashDispatchDeferredViaRouter(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "hayang")
	dec, err := f.router.OnVendorEvent(context.Background(), inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef: "ref-1", VendorThreadKey: "thread",
		VendorUserID: "ou-1", Context: inbound.MessageContextDM,
		Text: "/dispatch project=p1",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Kind != inbound.RouteDecisionRejectSlash || dec.Reason != "feature_deferred" {
		t.Errorf("decision: %v", dec)
	}
}

// TestResolver_PreboundIdentityPrefersBindingLookup ensures the
// resolver short-circuits when a binding already exists.
func TestResolver_PreboundIdentityPrefersBindingLookup(t *testing.T) {
	f := newFixture(t)
	user := f.seedUser(t, "hayang")
	if _, err := f.identReg.BindChannel(context.Background(), identity.BindChannelCommand{
		IdentityID: user, Channel: identity.Channel("feishu"),
		VendorUserID: "ou-known", Preferred: true,
		Actor: observability.Actor("system"),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := f.resolver.Resolve(context.Background(), "ou-known")
	if err != nil {
		t.Fatal(err)
	}
	if got != user {
		t.Errorf("id: %s want %s", got, user)
	}
	// No auto-bound event emitted (we hit the binding-lookup cache).
	if f.hasEvent(t, "bridge.identity_auto_bound") {
		t.Error("auto_bound should NOT fire for pre-bound vendor_user_id")
	}
}
