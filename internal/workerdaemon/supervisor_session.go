// Package workerdaemon: supervisor_session.go is the v2.7 D2-f s3b-1 daemon-side
// session abstraction backed by the PERSISTENT per-agent supervisor (s1/s2) and
// its manager (s3a). It mirrors the operations the AgentController needs from the
// old direct-claude ClaudeSession (Inject / Stop / event stream via OnEvent /
// crash+clean coordination via OnExit), but routes them through the supervisor's
// unix socket INSTEAD of owning claude itself.
//
// CRITICAL INVARIANT (PM): in supervisor mode the SUPERVISOR is the SOLE owner of
// the claude process. SupervisorSession NEVER exec.Commands claude — the only
// thing it spawns is the supervisor (via supervisormanager.SpawnSupervisor), and
// the supervisor is the only thing that execs claude (claude's parent is the
// supervisor, never the daemon/session). All input/output flows over the socket:
// Inject → claude's held-open stdin; the event-pump drains claude's stdout from
// the supervisor's persistent offset cursor (events.jsonl).
//
// TWO SHUTDOWN PATHS (PM):
//   - Stop  — EXPLICIT terminate (StopAgent/reset): SIGTERM the SUPERVISOR
//     process via supervisormanager.StopSupervisor; its signal handler gracefully
//     stops claude + exits. Joins the event-pump.
//   - Detach — daemon-shutdown SURVIVAL path: close the socket (no signal). The
//     supervisor + claude KEEP RUNNING, owned by init, ready for a future daemon
//     to re-attach. Joins the event-pump WITHOUT killing anything.
//
// This file is ADDITIVE and NOT wired into the AgentController yet (that is the
// next slice, s3b-2); it is not activated.
package workerdaemon

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/supervisormanager"
)

// pumpReadMax is the max bytes the event-pump requests per ReadFrom. The
// supervisor returns whole+partial lines up to this; the pump only advances the
// offset past COMPLETE newline-terminated lines so a chunk boundary never splits
// a JSON object.
const pumpReadMax = 1 << 20 // 1 MiB

// pumpIdlePoll is how long the pump waits after catching up (eof) before polling
// again, and the base backoff after a transient read error.
const pumpIdlePoll = 50 * time.Millisecond

// pumpMaxTransientErrs is how many CONSECUTIVE transient read errors the pump
// tolerates (with backoff) before treating the supervisor as definitively gone
// and firing OnExit. Reset to zero on any successful read.
const pumpMaxTransientErrs = 100

// pumpReconnectEvery is how many consecutive read errors the pump tolerates
// before it tries to RE-DIAL a still-alive supervisor (issue-9bd86b8f gap ③). A
// dropped CONNECTION (vs a dead supervisor) must not cost the healthy claude a
// destructive reap+relaunch. Re-dialing on a cadence (not every error) avoids
// churning a working conn on a brief blip; the pump keeps trying up to
// pumpMaxTransientErrs before finally giving up (the truly-dead fallback).
const pumpReconnectEvery = 5

// supervisorReconnectTimeout bounds a single re-dial+Hello probe attempt.
const supervisorReconnectTimeout = 2 * time.Second

// SupervisorSession is a long-lived daemon-side handle to one agent's persistent
// supervisor. It spawns ONLY the supervisor (never claude), pumps claude's stdout
// events over the socket into OnEvent, injects input over the socket, and offers
// the Stop (terminate) / Detach (survive) shutdown paths. Safe for concurrent
// Inject / Stop / Detach.
type SupervisorSession struct {
	cfg    SupervisorSessionConfig
	ref    *supervisormanager.SupervisorRef
	client *agentsupervisor.AttachClient
	logger func(string)

	stopGrace time.Duration

	mu     sync.Mutex // guards client (nil after Detach/Stop) + closed
	closed bool       // blocks Inject once shutdown begins

	// stopping signals the pump that an intentional shutdown (Stop/Detach) is in
	// progress, so the next read error is treated as a clean definitive end rather
	// than a crash. Guarded by mu.
	stopping bool

	stopOnce sync.Once
	exitOnce sync.Once
	done     chan struct{} // closed after the pump joins + OnExit fired
}

// StartSupervisorSession SPAWNS a new persistent supervisor (which execs claude —
// the session does NOT), then starts the event-pump from offset 0 (a fresh
// supervisor's events.jsonl starts empty). The returned session is immediately
// usable for Inject / Stop / Detach.
func StartSupervisorSession(ctx context.Context, cfg SupervisorSessionConfig) (*SupervisorSession, error) {
	if cfg.AgentID == "" {
		return nil, errors.New("supervisor_session: agent_id required")
	}
	if cfg.HomeDir == "" {
		return nil, errors.New("supervisor_session: home_dir required")
	}

	ref, err := supervisormanager.SpawnSupervisor(ctx, supervisormanager.SpawnSupervisorCfg{
		AgentID:             cfg.AgentID,
		HomeDir:             cfg.HomeDir,
		MCPConfigPath:       cfg.MCPConfigPath,
		TasksDir:            cfg.TasksDir,
		BinaryPath:          cfg.BinaryPath,
		Model:               cfg.Model,
		DisplayName:         cfg.DisplayName,
		AgentEnv:            cfg.AgentEnv,
		PromptDescription:   cfg.PromptDescription,
		ClaudeBin:           cfg.ClaudeBin,
		Epoch:               cfg.Epoch,
		Generation:          cfg.Generation,
		ResumeFromSessionID: cfg.ResumeFromSessionID,
		ConcurrencyEnabled:  cfg.ConcurrencyEnabled,
		ComeUpTimeout:       cfg.ComeUpTimeout,
	})
	if err != nil {
		return nil, err
	}
	if ref.Client == nil {
		// Defensive: a spawn ref must carry the open client for the pump.
		supervisormanager.Detach(ref)
		return nil, errors.New("supervisor_session: spawn returned no client")
	}

	s := newSession(cfg, ref, ref.Client)
	// A fresh supervisor's cursor starts at baseOffset 0.
	go s.pump(0)
	return s, nil
}

// ReattachSupervisorSession resumes an ALREADY-LIVE supervisor (from s3a
// ProbeAgent → Reattachable) WITHOUT spawning anything: it starts the event-pump
// from fromOffset (the daemon's last-acked offset). This is the
// re-attach-survives path used on daemon boot (s4). The caller passes the live
// ref + its open AttachClient (e.g. supervisormanager.RefFromProbe).
func ReattachSupervisorSession(
	ctx context.Context,
	ref *supervisormanager.SupervisorRef,
	client *agentsupervisor.AttachClient,
	onEvent func(ev claudestream.StreamEvent),
	onExit func(err error),
	logger func(msg string),
	fromOffset int64,
) (*SupervisorSession, error) {
	if ref == nil {
		return nil, errors.New("supervisor_session: reattach requires a live ref")
	}
	if client == nil {
		return nil, errors.New("supervisor_session: reattach requires an open client")
	}
	cfg := SupervisorSessionConfig{
		AgentID: ref.AgentID,
		HomeDir: ref.HomeDir,
		OnEvent: onEvent,
		OnExit:  onExit,
		Logger:  logger,
	}
	s := newSession(cfg, ref, client)
	go s.pump(fromOffset)
	return s, nil
}

// newSession builds the common SupervisorSession state. It does NOT start the
// pump (the caller picks the start offset).
func newSession(cfg SupervisorSessionConfig, ref *supervisormanager.SupervisorRef, client *agentsupervisor.AttachClient) *SupervisorSession {
	logger := cfg.Logger
	if logger == nil {
		logger = func(string) {}
	}
	grace := cfg.StopGrace
	return &SupervisorSession{
		cfg:       cfg,
		ref:       ref,
		client:    client,
		logger:    logger,
		stopGrace: grace,
		done:      make(chan struct{}),
	}
}

// pump is the event-pump goroutine: ReadFrom(offset) → split into COMPLETE lines
// → ParseStreamLine → OnEvent → advance offset → Ack(offset). It tolerates
// transient read errors (bounded retry + backoff) and only ends — firing OnExit
// exactly once — on a definitive end (intentional shutdown, or the supervisor
// gone past the retry budget). This is the sole goroutine the session spawns;
// joining it == OnExit fired.
func (s *SupervisorSession) pump(offset int64) {
	var transient int
	for {
		// Snapshot the client + whether an intentional shutdown is underway.
		s.mu.Lock()
		client := s.client
		stopping := s.stopping
		s.mu.Unlock()

		if client == nil {
			// Detach/Stop closed our client → clean definitive end.
			s.fireExit(nil)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		data, next, eof, err := client.ReadFrom(ctx, offset, pumpReadMax)
		cancel()
		if err != nil {
			if stopping {
				// Expected: the socket went away because WE are shutting down.
				s.fireExit(nil)
				return
			}
			if errors.Is(err, agentsupervisor.ErrOffsetTruncated) {
				// We asked below baseOffset (acked + truncated). Resync to the
				// supervisor's current base via Hello and continue.
				if b, herr := s.resyncOffset(); herr == nil {
					offset = b
					transient = 0
					continue
				}
			}
			transient++
			// issue-9bd86b8f gap ③: a dropped CONNECTION (not a dead supervisor)
			// should be recovered by re-dialing the live supervisor — NOT by burning
			// the whole error budget and firing OnExit, which upstream turns into a
			// destructive reap+relaunch that kills the still-healthy claude. Try to
			// reconnect on a cadence; on success swap in the fresh client and resume
			// from the SAME offset (a stale offset below base is handled by the
			// normal offset-truncated resync on the next read).
			if transient%pumpReconnectEvery == 0 && s.tryReconnect() {
				transient = 0
				continue
			}
			if transient >= pumpMaxTransientErrs {
				s.logger("[worker] supervisor_session: supervisor gone (read errors exhausted)")
				s.fireExit(err)
				return
			}
			time.Sleep(pumpIdlePoll)
			continue
		}
		transient = 0

		if len(data) > 0 {
			consumed := s.dispatchLines(data)
			if consumed > 0 {
				offset += int64(consumed)
				// Ack the consumed prefix so the supervisor truncates its cursor.
				ackCtx, ackCancel := context.WithTimeout(context.Background(), 5*time.Second)
				if _, ackErr := client.Ack(ackCtx, offset); ackErr != nil {
					s.logger("[worker] supervisor_session: ack: " + ackErr.Error())
				}
				ackCancel()
			} else {
				// A partial line with no complete line yet: advance to `next` so we
				// don't spin re-reading the same partial chunk, but do NOT parse it.
				offset = next
			}
			continue
		}

		// No data. If caught up (eof) idle-poll; otherwise advance and continue.
		if eof {
			time.Sleep(pumpIdlePoll)
		}
		offset = next
	}
}

// dispatchLines splits data into COMPLETE newline-terminated lines, parses each
// via ParseStreamLine, fires OnEvent per parsed StreamEvent (one line can carry
// multiple content-block events), and returns the number of BYTES consumed (the
// length of the complete-line prefix). A trailing partial line (no newline) is
// left unconsumed so the next ReadFrom re-delivers it whole.
func (s *SupervisorSession) dispatchLines(data []byte) int {
	consumed := 0
	for {
		nl := bytes.IndexByte(data[consumed:], '\n')
		if nl < 0 {
			break // trailing partial line; leave it for the next read
		}
		lineEnd := consumed + nl
		line := bytes.TrimSpace(data[consumed:lineEnd])
		consumed = lineEnd + 1 // include the newline in the consumed count
		if len(line) == 0 {
			continue
		}
		raw := make([]byte, len(line))
		copy(raw, line)
		events, err := claudestream.ParseStreamLine(raw)
		if err != nil {
			s.logger("[worker] supervisor_session: parse stream line: " + err.Error())
			continue
		}
		if s.cfg.OnEvent != nil {
			for _, ev := range events {
				s.cfg.OnEvent(ev)
			}
		}
	}
	return consumed
}

// resyncOffset re-reads the supervisor's current baseOffset via Hello (used when
// a read returns offset_truncated). Returns the base the pump should resume from.
func (s *SupervisorSession) resyncOffset() (int64, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return 0, errors.New("supervisor_session: client closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hello, err := client.Hello(ctx)
	if err != nil {
		return 0, err
	}
	return hello.BaseOffset, nil
}

// tryReconnect re-dials the agent's supervisor and swaps the fresh connection in,
// so a dropped socket (vs a dead supervisor) is recovered WITHOUT the destructive
// reap+relaunch the pump's give-up path triggers (issue-9bd86b8f gap ③). It reuses
// the boot-probe, which is identity-checked + PID-reuse-safe: a Reattachable probe
// means the SAME supervisor incarnation is still live, so events.jsonl is
// continuous and the pump resumes from its EXISTING offset. Returns false — leaving
// the pump to keep retrying and eventually give up (the truly-dead fallback) — when
// the supervisor is genuinely gone or a Stop/Detach is in progress.
func (s *SupervisorSession) tryReconnect() bool {
	s.mu.Lock()
	if s.closed || s.stopping {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), supervisorReconnectTimeout)
	defer cancel()
	pr, err := supervisormanager.ProbeAgent(ctx, s.cfg.HomeDir)
	if err != nil || pr.State != supervisormanager.Reattachable {
		// ProbeAgent closes its own connection on any non-Reattachable result.
		return false
	}

	s.mu.Lock()
	// Re-check under lock: a Stop/Detach may have raced in while we probed.
	if s.closed || s.stopping {
		s.mu.Unlock()
		_ = pr.Client.Close()
		return false
	}
	old := s.client
	s.client = pr.Client
	s.ref = supervisormanager.RefFromProbe(s.cfg.HomeDir, pr)
	s.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	s.logger("[worker] supervisor_session: RECONNECTED to live supervisor (non-destructive)")
	return true
}

// Inject sends msg over the socket; the supervisor wraps it as a stream-json user
// line and writes it to claude's held-open stdin. Concurrency-safe. Returns
// ErrSessionClosed once Stop/Detach has begun.
func (s *SupervisorSession) Inject(ctx context.Context, msg string) error {
	s.mu.Lock()
	if s.closed || s.client == nil {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	client := s.client
	s.mu.Unlock()
	return client.Inject(ctx, msg)
}

// Stop is the EXPLICIT-terminate path (StopAgent/reset): it SIGTERMs the
// SUPERVISOR process (which gracefully stops claude + exits) via
// supervisormanager.StopSupervisor — the session NEVER signals claude directly —
// then joins the event-pump. OnExit fires exactly once. Idempotent.
func (s *SupervisorSession) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.stopping = true
		ref := s.ref
		s.mu.Unlock()

		grace := s.stopGrace
		if err := supervisormanager.StopSupervisor(ref, grace); err != nil {
			s.logger("[worker] supervisor_session: stop supervisor: " + err.Error())
		}
		// StopSupervisor Detached the client; nil our copy so the pump ends.
		s.mu.Lock()
		s.client = nil
		s.mu.Unlock()
	})
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Detach is the daemon-shutdown SURVIVAL path: it closes the socket (NO signal)
// so the supervisor + claude KEEP RUNNING, then joins the event-pump WITHOUT
// killing anything. OnExit fires exactly once (clean). Idempotent.
func (s *SupervisorSession) Detach() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.stopping = true
		ref := s.ref
		s.mu.Unlock()

		// Detach closes the client conn (no signal to the processes) and nils
		// ref.Client; mirror it onto our copy so the pump observes the close.
		supervisormanager.Detach(ref)
		s.mu.Lock()
		s.client = nil
		s.mu.Unlock()
	})
	<-s.done
}

// fireExit marks the session closed and invokes OnExit exactly once, then closes
// done (joining the pump). Concurrency-safe; idempotent.
func (s *SupervisorSession) fireExit(err error) {
	s.exitOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if s.cfg.OnExit != nil {
			s.cfg.OnExit(err)
		}
		close(s.done)
	})
}

// Done returns a channel closed after the event-pump has joined and OnExit has
// fired. Useful for awaiting termination without Stop/Detach.
func (s *SupervisorSession) Done() <-chan struct{} { return s.done }

// Ref returns the underlying SupervisorRef (for diagnostics / ownership checks).
func (s *SupervisorSession) Ref() *supervisormanager.SupervisorRef { return s.ref }
