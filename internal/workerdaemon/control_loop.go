// Package workerdaemon: ControlLoop is the v2.7 D1 (ADR-0050, task #102)
// worker-initiated control-stream poll loop for the Environment BC.
//
// It is ADDITIVE and runs in its OWN goroutine, fully independent of the
// legacy dispatch loop (dispatch_loop.go / runtime pollOnce). It does NOT
// import, touch, or depend on the dispatch loop in any way.
//
// Lifecycle (mirrors the dispatch loop's interval/ctx-cancel idioms):
//  1. On start: ConnectControl(workerID) → resume cursor at the worker's
//     server-side last_acked_offset.
//  2. Loop on a poll interval: PullCommands(workerID, cursor) → for each
//     command, Handle it via the pluggable CommandHandler, then advance the
//     cursor to that command's offset. After the batch is fully handled,
//     AckControl(workerID, cursor) once (cumulative-ack model).
//  3. On a handler error: STOP advancing past the failed command (so it is
//     re-pulled and retried on the next iteration) and ack only the offsets
//     that succeeded before it.
//  4. ctx cancel → return cleanly.
//
// D1 EXECUTION IS A NO-OP. ControlChannel commands are logged but nothing real
// happens — real process control is D2's AgentController. D2 plugs a real
// CommandHandler in here; because the loop only acks AFTER a successful
// Handle, D2 can rely on at-least-once delivery + the per-command
// IdempotencyKey to dedupe.
package workerdaemon

import (
	"context"
	"fmt"
	"time"
)

// ControlClient is the subset of AdminClient methods the ControlLoop needs.
// Defined as an interface so control_loop_test.go can plug a fake and so the
// loop stays decoupled from the concrete transport. Production wires
// *AdminClient (its ConnectControl/PullCommands/AckControl satisfy this).
type ControlClient interface {
	ConnectControl(ctx context.Context, workerID string) (lastAckedOffset int64, err error)
	PullCommands(ctx context.Context, workerID string, after int64) ([]ControlCommand, error)
	AckControl(ctx context.Context, workerID string, offset int64) error
}

// CommandHandler executes a single control command. D1 ships NoopCommandHandler
// (logs + does nothing real); D2's AgentController will implement this for
// real process control. The loop acks ONLY after Handle returns nil — so a
// returned error keeps the command un-acked and it is retried next iteration.
type CommandHandler interface {
	Handle(ctx context.Context, cmd ControlCommand) error
}

// NoopCommandHandler is the D1 synthetic handler: it logs the command's type +
// offset and does nothing real. It never fails. D2 replaces it with the
// AgentController. The pluggable seam is ControlLoopConfig.Handler.
type NoopCommandHandler struct {
	// Logger receives one-line ops messages. Nil → silent.
	Logger func(msg string)
}

// Handle logs and succeeds. The no-op never errors, so in D1 the cursor always
// advances; the "ack only after success" structure exists so D2 can rely on it.
func (h NoopCommandHandler) Handle(_ context.Context, cmd ControlCommand) error {
	if h.Logger != nil {
		h.Logger(fmt.Sprintf("control: no-op handle command type=%s offset=%d id=%s (D2 AgentController will execute)",
			cmd.CommandType, cmd.Offset, cmd.ID))
	}
	return nil
}

// ControlLoopConfig parameterises the control poll loop.
type ControlLoopConfig struct {
	WorkerID string
	// PollInterval between PullCommands batches. Default 1s (mirrors the
	// dispatch loop default). Tests inject a short interval for determinism.
	PollInterval time.Duration
	// Handler executes each command. Nil → NoopCommandHandler{Logger}.
	Handler CommandHandler
	// Logger receives one-line ops messages with a `control: ` flavour. Nil →
	// no-op.
	Logger func(msg string)

	// StreamClient, when non-nil AND DisableStream is false, makes the loop
	// STREAM-FIRST: each connected tick opens the SSE down-push and handles
	// commands as they arrive (low latency), falling back to the poll path on
	// disconnect / error / heartbeat-timeout. Nil → poll-only (the always-
	// available path). Production wires the daemon's *AdminClient here (its
	// StreamCommands satisfies StreamClient).
	StreamClient StreamClient
	// DisableStream forces the poll-only path even when StreamClient is set. The
	// stream is OPT-IN/configurable but DEFAULT-ON for v2.7: production leaves
	// this false (stream-first) and can flip it true to fall back to pure poll
	// without losing any delivery guarantee (poll is the same contract).
	DisableStream bool
	// StreamIdleTimeout overrides the no-frame fallback timeout. Default
	// defaultStreamIdleTimeout (60s ≈ 2× the 30s server heartbeat). Tests inject
	// a short value for determinism.
	StreamIdleTimeout time.Duration
}

// holBlockThreshold is the number of CONSECUTIVE failed handle attempts of the
// SAME stuck offset after which the ControlLoop emits the distinct HOL-BLOCKED
// alarm. With the default 1s PollInterval this fires after ~30s of a starved
// control stream — long enough to rule out a transient inject/session race, short
// enough to alarm before the worker's whole control stream is silently wedged.
const holBlockThreshold = 30

// holBlockReescalateEvery de-spams the alarm: after the first crossing the alarm
// repeats only every Nth additional consecutive failure (so ~every 30s at the 1s
// poll interval), instead of every poll. The transient per-poll "(will retry)"
// line still logs each tick; this is the persistent-block escalation on top.
const holBlockReescalateEvery = 30

// streamChanBuf is the buffer on the reader→executor command channel. The reader
// goroutine forwards parsed SSE commands here; the executor drains them. A small
// buffer smooths bursts without unbounded memory; when full the reader simply blocks
// (backpressure) — no command is lost (the offset cursor + poll safety net backstop).
const streamChanBuf = 16

// defaultStreamIdleTimeout bounds how long the stream client waits for ANY frame
// (a command OR the server's 30s heartbeat) before declaring the stream dead and
// falling back to poll. The center heartbeats every controlstream.DefaultHeartbeat
// (30s), so 2× = 60s tolerates a single missed/late heartbeat without flapping yet
// fails over PROMPTLY rather than hanging on a silently-dropped subscriber. Lives
// here (daemon side) deliberately decoupled from the server constant — the daemon
// derives its own tolerance, it does not import the center's controlstream pkg.
const defaultStreamIdleTimeout = 60 * time.Second

// ControlLoop polls the center's control-command stream and dispatches each
// command to its handler. It is independent of the dispatch loop.
type ControlLoop struct {
	cfg    ControlLoopConfig
	client ControlClient

	// cursor is the cumulative last-acked offset; the loop pulls everything
	// after it. Seeded from ConnectControl on Run.
	cursor int64

	// stuckOffset / stuckFails track consecutive Handle failures of the SAME
	// head-of-line command offset (HOL escalation). stuckFails resets to 0 whenever
	// the cursor advances past the offset (the block cleared) or a DIFFERENT offset
	// becomes the head, so the counter measures one continuous block.
	stuckOffset int64
	stuckFails  int
}

// NewControlLoop constructs the loop. A nil Handler defaults to the D1
// NoopCommandHandler wired with the same Logger.
func NewControlLoop(cfg ControlLoopConfig, client ControlClient) *ControlLoop {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string) {}
	}
	if cfg.Handler == nil {
		cfg.Handler = NoopCommandHandler{Logger: cfg.Logger}
	}
	return &ControlLoop{cfg: cfg, client: client}
}

// Cursor returns the current cumulative cursor (for tests / observability).
func (l *ControlLoop) Cursor() int64 { return l.cursor }

// tickHandler is the OPTIONAL per-tick hook the ControlLoop invokes after each poll
// (on the single ControlLoop goroutine). AgentController implements it to drain due
// mid-run self-heal relaunches; a handler that does not implement it is never ticked
// (additive — D1's NoopCommandHandler is unaffected).
type tickHandler interface {
	OnTick(ctx context.Context)
}

// Run blocks until ctx is cancelled. It first ConnectControls to seed the cursor,
// then runs an executor select loop that multiplexes THREE sources on a SINGLE
// goroutine: (1) low-latency stream commands forwarded by a separate reader
// goroutine, (2) stream-end signals, and (3) a poll/self-heal tick. Transient
// pull/ack errors are logged, not fatal — the daemon keeps polling (graceful
// degradation). Connect failure is also non-fatal: the loop logs, starts the cursor
// at 0, and retries connecting on each tick until it succeeds.
//
// §-1 CONCURRENCY: handleBatch (→ CommandHandler.Handle → startSession) and OnTick
// (self-heal relaunch drain) BOTH run on THIS executor goroutine — they are NEVER
// concurrent (the single-goroutine invariant; startSession is not safe to call from
// two goroutines). The blocking SSE read lives on a SEPARATE reader goroutine that
// only PARSES frames and FORWARDS commands over a channel — it never touches
// startSession or the cursor. This is the fix for the OnTick-latency regression: a
// quiet stream blocks only the reader, so the tick cadence (poll backfill + OnTick
// relaunch drain) is never delayed by stream idleness (previously a silent stream
// could stall OnTick up to the idle timeout, delaying a self-heal relaunch backoff).
func (l *ControlLoop) Run(ctx context.Context) error {
	if l.cfg.WorkerID == "" {
		return fmt.Errorf("control loop: worker_id required")
	}

	connected := l.connect(ctx)

	tick := time.NewTicker(l.cfg.PollInterval)
	defer tick.Stop()

	// --- Stream plumbing (only active when streamEnabled). All these vars are
	// touched ONLY by this executor goroutine. The reader goroutine captures the
	// channel values by copy, so niling them here never races the reader. ---
	var streamCh chan ControlCommand
	var streamErrCh chan error
	var streamCancel context.CancelFunc
	streamRunning := false
	// holPaused: set when the executor stops the stream because a delivered command
	// did NOT advance the cursor (handler HOL-block or ack-fail). While paused the
	// stream is NOT re-opened (avoids reconnect churn); the poll safety net retries
	// the stuck head with the correct IN-BATCH HOL break. Cleared once poll advances
	// the cursor (the block resolved), after which the stream resumes (stream-first).
	holPaused := false

	stopStream := func() {
		if streamCancel != nil {
			streamCancel()
			streamCancel = nil
		}
		streamCh = nil
		streamErrCh = nil
		streamRunning = false
	}
	defer stopStream()

	startStream := func() {
		if !l.streamEnabled() || streamRunning || holPaused {
			return
		}
		sctx, scancel := context.WithCancel(ctx)
		ch := make(chan ControlCommand, streamChanBuf)
		errCh := make(chan error, 1)
		after := l.cursor // snapshot the shared offset cursor (executor-only read)
		streamCancel = scancel
		streamCh = ch
		streamErrCh = errCh
		streamRunning = true
		go func() { errCh <- l.streamReader(sctx, after, ch) }()
	}

	if connected {
		startStream()
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case cmd := <-streamCh:
			// Low-latency stream delivery on the executor goroutine. handleStreamCmd
			// routes it through the SAME handleBatch contract as poll (shared cursor/
			// ack/HOL). If the cursor does NOT advance past it (handler HOL-block or
			// ack-fail), stop+pause the stream: the poll safety net re-pulls the
			// contiguous batch so the IN-BATCH break enforces head-of-line blocking. A
			// one-at-a-time stream must NEVER handle past a stuck head — cumulative ack
			// of a later offset would silently skip and LOSE the stuck command.
			if !l.handleStreamCmd(ctx, cmd) {
				l.log("control: stream head offset=%d did not advance — poll takes over (HOL/ack-retry)", cmd.Offset)
				stopStream()
				holPaused = true
			}

		case err := <-streamErrCh:
			// Reader ended (disconnect / heartbeat-timeout / EOF / ctx). Mark not
			// running; the poll tick backfills from the SHARED offset cursor and the
			// next tick re-opens the stream (stream-first). Not a fatal condition.
			streamCh = nil
			streamErrCh = nil
			streamRunning = false
			if streamCancel != nil {
				streamCancel()
				streamCancel = nil
			}
			if ctx.Err() == nil && err != nil {
				l.log("control: stream ended (after=%d): %v — poll backfills, will re-stream", l.cursor, err)
			}

		case <-tick.C:
			if !connected {
				// Endpoints may have been unavailable at start (or the worker state
				// was lost). Re-attempt connect before pulling so the cursor is
				// correctly seeded; degrade gracefully otherwise.
				connected = l.connect(ctx)
				if !connected {
					continue
				}
			}
			// POLL safety net: ALWAYS runs on the tick cadence — backfills any gap
			// (stream down, a silently-dropped SSE frame, or a stuck head while
			// holPaused) from the SHARED offset cursor. It is the sole path when the
			// stream is disabled/down, and the HOL-correct path for a stuck head.
			// Identical handleBatch contract; the offset cursor dedups stream overlap.
			before := l.cursor
			l.pollOnce(ctx)
			if holPaused && l.cursor > before {
				// The stuck head cleared via poll → safe to stream again.
				holPaused = false
			}
			// Self-heal relaunch drain (GATE-7 Mode-B slice B, FINDING-3): runs EVERY
			// tick on this executor goroutine, NEVER blocked by a quiet stream (the
			// blocking read is on the reader goroutine). Optional — a handler without
			// OnTick is simply never ticked (additive).
			if th, ok := l.cfg.Handler.(tickHandler); ok {
				th.OnTick(ctx)
			}
			// (Re)start the stream if enabled and not currently running (first connect /
			// recovered after a transient end). No-op while holPaused or already running.
			startStream()
		}
	}
}

// streamEnabled reports whether the loop should try the SSE stream first this
// tick: a StreamClient is wired AND it has not been explicitly disabled. The
// stream is OPT-IN via wiring + DEFAULT-ON for v2.7 (production wires it); poll
// is the always-available fallback regardless.
func (l *ControlLoop) streamEnabled() bool {
	return l.cfg.StreamClient != nil && !l.cfg.DisableStream
}

// connect seeds the cursor from the server's last_acked_offset. Returns false
// (and logs) on failure so Run can keep the daemon alive and retry.
func (l *ControlLoop) connect(ctx context.Context) bool {
	off, err := l.client.ConnectControl(ctx, l.cfg.WorkerID)
	if err != nil {
		l.log("control: connect %s: %v (will retry; control disabled until then)", l.cfg.WorkerID, err)
		return false
	}
	l.cursor = off
	l.log("control: connected worker_id=%s resume_offset=%d", l.cfg.WorkerID, off)
	return true
}

// pollOnce pulls the batch after the cursor, handles each command in order,
// and cumulatively acks the highest SUCCESSFULLY handled offset. On a handler
// error it stops at the failed command (cursor not advanced past it) and acks
// only the prefix that succeeded — the failed command is retried next tick.
//
// pollOnce is the POLL path; the STREAM path (handleStreamed) shares the EXACT
// SAME delivery logic via handleBatch (do NOT fork the delivery contract).
func (l *ControlLoop) pollOnce(ctx context.Context) {
	cmds, err := l.client.PullCommands(ctx, l.cfg.WorkerID, l.cursor)
	if err != nil {
		l.log("control: pull commands (after=%d): %v", l.cursor, err)
		return
	}
	if len(cmds) == 0 {
		return
	}
	l.handleBatch(ctx, cmds)
}

// handleBatch is the SHARED in-order handle → cumulative-ack(offset) →
// cursor-advance contract used by BOTH the poll path (pollOnce) and the stream
// path (handleStreamed, one-command "batch"). It is the single source of truth
// for the delivery guarantees so stream and poll cannot drift:
//   - in-order: handle each command in offset order; STOP (break) on the first
//     handler error — never advance past a failed command (HOL; FINDING-1/3).
//   - cumulative ack: AckControl(highestHandled) once for the succeeded prefix.
//   - ack-fail → cursor NOT advanced: the command(s) are re-delivered (the
//     handler is idempotent-by-key, so re-handling / stream↔poll overlap dedups).
//   - HOL escalation + loud surface preserved (noteStuck / clearStuck).
//
// cmds MUST be offset-ascending and start strictly after l.cursor (both the
// poll PullCommands and the stream catch-up guarantee this).
func (l *ControlLoop) handleBatch(ctx context.Context, cmds []ControlCommand) {
	highestHandled := l.cursor
	advanced := false
	for _, cmd := range cmds {
		if err := l.cfg.Handler.Handle(ctx, cmd); err != nil {
			// Do NOT advance past the failed command — it will be re-pulled
			// (poll) or re-delivered on reconnect (stream catch-up) and retried.
			// Ack whatever prefix succeeded below.
			l.log("control: handle command offset=%d type=%s: %v (will retry)",
				cmd.Offset, cmd.CommandType, err)
			l.noteStuck(cmd.Offset, cmd.CommandType, err)
			break
		}
		highestHandled = cmd.Offset
		advanced = true
	}

	if !advanced {
		// First command failed; nothing new to ack. The stuck counter was already
		// bumped above (this is the HOL-block case — the head command keeps failing).
		return
	}
	if err := l.client.AckControl(ctx, l.cfg.WorkerID, highestHandled); err != nil {
		// Ack failed: leave the cursor where it is so we re-pull + re-ack.
		// The handler is idempotent-by-key (D2), so re-handling is safe.
		l.log("control: ack offset=%d: %v (cursor not advanced)", highestHandled, err)
		return
	}
	// The cursor advanced (a prefix succeeded) → the prior head-of-line block, if
	// any, has cleared. Reset the HOL escalation counter.
	l.clearStuck()
	l.cursor = highestHandled
}

// errStreamHOLStop is the sentinel the stream callback returns to STOP the stream
// when a delivered command did not advance the cursor (handler HOL-block / ack-fail).
// It signals an intentional fall-back-to-poll, not a transport failure. The control
// loop treats it like any other stream end: poll re-pulls the contiguous batch and
// the in-batch break enforces head-of-line blocking.
var errStreamHOLStop = &streamError{msg: "stream head did not advance (HOL/ack-retry) — falling back to poll"}

// handleStreamCmd processes ONE command delivered over the stream, on the EXECUTOR
// goroutine (the sole caller of handleBatch → startSession). It dedups overlap with
// the poll path by the shared cursor and routes the command through the SAME
// handleBatch delivery contract as poll (a one-command batch). It reports whether the
// cursor advanced past the command:
//   - true  → handled+acked (or a dedup of an already-passed offset): keep streaming.
//   - false → the head did NOT advance (handler error = HOL-block, OR ack POST
//     failed). The caller MUST stop the stream and let the POLL safety net re-pull the
//     contiguous batch, because the poll path's IN-BATCH break is the only thing that
//     enforces head-of-line blocking. A stream delivers one command at a time, so
//     continuing to the next offset would skip the stuck head and the cumulative ack
//     would silently LOSE it. (Stream + poll must not fork: the ordered-break contract
//     lives in handleBatch, reached the same way from both transports.)
//
// ack stays HTTP POST (AckControl, inside handleBatch): the hybrid is SSE-down /
// POST-ack-up. The cursor is the SAME field shared with poll (the resume key for both
// transports), so a stream↔poll transition resumes at the same offset and any overlap
// is deduped by the cursor (offset <= cursor never re-handled).
func (l *ControlLoop) handleStreamCmd(ctx context.Context, cmd ControlCommand) bool {
	// Defensive dedup: a frame at/under the cursor (catch-up/live overlap or a stale
	// re-delivery, or poll handled it first) is already past — not a stall, keep going.
	if cmd.Offset <= l.cursor {
		return true
	}
	before := l.cursor
	l.handleBatch(ctx, []ControlCommand{cmd})
	return l.cursor > before
}

// streamReader runs the blocking SSE read on its OWN goroutine and forwards each
// parsed command to out (the executor drains it). It NEVER calls handleBatch /
// startSession and never touches the cursor — that all stays on the executor
// goroutine (the §-1 single-goroutine invariant). Because the blocking read lives
// here, a quiet stream blocks ONLY this goroutine, never the executor's tick cadence
// → OnTick (self-heal relaunch drain) stays prompt. It returns when the stream ends
// (disconnect / heartbeat-timeout / EOF / ctx) so the executor can poll-backfill and
// re-stream. A forward blocked on a full channel unblocks on ctx cancel.
func (l *ControlLoop) streamReader(ctx context.Context, after int64, out chan<- ControlCommand) error {
	idle := l.cfg.StreamIdleTimeout
	if idle <= 0 {
		idle = defaultStreamIdleTimeout
	}
	return l.cfg.StreamClient.StreamCommands(ctx, l.cfg.WorkerID, after, idle, func(cmd ControlCommand) error {
		select {
		case out <- cmd:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

// streamOnce is the SYNCHRONOUS per-cycle form of the stream path, used by the
// deterministic per-cycle tests (drainOneCycle). It opens the SSE down-push from the
// current offset cursor and routes EACH arriving command through handleStreamCmd
// (the SAME per-command contract the executor goroutine uses in Run), STOPPING the
// stream the moment a command does not advance the cursor (HOL / ack-fail) so the
// poll fallback governs. Production Run does NOT call streamOnce — it runs the read
// on the reader goroutine for the non-blocking OnTick property — but both paths
// funnel through handleStreamCmd so the delivery contract is identical.
func (l *ControlLoop) streamOnce(ctx context.Context) error {
	idle := l.cfg.StreamIdleTimeout
	if idle <= 0 {
		idle = defaultStreamIdleTimeout
	}
	return l.cfg.StreamClient.StreamCommands(ctx, l.cfg.WorkerID, l.cursor, idle, func(cmd ControlCommand) error {
		if l.handleStreamCmd(ctx, cmd) {
			return nil
		}
		// Head did not advance: stop the stream so the poll fallback re-pulls the
		// contiguous batch and the in-batch break enforces HOL (never skip the head).
		return errStreamHOLStop
	})
}

// noteStuck records one consecutive Handle failure of the head-of-line command at
// offset off and emits the DISTINCT HOL-BLOCKED alarm once the same offset has
// failed holBlockThreshold times, then again every holBlockReescalateEvery further
// failures (de-spammed — not every poll). A different stuck offset resets the count.
func (l *ControlLoop) noteStuck(off int64, cmdType string, cause error) {
	if l.stuckFails == 0 || l.stuckOffset != off {
		l.stuckOffset = off
		l.stuckFails = 0
	}
	l.stuckFails++
	if l.stuckFails == holBlockThreshold ||
		(l.stuckFails > holBlockThreshold && (l.stuckFails-holBlockThreshold)%holBlockReescalateEvery == 0) {
		l.log("control: HOL-BLOCKED — command at offset=%d type=%s has failed %d times, "+
			"ALL subsequent commands for this worker are starved: %v",
			off, cmdType, l.stuckFails, cause)
	}
}

// clearStuck resets the HOL escalation counter (the head command finally handled,
// so the cursor advanced past the previously-stuck offset).
func (l *ControlLoop) clearStuck() {
	l.stuckOffset = 0
	l.stuckFails = 0
}

func (l *ControlLoop) log(format string, args ...any) {
	if l.cfg.Logger == nil {
		return
	}
	l.cfg.Logger(fmt.Sprintf(format, args...))
}
