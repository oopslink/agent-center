package workerdaemon

import (
	"strings"
	"testing"
)

// T250: a plan-chat converse brief must name WHICH plan the message belongs to —
// the resolved plan name AND its plan_id — so an agent woken in a plan chat can
// disambiguate "this plan" across concurrent plan chats (and act on the right
// plan_id for destructive actions like complete/archive). owner_ref=pm://plans/{id}
// is the discriminator; ConvName carries the env-resolved plan name.
func TestBuildConverseBrief_PlanChat_NamesPlan_T250(t *testing.T) {
	brief := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-plan-1", ConvKind: "plan",
		ConvName: "Reminder feature", OwnerRef: "pm://plans/plan-abc123",
		SenderDisplay: "hayang", MessageID: "m-1", MessageText: "完成这个 plan",
	})
	if !strings.Contains(brief, "plan_id=plan-abc123") {
		t.Fatalf("plan brief must carry the plan_id, got:\n%s", brief)
	}
	if !strings.Contains(brief, "Reminder feature") {
		t.Fatalf("plan brief must carry the resolved plan name, got:\n%s", brief)
	}
	// The disambiguation note must pin "this plan" to the plan_id.
	if !strings.Contains(brief, "this plan") || !strings.Contains(brief, "act on THAT plan_id") {
		t.Fatalf("plan brief must pin \"this plan\" to the plan_id, got:\n%s", brief)
	}
	// Must NOT mis-render as a DM.
	if strings.Contains(brief, "Direct message") {
		t.Fatalf("plan brief must not read as a DM, got:\n%s", brief)
	}
}

// Two different plan chats must produce briefs pointing at their OWN plan_id (no
// cross-talk) — the core bug this task fixes.
func TestBuildConverseBrief_PlanChat_NoCrossTalk_T250(t *testing.T) {
	a := buildConverseBrief(conversePayload{
		ConversationID: "c-a", ConvKind: "plan", ConvName: "Plan A",
		OwnerRef: "pm://plans/plan-aaa", SenderDisplay: "x", MessageID: "m", MessageText: "done",
	})
	b := buildConverseBrief(conversePayload{
		ConversationID: "c-b", ConvKind: "plan", ConvName: "Plan B",
		OwnerRef: "pm://plans/plan-bbb", SenderDisplay: "x", MessageID: "m", MessageText: "done",
	})
	if !strings.Contains(a, "plan-aaa") || strings.Contains(a, "plan-bbb") {
		t.Fatalf("plan A brief must reference only plan-aaa, got:\n%s", a)
	}
	if !strings.Contains(b, "plan-bbb") || strings.Contains(b, "plan-aaa") {
		t.Fatalf("plan B brief must reference only plan-bbb, got:\n%s", b)
	}
}

// When the plan name is unresolved (env resolver miss → empty ConvName), the brief
// still disambiguates by plan_id alone.
func TestBuildConverseBrief_PlanChat_NameMiss_FallsBackToID_T250(t *testing.T) {
	brief := buildConverseBrief(conversePayload{
		ConversationID: "c", ConvKind: "plan", ConvName: "",
		OwnerRef: "pm://plans/plan-xyz", SenderDisplay: "x", MessageID: "m", MessageText: "hi",
	})
	if !strings.Contains(brief, "plan_id=plan-xyz") {
		t.Fatalf("name-less plan brief must still carry plan_id, got:\n%s", brief)
	}
}

// Non-regression (acceptance #3): DM/channel briefs are unchanged — no plan
// framing, no plan note. A DM has empty owner_ref; a channel has a non-plan
// owner_ref scheme.
func TestBuildConverseBrief_NonPlan_Unaffected_T250(t *testing.T) {
	dm := buildConverseBrief(conversePayload{
		ConversationID: "c", ConvKind: "dm", SenderDisplay: "hayang",
		MessageID: "m", MessageText: "hello",
	})
	if !strings.Contains(dm, "Direct message from hayang") || strings.Contains(dm, "plan_id") {
		t.Fatalf("DM brief must be unchanged (no plan framing), got:\n%s", dm)
	}
	ch := buildConverseBrief(conversePayload{
		ConversationID: "c", ConvKind: "channel", ConvName: "general",
		OwnerRef: "id://organizations/org-1", SenderDisplay: "hayang",
		MessageID: "m", MessageText: "@bot hi",
	})
	if !strings.Contains(ch, "[Channel #general]") || strings.Contains(ch, "plan_id") {
		t.Fatalf("channel brief must be unchanged (no plan framing), got:\n%s", ch)
	}
}
