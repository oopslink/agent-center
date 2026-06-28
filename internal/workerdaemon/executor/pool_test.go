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
}
