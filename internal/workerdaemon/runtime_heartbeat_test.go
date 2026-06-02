package workerdaemon

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeHBClient satisfies both CenterClient (Enroll/Heartbeat) and ControlClient
// (ConnectControl/PullCommands/AckControl) so the Runtime can run end-to-end
// while we observe heartbeat timing. Heartbeats are signalled on hbCh.
type fakeHBClient struct {
	mu   sync.Mutex
	hbN  int
	hbCh chan struct{}
}

func (f *fakeHBClient) Enroll(ctx context.Context, workerID string, caps []string) error {
	return nil
}

func (f *fakeHBClient) Heartbeat(ctx context.Context, workerID string, caps []string) error {
	f.mu.Lock()
	f.hbN++
	n := f.hbN
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
