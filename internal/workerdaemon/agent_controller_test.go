package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingReporter is a fake feedbackReporter that records every call so
// assertions can inspect activity / lifecycle / work-item feedback. Safe for
// concurrent use (OnEvent/OnExit fire on the session reader goroutine).
type recordingReporter struct {
	mu sync.Mutex

	activities []activityCall
	lifecycles []lifecycleCall
	workItems  []workItemCall
}

type activityCall struct {
	agentID, eventType, payload, workItemRef, interactionRef string
}
type lifecycleCall struct {
	agentID, state, errMsg string
}
type workItemCall struct {
	agentID, workItemID, state string
}

func (r *recordingReporter) ReportAgentActivity(_ context.Context, agentID, eventType, payloadJSON, workItemRef, interactionRef string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activities = append(r.activities, activityCall{agentID, eventType, payloadJSON, workItemRef, interactionRef})
	return nil
}

func (r *recordingReporter) ReportAgentLifecycle(_ context.Context, agentID, state, errMsg string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lifecycles = append(r.lifecycles, lifecycleCall{agentID, state, errMsg})
	return nil
}

func (r *recordingReporter) ReportWorkItemState(_ context.Context, agentID, workItemID, state string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workItems = append(r.workItems, workItemCall{agentID, workItemID, state})
	return nil
}

func (r *recordingReporter) lifecycleCalls() []lifecycleCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]lifecycleCall, len(r.lifecycles))
	copy(out, r.lifecycles)
	return out
}

func (r *recordingReporter) activityCalls() []activityCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]activityCall, len(r.activities))
	copy(out, r.activities)
	return out
}

func (r *recordingReporter) workItemCalls() []workItemCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]workItemCall, len(r.workItems))
	copy(out, r.workItems)
	return out
}

var _ feedbackReporter = (*recordingReporter)(nil)

// recordingLauncher hands out a fresh fakeProc per Launch and records how many
// launches happened (so restart vs replay assertions work).
type recordingLauncher struct {
	mu     sync.Mutex
	procs  []*fakeProc
	specs  []ClaudeLaunchSpec
	nextFn func() *fakeProc
}

func (l *recordingLauncher) Launch(_ context.Context, spec ClaudeLaunchSpec) (sessionProc, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fp := newFakeProc()
	l.procs = append(l.procs, fp)
	l.specs = append(l.specs, spec)
	return fp, nil
}

func (l *recordingLauncher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.procs)
}

func (l *recordingLauncher) lastProc() *fakeProc {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.procs) == 0 {
		return nil
	}
	return l.procs[len(l.procs)-1]
}

func (l *recordingLauncher) lastSpec() ClaudeLaunchSpec {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.specs[len(l.specs)-1]
}

// newTestController builds a controller with the recording reporter + launcher
// rooted at a temp AgentHomeBase.
func newTestController(t *testing.T, base string) (*AgentController, *recordingReporter, *recordingLauncher) {
	t.Helper()
	rep := &recordingReporter{}
	lp := &recordingLauncher{}
	c, err := NewAgentController(AgentControllerConfig{
		Reporter:      rep,
		Launcher:      lp,
		WorkerID:      "w-1",
		AdminURL:      "unix:/tmp/admin.sock",
		WorkerToken:   "tok",
		BinaryPath:    "agent-center",
		AgentHomeBase: base,
		StopGrace:     50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, rep, lp
}

func reconcileCmd(t *testing.T, agentID, desired string, version int, scope string, offset int64) ControlCommand {
	t.Helper()
	pl := reconcilePayload{AgentID: agentID, DesiredLifecycle: desired, Version: version, ResetScope: scope}
	return ControlCommand{
		ID:          "cmd-r",
		Offset:      offset,
		CommandType: cmdTypeAgentReconcile,
		Payload:     mustJSON(t, pl),
	}
}

func workCmd(t *testing.T, agentID, workItemID, brief string, offset int64) ControlCommand {
	t.Helper()
	pl := workPayload{AgentID: agentID, WorkItemID: workItemID, Brief: brief}
	return ControlCommand{
		ID:          "cmd-w",
		Offset:      offset,
		CommandType: cmdTypeAgentWork,
		Payload:     mustJSON(t, pl),
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAgentController_ReconcileRunning_StartsAndStreamsActivity(t *testing.T) {
	c, rep, lp := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	if lp.count() != 1 {
		t.Fatalf("want 1 launch, got %d", lp.count())
	}

	// Feed canned stdout events and ensure they map to ReportAgentActivity.
	fp := lp.lastProc()
	fp.feed(`{"type":"thinking","text":"hmm"}`)
	fp.feed(`{"type":"tool_use","name":"Bash","input":{"cmd":"ls"}}`)

	// Wait for the activity calls to land (reader goroutine is async).
	waitFor(t, func() bool { return len(rep.activityCalls()) >= 2 })

	acts := rep.activityCalls()
	if acts[0].agentID != "agent-1" || acts[0].eventType != "thinking" {
		t.Fatalf("activity[0]: %+v", acts[0])
	}
	if acts[1].eventType != "tool_call" {
		t.Fatalf("activity[1] eventType: %q want tool_call", acts[1].eventType)
	}
	if !strings.Contains(acts[1].payload, "\"tool_name\":\"Bash\"") && !strings.Contains(acts[1].payload, "Bash") {
		t.Fatalf("activity[1] payload missing tool name: %s", acts[1].payload)
	}
}

func TestAgentController_Work_InjectsBriefAndReportsActive(t *testing.T) {
	c, rep, lp := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "do the task", 2)); err != nil {
		t.Fatalf("work: %v", err)
	}

	in := lp.lastProc().stdinBytes()
	if !strings.Contains(in, "do the task") {
		t.Fatalf("stdin missing brief: %q", in)
	}
	if !strings.Contains(in, `"type":"user"`) {
		t.Fatalf("stdin not stream-json user line: %q", in)
	}

	wis := rep.workItemCalls()
	if len(wis) != 1 || wis[0].workItemID != "wi-1" || wis[0].state != "active" {
		t.Fatalf("work-item calls: %+v", wis)
	}
}

func TestAgentController_Work_NoSessionRetries(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	// No reconcile(running) yet → work should return an error (retry).
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "brief", 1)); err == nil {
		t.Fatal("want error for work with no running session (retry signal)")
	}
}

func TestAgentController_ReconcileStop_ReportsStoppedOnce(t *testing.T) {
	c, rep, lp := newTestController(t, t.TempDir())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	fp := lp.lastProc()
	// Make the fake honour SIGTERM by exiting when it arrives.
	go honourSIGTERM(fp)

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "stopped", 2, "", 2)); err != nil {
		t.Fatalf("reconcile stop: %v", err)
	}

	if !fp.gotSIGTERM && !fp.gotKill {
		t.Fatal("expected the fake proc to receive SIGTERM or kill")
	}

	lc := rep.lifecycleCalls()
	stopped := 0
	for _, l := range lc {
		if l.agentID == "agent-1" && l.state == "stopped" {
			stopped++
		}
	}
	if stopped != 1 {
		t.Fatalf("want exactly 1 stopped lifecycle report, got %d (%+v)", stopped, lc)
	}
}

func TestAgentController_ReconcileReset_WipesWorkspaceWithContainment(t *testing.T) {
	base := t.TempDir()
	c, rep, lp := newTestController(t, base)

	// Start so the home dirs exist.
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	go honourSIGTERM(lp.lastProc())

	home := filepath.Join(base, "workers", "w-1", "agents", "agent-1")
	workspace := filepath.Join(home, "workspace")
	// Plant a file inside the workspace (to be wiped).
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(workspace, "scratch.txt")
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Plant a sibling dir OUTSIDE the agent home (must be untouched).
	sibling := filepath.Join(base, "workers", "w-1", "agents", "other-agent")
	if err := os.MkdirAll(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	siblingFile := filepath.Join(sibling, "keep.txt")
	if err := os.WriteFile(siblingFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "resetting", 2, "workspace", 2)); err != nil {
		t.Fatalf("reconcile reset: %v", err)
	}

	// Workspace wiped.
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace not wiped: stat err=%v", err)
	}
	// Sibling untouched (containment).
	if _, err := os.Stat(siblingFile); err != nil {
		t.Fatalf("sibling outside agent home was touched: %v", err)
	}

	// Exactly one stopped lifecycle report.
	stopped := 0
	for _, l := range rep.lifecycleCalls() {
		if l.state == "stopped" {
			stopped++
		}
	}
	if stopped != 1 {
		t.Fatalf("want 1 stopped lifecycle, got %d", stopped)
	}
}

func TestAgentController_ResetContainmentRefusesEscape(t *testing.T) {
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	home := filepath.Join(base, "workers", "w-1", "agents", "agent-1")

	// A target that escapes the agent home must be refused (nothing deleted).
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	escapeTarget := filepath.Join(home, "..", "..", "..", "outside")
	if err := c.wipeContained(home, escapeTarget); err == nil {
		t.Fatal("want containment refusal for escaping target")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("escape target was deleted despite refusal: %v", err)
	}
	// Home itself must be refused too.
	if err := c.wipeContained(home, home); err == nil {
		t.Fatal("want refusal to wipe the agent home itself")
	}
}

func TestAgentController_UnexpectedCrash_ReportsErrorOnceAndClears(t *testing.T) {
	c, rep, lp := newTestController(t, t.TempDir())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	fp := lp.lastProc()
	// Crash the process (desired is still running → OnExit reports error).
	fp.exit(errors.New("boom"))

	waitFor(t, func() bool {
		for _, l := range rep.lifecycleCalls() {
			if l.state == "error" {
				return true
			}
		}
		return false
	})

	errs := 0
	for _, l := range rep.lifecycleCalls() {
		if l.agentID == "agent-1" && l.state == "error" {
			errs++
			if !strings.Contains(l.errMsg, "boom") {
				t.Fatalf("error msg missing cause: %q", l.errMsg)
			}
		}
	}
	if errs != 1 {
		t.Fatalf("want exactly 1 error lifecycle, got %d", errs)
	}

	// Managed entry cleared.
	c.mu.Lock()
	_, ok := c.agents["agent-1"]
	c.mu.Unlock()
	if ok {
		t.Fatal("managed entry not cleared after crash")
	}
}

func TestAgentController_VersionIdempotency_ReplayNoRestart(t *testing.T) {
	c, _, lp := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 5, "", 1)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if lp.count() != 1 {
		t.Fatalf("want 1 launch, got %d", lp.count())
	}

	// Replay the SAME version (and an older one) → no restart.
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 5, "", 2)); err != nil {
		t.Fatalf("replay reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 3, "", 3)); err != nil {
		t.Fatalf("older reconcile: %v", err)
	}
	if lp.count() != 1 {
		t.Fatalf("replay caused a restart: launches=%d want 1", lp.count())
	}
}

func TestAgentController_RestartOnVersionBump(t *testing.T) {
	c, _, lp := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	go honourSIGTERM(lp.lastProc())

	// A version bump with desired=running → restart (stop old + start new).
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 2, "", 2)); err != nil {
		t.Fatalf("restart reconcile: %v", err)
	}
	if lp.count() != 2 {
		t.Fatalf("want 2 launches after restart, got %d", lp.count())
	}
}

func TestAgentController_UnknownCommandType_ReturnsNil(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	cmd := ControlCommand{ID: "x", Offset: 1, CommandType: "agent.frobnicate", Payload: "{}"}
	if err := c.Handle(context.Background(), cmd); err != nil {
		t.Fatalf("unknown command must return nil (don't wedge cursor), got %v", err)
	}
}

func TestAgentController_MalformedPayload_ReturnsNil(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	cmd := ControlCommand{ID: "x", Offset: 1, CommandType: cmdTypeAgentReconcile, Payload: "{not json"}
	if err := c.Handle(context.Background(), cmd); err != nil {
		t.Fatalf("malformed reconcile must return nil, got %v", err)
	}
}

// honourSIGTERM makes a fakeProc exit cleanly once it receives SIGTERM (or is
// killed), mirroring the c-ii-A graceful-stop test helper.
func honourSIGTERM(fp *fakeProc) {
	for {
		fp.mu.Lock()
		term := fp.gotSIGTERM || fp.gotKill
		fp.mu.Unlock()
		if term {
			fp.exit(nil)
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// waitFor polls cond up to ~2s, failing the test on timeout.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
