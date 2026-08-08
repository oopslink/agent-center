package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// fakeGitRunner records git invocations and optionally returns a canned error.
type fakeGitRunner struct {
	mu   sync.Mutex
	err  error
	args [][]string
}

func (f *fakeGitRunner) Run(_ context.Context, _ string, _ []string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.args = append(f.args, args)
	return "", f.err
}

// newTestPool builds a Pool over a temp agent root with fake git + a fake,
// process-less spawner. The fake spawner assigns a unique pid per launch.
func newTestPool(t *testing.T, max int, gitErr error) (*Pool, *fakeGitRunner) {
	t.Helper()
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	git := &fakeGitRunner{err: gitErr}
	wt, err := NewWorktreeProvisioner(root, git)
	if err != nil {
		t.Fatalf("NewWorktreeProvisioner: %v", err)
	}
	var pidSeq int
	var pmu sync.Mutex
	sp := &Spawner{
		start: func(cmd *exec.Cmd) error {
			pmu.Lock()
			pidSeq++
			cmd.Process = &os.Process{Pid: 4000 + pidSeq}
			pmu.Unlock()
			return nil
		},
		signal: func(int, syscall.Signal) error { return nil },
	}
	pool, err := NewPool(PoolConfig{
		Exchange:   fx,
		Worktrees:  wt,
		Spawner:    sp,
		AgentRoot:  root,
		BaseRef:    "main",
		BinaryPath: "/bin/agent-center",
		Max:        max,
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	return pool, git
}

func validPoolInput(id string) Input {
	return Input{
		ExecutorID: id,
		Goal:       Goal{Title: "do " + id},
		Model:      "claude-haiku",
		CreatedAt:  time.Unix(1700000000, 0),
	}
}

func launch(p *Pool, id string) (*Handle, error) {
	return p.Launch(context.Background(), LaunchSpec{Input: validPoolInput(id), RunnerCmd: []string{"claude", "-p", "x"}})
}

func TestPool_AdmitsUpToMaxThenAtCapacity(t *testing.T) {
	pool, _ := newTestPool(t, 3, nil)
	for i := 0; i < 3; i++ {
		if _, err := launch(pool, fmt.Sprintf("exec-%d", i)); err != nil {
			t.Fatalf("launch %d: %v", i, err)
		}
	}
	if pool.Active() != 3 {
		t.Fatalf("Active = %d, want 3", pool.Active())
	}
	if _, err := launch(pool, "exec-overflow"); !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("4th launch err = %v, want ErrAtCapacity", err)
	}
	if pool.Available() != 0 {
		t.Errorf("Available = %d, want 0", pool.Available())
	}
}

func TestPool_DefaultMax(t *testing.T) {
	pool, _ := newTestPool(t, 0, nil) // 0 → DefaultMaxConcurrent
	if pool.Max() != DefaultMaxConcurrent {
		t.Errorf("Max = %d, want %d", pool.Max(), DefaultMaxConcurrent)
	}
}

func TestPool_DuplicateIDRejected(t *testing.T) {
	pool, _ := newTestPool(t, 3, nil)
	if _, err := launch(pool, "dup"); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	if _, err := launch(pool, "dup"); !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("duplicate launch err = %v, want ErrAlreadyActive", err)
	}
}

func TestPool_ReleaseFreesSlot(t *testing.T) {
	pool, _ := newTestPool(t, 1, nil)
	if _, err := launch(pool, "a"); err != nil {
		t.Fatalf("launch a: %v", err)
	}
	if _, err := launch(pool, "b"); !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("launch b at cap err = %v, want ErrAtCapacity", err)
	}
	if !pool.Release("a") {
		t.Fatal("Release(a) should report a held slot")
	}
	if pool.Release("a") {
		t.Error("second Release(a) should be a no-op false")
	}
	if _, err := launch(pool, "b"); err != nil {
		t.Fatalf("launch b after release: %v", err)
	}
}

func TestPool_LaunchAssignsLowestFreeSlot(t *testing.T) {
	pool, _ := newTestPool(t, 3, nil)
	if _, err := launch(pool, "a"); err != nil {
		t.Fatalf("launch a: %v", err)
	}
	if _, err := launch(pool, "b"); err != nil {
		t.Fatalf("launch b: %v", err)
	}
	if got := mustSlot(t, pool, "a"); got != 0 {
		t.Fatalf("slot a = %d, want 0", got)
	}
	if got := mustSlot(t, pool, "b"); got != 1 {
		t.Fatalf("slot b = %d, want 1", got)
	}
	pool.Release("a")
	if _, err := launch(pool, "c"); err != nil {
		t.Fatalf("launch c: %v", err)
	}
	if got := mustSlot(t, pool, "c"); got != 0 {
		t.Fatalf("slot c = %d, want reused lowest free slot 0", got)
	}
}

func TestPool_ProvisionFailureFreesSlot(t *testing.T) {
	pool, _ := newTestPool(t, 2, errors.New("git boom"))
	if _, err := launch(pool, "x"); err == nil {
		t.Fatal("expected worktree failure to surface")
	}
	if pool.Active() != 0 {
		t.Errorf("failed launch must free its slot, Active = %d", pool.Active())
	}
}

func TestPool_ConcurrentLaunchesRespectCap(t *testing.T) {
	const max = 3
	pool, _ := newTestPool(t, max, nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok, capped int
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := launch(pool, fmt.Sprintf("c-%d", i))
			mu.Lock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrAtCapacity):
				capped++
			default:
				t.Errorf("unexpected launch err: %v", err)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if ok != max {
		t.Errorf("admitted %d, want exactly %d", ok, max)
	}
	if capped != 10-max {
		t.Errorf("capped %d, want %d", capped, 10-max)
	}
	if pool.Active() != max {
		t.Errorf("Active = %d, want %d", pool.Active(), max)
	}
	if len(pool.Handles()) != max {
		t.Errorf("Handles = %d, want %d", len(pool.Handles()), max)
	}
	assignments := pool.Assignments()
	if len(assignments) != max {
		t.Fatalf("Assignments = %d, want %d", len(assignments), max)
	}
	seen := map[int]bool{}
	for _, asg := range assignments {
		if asg.SlotIndex < 0 || asg.SlotIndex >= max {
			t.Fatalf("slot %d out of range [0,%d)", asg.SlotIndex, max)
		}
		if seen[asg.SlotIndex] {
			t.Fatalf("duplicate slot %d in assignments %+v", asg.SlotIndex, assignments)
		}
		seen[asg.SlotIndex] = true
	}
}

func TestPool_LaunchValidatesInput(t *testing.T) {
	pool, _ := newTestPool(t, 2, nil)
	_, err := pool.Launch(context.Background(), LaunchSpec{Input: Input{ExecutorID: "bad/id"}})
	if err == nil {
		t.Error("invalid Input must be rejected before reserving a slot")
	}
	if pool.Active() != 0 {
		t.Errorf("rejected launch must not occupy a slot, Active = %d", pool.Active())
	}
}

func TestPool_ResizeExpandAppendsAdmissibleSlots(t *testing.T) {
	pool, _ := newTestPool(t, 2, nil)
	if _, err := launch(pool, "a"); err != nil {
		t.Fatalf("launch a: %v", err)
	}
	if _, err := launch(pool, "b"); err != nil {
		t.Fatalf("launch b: %v", err)
	}
	if _, err := launch(pool, "before-expand"); !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("pre-expand launch err = %v, want ErrAtCapacity", err)
	}

	pool.Resize(4)
	if pool.Max() != 4 || pool.AdmissionMax() != 4 || pool.SlotCount() != 4 {
		t.Fatalf("after expand max/admission/slots = %d/%d/%d, want 4/4/4", pool.Max(), pool.AdmissionMax(), pool.SlotCount())
	}
	if _, err := launch(pool, "c"); err != nil {
		t.Fatalf("launch c after expand: %v", err)
	}
	if got := mustSlot(t, pool, "c"); got != 2 {
		t.Fatalf("slot c = %d, want appended slot 2", got)
	}
}

func TestPool_ResizeShrinkDrainsWithoutMovingRuns(t *testing.T) {
	pool, _ := newTestPool(t, 3, nil)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := launch(pool, id); err != nil {
			t.Fatalf("launch %s: %v", id, err)
		}
	}
	if got := mustSlot(t, pool, "c"); got != 2 {
		t.Fatalf("precondition c slot = %d, want 2", got)
	}

	pool.Resize(1)
	if pool.Max() != 1 || pool.AdmissionMax() != 1 || pool.SlotCount() != 3 {
		t.Fatalf("after shrink max/admission/slots = %d/%d/%d, want 1/1/3", pool.Max(), pool.AdmissionMax(), pool.SlotCount())
	}
	if got := mustSlot(t, pool, "c"); got != 2 {
		t.Fatalf("shrink moved c to slot %d, want it left in high draining slot 2", got)
	}
	if _, err := launch(pool, "d"); !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("launch while low slot occupied err = %v, want ErrAtCapacity", err)
	}

	if !pool.Release("b") {
		t.Fatal("Release(b) should free slot 1")
	}
	if pool.SlotCount() != 3 {
		t.Fatalf("slot_count after releasing middle high slot = %d, want 3 while slot 2 drains", pool.SlotCount())
	}
	if !pool.Release("a") {
		t.Fatal("Release(a) should free slot 0")
	}
	if _, err := launch(pool, "d"); err != nil {
		t.Fatalf("launch d after low slot freed: %v", err)
	}
	if got := mustSlot(t, pool, "d"); got != 0 {
		t.Fatalf("slot d = %d, want only admissible low slot 0", got)
	}
	if got := mustSlot(t, pool, "c"); got != 2 {
		t.Fatalf("high draining run c moved to slot %d, want 2", got)
	}
	if !pool.Release("c") {
		t.Fatal("Release(c) should free high slot")
	}
	if pool.SlotCount() != 1 {
		t.Fatalf("slot_count after high slot drained = %d, want converged cap 1", pool.SlotCount())
	}
}

func TestNewPool_Validation(t *testing.T) {
	root := t.TempDir()
	layout, _ := NewLayout(root)
	fx, _ := NewFileExchange(layout, nil)
	wt, _ := NewWorktreeProvisioner(root, &fakeGitRunner{})
	cases := []PoolConfig{
		{Worktrees: wt, AgentRoot: root, BaseRef: "main"}, // no exchange
		{Exchange: fx, AgentRoot: root, BaseRef: "main"},  // no worktrees
		{Exchange: fx, Worktrees: wt, BaseRef: "main"},    // no agent root
		{Exchange: fx, Worktrees: wt, AgentRoot: root},    // no base ref
	}
	for i, c := range cases {
		if _, err := NewPool(c); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
	// Plain-dir mode: neither Worktrees nor BaseRef → valid (W1, PD ruling).
	if _, err := NewPool(PoolConfig{Exchange: fx, AgentRoot: root}); err != nil {
		t.Errorf("plain-dir pool (no worktrees/base) should be valid, got %v", err)
	}
}

// TestPool_PlainDirWorkspace verifies the W1 worktree-optional path: with no
// WorktreeProvisioner/BaseRef the Pool provisions the executor workspace as a plain
// directory (no git), and never invokes git.
func TestPool_PlainDirWorkspace(t *testing.T) {
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	sp := &Spawner{
		start:  func(cmd *exec.Cmd) error { cmd.Process = &os.Process{Pid: 5000}; return nil },
		signal: func(int, syscall.Signal) error { return nil },
	}
	pool, err := NewPool(PoolConfig{Exchange: fx, Spawner: sp, AgentRoot: root, Max: 2})
	if err != nil {
		t.Fatalf("NewPool plain-dir: %v", err)
	}
	if _, err := pool.Launch(context.Background(), LaunchSpec{Input: validPoolInput("plain-1"), RunnerCmd: []string{"true"}}); err != nil {
		t.Fatalf("Launch plain-dir: %v", err)
	}
	wsDir, _ := layout.WorkspaceDir("plain-1")
	if fi, err := os.Stat(wsDir); err != nil || !fi.IsDir() {
		t.Errorf("expected plain workspace dir at %s, err=%v", wsDir, err)
	}
	// No .git anywhere under the workspace (it is NOT a worktree).
	if _, err := os.Stat(wsDir + "/.git"); err == nil {
		t.Error("plain-dir workspace must not be a git worktree")
	}
}

func mustSlot(t *testing.T, p *Pool, id string) int {
	t.Helper()
	slot, ok := p.SlotIndex(id)
	if !ok {
		t.Fatalf("missing slot for %s; assignments=%+v", id, p.Assignments())
	}
	return slot
}
