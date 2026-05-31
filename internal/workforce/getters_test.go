package workforce

import (
	"testing"
	"time"
)

func TestWorkerGetters_AllFields(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{
		ID: "W-1", Capabilities: []string{"x"},
		EnrolledAt: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	_ = w.Heartbeat(time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC), 60)
	w.MarkOnline(time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC))
	_ = w.MarkOffline(time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC), OfflineReasonShutdown, "user stopped")
	if w.ID() != "W-1" {
		t.Fatal()
	}
	if w.CreatedAt().IsZero() {
		t.Fatal()
	}
	if w.UpdatedAt().IsZero() {
		t.Fatal()
	}
	if w.EnrolledAt().IsZero() {
		t.Fatal()
	}
	if w.OnlineAt() == nil {
		t.Fatal("online_at should be set")
	}
	if w.OfflineAt() == nil {
		t.Fatal("offline_at should be set")
	}
}
