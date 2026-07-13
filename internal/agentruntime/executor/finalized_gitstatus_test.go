package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptedGitRunner returns canned output per git subcommand (keyed by space-joined
// args) so the finalize-time git-status probe (issue-0186f85e ②) is deterministic. A key
// present in `err` returns that error; otherwise `out` (default "") is returned.
type scriptedGitRunner struct {
	out map[string]string
	err map[string]error
}

func (s scriptedGitRunner) Run(_ context.Context, _ string, _ []string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	if e, ok := s.err[key]; ok {
		return "", e
	}
	return s.out[key], nil
}

// findFinalized returns the FinalizedRef for id (or fails).
func findFinalized(t *testing.T, fx *FileExchange, id string) FinalizedRef {
	t.Helper()
	refs, err := fx.ListFinalized()
	if err != nil {
		t.Fatalf("ListFinalized: %v", err)
	}
	for _, r := range refs {
		if r.ExecutorID == id {
			return r
		}
	}
	t.Fatalf("no finalized marker for %s (refs=%+v)", id, refs)
	return FinalizedRef{}
}

// TestMonitor_Finalize_CapturesCleanPushedGitStatus — issue-0186f85e ②: a terminal
// executor's `finalized` marker records the structured git status (branch / HEAD sha /
// dirty / pushed) so a later audit can judge delivery WITHOUT the worktree. Clean +
// pushed variant: the work is durably delivered.
func TestMonitor_Finalize_CapturesCleanPushedGitStatus(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-clean"
	mustProvision(t, f.fx, id)
	f.mon.git = scriptedGitRunner{out: map[string]string{
		"rev-parse HEAD":              "abc1234def\n",
		"rev-parse --abbrev-ref HEAD": "feat/work\n",
		"status --porcelain":          "", // clean
		"branch -r --contains HEAD":   "  origin/feat/work\n",
	}}
	if err := f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	ref := findFinalized(t, f.fx, id)
	if ref.Git == nil {
		t.Fatal("finalized marker must carry structured git status")
	}
	g := ref.Git
	if !g.Probed {
		t.Error("Probed must be true for a git workspace")
	}
	if g.Branch != "feat/work" {
		t.Errorf("Branch = %q, want feat/work", g.Branch)
	}
	if g.HeadSHA != "abc1234def" {
		t.Errorf("HeadSHA = %q, want abc1234def", g.HeadSHA)
	}
	if g.Dirty {
		t.Error("Dirty must be false for a clean worktree")
	}
	if !g.Pushed {
		t.Error("Pushed must be true when HEAD is on a remote-tracking branch")
	}
}

// TestMonitor_Finalize_CapturesDirtyUnpushedGitStatus — the "真做了但没推" delivery-risk
// case: uncommitted changes AND HEAD not on any remote. The marker records dirty=true /
// pushed=false so the audit can flag work that would be LOST if reaped without an eager
// push (the exact T947 gap).
func TestMonitor_Finalize_CapturesDirtyUnpushedGitStatus(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-dirty"
	mustProvision(t, f.fx, id)
	f.mon.git = scriptedGitRunner{out: map[string]string{
		"rev-parse HEAD":              "deadbeef01\n",
		"rev-parse --abbrev-ref HEAD": "feat/wip\n",
		"status --porcelain":          " M internal/foo.go\n?? new.go\n",
		"branch -r --contains HEAD":   "", // not on any remote → unpushed
	}}
	if err := f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeFailed, Error: &ErrorDetail{Kind: "x", Message: "y"}}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	g := findFinalized(t, f.fx, id).Git
	if g == nil || !g.Probed {
		t.Fatal("marker must carry a probed git status")
	}
	if !g.Dirty {
		t.Error("Dirty must be true when the worktree has uncommitted changes")
	}
	if g.Pushed {
		t.Error("Pushed must be false when HEAD is on no remote-tracking branch")
	}
}

// TestMonitor_Finalize_NonGitWorkspace_TimestampOnlyMarker — a plain-dir (non-git)
// executor: the probe fails, so the marker is timestamp-only (Git=nil) and the executor
// is STILL retained + reapable (delayed teardown unaffected — the probe never fails
// finalize).
func TestMonitor_Finalize_NonGitWorkspace_TimestampOnlyMarker(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-plain"
	mustProvision(t, f.fx, id)
	f.mon.git = scriptedGitRunner{err: map[string]error{
		"rev-parse HEAD": errors.New("fatal: not a git repository"),
	}}
	if err := f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	ref := findFinalized(t, f.fx, id)
	if ref.Git != nil {
		t.Errorf("non-git workspace must yield a timestamp-only marker, got Git=%+v", ref.Git)
	}
	// Delayed teardown still holds: retained now, reaped by the sweep.
	assertDelayedTeardown(t, f.mon, f.fx, id)
}

// TestProbeGitStatus_DetachedHead — a detached HEAD (abbrev-ref returns "HEAD") records an
// empty Branch but still a HeadSHA + Probed=true.
func TestProbeGitStatus_DetachedHead(t *testing.T) {
	r := scriptedGitRunner{out: map[string]string{
		"rev-parse HEAD":              "cafe0011\n",
		"rev-parse --abbrev-ref HEAD": "HEAD\n", // detached
		"status --porcelain":          "",
		"branch -r --contains HEAD":   "",
	}}
	gs := probeGitStatus(context.Background(), r, "/some/worktree")
	if !gs.Probed || gs.HeadSHA != "cafe0011" {
		t.Fatalf("gs = %+v, want probed with sha cafe0011", gs)
	}
	if gs.Branch != "" {
		t.Errorf("detached HEAD Branch = %q, want empty", gs.Branch)
	}
}

// TestProbeGitStatus_NilRunnerOrDir — defensive: a nil runner or empty dir yields the
// zero (unprobed) status, never a panic.
func TestProbeGitStatus_NilRunnerOrDir(t *testing.T) {
	if gs := probeGitStatus(context.Background(), nil, "/x"); gs.Probed {
		t.Error("nil runner must yield Probed=false")
	}
	if gs := probeGitStatus(context.Background(), scriptedGitRunner{}, ""); gs.Probed {
		t.Error("empty dir must yield Probed=false")
	}
}
