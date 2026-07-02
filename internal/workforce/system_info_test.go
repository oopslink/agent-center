package workforce

import (
	"testing"
	"time"
)

func TestSystemInfo_IsZeroAndJSONRoundTrip(t *testing.T) {
	var zero SystemInfo
	if !zero.IsZero() {
		t.Fatal("zero SystemInfo should report IsZero")
	}
	// zero marshals to "{}" (never null) so the column always holds valid JSON.
	b, err := systemInfoJSON(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if string(b) != "{}" {
		t.Fatalf("zero SystemInfo JSON = %q, want {}", string(b))
	}

	full := SystemInfo{
		Hostname:           "dev001.local",
		OS:                 "darwin",
		Arch:               "arm64",
		AgentCenterVersion: "v2.10.2",
		InstallPath:        "/usr/local/bin/agent-center",
		WorkerVersion:      "v2.10.2+abc1234",
	}
	if full.IsZero() {
		t.Fatal("full SystemInfo should not report IsZero")
	}
	b, err = systemInfoJSON(full)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}
	got, err := ParseSystemInfo(string(b))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != full {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, full)
	}

	// empty string parses to the zero value (older rows predate the column).
	got, err = ParseSystemInfo("")
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("parse empty should yield zero, got %+v", got)
	}
}

func TestWorker_ApplySystemInfo(t *testing.T) {
	w := newTestWorker(t)
	if !w.SystemInfo().IsZero() {
		t.Fatal("fresh worker should have zero SystemInfo")
	}
	v0 := w.Version()
	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	info := SystemInfo{Hostname: "h1", OS: "linux", Arch: "amd64", WorkerVersion: "v2.10.2"}
	w.ApplySystemInfo(at, info)
	if w.SystemInfo() != info {
		t.Fatalf("SystemInfo not applied: %+v", w.SystemInfo())
	}
	if w.Version() != v0+1 {
		t.Fatalf("version should bump on change: got %d want %d", w.Version(), v0+1)
	}

	// Idempotent: re-applying identical info is a no-op (no version churn) so an
	// idle re-report on every online does not thrash the version.
	w.ApplySystemInfo(at.Add(time.Minute), info)
	if w.Version() != v0+1 {
		t.Fatalf("re-applying identical info must not bump version: got %d", w.Version())
	}

	// A changed field bumps again.
	info2 := info
	info2.Hostname = "h2"
	w.ApplySystemInfo(at.Add(2*time.Minute), info2)
	if w.Version() != v0+2 {
		t.Fatalf("changed info should bump: got %d want %d", w.Version(), v0+2)
	}
}

func TestWorker_SystemInfoRehydrateRoundTrip(t *testing.T) {
	info := SystemInfo{Hostname: "h", OS: "darwin", Arch: "arm64", AgentCenterVersion: "v1", InstallPath: "/p", WorkerVersion: "v1+sha"}
	w, err := RehydrateWorker(RehydrateWorkerInput{
		ID:         "W-9",
		Status:     WorkerOffline,
		SystemInfo: info,
		EnrolledAt: time.Now(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Version:    3,
	})
	if err != nil {
		t.Fatalf("RehydrateWorker: %v", err)
	}
	if w.SystemInfo() != info {
		t.Fatalf("rehydrated SystemInfo mismatch: got %+v want %+v", w.SystemInfo(), info)
	}
	b, err := w.SystemInfoJSON()
	if err != nil {
		t.Fatalf("SystemInfoJSON: %v", err)
	}
	back, err := ParseSystemInfo(string(b))
	if err != nil || back != info {
		t.Fatalf("JSON round-trip: got %+v err %v", back, err)
	}
}
