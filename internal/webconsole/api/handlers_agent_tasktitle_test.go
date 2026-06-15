package api

import (
	"context"
	"testing"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// v2.7.1 #206: agentWorkItemMap exposes {task_id, task_title, project_id} for the
// AgentDetail work-item rows, each OMITTED when empty so the UI falls back and the
// #192 zero-raw-id invariant holds (no bare task-<hex> rendered).
func TestAgentWorkItemMap_TaskEnrichment(t *testing.T) {
	wi, err := agentbc.NewWorkItem(agentbc.NewWorkItemInput{
		ID: "WI-1", AgentID: agentbc.AgentID("AG-1"),
		TaskRef: "pm://tasks/task-abc12345", CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Resolved → all fields present, incl. the task's org_ref (T100).
	m := agentWorkItemMap(wi, "agent-face01", "task-abc12345", "Build login flow", "proj-x", "T84")
	if m["task_id"] != "task-abc12345" {
		t.Errorf("task_id=%v", m["task_id"])
	}
	if m["task_title"] != "Build login flow" {
		t.Errorf("task_title=%v", m["task_title"])
	}
	if m["project_id"] != "proj-x" {
		t.Errorf("project_id=%v", m["project_id"])
	}
	if m["org_ref"] != "T84" {
		t.Errorf("org_ref=%v, want T84", m["org_ref"])
	}
	if m["task_ref"] != "pm://tasks/task-abc12345" {
		t.Errorf("task_ref=%v", m["task_ref"])
	}

	// Unresolved → the enrichment keys are ABSENT (UI fallback; no raw-id leak).
	m2 := agentWorkItemMap(wi, "agent-face01", "", "", "", "")
	for _, k := range []string{"task_id", "task_title", "project_id", "org_ref"} {
		if _, ok := m2[k]; ok {
			t.Errorf("unresolved: key %q must be omitted, got %v", k, m2[k])
		}
	}
}

// v2.7.1 #206: taskMetaResolver maps a work-item task ref → (taskID, title,
// projectID) read-time via pm GetTask. Positive (seeded task) + negatives
// (unresolvable task → id only / non-matching ref → all empty / nil PM → id only).
func TestTaskMetaResolver(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	ctx := context.Background()
	// T100: the shared harness builds PM without the org-sequence allocator, so
	// CreateTask would leave org_number=0 (→ no org_ref). Rebuild PM with OrgSeq
	// wired so the seeded task gets a real org_number → the resolver yields T<n>.
	deps.PM = pmservice.New(pmservice.Deps{
		DB:       db,
		Projects: pmsql.NewProjectRepo(db),
		Members:  pmsql.NewProjectMemberRepo(db),
		Tasks:    pmsql.NewTaskRepo(db),
		Outbox:   outboxsql.NewOutboxRepo(db),
		IDGen:    idgen.NewGenerator(clock.SystemClock{}),
		Clock:    clock.SystemClock{},
		OrgSeq:   pmsql.NewOrgSequenceRepo(db),
	})
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: "org-x", Name: "P", CreatedBy: "user:u",
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "Build login flow", CreatedBy: "user:u",
	})
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{}
	resolve := s.taskMetaResolver(ctx, deps)

	// Positive: seeded task resolves to its title + owning project + org_ref
	// (T100: first task in org-x → T1).
	if id, title, proj, orgRef := resolve("pm://tasks/" + string(tid)); id != string(tid) || title != "Build login flow" || proj != string(pid) || orgRef != "T1" {
		t.Fatalf("positive resolve = (%q,%q,%q,%q), want (%q,Build login flow,%q,T1)", id, title, proj, orgRef, tid, pid)
	}
	// Unresolvable task → task_id only, empty title/project/org_ref (UI fallback).
	if id, title, proj, orgRef := resolve("pm://tasks/task-deadbeef"); id != "task-deadbeef" || title != "" || proj != "" || orgRef != "" {
		t.Fatalf("missing task = (%q,%q,%q,%q), want (task-deadbeef,,,)", id, title, proj, orgRef)
	}
	// Non-matching ref → all empty.
	for _, ref := range []string{"garbage", "", "pm://tasks/"} {
		if id, title, proj, orgRef := resolve(ref); id != "" || title != "" || proj != "" || orgRef != "" {
			t.Fatalf("ref %q = (%q,%q,%q,%q), want all empty", ref, id, title, proj, orgRef)
		}
	}

	// nil PM → task_id parsed but no title/project/org_ref (graceful).
	resolveNil := s.taskMetaResolver(ctx, HandlerDeps{})
	if id, title, proj, orgRef := resolveNil("pm://tasks/task-1"); id != "task-1" || title != "" || proj != "" || orgRef != "" {
		t.Fatalf("nil PM = (%q,%q,%q,%q), want (task-1,,,)", id, title, proj, orgRef)
	}
}
