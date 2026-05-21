// Package scenarios — cross-phase e2e scenarios that exercise the
// in-process harness (plan-7 § 5.3). These complement the binary-level
// e2e in tests/e2e/phase7_test.go (CLI sanity) by driving the full
// Bridge inbound → Conversation / Task / IR chain.
package scenarios

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
	"github.com/oopslink/agent-center/tests/e2e/harness"
)

// E2E-A (simplified): user @bot → inbound Message → events emit.
// Full multi-step Task creation / dispatch / completion chain lives
// in the Phase 2-6 integration tests (we don't re-prove those here);
// what we DO prove here is the inbound boundary.
func TestE2E_A_UserAtBotInbound(t *testing.T) {
	h := harness.Spin(t)
	h.SeedUser("hayang")
	h.Feishu.Inject(inbound.VendorEvent{
		Kind:            inbound.VendorEventMessageReceive,
		VendorMsgRef:    "ref-1",
		VendorThreadKey: "dm-1",
		VendorUserID:    "ou-1",
		Context:         inbound.MessageContextDM,
		Text:            "@bot please ship the feature",
		ReceivedAt:      h.Clock.Now(),
	})
	if err := h.AwaitEvent("conversation.message_added", 3*time.Second); err != nil {
		t.Fatalf("await: %v", err)
	}
	if err := h.AwaitEvent("bridge.identity_auto_bound", 1*time.Second); err != nil {
		t.Fatalf("auto_bound: %v", err)
	}
}

// E2E-B (simplified): /track T-1 → task.bind_conversation event chain.
func TestE2E_B_SlashTrack(t *testing.T) {
	h := harness.Spin(t)
	h.SeedUser("hayang")
	// Drive a non-existing task — exercises the reject path of
	// `bridge.slash_command_rejected`.
	h.Feishu.Inject(inbound.VendorEvent{
		Kind: inbound.VendorEventMessageReceive,
		VendorMsgRef:    "ref-1",
		VendorThreadKey: "dm-1", VendorUserID: "ou-1",
		Context: inbound.MessageContextDM,
		Text:    "/track T-not-found",
		ReceivedAt: h.Clock.Now(),
	})
	if err := h.AwaitEvent("bridge.slash_command_rejected", 3*time.Second); err != nil {
		t.Fatalf("await: %v", err)
	}
}

// E2E-Dedupe: same vendor_msg_ref → dedupe drop, only one message.
func TestE2E_DedupeDrop(t *testing.T) {
	h := harness.Spin(t)
	h.SeedUser("hayang")
	ev := inbound.VendorEvent{
		Kind:            inbound.VendorEventMessageReceive,
		VendorMsgRef:    "ref-dup",
		VendorThreadKey: "dm-1",
		VendorUserID:    "ou-1",
		Context:         inbound.MessageContextDM,
		Text:            "hi",
		ReceivedAt:      h.Clock.Now(),
	}
	h.Feishu.Inject(ev)
	h.Feishu.Inject(ev)
	if err := h.AwaitEvent("bridge.inbound_dedupe_drop", 3*time.Second); err != nil {
		t.Fatalf("await: %v", err)
	}
}
