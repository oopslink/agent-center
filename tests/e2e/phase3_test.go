package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// E-01: Full Issue → Bind → Comment ×2 → Conclude(closed_with_tasks) →
// expect 2 tasks spawned with batch-local dep resolved + all 7 events.
func TestE2E_P3_E1_FullIssueConcludeWithTasks(t *testing.T) {
	h := newHarness(t)
	// seed project (slug needs lowercase per workforce rule)
	if _, _, c := h.run("project", "add", "demo", "--name=Demo"); c != 0 {
		t.Fatal("project add")
	}
	// 1. open issue (CLI → lazy create)
	open, code := h.runJSON("issue", "open", "demo", "Test issue", "--description=...")
	if code != 0 {
		t.Fatalf("issue open: %d", code)
	}
	issueID := open["issue_id"].(string)
	// 2. bind --auto (creates kind=issue Conversation)
	bind, code := h.runJSON("issue", "bind-conversation", issueID, "--auto")
	if code != 0 {
		t.Fatalf("bind: %d", code)
	}
	if bind["conversation_id"] == nil {
		t.Fatal("no conv id")
	}
	// 3. comment ×2 (use distinct actors to trigger discussion_started)
	if _, _, c := h.run("issue", "comment", issueID,
		"--content=comment 1", "--actor=user:peer"); c != 0 {
		t.Fatalf("comment 1: %d", c)
	}
	if _, _, c := h.run("issue", "comment", issueID,
		"--content=comment 2", "--actor=user:hayang"); c != 0 {
		t.Fatalf("comment 2: %d", c)
	}
	// 4. conclude with 2 tasks (inline JSON)
	tasksDir := t.TempDir()
	tasksFile := filepath.Join(tasksDir, "tasks.json")
	if err := os.WriteFile(tasksFile, []byte(
		`[{"local_id":"a","title":"Task A"},{"local_id":"b","title":"Task B","depends_on":["a"]}]`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	conc, code := h.runJSON("issue", "conclude", issueID,
		"--resolution=closed_with_tasks", "--summary=go",
		"--spawn-tasks=@"+tasksFile)
	if code != 0 {
		t.Fatalf("conclude: %d", code)
	}
	taskIDsRaw, _ := conc["task_ids"].([]any)
	if len(taskIDsRaw) != 2 {
		t.Fatalf("expected 2 tasks, got %v", taskIDsRaw)
	}
	taskA := taskIDsRaw[0].(string)
	taskB := taskIDsRaw[1].(string)
	// 5. verify DB
	rows := h.queryEvents(t, `SELECT event_type FROM events ORDER BY seq`)
	got := map[string]int{}
	for _, r := range rows {
		got[r["event_type"]]++
	}
	wantCounts := map[string]int{
		"issue.opened":               1,
		"conversation.opened":        1, // from bind --auto
		"conversation.message_added": 3, // 2 user comments + 1 system spawn announce
		"issue.discussion_started":   1,
		"issue.concluded":            1,
		"issue.tasks_spawned":        1,
		"task.created":               2,
	}
	for k, v := range wantCounts {
		if got[k] != v {
			t.Errorf("event %s: got %d want %d (all: %+v)", k, got[k], v, got)
		}
	}
	// task B depends_on contains task A
	taskRows := h.queryEvents(t, `SELECT depends_on_task_ids FROM tasks WHERE id = '`+taskB+`'`)
	if len(taskRows) != 1 {
		t.Fatalf("task B row missing")
	}
	if !strings.Contains(taskRows[0]["depends_on_task_ids"], taskA) {
		t.Fatalf("task B deps don't contain A: %s vs %s", taskRows[0]["depends_on_task_ids"], taskA)
	}
	// issue.status = closed_with_tasks
	issueRows := h.queryEvents(t, `SELECT status, conclusion_summary FROM issues WHERE id = '`+issueID+`'`)
	if len(issueRows) != 1 || issueRows[0]["status"] != "closed_with_tasks" || issueRows[0]["conclusion_summary"] == "" {
		t.Fatalf("issue row: %+v", issueRows)
	}
}

// E-02: open-issue agent verb (lazy-create path, origin=agent_open_issue)
func TestE2E_P3_E2_AgentOpenIssue(t *testing.T) {
	h := newHarness(t)
	if _, _, c := h.run("project", "add", "demo", "--name=Demo"); c != 0 {
		t.Fatal("project add")
	}
	out, _, code := h.run("open-issue", "demo", "Agent-spawned issue",
		"--opened-by=agent:sess-1", "--format=json")
	if code != 0 {
		t.Fatalf("open-issue: %d out=%s", code, out)
	}
	var p map[string]string
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &p)
	if p["issue_id"] == "" {
		t.Fatal("no issue_id")
	}
	rows := h.queryEvents(t, `SELECT origin, opened_by_identity_id FROM issues WHERE id = '`+p["issue_id"]+`'`)
	if len(rows) != 1 {
		t.Fatal("issue row missing")
	}
	if rows[0]["origin"] != "agent_open_issue" {
		t.Fatalf("origin: %s", rows[0]["origin"])
	}
	if rows[0]["opened_by_identity_id"] != "agent:sess-1" {
		t.Fatalf("opener: %s", rows[0]["opened_by_identity_id"])
	}
}

// E-03: Issue Withdraw path
func TestE2E_P3_E3_IssueWithdraw(t *testing.T) {
	h := newHarness(t)
	if _, _, c := h.run("project", "add", "demo", "--name=Demo"); c != 0 {
		t.Fatal("project add")
	}
	open, _ := h.runJSON("issue", "open", "demo", "to be withdrawn")
	issueID := open["issue_id"].(string)
	if _, _, c := h.run("issue", "withdraw", issueID,
		"--reason=duplicate", "--message=dup of #5"); c != 0 {
		t.Fatalf("withdraw: %d", c)
	}
	rows := h.queryEvents(t,
		`SELECT event_type, payload FROM events WHERE event_type = 'issue.withdrawn'`)
	if len(rows) != 1 {
		t.Fatalf("expected 1 issue.withdrawn, got %d", len(rows))
	}
	if !strings.Contains(rows[0]["payload"], `"reason":"duplicate"`) ||
		!strings.Contains(rows[0]["payload"], `"message":"dup of #5"`) {
		t.Fatalf("payload missing reason/message: %s", rows[0]["payload"])
	}
}

// E-04: Issue conclude closed_no_action — no task.created / no tasks_spawned
func TestE2E_P3_E4_ConcludeNoAction(t *testing.T) {
	h := newHarness(t)
	if _, _, c := h.run("project", "add", "demo", "--name=Demo"); c != 0 {
		t.Fatal("project add")
	}
	open, _ := h.runJSON("issue", "open", "demo", "skip me")
	issueID := open["issue_id"].(string)
	if _, _, c := h.run("issue", "conclude", issueID,
		"--resolution=closed_no_action", "--summary=skip"); c != 0 {
		t.Fatalf("conclude: %d", c)
	}
	rows := h.queryEvents(t, `SELECT event_type FROM events ORDER BY seq`)
	for _, r := range rows {
		if r["event_type"] == "task.created" || r["event_type"] == "issue.tasks_spawned" {
			t.Fatalf("unexpected event %s", r["event_type"])
		}
	}
	issueRows := h.queryEvents(t, `SELECT status FROM issues WHERE id = '`+issueID+`'`)
	if issueRows[0]["status"] != "closed_no_action" {
		t.Fatalf("status: %s", issueRows[0]["status"])
	}
}

// E-05: Conclude failure rollback (dep ref to non-existent task uuid)
func TestE2E_P3_E5_ConcludeBadDepRollsBack(t *testing.T) {
	h := newHarness(t)
	if _, _, c := h.run("project", "add", "demo", "--name=Demo"); c != 0 {
		t.Fatal("project add")
	}
	open, _ := h.runJSON("issue", "open", "demo", "with bad dep")
	issueID := open["issue_id"].(string)
	inline := `[{"local_id":"a","title":"A","depends_on":["01H_THIS_IS_NOT_A_REAL_TASK"]}]`
	// The dep is treated as batch-local first (no match) → unknown local_id err
	// from spec validation pre-tx (cycle / unknown local). Either way: exit non-zero.
	_, _, code := h.run("issue", "conclude", issueID,
		"--resolution=closed_with_tasks", "--summary=g",
		"--spawn-tasks="+inline)
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	// Issue should remain open
	rows := h.queryEvents(t, `SELECT status FROM issues WHERE id = '`+issueID+`'`)
	if rows[0]["status"] != "open" {
		t.Fatalf("status should remain open: %s", rows[0]["status"])
	}
	// No task.created / issue.concluded / issue.tasks_spawned
	bad := h.queryEvents(t,
		`SELECT event_type FROM events WHERE event_type IN ('task.created','issue.concluded','issue.tasks_spawned')`)
	if len(bad) != 0 {
		t.Fatalf("unexpected events after rollback: %v", bad)
	}
}
