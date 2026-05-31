// Package e2e drives the agent-center binary end-to-end and asserts
// CLI surface contracts (plan § 5.3): exit codes, JSON shapes, event
// emission, mode-stub messages.
//
// Each test compiles a fresh binary into the temp dir, writes a config
// file pointing at a temp SQLite DB, and execs the binary via os/exec.
package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

// ensureBinary builds cmd/agent-center once per test process. Subsequent
// tests share the same compiled binary.
func ensureBinary(t *testing.T) string {
	t.Helper()
	binaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "agent-center-e2e-*")
		if err != nil {
			binaryErr = err
			return
		}
		binaryPath = filepath.Join(dir, "agent-center")
		cmd := exec.Command("go", "build", "-o", binaryPath, "github.com/oopslink/agent-center/cmd/agent-center")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			binaryErr = fmt.Errorf("go build: %w", err)
		}
	})
	if binaryErr != nil {
		t.Skipf("go build not available: %v", binaryErr)
	}
	return binaryPath
}

type harness struct {
	t       *testing.T
	binary  string
	cfgPath string
	dbPath  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	bin := ensureBinary(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	dbPath := filepath.Join(dir, "test.db")
	// v2.6: webconsole requires master_key for Identity BC auth (JWT signing).
	mkPath := filepath.Join(dir, "master.key")
	if err := writeE2ETestMasterKey(mkPath); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(
		"server:\n  listen_addr: ':7000'\n  sqlite_path: '%s'\n  admin_socket_path: '%s/admin.sock'\nidentity:\n  default_user: hayang\nsecret_management:\n  master_key_file: '%s'\n  skip_perms_check: true\n",
		dbPath, dir, mkPath,
	)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, binary: bin, cfgPath: cfgPath, dbPath: dbPath}
}

// writeE2ETestMasterKey writes a deterministic base64-encoded 32-byte AES key.
func writeE2ETestMasterKey(path string) error {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	b64 := base64.StdEncoding.EncodeToString(key)
	return os.WriteFile(path, []byte(b64+"\n"), 0600)
}

func (h *harness) run(args ...string) (stdout, stderr string, code int) {
	h.t.Helper()
	allArgs := append([]string{"--config=" + h.cfgPath}, args...)
	cmd := exec.Command(h.binary, allArgs...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		h.t.Fatalf("exec failed: %v\nstderr: %s", err, errBuf.String())
	}
	if exitErr != nil {
		code = exitErr.ExitCode()
	}
	return outBuf.String(), errBuf.String(), code
}

func (h *harness) runJSON(args ...string) (data map[string]any, code int) {
	h.t.Helper()
	stdout, _, code := h.run(append(args, "--format=json")...)
	if code != 0 {
		return nil, code
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &data); err != nil {
		h.t.Fatalf("unmarshal: %v\nstdout: %s", err, stdout)
	}
	return data, code
}

func (h *harness) runJSONArray(args ...string) (data []map[string]any, code int) {
	h.t.Helper()
	stdout, _, code := h.run(append(args, "--format=json")...)
	if code != 0 {
		return nil, code
	}
	trim := strings.TrimSpace(stdout)
	if trim == "" || trim == "null" {
		return nil, code
	}
	if err := json.Unmarshal([]byte(trim), &data); err != nil {
		h.t.Fatalf("unmarshal: %v\nstdout: %s", err, stdout)
	}
	return data, code
}

// =============================================================================
// E2E-1: worker enroll → list → status
// =============================================================================

func TestE2E1_WorkerEnrollListStatus(t *testing.T) {
	h := newHarness(t)
	stdout, _, code := h.run("worker", "enroll", "--worker-id=W-1")
	if code != 0 {
		t.Fatalf("enroll: code=%d stdout=%s", code, stdout)
	}
	arr, _ := h.runJSONArray("worker", "list")
	found := false
	for _, w := range arr {
		if w["worker_id"] == "W-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("worker not in list: %v", arr)
	}
	res, code := h.runJSON("worker", "status", "W-1")
	if code != 0 {
		t.Fatalf("status: %d", code)
	}
	if res["worker_id"] != "W-1" {
		t.Fatalf("status: %v", res)
	}
}

// =============================================================================
// E2E-4: conversation open → add → read
// =============================================================================

func TestE2E4_ConversationOpenAddRead(t *testing.T) {
	h := newHarness(t)
	res, code := h.runJSON("conversation", "open", "--kind=dm", "--name=DM")
	if code != 0 {
		t.Fatalf("open: %d", code)
	}
	cid := res["conversation_id"].(string)
	for i := 0; i < 3; i++ {
		_, _, c := h.run("conversation", "add-message", cid,
			"--kind=text", "--content=hi"+fmt.Sprint(i), "--direction=internal")
		if c != 0 {
			t.Fatalf("add-message #%d: %d", i, c)
		}
	}
	msgs, _ := h.runJSONArray("conversation", "read", cid)
	if len(msgs) != 3 {
		t.Fatalf("got %d", len(msgs))
	}
}

// =============================================================================
// E2E-5: worker enroll → events table contains worker.enrolled row
// =============================================================================

func TestE2E5_EnrollEmitsEvent(t *testing.T) {
	h := newHarness(t)
	_, _, c := h.run("worker", "enroll", "--worker-id=W-1")
	if c != 0 {
		t.Fatal()
	}
	rows := h.queryEvents(t, `SELECT event_type, refs, actor FROM events WHERE event_type = 'workforce.worker.enrolled'`)
	if len(rows) != 1 {
		t.Fatalf("expected 1 worker.enrolled, got %d", len(rows))
	}
	if !strings.Contains(rows[0]["refs"], `"worker_id":"W-1"`) {
		t.Fatalf("refs: %s", rows[0]["refs"])
	}
	if !strings.Contains(rows[0]["actor"], "user:hayang") {
		t.Fatalf("actor: %s", rows[0]["actor"])
	}
}

// =============================================================================
// E2E-7: conversation flow → 2 events (opened + message_added)
// =============================================================================

func TestE2E7_ConversationEvents(t *testing.T) {
	h := newHarness(t)
	res, _ := h.runJSON("conversation", "open", "--kind=dm")
	cid := res["conversation_id"].(string)
	if _, _, c := h.run("conversation", "add-message", cid,
		"--kind=text", "--content=hi", "--direction=internal"); c != 0 {
		t.Fatal()
	}
	rows := h.queryEvents(t, `SELECT event_type FROM events WHERE event_type LIKE 'conversation.%' ORDER BY seq`)
	if len(rows) != 2 {
		t.Fatalf("got %d rows: %v", len(rows), rows)
	}
	if rows[0]["event_type"] != "conversation.opened" {
		t.Fatalf("event[0]: %s", rows[0]["event_type"])
	}
	if rows[1]["event_type"] != "conversation.message_added" {
		t.Fatalf("event[1]: %s", rows[1]["event_type"])
	}
}

// =============================================================================
// E2E-8: server start → SIGTERM → clean exit
// =============================================================================

// TestE2E8_ServerStartSIGTERM exercises GRACEFUL shutdown: the server must
// react to a real SIGTERM by running its shutdown path (it logs "shutting
// down"), NOT be hard-killed. #129: the prior version used exec.CommandContext
// with no cmd.Cancel, so ctx-cancel sent SIGKILL (despite the name/comment) →
// Shutdown never ran and the assertion merely hoped the startup banner had
// flushed before the hard kill (flaky). The fix: (1) cmd.Cancel sends a real
// SIGTERM (cmd.WaitDelay force-kills if graceful hangs); (2) readiness is a
// race-free poll of the admin socket file (created when the server is up),
// not a fixed sleep + concurrent read of the stdout buffer; (3) assert the
// graceful "shutting down" log — which is what the name claims and what
// exercises the #128 listener-shutdown path.
func TestE2E8_ServerStartSIGTERM(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.binary, "--config="+h.cfgPath, "server")
	// Real SIGTERM on cancel (default ctx-cancel = SIGKILL, which skips
	// graceful shutdown). WaitDelay force-kills if graceful teardown hangs.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if startErr := cmd.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	// Race-free readiness: the admin unix socket is created when the server's
	// admin listener is up. Poll the file (NOT the stdout buffer, which the
	// os/exec copy goroutine writes concurrently).
	sock := filepath.Join(filepath.Dir(h.cfgPath), "admin.sock")
	readyDeadline := time.Now().Add(10 * time.Second)
	for {
		if _, statErr := os.Stat(sock); statErr == nil {
			break
		}
		if time.Now().After(readyDeadline) {
			cancel()
			_ = cmd.Wait()
			t.Fatalf("server never became ready (admin socket %s absent)\nstderr: %s", sock, errb.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Graceful SIGTERM.
	cancel()
	waitErr := cmd.Wait()
	if waitErr != nil {
		var exitErr *exec.ExitError
		// context.Canceled is expected: os/exec returns it when the process is
		// interrupted via cmd.Cancel (our SIGTERM), even on a graceful exit.
		if !errors.As(waitErr, &exitErr) && !errors.Is(waitErr, context.Canceled) {
			t.Fatalf("wait: %v stderr: %s", waitErr, errb.String())
		}
	}
	// out is read only after Wait() (copy goroutine done) → no race.
	if !strings.Contains(out.String(), "shutting down") {
		t.Fatalf("expected graceful 'shutting down' on SIGTERM, got stdout: %q\nstderr: %q", out.String(), errb.String())
	}
}

// =============================================================================
// E2E-9: supervisor command removed in v2.6 (BE-9 supervisor cut).
// =============================================================================

// TestE2E9_SupervisorRemoved verifies the supervisor subcommand is gone in v2.6.
// The CLI router shows help and exits 0 for unknown commands; the test checks
// that "supervisor" is NOT listed as a command in the help output.
func TestE2E9_SupervisorRemoved(t *testing.T) {
	h := newHarness(t)
	helpOut, _, _ := h.run("help")
	// supervisor must not appear as a top-level command in the help tree.
	if strings.Contains(helpOut, "  supervisor ") {
		t.Fatalf("supervisor still listed as a command in help; output:\n%s", helpOut)
	}
}

// TestE2E9_WorkerRunRequiresWorkerID verifies `worker run` now routes to the real
// daemon handler (v2.7 (b) unified binary) and requires --worker-id, replacing the
// old not-implemented stub. Assertion owned by Tester (msg 2ce24698): no
// --worker-id → exit 2 (ExitUsage) + stderr "--worker-id is required".
func TestE2E9_WorkerRunRequiresWorkerID(t *testing.T) {
	h := newHarness(t)
	_, errOut, code := h.run("worker", "run")
	if code != 2 {
		t.Fatalf("expected exit 2 (ExitUsage), got %d", code)
	}
	if !strings.Contains(errOut, "--worker-id is required") {
		t.Fatalf("stderr: %s", errOut)
	}
}

func TestE2E9_AdminBlobMigrateStub(t *testing.T) {
	h := newHarness(t)
	_, errOut, code := h.run("admin", "blob-migrate")
	if code != 64 {
		t.Fatalf("expected exit 64, got %d", code)
	}
	if !strings.Contains(errOut, "not_implemented_in_phase_1") {
		t.Fatalf("stderr: %s", errOut)
	}
}

// =============================================================================
// E2E-10: config fail-fast on malformed YAML
// =============================================================================

func TestE2E10_BadConfigFailFast(t *testing.T) {
	h := newHarness(t)
	// Overwrite the config file with malformed YAML.
	if err := os.WriteFile(h.cfgPath, []byte("server:\n  unknown_field: yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errOut, code := h.run("worker", "enroll", "--worker-id=W-1")
	if code != 1 && code != 2 {
		t.Fatalf("expected non-zero exit, got %d", code)
	}
	if !strings.Contains(errOut, "config") && !strings.Contains(errOut, "unknown") {
		t.Fatalf("stderr: %s", errOut)
	}
}

// =============================================================================
// E2E: version + migrate
// =============================================================================

func TestE2E_Version(t *testing.T) {
	h := newHarness(t)
	stdout, _, code := h.run("version")
	if code != 0 {
		t.Fatal()
	}
	if !strings.Contains(stdout, "agent-center") {
		t.Fatalf("got: %s", stdout)
	}
}

func TestE2E_MigrateTwice(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 2; i++ {
		_, _, c := h.run("migrate")
		if c != 0 {
			t.Fatalf("migrate #%d failed", i)
		}
	}
}

func TestE2E_Help(t *testing.T) {
	h := newHarness(t)
	stdout, _, code := h.run("--help")
	if code != 0 {
		t.Fatal()
	}
	if !strings.Contains(stdout, "worker") || !strings.Contains(stdout, "project") {
		t.Fatalf("help: %s", stdout)
	}
}

// queryEvents runs the given SQL against the harness DB and returns rows.
//
// Uses a separate sqlite connection so we don't interfere with the binary
// being tested.
func (h *harness) queryEvents(t *testing.T, query string) []map[string]string {
	t.Helper()
	return queryDB(t, h.dbPath, query)
}
