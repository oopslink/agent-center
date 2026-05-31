package integration

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cli"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// TestPhase4_CLIHandlers_FullStack drives the inspect / query / ps / stats
// verbs through cli.App against a real SQLite-backed App stack.
func TestPhase4_CLIHandlers_FullStack(t *testing.T) {
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	app, err := cli.NewApp(cfg, db, clk)
	if err != nil {
		t.Fatal(err)
	}
	// Seed a worker + project + task directly via the still-wired write
	// services (the `project add` / `task create` CLI commands were removed in
	// #132). The real assertions below are on the read-only observability verbs.
	if _, _, code := runHandler(t, findCmd(app.WorkerCommands(), "enroll"), []string{"--worker-id=W-1"}); code != cli.ExitOK {
		t.Fatalf("enroll: %d", code)
	}
	if _, err := app.ProjectSvc.Add(context.Background(), wfservice.AddCommand{
		ID:    workforce.ProjectID("proj"),
		Name:  "Proj",
		Actor: app.DefaultActor(),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := app.TaskSvc.Create(context.Background(), trservice.TaskCreateInput{
		ProjectID:        "proj",
		Title:            "build foo",
		WithConversation: true,
		Actor:            app.DefaultActor(),
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	// inspect / query / ps / stats
	for _, args := range [][]string{
		{"query", "tasks", "--project=proj", "--format=json"},
		{"query", "workers", "--format=json"},
		{"query", "events", "--type=task.", "--format=json"},
		{"ps", "--format=json"},
		{"stats", "--scope=tasks", "--format=json"},
		{"stats", "--scope=events", "--format=json"},
		{"stats", "--scope=workers"},
	} {
		cmd := findCmd(app.ObservabilityCommands(), args[0])
		if cmd == nil {
			t.Fatalf("cmd %s not found", args[0])
		}
		_, errOut, code := runHandler(t, cmd, args[1:])
		if code != cli.ExitOK {
			t.Errorf("%v exit=%d err=%s", args, code, errOut)
		}
	}
}

// TestPhase4_NewApp_BlobStoreWired confirms NewApp wires the LocalDir blob
// store when config.BlobStore.Root is set.
func TestPhase4_NewApp_BlobStoreWired(t *testing.T) {
	path := t.TempDir() + "/test.db"
	db, _ := persistence.Open(path)
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	cfg := config.DefaultConfig()
	cfg.BlobStore.Root = t.TempDir()
	app, err := cli.NewApp(cfg, db, clock.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	if app.BlobStore == nil {
		t.Fatal("BlobStore not wired")
	}
	if app.LogsSvc == nil {
		t.Fatal("LogsSvc not wired")
	}
}

// runHandler mirrors the cli-package test helper (private to that pkg);
// duplicated here for the integration test.
func runHandler(t *testing.T, cmd *cli.Command, args []string) (string, string, cli.ExitCode) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	if cmd.Run != nil {
		code := cmd.Run(context.Background(), args, &outBuf, &errBuf)
		return outBuf.String(), errBuf.String(), code
	}
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	h := cmd.Flags(fs)
	if err := fs.Parse(args); err != nil {
		errBuf.WriteString(err.Error())
		return outBuf.String(), errBuf.String(), cli.ExitUsage
	}
	code := h(context.Background(), fs.Args(), &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

// findCmd locates a leaf by name in a list.
func findCmd(cs []*cli.Command, name string) *cli.Command {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// _ keeps imports stable.
var _ = strings.Contains
var _ sql.DB
