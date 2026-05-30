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
