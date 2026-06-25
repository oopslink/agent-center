package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/outbox"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// leaseNudgeEvent builds an EvtTaskLeaseExpiredNudge outbox event (T456).
func leaseNudgeEvent(id, assigneeRef, ownerRef, taskID string) outbox.Event {
	pl, err := json.Marshal(map[string]string{
		"task_id":      taskID,
		"project_id":   "proj-1",
		"owner_ref":    ownerRef,
		"assignee_ref": assigneeRef,
	})
	if err != nil {
		panic(err)
	}
	return outbox.Event{
		ID:        id,
		EventType: pmservice.EvtTaskLeaseExpiredNudge,
		Payload:   string(pl),
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

// T456 headline: a lapsed-lease nudge event → the WakeProjector (a) posts a visible
// @assignee system message into the task's bound conversation AND (b) enqueues ONE
// agent.converse for the RUNNING assignee so the SAME owner is woken to continue.
func TestWakeProjector_LeaseNudge_PostsMessageAndWakes(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "BOT", "W7")
	f.saveTaskConv(t, "task-conv-1", "T9")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(nil, &sysNotes, &sysMessages)

	e := leaseNudgeEvent("EVL1", "agent:BOT", "pm://tasks/T9", "T9")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}

	// (a) a visible @assignee message was posted into the task conversation.
	if len(sysNotes) != 0 {
		t.Fatalf("lease nudge must use ordinary system message, not systemNotify: %q", sysNotes)
	}
	if len(sysMessages) != 1 {
		t.Fatalf("want 1 system nudge message, got %d (%v)", len(sysMessages), sysMessages)
	}
	if !strings.Contains(sysMessages[0], "task-conv-1: ") || !strings.Contains(sysMessages[0], "@A BOT") {
		t.Fatalf("nudge message wrong: %q", sysMessages[0])
	}

	// (b) exactly one agent.converse wake, keyed by the unique event id.
	cmds := f.commandsFor(t, "W7")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse, got %d (%v)", len(cmds), cmds)
	}
	c := cmds[0]
	if c.IdempotencyKey() != "agent.converse:task-conv-1:lease-nudge:EVL1:BOT" {
		t.Fatalf("idempotency_key = %q", c.IdempotencyKey())
	}
	for _, want := range []string{`"agent_id":"BOT"`, `"conversation_id":"task-conv-1"`, `"owner_ref":"pm://tasks/T9"`} {
		if !strings.Contains(c.Payload(), want) {
			t.Errorf("payload missing %s: %s", want, c.Payload())
		}
	}
}

// Replaying the SAME nudge event posts the message + wakes exactly once (same-tx
// AppliedStore dedups the redelivery).
func TestWakeProjector_LeaseNudge_ReplayOnce(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "BOT", "W7")
	f.saveTaskConv(t, "task-conv-1", "T9")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(nil, &sysNotes, &sysMessages)

	e := leaseNudgeEvent("EVL1", "agent:BOT", "pm://tasks/T9", "T9")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 1: %v", err)
	}
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 2: %v", err)
	}
	if len(sysNotes) != 0 {
		t.Fatalf("lease nudge must use ordinary system message, not systemNotify: %q", sysNotes)
	}
	if len(sysMessages) != 1 {
		t.Fatalf("replay must not duplicate the nudge message: got %d", len(sysMessages))
	}
	if cmds := f.commandsFor(t, "W7"); len(cmds) != 1 {
		t.Fatalf("replay must not duplicate the converse: want 1, got %d", len(cmds))
	}
}

// A STOPPED assignee still gets the durable @assignee message (it reads it on its
// next run, e.g. after self-heal relaunch) but NO converse wake — the anti-orphan
// fix never reclaims, it just leaves the nudge waiting.
func TestWakeProjector_LeaseNudge_StoppedAgent_MessageButNoConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgent(t, "BOT", "W7") // NewAgent → LifecycleStopped
	f.saveTaskConv(t, "task-conv-1", "T9")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(nil, &sysNotes, &sysMessages)

	e := leaseNudgeEvent("EVL1", "agent:BOT", "pm://tasks/T9", "T9")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(sysNotes) != 0 {
		t.Fatalf("lease nudge must use ordinary system message, not systemNotify: %q", sysNotes)
	}
	if len(sysMessages) != 1 {
		t.Fatalf("stopped assignee must still get the durable message, got %d", len(sysMessages))
	}
	if cmds := f.commandsFor(t, "W7"); len(cmds) != 0 {
		t.Fatalf("stopped assignee must enqueue no converse, got %d", len(cmds))
	}
}

// No bound task conversation → drain the event (no message, no converse, no error):
// a missing target must not stall the projector.
func TestWakeProjector_LeaseNudge_NoConversation_NoOp(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "BOT", "W7")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(nil, &sysNotes, &sysMessages)

	e := leaseNudgeEvent("EVL1", "agent:BOT", "pm://tasks/T-missing", "T-missing")
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project must not fail with no conversation: %v", err)
	}
	if len(sysNotes) != 0 || len(sysMessages) != 0 {
		t.Fatalf("no conversation → no message, got notify=%d message=%d", len(sysNotes), len(sysMessages))
	}
	if cmds := f.commandsFor(t, "W7"); len(cmds) != 0 {
		t.Fatalf("no conversation → no converse, got %d", len(cmds))
	}
}

// A non-agent assignee / non-task owner_ref is a defensive skip (no message, no
// converse, no error).
func TestWakeProjector_LeaseNudge_MalformedPayload_NoOp(t *testing.T) {
	f := newWakeFixture(t)
	f.saveTaskConv(t, "task-conv-1", "T9")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(nil, &sysNotes, &sysMessages)

	e := leaseNudgeEvent("EVL1", "user:alice", "pm://tasks/T9", "T9") // non-agent assignee
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(sysNotes) != 0 || len(sysMessages) != 0 || len(f.commandsFor(t, "W7")) != 0 {
		t.Fatalf("malformed assignee must be a no-op")
	}
}
