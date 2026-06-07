package workerdaemon

import (
	"context"
	"testing"
)

func workAvailableCmd(t *testing.T, agentID, workItemID string, offset int64) ControlCommand {
	t.Helper()
	pl := workAvailablePayload{AgentID: agentID, WorkItemID: workItemID}
	return ControlCommand{
		ID:          "cmd-wa",
		Offset:      offset,
		CommandType: cmdTypeWorkAvailable,
		Payload:     mustJSON(t, pl),
	}
}

// PR3 (v2.8.1 #278 D): agent.work_available is HANDLED = coalesce + log + ack
// ONLY. It must NOT inject (the pull nudge + the agent pull-loop land together in
// PR4) and must NOT report the WorkItem active (the agent self-activates via
// start_work in the pull model). Acking keeps the control-stream cursor advancing
// (never wedges the loop — the dual-track old push still drives the agent).
func TestAgentController_WorkAvailable_LogOnlyNoInject(t *testing.T) {
	c, rep, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workAvailableCmd(t, "agent-1", "wi-1", 2)); err != nil {
		t.Fatalf("work_available must ack (never wedge the cursor), got %v", err)
	}

	// PR3 = log-only: NO inject (deferred to PR4).
	if in := rs.last().injectedMsgs(); len(in) != 0 {
		t.Fatalf("PR3 work_available must NOT inject (deferred to PR4), got %+v", in)
	}
	// NO report active (the agent self-activates via start_work — pull model).
	if wis := rep.workItemCalls(); len(wis) != 0 {
		t.Fatalf("work_available must NOT report WorkItem state, got %+v", wis)
	}

	// Coalesce: a re-emit/replay of the SAME work_item_id is a no-op (still acks,
	// still no inject).
	if err := c.Handle(context.Background(), workAvailableCmd(t, "agent-1", "wi-1", 3)); err != nil {
		t.Fatalf("work_available replay must ack, got %v", err)
	}
	if in := rs.last().injectedMsgs(); len(in) != 0 {
		t.Fatalf("coalesced replay must still not inject, got %+v", in)
	}
}

// recordWorkAvail coalesces by work_item_id (mirrors the wake message dedup):
// first = newly recorded, replay = coalesced, distinct = new, empty = no-op.
func TestAgentController_RecordWorkAvail_Coalesce(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if !c.recordWorkAvail("agent-1", "wi-1") {
		t.Fatal("first wi-1 must be newly recorded (true)")
	}
	if c.recordWorkAvail("agent-1", "wi-1") {
		t.Fatal("replay of wi-1 must be coalesced (false)")
	}
	if !c.recordWorkAvail("agent-1", "wi-2") {
		t.Fatal("distinct wi-2 must be newly recorded (true)")
	}
	if c.recordWorkAvail("agent-1", "") {
		t.Fatal("empty work_item_id must be a no-op (false)")
	}
}
