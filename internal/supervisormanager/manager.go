// Package supervisormanager is the daemon-side orchestration of the persistent
// per-agent supervisors built in slice D2-f s1/s2. It is the logic a worker
// daemon uses to SPAWN, PROBE (attach / re-attach decision), REAP residual, and
// LOCK per-agent supervisors so a daemon crash/restart NEVER kills the agent's
// claude.
//
// WHY A SEPARATE PACKAGE (not internal/workerdaemon). internal/agentsupervisor
// already imports internal/workerdaemon (it reuses the claude stream parser +
// argv builder), so a supervisor manager living IN workerdaemon and importing
// agentsupervisor would form an import cycle. This package sits one layer up: it
// imports agentsupervisor (for AttachClient / Hello / the artifact names) and is
// itself imported by the daemon wiring in s3b. It is purely
// additive and NOT wired into the AgentController yet (that is s3b).
//
// THE FOUR PM FOCI this package gets right:
//   - detach-not-kill (Detach/DetachAll): the new control-loop shutdown path
//     closes attach clients + releases lockfiles but NEVER signals the supervisor
//     or claude — they outlive the daemon. (Contrast the OLD direct-claude path
//     which SIGKILLs sessions.)
//   - reap-single-instance (ReapResidual): before a mode-B relaunch we killpg the
//     RECORDED supervisor+claude pids so we never end up with two claudes for one
//     agent.
//   - home lockfile (AcquireHomeLock): flock(LOCK_EX|LOCK_NB) so two daemons
//     cannot both relaunch the same agent (which would double the claude).
//   - no version gate (v2.7): a live, identity-matched supervisor is ALWAYS
//     Reattachable regardless of its advertised protocol version — the protocol is
//     assumed backward-compatible. (See ProbeAgent + protocol.go for the deferred
//     breaking-change trigger that would reinstate a guard.)
package supervisormanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
)

// SpawnSupervisorCfg is the input to SpawnSupervisor. It mirrors the
// `worker agent-supervisor` subcommand flags (handlers_agentsupervisor.go).
type SpawnSupervisorCfg struct {
	// AgentID is the agent this supervisor owns (--agent-id, required).
	AgentID string
	// HomeDir is the per-agent home for artifacts (--home-dir, required). The
	// daemon resolves it from agentPaths() (AgentHomeBase/workers/{worker}/agents/{agent}).
	HomeDir string
	// MCPConfigPath is the daemon-generated mcp-config file path (--mcp-config-path,
	// optional). The supervisor never holds the token; it just points claude at it.
	MCPConfigPath string
	// TasksDir is claude's working directory (--workspace-dir, the agent
	// tasks directory). Empty → claude inherits the supervisor's cwd. The daemon
	// resolves it to <home>/tasks.
	TasksDir string
	// BinaryPath is the agent-center executable to exec as the supervisor. Empty
	// → os.Executable() (the running daemon binary, which carries the subcommand).
	BinaryPath string
	// Model is an optional claude --model override.
	Model string
	// DisplayName is the agent's human-readable display_name (--display-name). The
	// supervisor injects it as GIT_{AUTHOR,COMMITTER}_NAME via the ② AgentEnv seam
	// so commit authorship reads as the display_name instead of the ULID AgentID
	// (T469). Empty → the supervisor omits the flag → NAME falls back to the AgentID.
	DisplayName string
	// AgentEnv is the per-agent profile env overlay. It is written to a 0600 file
	// under HomeDir and the supervisor receives only the file path.
	AgentEnv map[string]string
	// PromptDescription is the already-gated description text (--prompt-description).
	// The supervisor injects it into the system prompt as a persona段 (T728). Empty →
	// the supervisor omits the flag → no injection.
	PromptDescription string
	// ClaudeBin overrides the claude binary path (--claude-bin). In tests this
	// points at a stand-in so no real claude is required.
	ClaudeBin string
	// Epoch is the agent's durable reset epoch (--reset-epoch). It derives claude's
	// --session-id (SessionUUIDGen(agentID, epoch, generation)). The DAEMON resolves
	// it before spawning: ReadEpoch(home) for a normal spawn / crash-relaunch (so a
	// relaunch resumes the SAME session, never silently resetting to 0), and the
	// post-bump value for a clean-slate reset. 0 = the initial epoch. Only emitted in
	// the argv when > 0 (0 == the subcommand default).
	Epoch int
	// Generation is the agent's crash-relaunch fork generation (--generation, v2.7
	// GATE-7 Mode-B). It derives claude's --session-id together with Epoch. The DAEMON
	// bumps it per Mode-B relaunch (BumpGenerationForRelaunch) so a relaunch forks a
	// fresh, never-locked session-id instead of re-using the killed one. 0 = the
	// pre-fix id (initial/normal start). Only emitted in the argv when > 0.
	Generation int
	// ResumeFromSessionID is the Mode-B fork source (--resume-from, v2.7 GATE-7): the
	// prior (killed, possibly lock-held) session-id to `--resume … --fork-session`
	// from, so the relaunched claude inherits the conversation under the NEW
	// --session-id. Empty = a plain start with no fork (initial/normal start).
	ResumeFromSessionID string
	// ConcurrencyEnabled indicates whether this agent runs in concurrent mode
	// (--concurrency-enabled). When true the supervisor selects the orchestrator
	// system prompt instead of the single-task work-queue prompt.
	ConcurrencyEnabled bool
	// ComeUpTimeout bounds how long SpawnSupervisor waits for the supervisor to
	// listen on its socket and answer Hello. Zero → defaultComeUpTimeout.
	ComeUpTimeout time.Duration
}

const defaultComeUpTimeout = 15 * time.Second

// supervisorLogMaxBytes bounds the per-agent supervisor.log. The supervisor is a
// DETACHED process the daemon never Wait()s, so a respawn churn (crash-loop) could
// otherwise grow this file without bound. On spawn, a log already at/over the cap is
// TRUNCATED (fresh) rather than appended — keeping the most recent crash tail.
const supervisorLogMaxBytes = 4 << 20 // 4 MiB

// openSupervisorLog opens <homeDir>/supervisor.log to capture the spawned
// supervisor's stdout+stderr. It APPENDS so successive supervisor instances
// accumulate behind spawn markers, unless the file has reached supervisorLogMaxBytes
// (then it truncates to bound growth). Best-effort: the caller treats a returned
// error as "no capture" and proceeds with the spawn.
func openSupervisorLog(homeDir string) (*os.File, error) {
	path := filepath.Join(homeDir, "supervisor.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() >= supervisorLogMaxBytes {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	return os.OpenFile(path, flags, 0o600)
}

// SupervisorRef is a live handle to a supervisor the daemon spawned or attached
// to. It carries the negotiated protocol version (PM focus #4) and, when the ref
// owns a connection, the AttachClient. Detach closes the client without killing.
type SupervisorRef struct {
	AgentID    string
	HomeDir    string
	SockPath   string
	InstanceID string
	ChildPID   int

	// NegotiatedVersion is min(ProtocolVersion, hello.ProtocolVersion): the cap on
	// what THIS daemon may send to this supervisor (PM focus #4, speak-at-remote
	// -version). Today ProtocolVersion == 1 and the only compatible remote is 1, so
	// it is always 1 and there is no down-speaking path to exercise — but the field
	// + the min() are in place so that when a v2 message/field is added the daemon
	// must gate it on NegotiatedVersion >= 2 and will NOT send v2-only data to a v1
	// supervisor. NO V2 YET → no down-speaking path exercised; structure in place.
	NegotiatedVersion int

	// Client is the attached connection (may be nil for a spawn-only ref where the
	// caller closed the probe connection). Detach closes it without signalling.
	Client *agentsupervisor.AttachClient
}

// negotiate caps the version we will speak at min(ours, remote). See
// SupervisorRef.NegotiatedVersion.
func negotiate(remote int) int {
	if remote < agentsupervisor.ProtocolVersion {
		return remote
	}
	return agentsupervisor.ProtocolVersion
}

// SpawnSupervisor spawns a NEW persistent supervisor and waits (bounded) for it
// to come up, returning a SupervisorRef built from its Hello.
//
// DETACH-SURVIVABLE SPAWN. We use plain exec.Command (NOT exec.CommandContext)
// so the supervisor is NOT killed when ctx is cancelled or the daemon shuts
// down — it must outlive us. We do NOT add it to any daemon kill-on-shutdown
// tracking. After Start we call cmd.Process.Release() and never Wait: the
// supervisor setsids (s1) and reparents to init, so Release avoids holding it as
// our child (no zombie) while letting it survive.
//
// COME-UP WAIT. The subcommand setsids, starts claude, then Serve()s the socket.
// We poll for the socket file + a successful AttachClient Connect+Hello; on
// success we build the ref from Hello (InstanceID/ChildPID/negotiated version).
// On timeout we return an error (the caller decides — e.g. reap + retry).
func SpawnSupervisor(ctx context.Context, cfg SpawnSupervisorCfg) (*SupervisorRef, error) {
	if cfg.AgentID == "" {
		return nil, errors.New("supervisormanager: AgentID required")
	}
	if cfg.HomeDir == "" {
		return nil, errors.New("supervisormanager: HomeDir required")
	}
	bin := cfg.BinaryPath
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("supervisormanager: resolve executable: %w", err)
		}
		bin = exe
	}
	if err := os.MkdirAll(cfg.HomeDir, 0o700); err != nil {
		return nil, fmt.Errorf("supervisormanager: mkdir home: %w", err)
	}

	args := buildSupervisorArgs(cfg)
	if len(cfg.AgentEnv) > 0 {
		envPath, err := writeAgentEnvFile(cfg.HomeDir, cfg.AgentEnv)
		if err != nil {
			return nil, err
		}
		args = append(args, "--agent-env-file", envPath)
	}

	// PLAIN exec.Command (not CommandContext): ctx must NOT be able to kill the
	// supervisor. The supervisor reparents to init after its own setsid; we are not
	// its long-term parent.
	cmd := exec.Command(bin, args...)
	// v2.7 security (defense-in-depth ⑤): the supervisor process itself gets only
	// the allowlisted system env — no worker-daemon secrets. The supervisor is
	// trusted code but never needs them (the mcp-config token reaches the mcp-host
	// via a file path, not env), so we strip them at this hop too — not raw
	// os.Environ().
	cmd.Env = agentsupervisor.BuildSupervisorEnv(os.Environ())
	// Capture the supervisor's stdout+stderr — otherwise DISCARDED — to a per-agent
	// supervisor.log so the REAL death cause is recoverable post-mortem. Both the
	// supervisor's own Logger diagnostics (incl. the graceful-signal it logs on the
	// way out) AND the claude child's stderr (wired to the supervisor's os.Stderr)
	// land here. This matters because the spawn is DETACH-SURVIVABLE: we Release the
	// process and never Wait() it, so an exit code/signal is otherwise unobservable —
	// when a supervisor dies between socket round-trips the daemon only sees the
	// downstream "broken pipe" on its next write, never WHY. Best-effort: a log-open
	// failure does not block the spawn (capture is simply skipped).
	logf, _ := openSupervisorLog(cfg.HomeDir)
	if logf != nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
		// The child inherits its own dup of the fd at Start; we close OUR copy after
		// the come-up wait. (A SIGKILL leaves no output — that is inherent — but a
		// panic, an OOM/SIGTERM the supervisor logs, or a claude crash all land here.)
		defer func() { _ = logf.Close() }()
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("supervisormanager: start supervisor: %w", err)
	}
	if logf != nil {
		// Spawn marker: makes each supervisor instance's output attributable in the
		// appended log (spawn time + child PID). Written via our fd; the child appends
		// after (O_APPEND keeps writes from both fds ordered at the OS level).
		fmt.Fprintf(logf, "\n=== agent-center supervisor spawn agent=%s pid=%d at=%s ===\n",
			cfg.AgentID, cmd.Process.Pid, time.Now().Format(time.RFC3339))
	}
	// Release the process handle so the Go runtime does not keep it as our child
	// (and does not try to reap it). The supervisor survives our death.
	if err := cmd.Process.Release(); err != nil {
		// Non-fatal: we still proceed to the come-up wait. Worst case the OS holds a
		// zombie until we exit, but the supervisor itself is unaffected.
		_ = err
	}

	timeout := cfg.ComeUpTimeout
	if timeout <= 0 {
		timeout = defaultComeUpTimeout
	}
	// v2.7 #178: the supervisor now binds a short temp-dir socket (deterministic
	// from agent-id) instead of one under the deep agent home that overflowed
	// macOS's 104B sun_path limit. Both sides derive it via the same helper.
	sockPath := agentsupervisor.SockPath(cfg.AgentID)

	ref, err := waitComeUp(ctx, cfg.AgentID, cfg.HomeDir, sockPath, timeout)
	if err != nil {
		return nil, err
	}
	return ref, nil
}

func writeAgentEnvFile(homeDir string, env map[string]string) (string, error) {
	path := filepath.Join(homeDir, "agent_env.runtime.json")
	b, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("supervisormanager: encode agent env: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", fmt.Errorf("supervisormanager: write agent env: %w", err)
	}
	return path, nil
}

// buildSupervisorArgs assembles the `worker agent-supervisor` subcommand argv
// from cfg (the single source of truth for the spawn flags). Pure + side-effect
// free so the flag mapping — notably the --reset-epoch emission — is unit-testable
// without spawning a process. Optional flags are omitted when empty/zero so the
// common-case argv stays minimal and matches the subcommand defaults.
func buildSupervisorArgs(cfg SpawnSupervisorCfg) []string {
	args := []string{
		"worker", "agent-supervisor",
		"--agent-id", cfg.AgentID,
		"--home-dir", cfg.HomeDir,
	}
	if cfg.MCPConfigPath != "" {
		args = append(args, "--mcp-config-path", cfg.MCPConfigPath)
	}
	if cfg.TasksDir != "" {
		args = append(args, "--workspace-dir", cfg.TasksDir)
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.DisplayName != "" {
		args = append(args, "--display-name", cfg.DisplayName)
	}
	if cfg.PromptDescription != "" {
		args = append(args, "--prompt-description", cfg.PromptDescription)
	}
	if cfg.ClaudeBin != "" {
		args = append(args, "--claude-bin", cfg.ClaudeBin)
	}
	// Only pass --reset-epoch for a post-reset spawn (epoch > 0); 0 is the
	// subcommand default so omitting it keeps the common-case argv clean.
	if cfg.Epoch > 0 {
		args = append(args, "--reset-epoch", strconv.Itoa(cfg.Epoch))
	}
	// Mode-B crash-relaunch fork (v2.7 GATE-7): --generation > 0 forks a fresh
	// session-id and --resume-from carries the killed session to fork from. Both
	// omitted on the common-case initial/normal start (generation 0, no fork).
	if cfg.Generation > 0 {
		args = append(args, "--generation", strconv.Itoa(cfg.Generation))
	}
	if cfg.ResumeFromSessionID != "" {
		args = append(args, "--resume-from", cfg.ResumeFromSessionID)
	}
	if cfg.ConcurrencyEnabled {
		args = append(args, "--concurrency-enabled")
	}
	return args
}

// waitComeUp polls for the socket + a successful Hello until the timeout, ctx
// cancel, or success. On success it returns a ref holding the OPEN client.
func waitComeUp(ctx context.Context, agentID, home, sockPath string, timeout time.Duration) (*SupervisorRef, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("supervisormanager: come-up wait cancelled: %w", ctx.Err())
		}
		if time.Now().After(deadline) {
			if lastErr == nil {
				lastErr = errors.New("socket never appeared")
			}
			return nil, fmt.Errorf("supervisormanager: supervisor did not come up within %s: %w", timeout, lastErr)
		}
		if _, statErr := os.Stat(sockPath); statErr != nil {
			lastErr = statErr
			time.Sleep(50 * time.Millisecond)
			continue
		}
		dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		client, err := agentsupervisor.Connect(dialCtx, sockPath)
		cancel()
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		hello, err := client.Hello(ctx)
		if err != nil {
			_ = client.Close()
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		// IDENTITY GUARD (mirrors ProbeAgent's PID-REUSE-SAFE check): the socket
		// path is DETERMINISTIC per agent-id and thus SHARED across supervisor
		// incarnations. During a rapid restart, a PREVIOUS supervisor for this
		// agent-id can still be alive on the same sockPath (its kill/reap is async
		// and not awaited) and answer THIS Hello while its artifacts live under a
		// DIFFERENT home. Accepting it would hand back a ref whose HomeDir has no
		// matching supervisor.instance — the daemon would then read a missing/foreign
		// record (observed as the flaky "supervisor.instance: no such file" during
		// concurrent come-ups). Require the on-disk record under THIS home to exist
		// and self-report the SAME instance-id as the process we just spoke to.
		// Otherwise it is not OUR supervisor: keep polling until the fresh one binds
		// the socket — it writes its instance file BEFORE it serves (Start before
		// Serve), so a matching record is guaranteed once it answers.
		rec, ok := readInstance(home)
		if !ok {
			_ = client.Close()
			lastErr = errors.New("supervisor.instance not yet written under home")
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if rec.InstanceID != hello.InstanceID {
			_ = client.Close()
			lastErr = fmt.Errorf("foreign supervisor on shared socket: home instance_id=%q hello instance_id=%q", rec.InstanceID, hello.InstanceID)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return &SupervisorRef{
			AgentID:           agentID,
			HomeDir:           home,
			SockPath:          sockPath,
			InstanceID:        hello.InstanceID,
			ChildPID:          hello.ChildPID,
			NegotiatedVersion: negotiate(hello.ProtocolVersion),
			Client:            client,
		}, nil
	}
}

// ProbeState classifies the LOCAL state of an agent's supervisor — it is a
// MECHANISM report, NOT a decision. The boundary (PM, v2.7 D2-f): s3a reports
// local state (this enum); the reattach / relaunch / stop DECISION belongs to s4,
// which JOINS this with the center's desired-state. In particular s4 honors
// desired==stopped (stop+reap regardless of Reattachable) and only relaunches an
// Unavailable supervisor when desired==running with in-flight work. s3a must NOT
// autonomously reattach/relaunch off this state alone (no desired = incomplete).
type ProbeState int

const (
	// Reattachable: a live, compatible supervisor whose self-reported identity
	// matches the on-disk record — i.e. the daemon CAN re-attach (the survival
	// mechanism is available). Whether it SHOULD is s4's call (desired-state).
	Reattachable ProbeState = iota
	// Unavailable: the supervisor is gone, lies about its identity (stale file /
	// reused pid), or speaks an incompatible version — i.e. no live supervisor to
	// re-attach to here. Whether to relaunch (mode-B) is s4's call (only when
	// desired==running + in-flight work); s4 invokes ReapResidual + SpawnSupervisor.
	Unavailable
)

// Relaunch reasons (Unavailable.Reason). NOTE: there is no "incompatible" reason
// anymore (v2.7 — the cross-version gate was dropped; a live supervisor is always
// Reattachable). If the deferred breaking-change trigger ever fires, re-add an
// "incompatible" reason here + the guard in ProbeAgent (see ProbeAgent's note).
const (
	ReasonMissing = "missing" // no supervisor.instance file
	ReasonDead    = "dead"    // connect/hello failed, or identity mismatch (stale/reused)
)

// ProbeResult is the outcome of ProbeAgent. On Reattachable, Client + Hello are
// set and the caller OWNS the Client (Detach to close). On Unavailable, Reason
// explains why and Client is nil.
type ProbeResult struct {
	State  ProbeState
	Reason string

	Client *agentsupervisor.AttachClient
	Hello  agentsupervisor.HelloResp

	// NegotiatedVersion is set on Reattachable (min of ours/remote).
	NegotiatedVersion int

	// SockPath is the resolved socket path the probe connected on (v2.7 #178);
	// RefFromProbe carries it onto the SupervisorRef so stop/reap target the
	// same path rather than recomputing.
	SockPath string
}

// instanceRecord mirrors the supervisor.instance document written by s1
// (agentsupervisor/artifacts.go). We re-declare it here because the s1 type is
// unexported; the JSON contract is the stable interface.
type instanceRecord struct {
	InstanceID    string `json:"instance_id"`
	AgentID       string `json:"agent_id"`
	SupervisorPID int    `json:"supervisor_pid"`
	ChildPID      int    `json:"child_pid"`
	StartedAt     string `json:"started_at"`          // RFC3339Nano
	SockPath      string `json:"sock_path,omitempty"` // v2.7 #178: live socket (outside HomeDir)
}

// sockPathFor resolves a supervisor's socket path from its instance record:
// prefer the recorded sock_path (robust across $TMPDIR contexts / restarts),
// falling back to the deterministic helper for pre-#178 records that lack the
// field. v2.7 #178.
func sockPathFor(rec instanceRecord) string {
	if rec.SockPath != "" {
		return rec.SockPath
	}
	return agentsupervisor.SockPath(rec.AgentID)
}

// readInstance reads + decodes <home>/supervisor.instance. A missing/unreadable
// file is reported via ok=false so callers map it to ReasonMissing / nothing-to
// -reap rather than a hard error.
func readInstance(home string) (rec instanceRecord, ok bool) {
	b, err := os.ReadFile(filepath.Join(home, agentsupervisor.InstanceFileName))
	if err != nil {
		return instanceRecord{}, false
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return instanceRecord{}, false
	}
	return rec, true
}

// ProbeAgent inspects an agent home and decides attach vs relaunch. This is the
// boot-probe (PM focus #1). The decision tree:
//
//  1. No supervisor.instance → Unavailable{missing}.
//  2. Connect+Hello fails → Unavailable{dead} (process gone / socket stale).
//  3. PID-REUSE-SAFE IDENTITY CHECK: hello.InstanceID must equal the file's
//     instance_id (the running process self-reports its instance-id over the
//     socket; a reused pid answering on a stale socket would NOT match). We also
//     compare StartedAt as a secondary guard. Mismatch → Unavailable{dead}
//     (stale file, different process).
//  4. NO version gate (v2.7): once the process is live + identity-matched it is
//     Reattachable regardless of hello.ProtocolVersion (backward-compat assumed).
//     The deferred breaking-change trigger would reinstate a guard here.
//
// On Reattachable the caller owns ProbeResult.Client; on any Unavailable the
// probe connection (if opened) is closed before returning.
func ProbeAgent(ctx context.Context, home string) (ProbeResult, error) {
	// Default probe dial timeout: 2s keeps a returning daemon's restart probe snappy.
	return ProbeAgentWithDialTimeout(ctx, home, 2*time.Second)
}

// ProbeAgentWithDialTimeout is ProbeAgent with a caller-chosen socket dial/connect
// timeout. Production callers use ProbeAgent (2s). A caller that must tolerate a
// live-but-CPU-starved supervisor — e.g. a test under full-suite parallel load,
// where the process is up (an alive() check passes) but can't accept the socket
// within 2s — passes a generous timeout so a probe that STILL can't reach reliably
// means a genuine break (fail), not a starved-out probe (which a generous dial
// absorbs). This is what lets the reattach test distinguish a real
// alive-but-unreattachable regression from load starvation WITHOUT skipping.
func ProbeAgentWithDialTimeout(ctx context.Context, home string, dialTimeout time.Duration) (ProbeResult, error) {
	rec, ok := readInstance(home)
	if !ok {
		return ProbeResult{State: Unavailable, Reason: ReasonMissing}, nil
	}

	sockPath := sockPathFor(rec) // v2.7 #178: prefer recorded sock_path, fallback helper
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	client, err := agentsupervisor.Connect(dialCtx, sockPath)
	cancel()
	if err != nil {
		return ProbeResult{State: Unavailable, Reason: ReasonDead}, nil
	}
	hello, err := client.Hello(ctx)
	if err != nil {
		_ = client.Close()
		return ProbeResult{State: Unavailable, Reason: ReasonDead}, nil
	}

	// PID-reuse-safe identity: the live process self-reports instance_id; it must
	// match the file. (A reused pid answering a stale socket would mismatch.)
	if hello.InstanceID != rec.InstanceID ||
		(rec.StartedAt != "" && hello.StartedAt != "" && hello.StartedAt != rec.StartedAt) {
		_ = client.Close()
		return ProbeResult{State: Unavailable, Reason: ReasonDead}, nil
	}

	// NO version gate (v2.7, @oopslink — drop the cross-version range): a live
	// supervisor with a matching identity is ALWAYS Reattachable, regardless of its
	// advertised hello.ProtocolVersion. The protocol is assumed backward-compatible
	// (additive-only evolution); a daemon that was once "incompatible" now simply
	// re-attaches — the expected behavior under that convention.
	//
	// 🕒 DEFERRED-WITH-TRIGGER re-entry point: this is WHERE the mixed-version guard
	// goes back if a future BREAKING protocol change is ever made — re-introduce a
	// version check that closes the client and returns Unavailable with an
	// "incompatible" reason, so a returning daemon force-relaunches the incompatible
	// old supervisor instead of silently mis-parsing its wire. See
	// agentsupervisor/protocol.go's ProtocolVersion note for the full trigger contract.
	return ProbeResult{
		State:             Reattachable,
		Client:            client,
		Hello:             hello,
		NegotiatedVersion: negotiate(hello.ProtocolVersion),
		SockPath:          sockPath,
	}, nil
}

// RefFromProbe builds a SupervisorRef from a Reattachable ProbeResult, taking
// ownership of the probe's Client. Returns nil if the probe was not Reattachable.
func RefFromProbe(home string, pr ProbeResult) *SupervisorRef {
	if pr.State != Reattachable {
		return nil
	}
	return &SupervisorRef{
		AgentID:           pr.Hello.AgentID,
		HomeDir:           home,
		SockPath:          pr.SockPath, // v2.7 #178: the path the probe connected on
		InstanceID:        pr.Hello.InstanceID,
		ChildPID:          pr.Hello.ChildPID,
		NegotiatedVersion: pr.NegotiatedVersion,
		Client:            pr.Client,
	}
}
