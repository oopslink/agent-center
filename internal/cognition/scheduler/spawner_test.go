package scheduler_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
)

// fakeProcessRunner records spawn requests + lets tests script exit
// outcomes.
type fakeProcessRunner struct {
	mu        sync.Mutex
	calls     []scheduler.ProcessSpec
	onExit    func(scheduler.ProcessSpec) (exitCode int, runErr error, stderr string)
	handles   []*fakeProcessHandle
	startErr  error
	waitGroup sync.WaitGroup
}

func (f *fakeProcessRunner) Start(_ context.Context, spec scheduler.ProcessSpec, cb func(int, error, string)) (scheduler.ProcessHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, spec)
	if f.startErr != nil {
		return nil, f.startErr
	}
	h := &fakeProcessHandle{done: make(chan struct{})}
	f.handles = append(f.handles, h)
	f.waitGroup.Add(1)
	go func() {
		defer f.waitGroup.Done()
		exitCode := 0
		var runErr error
		var stderr string
		if f.onExit != nil {
			exitCode, runErr, stderr = f.onExit(spec)
		}
		if cb != nil {
			cb(exitCode, runErr, stderr)
		}
		close(h.done)
	}()
	return h, nil
}

type fakeProcessHandle struct {
	done chan struct{}
}

func (h *fakeProcessHandle) PID() int                  { return 42 }
func (h *fakeProcessHandle) Signal(_ os.Signal) error  { return nil }
func (h *fakeProcessHandle) Kill() error               { return nil }
func (h *fakeProcessHandle) Done() <-chan struct{}     { return h.done }

func newSpawnerTestRig(t *testing.T) (*scheduler.Spawner, *fakeProcessRunner, *fakeInvocationRepoAdapter, *cognitiondb.InvocationRepo, *clock.FakeClock, *obsqlite.EventRepo) {
	t.Helper()
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(42))
	sink := observability.NewEventSink(er, er, gen, clk)
	usageDir := t.TempDir()
	r := &fakeProcessRunner{}
	sp, err := scheduler.NewSpawner(scheduler.SpawnerConfig{
		Binary:       "agent-center",
		HomeOverride: t.TempDir(),
		MemoryDir:    t.TempDir(),
		UsageDir:     usageDir,
	}, scheduler.SpawnerDeps{
		DB: db, Repo: repo, Sink: sink, Clock: clk, IDGen: gen,
	}, r)
	if err != nil {
		t.Fatal(err)
	}
	// adapter for sharing repo with test helpers
	a := &fakeInvocationRepoAdapter{real: repo}
	return sp, r, a, repo, clk, er
}

type fakeInvocationRepoAdapter struct {
	real *cognitiondb.InvocationRepo
}

func TestSpawner_New_Validation(t *testing.T) {
	if _, err := scheduler.NewSpawner(scheduler.DefaultSpawnerConfig(), scheduler.SpawnerDeps{}, nil); err == nil {
		t.Fatal("missing db")
	}
	db := openSchedulerTestDB(t)
	if _, err := scheduler.NewSpawner(scheduler.DefaultSpawnerConfig(), scheduler.SpawnerDeps{DB: db}, nil); err == nil {
		t.Fatal("missing repo")
	}
}

// TestSpawner_New_DefaultsFill exercises every default-fill branch of
// NewSpawner: zero clock / zero idgen / empty binary / empty usage-dir
// with HomeOverride / empty usage-dir with MemoryDir / nil runner.
func TestSpawner_New_DefaultsFill(t *testing.T) {
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, nil, nil)
	// zero clk + idgen + binary + runner + HomeOverride only → UsageDir
	// is derived from HomeOverride.
	sp, err := scheduler.NewSpawner(scheduler.SpawnerConfig{
		HomeOverride: t.TempDir(),
	}, scheduler.SpawnerDeps{DB: db, Repo: repo, Sink: sink}, nil)
	if err != nil {
		t.Fatalf("home override path: %v", err)
	}
	if sp == nil {
		t.Fatal("nil spawner")
	}
	// MemoryDir as the source of UsageDir when HomeOverride is empty.
	sp2, err := scheduler.NewSpawner(scheduler.SpawnerConfig{
		MemoryDir: t.TempDir(),
	}, scheduler.SpawnerDeps{DB: db, Repo: repo, Sink: sink}, nil)
	if err != nil {
		t.Fatalf("memdir path: %v", err)
	}
	if sp2 == nil {
		t.Fatal("nil spawner")
	}
}

func TestSpawner_HappyExitZero(t *testing.T) {
	sp, runner, _, repo, clk, er := newSpawnerTestRig(t)
	runner.onExit = func(spec scheduler.ProcessSpec) (int, error, string) {
		// write usage file using AGENT_CENTER_INVOCATION_ID + AGENT_CENTER_USAGE_DIR env
		invID := envFromSpec(spec, "AGENT_CENTER_INVOCATION_ID")
		usageDir := envFromSpec(spec, "AGENT_CENTER_USAGE_DIR")
		if usageDir != "" && invID != "" {
			p := filepath.Join(usageDir, invID+".usage.json")
			_ = os.WriteFile(p, []byte(`{"input":100,"output":50}`), 0o644)
		}
		return 0, nil, ""
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	id, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	runner.waitGroup.Wait() // wait for exit goroutine
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Status() != cognition.StatusSucceeded {
		t.Errorf("status = %s", got.Status())
	}
	if got.TokenUsage().Input != 100 || got.TokenUsage().Output != 50 {
		t.Errorf("usage = %+v", got.TokenUsage())
	}
	// emitted events
	until := clk.Now().Add(time.Hour)
	rows, _ := er.Find(context.Background(), observability.EventQueryFilter{Until: &until, Limit: 50})
	gotTypes := map[observability.EventType]bool{}
	for _, r := range rows {
		gotTypes[r.Type()] = true
	}
	if !gotTypes["supervisor.invocation_started"] || !gotTypes["supervisor.invocation_succeeded"] {
		t.Errorf("missing events: %+v", gotTypes)
	}
}

func TestSpawner_ExitNonZero(t *testing.T) {
	sp, runner, _, repo, _, _ := newSpawnerTestRig(t)
	runner.onExit = func(spec scheduler.ProcessSpec) (int, error, string) {
		return 1, nil, "claude blew up"
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-2")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	id, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	runner.waitGroup.Wait()
	got, _ := repo.FindByID(context.Background(), id)
	if got.Status() != cognition.StatusFailed {
		t.Errorf("status = %s", got.Status())
	}
	if got.FailedReason() != cognition.FailedReasonClaudeNonZero {
		t.Errorf("reason = %s", got.FailedReason())
	}
}

func TestSpawner_ExitOOM137(t *testing.T) {
	sp, runner, _, repo, _, _ := newSpawnerTestRig(t)
	runner.onExit = func(_ scheduler.ProcessSpec) (int, error, string) {
		return 137, nil, "OOM Killed"
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-3")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	id, _ := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes})
	runner.waitGroup.Wait()
	got, _ := repo.FindByID(context.Background(), id)
	if got.FailedReason() != cognition.FailedReasonOOM {
		t.Errorf("reason = %s", got.FailedReason())
	}
}

func TestSpawner_StartFailure(t *testing.T) {
	sp, runner, _, repo, _, _ := newSpawnerTestRig(t)
	runner.startErr = errors.New("exec failed")
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-4")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	id, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes})
	if err == nil {
		t.Fatal("expected start error")
	}
	got, _ := repo.FindByID(context.Background(), id)
	if got == nil || got.FailedReason() != cognition.FailedReasonCLICommandError {
		t.Errorf("reason = %v", got)
	}
}

func TestSpawner_ScopeRunningConflict(t *testing.T) {
	sp, runner, _, _, _, _ := newSpawnerTestRig(t)
	// keep the process alive so the second spawn collides on partial uniq
	runner.onExit = func(_ scheduler.ProcessSpec) (int, error, string) {
		time.Sleep(100 * time.Millisecond)
		return 0, nil, ""
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-5")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	if _, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes}); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes})
	if !errors.Is(err, cognition.ErrScopeKeyRunningExists) {
		t.Errorf("expected ErrScopeKeyRunningExists, got %v", err)
	}
	runner.waitGroup.Wait()
}

func TestSpawner_EnvInjection(t *testing.T) {
	sp, runner, _, _, _, _ := newSpawnerTestRig(t)
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-6")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	if _, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes}); err != nil {
		t.Fatal(err)
	}
	runner.waitGroup.Wait()
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d", len(runner.calls))
	}
	spec := runner.calls[0]
	if !strings.Contains(spec.Args[0], "supervisor") {
		t.Errorf("first arg = %q", spec.Args[0])
	}
	envMap := envToMap(spec.Env)
	for _, key := range []string{
		"AGENT_CENTER_INVOCATION_ID",
		"AGENT_CENTER_SCOPE",
		"GIT_AUTHOR_NAME",
		"GIT_AUTHOR_EMAIL",
		"GIT_CONFIG_GLOBAL",
		"HOME",
		"AGENT_CENTER_MEMORY_DIR",
		"AGENT_CENTER_USAGE_DIR",
	} {
		if _, ok := envMap[key]; !ok {
			t.Errorf("env missing %s", key)
		}
	}
	if envMap["GIT_CONFIG_GLOBAL"] != "/dev/null" {
		t.Errorf("global config not muted: %q", envMap["GIT_CONFIG_GLOBAL"])
	}
	if !strings.HasPrefix(envMap["GIT_AUTHOR_NAME"], "supervisor:") {
		t.Errorf("author name = %q", envMap["GIT_AUTHOR_NAME"])
	}
}

func TestSpawner_LiveCount(t *testing.T) {
	sp, runner, _, _, _, _ := newSpawnerTestRig(t)
	// blocked exit
	runner.onExit = func(_ scheduler.ProcessSpec) (int, error, string) {
		time.Sleep(100 * time.Millisecond)
		return 0, nil, ""
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-7")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	if _, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes}); err != nil {
		t.Fatal(err)
	}
	if sp.LiveCount() != 1 {
		t.Errorf("live = %d", sp.LiveCount())
	}
	runner.waitGroup.Wait()
	// poll until finalize callback drains the live map
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && sp.LiveCount() > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if sp.LiveCount() != 0 {
		t.Errorf("live after exit = %d", sp.LiveCount())
	}
}

func TestSpawner_SignalAndKill(t *testing.T) {
	sp, runner, _, _, clk, _ := newSpawnerTestRig(t)
	runner.onExit = func(_ scheduler.ProcessSpec) (int, error, string) {
		time.Sleep(200 * time.Millisecond)
		return 0, nil, ""
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-8")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	id, _ := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes})
	_, ok := sp.LiveProcessHandle(id)
	if !ok {
		t.Fatal("handle not live")
	}
	sp.SignalAndKill(id, 50*time.Millisecond, clk)
	runner.waitGroup.Wait()
}

// TestSpawner_SignalAndKill_GraceExpires deterministically exercises the
// SIGTERM→SIGKILL escalation path (spawner.go:443-450 timer.C branch).
// The fake handle never closes Done() during grace, so the timer fires and
// Kill() is invoked. Without this test the escalation branch is 0% covered
// because TestSpawner_SignalAndKill's handle closes Done() immediately.
func TestSpawner_SignalAndKill_GraceExpires(t *testing.T) {
	sp, runner, _, _, clk, _ := newSpawnerTestRig(t)
	// Process "lives" until killed externally — onExit blocks until we
	// release it via stopBlocking.
	stopBlocking := make(chan struct{})
	runner.onExit = func(_ scheduler.ProcessSpec) (int, error, string) {
		<-stopBlocking
		return 137, nil, ""
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-grace")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	id, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope, TriggerEvents: tes})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sp.LiveProcessHandle(id); !ok {
		t.Fatal("handle not live")
	}
	// Tiny grace forces the timer.C branch (Kill) to fire.
	sp.SignalAndKill(id, 10*time.Millisecond, clk)
	// Release the fake process so finalize can complete.
	close(stopBlocking)
	runner.waitGroup.Wait()
}

// TestSpawner_SignalAndKill_NotLive deterministically exercises the
// early-return branch (spawner.go:440-442) when SignalAndKill is called
// for an id that's no longer in s.live.
func TestSpawner_SignalAndKill_NotLive(t *testing.T) {
	sp, _, _, _, clk, _ := newSpawnerTestRig(t)
	// id never spawned → not in s.live → SignalAndKill returns immediately.
	sp.SignalAndKill(cognition.InvocationID("nonexistent"), time.Second, clk)
}

func TestSpawner_BadInput(t *testing.T) {
	sp, _, _, _, _, _ := newSpawnerTestRig(t)
	if _, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{}); err == nil {
		t.Error("expected zero-scope err")
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-x")
	if _, err := sp.Spawn(context.Background(), scheduler.InvocationRequest{Scope: scope}); err == nil {
		t.Error("expected missing trigger events err")
	}
}

// envFromSpec extracts a single env var from a ProcessSpec.Env slice.
func envFromSpec(spec scheduler.ProcessSpec, k string) string {
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, k+"=") {
			return strings.TrimPrefix(kv, k+"=")
		}
	}
	return ""
}

func envToMap(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		out[kv[:i]] = kv[i+1:]
	}
	return out
}

// Unused but referenced for completeness — keeps lint happy.
var _ = syscall.SIGTERM
