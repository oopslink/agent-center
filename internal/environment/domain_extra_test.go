package environment

import (
	"testing"
	"time"
)

func TestNewWorker_Validation(t *testing.T) {
	if _, err := NewWorker(NewWorkerInput{CreatedAt: time.Unix(1, 0)}); err == nil {
		t.Fatal("missing id should error")
	}
	// v2.7 #140 step-3: org is no longer required on the control-channel Worker.
	if _, err := NewWorker(NewWorkerInput{ID: "W1"}); err == nil {
		t.Fatal("zero created_at should error")
	}
}

func TestRehydrateWorker_RoundTripAndGuards(t *testing.T) {
	at := time.Unix(1_700_000_000, 0).UTC()
	w, err := RehydrateWorker(RehydrateWorkerInput{
		ID: "W1", Name: "box", Status: WorkerOnline,
		LastAckedOffset: 7, LastHeartbeatAt: at, CreatedAt: at, UpdatedAt: at, Version: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.ID() != "W1" || w.Name() != "box" ||
		w.Status() != WorkerOnline || w.LastAckedOffset() != 7 || w.Version() != 4 ||
		!w.LastHeartbeatAt().Equal(at) || !w.CreatedAt().Equal(at) || !w.UpdatedAt().Equal(at) {
		t.Fatalf("rehydrate round-trip mismatch: %+v", w)
	}
	if _, err := RehydrateWorker(RehydrateWorkerInput{ID: "W1", Status: "bogus", Version: 1}); err != ErrInvalidWorkerStatus {
		t.Fatalf("bad status → ErrInvalidWorkerStatus, got %v", err)
	}
	if _, err := RehydrateWorker(RehydrateWorkerInput{ID: "W1", Status: WorkerOffline, Version: 0}); err == nil {
		t.Fatal("version<1 should error")
	}
}

func TestNewWorkerControlEvent_Validation(t *testing.T) {
	base := NewWorkerControlEventInput{ID: "e1", WorkerID: "W1", Offset: 1, IdempotencyKey: "k", CommandType: "agent.start"}
	if _, err := NewWorkerControlEvent(base); err != nil {
		t.Fatalf("valid event should construct: %v", err)
	}
	bad := base
	bad.CommandType = ""
	if _, err := NewWorkerControlEvent(bad); err != ErrEmptyCommandType {
		t.Fatalf("want ErrEmptyCommandType, got %v", err)
	}
	bad = base
	bad.IdempotencyKey = ""
	if _, err := NewWorkerControlEvent(bad); err != ErrEmptyIdempotencyKey {
		t.Fatalf("want ErrEmptyIdempotencyKey, got %v", err)
	}
	bad = base
	bad.Offset = 0
	if _, err := NewWorkerControlEvent(bad); err != ErrOffsetRegress {
		t.Fatalf("offset<1 → ErrOffsetRegress, got %v", err)
	}
	bad = base
	bad.WorkerID = ""
	if _, err := NewWorkerControlEvent(bad); err == nil {
		t.Fatal("empty worker id should error")
	}
	// createdAt defaulting branch.
	noTime := base
	noTime.CreatedAt = time.Time{}
	if e, err := NewWorkerControlEvent(noTime); err != nil || e.CreatedAt().IsZero() {
		t.Fatalf("createdAt should default to now: %v / %v", err, e)
	}
}

func TestNewControlLog_DefaultsClock(t *testing.T) {
	if l := NewControlLog(newFakeEventRepo(), nil, nil); l == nil || l.clock == nil {
		t.Fatal("NewControlLog should default the clock when nil")
	}
}
