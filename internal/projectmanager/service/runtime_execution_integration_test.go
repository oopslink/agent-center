package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func TestRuntimeSnapshotProductionLifecycleIsByteStable(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(t.TempDir() + "/runtime-lifecycle.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	repo := airuntimesql.NewRepository(db)
	n := 0
	catalog := airuntime.NewService(repo, func() string {
		n++
		return fmt.Sprintf("runtime-%d", n)
	})
	model, revision, err := catalog.CreateModel(ctx, "org-1", "user:a", 0, airuntime.ModelDefinition{
		Key: "first", ModelKey: "model-first", DisplayName: "First",
		CompatibleCLIKeys: []string{"codex"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, revision, err := catalog.CreateProfile(ctx, "org-1", "user:a", revision, airuntime.RuntimeProfile{
		Key: "default-first", Name: "Default First", CLIKey: "codex", ModelKey: model.Key, Parameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision, err = catalog.SetDefaultProfile(ctx, "org-1", "user:a", profile.ID, revision); err != nil {
		t.Fatal(err)
	}

	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: idgen.NewGenerator(clk), Clock: clk, AgentDir: allOrgDir("org-1"),
		RuntimeExecutions: airuntime.NewExecutionFreezer(repo),
	})
	projectID, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := svc.CreateTask(ctx, CreateTaskCommand{
		ProjectID: projectID, Title: "execute", CreatedBy: "user:a",
		Assignee: "user:a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartTask(ctx, taskID, "user:a"); err != nil {
		t.Fatal(err)
	}
	before := snapshotBytes(t, db, string(taskID))

	second, revision, err := catalog.CreateModel(ctx, "org-1", "user:a", revision, airuntime.ModelDefinition{
		Key: "second", ModelKey: "model-second", DisplayName: "Second",
		CompatibleCLIKeys: []string{"codex"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondProfile, revision, err := catalog.CreateProfile(ctx, "org-1", "user:a", revision, airuntime.RuntimeProfile{
		Key: "default-second", Name: "Default Second", CLIKey: "codex", ModelKey: second.Key, Parameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SetDefaultProfile(ctx, "org-1", "user:a", secondProfile.ID, revision); err != nil {
		t.Fatal(err)
	}

	// retry/resume: block terminates the attempt and unblock re-dispatches the same
	// logical execution. reassign: ownership changes while the execution id remains.
	if err := svc.BlockTask(ctx, taskID, "retry", pm.BlockReasonObstacle, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.UnblockTask(ctx, UnblockTaskCommand{TaskID: taskID, Comment: "resume", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignTask(ctx, taskID, "user:b", "user:a"); err != nil {
		t.Fatal(err)
	}
	after := snapshotBytes(t, db, string(taskID))
	if !bytes.Equal(before, after) {
		t.Fatalf("snapshot bytes changed across retry/resume/reassign\nbefore=%s\nafter=%s", before, after)
	}
}

func snapshotBytes(t *testing.T, db *sql.DB, executionID string) []byte {
	t.Helper()
	var raw []byte
	if err := db.QueryRow(`SELECT snapshot_json FROM ai_runtime_execution_snapshots WHERE execution_id=?`, executionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), raw...)
}
