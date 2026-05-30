package supervisormanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// epoch.go is the DURABLE per-agent reset-epoch store (v2.7 D2-f s3b-2a). It is
// the persistence behind claudestream.SessionUUID(agentID, epoch): the epoch
// selects which claude session-id an agent's supervisor spawns with, so it MUST
// outlive the daemon (and survive a crash) — otherwise a mode-B crash-relaunch
// would fall back to epoch 0, re-derive a DIFFERENT session-id, and silently
// start a fresh claude conversation. That is the precise context-loss trap this
// store exists to prevent: a CRASH must resume the same session; only an explicit
// RESET advances the epoch to a clean slate.
//
// STORAGE LOCATION (PM-approved, v2.7): a per-agent HOME file
// <home>/session.epoch holding {epoch, last_reset_version}. Home-local is correct
// for v2.7 because deployments are single-machine / same-worker with NO
// cross-machine migration — the agent always relaunches against the same home, so
// the file is always there.
//
// 🕒 DEFERRED-WITH-TRIGGER (PM): the COMPANION upgrade is a center-authoritative
// session_epoch on the Agent aggregate (carried in resume-state), needed WHEN
// cross-machine migration lands — an agent moving to a NEW worker has a fresh
// home with no session.epoch and would otherwise reset to 0, losing its session.
// That is explicitly OUT OF SCOPE for v2.7 and bound to the migration roadmap
// item; this home-file store is the v2.7 stand-in. Do not generalize it to a
// center sync here.
//
// CONCURRENCY: BumpEpochForReset must be called UNDER the agent's home lock
// (AcquireHomeLock — see lock.go); the reset sequence (s3b-2b) holds that lock
// across the whole stop→wipe→bump→spawn chain so the bump cannot interleave with
// a probe or a competing daemon. The temp-write+rename gives single-write
// atomicity as a second line of defense.

// EpochFileName is the per-agent home file holding the durable reset epoch.
const EpochFileName = "session.epoch"

// EpochState is the persisted reset epoch for one agent.
//
//   - Epoch is the clean-slate counter fed to SessionUUID(agentID, Epoch). 0 = a
//     never-reset agent.
//   - LastResetVersion is the reconcile version that last advanced the epoch. It
//     makes BumpEpochForReset IDEMPOTENT: reconcile delivery is at-least-once, so a
//     replayed reset (version <= LastResetVersion) must NOT double-bump.
type EpochState struct {
	Epoch            int `json:"epoch"`
	LastResetVersion int `json:"last_reset_version"`
}

func epochFilePath(home string) string { return filepath.Join(home, EpochFileName) }

// ReadEpoch reads <home>/session.epoch. A MISSING file is the initial state
// {0, 0} (a fresh, never-reset agent) — returned without error. A present but
// CORRUPT/unreadable file is an ERROR, NOT silently treated as {0,0}: collapsing
// corruption to epoch 0 would re-introduce the very context-loss trap this store
// guards against (a crash-relaunch would mistake the corrupt file for "never
// reset" and start a fresh claude session). The caller must surface it.
func ReadEpoch(home string) (EpochState, error) {
	if home == "" {
		return EpochState{}, errors.New("supervisormanager: ReadEpoch requires home")
	}
	b, err := os.ReadFile(epochFilePath(home))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EpochState{Epoch: 0, LastResetVersion: 0}, nil
		}
		return EpochState{}, fmt.Errorf("supervisormanager: read epoch: %w", err)
	}
	var st EpochState
	if err := json.Unmarshal(b, &st); err != nil {
		return EpochState{}, fmt.Errorf("supervisormanager: corrupt epoch file %s: %w", epochFilePath(home), err)
	}
	if st.Epoch < 0 {
		return EpochState{}, fmt.Errorf("supervisormanager: invalid negative epoch %d in %s", st.Epoch, epochFilePath(home))
	}
	return st, nil
}

// BumpEpochForReset advances the agent's epoch for a clean-slate reset, IDEMPOTENTLY.
//
// reconcileVersion is the version of the reset reconcile being applied. Because
// reconcile delivery is at-least-once, a reset may be re-delivered; the bump is
// keyed on the version so a replay is a no-op:
//   - reconcileVersion <= LastResetVersion → REPLAY: return the current state
//     UNCHANGED (no double-bump, no new session-id churn).
//   - reconcileVersion >  LastResetVersion → advance: Epoch++ and
//     LastResetVersion = reconcileVersion, persist atomically, return the new state.
//
// MUST be called under the agent's home lock (see the package note). The write is
// temp-file + rename for atomicity; a returned EpochState is the value now on disk.
func BumpEpochForReset(home string, reconcileVersion int) (EpochState, error) {
	if home == "" {
		return EpochState{}, errors.New("supervisormanager: BumpEpochForReset requires home")
	}
	cur, err := ReadEpoch(home)
	if err != nil {
		return EpochState{}, err
	}
	if reconcileVersion <= cur.LastResetVersion {
		// Replay of an already-applied reset: idempotent no-op.
		return cur, nil
	}
	next := EpochState{
		Epoch:            cur.Epoch + 1,
		LastResetVersion: reconcileVersion,
	}
	if err := writeEpochAtomic(home, next); err != nil {
		return EpochState{}, err
	}
	return next, nil
}

// writeEpochAtomic persists st to <home>/session.epoch via a temp file + rename so
// a crash mid-write never leaves a torn/partial epoch file (which ReadEpoch would
// then reject as corrupt). The home dir is created if missing (0700).
func writeEpochAtomic(home string, st EpochState) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("supervisormanager: mkdir home for epoch: %w", err)
	}
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("supervisormanager: marshal epoch: %w", err)
	}
	final := epochFilePath(home)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("supervisormanager: write temp epoch: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("supervisormanager: rename epoch: %w", err)
	}
	return nil
}
