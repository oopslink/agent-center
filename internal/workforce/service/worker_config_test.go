package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
)

// cfgSuite extends suite with WorkerConfigService.
type cfgSuite struct {
	*suite
	cfg *WorkerConfigService
}

func setupCfgSuite(t *testing.T) *cfgSuite {
	t.Helper()
	s := setupSuite(t)
	cfg := NewWorkerConfigService(s.db, s.workerRepo, s.sink, s.clock)
	return &cfgSuite{suite: s, cfg: cfg}
}

// seedActiveWorker enrolls a worker via NewWorker/Save so config tests have
// a row to mutate.
func seedActiveWorker(t *testing.T, s *cfgSuite, id workforce.WorkerID) *workforce.Worker {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID: id,
		CapabilityList: []workforce.Capability{
			{AgentCLI: "claude-code", Detected: true, Enabled: true},
			{AgentCLI: "codex", Detected: true, Enabled: true},
		},
		EnrolledAt: s.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.workerRepo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWorkerConfigService_SetConfig_ConcurrencyOnly(t *testing.T) {
	s := setupCfgSuite(t)
	w := seedActiveWorker(t, s, "W-1")
	newC := workforce.WorkerConcurrency{PerAgentType: 5}
	res, err := s.cfg.SetConfig(context.Background(), SetConfigCommand{
		WorkerID:      "W-1",
		Concurrency:   &newC,
		Version:       w.Version(),
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if res.NewVersion != w.Version()+1 {
		t.Fatalf("version: %d", res.NewVersion)
	}
	if res.EventID == "" {
		t.Fatal("event id missing")
	}
	got, _ := s.workerRepo.FindByID(context.Background(), "W-1")
	if got.Concurrency().PerAgentType != 5 {
		t.Fatalf("concurrency: %+v", got.Concurrency())
	}
}

func TestWorkerConfigService_SetConfig_DiscoveryOnly(t *testing.T) {
	s := setupCfgSuite(t)
	w := seedActiveWorker(t, s, "W-1")
	newD := workforce.WorkerDiscovery{
		ScanPaths:    []string{"/home/dev"},
		Exclude:      []string{".cache"},
		ScanInterval: "30m",
	}
	if _, err := s.cfg.SetConfig(context.Background(), SetConfigCommand{
		WorkerID:      "W-1",
		Discovery:     &newD,
		Version:       w.Version(),
		ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.workerRepo.FindByID(context.Background(), "W-1")
	if got.Discovery().ScanInterval != "30m" {
		t.Fatalf("discovery: %+v", got.Discovery())
	}
}

func TestWorkerConfigService_SetConfig_BothFields(t *testing.T) {
	s := setupCfgSuite(t)
	w := seedActiveWorker(t, s, "W-1")
	newC := workforce.WorkerConcurrency{PerAgentType: 3}
	newD := workforce.WorkerDiscovery{ScanInterval: "15m"}
	_, err := s.cfg.SetConfig(context.Background(), SetConfigCommand{
		WorkerID:      "W-1",
		Concurrency:   &newC,
		Discovery:     &newD,
		Version:       w.Version(),
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkerID: "W-1"},
	})
	// Find the config.updated event and check payload.
	var configEvent *observability.Event
	for _, e := range events {
		if e.Type() == observability.EventType("workforce.worker.config.updated") {
			configEvent = e
			break
		}
	}
	if configEvent == nil {
		t.Fatal("no config.updated event")
	}
	changed, _ := configEvent.Payload()["changed_fields"].([]any)
	if len(changed) != 2 {
		t.Fatalf("expected 2 changed fields, got %d", len(changed))
	}
}

func TestWorkerConfigService_SetConfig_NoFields(t *testing.T) {
	s := setupCfgSuite(t)
	w := seedActiveWorker(t, s, "W-1")
	_, err := s.cfg.SetConfig(context.Background(), SetConfigCommand{
		WorkerID:      "W-1",
		Version:       w.Version(),
		ActorIdentity: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected error for no fields")
	}
}

func TestWorkerConfigService_SetConfig_VersionConflict(t *testing.T) {
	s := setupCfgSuite(t)
	seedActiveWorker(t, s, "W-1")
	newC := workforce.WorkerConcurrency{PerAgentType: 5}
	_, err := s.cfg.SetConfig(context.Background(), SetConfigCommand{
		WorkerID:      "W-1",
		Concurrency:   &newC,
		Version:       99,
		ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkerConfigService_SetConfig_NotFound(t *testing.T) {
	s := setupCfgSuite(t)
	newC := workforce.WorkerConcurrency{PerAgentType: 5}
	_, err := s.cfg.SetConfig(context.Background(), SetConfigCommand{
		WorkerID:      "W-NEVER",
		Concurrency:   &newC,
		Version:       1,
		ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestWorkerConfigService_SetConfig_BadActor(t *testing.T) {
	s := setupCfgSuite(t)
	w := seedActiveWorker(t, s, "W-1")
	newC := workforce.WorkerConcurrency{PerAgentType: 5}
	_, err := s.cfg.SetConfig(context.Background(), SetConfigCommand{
		WorkerID:      "W-1",
		Concurrency:   &newC,
		Version:       w.Version(),
		ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestWorkerConfigService_SetCapabilityEnabled_Toggle(t *testing.T) {
	s := setupCfgSuite(t)
	w := seedActiveWorker(t, s, "W-1")
	res, err := s.cfg.SetCapabilityEnabled(context.Background(), SetCapabilityEnabledCommand{
		WorkerID:      "W-1",
		AgentCLI:      "codex",
		Enabled:       false,
		Version:       w.Version(),
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("SetCapabilityEnabled: %v", err)
	}
	if res.EventID == "" {
		t.Fatal("event id missing")
	}
	got, _ := s.workerRepo.FindByID(context.Background(), "W-1")
	for _, c := range got.CapabilityList() {
		if c.AgentCLI == "codex" {
			if c.Enabled != false {
				t.Fatalf("expected disabled, got %+v", c)
			}
		}
	}
	// Capabilities() (only Detected ∧ Enabled) should now exclude codex.
	cliList := got.Capabilities()
	for _, cli := range cliList {
		if cli == "codex" {
			t.Fatal("codex should be filtered out of Capabilities() after disable")
		}
	}
}

func TestWorkerConfigService_SetCapabilityEnabled_NotFound(t *testing.T) {
	s := setupCfgSuite(t)
	w := seedActiveWorker(t, s, "W-1")
	_, err := s.cfg.SetCapabilityEnabled(context.Background(), SetCapabilityEnabledCommand{
		WorkerID:      "W-1",
		AgentCLI:      "gemini-cli", // not in the seeded list
		Enabled:       true,
		Version:       w.Version(),
		ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrWorkerCapabilityNotFound) {
		t.Fatalf("expected capability not found, got %v", err)
	}
}

func TestWorkerConfigService_SetCapabilityEnabled_VersionConflict(t *testing.T) {
	s := setupCfgSuite(t)
	seedActiveWorker(t, s, "W-1")
	_, err := s.cfg.SetCapabilityEnabled(context.Background(), SetCapabilityEnabledCommand{
		WorkerID:      "W-1",
		AgentCLI:      "codex",
		Enabled:       false,
		Version:       99,
		ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}
