package workerdaemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

// fakeHBClient satisfies both CenterClient (Enroll/Heartbeat) and ControlClient
// (ConnectControl/PullCommands/AckControl) so the Runtime can run end-to-end
// while we observe heartbeat timing. Heartbeats are signalled on hbCh; the last
// snapshots received are recorded for the adaptive-cadence test.
type fakeHBClient struct {
	mu       sync.Mutex
	hbN      int
	hbCh     chan struct{}
	lastSnap map[string]concurrency.AgentSnapshot
}

func (f *fakeHBClient) Enroll(ctx context.Context, workerID string, caps []string) error {
	return nil
}

func (f *fakeHBClient) Heartbeat(ctx context.Context, workerID string, caps []string, snaps map[string]concurrency.AgentSnapshot) error {
	f.mu.Lock()
	f.hbN++
	n := f.hbN
	f.lastSnap = snaps
	f.mu.Unlock()
	if n == 1 {
		select {
		case f.hbCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *fakeHBClient) ConnectControl(ctx context.Context, workerID string) (int64, error) {
	return 0, nil
}
func (f *fakeHBClient) PullCommands(ctx context.Context, workerID string, after int64) ([]ControlCommand, error) {
	return nil, nil
}
func (f *fakeHBClient) AckControl(ctx context.Context, workerID string, offset int64) error {
	return nil
}

// v2.7 #154: the daemon must heartbeat IMMEDIATELY on startup so the worker
// shows online within ~1 RTT, not after a full HeartbeatEvery ticker interval.
// HeartbeatEvery is set to 1h here, so the only way a heartbeat lands within the
// 2s window is the immediate startup heartbeat (the ticker won't have fired).
func TestRuntime_HeartbeatsImmediatelyOnStart(t *testing.T) {
	fc := &fakeHBClient{hbCh: make(chan struct{}, 1)}
	rt := NewRuntime(RuntimeConfig{
		WorkerID:          "w-154",
		SkipInitialEnroll: true,
		HeartbeatEvery:    time.Hour, // ticker will NOT fire during the test
		PollInterval:      time.Hour, // keep the control loop idle
		ControlClient:     fc,
	}, fc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = rt.Run(ctx) }()

	select {
	case <-fc.hbCh:
		// Immediate startup heartbeat observed (well before HeartbeatEvery=1h).
	case <-time.After(2 * time.Second):
		t.Fatal("no heartbeat within 2s of startup — worker would stay offline until the first ticker tick (#154)")
	}
}

// snapHandler is a CommandHandler that also reports a fixed concurrency snapshot.
type snapHandler struct {
	snaps map[string]concurrency.AgentSnapshot
}

func (snapHandler) Handle(_ context.Context, _ ControlCommand) error { return nil }
func (h snapHandler) SnapshotConcurrency() map[string]concurrency.AgentSnapshot {
	return h.snaps
}

// v2.19.0: the heartbeat cadence is ADAPTIVE — with a live executor it beats at the
// fast ActiveHeartbeatEvery; idle it falls back to HeartbeatEvery. With idle=1h and
// active=20ms, only the adaptive fast path can produce many beats in a short window.
func TestRuntime_AdaptiveHeartbeat_FastWhenActive(t *testing.T) {
	fc := &fakeHBClient{hbCh: make(chan struct{}, 1)}
	handler := snapHandler{snaps: map[string]concurrency.AgentSnapshot{
		"a1": {Active: 1, Executors: []concurrency.ExecutorSnapshot{{ExecutorID: "e1", State: concurrency.StateRunning}}},
	}}
	rt := NewRuntime(RuntimeConfig{
		WorkerID:             "w-adaptive",
		SkipInitialEnroll:    true,
		HeartbeatEvery:       time.Hour,             // idle cadence would allow ~1 beat
		ActiveHeartbeatEvery: 20 * time.Millisecond, // fast cadence while active
		PollInterval:         time.Hour,
		ControlClient:        fc,
		ControlHandler:       handler,
	}, fc)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = rt.Run(ctx) }()
	time.Sleep(250 * time.Millisecond)
	cancel()

	fc.mu.Lock()
	n := fc.hbN
	lastSnap := fc.lastSnap
	fc.mu.Unlock()

	// Fast cadence (20ms) over ~250ms → many beats; idle (1h) would give ~1.
	if n < 5 {
		t.Errorf("adaptive heartbeat fired %d times in 250ms; want many (fast cadence while active)", n)
	}
	// The snapshot rode the heartbeat.
	if lastSnap == nil || lastSnap["a1"].Active != 1 {
		t.Errorf("heartbeat should carry the agent snapshot, got %+v", lastSnap)
	}
}

// Idle (no active executors) → only the immediate startup beat within the window.
func TestRuntime_AdaptiveHeartbeat_IdleStaysSlow(t *testing.T) {
	fc := &fakeHBClient{hbCh: make(chan struct{}, 1)}
	handler := snapHandler{snaps: nil} // no live executors
	rt := NewRuntime(RuntimeConfig{
		WorkerID:             "w-idle",
		SkipInitialEnroll:    true,
		HeartbeatEvery:       time.Hour,
		ActiveHeartbeatEvery: 20 * time.Millisecond,
		PollInterval:         time.Hour,
		ControlClient:        fc,
		ControlHandler:       handler,
	}, fc)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = rt.Run(ctx) }()
	time.Sleep(250 * time.Millisecond)
	cancel()

	fc.mu.Lock()
	n := fc.hbN
	fc.mu.Unlock()
	// Only the immediate startup beat; the next is scheduled 1h out.
	if n != 1 {
		t.Errorf("idle heartbeat fired %d times in 250ms; want exactly 1 (immediate beat, then idle cadence)", n)
	}
}
