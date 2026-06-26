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
	// ComeUpTimeout bounds how long SpawnSupervisor waits for the supervisor to
	// listen on its socket and answer Hello. Zero → defaultComeUpTimeout.
	ComeUpTimeout time.Duration
}

const defaultComeUpTimeout = 15 * time.Second

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
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("supervisormanager: start supervisor: %w", err)
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
	rec, ok := readInstance(home)
	if !ok {
		return ProbeResult{State: Unavailable, Reason: ReasonMissing}, nil
	}

	sockPath := sockPathFor(rec) // v2.7 #178: prefer recorded sock_path, fallback helper
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
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
