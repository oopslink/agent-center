// Package sessioninstance tracks the CLI single-instance lease per design §3, §4.1.
// It persists a session.instance file under the agent home directory containing
// session_id, generation (monotonic across acquisitions), pid, prev_pid, and
// prev_crash_at (populated when the prior instance did not call ReleaseInstance).
//
// The file is written atomically (temp-file + rename) following the same pattern
// as supervisormanager/epoch.go. A missing file is the zero/initial state; a
// corrupt file is an error (not silently zeroed).
//
// CONCURRENCY: AcquireInstance and ReleaseInstance must be called under the
// agent's home lock (supervisormanager.AcquireHomeLock) — see epoch.go for
// the same requirement. The atomic write is a second-line defense only.
package sessioninstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// InstanceFileName is the per-agent home file holding the single-instance lease.
const InstanceFileName = "session.instance"

// InstanceState tracks the CLI single-instance lease per design §3.
type InstanceState struct {
	SessionID   string    `json:"session_id"`
	Generation  int       `json:"generation"`
	PID         int       `json:"pid"`
	PrevPID     int       `json:"prev_pid"`
	PrevCrashAt time.Time `json:"prev_crash_at,omitempty"`
	// CleanRelease is true when the prior session exited via ReleaseInstance
	// (not a crash). Internal bookkeeping — AcquireInstance checks this to
	// decide whether to populate PrevCrashAt.
	CleanRelease bool `json:"clean_release,omitempty"`
	// CompletedTurn is true once THIS instance (this generation) has finished at
	// least one clean conversation turn (a Claude CLI "result" event that was not
	// an error). It is the cross-restart signal that the prior generation got far
	// enough to be safely --resume'd: resuming a session that never completed a
	// turn triggers a Claude no-completed-turn crash loop. AcquireInstance must
	// NOT carry this forward — a fresh generation has, by definition, not yet
	// completed a turn, so it is left false (its zero value) and set later via
	// MarkCompletedTurn.
	CompletedTurn bool `json:"completed_turn,omitempty"`
}

func instanceFilePath(home string) string {
	return filepath.Join(home, InstanceFileName)
}

// ReadInstance reads <home>/session.instance. A MISSING file is the initial
// state (zero value) — returned without error. A present but CORRUPT file is
// an ERROR, following the same principle as supervisormanager.ReadEpoch.
func ReadInstance(home string) (InstanceState, error) {
	if home == "" {
		return InstanceState{}, errors.New("sessioninstance: home required")
	}
	b, err := os.ReadFile(instanceFilePath(home))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return InstanceState{}, nil
		}
		return InstanceState{}, fmt.Errorf("sessioninstance: read: %w", err)
	}
	var st InstanceState
	if err := json.Unmarshal(b, &st); err != nil {
		return InstanceState{}, fmt.Errorf("sessioninstance: corrupt %s: %w",
			instanceFilePath(home), err)
	}
	return st, nil
}

// AcquireInstance claims the single-instance lease: bumps generation, records
// PID, preserves crash history. The caller must hold the agent home lock.
func AcquireInstance(home, sessionID string, pid int) (InstanceState, error) {
	if home == "" {
		return InstanceState{}, errors.New("sessioninstance: home required")
	}
	prev, err := ReadInstance(home)
	if err != nil {
		return InstanceState{}, err
	}

	next := InstanceState{
		SessionID:  sessionID,
		Generation: prev.Generation + 1,
		PID:        pid,
		PrevPID:    prev.PID,
	}
	// If the previous instance had a PID and was NOT cleanly released, it
	// crashed — record the crash timestamp.
	if prev.PID != 0 && !prev.CleanRelease {
		next.PrevCrashAt = time.Now().UTC()
	}
	if err := writeInstanceAtomic(home, next); err != nil {
		return InstanceState{}, err
	}
	return next, nil
}

// ReleaseInstance marks a clean shutdown: clears PID, sets CleanRelease so the
// next AcquireInstance knows the prior exit was intentional (not a crash).
func ReleaseInstance(home string) error {
	if home == "" {
		return errors.New("sessioninstance: home required")
	}
	st, err := ReadInstance(home)
	if err != nil {
		return err
	}
	st.PID = 0
	st.CleanRelease = true
	return writeInstanceAtomic(home, st)
}

// MarkCompletedTurn records that the current instance has finished at least one
// clean conversation turn, persisting CompletedTurn=true so a subsequent boot
// can safely --resume this session. It read-modify-writes the current state
// (preserving SessionID/Generation/PID), so it must be called by the process
// that currently holds the agent (the agent_controller serializes its writes);
// the atomic temp-file+rename is the second-line defense. Idempotent: calling it
// when CompletedTurn is already true rewrites the same value.
func MarkCompletedTurn(home string) error {
	if home == "" {
		return errors.New("sessioninstance: home required")
	}
	st, err := ReadInstance(home)
	if err != nil {
		return err
	}
	if st.CompletedTurn {
		return nil
	}
	st.CompletedTurn = true
	return writeInstanceAtomic(home, st)
}

// MarkSessionID persists sessionID into the current instance (read-modify-write,
// preserving Generation/PID/CompletedTurn), so a session whose LLM session id is minted
// at RUNTIME — codex: the thread_id from the first thread.started event — can be durably
// captured for a later resume. Unlike claude (whose session id is pre-assigned at
// AcquireInstance), codex has nothing to resume until this early-persist runs. Idempotent
// when sessionID is unchanged; an EMPTY sessionID is a no-op (it never CLEARS a captured
// id). Must be called by the process holding the agent (the agent_controller serializes
// its writes); the atomic temp-file+rename is the second-line defense.
func MarkSessionID(home, sessionID string) error {
	if home == "" {
		return errors.New("sessioninstance: home required")
	}
	if sessionID == "" {
		return nil // never clear a captured id
	}
	st, err := ReadInstance(home)
	if err != nil {
		return err
	}
	if st.SessionID == sessionID {
		return nil
	}
	st.SessionID = sessionID
	return writeInstanceAtomic(home, st)
}

// ClearSessionID removes a stale runtime-minted session id from the current
// instance while preserving the live lease metadata. This is intentionally separate
// from MarkSessionID because MarkSessionID("") is a defensive no-op for normal
// capture paths.
func ClearSessionID(home string) error {
	if home == "" {
		return errors.New("sessioninstance: home required")
	}
	st, err := ReadInstance(home)
	if err != nil {
		return err
	}
	if st.SessionID == "" {
		return nil
	}
	st.SessionID = ""
	return writeInstanceAtomic(home, st)
}

// writeInstanceAtomic persists st to <home>/session.instance via a temp file +
// rename so a crash mid-write never leaves a torn/partial file. The home
// directory is created if missing (0700).
func writeInstanceAtomic(home string, st InstanceState) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("sessioninstance: mkdir: %w", err)
	}
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("sessioninstance: marshal: %w", err)
	}
	final := instanceFilePath(home)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("sessioninstance: write tmp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("sessioninstance: rename: %w", err)
	}
	return nil
}
