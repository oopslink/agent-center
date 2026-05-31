package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/peek"
	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func TestPrintFleet_HumanIncludesAllSegments(t *testing.T) {
	var out, errw bytes.Buffer
	snap := query.FleetSnapshot{
		WorkItems:     []query.WorkItemRow{{WorkItemID: "WI-1", TaskID: "T-1", AgentID: "AG-1", Status: "active"}},
		Workers:       []query.FleetWorkerRow{{WorkerID: "W-1", Status: "online", ActiveCount: 1}},
		PendingIssues: []query.FleetIssueRow{{IssueID: "I-1", ProjectID: "p", Title: "discuss"}},
		Warnings:      []string{"x: y"},
		GeneratedAt:   "2026-05-20T10:00:00Z",
	}
	rc := printFleet(&out, &errw, "human", snap)
	if rc != ExitOK {
		t.Fatal("rc != OK")
	}
	if !strings.Contains(errw.String(), "warning") {
		t.Fatalf("warning not emitted: %s", errw.String())
	}
	for _, want := range []string{"WI-1", "W-1", "I-1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in: %s", want, out.String())
		}
	}
}

func TestPrintStats_HumanContainsCountersAndTotals(t *testing.T) {
	var out bytes.Buffer
	res := query.StatsResult{
		Scope: query.StatsScopeTasks, Since: "2026-05-20T00:00:00Z",
		Counters:  map[string]int{"open": 5, "done": 2},
		Totals:    map[string]any{"total": 7},
		Generated: "now",
	}
	if rc := printStats(&out, "human", res); rc != ExitOK {
		t.Fatal("rc != OK")
	}
	for _, want := range []string{"open", "done", "total"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in: %s", want, out.String())
		}
	}
}

func TestPrintQueryResult_HumanShape(t *testing.T) {
	var out bytes.Buffer
	res := query.QueryResult{
		Resource: query.QueryTasks,
		Items: []any{
			map[string]any{"id": "T-1", "title": "x"},
		},
		NextCursor: "C-1",
	}
	if rc := printQueryResult(&out, "human", res); rc != ExitOK {
		t.Fatal("rc != OK")
	}
	s := out.String()
	if !strings.Contains(s, "T-1") || !strings.Contains(s, "C-1") {
		t.Fatalf("output: %s", s)
	}
}

func TestMapInspectErr_AllBranches(t *testing.T) {
	var errw bytes.Buffer
	if c := mapInspectErr(query.ErrInspectKindUnknown, "human", &errw); c != ExitUsage {
		t.Fatal("kind")
	}
	if c := mapInspectErr(query.ErrInspectIDRequired, "human", &errw); c != ExitUsage {
		t.Fatal("id")
	}
	if c := mapInspectErr(query.ErrInspectNotFound, "human", &errw); c != ExitNotFound {
		t.Fatal("notfound")
	}
	if c := mapInspectErr(errors.New("other"), "human", &errw); c != ExitBusinessError {
		t.Fatal("other")
	}
}

func TestMapPeekReason(t *testing.T) {
	if mapPeekReason(peek.ReasonExecutionNotFound) != ExitNotFound {
		t.Fatal("not found")
	}
	if mapPeekReason(peek.ReasonInvalidRequest) != ExitUsage {
		t.Fatal("invalid")
	}
	if mapPeekReason("anything-else") != ExitBusinessError {
		t.Fatal("default")
	}
}

func TestParseSince_RFC3339(t *testing.T) {
	t0, err := parseSince("2026-05-20T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if t0.IsZero() {
		t.Fatal("zero time")
	}
}

func TestParseSince_Invalid(t *testing.T) {
	if _, err := parseSince("blob"); err == nil {
		t.Fatal("expected error")
	}
}

func TestOneLineMap_StableOrder(t *testing.T) {
	m := map[string]any{"b": 1, "a": 2}
	got := oneLineMap(m)
	if !strings.HasPrefix(got, "a=") {
		t.Fatalf("not sorted: %s", got)
	}
}

func TestQueryHandler_BadUntil(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "query")
	_, _, code := runHandler(t, cmd, []string{"events", "--until=blob"})
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
}

func TestStatsCmd_AllScopes_NoCrash(t *testing.T) {
	app := newTestApp(t)
	// Seed minimal data so each scope hits live paths.
	tk, _ := task.New(task.NewInput{ID: "T-1", ProjectID: "p", Title: "x", CreatedBy: "user:t", Now: time.Now()})
	_ = app.TaskRepo.Save(context.Background(), tk)
	ex, _ := execution.New(execution.NewInput{ID: "E-1", TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", WorkspaceMode: execution.WorkspaceWorktree, Now: time.Now()})
	_ = app.ExecRepo.Save(context.Background(), ex)
	cmd := findCmd(app.ObservabilityCommands(), "stats")
	for _, scope := range []string{"tasks", "executions", "workers", "events", "issues"} {
		_, _, code := runHandler(t, cmd, []string{"--scope=" + scope})
		if code != ExitOK {
			t.Errorf("scope=%s code=%d", scope, code)
		}
	}
}

func TestQueryCmd_AllResources_NoCrash(t *testing.T) {
	app := newTestApp(t)
	tk, _ := task.New(task.NewInput{ID: "T-1", ProjectID: "p", Title: "x", CreatedBy: "user:t", Now: time.Now()})
	_ = app.TaskRepo.Save(context.Background(), tk)
	cmd := findCmd(app.ObservabilityCommands(), "query")
	for _, r := range []string{"tasks", "executions", "workers", "issues", "events"} {
		_, _, code := runHandler(t, cmd, []string{r, "--format=json"})
		if code != ExitOK {
			t.Errorf("resource=%s code=%d", r, code)
		}
	}
}

func TestPeekTraceCmd_SocketUnsetWhenConfigEmpty(t *testing.T) {
	app := newTestApp(t)
	app.Config.Peek.WorkerSocket = ""
	cmd := findCmd(app.ObservabilityCommands(), "peek-trace")
	_, _, code := runHandler(t, cmd, []string{"E-1"})
	if code != ExitBusinessError {
		t.Fatalf("expected ExitBusinessError for unset socket, got %d", code)
	}
}

func TestPsHandlerEmits_WhenSegmentMissing(t *testing.T) {
	app := newTestApp(t)
	// Make one segment fail
	app.FleetSvc = nil
	// rebuild minimal FleetSvc with broken deps
	cmd := findCmd(app.ObservabilityCommands(), "ps")
	// Should panic / crash? — instead, restore svc with explicit nil-Issues to surface warning
	deps := query.Deps{} // all nil
	app.FleetSvc = query.NewFleetSnapshotService(deps)
	_, errw, code := runHandler(t, cmd, []string{})
	if code != ExitOK {
		t.Fatalf("expected ExitOK even on warnings, got %d", code)
	}
	if !strings.Contains(errw, "warning") {
		t.Fatalf("expected warning lines, got: %s", errw)
	}
}

func TestInspect_ConvJSON_Pretty(t *testing.T) {
	app := newTestApp(t)
	cmd := findCmd(app.ObservabilityCommands(), "inspect")
	_, _, code := runHandler(t, cmd, []string{"conversation", "C-missing", "--format=json"})
	if code != ExitNotFound {
		t.Fatalf("expected ExitNotFound, got %d", code)
	}
}

// Touch unused observability import to keep this file stable across edits.
var _ = observability.EventType("noop")
