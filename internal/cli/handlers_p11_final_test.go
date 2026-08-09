package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/cognition/memory/teammemory"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	"github.com/oopslink/agent-center/internal/webconsole/sse"
)

// ============================================================================
// runWebConsole — starts the web console, hits /healthz, then shuts down.
// (v2.7 #162: the resolveSecretInput + convTail tests were removed with the
// retired secret/conversation CLI commands.)
// ============================================================================

// newWebConsoleTestApp builds an *App on a SELF-MANAGED temp dir (deliberately
// NOT t.TempDir) so this test owns the teardown ordering.
//
// Flake fix (issue-b06282a2): runWebConsole's cleanup() only *cancels* the ~8
// background loops it starts (fanout / outbox pump / files-GC / wake- and
// plan-reconcile / …) plus srv.Shutdown — it never JOINS those loop goroutines.
// newTestApp additionally never stops the AdminTokenSvc bookkeeping pump. Under
// full-run load those goroutines are still opening the sqlite DB (which recreates
// the WAL "-wal"/"-shm" sidecar files) inside the temp dir at the instant
// t.TempDir's SINGLE-SHOT RemoveAll runs → that RemoveAll fails with ENOTEMPTY
// ("directory not empty") and t.Errorf turns the test false-RED. Isolated
// `-count=1` never hits the timing; only the loaded full run does.
//
// The teardown below removes the race deterministically:
//  1. Stop the AdminToken pump BEFORE closing the DB (mirrors
//     admin_client_testhelper) so it can't write through a torn-down handle.
//  2. db.Close() — sql.DB.Close blocks until every in-flight query returns, so it
//     is the real synchronisation point for the still-cancelling webconsole loops'
//     DB access (after it returns they only ever get "database is closed").
//  3. RemoveAll with bounded backoff so any last -wal/-shm a not-yet-returned loop
//     recreates is cleared once that loop actually exits — no blind sleep, we retry
//     against the real condition (dir removable) instead of guessing a delay.
func newWebConsoleTestApp(t *testing.T) *App {
	t.Helper()
	dir, err := os.MkdirTemp("", "webconsole-test-*")
	if err != nil {
		t.Fatal(err)
	}
	db, err := persistence.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Server.SqlitePath = dir + "/test.db"
	mkPath := dir + "/master.key"
	if err := writeTestMasterKey(mkPath); err != nil {
		t.Fatal(err)
	}
	cfg.SecretManagement.MasterKeyFile = mkPath
	cfg.SecretManagement.SkipPermsCheck = true
	app, err := NewApp(cfg, db, clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	// Registered AFTER the test body's `defer cleanup()` (which stops the
	// webconsole loops); t.Cleanup runs after all defers, so the effective order
	// is cleanup() → this — the pump/DB are quiet before we remove the dir.
	t.Cleanup(func() {
		if app.AdminTokenSvc != nil {
			app.AdminTokenSvc.Close()
		}
		_ = db.Close()
		removeDirWithRetry(t, dir)
	})
	return app
}

// removeDirWithRetry removes dir, retrying on transient ENOTEMPTY/EBUSY caused by
// a background goroutine that recreated a sqlite sidecar file just after db.Close.
// It polls the real condition (dir gone) rather than sleeping a fixed guess.
func removeDirWithRetry(t *testing.T, dir string) {
	t.Helper()
	var err error
	for i := 0; i < 100; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("temp dir cleanup: %v", err)
}

func TestRunWebConsole_StartsAndStops(t *testing.T) {
	app := newWebConsoleTestApp(t)
	bus := sse.NewBus()
	defer bus.Shutdown(context.Background())
	// Grab a free loopback port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	logs := []string{}
	cleanup, err := runWebConsole(context.Background(), app, bus, addr, WebConsoleEnrollWiring{}, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	// Give listener a moment to bind.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never came up: %v", err)
	}
	_ = resp.Body.Close()
}

func TestCenterStartup_ReconcilesTeamMemoryFromGit(t *testing.T) {
	app := newWebConsoleTestApp(t)
	ctx := context.Background()
	tm, err := newHardenedTeamService(app).CreateTeam(ctx, teamservice.CreateTeamInput{
		OrgID: "org-startup-reconcile", Name: "Startup Reconcile",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := centergit.NewTeamMemoryRepository(buildTeamGitHost(app), nil,
		centergit.WithProposalIDGen(func() string { return "startup-proposal" }))
	created, err := repo.Propose(ctx, tm.ID().String(), teammemory.ProposeCommand{
		ActorRef: "agent:startup", IdempotencyKey: "startup/reconcile/1",
		Operation: teammemory.OperationAdd, TargetKind: teammemory.TargetEntry,
		Candidate: &teammemory.Candidate{Slug: "boot-proof", Title: "Boot proof", Description: "restart", Body: "Backfill on boot."},
		Rationale: "prove startup reconciliation",
	})
	if err != nil {
		t.Fatal(err)
	}

	reconcileTeamMemoryFromApp(ctx, app, func(msg string) { t.Log(msg) })

	typ := observability.EventType("team_memory.proposed")
	events, err := app.EventRepo.Find(ctx, observability.EventQueryFilter{
		EventType: &typ,
		Refs:      observability.EventRefsFilter{TeamID: tm.ID().String(), ProposalID: created.ProposalID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("startup-projected events = %d, want 1", len(events))
	}
	var checkpoint string
	if err := app.DB.QueryRowContext(ctx, `SELECT last_projected_commit FROM team_memory_observability_checkpoints WHERE team_id=?`, tm.ID().String()).Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint != created.RepoCommit {
		t.Fatalf("checkpoint = %q, want Git commit %q", checkpoint, created.RepoCommit)
	}
}

type failingStartupReconciler struct{}

func (failingStartupReconciler) ReconcileTeam(context.Context, string) error {
	return errors.New("git unavailable")
}

func TestReconcileTeamMemoryOnStartup_ReportsFailures(t *testing.T) {
	app := newWebConsoleTestApp(t)
	if _, err := newHardenedTeamService(app).CreateTeam(context.Background(), teamservice.CreateTeamInput{
		OrgID: "org-reconcile-error", Name: "Reconcile Error",
	}); err != nil {
		t.Fatal(err)
	}
	var logs []string
	reconcileTeamMemoryOnStartup(context.Background(), newHardenedTeamService(app), failingStartupReconciler{}, func(msg string) {
		logs = append(logs, msg)
	})
	if len(logs) != 1 || !strings.Contains(logs[0], "team memory startup reconcile") || !strings.Contains(logs[0], "git unavailable") {
		t.Fatalf("startup reconcile logs = %#v, want observable Git failure", logs)
	}
}

func TestRunWebConsole_NilApp(t *testing.T) {
	bus := sse.NewBus()
	defer bus.Shutdown(context.Background())
	_, err := runWebConsole(context.Background(), nil, bus, "127.0.0.1:0", WebConsoleEnrollWiring{}, func(string) {})
	if err == nil {
		t.Fatal("expected error for nil app")
	}
}
