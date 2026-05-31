package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/observability/peek"
	"github.com/oopslink/agent-center/internal/observability/query"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// runHandlerCtx is like runHandler but uses a caller-supplied context so
// long-running handlers (ps --watch / peek-trace --follow) can be cancelled.
func runHandlerCtx(t *testing.T, ctx context.Context, cmd *Command, args []string) (string, string, ExitCode) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	if cmd.Run != nil {
		code := cmd.Run(ctx, args, &outBuf, &errBuf)
		return outBuf.String(), errBuf.String(), code
	}
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	h := cmd.Flags(fs)
	pos, err := permissiveParse(fs, args)
	if err != nil {
		errBuf.WriteString(err.Error())
		return outBuf.String(), errBuf.String(), ExitUsage
	}
	code := h(ctx, pos, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

func TestPsCmd_Watch_TicksAndExitsOnCancel(t *testing.T) {
	app := newTestApp(t)
	prev := PsWatchInterval
	PsWatchInterval = 30 * time.Millisecond
	t.Cleanup(func() { PsWatchInterval = prev })
	cmd := findCmd(app.ObservabilityCommands(), "ps")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	out, _, code := runHandlerCtx(t, ctx, cmd, []string{"--watch"})
	if code != ExitOK {
		t.Fatalf("watch ps code=%d", code)
	}
	if strings.Count(out, "FLEET SNAPSHOT") < 1 {
		t.Fatalf("expected at least one snapshot in output: %s", out)
	}
}

func TestLogsCmd_HappyPath_BlobConfigured(t *testing.T) {
	app := newTestApp(t)
	root := t.TempDir()
	bs, err := blobstore.NewLocalDir(root)
	if err != nil {
		t.Fatal(err)
	}
	app.BlobStore = bs
	deps := query.Deps{PMTasks: pmsql.NewTaskRepo(app.DB)}
	app.LogsSvc = query.NewLogsService(deps, bs)
	// Seed a pm task so the logsTask path can find it.
	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: pm.TaskID("T-1"), ProjectID: "p", Title: "x",
		CreatedBy: "user:t", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pmsql.NewTaskRepo(app.DB).Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	body := []byte("log body\n")
	if err := bs.Put(context.Background(), "tasks/T-1/log.log.gz", bytes.NewReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	cmd := findCmd(app.ObservabilityCommands(), "logs")
	out, _, code := runHandler(t, cmd, []string{"task", "T-1"})
	if code != ExitOK {
		t.Fatalf("logs: %d, out=%s", code, out)
	}
	if !strings.Contains(out, "log body") {
		t.Fatalf("blob content missing: %s", out)
	}
}

func TestLogsCmd_NoBlobStore_Error(t *testing.T) {
	app := newTestApp(t)
	app.BlobStore = nil
	app.LogsSvc = nil
	cmd := findCmd(app.ObservabilityCommands(), "logs")
	_, _, code := runHandler(t, cmd, []string{"task", "T-1"})
	if code != ExitBusinessError {
		t.Fatalf("expected ExitBusinessError, got %d", code)
	}
}

func TestPeekTraceCmd_HappyPath_TailLast(t *testing.T) {
	root := t.TempDir()
	sock := fmt.Sprintf("/tmp/pk_%d_%d.sock", os.Getpid(), rand.Int63())
	t.Cleanup(func() { _ = os.Remove(sock) })
	dir := filepath.Join(root, "E-x")
	_ = os.MkdirAll(dir, 0o755)
	for _, l := range []string{
		`{"type":"thinking","text":"a"}`,
		`{"type":"thinking","text":"b"}`,
	} {
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

	app := newTestApp(t)
	app.Config.Peek.WorkerSocket = sock
	cmd := findCmd(app.ObservabilityCommands(), "peek-trace")
	out, _, code := runHandler(t, cmd, []string{"E-x", "--last=2"})
	if code != ExitOK {
		t.Fatalf("peek-trace happy: code=%d out=%s", code, out)
	}
	if strings.Count(strings.TrimSpace(out), "\n")+1 != 2 {
		t.Fatalf("expected 2 lines: %s", out)
	}
}
