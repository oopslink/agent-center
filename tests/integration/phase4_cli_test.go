package integration

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cli"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/persistence"
)

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
