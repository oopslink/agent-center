package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func TestDeleteArchivedProject_CascadesWorkAndConversations(t *testing.T) {
	db := openMigratedTestDB(t)
	ctx := context.Background()
	clk := clock.NewFakeClock(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	projects := pmsql.NewProjectRepo(db)
	members := pmsql.NewProjectMemberRepo(db)
	issues := pmsql.NewIssueRepo(db)
	tasks := pmsql.NewTaskRepo(db)
	plans := pmsql.NewPlanRepo(db)
	svc := New(Deps{
		DB: db, Projects: projects, Members: members, Issues: issues, Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans,
		Outbox: outboxsql.NewOutboxRepo(db), IDGen: idgen.NewGenerator(clk), Clock: clk,
	})
	actor := pm.IdentityRef("user:owner")
	projectID, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "Archived", CreatedBy: actor})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: projectID, Title: "T", CreatedBy: actor})
	if err != nil {
		t.Fatal(err)
	}
	issueID, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: projectID, Title: "I", CreatedBy: actor})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := pm.NewPlan(pm.NewPlanInput{ID: "plan-delete-me", ProjectID: projectID, Name: "P", CreatorRef: actor, CreatedAt: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := plans.Save(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err := plans.RecordDispatch(ctx, plan.ID(), taskID, clk.Now(), "msg-1"); err != nil {
		t.Fatal(err)
	}
	if err := seedOwnedConversation(ctx, db, "conv-task", "task", "pm://tasks/"+string(taskID), string(projectID)); err != nil {
		t.Fatal(err)
	}
	if err := seedOwnedConversation(ctx, db, "conv-issue", "issue", "pm://issues/"+string(issueID), string(projectID)); err != nil {
		t.Fatal(err)
	}
	if err := seedOwnedConversation(ctx, db, "conv-plan", "plan", "pm://plans/"+string(plan.ID()), string(projectID)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO messages (id, conversation_id, sender_identity_id, content_kind, content, direction, posted_at, created_at) VALUES ('msg-task','conv-task','user:owner','text','hello','inbound',?,?)`, tsTest(clk.Now()), tsTest(clk.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_conversation_read_state (user_id, conversation_id, last_seen_message_id, updated_at, version) VALUES ('user:owner','conv-task','msg-task',?,1)`, tsTest(clk.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_conversation_follow_state (user_id, conversation_id, followed, updated_at, version) VALUES ('user:owner','conv-task',1,?,1)`, tsTest(clk.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO conversation_message_reference (id, child_conversation_id, source_conversation_id, source_message_id, created_by, created_at) VALUES ('ref-1','conv-plan','conv-task','msg-task','user:owner',?)`, tsTest(clk.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO authorization_role_assignments (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version) VALUES
		('auth-project', 'org-1', 'user:owner', 'role-project', 'project', ?, 'system', ?, 1),
		('auth-task', 'org-1', 'user:owner', 'role-task', 'task', ?, 'system', ?, 1),
		('auth-plan-project-wildcard', 'org-1', 'user:owner', 'role-plan-project', 'plan', ?, 'system', ?, 1)`,
		string(projectID), tsTest(clk.Now()), string(taskID), tsTest(clk.Now()), "project:"+string(projectID)+":*", tsTest(clk.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO outbox_events (id, event_type, refs, payload, created_at) VALUES ('evt-project-delete-test', 'pm.task.created', ?, '{}', ?)`, `{"project_id":"`+string(projectID)+`","task_id":"`+string(taskID)+`"}`, tsTest(clk.Now())); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteArchivedProject(ctx, projectID, actor); err != pm.ErrProjectNotArchived {
		t.Fatalf("DeleteArchivedProject(active) = %v, want ErrProjectNotArchived", err)
	}
	if err := svc.ArchiveProject(ctx, projectID, actor); err != nil {
		t.Fatalf("ArchiveProject: %v", err)
	}
	if err := svc.DeleteArchivedProject(ctx, projectID, actor); err != nil {
		t.Fatalf("DeleteArchivedProject(archived): %v", err)
	}
	if _, err := projects.FindByID(ctx, projectID); err != pm.ErrProjectNotFound {
		t.Fatalf("FindByID after delete = %v, want ErrProjectNotFound", err)
	}
	for table, want := range map[string]int{
		"pm_tasks": 0, "pm_issues": 0, "pm_plans": 0, "pm_project_members": 0,
		"pm_plan_dispatch_records": 0, "messages": 0, "conversations": 0,
		"user_conversation_read_state": 0, "user_conversation_follow_state": 0,
		"conversation_message_reference": 0,
		"outbox_events":                  0,
	} {
		if got := countRows(t, ctx, db, table); got != want {
			t.Fatalf("%s rows after delete = %d, want %d", table, got, want)
		}
	}
	if got := countMatchingRows(t, ctx, db, `SELECT COUNT(*) FROM authorization_role_assignments WHERE id IN ('auth-project', 'auth-task', 'auth-plan-project-wildcard')`); got != 0 {
		t.Fatalf("project authorization assignment rows after delete = %d, want 0", got)
	}
}

func seedOwnedConversation(ctx context.Context, db *sql.DB, id, kind, ownerRef, projectRef string) error {
	now := tsTest(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	_, err := db.ExecContext(ctx, `INSERT INTO conversations (id, kind, status, opened_at, created_at, updated_at, participants, created_by, organization_id, owner_ref, project_ref) VALUES (?, ?, 'open', ?, ?, ?, '[]', 'system', 'org-1', ?, ?)`, id, kind, now, now, now, ownerRef, projectRef)
	return err
}

func countRows(t *testing.T, ctx context.Context, db *sql.DB, table string) int {
	t.Helper()
	return countMatchingRows(t, ctx, db, fmt.Sprintf("SELECT COUNT(*) FROM %s", table))
}

func countMatchingRows(t *testing.T, ctx context.Context, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func tsTest(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
