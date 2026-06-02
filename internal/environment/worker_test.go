package environment

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
)

func TestWorker_ConnectDisconnectHeartbeat(t *testing.T) {
	w, err := NewWorker(NewWorkerInput{ID: "W1", Name: "box", CreatedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if w.Status() != WorkerOffline {
		t.Fatalf("new worker should be offline, got %s", w.Status())
	}
	w.Connect(time.Unix(2, 0))
	if w.Status() != WorkerOnline || w.LastHeartbeatAt().IsZero() {
		t.Fatalf("connect → online + heartbeat, got %s / %v", w.Status(), w.LastHeartbeatAt())
	}
	w.Disconnect(time.Unix(3, 0))
	if w.Status() != WorkerOffline {
		t.Fatalf("disconnect → offline, got %s", w.Status())
	}
	w.Heartbeat(time.Unix(4, 0))
	if w.Status() != WorkerOnline {
		t.Fatalf("heartbeat → online, got %s", w.Status())
	}
}

func TestWorker_AckOffsetMonotonic(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{ID: "W1", CreatedAt: time.Unix(1, 0)})
	w.AckOffset(3, time.Unix(2, 0))
	if w.LastAckedOffset() != 3 {
		t.Fatalf("ack 3 → 3, got %d", w.LastAckedOffset())
	}
	// Stale/replayed ack (<= current) is a tolerated no-op — never regresses.
	w.AckOffset(1, time.Unix(3, 0))
	if w.LastAckedOffset() != 3 {
		t.Fatalf("stale ack must not regress cursor, got %d", w.LastAckedOffset())
	}
	w.AckOffset(5, time.Unix(4, 0))
	if w.LastAckedOffset() != 5 {
		t.Fatalf("forward ack 5 → 5, got %d", w.LastAckedOffset())
	}
}

// TestWorker_StatusFeedsAvailability proves the D1-confirmed mapping (boundary
// #4): an Environment Worker's status → the workerOnline input of the Agent BC's
// DeriveAvailability. The REAL source switch (agents reading Environment Worker
// instead of workforce.Worker) is D2; D1 only proves the mapping.
func TestWorker_StatusFeedsAvailability(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{ID: "W1", CreatedAt: time.Unix(1, 0)})
	online := func() bool { return w.Status() == WorkerOnline }

	// Offline worker → every agent on it is unavailable, regardless of lifecycle.
	if got := agent.DeriveAvailability(online(), agent.LifecycleRunning, true); got != agent.Unavailable {
		t.Fatalf("offline worker → unavailable, got %s", got)
	}
	w.Connect(time.Unix(2, 0))
	if got := agent.DeriveAvailability(online(), agent.LifecycleRunning, false); got != agent.Available {
		t.Fatalf("online + running + idle → available, got %s", got)
	}
	if got := agent.DeriveAvailability(online(), agent.LifecycleRunning, true); got != agent.Busy {
		t.Fatalf("online + running + active work → busy, got %s", got)
	}
	if got := agent.DeriveAvailability(online(), agent.LifecycleStopped, false); got != agent.Unavailable {
		t.Fatalf("online + stopped → unavailable, got %s", got)
	}
}
