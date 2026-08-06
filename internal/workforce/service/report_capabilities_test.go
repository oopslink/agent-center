package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
)

// reportSuite gives the report-capabilities tests an enroll service (which
// owns ReportCapabilities) plus a config service to simulate the operator
// toggle that must be preserved across re-probe.
type reportSuite struct {
	*suite
	enroll *WorkerEnrollService
	cfg    *WorkerConfigService
}

func setupReportSuite(t *testing.T) *reportSuite {
	t.Helper()
	s := setupSuite(t)
	return &reportSuite{
		suite:  s,
		enroll: NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock),
		cfg:    NewWorkerConfigService(s.db, s.workerRepo, s.sink, s.clock),
	}
}

func seedReportWorker(t *testing.T, s *reportSuite, id workforce.WorkerID, caps []workforce.Capability) *workforce.Worker {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:             id,
		CapabilityList: caps,
		EnrolledAt:     s.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.workerRepo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	return w
}

func capByCLI(t *testing.T, s *reportSuite, id workforce.WorkerID, cli string) (workforce.Capability, bool) {
	t.Helper()
	w, err := s.workerRepo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range w.CapabilityList() {
		if c.AgentCLI == cli {
			return c, true
		}
	}
	return workforce.Capability{}, false
}

// §-1③ + "新探测默认启用": a newly-probed detected CLI lands Enabled=true; a
// newly-probed not-detected CLI is stored (complete表态) but Enabled=false.
func TestWorkerEnrollService_ReportCapabilities_NewDetectedEnabled_NotDetectedStored(t *testing.T) {
	s := setupReportSuite(t)
	seedReportWorker(t, s, "W-1", []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: true},
	})

	_, err := s.enroll.ReportCapabilities(context.Background(), ReportCapabilitiesCommand{
		WorkerID: "W-1",
		Capabilities: []workforce.Capability{
			{AgentCLI: "claude-code", Detected: true, Version: "1.2.3"},
			{AgentCLI: "codex", Detected: true, Version: "0.9"},
			{AgentCLI: "opencode", Detected: false}, // checked but not installed
		},
		ActorIdentity: "user:probe",
	})
	if err != nil {
		t.Fatalf("ReportCapabilities: %v", err)
	}

	codex, ok := capByCLI(t, s, "W-1", "codex")
	if !ok || !codex.Detected || !codex.Enabled {
		t.Fatalf("newly-detected codex should be Detected+Enabled, got %+v ok=%v", codex, ok)
	}
	oc, ok := capByCLI(t, s, "W-1", "opencode")
	if !ok {
		t.Fatal("not-detected opencode should still be stored (complete表态)")
	}
	if oc.Detected || oc.Enabled {
		t.Fatalf("not-detected opencode must be stored Detected=false Enabled=false, got %+v", oc)
	}
	if codex.Version != "0.9.0" {
		t.Fatalf("codex version normalized=%q, want 0.9.0", codex.Version)
	}
	if codex.ScannedAt.IsZero() || codex.ExpiresAt.IsZero() || !codex.Healthy {
		t.Fatalf("codex health window not stamped: %+v", codex)
	}
}

// §-1②: a re-probe MUST preserve a user-disabled toggle (disabled → re-online
// → still disabled). This is the merge red-line PD called out for acceptance.
func TestWorkerEnrollService_ReportCapabilities_PreservesUserDisabledToggle(t *testing.T) {
	s := setupReportSuite(t)
	w := seedReportWorker(t, s, "W-1", []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: true},
		{AgentCLI: "codex", Detected: true, Enabled: true},
	})

	// Operator disables codex.
	if _, err := s.cfg.SetCapabilityEnabled(context.Background(), SetCapabilityEnabledCommand{
		WorkerID:      "W-1",
		AgentCLI:      "codex",
		Enabled:       false,
		Version:       w.Version(),
		ActorIdentity: "user:op",
	}); err != nil {
		t.Fatalf("SetCapabilityEnabled: %v", err)
	}

	// Worker re-probes on next online: codex still installed (detected).
	if _, err := s.enroll.ReportCapabilities(context.Background(), ReportCapabilitiesCommand{
		WorkerID: "W-1",
		Capabilities: []workforce.Capability{
			{AgentCLI: "claude-code", Detected: true},
			{AgentCLI: "codex", Detected: true},
		},
		ActorIdentity: "user:probe",
	}); err != nil {
		t.Fatalf("ReportCapabilities: %v", err)
	}

	codex, _ := capByCLI(t, s, "W-1", "codex")
	if codex.Enabled {
		t.Fatalf("user-disabled codex must stay disabled across re-probe, got %+v", codex)
	}
}

func TestWorkerEnrollService_ReportCapabilities_RejectsUnparseableVersion(t *testing.T) {
	s := setupReportSuite(t)
	seedReportWorker(t, s, "W-1", nil)

	_, err := s.enroll.ReportCapabilities(context.Background(), ReportCapabilitiesCommand{
		WorkerID: "W-1",
		Capabilities: []workforce.Capability{
			{AgentCLI: "codex", Detected: true, Version: "release latest"},
		},
		ActorIdentity: "user:probe",
	})
	if !errors.Is(err, workforce.ErrWorkerCapabilityVersion) {
		t.Fatalf("err=%v, want ErrWorkerCapabilityVersion", err)
	}
}

func TestWorkerEnrollService_ReportCapabilities_OutOfOrderDoesNotClobberNewer(t *testing.T) {
	s := setupReportSuite(t)
	now := s.clock.Now()
	seedReportWorker(t, s, "W-1", []workforce.Capability{{
		AgentCLI: "codex", Detected: true, Enabled: true, Healthy: true,
		Version: "1.2.0", ScannedAt: now, ExpiresAt: now.Add(time.Hour),
	}})

	_, err := s.enroll.ReportCapabilities(context.Background(), ReportCapabilitiesCommand{
		WorkerID: "W-1",
		Capabilities: []workforce.Capability{{
			AgentCLI: "codex", Detected: true, Version: "0.9.0",
			ScannedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		}},
		ActorIdentity: "user:probe",
	})
	if err != nil {
		t.Fatalf("ReportCapabilities: %v", err)
	}
	codex, _ := capByCLI(t, s, "W-1", "codex")
	if codex.Version != "1.2.0" {
		t.Fatalf("out-of-order report clobbered version: %+v", codex)
	}
}
