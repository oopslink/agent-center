package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeployedBinaryRuntimeVersionSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("deployment-level runtime-version smoke spawns real server/worker/agent-runtime")
	}

	for _, tc := range []struct {
		name                 string
		disableControlStream bool
	}{
		{name: "control-stream-on"},
		{name: "control-stream-off", disableControlStream: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runRuntimeVersionSmoke(t, tc.disableControlStream)
		})
	}
}

func runRuntimeVersionSmoke(t *testing.T, disableControlStream bool) {
	t.Helper()
	sourceBin := ensureBinary(t)
	expected := ensureBinaryIdentity(t)
	if expected.Version == "" || expected.Commit == "" || expected.Branch == "" || expected.BuiltAt == "" {
		t.Fatalf("expected build identity is incomplete: %+v", expected)
	}

	sandbox := shortRuntimeSandbox(t, "rvsmoke-*")
	currentBin := installLikeLayout(t, filepath.Join(sandbox, "install"), expected, sourceBin)

	binDir := filepath.Join(sandbox, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeClaude := filepath.Join(binDir, "claude")
	buildBin(t, "github.com/oopslink/agent-center/tests/e2e/cmd/fakeclaude", fakeClaude)

	dbPath := filepath.Join(sandbox, "agent-center.db")
	sock := filepath.Join(sandbox, "admin.sock")
	masterKey := filepath.Join(sandbox, "master.key")
	cfgPath := filepath.Join(sandbox, "config.yaml")
	bootstrapPath := filepath.Join(sandbox, "bootstrap_token")
	fcLog := filepath.Join(sandbox, "fakeclaude.log")
	webPort, err := pickLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	serverPort, err := pickLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeE2ETestMasterKey(masterKey); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`server:
  listen_addr: "127.0.0.1:%d"
  sqlite_path: "%s"
  admin_socket_path: "%s"
web_console:
  enabled: true
  listen_addr: "127.0.0.1:%d"
secret_management:
  master_key_file: "%s"
  skip_perms_check: true
`, serverPort, dbPath, sock, webPort, masterKey)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	const orgID = "organization-rvsmoke"
	agentID := "agent-rvsmoke-on"
	workerID := "w-rvsmoke-on"
	convID := "conv-rvsmoke-on"
	msgID := "msg-rvsmoke-on-0001"
	if disableControlStream {
		agentID = "agent-rvsmoke-off"
		workerID = "w-rvsmoke-off"
		convID = "conv-rvsmoke-off"
		msgID = "msg-rvsmoke-off-0001"
	}
	sockDir := workerSockDirForTest(workerID)
	_ = os.RemoveAll(sockDir)
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	t.Cleanup(func() { reapStrays(t, fakeClaude, agentID) })

	srv := spawn(t, currentBin, []string{"--config=" + cfgPath, "server"}, []string{"AGENT_CENTER_INVOCATION_ID="})
	t.Cleanup(func() { srv.sigterm() })
	waitFileWithContext(t, sock, 10*time.Second, srv.out)
	waitFileWithContext(t, bootstrapPath, 10*time.Second, srv.out)
	bootstrap := readTrim(t, bootstrapPath)

	seedBase(t, dbPath, seedParams{
		orgID: orgID, agentID: agentID, workerID: workerID,
		convID: convID, userRef: "user:runtime-smoke", msgID: msgID,
	})

	workerToken := mintToken(t, sock, bootstrap, "worker:"+workerID, []string{
		"workforce:enroll", "dispatch:pull", "task:*", "secret:resolve", "blob:put",
	})
	workerArgs := []string{
		"--config=" + cfgPath, "worker", "run",
		"--worker-id=" + workerID,
		"--admin-target=unix:" + sock,
		"--admin-token=" + workerToken,
		"--poll-interval=200ms",
	}
	if disableControlStream {
		workerArgs = append(workerArgs, "--disable-control-stream")
	}
	workerEnv := []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AGENT_CENTER_INVOCATION_ID=",
		"AGENT_CENTER_DISABLE_USAGE_REPORT=1",
		"AGENT_CENTER_CLAUDE_BINARY=" + fakeClaude,
		"CLAUDE_FAKE_LOG=" + fcLog,
		"CLAUDE_FAKE_RESULT_ON_START=1",
	}
	worker := spawn(t, currentBin, workerArgs, workerEnv)
	t.Cleanup(func() { worker.sigkill(t) })
	orgEnrollWorker(t, dbPath, workerID, orgID)

	agentInfo := waitAgentInfo(t, sockDir, agentID, 40*time.Second)
	workerInfo := workerInfoMatchingBuildFromAdmin(t, sock, bootstrap, workerID, expected)
	centerBody := getSystemVersion(t, fmt.Sprintf("http://127.0.0.1:%d", webPort))

	reports := []componentVersionReport{
		reportFromCenterVersion(expected, centerBody),
		reportFromWorkerInfo(expected, expected, workerInfo, worker.cmd.Process.Pid),
		reportFromAgentHealth(expected, expected, agentInfo, worker.cmd.Process.Pid),
	}
	reports[0].Current = expected
	if err := assertRuntimeVersionReports(reports); err != nil {
		t.Fatalf("%v\n--- worker output ---\n%s\n--- agent health ---\n%+v\n--- fakeclaude.log ---\n%s",
			err, worker.out(), agentInfo, safeRead(fcLog))
	}
	if strings.TrimSpace(workerInfo.InstallPath) == "" {
		t.Fatalf("worker did not report install_path; system_info=%+v", workerInfo)
	}
}

func TestDeployedBinaryRuntimeVersion_AdoptedOldRuntimeSkewFails(t *testing.T) {
	if testing.Short() {
		t.Skip("deployment-level runtime-version skew test spawns real server/worker/agent-runtime")
	}

	sandbox := shortRuntimeSandbox(t, "rvskew-*")
	oldID := runtimeBuildIdentity{Version: "skew-old", Commit: "old1234", Branch: "skew", BuiltAt: "2026-08-06T00:00:00Z"}
	newID := runtimeBuildIdentity{Version: "skew-new", Commit: "new1234", Branch: "skew", BuiltAt: "2026-08-06T00:01:00Z"}
	oldBin := buildVariantBinary(t, sandbox, "agent-center-old", oldID)
	newBin := buildVariantBinary(t, sandbox, "agent-center-new", newID)

	installRoot := filepath.Join(sandbox, "install")
	currentOld := installLikeLayout(t, installRoot, oldID, oldBin)

	binDir := filepath.Join(sandbox, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeClaude := filepath.Join(binDir, "claude")
	buildBin(t, "github.com/oopslink/agent-center/tests/e2e/cmd/fakeclaude", fakeClaude)

	dbPath := filepath.Join(sandbox, "agent-center.db")
	sock := filepath.Join(sandbox, "admin.sock")
	masterKey := filepath.Join(sandbox, "master.key")
	cfgPath := filepath.Join(sandbox, "config.yaml")
	bootstrapPath := filepath.Join(sandbox, "bootstrap_token")
	fcLog := filepath.Join(sandbox, "fakeclaude.log")
	webPort, err := pickLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	serverPort, err := pickLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeE2ETestMasterKey(masterKey); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`server:
  listen_addr: "127.0.0.1:%d"
  sqlite_path: "%s"
  admin_socket_path: "%s"
web_console:
  enabled: true
  listen_addr: "127.0.0.1:%d"
secret_management:
  master_key_file: "%s"
  skip_perms_check: true
`, serverPort, dbPath, sock, webPort, masterKey)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	const (
		orgID    = "organization-rvskew"
		agentID  = "agent-rvskew"
		workerID = "w-rvskew"
		convID   = "conv-rvskew"
		msgID    = "msg-rvskew-0001"
	)
	sockDir := workerSockDirForTest(workerID)
	_ = os.RemoveAll(sockDir)
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	t.Cleanup(func() { reapStrays(t, fakeClaude, agentID) })

	// Center is already the new artifact; only the surviving agent-runtime is old.
	srv := spawn(t, newBin, []string{"--config=" + cfgPath, "server"}, []string{"AGENT_CENTER_INVOCATION_ID="})
	t.Cleanup(func() { srv.sigterm() })
	waitFileWithContext(t, sock, 10*time.Second, srv.out)
	waitFileWithContext(t, bootstrapPath, 10*time.Second, srv.out)
	bootstrap := readTrim(t, bootstrapPath)

	seedBase(t, dbPath, seedParams{
		orgID: orgID, agentID: agentID, workerID: workerID,
		convID: convID, userRef: "user:runtime-skew", msgID: msgID,
	})
	workerToken := mintToken(t, sock, bootstrap, "worker:"+workerID, []string{
		"workforce:enroll", "dispatch:pull", "task:*", "secret:resolve", "blob:put",
	})
	workerArgs := []string{
		"--config=" + cfgPath, "worker", "run",
		"--worker-id=" + workerID,
		"--admin-target=unix:" + sock,
		"--admin-token=" + workerToken,
		"--poll-interval=200ms",
	}
	workerEnv := []string{
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AGENT_CENTER_INVOCATION_ID=",
		"AGENT_CENTER_DISABLE_USAGE_REPORT=1",
		"AGENT_CENTER_CLAUDE_BINARY=" + fakeClaude,
		"CLAUDE_FAKE_LOG=" + fcLog,
		"CLAUDE_FAKE_RESULT_ON_START=1",
	}

	wOld := spawn(t, currentOld, workerArgs, workerEnv)
	wOldKilled := false
	t.Cleanup(func() {
		if !wOldKilled {
			wOld.sigkill(t)
		}
	})
	orgEnrollWorker(t, dbPath, workerID, orgID)
	oldInfo := waitAgentInfo(t, sockDir, agentID, 40*time.Second)
	oldPID := parsePIDFile(t, sockDir, agentID)
	if oldInfo.PID != 0 && oldInfo.PID != oldPID {
		t.Fatalf("old runtime pid mismatch: health=%d pidstore=%d health=%+v", oldInfo.PID, oldPID, oldInfo)
	}

	wOld.sigkill(t) // direct worker kill; agent-runtime intentionally survives for adoption.
	wOldKilled = true
	currentNew := installLikeLayout(t, installRoot, newID, newBin)
	wNew := spawn(t, currentNew, workerArgs, workerEnv)
	t.Cleanup(func() { wNew.sigkill(t) })

	readoptMarker := "re-adopted surviving agent=" + agentID
	if !waitFor(40*time.Second, func() bool {
		return strings.Contains(wNew.out(), readoptMarker)
	}) {
		t.Fatalf("new worker did not re-adopt old runtime (want %q)\nworker out:\n%s\nold health:%+v",
			readoptMarker, wNew.out(), oldInfo)
	}
	adoptedInfo := waitAgentInfo(t, sockDir, agentID, 5*time.Second)
	if adoptedInfo.PID != 0 && adoptedInfo.PID != oldPID {
		t.Fatalf("expected adopted survivor pid=%d, got health=%+v", oldPID, adoptedInfo)
	}

	workerInfo := workerInfoMatchingBuildFromAdmin(t, sock, bootstrap, workerID, newID)
	centerBody := getSystemVersion(t, fmt.Sprintf("http://127.0.0.1:%d", webPort))
	reports := []componentVersionReport{
		reportFromCenterVersion(newID, centerBody),
		reportFromWorkerInfo(newID, newID, workerInfo, wNew.cmd.Process.Pid),
		reportFromAgentHealth(newID, newID, adoptedInfo, wNew.cmd.Process.Pid),
	}
	reports[0].Current = newID
	err = assertRuntimeVersionReports(reports)
	if err == nil {
		t.Fatalf("expected adopted old runtime skew to fail; adopted health=%+v worker_info=%+v", adoptedInfo, workerInfo)
	}
	msg := err.Error()
	for _, want := range []string{
		"component=agent-runtime", "expected={version:skew-new", "running={version:skew-old",
		"pid=", "started_at=", "adopted=true", "runtime parent/adopt",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("skew diagnostic missing %q:\n%s", want, msg)
		}
	}
}
