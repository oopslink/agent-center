package workforce

import (
	"errors"
	"testing"
	"time"
)

func newTestWorker(t *testing.T) *Worker {
	t.Helper()
	w, err := NewWorker(NewWorkerInput{
		ID:           "W-1",
		Capabilities: []string{"claude-code"},
		EnrolledAt:   time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return w
}

func TestWorker_Enroll_Happy(t *testing.T) {
	w := newTestWorker(t)
	if w.Status() != WorkerOffline {
		t.Fatalf("initial status: %s", w.Status())
	}
	if w.Version() != 1 {
		t.Fatalf("initial version: %d", w.Version())
	}
}

func TestWorker_Enroll_RejectsEmptyID(t *testing.T) {
	_, err := NewWorker(NewWorkerInput{ID: "", EnrolledAt: time.Now()})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorker_Enroll_RejectsBadID(t *testing.T) {
	for _, id := range []WorkerID{"bad space", "bad/slash", "x*y"} {
		_, err := NewWorker(NewWorkerInput{ID: id, EnrolledAt: time.Now()})
		if err == nil {
			t.Fatalf("expected error for %q", id)
		}
	}
}

func TestWorker_Enroll_RejectsTooLongID(t *testing.T) {
	long := WorkerID("")
	for i := 0; i < 200; i++ {
		long += "a"
	}
	_, err := NewWorker(NewWorkerInput{ID: long, EnrolledAt: time.Now()})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorker_Enroll_RejectsZeroEnrolledAt(t *testing.T) {
	_, err := NewWorker(NewWorkerInput{ID: "W-1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorker_MarkOnline_FromOffline(t *testing.T) {
	w := newTestWorker(t)
	at := time.Now().UTC()
	w.MarkOnline(at)
	if w.Status() != WorkerOnline {
		t.Fatal("status not online")
	}
	if w.Version() != 2 {
		t.Fatalf("version: %d", w.Version())
	}
	if w.OnlineAt() == nil || !w.OnlineAt().Equal(at) {
		t.Fatalf("online_at: %v", w.OnlineAt())
	}
}

func TestWorker_MarkOnline_AlreadyOnline_Noop(t *testing.T) {
	w := newTestWorker(t)
	w.MarkOnline(time.Now())
	v1 := w.Version()
	w.MarkOnline(time.Now())
	if w.Version() != v1 {
		t.Fatalf("version changed on noop: %d", w.Version())
	}
}

func TestWorker_MarkOffline(t *testing.T) {
	w := newTestWorker(t)
	w.MarkOnline(time.Now())
	at := time.Now()
	err := w.MarkOffline(at, OfflineReasonHeartbeatTimeout, "60s silent")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status() != WorkerOffline {
		t.Fatal("status not offline")
	}
	if w.OfflineReason() != OfflineReasonHeartbeatTimeout {
		t.Fatal("reason")
	}
	if w.OfflineMessage() != "60s silent" {
		t.Fatal("message")
	}
}

func TestWorker_MarkOffline_RequiresMessage(t *testing.T) {
	w := newTestWorker(t)
	w.MarkOnline(time.Now())
	err := w.MarkOffline(time.Now(), OfflineReasonShutdown, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorker_MarkOffline_RequiresValidReason(t *testing.T) {
	w := newTestWorker(t)
	w.MarkOnline(time.Now())
	err := w.MarkOffline(time.Now(), "bogus", "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorker_MarkOffline_AlreadyOffline_Noop(t *testing.T) {
	w := newTestWorker(t)
	v := w.Version()
	if err := w.MarkOffline(time.Now(), OfflineReasonShutdown, "off"); err != nil {
		t.Fatal(err)
	}
	if w.Version() != v {
		t.Fatalf("version changed: %d", w.Version())
	}
}

func TestWorker_Heartbeat(t *testing.T) {
	w := newTestWorker(t)
	at := time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC)
	if err := w.Heartbeat(at, 30); err != nil {
		t.Fatal(err)
	}
	if w.LastHeartbeatAt() == nil || !w.LastHeartbeatAt().Equal(at) {
		t.Fatalf("heartbeat_at: %v", w.LastHeartbeatAt())
	}
	if w.WorkingSeconds() != 30 {
		t.Fatal("working seconds")
	}
}

func TestWorker_Heartbeat_NegativeRejected(t *testing.T) {
	w := newTestWorker(t)
	if err := w.Heartbeat(time.Now(), -1); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorker_CapabilitiesJSON(t *testing.T) {
	w := newTestWorker(t)
	b, err := w.CapabilitiesJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `["claude-code"]` {
		t.Fatalf("got %s", b)
	}
}

func TestWorker_CapabilitiesJSON_Empty(t *testing.T) {
	w, err := NewWorker(NewWorkerInput{
		ID:         "W-1",
		EnrolledAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.CapabilitiesJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Fatalf("expected [], got %s", b)
	}
}

func TestWorker_RehydrateInvalidStatus(t *testing.T) {
	_, err := RehydrateWorker(RehydrateWorkerInput{
		ID:         "W-1",
		Status:     "bogus",
		EnrolledAt: time.Now(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Version:    1,
	})
	if !errors.Is(err, ErrWorkerInvalidStatus) {
		t.Fatalf("expected ErrWorkerInvalidStatus, got %v", err)
	}
}

func TestWorker_RehydrateBadVersion(t *testing.T) {
	_, err := RehydrateWorker(RehydrateWorkerInput{
		ID:      "W-1",
		Status:  WorkerOnline,
		Version: 0,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorker_RehydrateBadID(t *testing.T) {
	_, err := RehydrateWorker(RehydrateWorkerInput{
		ID:      "",
		Status:  WorkerOnline,
		Version: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkerStatus_Validation(t *testing.T) {
	if !WorkerOnline.IsValid() || !WorkerOffline.IsValid() {
		t.Fatal()
	}
	if WorkerStatus("nope").IsValid() {
		t.Fatal()
	}
	if WorkerOnline.String() != "online" {
		t.Fatal()
	}
}

func TestOfflineReason_Validation(t *testing.T) {
	if !OfflineReasonHeartbeatTimeout.IsValid() {
		t.Fatal()
	}
	if OfflineReason("nope").IsValid() {
		t.Fatal()
	}
}

func TestWorkerID_String(t *testing.T) {
	if WorkerID("W-1").String() != "W-1" {
		t.Fatal()
	}
}

func TestWorker_Capabilities_Dedup(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{
		ID:           "W-1",
		Capabilities: []string{"a", "b", "a", "c", "b"},
		EnrolledAt:   time.Now(),
	})
	caps := w.Capabilities()
	if len(caps) != 3 {
		t.Fatalf("expected 3 unique, got %d: %v", len(caps), caps)
	}
}

func TestWorker_Capabilities_IsCopy(t *testing.T) {
	w := newTestWorker(t)
	caps := w.Capabilities()
	caps[0] = "MUTATED"
	again := w.Capabilities()
	if again[0] == "MUTATED" {
		t.Fatal("Capabilities() leaked internal slice")
	}
}
