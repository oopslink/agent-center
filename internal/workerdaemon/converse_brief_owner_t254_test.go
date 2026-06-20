package workerdaemon

import (
	"strings"
	"testing"
)

// T254 (I19): plan-chat brief must stay BYTE-IDENTICAL across the OwnerContext
// refactor. This snapshots the full rendered brief — if the refactor drifts the
// plan framing by even one byte, this fails. (Per-field assertions live in
// converse_brief_plan_t250_test.go; this is the regression anchor.)
func TestBuildConverseBrief_PlanSnapshot_ByteStable_T254(t *testing.T) {
	got := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-plan-1", ConvKind: "plan",
		ConvName: "Reminder feature", OwnerRef: "pm://plans/plan-abc123",
		SenderDisplay: "hayang", MessageID: "m-1", MessageText: "完成这个 plan",
	})
	want := "[Plan chat — \"Reminder feature\" (plan_id=plan-abc123)] hayang mentioned you:\n" +
		"完成这个 plan\n\n" +
		"(This message belongs to plan_id=plan-abc123. When it refers to \"this plan\" — e.g. completing, archiving, or editing it — act on THAT plan_id, not any other plan you may also be in.)\n" +
		"(To reply, use the post_message tool with conversation_id=\"conv-plan-1\". This is a conversation, not a task — there is no work item to complete.)"
	if got != want {
		t.Fatalf("plan brief drifted.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// An ISSUE chat (a collaborator @mentioned the agent on an issue conversation)
// must render an id-anchored Issue header + a "this issue" anchor note, and must
// NOT carry the misleading "not a task" clause nor read as a DM.
func TestBuildConverseBrief_IssueChat_AnchorsIssueID_T254(t *testing.T) {
	brief := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-i", ConvKind: "issue",
		ConvName: "Login broken", OwnerRef: "pm://issues/issue-7",
		SenderDisplay: "bob", MessageID: "m", MessageText: "看下这个 issue",
	})
	for _, want := range []string{
		"[Issue chat — \"Login broken\" (issue_id=issue-7)] bob mentioned you:",
		"This message belongs to issue_id=issue-7",
		"this issue",
		"act on THAT issue_id, not any other issue",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("issue brief missing %q, got:\n%s", want, brief)
		}
	}
	for _, bad := range []string{
		"This is a conversation, not a task",
		"Direct message",
		"plan_id",
	} {
		if strings.Contains(brief, bad) {
			t.Fatalf("issue brief must NOT contain %q, got:\n%s", bad, brief)
		}
	}
}

// A TASK chat (the @-mentioned-collaborator path: an agent pulled into a task
// conversation it does not own) must render an id-anchored Task header + a "this
// task" anchor note, drop the "not a task" clause, and not read as a DM.
func TestBuildConverseBrief_TaskChat_AnchorsTaskID_T254(t *testing.T) {
	brief := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-t", ConvKind: "task",
		ConvName: "Refactor brief", OwnerRef: "pm://tasks/task-9",
		SenderDisplay: "alice", MessageID: "m", MessageText: "@helper 看下",
	})
	for _, want := range []string{
		"[Task chat — \"Refactor brief\" (task_id=task-9)] alice mentioned you:",
		"This message belongs to task_id=task-9",
		"this task",
		"act on THAT task_id, not any other task",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("task brief missing %q, got:\n%s", want, brief)
		}
	}
	for _, bad := range []string{"This is a conversation, not a task", "Direct message"} {
		if strings.Contains(brief, bad) {
			t.Fatalf("task brief must NOT contain %q, got:\n%s", bad, brief)
		}
	}
}

// Name miss (env title unresolved → empty ConvName) still anchors by {kind}_id
// alone — a name miss must never block the wake or hide the id.
func TestBuildConverseBrief_AnchoredOwner_NameMiss_FallsBackToID_T254(t *testing.T) {
	brief := buildConverseBrief(conversePayload{
		ConversationID: "c", ConvKind: "task", ConvName: "",
		OwnerRef: "pm://tasks/task-x", SenderDisplay: "x", MessageID: "m", MessageText: "hi",
	})
	if !strings.Contains(brief, "[Task chat (task_id=task-x)]") {
		t.Fatalf("name-less task brief must use id-only header, got:\n%s", brief)
	}
	if strings.Contains(brief, "— \"\"") {
		t.Fatalf("name-less task brief must not render an empty quoted name, got:\n%s", brief)
	}
}

// Thread coverage: when an anchored-owner mention is INSIDE a thread, the brief
// must carry BOTH the thread reply instruction (parent_message_id) AND the
// {kind}_id anchor note — "顶层 + thread 内都带 id".
func TestBuildConverseBrief_AnchoredOwner_InThread_KeepsIDAndParent_T254(t *testing.T) {
	brief := buildConverseBrief(conversePayload{
		ConversationID: "conv-t", ConvKind: "task", ConvName: "Refactor brief",
		OwnerRef: "pm://tasks/task-9", SenderDisplay: "alice", MessageID: "m",
		MessageText: "@helper 看下", RootMessageID: "m-root",
	})
	if !strings.Contains(brief, "parent_message_id=\"m-root\"") {
		t.Fatalf("in-thread brief must instruct parent_message_id, got:\n%s", brief)
	}
	if !strings.Contains(brief, "This message belongs to task_id=task-9") {
		t.Fatalf("in-thread brief must still carry the task_id anchor, got:\n%s", brief)
	}
	// The anchor note precedes the thread hint (prepended after substitution).
	if i, j := strings.Index(brief, "task_id=task-9"), strings.Index(brief, "parent_message_id"); i < 0 || j < 0 || i > j {
		t.Fatalf("anchor note must precede the thread reply hint, got:\n%s", brief)
	}
}

// Non-regression: a DM (no owner_ref) is unchanged — no owner framing, keeps the
// "not a task" clause.
func TestBuildConverseBrief_DM_Unchanged_T254(t *testing.T) {
	dm := buildConverseBrief(conversePayload{
		ConversationID: "c", ConvKind: "dm", SenderDisplay: "hayang",
		MessageID: "m", MessageText: "hello",
	})
	if !strings.Contains(dm, "[Direct message from hayang]:") {
		t.Fatalf("DM header changed, got:\n%s", dm)
	}
	if !strings.Contains(dm, "This is a conversation, not a task") {
		t.Fatalf("DM must keep the conversation note, got:\n%s", dm)
	}
	for _, bad := range []string{"chat (", "chat —", "mentioned you:", "plan_id=", "task_id=", "issue_id=", "project_id="} {
		if strings.Contains(dm, bad) {
			t.Fatalf("DM must not carry owner framing (%q), got:\n%s", bad, dm)
		}
	}
}
