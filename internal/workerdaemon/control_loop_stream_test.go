package workerdaemon

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeStreamClient is a programmable StreamClient for the control loop's
// stream-first / poll-fallback tests. It serves commands with offset > after
// from a shared log, then ends with a configured outcome (EOF / error /
// heartbeat-timeout) so the loop falls back to poll. It records the `after`
// (cursor) it was opened with, proving stream + poll share the offset cursor.
type fakeStreamClient struct {
	mu sync.Mutex

	log        []ControlCommand // shared command log (offsets ascending from 1)
	openAfters []int64          // each StreamCommands call's after= (cursor)
	calls      int

	// endErr is returned after delivering the eligible commands (default a
	// generic disconnect → loop falls back to poll).
	endErr error
	// maxCallsBeforeDisable: after this many opens, StreamCommands returns
	// immediately with endErr WITHOUT delivering (simulates a wedged subscriber
	// so the test can prove the poll fallback backfills). 0 = unlimited.
	deliverUntilCall int
	// deliverErrAfterOffset: if >0, stop delivering at this offset and return
	// endErr (simulates a mid-stream drop / replay-incomplete).
	deliverErrAfterOffset int64
}

func (f *fakeStreamClient) StreamCommands(ctx context.Context, workerID string, after int64, idle time.Duration, onCommand func(ControlCommand) error) error {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.openAfters = append(f.openAfters, after)
	log := append([]ControlCommand(nil), f.log...)
	endErr := f.endErr
	deliverUntil := f.deliverUntilCall
	dropAt := f.deliverErrAfterOffset
	f.mu.Unlock()

	if endErr == nil {
		endErr = errors.New("stream disconnect")
	}
	if deliverUntil > 0 && call > deliverUntil {
		return endErr // wedged: deliver nothing → poll must backfill.
	}
	for _, c := range log {
		if c.Offset <= after {
			continue
		}
		if dropAt > 0 && c.Offset > dropAt {
			break // simulate ring-evict / replay-incomplete mid-stream.
		}
		if err := onCommand(c); err != nil {
			return err
		}
	}
	return endErr
}

func (f *fakeStreamClient) afters() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.openAfters...)
}

// seedShared appends the same command to BOTH the fake stream log and the poll
// fake server, so the two transports serve a consistent log (as the real
// center does — one WorkerControlEvent log behind both).
func seedShared(fs *controlFakeServer, st *fakeStreamClient, cmdType, payload, idem string) {
	fs.seed(cmdType, payload, idem)
	fs.mu.Lock()
	c := fs.cmds[len(fs.cmds)-1]
	fs.mu.Unlock()
	st.mu.Lock()
	st.log = append(st.log, c)
	st.mu.Unlock()
}

func newStreamLoop(t *testing.T, fs *controlFakeServer, st *fakeStreamClient, rec CommandHandler, logf func(string)) *ControlLoop {
	t.Helper()
	client := newControlTestClient(t, fs)
	return NewControlLoop(ControlLoopConfig{
		WorkerID:          "w-1",
		PollInterval:      time.Millisecond,
		Handler:           rec,
		StreamClient:      st,
		StreamIdleTimeout: 50 * time.Millisecond,
		Logger:            logf,
	}, client)
}

// drainOneCycle runs ONE Run iteration's worth of work deterministically:
// streamOnce (if enabled) then pollOnce — exactly what the Run loop does per
// connected tick. Avoids racing the real ticker.
func (l *ControlLoop) drainOneCycle(ctx context.Context) {
	if l.streamEnabled() {
		_ = l.streamOnce(ctx)
	}
	l.pollOnce(ctx)
}

// TestStream_HandleAckCursor_SameAsPoll: streamed commands are handled in-order,
// acked cumulatively (HTTP POST), and the cursor advances — IDENTICALLY to poll.
// The server ack cursor (POST up) reaches the highest offset.
func TestStream_HandleAckCursor_SameAsPoll(t *testing.T) {
	fs := newControlFakeServer()
	st := &fakeStreamClient{}
	seedShared(fs, st, "noop", "{}", "k1")
	seedShared(fs, st, "noop", "{}", "k2")
	seedShared(fs, st, "noop", "{}", "k3")

	rec := &recordingHandler{}
	loop := newStreamLoop(t, fs, st, rec, nil)
	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	loop.drainOneCycle(ctx)

	if got := rec.ids(); len(got) != 3 || got[0] != "cmd-1" || got[2] != "cmd-3" {
		t.Fatalf("streamed handle order wrong: %v", got)
	}
	if loop.Cursor() != 3 {
		t.Fatalf("cursor = %d, want 3 (advanced over stream)", loop.Cursor())
	}
	if fs.ackedOffset() != 3 {
		t.Fatalf("server acked = %d, want 3 (ack POST up while SSE down)", fs.ackedOffset())
	}
	// First stream opened at the connect cursor (0) — offset is the resume key.
	if a := st.afters(); a[0] != 0 {
		t.Fatalf("first stream after = %d, want 0", a[0])
	}
}

// TestStream_115BriefDeliveredOverStream: the full command payload (the #115
// work brief) is delivered identically over the stream — no payload field
// dropped. The handler captures the payload it received.
func TestStream_115BriefDeliveredOverStream(t *testing.T) {
	fs := newControlFakeServer()
	st := &fakeStreamClient{}
	brief := `{"work_item_id":"wi-7","brief":"build the thing","task_ref":"t-7"}`
	seedShared(fs, st, "agent.work", brief, "k1")

	rec := &recordingHandler{}
	loop := newStreamLoop(t, fs, st, rec, nil)
	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	loop.drainOneCycle(ctx)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.handled) != 1 {
		t.Fatalf("handled %d, want 1", len(rec.handled))
	}
	if rec.handled[0].Payload != brief {
		t.Fatalf("#115 brief dropped over stream: got %q want %q", rec.handled[0].Payload, brief)
	}
	if rec.handled[0].CommandType != "agent.work" {
		t.Fatalf("command_type lost over stream: %q", rec.handled[0].CommandType)
	}
}

// TestStream_DisconnectThenPollFallback_NoLoss: the stream wedges (delivers
// nothing); the poll fallback in the SAME cycle backfills from the offset cursor
// — no command lost. Then a later cycle re-attempts the stream (stream-first).
func TestStream_DisconnectThenPollFallback_NoLoss(t *testing.T) {
	fs := newControlFakeServer()
	// deliverUntilCall=0 → every open has call(>=1) > 0 → returns endErr WITHOUT
	// delivering (wedged subscriber). The poll fallback must backfill.
	st := &fakeStreamClient{deliverUntilCall: 0}
	seedShared(fs, st, "noop", "{}", "k1")
	seedShared(fs, st, "noop", "{}", "k2")

	rec := &recordingHandler{}
	loop := newStreamLoop(t, fs, st, rec, nil)
	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	loop.drainOneCycle(ctx) // stream wedged → poll backfills

	if got := rec.ids(); len(got) != 2 {
		t.Fatalf("poll fallback did not backfill: handled %v, want 2", got)
	}
	if loop.Cursor() != 2 || fs.ackedOffset() != 2 {
		t.Fatalf("cursor=%d acked=%d, want 2/2 after poll backfill", loop.Cursor(), fs.ackedOffset())
	}
}

// TestStream_RingEvictReplayIncomplete_PollBackfills: the stream delivers only a
// PREFIX (drops at an offset, simulating ring-evict / replay-incomplete); the
// poll fallback backfills the gap from the offset cursor. No command lost, none
// duplicated (cursor dedups).
func TestStream_RingEvictReplayIncomplete_PollBackfills(t *testing.T) {
	fs := newControlFakeServer()
	st := &fakeStreamClient{deliverErrAfterOffset: 1} // delivers offset 1 then drops
	seedShared(fs, st, "noop", "{}", "k1")
	seedShared(fs, st, "noop", "{}", "k2")
	seedShared(fs, st, "noop", "{}", "k3")

	rec := &recordingHandler{}
	loop := newStreamLoop(t, fs, st, rec, nil)
	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	loop.drainOneCycle(ctx) // stream delivers cmd-1, drops; poll backfills 2,3

	got := rec.ids()
	if len(got) != 3 {
		t.Fatalf("backfill incomplete: handled %v, want 3", got)
	}
	// Exactly once each (no dup across stream→poll boundary).
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	for _, id := range []string{"cmd-1", "cmd-2", "cmd-3"} {
		if seen[id] != 1 {
			t.Fatalf("command %s handled %d times, want 1 (no dup across boundary)", id, seen[id])
		}
	}
	if loop.Cursor() != 3 || fs.ackedOffset() != 3 {
		t.Fatalf("cursor=%d acked=%d, want 3/3", loop.Cursor(), fs.ackedOffset())
	}
}

// TestStream_HeartbeatTimeoutFallsBackToPoll: a real AdminClient stream against a
// silent SSE server times out on the idle watchdog and the loop falls through to
// poll, which delivers the commands. Uses the real SSE transport + real poll.
func TestStream_HeartbeatTimeoutFallsBackToPoll(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("noop", "{}", "k1")
	fs.seed("noop", "{}", "k2")

	// Real poll client (httptest TCP) + a real stream client pointed at a SILENT
	// SSE server (holds open, emits nothing → idle timeout).
	pollClient := newControlTestClient(t, fs)
	silent := &sseFakeServer{hold: true, holdFor: 5 * time.Second}
	streamClient := newStreamTestClient(t, silent)

	rec := &recordingHandler{}
	loop := NewControlLoop(ControlLoopConfig{
		WorkerID:          "w-1",
		PollInterval:      time.Millisecond,
		Handler:           rec,
		StreamClient:      streamClient,
		StreamIdleTimeout: 40 * time.Millisecond,
	}, pollClient)

	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	start := time.Now()
	loop.drainOneCycle(ctx) // stream idle-times-out (~40ms) then poll delivers
	if time.Since(start) > time.Second {
		t.Fatalf("cycle took %v — stream hung instead of prompt fallback", time.Since(start))
	}
	if got := rec.ids(); len(got) != 2 {
		t.Fatalf("poll fallback after heartbeat-timeout handled %v, want 2", got)
	}
	if loop.Cursor() != 2 {
		t.Fatalf("cursor = %d, want 2 after fallback", loop.Cursor())
	}
}

// TestStream_PollToStream_SameCursorNoLoss: cycle 1 the stream is wedged → poll
// handles 1,2 (cursor=2). New commands 3,4 arrive. cycle 2 the stream recovers
// and resumes at the SHARED cursor (after=2), delivering only 3,4 — no re-handle
// of 1,2, no loss. Proves stream↔poll share the offset cursor.
func TestStream_PollToStream_SameCursorNoLoss(t *testing.T) {
	fs := newControlFakeServer()
	// deliverUntilCall=0 → cycle-1 stream open (call 1) is wedged (1 > 0); we bump
	// it to 100 before cycle 2 so the recovered stream delivers (2 > 100 false).
	st := &fakeStreamClient{deliverUntilCall: 0}
	seedShared(fs, st, "noop", "{}", "k1")
	seedShared(fs, st, "noop", "{}", "k2")

	rec := &recordingHandler{}
	loop := newStreamLoop(t, fs, st, rec, nil)
	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}

	// Cycle 1: stream wedged (deliverUntilCall=0 → returns immediately), poll backfills 1,2.
	loop.drainOneCycle(ctx)
	if loop.Cursor() != 2 {
		t.Fatalf("after cycle1 cursor=%d, want 2", loop.Cursor())
	}

	// New commands while we were polling.
	seedShared(fs, st, "noop", "{}", "k3")
	seedShared(fs, st, "noop", "{}", "k4")

	// Re-enable stream delivery for cycle 2 (recovered).
	st.mu.Lock()
	st.deliverUntilCall = 100 // now delivers
	st.mu.Unlock()

	loop.drainOneCycle(ctx) // stream resumes at after=2 → delivers 3,4

	got := rec.ids()
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	for _, id := range []string{"cmd-1", "cmd-2", "cmd-3", "cmd-4"} {
		if seen[id] != 1 {
			t.Fatalf("command %s handled %d times, want 1 (no loss/no dup across poll→stream): %v", id, seen[id], got)
		}
	}
	// The recovered stream MUST have opened at the shared cursor 2.
	afters := st.afters()
	last := afters[len(afters)-1]
	if last != 2 {
		t.Fatalf("recovered stream opened at after=%d, want 2 (shared offset cursor)", last)
	}
	if loop.Cursor() != 4 {
		t.Fatalf("final cursor=%d, want 4", loop.Cursor())
	}
}

// TestStream_AckFail_CursorNotAdvanced: when AckControl POST fails, the cursor is
// NOT advanced over the stream path (reuses handleBatch's ack-fail→no-advance) →
// the command is re-delivered next cycle. Proves the shared contract holds for
// the stream transport.
func TestStream_AckFail_CursorNotAdvanced(t *testing.T) {
	fs := newControlFakeServer()
	st := &fakeStreamClient{}
	seedShared(fs, st, "noop", "{}", "k1")

	// Wrap the poll client so AckControl fails the first N calls. In cycle 1 BOTH
	// the stream-path ack AND the poll-fallback ack fire (cmd-1 delivered twice,
	// handler idempotent), so fail BOTH to prove neither advances the cursor.
	client := newControlTestClient(t, fs)
	failing := &ackFailNClient{ControlClient: client, failFirst: 2}
	rec := &recordingHandler{}
	loop := NewControlLoop(ControlLoopConfig{
		WorkerID:          "w-1",
		PollInterval:      time.Millisecond,
		Handler:           rec,
		StreamClient:      st,
		StreamIdleTimeout: 50 * time.Millisecond,
	}, failing)

	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	loop.drainOneCycle(ctx) // stream + poll both deliver cmd-1; both acks fail → cursor NOT advanced

	if loop.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0 (ack failed → not advanced)", loop.Cursor())
	}
	// cmd-1 was handled (at-least-once); the handler is idempotent-by-key so a
	// re-delivery is safe. The KEY invariant: cursor did not advance past it.
	if got := rec.ids(); len(got) < 1 {
		t.Fatalf("cmd-1 should have been handled at least once, got %v", got)
	}

	// Next cycle: ack now succeeds → cursor advances. cmd-1 may be re-handled
	// (idempotent), but the cursor reaches 1.
	loop.drainOneCycle(ctx)
	if loop.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1 after ack recovers", loop.Cursor())
	}
}

// ackFailNClient wraps a ControlClient and fails the first failFirst AckControl
// calls (then delegates). Models a transient ack POST outage.
type ackFailNClient struct {
	ControlClient
	mu        sync.Mutex
	failFirst int
	calls     int
}

func (c *ackFailNClient) AckControl(ctx context.Context, workerID string, offset int64) error {
	c.mu.Lock()
	c.calls++
	if c.calls <= c.failFirst {
		c.mu.Unlock()
		return errors.New("ack POST failed (transient)")
	}
	c.mu.Unlock()
	return c.ControlClient.AckControl(ctx, workerID, offset)
}

// TestStream_HOLPreservedUnderStream: a handler error on the head-of-line command
// over the STREAM does NOT advance the cursor (HOL), and the SAME loud HOL-BLOCKED
// surface fires once the consecutive-failure count crosses the threshold — proving
// the stream path reuses the poll path's HOL contract (shared handleBatch +
// noteStuck). The stream re-delivers the stuck head each cycle (catch-up from the
// un-advanced cursor).
func TestStream_HOLPreservedUnderStream(t *testing.T) {
	fs := newControlFakeServer()
	st := &fakeStreamClient{}
	seedShared(fs, st, "agent.work", "{}", "k1") // cmd-1 is the stuck head

	rec := &recordingHandler{failOn: "cmd-1", failErr: errors.New("no running session (retry after reconcile)")}
	var mu sync.Mutex
	var holLines []string
	loop := newStreamLoop(t, fs, st, rec, func(m string) {
		if len(m) >= len("control: HOL-BLOCKED") && m[:len("control: HOL-BLOCKED")] == "control: HOL-BLOCKED" {
			mu.Lock()
			holLines = append(holLines, m)
			mu.Unlock()
		}
	})

	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}

	// Each cycle the stream re-delivers cmd-1 (cursor un-advanced) and it fails.
	// Note: each drainOneCycle runs stream (1 failure via handleBatch) AND poll
	// (another failure) → 2 failures/cycle. Run enough cycles to cross threshold.
	for i := 0; i < holBlockThreshold; i++ {
		loop.drainOneCycle(ctx)
	}
	mu.Lock()
	n := len(holLines)
	mu.Unlock()
	if n < 1 {
		t.Fatalf("HOL-BLOCKED alarm never fired over stream path (got %d)", n)
	}
	if loop.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0 (HOL — never advance past failed head)", loop.Cursor())
	}
	first := holLines[0]
	for _, want := range []string{"offset=1", "type=agent.work", "starved", "no running session"} {
		if !containsStr(first, want) {
			t.Fatalf("HOL alarm over stream missing %q: %s", want, first)
		}
	}

	// Recovery: clear the failure → next cycle the head is handled, cursor advances,
	// stuck counter resets.
	rec.failOn = ""
	loop.drainOneCycle(ctx)
	if loop.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1 after HOL clears over stream", loop.Cursor())
	}
	if loop.stuckFails != 0 {
		t.Fatalf("stuckFails = %d, want 0 after recovery", loop.stuckFails)
	}
}

// tickRecordingHandler is a CommandHandler that ALSO implements tickHandler, counting
// OnTick invocations (atomic) so a test can assert the self-heal drain keeps firing.
type tickRecordingHandler struct {
	recordingHandler
	ticks atomic.Int64
}

func (h *tickRecordingHandler) OnTick(_ context.Context) { h.ticks.Add(1) }

// TestStream_SilentStream_OnTickStaysPrompt is the regression test for the OnTick
// latency item (FINDING-3 interaction): a SILENT stream (holds open, no frames) must
// NOT block the self-heal relaunch drain. With the old per-tick design streamOnce
// blocked the Run goroutine up to the idle timeout, so OnTick (which drains due
// self-heal relaunches on their backoff cadence) was delayed up to ~idle. With the
// reader-goroutine design the blocking read is off the executor goroutine, so OnTick
// fires every PollInterval regardless of stream idleness. The idle timeout here is set
// LONGER than the observation window, so an OnTick that fired many times PROVES it is
// not gated on the stream — under the old code it would have fired ~0–1 times.
func TestStream_SilentStream_OnTickStaysPrompt(t *testing.T) {
	fs := newControlFakeServer()
	pollClient := newControlTestClient(t, fs)
	// Silent SSE server: holds the connection open for a long time emitting nothing.
	silent := &sseFakeServer{hold: true, holdFor: 5 * time.Second}
	streamClient := newStreamTestClient(t, silent)

	rec := &tickRecordingHandler{}
	loop := NewControlLoop(ControlLoopConfig{
		WorkerID:     "w-1",
		PollInterval: 5 * time.Millisecond,
		Handler:      rec,
		StreamClient: streamClient,
		// Idle timeout MUCH longer than the observation window: if OnTick were gated
		// on the stream read it would be starved for ~2s; the new design fires it on
		// the 5ms poll cadence instead.
		StreamIdleTimeout: 2 * time.Second,
	}, pollClient)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	// Within ~250ms the 5ms tick should have driven OnTick many times despite the
	// silent 2s-idle stream. Require a healthy margin (>=10) to rule out the old
	// blocked behaviour (which would yield ~0–1 in this window).
	deadline := time.Now().Add(2 * time.Second)
	for rec.ticks.Load() < 10 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("OnTick fired only %d times in the window — a silent stream is blocking the self-heal drain (regression)", rec.ticks.Load())
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// TestStream_HOL_DoesNotSkipStuckHead: a handler error on the head-of-line command
// over the STREAM must NOT let a LATER command be handled — the stream delivers one
// command at a time, and handling cmd-2 after a stuck cmd-1 would cumulative-ack
// cmd-2's offset and SILENTLY SKIP+LOSE cmd-1. The stream must stop on the stuck head
// and let the poll safety net re-pull the contiguous batch (where the in-batch break
// enforces HOL). This is the §-1 "stream and poll must not fork the delivery contract"
// invariant at the head-of-line boundary (the existing HOL test only seeded the stuck
// head alone, so it could not expose the skip).
func TestStream_HOL_DoesNotSkipStuckHead(t *testing.T) {
	fs := newControlFakeServer()
	st := &fakeStreamClient{}
	seedShared(fs, st, "agent.work", "{}", "k1") // cmd-1: the stuck head (fails)
	seedShared(fs, st, "noop", "{}", "k2")       // cmd-2: must NOT be handled while cmd-1 stuck

	rec := &recordingHandler{failOn: "cmd-1", failErr: errors.New("no running session (retry)")}
	loop := newStreamLoop(t, fs, st, rec, nil)
	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}

	// ONE stream pass: cmd-1 fails → the stream must stop, NOT proceed to cmd-2.
	_ = loop.streamOnce(ctx)

	if loop.Cursor() != 0 {
		t.Fatalf("cursor=%d, want 0 (HOL: must never advance/skip past the stuck head)", loop.Cursor())
	}
	for _, id := range rec.ids() {
		if id == "cmd-2" {
			t.Fatalf("cmd-2 handled over stream while cmd-1 stuck — HOL violated, cmd-1 skipped+lost (handled=%v)", rec.ids())
		}
	}
}

// TestStream_DisabledFallsBackToPollOnly: with DisableStream the loop never opens
// the stream (poll only) — proving the opt-out config. The stream client must see
// zero opens.
func TestStream_DisabledFallsBackToPollOnly(t *testing.T) {
	fs := newControlFakeServer()
	st := &fakeStreamClient{}
	seedShared(fs, st, "noop", "{}", "k1")

	client := newControlTestClient(t, fs)
	rec := &recordingHandler{}
	loop := NewControlLoop(ControlLoopConfig{
		WorkerID:      "w-1",
		PollInterval:  time.Millisecond,
		Handler:       rec,
		StreamClient:  st,
		DisableStream: true, // opt-out → poll only
	}, client)

	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	loop.drainOneCycle(ctx)

	if len(st.afters()) != 0 {
		t.Fatalf("stream opened %d times despite DisableStream, want 0", len(st.afters()))
	}
	if loop.Cursor() != 1 || fs.ackedOffset() != 1 {
		t.Fatalf("poll-only path did not deliver: cursor=%d acked=%d", loop.Cursor(), fs.ackedOffset())
	}
}

// TestStream_RunLoopEndToEnd_RaceClean drives the REAL Run loop (ticker) with a
// real SSE stream + real poll fallback to exercise the stream/poll goroutine
// concurrency under -race. The stream is silent (idle-timeout each tick) so the
// poll path delivers; we assert all commands land and Run exits on ctx cancel.
func TestStream_RunLoopEndToEnd_RaceClean(t *testing.T) {
	fs := newControlFakeServer()
	for i := 0; i < 3; i++ {
		fs.seed("noop", "{}", "k"+strconv.Itoa(i))
	}
	pollClient := newControlTestClient(t, fs)
	silent := &sseFakeServer{hold: true, holdFor: 5 * time.Second}
	streamClient := newStreamTestClient(t, silent)

	rec := &recordingHandler{}
	loop := NewControlLoop(ControlLoopConfig{
		WorkerID:          "w-1",
		PollInterval:      2 * time.Millisecond,
		Handler:           rec,
		StreamClient:      streamClient,
		StreamIdleTimeout: 20 * time.Millisecond,
	}, pollClient)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for fs.ackedOffset() < 3 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("loop never acked all commands (acked=%d)", fs.ackedOffset())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}
