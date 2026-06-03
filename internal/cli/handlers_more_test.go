package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
)

func TestCLI_WorkerEnroll_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "enroll")
	stdout, _, code := run([]string{"--worker-id=W-1"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(stdout, "enrolled worker W-1") {
		t.Fatalf("stdout: %s", stdout)
	}
}

func TestCLI_WorkerList_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "enroll")
	_, _, _ = run([]string{"--worker-id=W-1"})
	listRun := runByName(t, app, "worker", "list")
	stdout, _, code := listRun([]string{})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(stdout, "W-1") {
		t.Fatalf("stdout: %s", stdout)
	}
}

func TestCLI_WorkerList_StatusFilter(t *testing.T) {
	app := newTestApp(t)
	enroll := runByName(t, app, "worker", "enroll")
	_, _, _ = enroll([]string{"--worker-id=W-1"})
	listRun := runByName(t, app, "worker", "list")
	stdout, _, _ := listRun([]string{"--status=offline", "--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(stdout), &arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

func TestCLI_WorkerStatus_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	enroll := runByName(t, app, "worker", "enroll")
	_, _, _ = enroll([]string{"--worker-id=W-1"})
	status := runByName(t, app, "worker", "status")
	stdout, _, code := status([]string{"W-1"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(stdout, "W-1") || !strings.Contains(stdout, "offline") {
		t.Fatalf("stdout: %s", stdout)
	}
}

func TestWorkerToMap_WithHeartbeat(t *testing.T) {
	w, _ := workforce.NewWorker(workforce.NewWorkerInput{
		ID: "W-1", EnrolledAt: timeNow(),
	})
	_ = w.Heartbeat(timeNow(), 30)
	m := workerToMap(w)
	if _, ok := m["last_heartbeat_at"]; !ok {
		t.Fatal("expected last_heartbeat_at field")
	}
}

// TestLazyApp_Build_BadConfig confirms the lazy build path surfaces a
// config error (covers withApp + build).
func TestLazyApp_Build_BadConfig(t *testing.T) {
	provider := &lazyApp{cfgPath: "/nonexistent.yaml"}
	if _, err := provider.build(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLazyApp_BuildOK(t *testing.T) {
	// Write a valid config + non-existent DB path → lazy.build opens
	// and migrates successfully.
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	dbPath := dir + "/x.db"
	_ = writeFileImpl(cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '"+dbPath+"'\n")
	provider := &lazyApp{cfgPath: cfgPath}
	app, err := provider.build()
	if err != nil {
		t.Fatal(err)
	}
	defer app.DB.Close()
	// Quick health check.
	if app.WorkerRepo == nil {
		t.Fatal("WorkerRepo nil")
	}
}

// End-to-end through BuildRouter + Run, using a real config + DB. Covers
// withApp wrapper's runtime path.
func TestBuildRouter_LazyResourceCmd(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	dbPath := dir + "/x.db"
	_ = writeFileImpl(cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '"+dbPath+"'\n")
	args := []string{"--config=" + cfgPath, "worker", "enroll", "--worker-id=W-1", "--format=json"}
	router, _, err := BuildRouter("v", "c", args)
	if err != nil {
		t.Fatal(err)
	}
	code := router.Run(context.Background(), StripGlobalFlags(args, cfgPath))
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
}

func TestBuildRouter_LazyResourceCmd_BadConfig(t *testing.T) {
	args := []string{"--config=/nonexistent.yaml", "worker", "enroll", "--worker-id=W-1"}
	router, _, _ := BuildRouter("v", "c", args)
	code := router.Run(context.Background(), StripGlobalFlags(args, ""))
	if code == ExitOK {
		t.Fatal("expected non-zero exit")
	}
}

func TestLazyApp_Build_BadDBPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	_ = writeFileImpl(cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '/proc/no/such/file'\n")
	provider := &lazyApp{cfgPath: cfgPath}
	_, err := provider.build()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFindCmd_NotFound(t *testing.T) {
	cs := []*Command{{Name: "a"}, {Name: "b"}}
	if findCmd(cs, "c") != nil {
		t.Fatal()
	}
	if findCmd(cs, "a") == nil {
		t.Fatal()
	}
}

func TestEmitConfigErrors_PlainErr(t *testing.T) {
	var buf strings.Builder
	emitConfigErrors(&buf, errIO("io fail"))
	if !strings.Contains(buf.String(), "io fail") {
		t.Fatalf("got %s", buf.String())
	}
}

type errIO string

func (e errIO) Error() string { return string(e) }

func timeNow() time.Time {
	return time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
}

var _ = io.Discard
var _ = context.Background
