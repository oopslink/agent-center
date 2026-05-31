package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability/peek"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// Phase 4 e2e tests drive the real binary against the 6 Observability
// verbs (inspect / query / ps / stats / logs / peek-trace) and the
// peek-trace cross-process RPC.

func runWithCfg(t *testing.T, h *harness, args ...string) (string, string, int) {
	t.Helper()
	return h.run(args...)
}

// seedPMTaskE2E writes a pm.Task directly into the harness DB so the pm read
// path (`query tasks` / `inspect task`) can be exercised end-to-end. v2.7 #107
// proj-B (B′): observability reads the pm model; CLI `task create` still writes
// the old taskruntime model (migration tracked in #132). Runs migrations first
// so the call is self-contained (no prior CLI command required).
func seedPMTaskE2E(t *testing.T, dbPath, id, project, title string) {
	t.Helper()
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: pm.TaskID(id), ProjectID: pm.ProjectID(project), Title: title,
		CreatedBy: "user:test", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pmsql.NewTaskRepo(db).Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
}

func TestE2EP4_Inspect_Task_Roundtrip(t *testing.T) {
	h := newHarness(t)
	// v2.7 #107 Phase-2 (proj-B, B′): the observability "task" inspect/query
	// verbs read the project-management (pm) model. CLI `task create` still
	// writes the OLD taskruntime model (CLI management-surface migration tracked
	// in #132), so seed a pm.Task directly via the repo to exercise the pm read
	// path end-to-end.
	seedPMTaskE2E(t, h.dbPath, "T-e2e", "proj", "build foo")
	// Find task id via `query tasks`
	out, _, code := h.run("query", "tasks", "--project=proj", "--format=json")
	if code != 0 {
		t.Fatalf("query tasks: %d", code)
	}
	var qres struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &qres); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if len(qres.Items) == 0 {
		t.Fatalf("no tasks found: %s", out)
	}
	tid := qres.Items[0]["id"].(string)
	// inspect task <id>
	out, _, code = h.run("inspect", "task", tid, "--format=json")
	if code != 0 {
		t.Fatalf("inspect: %d, out=%s", code, out)
	}
	var inspectRes map[string]any
	_ = json.Unmarshal([]byte(out), &inspectRes)
	if inspectRes["id"] != tid {
		t.Fatalf("inspect id mismatch: %v", inspectRes)
	}
}

func TestE2EP4_Inspect_NotFound_ExitNotFound(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.run("inspect", "task", "T-missing")
	if code != 17 {
		t.Fatalf("expected exit 17 (NotFound), got %d", code)
	}
}

func TestE2EP4_Inspect_UnknownKind_ExitUsage(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.run("inspect", "blob", "X")
	if code != 2 {
		t.Fatalf("expected exit 2 (Usage), got %d", code)
	}
}

func TestE2EP4_Query_Events_PrefixMatch(t *testing.T) {
	h := newHarness(t)
	_, _, _ = h.run("worker", "enroll", "--worker-id=W-1")
	_, _, _ = h.run("project", "add", "--name=proj", "proj")
	_, _, _ = h.run("task", "create", "proj", "title 1")
	// Pull events
	out, _, code := h.run("query", "events", "--type=task.", "--format=json")
	if code != 0 {
		t.Fatalf("query events: %d", code)
	}
	if !strings.Contains(out, "task.") {
		t.Fatalf("expected at least one task.* event, got %s", out)
	}
}

func TestE2EP4_Query_LimitTooLarge_ExitUsage(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.run("query", "events", "--limit=99999")
	if code != 2 {
		t.Fatalf("expected exit 2 (Usage) for limit too large, got %d", code)
	}
}

func TestE2EP4_Ps_HumanAndJSON(t *testing.T) {
	h := newHarness(t)
	_, _, _ = h.run("worker", "enroll", "--worker-id=W-1")
	out, _, code := h.run("ps")
	if code != 0 {
		t.Fatalf("ps human: %d", code)
	}
	if !strings.Contains(out, "FLEET SNAPSHOT") {
		t.Fatalf("missing header: %s", out)
	}
	out, _, code = h.run("ps", "--format=json")
	if code != 0 {
		t.Fatalf("ps json: %d", code)
	}
	if !strings.Contains(out, `"work_items"`) {
		t.Fatalf("missing work_items key: %s", out)
	}
}

func TestE2EP4_Stats_Counters(t *testing.T) {
	h := newHarness(t)
	_, _, _ = h.run("worker", "enroll", "--worker-id=W-1")
	out, _, code := h.run("stats", "--scope=workers", "--format=json")
	if code != 0 {
		t.Fatalf("stats: %d", code)
	}
	if !strings.Contains(out, `"scope": "workers"`) {
		t.Fatalf("scope key missing: %s", out)
	}
}

func TestE2EP4_Stats_UnknownScope_ExitUsage(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.run("stats", "--scope=blob")
	if code != 2 {
		t.Fatalf("expected exit 2 (Usage), got %d", code)
	}
}

func TestE2EP4_Logs_FollowOnArchived_Explicit(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.run("logs", "task", "T-x", "--follow")
	if code != 2 {
		t.Fatalf("expected exit 2 (Usage) for --follow on archived, got %d", code)
	}
}

func TestE2EP4_PeekTrace_WorkerOffline_ExitBusinessError(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.run("peek-trace", "E-1", "--socket=/tmp/absent-pk-sock.sock")
	if code != 1 {
		t.Fatalf("expected exit 1 (Business) for offline worker, got %d", code)
	}
}

func TestE2EP4_PeekTrace_TailLast_CrossProcess(t *testing.T) {
	// Start a peek server in-process (worker daemon stand-in) and exercise
	// the binary against it.
	root := t.TempDir()
	sock := fmt.Sprintf("/tmp/pk_%d_%d.sock", os.Getpid(), rand.Int63())
	t.Cleanup(func() { _ = os.Remove(sock) })

	// Seed events.jsonl
	dir := filepath.Join(root, "E-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"thinking","text":"a"}`,
		`{"type":"thinking","text":"b"}`,
		`{"type":"tool_use","name":"Bash"}`,
	}
	for _, l := range lines {
		f, _ := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		_, _ = f.WriteString(l + "\n")
		_ = f.Close()
	}

	srv, err := peek.NewServer(sock, root)
	if err != nil {
		t.Fatal(err)
	}
	srv = srv.WithPollInterval(30 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer srv.Close()
	go func() { _ = srv.Serve(ctx) }()
	time.Sleep(80 * time.Millisecond)

	h := newHarness(t)
	out, _, code := h.run("peek-trace", "E-1", "--socket="+sock, "--last=2")
	if code != 0 {
		t.Fatalf("peek-trace: code=%d out=%s", code, out)
	}
	got := strings.Count(strings.TrimSpace(out), "\n") + 1
	if got != 2 {
		t.Fatalf("expected 2 lines from peek-trace, got %d:\n%s", got, out)
	}
}

func TestE2EP4_PeekTrace_ExecutionNotFound(t *testing.T) {
	root := t.TempDir()
	sock := fmt.Sprintf("/tmp/pk_%d_%d.sock", os.Getpid(), rand.Int63())
	t.Cleanup(func() { _ = os.Remove(sock) })
	srv, err := peek.NewServer(sock, root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer srv.Close()
	go func() { _ = srv.Serve(ctx) }()
	time.Sleep(80 * time.Millisecond)
	h := newHarness(t)
	_, _, code := h.run("peek-trace", "E-missing", "--socket="+sock)
	if code != 17 {
		t.Fatalf("expected exit 17 (NotFound), got %d", code)
	}
}

func TestE2EP4_AllInspectKinds_SmokeNoCrash(t *testing.T) {
	h := newHarness(t)
	// v2.6: supervisor/decision kinds removed in BE-9 supervisor cut.
	// v2.7 #107 Phase-2 (proj-A): worktree kind removed (no work-item equivalent).
	// v2.7 #127: input_request kind removed (no new-model IR entity).
	kinds := []string{"task", "execution", "worker", "issue", "conversation", "project"}
	for _, k := range kinds {
		_, _, code := h.run("inspect", k, "X")
		// NotFound (17) for absent IDs is the expected outcome.
		if code != 17 && code != 0 {
			t.Errorf("inspect %s exit=%d (expected 17 or 0)", k, code)
		}
	}
}
