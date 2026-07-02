package service

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/workforce"
)

// T752: ReportCapabilities persists the worker-reported system info on the same
// online path, so the Worker Profile page shows real host + build values.
func TestReportCapabilities_PersistsSystemInfo(t *testing.T) {
	s := setupReportSuite(t)
	seedReportWorker(t, s, "W-1", nil)

	info := workforce.SystemInfo{
		Hostname:           "dev001.local",
		OS:                 "darwin",
		Arch:               "arm64",
		AgentCenterVersion: "v2.10.2",
		InstallPath:        "/usr/local/bin/agent-center",
		WorkerVersion:      "v2.10.2+abc1234",
	}
	if _, err := s.enroll.ReportCapabilities(context.Background(), ReportCapabilitiesCommand{
		WorkerID:      "W-1",
		Capabilities:  []workforce.Capability{{AgentCLI: "claude-code", Detected: true, Enabled: true}},
		SystemInfo:    info,
		ActorIdentity: "user:probe",
	}); err != nil {
		t.Fatalf("ReportCapabilities: %v", err)
	}

	w, err := s.workerRepo.FindByID(context.Background(), "W-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if w.SystemInfo() != info {
		t.Fatalf("system info not persisted: got %+v want %+v", w.SystemInfo(), info)
	}
}

// An older worker that reports no system info must not clobber whatever is
// stored (zero SystemInfo → skip the write).
func TestReportCapabilities_EmptySystemInfo_DoesNotClobber(t *testing.T) {
	s := setupReportSuite(t)
	seedReportWorker(t, s, "W-1", nil)

	info := workforce.SystemInfo{Hostname: "h1", OS: "linux", Arch: "amd64"}
	if _, err := s.enroll.ReportCapabilities(context.Background(), ReportCapabilitiesCommand{
		WorkerID: "W-1", SystemInfo: info, ActorIdentity: "user:probe",
	}); err != nil {
		t.Fatalf("first report: %v", err)
	}

	// A subsequent report with NO system info (pre-T752 worker) leaves it intact.
	if _, err := s.enroll.ReportCapabilities(context.Background(), ReportCapabilitiesCommand{
		WorkerID:      "W-1",
		Capabilities:  []workforce.Capability{{AgentCLI: "opencode", Detected: true, Enabled: true}},
		ActorIdentity: "user:probe",
	}); err != nil {
		t.Fatalf("second report: %v", err)
	}

	w, err := s.workerRepo.FindByID(context.Background(), "W-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if w.SystemInfo() != info {
		t.Fatalf("stored system info was clobbered: got %+v want %+v", w.SystemInfo(), info)
	}
}

// Re-reporting identical system info must not churn the worker version (the
// report runs on every online).
func TestReportCapabilities_UnchangedSystemInfo_NoVersionChurn(t *testing.T) {
	s := setupReportSuite(t)
	seedReportWorker(t, s, "W-1", nil)

	info := workforce.SystemInfo{Hostname: "h1", OS: "linux", WorkerVersion: "v2"}
	cmd := ReportCapabilitiesCommand{WorkerID: "W-1", SystemInfo: info, ActorIdentity: "user:probe"}
	if _, err := s.enroll.ReportCapabilities(context.Background(), cmd); err != nil {
		t.Fatalf("first: %v", err)
	}
	w1, _ := s.workerRepo.FindByID(context.Background(), "W-1")
	v1 := w1.Version()

	if _, err := s.enroll.ReportCapabilities(context.Background(), cmd); err != nil {
		t.Fatalf("second: %v", err)
	}
	w2, _ := s.workerRepo.FindByID(context.Background(), "W-1")
	if w2.Version() != v1 {
		t.Fatalf("identical re-report churned version: got %d want %d", w2.Version(), v1)
	}
}
