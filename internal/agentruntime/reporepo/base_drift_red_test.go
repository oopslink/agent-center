package reporepo

// base_drift_red_test.go — RED BASELINE for the frozen-canonical-main bug [I112].
//
// THESE TESTS FAIL ON PURPOSE at 85356c92. They are the falsifiable judge for the fix:
// they go green if and only if a fetched canonical source actually advances the ref that
// PrepareWorktree branches from. Do not weaken them to make them pass.
//
// The bug, read off the production code in materializer.go:
//
//	EnsureSource reuse branch  → `git fetch --prune origin` and NOTHING else (:224).
//	                             origin/main advances; the LOCAL `main` never moves.
//	PrepareWorktree            → branches from resolvedBaseRef() (:401-403), which falls
//	                             back to DefaultBranch="main" (:67-72) — the LOCAL, frozen
//	                             main. So every executor forever branches off whatever
//	                             origin/main happened to be at FIRST-CLONE time.
//
// The fake-git suite in materializer_test.go cannot see any of this: its `clone`/`fetch`
// are os.MkdirAll no-ops with no refs, so "origin moved but local main didn't" is not
// even representable. Only real git against a real remote can express the drift, which is
// why this file uses the real runner end-to-end (real bare origin, real clone, real push,
// real worktree add) and asserts on real `rev-parse` output.
//
// Scope: this exercises the materializer port itself — the layer that owns the base-ref
// decision — not the dispatch plumbing above it. The drift originates here; every symptom
// upstream (a worktree cut from a stale base, a BaseRef that names a frozen ref, an
// ahead_of_base that bills other people's commits to this executor) is downstream of the
// two assertions below.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- rig ---------------------------------------------------------------------------
//
// A real bare origin plus a private seed clone used to push commits into it. The bare
// repo stands in for the real remote so a test can advance origin/main WITHOUT touching
// any real repository.

// baseDriftRig is a real bare origin + a seed clone that pushes into it.
type baseDriftRig struct {
	originURL string // path to the bare origin (a clone URL)
	seed      string // a working clone used only to author commits onto origin/main
}

// newBaseDriftRig stands up a bare origin whose main holds one commit, and returns the rig
// plus that first commit's sha.
func newBaseDriftRig(t *testing.T) (baseDriftRig, string) {
	t.Helper()
	originURL := filepath.Join(t.TempDir(), "origin.git")
	if err := os.MkdirAll(originURL, 0o755); err != nil {
		t.Fatalf("mkdir origin: %v", err)
	}
	runGit(t, originURL, "init", "-q", "--bare", "-b", "main")

	seed := t.TempDir()
	runGit(t, seed, "init", "-q", "-b", "main")
	runGit(t, seed, "remote", "add", "origin", originURL)
	rig := baseDriftRig{originURL: originURL, seed: seed}
	return rig, rig.pushCommit(t, "init")
}

// pushCommit authors one commit on the rig's seed clone, pushes it to origin/main, and
// returns its sha. This is how the test advances the REMOTE out from under a source that
// has already been cloned.
func (r baseDriftRig) pushCommit(t *testing.T, msg string) string {
	t.Helper()
	f := filepath.Join(r.seed, msg+".txt")
	if err := os.WriteFile(f, []byte(msg+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", f, err)
	}
	runGit(t, r.seed, "add", "-A")
	runGit(t, r.seed, "commit", "-q", "-m", msg)
	runGit(t, r.seed, "push", "-q", "origin", "main")
	return revParse(t, r.seed, "HEAD")
}

// revParse resolves a ref to a full sha in dir.
func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, dir, "rev-parse", ref))
}

// target is the RepoTarget the runtime builds for this rig: no explicit BaseRef, so
// resolvedBaseRef falls back to DefaultBranch — exactly the production default that makes
// the base a moving *name* rather than a pinned sha.
func (r baseDriftRig) target() RepoTarget {
	return RepoTarget{RepoID: "repo-1", URL: r.originURL, Provider: "local", DefaultBranch: "main"}
}

// prepare runs the real dispatch shape for one executor: EnsureSource (clone or fetch)
// then PrepareWorktree, and returns the prepared worktree.
func prepare(t *testing.T, m *LocalGitMaterializer, r baseDriftRig, execID string) PreparedWorktree {
	t.Helper()
	src, err := m.EnsureSource(context.Background(), r.target())
	if err != nil {
		t.Fatalf("EnsureSource(%s): %v", execID, err)
	}
	ws := filepath.Join(t.TempDir(), "executors", execID, "workspace")
	wt, err := m.PrepareWorktree(context.Background(), src, WorktreeRequest{
		ExecutorID:    execID,
		TaskID:        "task-1",
		BranchName:    "ac-exec/task-1/" + execID,
		WorkspacePath: ws,
	})
	if err != nil {
		t.Fatalf("PrepareWorktree(%s): %v", execID, err)
	}
	return wt
}

// --- the red -----------------------------------------------------------------------

// TestEnsureSource_FetchDoesNotAdvanceBase_NewWorktreeBranchesOffStaleMain is THE red.
//
// Shape (the minimal repro of the field incident):
//  1. cold empty repos root; first executor triggers the first clone at origin/main = A;
//  2. assert that executor's worktree is based at A (the honest precondition — if this
//     were already wrong the diagnosis would be a different bug);
//  3. advance the REMOTE to B by pushing to the rig's own bare origin;
//  4. a second executor dispatches: EnsureSource fetches, PrepareWorktree cuts a branch.
//     Its base MUST be B. On the buggy code it is A.
//
// The judgement is falsifiable in both directions: if step 4 yields B, there is no drift
// and the diagnosis is wrong.
func TestEnsureSource_FetchDoesNotAdvanceBase_NewWorktreeBranchesOffStaleMain(t *testing.T) {
	rig, shaA := newBaseDriftRig(t)
	m, _ := realGitMat(t)

	// 1+2. First clone at origin/main = A. Worktree #1 must sit at A.
	wt1 := prepare(t, m, rig, "exec-1")
	if got := revParse(t, wt1.WorkspacePath, "HEAD"); got != shaA {
		t.Fatalf("precondition broken: first worktree base = %s, want origin/main at clone time A = %s", got, shaA)
	}

	// 3. Someone else lands work: origin/main advances A -> B.
	shaB := rig.pushCommit(t, "someone-elses-work")
	if shaB == shaA {
		t.Fatalf("rig broken: origin/main did not advance (A = B = %s)", shaA)
	}

	// 4. A NEW executor dispatches after the fetch. It must branch off B.
	wt2 := prepare(t, m, rig, "exec-2")
	got := revParse(t, wt2.WorkspacePath, "HEAD")

	// Show the drift mechanically: the fetch DID land B on origin/main, but the local
	// `main` that PrepareWorktree branches from is still pinned at A.
	localMain := revParse(t, wt2.SourcePath, "main")
	remoteMain := revParse(t, wt2.SourcePath, "origin/main")
	t.Logf("A (first clone)      = %s", shaA)
	t.Logf("B (origin advanced)  = %s", shaB)
	t.Logf("source local  main   = %s", localMain)
	t.Logf("source origin/main   = %s", remoteMain)
	t.Logf("new worktree HEAD    = %s", got)

	if got != shaB {
		t.Errorf("RED: new executor branched off a STALE base.\n"+
			"  new worktree HEAD = %s\n"+
			"  want origin/main  = %s (B)\n"+
			"  got  frozen base  = %s (A, the sha origin/main held at FIRST CLONE)\n"+
			"  source local main = %s / source origin/main = %s\n"+
			"  ⇒ EnsureSource fetched B onto origin/main but never advanced local main,\n"+
			"    so PrepareWorktree keeps cutting branches off A. Drift is unbounded.",
			got, shaB, shaA, localMain, remoteMain)
	}
}

// TestBaseRef_NamesAFrozenRef_AheadOfBaseBillsForeignCommits pins the two machine-readable
// symptoms that ride on the same root cause, so a fix can be judged on them too.
//
// The recorded BaseRef is the NAME "main", not a sha. probeGitStatus (executor/gitstatus.go
// :129-137) later counts `rev-list --count main..HEAD` INSIDE the worktree — and a worktree
// shares the source's refs, so `main` there resolves to the same frozen A. Every commit
// between A and B that the executor merely SYNCED (never authored) is therefore counted
// into its own ahead_of_base — the machine-readable form of the "ahead 5" sighting.
func TestBaseRef_NamesAFrozenRef_AheadOfBaseBillsForeignCommits(t *testing.T) {
	rig, shaA := newBaseDriftRig(t)
	m, _ := realGitMat(t)

	prepare(t, m, rig, "exec-1") // first clone pins local main at A

	// Two commits by OTHER people land on origin/main.
	rig.pushCommit(t, "other-work-1")
	shaB := rig.pushCommit(t, "other-work-2")

	wt := prepare(t, m, rig, "exec-2")
	ws := wt.WorkspacePath

	// The durable record must name SOME base to measure against. It may be a moving ref
	// (`main`) or the concrete SHA the worktree was cut from — BOTH are valid shapes and
	// both are measurable, so assert only that a base EXISTS. Never assert its shape.
	//
	// An earlier revision asserted BaseRef == "main" here, on the reasoning that a fix
	// "moves where the name POINTS, not the fact that it is a name". That was an assumption
	// about the SHAPE of the fix, and it was wrong: recording the base SHA outright is also
	// a correct fix (a better one), and against it this precondition Fatal'd before the
	// verdict below ever ran — i.e. a false red on correct code. That is the same disease
	// as encoding the bug into the precondition, one level up: a precondition must assert
	// the SCENARIO (a base exists to measure against), never how the fix chose to spell it.
	if strings.TrimSpace(wt.BaseRef) == "" {
		t.Fatalf("precondition: BaseRef is empty — nothing to measure ahead_of_base against")
	}
	// NOTE: deliberately NOT asserting that the base is frozen at A here. That would encode
	// the bug into the precondition, making this test pass only on buggy code and fail on
	// a correct fix — a test that cannot go green judges nothing. The frozen ref is LOGGED
	// as the diagnosis and the count below is the sole verdict.
	t.Logf("BaseRef %q resolves to %s inside the worktree (A=%s, real origin/main B=%s)",
		wt.BaseRef, revParse(t, ws, wt.BaseRef), shaA, shaB)

	// The agent syncs to the real remote tip (what any `git pull` does), then authors
	// exactly ONE commit of its own.
	runGit(t, ws, "merge", "-q", "--ff-only", "origin/main")
	if err := os.WriteFile(filepath.Join(ws, "my-work.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, ws, "add", "-A")
	runGit(t, ws, "commit", "-q", "-m", "my-work")

	// This is literally what probeGitStatus runs to fill FinalizedGitStatus.AheadOfBase.
	ahead := strings.TrimSpace(runGit(t, ws, "rev-list", "--count", wt.BaseRef+"..HEAD"))
	t.Logf("ahead_of_base = %s (executor authored exactly 1 commit)", ahead)

	if ahead != "1" {
		t.Errorf("RED: ahead_of_base bills this executor for commits it never authored.\n"+
			"  ahead_of_base = %s, want 1 (the executor authored exactly ONE commit)\n"+
			"  BaseRef %q resolves to %s (A) but origin/main is %s (B)\n"+
			"  ⇒ the %s-1 extra commits are OTHER people's work in A..B, counted here only\n"+
			"    because the base ref is a name frozen at first-clone time.",
			ahead, wt.BaseRef, revParse(t, ws, wt.BaseRef), shaB, ahead)
	}
}
