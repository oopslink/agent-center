package workerdaemon

// control_loop_park_test.go — issue-13e7bfe8 layer 3: the HOL ESCAPE.
//
// The pre-fix loop had an alarm but no escape: a command whose handler kept failing was
// retried forever, and because the control cursor is shared by EVERY agent on the worker,
// that starved all of them. In prod the HOL-BLOCKED alarm fired exactly as designed and
// the loop still burned 420 consecutive retries with the worker doing nothing. These
// tests pin that the loop now gives up on a hopeless head command and drains the rest.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// parkLoop builds a loop whose Logger captures HOL-PARKED lines.
func parkLoop(t *testing.T, fs *controlFakeServer, rec *recordingHandler, sink *[]string, mu *sync.Mutex) *ControlLoop {
	t.Helper()
	return NewControlLoop(ControlLoopConfig{
		WorkerID:     "w-1",
		PollInterval: time.Millisecond,
		Handler:      rec,
		Logger: func(m string) {
			if containsStr(m, "HOL-PARKED") {
				mu.Lock()
				*sink = append(*sink, m)
				mu.Unlock()
			}
		},
	}, newControlTestClient(t, fs))
}

// TestControlLoop_HOLParkReleasesTheStarvedStream is the layer-3 regression lock: a
// permanently failing head command must be PARKED so the commands queued behind it — which
// belong to OTHER agents on this worker — finally run.
//
// Pre-fix this test would loop forever without cmd-2 ever being handled.
func TestControlLoop_HOLParkReleasesTheStarvedStream(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("agent.work", "{}", "k1")           // cmd-1: the poisoned head
	fs.seed("agent.work_available", "{}", "k2") // cmd-2: an innocent command behind it

	rec := &recordingHandler{failOn: "cmd-1", failErr: errors.New("permanently wedged")}
	var mu sync.Mutex
	var parked []string
	loop := parkLoop(t, fs, rec, &parked, &mu)

	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}

	// Retry right up to (not past) the park threshold: the head is still blocking, so
	// cmd-2 must NOT have run — head-of-line ordering still holds below the threshold.
	for i := 0; i < holParkThreshold-1; i++ {
		loop.pollOnce(ctx)
	}
	mu.Lock()
	nParked := len(parked)
	mu.Unlock()
	if nParked != 0 {
		t.Fatalf("parked before the retry budget was exhausted (%d): a transient failure must still be retried", nParked)
	}
	for _, id := range rec.ids() {
		if id == "cmd-2" {
			t.Fatalf("cmd-2 ran while the head was still within its retry budget — HOL ordering must hold until the loop gives up")
		}
	}

	// The poll that exhausts the budget parks the head and lets the stream drain.
	loop.pollOnce(ctx)

	mu.Lock()
	got := append([]string(nil), parked...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("want exactly 1 HOL-PARKED line at the threshold, got %d: %v", len(got), got)
	}
	// The park is lossy, so it must be greppable and self-explanatory.
	for _, want := range []string{"offset=1", "type=agent.work", "DROPPED", "permanently wedged"} {
		if !containsStr(got[0], want) {
			t.Fatalf("HOL-PARKED line missing %q: %s", want, got[0])
		}
	}

	// The escape's whole purpose: the command behind the poisoned head finally ran.
	var sawCmd2 bool
	for _, id := range rec.ids() {
		if id == "cmd-2" {
			sawCmd2 = true
		}
	}
	if !sawCmd2 {
		t.Fatalf("cmd-2 never ran after the head was parked: handled = %v — the worker is still starved", rec.ids())
	}
	// The cursor advanced past BOTH: nothing is left to re-deliver forever.
	if fs.ackedOffset() != 2 {
		t.Fatalf("acked offset = %d, want 2 (park + the drained command)", fs.ackedOffset())
	}
}

// TestControlLoop_TransientFailureRecoversWithoutParking guards the other direction: the
// escape must not fire for an ordinary transient failure. A command that fails a few
// times and then succeeds must be handled normally, never dropped.
func TestControlLoop_TransientFailureRecoversWithoutParking(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("agent.work", "{}", "k1")

	rec := &recordingHandler{failOn: "cmd-1", failErr: errors.New("agent restarting")}
	var mu sync.Mutex
	var parked []string
	loop := parkLoop(t, fs, rec, &parked, &mu)

	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	for i := 0; i < 5; i++ { // a few transient failures, well under the budget
		loop.pollOnce(ctx)
	}
	rec.failOn = "" // the agent came back
	loop.pollOnce(ctx)

	mu.Lock()
	nParked := len(parked)
	mu.Unlock()
	if nParked != 0 {
		t.Fatalf("a transient failure must never be parked (dropped), got %d park(s)", nParked)
	}
	if fs.ackedOffset() != 1 {
		t.Fatalf("acked offset = %d, want 1 (handled after recovery)", fs.ackedOffset())
	}
}

// TestControlLoop_ParkCounterResetsPerHead pins that the retry budget is per stuck head,
// not a global tally: clearing one block must not leave the next command pre-charged
// toward a park it never earned.
func TestControlLoop_ParkCounterResetsPerHead(t *testing.T) {
	fs := newControlFakeServer()
	fs.seed("agent.work", "{}", "k1")

	rec := &recordingHandler{failOn: "cmd-1", failErr: errors.New("transient")}
	var mu sync.Mutex
	var parked []string
	loop := parkLoop(t, fs, rec, &parked, &mu)

	ctx := context.Background()
	if !loop.connect(ctx) {
		t.Fatal("connect failed")
	}
	for i := 0; i < holParkThreshold-1; i++ {
		loop.pollOnce(ctx)
	}
	rec.failOn = ""
	loop.pollOnce(ctx) // cmd-1 succeeds → the block clears and the counter resets

	if loop.stuckFails != 0 {
		t.Fatalf("stuckFails = %d after the block cleared, want 0", loop.stuckFails)
	}

	// A NEW head that fails once must be nowhere near the park threshold.
	fs.seed("agent.work", "{}", "k2")
	rec.failOn = "cmd-2"
	loop.pollOnce(ctx)
	mu.Lock()
	nParked := len(parked)
	mu.Unlock()
	if nParked != 0 {
		t.Fatalf("a fresh head failing once must not park: the budget is per-head, not cumulative")
	}
	if loop.stuckFails != 1 {
		t.Fatalf("stuckFails = %d for a fresh head's first failure, want 1", loop.stuckFails)
	}
}
