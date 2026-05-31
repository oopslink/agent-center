package e2e

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/cli"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
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

