package orchestrator

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/modelrouter"
)

func TestNewEngine_RequiredFieldBranches(t *testing.T) {
	// A fully-valid baseline so each case omits exactly one required field.
	eng, _, _ := newTestEngine(t, 1, modelrouter.Config{DefaultExecutorModel: "m"}, &fakeRunner{})
	base := EngineConfig{
		Pool: eng.pool, Routing: eng.routing, Router: eng.router,
		Runners: map[string]RunnerCmdBuilder{"claude-code": &fakeRunner{}}, IDs: &fakeIDMinter{},
	}
	cases := map[string]func(c *EngineConfig){
		"no pool":    func(c *EngineConfig) { c.Pool = nil },
		"no routing": func(c *EngineConfig) { c.Routing = nil },
		"no router":  func(c *EngineConfig) { c.Router = nil },
		"no runners": func(c *EngineConfig) { c.Runners = nil },
		"nil runner value": func(c *EngineConfig) {
			c.Runners = map[string]RunnerCmdBuilder{"claude-code": nil}
		},
		"no ids": func(c *EngineConfig) { c.IDs = nil },
	}
	for name, mut := range cases {
		cfg := base
		mut(&cfg)
		if _, err := NewEngine(cfg); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	// The valid baseline builds (and Pool() returns the pool).
	ok, err := NewEngine(base)
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if ok.Pool() != eng.pool {
		t.Error("Pool() should return the configured pool")
	}
}

// errRunner always fails Build — exercises HandleWork's runner-error path.
type errRunner struct{}

func (errRunner) Build(string, string) ([]string, error) { return nil, errors.New("runner boom") }

func TestHandleWork_RunnerBuildError(t *testing.T) {
	eng, _, _ := newTestEngine(t, 2, modelrouter.Config{DefaultExecutorModel: "m"}, errRunner{})
	_, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g"}, ChatID: "c"})
	if err == nil || !strings.Contains(err.Error(), "build runner") {
		t.Errorf("expected build-runner error, got %v", err)
	}
}

// fixedMinter returns a constant problem id so the SECOND new-problem registration
// hits a duplicate — exercising HandleWork's register-error path.
type fixedMinter struct{ n int }

func (m *fixedMinter) NewExecutorID() string { m.n++; return "exec-fixed-" + string(rune('a'+m.n)) }
func (m *fixedMinter) NewProblemID() string  { return "problem-CONST" }

func TestHandleWork_RegisterError(t *testing.T) {
	eng, _, _ := newTestEngine(t, 3, modelrouter.Config{DefaultExecutorModel: "m"}, &fakeRunner{})
	eng.ids = &fixedMinter{}
	// First new problem registers problem-CONST (distinct chats so each is "new").
	a, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g1"}, ChatID: "c1"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer reap(t, a.Handle)
	// Second "new" problem mints the SAME id → Register duplicate → error surfaces.
	_, err = eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g2"}, ChatID: "c2"})
	if err == nil || !strings.Contains(err.Error(), "register problem") {
		t.Errorf("expected register-problem error, got %v", err)
	}
}

func TestHandleWork_RouteError_CorruptRoutingJSON(t *testing.T) {
	eng, _, root := newTestEngine(t, 2, modelrouter.Config{DefaultExecutorModel: "m"}, &fakeRunner{})
	// Corrupt routing.json so RoutingStore.Route → Load fails (not NotExist).
	if err := os.WriteFile(root+"/routing.json", []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write corrupt routing: %v", err)
	}
	_, err := eng.HandleWork(context.Background(), WorkItem{Goal: executor.Goal{Title: "g"}, ChatID: "c"})
	if err == nil || !strings.Contains(err.Error(), "route") {
		t.Errorf("expected route error on corrupt routing.json, got %v", err)
	}
}

func TestBuildPrompt_AllBranches(t *testing.T) {
	full := buildPrompt(WorkItem{
		Goal:    executor.Goal{Title: "T", Description: "D", IssueSpec: "S"},
		Context: "C",
	})
	for _, want := range []string{"T", "D", "## Spec\nS", "## Context\nC"} {
		if !strings.Contains(full, want) {
			t.Errorf("prompt %q missing %q", full, want)
		}
	}
	// Title-only: no Spec/Context headers.
	bare := buildPrompt(WorkItem{Goal: executor.Goal{Title: "only"}})
	if bare != "only" {
		t.Errorf("title-only prompt = %q, want 'only'", bare)
	}
}
