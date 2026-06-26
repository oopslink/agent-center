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
