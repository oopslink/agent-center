package workforce

import (
	"errors"
	"testing"
	"time"
)

func newTestMapping(t *testing.T) *WorkerProjectMapping {
	t.Helper()
	m, err := NewWorkerProjectMapping(NewMappingInput{
		ID:               "M-1",
		WorkerID:         "W-1",
		ProjectID:        "p-1",
		BasePath:         "/home/w/p",
		SourceProposalID: "PR-1",
		AddedAt:          time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWorkerProjectMapping: %v", err)
	}
	return m
}

func TestMapping_New_Happy(t *testing.T) {
	m := newTestMapping(t)
	if m.Status() != MappingActive {
		t.Fatal("status")
	}
	if m.Version() != 1 {
		t.Fatal("version")
	}
}

func TestMapping_New_RejectsEmptyFields(t *testing.T) {
	cases := []NewMappingInput{
		{},
		{ID: "M-1"},
		{ID: "M-1", WorkerID: "W-1"},
		{ID: "M-1", WorkerID: "W-1", ProjectID: "p-1"},
		{ID: "M-1", WorkerID: "W-1", ProjectID: "p-1", BasePath: "/x"},
	}
	for i, in := range cases {
		if _, err := NewWorkerProjectMapping(in); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestMapping_Invalidate(t *testing.T) {
	m := newTestMapping(t)
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := m.Invalidate(at, InvalidateReasonPathMissing, "base_path gone"); err != nil {
		t.Fatal(err)
	}
	if m.Status() != MappingInvalidated {
		t.Fatal("status")
	}
	if m.InvalidatedAt() == nil || !m.InvalidatedAt().Equal(at) {
		t.Fatalf("invalidated_at: %v", m.InvalidatedAt())
	}
	if m.InvalidateReason() != InvalidateReasonPathMissing {
		t.Fatal("reason")
	}
	if m.InvalidateMessage() == "" {
		t.Fatal("message")
	}
	if m.Version() != 2 {
		t.Fatalf("version: %d", m.Version())
	}
}

func TestMapping_Invalidate_AlreadyInvalid(t *testing.T) {
	m := newTestMapping(t)
	_ = m.Invalidate(time.Now(), InvalidateReasonPathMissing, "x")
	err := m.Invalidate(time.Now(), InvalidateReasonPathMissing, "x")
	if !errors.Is(err, ErrMappingNotActive) {
		t.Fatalf("expected ErrMappingNotActive, got %v", err)
	}
}

func TestMapping_Invalidate_RequiresReason(t *testing.T) {
	m := newTestMapping(t)
	if err := m.Invalidate(time.Now(), "bogus", "x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMapping_Invalidate_RequiresMessage(t *testing.T) {
	m := newTestMapping(t)
	if err := m.Invalidate(time.Now(), InvalidateReasonPathMissing, ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestMapping_RehydrateBadStatus(t *testing.T) {
	_, err := RehydrateWorkerProjectMapping(RehydrateMappingInput{
		Status:  "bogus",
		Version: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMapping_RehydrateBadVersion(t *testing.T) {
	_, err := RehydrateWorkerProjectMapping(RehydrateMappingInput{
		Status:  MappingActive,
		Version: 0,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMappingStatus_Validation(t *testing.T) {
	if !MappingActive.IsValid() || !MappingInvalidated.IsValid() {
		t.Fatal()
	}
	if MappingStatus("x").IsValid() {
		t.Fatal()
	}
	if MappingActive.String() != "active" {
		t.Fatal()
	}
}

func TestInvalidateReason_Validation(t *testing.T) {
	for _, r := range []InvalidateReason{
		InvalidateReasonPathMissing,
		InvalidateReasonNotGitRepo,
		InvalidateReasonManualRemove,
	} {
		if !r.IsValid() {
			t.Fatalf("expected valid: %v", r)
		}
	}
	if InvalidateReason("x").IsValid() {
		t.Fatal()
	}
	if InvalidateReasonPathMissing.String() != "path_missing" {
		t.Fatal()
	}
}
