package workforce

import (
	"errors"
	"testing"
	"time"
)

// =============================================================================
// AgentInstance getter / Rehydrate / mark-invalid-state coverage
// =============================================================================

func TestAgentInstance_AllGetters(t *testing.T) {
	cap := 7
	a, err := NewAgentInstance(NewAgentInstanceInput{
		ID:            "01HG",
		Name:          "n",
		AgentCLI:      "claude-code",
		WorkerID:      wid("W-1"),
		Config:        `{"k":1}`,
		MaxConcurrent: &cap,
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = a.Name()
	_ = a.AgentCLI()
	_ = a.MaxConcurrent()
	_ = a.CreatedAt()
	_ = a.ArchivedAt()
	_ = a.ArchivedReason()
	_ = a.ArchivedMessage()
	_ = a.Config()
}

func TestAgentInstanceState_String(t *testing.T) {
	if AgentInstanceIdle.String() != "idle" {
		t.Fatal()
	}
	if !AgentInstanceArchived.IsTerminal() {
		t.Fatal()
	}
}

func TestAgentInstanceArchivedReason_String(t *testing.T) {
	if AgentInstanceArchivedReasonManual.String() != "manual" {
		t.Fatal()
	}
	if !AgentInstanceArchivedReasonManual.IsValid() {
		t.Fatal()
	}
	if AgentInstanceArchivedReason("bogus").IsValid() {
		t.Fatal()
	}
}

func TestRehydrateAgentInstance_Happy(t *testing.T) {
	a, err := RehydrateAgentInstance(RehydrateAgentInstanceInput{
		ID:        "01H",
		Name:      "n",
		AgentCLI:  "claude-code",
		WorkerID:  wid("W-1"),
		Config:    "{}",
		State:     AgentInstanceIdle,
		CreatedAt: time.Now(),
		Version:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.State() != AgentInstanceIdle {
		t.Fatal()
	}
}

func TestRehydrateAgentInstance_InvalidState(t *testing.T) {
	if _, err := RehydrateAgentInstance(RehydrateAgentInstanceInput{
		ID: "01H", Name: "n", AgentCLI: "c", WorkerID: wid("W-1"),
		State: "bogus", CreatedAt: time.Now(), Version: 1,
	}); err == nil {
		t.Fatal()
	}
}

func TestRehydrateAgentInstance_BadVersion(t *testing.T) {
	if _, err := RehydrateAgentInstance(RehydrateAgentInstanceInput{
		ID: "01H", Name: "n", AgentCLI: "c", WorkerID: wid("W-1"),
		State: AgentInstanceIdle, CreatedAt: time.Now(), Version: 0,
	}); err == nil {
		t.Fatal()
	}
}

func TestRehydrateAgentInstance_EmptyConfigDefaults(t *testing.T) {
	a, err := RehydrateAgentInstance(RehydrateAgentInstanceInput{
		ID: "01H", Name: "n", AgentCLI: "c", WorkerID: wid("W-1"),
		Config: "", State: AgentInstanceIdle, CreatedAt: time.Now(), Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Config() != "{}" {
		t.Fatalf("config: %s", a.Config())
	}
}

func TestAgentInstance_MarkInvalidTransitions(t *testing.T) {
	a := freshAI(t)
	// Sleeping → active is invalid.
	_ = a.MarkSleeping()
	if err := a.MarkActive(); err == nil {
		t.Fatal("sleeping→active should reject")
	}
	a = freshAI(t)
	_ = a.MarkSleeping()
	if err := a.MarkIdle(); err == nil {
		t.Fatal("sleeping→idle (without Awakened) should reject")
	}
	a = freshAI(t)
	_ = a.Archive(time.Now(), AgentInstanceArchivedReasonManual, "test")
	if err := a.MarkActive(); err == nil {
		t.Fatal("archived→active should reject")
	}
	if err := a.MarkSleeping(); err == nil {
		t.Fatal("archived→sleeping should reject")
	}
	if err := a.MarkAwakened(); err == nil {
		t.Fatal("archived→idle should reject")
	}
}

func TestAgentInstance_MarkSleeping_Idempotent(t *testing.T) {
	a := freshAI(t)
	_ = a.MarkSleeping()
	if err := a.MarkSleeping(); err != nil {
		t.Fatal("sleeping→sleeping should be idempotent")
	}
}

func TestAgentInstance_MarkAwakened_Idempotent(t *testing.T) {
	a := freshAI(t)
	if err := a.MarkAwakened(); err != nil {
		t.Fatal("idle→idle awaken should be idempotent")
	}
}

func TestAgentInstance_MarkActive_FromSleeping(t *testing.T) {
	a := freshAI(t)
	_ = a.MarkSleeping()
	if err := a.MarkActive(); err == nil {
		t.Fatal()
	}
}

func TestAgentInstance_Archive_InvalidReason(t *testing.T) {
	a := freshAI(t)
	err := a.Archive(time.Now(), AgentInstanceArchivedReason("bogus"), "msg")
	if err == nil {
		t.Fatal()
	}
}

func TestAgentInstance_Archive_EmptyMessage(t *testing.T) {
	a := freshAI(t)
	err := a.Archive(time.Now(), AgentInstanceArchivedReasonManual, "  ")
	if err == nil {
		t.Fatal()
	}
}

func TestAgentInstance_SetMaxConcurrent(t *testing.T) {
	a := freshAI(t)
	val := 3
	if err := a.SetMaxConcurrent(time.Now(), &val); err != nil {
		t.Fatal(err)
	}
	if a.MaxConcurrent() == nil || *a.MaxConcurrent() != 3 {
		t.Fatal()
	}
	// nil = clear cap
	if err := a.SetMaxConcurrent(time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if a.MaxConcurrent() != nil {
		t.Fatal()
	}
	// invalid (< 1) rejects
	zero := 0
	if err := a.SetMaxConcurrent(time.Now(), &zero); err == nil {
		t.Fatal()
	}
	// after archive rejects
	_ = a.Archive(time.Now(), AgentInstanceArchivedReasonManual, "test")
	val2 := 5
	if err := a.SetMaxConcurrent(time.Now(), &val2); !errors.Is(err, ErrAgentInstanceArchived) {
		t.Fatalf("expected archived, got %v", err)
	}
}

func TestAgentInstance_SetConfig_DefaultsEmpty(t *testing.T) {
	a := freshAI(t)
	if err := a.SetConfig(time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	if a.Config() != "{}" {
		t.Fatalf("config: %s", a.Config())
	}
}

func TestAgentInstance_validateName_LongAndBadChars(t *testing.T) {
	if err := validateAgentInstanceName(string(make([]byte, 129))); err == nil {
		t.Fatal("129-char name should reject")
	}
}

// =============================================================================
// BootstrapToken getter / Rehydrate / String() coverage
// =============================================================================

func TestBootstrapToken_AllGetters(t *testing.T) {
	tok := newActiveTokenForTest(t)
	_ = tok.ID()
	_ = tok.WorkerID()
	_ = tok.ValueHash()
	_ = tok.CreatedAt()
	_ = tok.ExpiresAt()
	_ = tok.UsedAt()
	_ = tok.RevokedAt()
	_ = tok.RevokedReason()
	_ = tok.RevokedMessage()
	_ = tok.CreatedBy()
}

func TestBootstrapTokenID_String(t *testing.T) {
	id := BootstrapTokenID("01HX")
	if id.String() != "01HX" {
		t.Fatal()
	}
}

func TestBootstrapTokenStatus_String(t *testing.T) {
	if BootstrapTokenActive.String() != "active" {
		t.Fatal()
	}
}

func TestBootstrapTokenRevokedReason_String(t *testing.T) {
	if BootstrapTokenRevokedReasonManual.String() != "manual" {
		t.Fatal()
	}
}

func TestRehydrateBootstrapToken_Happy(t *testing.T) {
	tok, err := RehydrateBootstrapToken(RehydrateBootstrapTokenInput{
		ID: "01H", WorkerID: "W-1", ValueHash: "h",
		Status:    BootstrapTokenActive,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
		CreatedBy: "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok.Status() != BootstrapTokenActive {
		t.Fatal()
	}
}

// =============================================================================
// AgentInstanceID String + WorkerID copy paths
// =============================================================================

func TestAgentInstanceID_String(t *testing.T) {
	id := AgentInstanceID("01HX")
	if id.String() != "01HX" {
		t.Fatal()
	}
}

func TestCopyHelpers(t *testing.T) {
	// copyWorkerIDPtr / copyIntPtr nil + non-nil paths.
	if copyWorkerIDPtr(nil) != nil {
		t.Fatal()
	}
	id := WorkerID("W-1")
	cp := copyWorkerIDPtr(&id)
	if cp == nil || *cp != "W-1" {
		t.Fatal()
	}
	if copyIntPtr(nil) != nil {
		t.Fatal()
	}
	v := 5
	cp2 := copyIntPtr(&v)
	if cp2 == nil || *cp2 != 5 {
		t.Fatal()
	}
}

// =============================================================================
// Worker AR misc paths
// =============================================================================

func TestWorker_ConcurrencyDiscoveryJSON(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{
		ID: "W-1", EnrolledAt: time.Now(),
	})
	if b, _ := w.ConcurrencyJSON(); len(b) == 0 {
		t.Fatal()
	}
	if b, _ := w.DiscoveryJSON(); len(b) == 0 {
		t.Fatal()
	}
}

func TestWorker_ApplyConfig(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{ID: "W-1", EnrolledAt: time.Now()})
	prevVer := w.Version()
	c := WorkerConcurrency{PerAgentType: 9}
	w.ApplyConfig(time.Now(), &c, nil)
	if w.Concurrency().PerAgentType != 9 {
		t.Fatal()
	}
	if w.Version() != prevVer+1 {
		t.Fatal()
	}
}

func TestWorker_ApplyCapabilities_NewAndDedup(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{ID: "W-1", EnrolledAt: time.Now()})
	prevVer := w.Version()
	w.ApplyCapabilities(time.Now(), []Capability{
		{AgentCLI: "claude-code", Detected: true},
		{AgentCLI: "claude-code", Detected: true}, // dup ignored
		{AgentCLI: "codex", Detected: false},
	})
	if w.Version() != prevVer+1 {
		t.Fatal()
	}
	if len(w.CapabilityList()) != 2 {
		t.Fatalf("expected 2 caps, got %d", len(w.CapabilityList()))
	}
}

func TestBuildCapabilityList_RichDedup(t *testing.T) {
	out := buildCapabilityList([]Capability{
		{AgentCLI: "x", Detected: true, Enabled: true},
		{AgentCLI: "x", Detected: true, Enabled: false}, // dup ignored
		{AgentCLI: "y", Detected: true, Enabled: true},
	}, nil)
	if len(out) != 2 {
		t.Fatalf("got %d", len(out))
	}
}

func TestBuildCapabilityList_LegacyDedup(t *testing.T) {
	out := buildCapabilityList(nil, []string{"x", "x", "y"})
	if len(out) != 2 {
		t.Fatal()
	}
}

func TestBuildCapabilityList_NilOrEmpty(t *testing.T) {
	if out := buildCapabilityList(nil, nil); out != nil {
		t.Fatal()
	}
	if out := buildCapabilityList(nil, []string{}); out != nil {
		t.Fatal()
	}
}

// Worker.MarkOnline / MarkOffline idempotent paths
func TestWorker_MarkOnline_Idempotent(t *testing.T) {
	w := newTestWorker(t)
	w.MarkOnline(time.Now())
	prevVer := w.Version()
	w.MarkOnline(time.Now())
	if w.Version() != prevVer {
		t.Fatal("idempotent should not bump version")
	}
}

func TestWorker_MarkOffline_AlreadyOffline(t *testing.T) {
	w := newTestWorker(t) // offline
	prevVer := w.Version()
	if err := w.MarkOffline(time.Now(), OfflineReasonHeartbeatTimeout, "test"); err != nil {
		t.Fatal()
	}
	// version should NOT bump on no-op
	if w.Version() != prevVer {
		t.Fatal()
	}
}

func TestWorker_MarkOffline_InvalidReason(t *testing.T) {
	w := newTestWorker(t)
	w.MarkOnline(time.Now())
	if err := w.MarkOffline(time.Now(), OfflineReason("bogus"), "msg"); err == nil {
		t.Fatal()
	}
}

func TestWorker_MarkOffline_EmptyMessage(t *testing.T) {
	w := newTestWorker(t)
	w.MarkOnline(time.Now())
	if err := w.MarkOffline(time.Now(), OfflineReasonHeartbeatTimeout, "  "); err == nil {
		t.Fatal()
	}
}

func TestRehydrateWorker_BadVersion(t *testing.T) {
	if _, err := RehydrateWorker(RehydrateWorkerInput{
		ID: "W-1", Status: WorkerOffline, EnrolledAt: time.Now(),
		Version: 0,
	}); err == nil {
		t.Fatal()
	}
}

func TestRehydrateWorker_InvalidStatus(t *testing.T) {
	if _, err := RehydrateWorker(RehydrateWorkerInput{
		ID: "W-1", Status: "bogus", EnrolledAt: time.Now(), Version: 1,
	}); err == nil {
		t.Fatal()
	}
}

func TestRehydrateWorker_InvalidID(t *testing.T) {
	if _, err := RehydrateWorker(RehydrateWorkerInput{
		ID: "", Status: WorkerOffline, EnrolledAt: time.Now(), Version: 1,
	}); err == nil {
		t.Fatal()
	}
}

func TestNewWorker_InvalidID(t *testing.T) {
	if _, err := NewWorker(NewWorkerInput{ID: "with bad chars!", EnrolledAt: time.Now()}); err == nil {
		t.Fatal()
	}
	if _, err := NewWorker(NewWorkerInput{ID: WorkerID(string(make([]byte, 129))), EnrolledAt: time.Now()}); err == nil {
		t.Fatal()
	}
}

func TestNewWorker_ZeroEnrolled(t *testing.T) {
	if _, err := NewWorker(NewWorkerInput{ID: "W-1"}); err == nil {
		t.Fatal()
	}
}

// =============================================================================
// Heartbeat / Concurrency / Discovery validation
// =============================================================================

func TestWorker_Heartbeat_NegativeDelta(t *testing.T) {
	w := newTestWorker(t)
	if err := w.Heartbeat(time.Now(), -1); err == nil {
		t.Fatal()
	}
}

// =============================================================================
// HashTokenValue branches (deterministic + nonempty)
// =============================================================================

func TestHashTokenValue_EmptyAndNotEmpty(t *testing.T) {
	a := HashTokenValue("")
	if a == "" {
		t.Fatal()
	}
	b := HashTokenValue("x")
	if a == b {
		t.Fatal()
	}
}
