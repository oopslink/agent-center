package supervisormanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildSupervisorArgs_ResetEpoch proves the spawn argv carries --reset-epoch
// ONLY for a post-reset spawn (epoch > 0), and omits it for the initial epoch 0
// (matching the subcommand default). This is the daemon→supervisor half of the
// epoch plumbing: SpawnSupervisor passes the resolved epoch, the subcommand
// re-derives the session-id from it.
func TestBuildSupervisorArgs_ResetEpoch(t *testing.T) {
	base := SpawnSupervisorCfg{AgentID: "agent-1", HomeDir: "/home/agent-1"}

	// epoch 0 → flag omitted.
	got0 := strings.Join(buildSupervisorArgs(base), " ")
	if strings.Contains(got0, "--reset-epoch") {
		t.Fatalf("epoch 0 must omit --reset-epoch, got %q", got0)
	}

	// epoch > 0 → flag present with the value.
	cfg := base
	cfg.Epoch = 3
	got := strings.Join(buildSupervisorArgs(cfg), " ")
	if !strings.Contains(got, "--reset-epoch 3") {
		t.Fatalf("epoch 3 must emit --reset-epoch 3, got %q", got)
	}
	// Sanity: the required identity flags are always present.
	for _, w := range []string{"agent-supervisor", "--agent-id agent-1", "--home-dir /home/agent-1"} {
		if !strings.Contains(got, w) {
			t.Fatalf("argv missing %q: %q", w, got)
		}
	}
}

// TestBuildSupervisorArgs_DisplayName (T469) proves the spawn argv carries
// --display-name ONLY when set, so the supervisor injects it as the git author NAME
// (② AgentEnv seam); an empty display_name omits the flag → the supervisor falls back
// to the ULID AgentID.
func TestBuildSupervisorArgs_DisplayName(t *testing.T) {
	base := SpawnSupervisorCfg{AgentID: "agent-1", HomeDir: "/home/agent-1"}

	// empty → flag omitted.
	got0 := strings.Join(buildSupervisorArgs(base), " ")
	if strings.Contains(got0, "--display-name") {
		t.Fatalf("empty display_name must omit --display-name, got %q", got0)
	}

	// set → flag present with the value.
	cfg := base
	cfg.DisplayName = "agent-center-dev4"
	got := strings.Join(buildSupervisorArgs(cfg), " ")
	if !strings.Contains(got, "--display-name agent-center-dev4") {
		t.Fatalf("display_name must emit --display-name agent-center-dev4, got %q", got)
	}
}

// TestBuildSupervisorArgs_GenerationAndResumeFrom proves the spawn argv carries the
// v2.7 GATE-7 Mode-B fork flags only when set: --generation for generation > 0 (0 =
// the subcommand default, omitted) and --resume-from when a fork source is given.
func TestBuildSupervisorArgs_GenerationAndResumeFrom(t *testing.T) {
	base := SpawnSupervisorCfg{AgentID: "agent-1", HomeDir: "/home/agent-1"}

	// Initial/normal start: neither flag.
	got0 := strings.Join(buildSupervisorArgs(base), " ")
	if strings.Contains(got0, "--generation") || strings.Contains(got0, "--resume-from") {
		t.Fatalf("generation 0 / no fork must omit both flags, got %q", got0)
	}

	// Mode-B fork relaunch: both present.
	cfg := base
	cfg.Generation = 2
	cfg.ResumeFromSessionID = "prev-session-uuid"
	got := strings.Join(buildSupervisorArgs(cfg), " ")
	if !strings.Contains(got, "--generation 2") {
		t.Fatalf("generation 2 must emit --generation 2, got %q", got)
	}
	if !strings.Contains(got, "--resume-from prev-session-uuid") {
		t.Fatalf("fork source must emit --resume-from, got %q", got)
	}
}

// TestBumpGenerationForRelaunch_PerAttemptMonotonic pins the key correctness
// property (PM's catch): generation increments by exactly ONE PER CALL and
// persists, so each Mode-B relaunch attempt derives a FRESH never-locked id. (If a
// single gen+1 were reused across the backoff loop, attempt #2 would re-collide
// with attempt #1's now-locked id → the crash-loop would self-recur.) Epoch and
// LastResetVersion are preserved across bumps.
func TestBumpGenerationForRelaunch_PerAttemptMonotonic(t *testing.T) {
	home := t.TempDir()
	// Seed a reset so epoch + last_reset_version are non-zero (must be preserved).
	if _, err := BumpEpochForReset(home, 4); err != nil {
		t.Fatalf("seed reset: %v", err)
	}

	for want := 1; want <= 3; want++ {
		st, err := BumpGenerationForRelaunch(home)
		if err != nil {
			t.Fatalf("bump generation #%d: %v", want, err)
		}
		if st.Generation != want {
			t.Fatalf("attempt #%d must yield generation %d, got %d (per-attempt monotonic)", want, want, st.Generation)
		}
		if st.Epoch != 1 || st.LastResetVersion != 4 {
			t.Fatalf("generation bump must preserve epoch/last_reset_version, got %+v", st)
		}
		// Durable: a fresh read (as the next spawn / a boot-reconcile would do) sees it.
		got, err := ReadEpoch(home)
		if err != nil {
			t.Fatalf("re-read: %v", err)
		}
		if got != st {
			t.Fatalf("persisted %+v != bumped %+v", got, st)
		}
	}
}

// TestBumpEpochForReset_ZeroesGeneration proves a reset is a clean slate on BOTH
// axes: it advances Epoch AND resets Generation to 0, so a post-reset agent starts
// at the pre-fix base id (gen 0), not carrying a stale fork generation.
func TestBumpEpochForReset_ZeroesGeneration(t *testing.T) {
	home := t.TempDir()
	// Accrue some fork generations (as crash relaunches would).
	for i := 0; i < 3; i++ {
		if _, err := BumpGenerationForRelaunch(home); err != nil {
			t.Fatalf("bump generation: %v", err)
		}
	}
	pre, _ := ReadEpoch(home)
	if pre.Generation != 3 {
		t.Fatalf("setup: want generation 3, got %d", pre.Generation)
	}

	st, err := BumpEpochForReset(home, 1)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if st.Epoch != 1 || st.Generation != 0 {
		t.Fatalf("reset must advance epoch AND zero generation, got %+v", st)
	}
}

// TestReadEpoch_MissingIsInitial proves a fresh agent (no session.epoch file) reads
// as the initial {0,0} WITHOUT error — a never-reset agent spawns at epoch 0.
func TestReadEpoch_MissingIsInitial(t *testing.T) {
	home := t.TempDir()
	st, err := ReadEpoch(home)
	if err != nil {
		t.Fatalf("ReadEpoch on fresh home: %v", err)
	}
	if st.Epoch != 0 || st.LastResetVersion != 0 {
		t.Fatalf("missing epoch file must be initial {0,0}, got %+v", st)
	}
}

// TestReadEpoch_CorruptIsError proves a present-but-corrupt epoch file is an ERROR,
// not silently collapsed to {0,0}. Collapsing corruption to epoch 0 would let a
// crash-relaunch mistake it for "never reset" and start a fresh claude session —
// the exact context-loss trap the store guards against.
func TestReadEpoch_CorruptIsError(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, EpochFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEpoch(home); err == nil {
		t.Fatal("corrupt epoch file must error, not be treated as {0,0}")
	}
}

// TestBumpEpochForReset_AdvancesThenPersists proves a reset advances Epoch by one,
// records the reconcile version, and persists durably (re-read sees the new value
// — what a later crash-relaunch reads to resume the SAME reset session).
func TestBumpEpochForReset_AdvancesThenPersists(t *testing.T) {
	home := t.TempDir()

	st, err := BumpEpochForReset(home, 5)
	if err != nil {
		t.Fatalf("first bump: %v", err)
	}
	if st.Epoch != 1 || st.LastResetVersion != 5 {
		t.Fatalf("first reset must be {1,5}, got %+v", st)
	}
	// Durable: a fresh read (as a relaunch would do) sees the persisted epoch.
	got, err := ReadEpoch(home)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got != st {
		t.Fatalf("persisted epoch %+v != bumped %+v", got, st)
	}

	// A NEWER reset advances again.
	st2, err := BumpEpochForReset(home, 9)
	if err != nil {
		t.Fatalf("second bump: %v", err)
	}
	if st2.Epoch != 2 || st2.LastResetVersion != 9 {
		t.Fatalf("second reset must be {2,9}, got %+v", st2)
	}
}

// TestBumpEpochForReset_Idempotent proves the bump is keyed on the reconcile
// version: a replayed reset (version <= LastResetVersion) is a NO-OP — no
// double-bump, no session-id churn. Reconcile delivery is at-least-once, so this
// idempotence is load-bearing.
func TestBumpEpochForReset_Idempotent(t *testing.T) {
	home := t.TempDir()

	first, err := BumpEpochForReset(home, 7)
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if first.Epoch != 1 {
		t.Fatalf("want epoch 1, got %d", first.Epoch)
	}

	// Replay the SAME version → unchanged.
	replay, err := BumpEpochForReset(home, 7)
	if err != nil {
		t.Fatalf("replay bump: %v", err)
	}
	if replay != first {
		t.Fatalf("replay of reset v7 must be a no-op: %+v != %+v", replay, first)
	}

	// An OLDER version (out-of-order redelivery) → also unchanged.
	older, err := BumpEpochForReset(home, 3)
	if err != nil {
		t.Fatalf("older bump: %v", err)
	}
	if older != first {
		t.Fatalf("older reset v3 must not bump past v7 state: %+v != %+v", older, first)
	}
}
