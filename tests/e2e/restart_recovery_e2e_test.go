package e2e

// Deployment-level end-to-end verification of the v2.24.0 restart-recovery fix
// (internal/workerdaemon: MarkCompletedTurn + boot_reconcile resume-gating +
// maybeReplyNudge re-trigger). [P46/I59]
//
// It drives REAL processes — NOT unit tests, NOT in-process shortcuts for the
// path under test:
//   - a real `agent-center server` over its real admin unix socket,
//   - a real `agent-center worker run` daemon that spawns a real per-agent
//     supervisor which execs a stand-in `claude` (tests/e2e/cmd/fakeclaude) that
//     speaks the claude stream-json protocol — so the genuine supervisor-session
//     code path (claude_session / agent_controller / boot_reconcile) executes,
//   - a real SIGKILL of the worker + its supervisor/claude children mid-life,
//     and a real restart of the daemon with the SAME config + worker-id + home.
//
// The fake `claude` is placed FIRST on PATH so the production daemon (which has
// no --claude-bin knob at `worker run`) execs it transparently — the supervisor's
// claudestream.BuildStreamingArgv resolves `claude` from PATH.
//
// Asserted from REAL state (the on-disk session.instance JSON + the fakeclaude
// stdin/lifecycle log written across the kill/restart boundary):
//   1. completed_turn=true is PERSISTED to session.instance after a clean turn.
//   2. The worker (and its claude child) are SIGKILLed before the agent replies
//      to a directed DM message.
//   3. After restart, boot-reconcile RELAUNCHES the agent, the relaunch is
//      RESUMED (--resume <prevSessionID> --fork-session) because the prior
//      generation completed a clean turn (resume gating), AND the un-answered
//      directed message is PROACTIVELY re-injected (maybeReplyNudge) so the agent
//      is driven to reply.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// runQuiet runs a command, ignoring its output; returns the run error (if any).
func runQuiet(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func TestE2E_RestartRecovery_DeployLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("deployment-level e2e (spawns real server+worker+supervisor) — skipped in -short")
	}

	// ---- sandbox scaffolding (everything under a unique /tmp dir) -----------
	// Explicitly under /tmp (not $TMPDIR) so ALL runtime state is in one isolated,
	// easy-to-inspect sandbox dir — and provably nowhere near ~/.agent-center or
	// any prod path. The macOS sun_path 104-byte limit on the agent-supervisor
	// socket is sidestepped because that socket lives under the OS temp dir, not
	// here (agentsupervisor.SockPath, v2.7 #178).
	sandboxRoot := "/tmp"
	if err := os.MkdirAll(sandboxRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	sandbox, err := os.MkdirTemp(sandboxRoot, "p46-restart-recovery-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SANDBOX=%s", sandbox)
	// Keep the sandbox on failure for triage; remove on success.
	keep := false
	t.Cleanup(func() {
		if keep || t.Failed() {
			t.Logf("SANDBOX RETAINED for triage: %s", sandbox)
			return
		}
		_ = os.RemoveAll(sandbox)
	})

	bin := ensureBinary(t) // the unified agent-center binary (server + worker)
	binDir := filepath.Join(sandbox, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The stand-in claude MUST be named exactly "claude" and be first on PATH.
	fakeClaude := filepath.Join(binDir, "claude")
	buildBin(t, "github.com/oopslink/agent-center/tests/e2e/cmd/fakeclaude", fakeClaude)

	dbPath := filepath.Join(sandbox, "agent-center.db")
	sock := filepath.Join(sandbox, "admin.sock")
	masterKey := filepath.Join(sandbox, "master.key")
	cfgPath := filepath.Join(sandbox, "config.yaml")
	bootstrapPath := filepath.Join(sandbox, "bootstrap_token")
	// fakeclaude appends its lifecycle/stdin log here; it survives the worker
	// kill/restart so the whole timeline is in ONE file.
	fcLog := filepath.Join(sandbox, "fakeclaude.log")

	if err := writeE2ETestMasterKey(masterKey); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`server:
  listen_addr: "127.0.0.1:0"
  sqlite_path: "%s"
  admin_socket_path: "%s"
secret_management:
  master_key_file: "%s"
  skip_perms_check: true
`, dbPath, sock, masterKey)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// ---- PHASE 1: real server up -------------------------------------------
	srv := spawn(t, bin, []string{"--config=" + cfgPath, "server"},
		[]string{"AGENT_CENTER_INVOCATION_ID="})
	t.Cleanup(func() { srv.sigterm() })
	waitFile(t, sock, 10*time.Second)
	waitFile(t, bootstrapPath, 10*time.Second)
	bootstrap := readTrim(t, bootstrapPath)
	t.Logf("server up; bootstrap token len=%d", len(bootstrap))

	const workerID = "w-p46"
	const agentID = "agent-p46-0001"
	const orgID = "organization-p46aa001"
	const convID = "conv-p46-dm-0001"
	const userRef = "user:tester"
	const msgID = "msg-p46-0000000001" // sortable id

	// Guarantee cleanup of any agent-supervisor (+ its claude child) that
	// setsid-escaped a worker's process group — from EITHER the pre-crash worker
	// or the post-restart relaunch. Registered here (before the worker cleanups),
	// so via t.Cleanup's LIFO order it runs AFTER the workers are SIGKILLed, and it
	// runs on every exit path including panic / t.Fatal / timeout. Without this the
	// post-restart supervisor leaks as an orphan (reparented to launchd). Scoped to
	// this test's sandbox (fakeClaude path + unique agentID) — never touches prod.
	t.Cleanup(func() { reapStrays(t, fakeClaude, agentID) })

	// Worker token (owner worker:<id>) so the daemon's resume-state + reply-nudge
	// calls authorize. The daemon also self-enrolls + mints its own long-term
	// token; this one seeds the enroll handshake.
	workerToken := mintToken(t, sock, bootstrap, "worker:"+workerID, []string{
		"workforce:enroll", "dispatch:pull", "task:*", "secret:resolve", "blob:put",
	})

	// ---- PHASE 2: seed org + agent(running, worker-bound) + EMPTY DM --------
	// Direct SQLite seed (the established e2e shortcut — see v22-deployed-pipeline
	// + cold-start specs). The DISPATCH/recovery itself goes over real processes.
	// The directed MESSAGE is posted LATER (after the agent's first clean turn) so
	// the obligation belongs strictly to the post-restart relaunch — exactly the
	// scenario "agent completes a turn; THEN a new directed message arrives; crash
	// before it replies; restart re-injects it".
	seedBase(t, dbPath, seedParams{
		orgID: orgID, agentID: agentID, workerID: workerID,
		convID: convID, userRef: userRef, msgID: msgID,
	})

	// ---- PHASE 3: real worker daemon up (fake claude first on PATH) ---------
	pathEnv := "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	workerEnv := []string{
		pathEnv,
		"AGENT_CENTER_INVOCATION_ID=",
		"CLAUDE_FAKE_LOG=" + fcLog,
		// generation-0 completes one clean turn at start → completed_turn=true.
		"CLAUDE_FAKE_RESULT_ON_START=1",
		// keep the worker daemon's own state OUT of any prod path: it derives the
		// state dir from cfg.server.sqlite_path's dir (= our sandbox).
	}
	workerArgs := []string{
		"--config=" + cfgPath, "worker", "run",
		"--worker-id=" + workerID,
		"--admin-target=unix:" + sock,
		"--admin-token=" + workerToken,
		"--poll-interval=300ms",
	}
	w1 := spawn(t, bin, workerArgs, workerEnv)
	w1Killed := false
	t.Cleanup(func() {
		if !w1Killed {
			w1.sigkill(t)
		}
	})

	// Bind the self-enrolled worker to the seeded org. Under the controller model the
	// worker MUST org-enroll to control-connect (else 409 worker_not_org_enrolled, v2.7
	// #148/#255) and receive reconcile/work; the bare worker enroll token mints no org.
	// This SQL bind is the admin-socket-harness equivalent of production's org-scoped
	// mint-enroll; it persists across the kill/restart (claimPreEnrolled keeps org).
	orgEnrollWorker(t, dbPath, workerID, orgID)

	// The agent home (where session.instance lands): <sqlite_dir>/agents/<agentID>/.
	agentHome := filepath.Join(sandbox, "agents", agentID)
	instanceFile := filepath.Join(agentHome, "session.instance")

	// ---- ASSERT 1: a clean turn persists completed_turn=true ---------------
	if !waitFor(40*time.Second, func() bool {
		return readCompletedTurn(instanceFile)
	}) {
		t.Fatalf("ASSERT-1 FAILED: completed_turn never became true in %s\n"+
			"session.instance: %s\nfakeclaude.log:\n%s\nworker out:\n%s\nagent dir:\n%s",
			instanceFile, safeRead(instanceFile), safeRead(fcLog), w1.out(), dumpDir(filepath.Join(sandbox, "agents")))
	}
	gen0Instance := safeRead(instanceFile)
	gen0Session := readSessionID(instanceFile)
	t.Logf("ASSERT-1 PASS: completed_turn=true persisted. session.instance(gen0)=%s", gen0Instance)
	if gen0Session == "" {
		t.Fatalf("could not read gen0 session_id from %s", gen0Instance)
	}

	// ---- post a NEW directed DM message the agent has not answered ----------
	// It is perceived (read-state cursor advanced) but undischarged (no agent
	// reply). This is the message that must be re-injected after the crash.
	seedDirectedMessage(t, dbPath, seedParams{
		orgID: orgID, agentID: agentID, workerID: workerID,
		convID: convID, userRef: userRef, msgID: msgID,
	})
	t.Logf("posted unanswered directed DM message %s from %s", msgID, userRef)

	// Confirm the agent has NOT replied to the DM yet (no agent:<id> message row).
	if n := countAgentReplies(t, dbPath, convID, agentID); n != 0 {
		t.Fatalf("precondition: agent already replied (%d msgs) before kill — cannot test re-reply", n)
	}

	// ---- ASSERT 2: SIGKILL the worker (and its claude) mid-life, pre-reply --
	// The agent is alive with a completed clean turn but has NOT answered the
	// directed DM message. Kill the whole tree HARD (no graceful drain) to model
	// a crash. We kill the worker; the supervisor setsid-escapes, so also reap
	// any surviving claude/agent-supervisor by name to model a full-host crash.
	w1.sigkill(t)
	w1Killed = true
	reapStrays(t, fakeClaude, agentID)
	t.Logf("ASSERT-2 PASS: worker SIGKILLed before the agent replied to the directed message")

	// snapshot the fakeclaude log up to the kill (the post-restart lines are what we assert on)
	preRestartLogLen := len(safeRead(fcLog))

	// ---- PHASE 4: restart the worker (SAME config + worker-id + home) -------
	// CRITICAL for isolating fix B: the relaunched generation runs WITHOUT
	// CLAUDE_FAKE_RESULT_ON_START, i.e. it is IDLE on resume (emits no turn until
	// it receives stdin) — exactly like real `claude --resume` with no pending
	// input. This means the ONLY thing that can re-inject the un-answered directed
	// message is boot_reconcile's `go c.maybeReplyNudge(agentID)` (fix B, the
	// boot-relaunch reply-guardrail re-trigger). If the relaunch auto-completed a
	// turn, the ORIGINAL turn-end hook (agent_controller.go maybeReplyNudge) would
	// also fire and ASSERT-3c would pass even with fix B removed — masking it.
	// Negative control (verified by PD): disabling boot_reconcile.go:567 makes
	// ASSERT-3c FAIL under this idle relaunch. Boot-reconcile reads the prior
	// session.instance regardless.
	workerEnv2 := make([]string, 0, len(workerEnv))
	for _, e := range workerEnv {
		if strings.HasPrefix(e, "CLAUDE_FAKE_RESULT_ON_START=") {
			continue // idle relaunch: no auto-turn → turn-end nudge path cannot mask fix B
		}
		workerEnv2 = append(workerEnv2, e)
	}
	w2 := spawn(t, bin, workerArgs, workerEnv2)
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(sandbox, "worker2.out"), []byte(w2.out()), 0o600)
		w2.sigkill(t)
	})

	// ---- ASSERT 3a: the agent-runtime AUTONOMOUSLY relaunches + resumes the session --
	// Controller / process-per-agent model + gap6 fold-in (T860): on restart the
	// agent-runtime process re-boots and AUTONOMOUSLY relaunches the desired-running
	// agent's supervisor session from local durable ResumeState — it does NOT wait for
	// a control reconcile command (the pre-D6 daemon boot_reconcile "RELAUNCHED" string
	// is gone). Double anchor, both in worker stdout (child log prefixed [agent-runtime <id>]):
	//   - `boot-session relaunch agent=<id> resume=true` — proves the relaunch was decided
	//     AUTONOMOUSLY at boot (DecideBootSession → ReapRelaunch), not control-driven. This
	//     is the anchor that would stay red if gap6 were dead-coded (control never fires a
	//     reconcile for an already-desired-running agent — the deadlock this feature fixes).
	//   - `started agent=<id> ... resume=true` — proves the session actually STARTED and
	//     RESUMED the prior generation (resume gated on the prior clean turn).
	relaunchMarker := "boot-session relaunch agent=" + agentID
	startedMarker := "started agent=" + agentID
	if !waitFor(40*time.Second, func() bool {
		out := w2.out()
		return strings.Contains(out, relaunchMarker) &&
			strings.Contains(out, startedMarker) &&
			strings.Contains(out, "resume=true")
	}) {
		t.Fatalf("ASSERT-3a FAILED: agent-runtime did not AUTONOMOUSLY relaunch+resume the supervisor "+
			"session after restart (want %q + %q + resume=true).\nworker2 out:\n%s\nfakeclaude.log:\n%s",
			relaunchMarker, startedMarker, w2.out(), safeRead(fcLog))
	}
	t.Logf("ASSERT-3a PASS: agent-runtime AUTONOMOUSLY RELAUNCHED + RESUMED the supervisor session at boot")

	// ---- ASSERT 3b: the relaunch RESUMED (resume gated on completed_turn) ---
	// The fix: because the prior generation completed a clean turn, boot-reconcile
	// passes resume=true, so the new claude is invoked with `--resume <prevGenId>
	// --fork-session`. The fakeclaude logs RESUME_FROM <id> from its argv.
	if !waitFor(20*time.Second, func() bool {
		return fileContains(fcLog, "RESUME_FROM")
	}) {
		t.Fatalf("ASSERT-3b FAILED: relaunch did NOT resume (no RESUME_FROM in fakeclaude argv) "+
			"despite completed_turn=true — resume gating regressed.\nfakeclaude.log:\n%s\nworker2 out:\n%s",
			safeRead(fcLog), w2.out())
	}
	resumeFrom := grepValue(fcLog, "RESUME_FROM ")
	t.Logf("ASSERT-3b PASS: relaunch RESUMED prior session (RESUME_FROM=%s; gen0 session=%s)", resumeFrom, gen0Session)

	// ---- ASSERT 3c: the un-answered directed message is re-injected ---------
	// maybeReplyNudge fires after the relaunch; the server derives the outstanding
	// directed reply and the daemon injects the nudge prompt into the live session.
	// The fakeclaude receives it on stdin → logs it. This is the agent being
	// PROACTIVELY driven to reply to the message it never answered.
	nudgeMarker := "unanswered directed message"
	if !waitFor(30*time.Second, func() bool {
		log := safeRead(fcLog)
		// only count lines AFTER the restart boundary
		if len(log) <= preRestartLogLen {
			return false
		}
		return strings.Contains(log[preRestartLogLen:], nudgeMarker)
	}) {
		t.Fatalf("ASSERT-3c FAILED: the un-answered directed message was NOT re-injected after restart "+
			"(no reply-nudge reached the relaunched session).\nfakeclaude.log (post-restart):\n%s\nworker2 out:\n%s",
			tailFrom(fcLog, preRestartLogLen), w2.out())
	}
	t.Logf("ASSERT-3c PASS: the un-answered directed message was PROACTIVELY re-injected after restart")

	// ---- summary -----------------------------------------------------------
	t.Logf("\n========== RESULT: PASSED ==========\n"+
		"completed_turn=true persisted: yes (gen0 session=%s)\n"+
		"worker SIGKILLed before reply:  yes\n"+
		"relaunch RESUMED prior session: yes (RESUME_FROM=%s)\n"+
		"directed msg re-injected:       yes (reply-nudge after restart)\n"+
		"====================================", gen0Session, resumeFrom)
}

// TestE2E_RestartRecovery_ReadoptSurvivor is the worker-restart "don't interrupt the
// survivor" counterpart of the relaunch test above (T860 gap6, gap5 survivor invariant).
// When ONLY the worker is SIGKILLed (a worker-only crash/restart, not a full-host crash),
// its agent-runtime PROCESS setsid-ESCAPED and stays ALIVE; the restarted worker must
// RE-ADOPT that surviving process in place — NOT relaunch it and NOT re-exec its claude.
// Behaviorally asserted:
//   - POSITIVE: `re-adopted surviving agent=<id>` (the launcher re-adopts the live process).
//   - NEGATIVE (the "did not re-exec" invariant): after the restart boundary there is NO
//     `boot-session relaunch` and NO new `RESUME_FROM` — no new claude was spawned.
//
// SCOPE NOTE — this covers the LAUNCHER-level survivor invariant (worker restart, agent-
// runtime process alive → re-adopt). The BOOT-SESSION-level reattach (`boot-session
// reattach`: a FRESH agent-runtime process boots and ProbeAgent → Reattachable → reattaches
// a surviving SUPERVISOR) is NOT cleanly triggerable in this CI harness — a worker-only
// restart re-adopts the live agent-runtime PROCESS (observed: `agentlauncher: adopted
// surviving`) so no fresh agent-runtime boot runs the DecideBootSession reattach branch.
// Constructing it needs surgically killing the agent-runtime process while preserving its
// supervisor (fragile, architecture-specific) → deferred to tester3 §6.10 real-machine.
func TestE2E_RestartRecovery_ReadoptSurvivor(t *testing.T) {
	if testing.Short() {
		t.Skip("deployment-level e2e (spawns real server+worker+supervisor) — skipped in -short")
	}

	sandboxRoot := "/tmp"
	if err := os.MkdirAll(sandboxRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	sandbox, err := os.MkdirTemp(sandboxRoot, "p46-reattach-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SANDBOX=%s", sandbox)
	keep := false
	t.Cleanup(func() {
		if keep || t.Failed() {
			t.Logf("SANDBOX RETAINED for triage: %s", sandbox)
			return
		}
		_ = os.RemoveAll(sandbox)
	})

	bin := ensureBinary(t)
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

	if err := writeE2ETestMasterKey(masterKey); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`server:
  listen_addr: "127.0.0.1:0"
  sqlite_path: "%s"
  admin_socket_path: "%s"
secret_management:
  master_key_file: "%s"
  skip_perms_check: true
`, dbPath, sock, masterKey)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// ---- PHASE 1: real server up -------------------------------------------
	srv := spawn(t, bin, []string{"--config=" + cfgPath, "server"},
		[]string{"AGENT_CENTER_INVOCATION_ID="})
	t.Cleanup(func() { srv.sigterm() })
	waitFile(t, sock, 10*time.Second)
	waitFile(t, bootstrapPath, 10*time.Second)
	bootstrap := readTrim(t, bootstrapPath)
	t.Logf("server up; bootstrap token len=%d", len(bootstrap))

	const workerID = "w-p46r"
	const agentID = "agent-p46r-0001"
	const orgID = "organization-p46r001"
	const convID = "conv-p46r-dm-0001"
	const userRef = "user:tester"
	const msgID = "msg-p46r-0000000001"

	// Reap any setsid-escaped survivor at the very end (this test INTENTIONALLY leaves
	// the survivor alive across the restart to exercise reattach, so cleanup MUST reap it).
	t.Cleanup(func() { reapStrays(t, fakeClaude, agentID) })

	workerToken := mintToken(t, sock, bootstrap, "worker:"+workerID, []string{
		"workforce:enroll", "dispatch:pull", "task:*", "secret:resolve", "blob:put",
	})

	seedBase(t, dbPath, seedParams{
		orgID: orgID, agentID: agentID, workerID: workerID,
		convID: convID, userRef: userRef, msgID: msgID,
	})

	// ---- PHASE 2: real worker daemon up ------------------------------------
	pathEnv := "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	workerEnv := []string{
		pathEnv,
		"AGENT_CENTER_INVOCATION_ID=",
		"CLAUDE_FAKE_LOG=" + fcLog,
		"CLAUDE_FAKE_RESULT_ON_START=1",
	}
	workerArgs := []string{
		"--config=" + cfgPath, "worker", "run",
		"--worker-id=" + workerID,
		"--admin-target=unix:" + sock,
		"--admin-token=" + workerToken,
		"--poll-interval=300ms",
	}
	w1 := spawn(t, bin, workerArgs, workerEnv)
	w1Killed := false
	t.Cleanup(func() {
		if !w1Killed {
			w1.sigkill(t)
		}
	})
	orgEnrollWorker(t, dbPath, workerID, orgID)

	agentHome := filepath.Join(sandbox, "agents", agentID)
	instanceFile := filepath.Join(agentHome, "session.instance")

	// ---- ASSERT 1: a clean turn brings up a live supervisor session --------
	if !waitFor(40*time.Second, func() bool {
		return readCompletedTurn(instanceFile)
	}) {
		t.Fatalf("ASSERT-1 FAILED: completed_turn never became true in %s\nfakeclaude.log:\n%s\nworker out:\n%s",
			instanceFile, safeRead(fcLog), w1.out())
	}
	t.Logf("ASSERT-1 PASS: live supervisor session up (completed_turn=true)")

	// ---- kill the WORKER ONLY — leave the setsid-escaped survivor ALIVE -----
	// This is the reattach precondition: unlike the relaunch test (which reapStrays to
	// model a full-host crash), here ONLY the worker dies; the supervisor + claude
	// survive (setsid-escape) so the restarted worker finds a Reattachable session.
	preRestartLogLen := len(safeRead(fcLog))
	w1.sigkill(t)
	w1Killed = true
	t.Logf("worker SIGKILLed; supervisor+claude survivor left ALIVE (reattach precondition)")

	// ---- PHASE 3: restart the worker (SAME config + worker-id + home) -------
	w2 := spawn(t, bin, workerArgs, workerEnv)
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(sandbox, "worker2.out"), []byte(w2.out()), 0o600)
		w2.sigkill(t)
	})

	// ---- ASSERT: the restarted worker RE-ADOPTS the live survivor in place --
	readoptMarker := "re-adopted surviving agent=" + agentID
	if !waitFor(40*time.Second, func() bool {
		return strings.Contains(w2.out(), readoptMarker)
	}) {
		t.Fatalf("ASSERT-Readopt FAILED: restarted worker did not re-adopt the live survivor "+
			"(want %q) — it may have relaunched instead of adopting.\nworker2 out:\n%s\nfakeclaude.log:\n%s",
			readoptMarker, w2.out(), safeRead(fcLog))
	}
	t.Logf("ASSERT-Readopt PASS: worker RE-ADOPTED the surviving agent-runtime in place (%q)", readoptMarker)

	// ---- NEGATIVE invariant: re-adopt did NOT relaunch or re-exec claude ----
	// Re-adopting the live process in place must NOT relaunch (no `boot-session relaunch`)
	// and must NOT spawn a new claude (no new RESUME_FROM after the restart boundary).
	// Give any errant relaunch a moment to appear.
	time.Sleep(2 * time.Second)
	if strings.Contains(w2.out(), "boot-session relaunch agent="+agentID) {
		t.Fatalf("ASSERT-Readopt NEGATIVE FAILED: worker ALSO relaunched (boot-session relaunch present) "+
			"— the live survivor was interrupted instead of adopted.\nworker2 out:\n%s", w2.out())
	}
	postLog := safeRead(fcLog)
	if len(postLog) > preRestartLogLen && strings.Contains(postLog[preRestartLogLen:], "RESUME_FROM") {
		t.Fatalf("ASSERT-Readopt NEGATIVE FAILED: a new claude was spawned after restart (RESUME_FROM appeared post-boundary) "+
			"— re-adopt must keep the running claude, not re-exec.\nfakeclaude.log (post-restart):\n%s",
			postLog[preRestartLogLen:])
	}
	t.Logf("ASSERT-Readopt NEGATIVE PASS: no relaunch, no new claude (survivor adopted in place, not interrupted)")

	t.Logf("\n========== RESULT: PASSED (worker-restart re-adopt survivor) ==========\n"+
		"live survivor across worker restart:       yes\n"+
		"worker RE-ADOPTED (not relaunched):        yes\n"+
		"claude NOT re-exec'd (adopted in place):   yes\n"+
		"boot-session reattach (agent-runtime restart): deferred to tester3 §6.10\n"+
		"=======================================================================")
}

// ---------------------------------------------------------------------------
// seeding
// ---------------------------------------------------------------------------

type seedParams struct {
	orgID, agentID, workerID, convID, userRef, msgID string
}

// orgEnrollWorker binds the self-enrolled worker to the seeded org. The harness mints a
// BARE worker enroll token (no org), so the daemon's workforce.Worker row lands with an
// empty organization_id — under the controller model that makes the worker's
// control-connect fail with 409 worker_not_org_enrolled (v2.7 #148/#255), so it never
// receives reconcile/work. Production binds the org at mint-enroll time (the org install
// command); the admin-socket enroll route does NOT org-stamp, so this SQL bind is the
// harness-equivalent fixture. It is set once after the daemon self-enrolls (the row
// appears shortly after spawn) and survives the kill/restart because the post-restart
// re-enroll takes the claimPreEnrolled path, which does not overwrite organization_id.
func orgEnrollWorker(t *testing.T, dbPath, workerID, orgID string) {
	t.Helper()
	db := openSeedDB(t, dbPath)
	defer db.Close()
	deadline := time.Now().Add(20 * time.Second)
	for {
		res, err := db.Exec(`UPDATE workers SET organization_id = ? WHERE id = ?`, orgID, workerID)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				t.Logf("org-enrolled worker %s → org %s (control-connect precondition)", workerID, orgID)
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("org-enroll: worker row %s never appeared within 20s (last err=%v)", workerID, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func openSeedDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func exec1(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("seed exec failed: %v\nSQL: %s", err, q)
	}
}

// seedBase seeds the org + a RUNNING worker-bound agent + an EMPTY active DM the
// agent participates in. NO directed message yet (that is posted later, after the
// first clean turn). It also lowers the reply-nudge cooldown so the post-restart
// nudge is not throttled by an earlier one.
func seedBase(t *testing.T, dbPath string, p seedParams) {
	t.Helper()
	db := openSeedDB(t, dbPath)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	participants := fmt.Sprintf(
		`[{"identity_id":%q,"role":"member","joined_at":%q,"joined_by":"system"},`+
			`{"identity_id":"agent:%s","role":"member","joined_at":%q,"joined_by":"system"}]`,
		p.userRef, now, p.agentID, now)

	exec1(t, db, `INSERT INTO organizations (id,slug,name,description,created_by_identity_id,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?)`,
		p.orgID, "p46-org", "P46 Org", "", "user:tester", now, now)
	exec1(t, db, `INSERT INTO agents (id,organization_id,name,description,model,cli,worker_id,lifecycle,created_by,identity_member_id,created_at,updated_at,version)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,1)`,
		p.agentID, p.orgID, "AgentP46", "", "", "", p.workerID, "running", "user:tester", "", now, now)
	// status MUST be the domain enum value "active" (ConversationActive) — the
	// reply-obligation Find filter is `status = 'active'`. ("open" silently excludes
	// the conversation → no obligation derived.)
	exec1(t, db, `INSERT INTO conversations (id,kind,status,opened_at,created_at,updated_at,version,name,participants,created_by,organization_id)
		VALUES (?,?,?,?,?,?,1,?,?,?,?)`,
		p.convID, "dm", "active", now, now, now, "p46 dm", participants, "user:tester", p.orgID)
	// Lower the reply-nudge cooldown so a nudge can re-fire promptly after restart.
	exec1(t, db, `INSERT INTO center_settings (key,value,updated_at) VALUES ('reply.nudge_cooldown_sec','1',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, now)

	t.Logf("seeded org=%s agent=%s(running,worker=%s) empty-dm=%s (no directed msg yet)",
		p.orgID, p.agentID, p.workerID, p.convID)
}

// seedDirectedMessage posts ONE user-authored DM message the agent perceives
// (read-state cursor advanced to it) but never replies to — the obligation that
// must be re-injected after the crash/restart.
func seedDirectedMessage(t *testing.T, dbPath string, p seedParams) {
	t.Helper()
	db := openSeedDB(t, dbPath)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Backdate ~1 minute so it is reliably past the 30s idle-grace "perceived"
	// window (belt-and-suspenders with the explicit read-state cursor below) and
	// well within the 1h obligation TTL.
	past := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339Nano)

	exec1(t, db, `INSERT INTO messages (id,conversation_id,sender_identity_id,content_kind,content,direction,posted_at,created_at,context_refs,attachments)
		VALUES (?,?,?,?,?,?,?,?,'{}','[]')`,
		p.msgID, p.convID, p.userRef, "text", "Please confirm you received this and reply.", "inbound", past, past)
	// Mark the message PERCEIVED by the agent (read-state cursor at the msg) so the
	// obligation is deterministic regardless of the idle-grace timer.
	exec1(t, db, `INSERT INTO user_conversation_read_state (user_id,conversation_id,last_seen_message_id,updated_at,version)
		VALUES (?,?,?,?,1) ON CONFLICT(user_id,conversation_id) DO UPDATE SET last_seen_message_id=excluded.last_seen_message_id`,
		"agent:"+p.agentID, p.convID, p.msgID, now)
}

// ---------------------------------------------------------------------------
// state readers
// ---------------------------------------------------------------------------

func readCompletedTurn(instanceFile string) bool {
	b, err := os.ReadFile(instanceFile)
	if err != nil {
		return false
	}
	var st struct {
		CompletedTurn bool `json:"completed_turn"`
	}
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	return st.CompletedTurn
}

func readSessionID(instanceFile string) string {
	b, err := os.ReadFile(instanceFile)
	if err != nil {
		return ""
	}
	var st struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(b, &st) != nil {
		return ""
	}
	return st.SessionID
}

func countAgentReplies(t *testing.T, dbPath, convID, agentID string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	row := db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE conversation_id=? AND sender_identity_id LIKE 'agent:%'`,
		convID)
	if err := row.Scan(&n); err != nil {
		return 0
	}
	return n
}

// reapStrays kills any surviving fakeclaude / agent-supervisor processes for
// THIS test, modeling a full-host crash (the supervisor setsid-escapes the
// worker's process group, so a group-kill of the worker leaves it — and its
// claude child — running).
//
// Both matchers are scoped to this test's sandbox and MUST NOT match anything
// else on the host: on a shared machine, production agent-supervisors (and other
// e2e runs) are live at the same time. The old `pkill -f "worker agent-supervisor"`
// was host-wide and would SIGKILL every production supervisor — do not reintroduce it.
//   - fakeClaude is the sandbox-unique stand-in claude path (<sandbox>/bin/claude).
//   - agentID (e.g. "agent-p46-0001") is globally unique to this test and appears
//     in both the supervisor argv (--agent-id / --home-dir) and its claude child's
//     argv (--home-dir / mcp-config / memory path); it is absent from the server
//     and worker argv, so neither is collateral.
func reapStrays(t *testing.T, fakeClaude, agentID string) {
	t.Helper()
	_ = runQuiet("pkill", "-9", "-f", fakeClaude)
	_ = runQuiet("pkill", "-9", "-f", agentID)
	time.Sleep(300 * time.Millisecond)
}

func safeRead(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "<unreadable: " + err.Error() + ">"
	}
	return string(b)
}

func tailFrom(path string, off int) string {
	s := safeRead(path)
	if off >= len(s) {
		return ""
	}
	return s[off:]
}

// grepValue returns the text following the first occurrence of prefix on its line.
func grepValue(path, prefix string) string {
	for _, line := range strings.Split(safeRead(path), "\n") {
		if i := strings.Index(line, prefix); i >= 0 {
			return strings.TrimSpace(line[i+len(prefix):])
		}
	}
	return ""
}
