package workerdaemon

import (
	"context"
	"strings"
	"testing"
)

func workAvailableCmd(t *testing.T, agentID, workItemID string, offset int64) ControlCommand {
	t.Helper()
	pl := workAvailablePayload{AgentID: agentID, TaskID: workItemID}
	return ControlCommand{
		ID:          "cmd-wa",
		Offset:      offset,
		CommandType: cmdTypeWorkAvailable,
		Payload:     mustJSON(t, pl),
	}
}

// PR4a (v2.8.1 #278 D): agent.work_available NUDGES the agent to run its pull
// loop — injects a SHORT wake (not the full prompt; the loop instructions are the
// persistent system prompt). It must NOT report the WorkItem active (the agent
// self-activates via start_work). Coalesce: a re-emit/replay of the same WI does
// NOT inject again. Acking keeps the control-stream cursor advancing.
func TestAgentController_WorkAvailable_NudgesOnceCoalesced(t *testing.T) {
	c, _, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workAvailableCmd(t, "agent-1", "wi-1", 2)); err != nil {
		t.Fatalf("work_available must ack (never wedge the cursor), got %v", err)
	}

	// PR4a: injects exactly ONE short nudge (not the full prompt).
	in := rs.last().injectedMsgs()
	if len(in) != 1 {
		t.Fatalf("work_available must inject one nudge, got %+v", in)
	}
	if in[0] != workAvailableNudge {
		t.Fatalf("work_available nudge = %q, want the short workAvailableNudge", in[0])
	}

	// Coalesce: a re-emit/replay of the SAME work_item_id does NOT inject again.
	if err := c.Handle(context.Background(), workAvailableCmd(t, "agent-1", "wi-1", 3)); err != nil {
		t.Fatalf("work_available replay must ack, got %v", err)
	}
	if in := rs.last().injectedMsgs(); len(in) != 1 {
		t.Fatalf("coalesced replay must NOT inject again, got %+v", in)
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

// TestWorkAvailableNudge_CarriesDiscoverabilityHint pins T463 (issue d8c8c9b8
// (b) #3): the dispatch wake nudge points a freshly-dispatched agent at
// search_tools before it concludes a read tool (e.g. get_issue) is missing — so
// "派活就卡、静默无报错" can't recur via the wake path.
func TestWorkAvailableNudge_CarriesDiscoverabilityHint(t *testing.T) {
	for _, want := range []string{"search_tools", "get_issue", "deferred"} {
		if !strings.Contains(workAvailableNudge, want) {
			t.Fatalf("workAvailableNudge missing %q (T463 discoverability hint)", want)
		}
	}
}
