package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cli"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// inProcessApp builds a cli.App against the harness DB/config so e2e tests can
// seed data through the still-wired write SERVICES (the CLI management/write
// commands `project add` / `task create` / `issue open` were removed in #132,
// but wfservice/trservice/disservice remain). Writes land in the same SQLite
// file the binary reads, so subsequent `h.run(...)` CLI invocations observe the
// seeded rows + emitted events.
func inProcessApp(t *testing.T, h *harness) (*cli.App, func()) {
	t.Helper()
	cfg, err := config.Load(config.LoadOptions{Path: h.cfgPath})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	db, err := persistence.Open(h.dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("migrate: %v", err)
	}
	app, err := cli.NewApp(cfg, db, clock.SystemClock{})
	if err != nil {
		_ = db.Close()
		t.Fatalf("new app: %v", err)
	}
	return app, func() { _ = db.Close() }
}

// seedProjectE2E creates a workforce Project directly via the service.
func seedProjectE2E(t *testing.T, app *cli.App, id, name string) {
	t.Helper()
	if _, err := app.ProjectSvc.Add(context.Background(), wfservice.AddCommand{
		ID:    workforce.ProjectID(id),
		Name:  name,
		Actor: app.DefaultActor(),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

// seedPMProjectE2E creates a pm.Project directly via the pm project repo. The
// CLI project READ commands (list/show) read the new pm.Project model since
// #131 PR-3, so e2e read assertions on `project show` / `project list` seed
// via pm rather than the workforce service.
func seedPMProjectE2E(t *testing.T, app *cli.App, id, name string) {
	t.Helper()
	p, err := pm.NewProject(pm.NewProjectInput{
		ID:             pm.ProjectID(id),
		OrganizationID: "org-e2e",
		Name:           name,
		CreatedBy:      pm.IdentityRef("user:hayang"),
		CreatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed pm project (NewProject): %v", err)
	}
	if err := app.PMProjectRepo.Save(context.Background(), p); err != nil {
		t.Fatalf("seed pm project (Save): %v", err)
	}
}

// seedWorkerE2E enrolls a worker directly via the service.
func seedWorkerE2E(t *testing.T, app *cli.App, id string) {
	t.Helper()
	if _, err := app.EnrollSvc.Enroll(context.Background(), wfservice.EnrollCommand{
		WorkerID:      workforce.WorkerID(id),
		Capabilities:  []string{"claude-code"},
		ActorIdentity: app.DefaultActor(),
	}); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
}

// seedTaskRuntimeE2E creates a taskruntime Task directly via the TaskService
// (replacing the removed `task create` CLI command). It emits task.created and
// returns the new task id and — when withConversation is true — the
// conversation id created alongside it.
func seedTaskRuntimeE2E(t *testing.T, app *cli.App, projectID, title string, withConversation bool) (taskID, convID string) {
	t.Helper()
	res, err := app.TaskSvc.Create(context.Background(), trservice.TaskCreateInput{
		ProjectID:        projectID,
		Title:            title,
		WithConversation: withConversation,
		Actor:            app.DefaultActor(),
	})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return string(res.TaskID), string(res.ConversationID)
}
