package sessioninstance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadInstance_Missing_ReturnsZero(t *testing.T) {
	home := t.TempDir()
	st, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("ReadInstance on empty home: %v", err)
	}
	if st.Generation != 0 || st.SessionID != "" || st.PID != 0 {
		t.Errorf("expected zero state, got %+v", st)
	}
}

func TestAcquireInstance_Fresh(t *testing.T) {
	home := t.TempDir()
	st, err := AcquireInstance(home, "sess-1", 1234)
	if err != nil {
		t.Fatalf("AcquireInstance: %v", err)
	}
	if st.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", st.SessionID, "sess-1")
	}
	if st.Generation != 1 {
		t.Errorf("Generation = %d, want 1", st.Generation)
	}
	if st.PID != 1234 {
		t.Errorf("PID = %d, want 1234", st.PID)
	}
	if st.PrevPID != 0 {
		t.Errorf("PrevPID = %d, want 0", st.PrevPID)
	}

	// Verify persisted
	persisted, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("ReadInstance after acquire: %v", err)
	}
	if persisted.SessionID != "sess-1" || persisted.Generation != 1 {
		t.Errorf("persisted = %+v", persisted)
	}
}

func TestAcquireInstance_Successive_BumpsGeneration(t *testing.T) {
	home := t.TempDir()
	st1, _ := AcquireInstance(home, "sess-1", 100)
	st2, err := AcquireInstance(home, "sess-2", 200)
	if err != nil {
		t.Fatalf("second AcquireInstance: %v", err)
	}
	if st2.Generation != st1.Generation+1 {
		t.Errorf("Generation = %d, want %d", st2.Generation, st1.Generation+1)
	}
	if st2.PrevPID != 100 {
		t.Errorf("PrevPID = %d, want 100", st2.PrevPID)
	}
}

func TestAcquireInstance_AfterCrash_RecordsPrevCrashAt(t *testing.T) {
	home := t.TempDir()
	// Simulate a crash: write state with a PID, then re-acquire without release
	AcquireInstance(home, "sess-1", 100)
	// No ReleaseInstance call = simulated crash
	st, err := AcquireInstance(home, "sess-2", 200)
	if err != nil {
		t.Fatalf("AcquireInstance after crash: %v", err)
	}
	if st.PrevCrashAt.IsZero() {
		t.Error("PrevCrashAt should be non-zero after crash (no clean release)")
	}
}

func TestReleaseInstance(t *testing.T) {
	home := t.TempDir()
	AcquireInstance(home, "sess-1", 100)
	if err := ReleaseInstance(home); err != nil {
		t.Fatalf("ReleaseInstance: %v", err)
	}
	// After release, the file still exists but PID is cleared
	st, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("ReadInstance after release: %v", err)
	}
	if st.PID != 0 {
		t.Errorf("PID = %d after release, want 0", st.PID)
	}
}

func TestReadInstance_CorruptFile_ReturnsError(t *testing.T) {
	home := t.TempDir()
	// Write garbage
	os.WriteFile(filepath.Join(home, InstanceFileName), []byte("not json"), 0o600)
	_, err := ReadInstance(home)
	if err == nil {
		t.Fatal("expected error on corrupt file, got nil")
	}
}

func TestReadInstance_EmptyHome_ReturnsError(t *testing.T) {
	_, err := ReadInstance("")
	if err == nil {
		t.Fatal("expected error for empty home, got nil")
	}
}

func TestAcquireInstance_EmptyHome_ReturnsError(t *testing.T) {
	_, err := AcquireInstance("", "sess-1", 1234)
	if err == nil {
		t.Fatal("expected error for empty home, got nil")
	}
}

func TestReleaseInstance_EmptyHome_ReturnsError(t *testing.T) {
	err := ReleaseInstance("")
	if err == nil {
		t.Fatal("expected error for empty home, got nil")
	}
}

func TestAcquireInstance_AfterCleanRelease_NoCrashAt(t *testing.T) {
	home := t.TempDir()
	AcquireInstance(home, "sess-1", 100)
	ReleaseInstance(home) // clean release
	st, err := AcquireInstance(home, "sess-2", 200)
	if err != nil {
		t.Fatalf("AcquireInstance after clean release: %v", err)
	}
	if !st.PrevCrashAt.IsZero() {
		t.Errorf("PrevCrashAt should be zero after clean release, got %v", st.PrevCrashAt)
	}
}

func TestAcquireInstance_GenerationMonotonic(t *testing.T) {
	home := t.TempDir()
	for i := 1; i <= 5; i++ {
		st, err := AcquireInstance(home, "sess", i*100)
		if err != nil {
			t.Fatalf("AcquireInstance iter %d: %v", i, err)
		}
		if st.Generation != i {
			t.Errorf("iter %d: Generation = %d, want %d", i, st.Generation, i)
		}
	}
}

func TestReleaseInstance_SetsCleanRelease(t *testing.T) {
	home := t.TempDir()
	AcquireInstance(home, "sess-1", 100)
	ReleaseInstance(home)
	st, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("ReadInstance: %v", err)
	}
	if !st.CleanRelease {
		t.Error("CleanRelease should be true after ReleaseInstance")
	}
}

func TestAcquireInstance_PrevCrashAt_Timestamp(t *testing.T) {
	home := t.TempDir()
	before := time.Now().UTC()
	AcquireInstance(home, "sess-1", 100)
	// No release = crash
	st, err := AcquireInstance(home, "sess-2", 200)
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("AcquireInstance: %v", err)
	}
	if st.PrevCrashAt.Before(before) || st.PrevCrashAt.After(after) {
		t.Errorf("PrevCrashAt %v not in [%v, %v]", st.PrevCrashAt, before, after)
	}
}

func TestAcquireInstance_FreshHome_NoPrevPID(t *testing.T) {
	home := t.TempDir()
	st, err := AcquireInstance(home, "sess-1", 999)
	if err != nil {
		t.Fatalf("AcquireInstance: %v", err)
	}
	if st.PrevPID != 0 {
		t.Errorf("PrevPID = %d on fresh home, want 0", st.PrevPID)
	}
	if !st.PrevCrashAt.IsZero() {
		t.Errorf("PrevCrashAt = %v on fresh home, want zero", st.PrevCrashAt)
	}
}

func TestReadInstance_UnreadableFile_ReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read any file")
	}
	home := t.TempDir()
	path := filepath.Join(home, InstanceFileName)
	if err := os.WriteFile(path, []byte(`{"session_id":"x"}`), 0o000); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadInstance(home)
	if err == nil {
		t.Fatal("expected error on unreadable file, got nil")
	}
}

func TestWriteInstanceAtomic_UnwritableDir_ReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	home := t.TempDir()
	// Make the home read-only so WriteFile to .tmp fails.
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(home, 0o700) })

	_, err := AcquireInstance(home, "sess-1", 1234)
	if err == nil {
		t.Fatal("expected error on unwritable dir, got nil")
	}
}

func TestReleaseInstance_NoFile_ReturnsZeroAndWrites(t *testing.T) {
	// ReleaseInstance on a fresh home: ReadInstance returns zero, PID is already
	// 0, so writing is still valid (no crash — just clears and marks clean).
	home := t.TempDir()
	if err := ReleaseInstance(home); err != nil {
		t.Fatalf("ReleaseInstance on fresh home: %v", err)
	}
	st, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("ReadInstance: %v", err)
	}
	if st.PID != 0 {
		t.Errorf("PID = %d, want 0", st.PID)
	}
	if !st.CleanRelease {
		t.Error("CleanRelease should be true")
	}
}

func TestAcquireInstance_CorruptExistingFile_ReturnsError(t *testing.T) {
	home := t.TempDir()
	// Write a corrupt file so ReadInstance inside AcquireInstance returns an error.
	os.WriteFile(filepath.Join(home, InstanceFileName), []byte("not json"), 0o600)
	_, err := AcquireInstance(home, "sess-1", 1234)
	if err == nil {
		t.Fatal("expected error on corrupt existing file, got nil")
	}
}

func TestReleaseInstance_CorruptExistingFile_ReturnsError(t *testing.T) {
	home := t.TempDir()
	// Write a corrupt file so ReadInstance inside ReleaseInstance returns an error.
	os.WriteFile(filepath.Join(home, InstanceFileName), []byte("not json"), 0o600)
	err := ReleaseInstance(home)
	if err == nil {
		t.Fatal("expected error on corrupt existing file, got nil")
	}
}

func TestWriteInstanceAtomic_RenameFailure_ReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can rename anywhere")
	}
	home := t.TempDir()
	// First acquire succeeds (creates the directory).
	if _, err := AcquireInstance(home, "sess-1", 100); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// Make the directory read-only: WriteFile of .tmp will fail (cannot create new file).
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(home, 0o700) })

	_, err := AcquireInstance(home, "sess-2", 200)
	if err == nil {
		t.Fatal("expected error when dir is read-only, got nil")
	}
}

// 重启不丢上下文 改动 A: MarkCompletedTurn read-modify-writes CompletedTurn=true on the
// CURRENT instance, preserving the rest of the lease, and is observable by ReadInstance.
// T972: MarkSessionID persists a runtime-minted session id (codex thread_id) into the
// current instance, preserving Generation/PID/CompletedTurn; an empty id never CLEARS a
// captured one; it is idempotent when unchanged.
func TestMarkSessionID_SetsPreservesAndGuards(t *testing.T) {
	home := t.TempDir()
	if _, err := AcquireInstance(home, "", 100); err != nil { // codex starts with no session id
		t.Fatalf("acquire: %v", err)
	}
	if err := MarkCompletedTurn(home); err != nil {
		t.Fatalf("mark turn: %v", err)
	}
	// Capture the thread_id (early-persist).
	if err := MarkSessionID(home, "th_abc"); err != nil {
		t.Fatalf("MarkSessionID: %v", err)
	}
	st, _ := ReadInstance(home)
	if st.SessionID != "th_abc" {
		t.Fatalf("SessionID = %q, want th_abc", st.SessionID)
	}
	// Preserves the rest of the lease (read-modify-write).
	if st.Generation != 1 || st.PID != 100 || !st.CompletedTurn {
		t.Fatalf("MarkSessionID clobbered the lease: %+v", st)
	}
	// Empty id is a no-op (never clears a captured id).
	if err := MarkSessionID(home, ""); err != nil {
		t.Fatalf("empty MarkSessionID: %v", err)
	}
	if st, _ := ReadInstance(home); st.SessionID != "th_abc" {
		t.Fatalf("empty id must not clear captured: %q", st.SessionID)
	}
}

func TestClearSessionID_ClearsPreservesAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	if _, err := AcquireInstance(home, "th_stale", 100); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := MarkCompletedTurn(home); err != nil {
		t.Fatalf("mark turn: %v", err)
	}
	before, err := ReadInstance(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := ClearSessionID(home); err != nil {
		t.Fatalf("ClearSessionID: %v", err)
	}
	after, err := ReadInstance(home)
	if err != nil {
		t.Fatal(err)
	}
	if after.SessionID != "" {
		t.Fatalf("SessionID = %q, want cleared", after.SessionID)
	}
	if after.Generation != before.Generation || after.PID != before.PID || after.CompletedTurn != before.CompletedTurn {
		t.Fatalf("ClearSessionID clobbered the lease: before=%+v after=%+v", before, after)
	}
	if err := ClearSessionID(home); err != nil {
		t.Fatalf("second ClearSessionID should be idempotent: %v", err)
	}
}

func TestMarkCompletedTurn_SetsAndPersists(t *testing.T) {
	home := t.TempDir()
	if _, err := AcquireInstance(home, "sess-1", 100); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := MarkCompletedTurn(home); err != nil {
		t.Fatalf("MarkCompletedTurn: %v", err)
	}
	st, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !st.CompletedTurn {
		t.Fatalf("CompletedTurn = false, want true")
	}
	// The rest of the lease is preserved (read-modify-write, not overwrite).
	if st.SessionID != "sess-1" || st.Generation != 1 || st.PID != 100 {
		t.Fatalf("MarkCompletedTurn clobbered the lease: %+v", st)
	}
}

// MarkCompletedTurn is idempotent: a second call leaves CompletedTurn true.
func TestMarkCompletedTurn_Idempotent(t *testing.T) {
	home := t.TempDir()
	if _, err := AcquireInstance(home, "sess-1", 100); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := MarkCompletedTurn(home); err != nil {
		t.Fatalf("first MarkCompletedTurn: %v", err)
	}
	if err := MarkCompletedTurn(home); err != nil {
		t.Fatalf("second MarkCompletedTurn: %v", err)
	}
	st, _ := ReadInstance(home)
	if !st.CompletedTurn {
		t.Fatalf("CompletedTurn = false after idempotent re-mark, want true")
	}
}

func TestMarkCompletedTurn_EmptyHome_ReturnsError(t *testing.T) {
	if err := MarkCompletedTurn(""); err == nil {
		t.Fatal("expected error for empty home, got nil")
	}
}

// CRITICAL invariant: a fresh generation must NOT inherit the prior generation's
// CompletedTurn — AcquireInstance leaves it false so a never-completed new gen is
// not falsely treated as safely resumable (which would re-open the no-completed-turn
// crash loop boot_reconcile relies on CompletedTurn to avoid).
func TestAcquireInstance_DoesNotCarryCompletedTurn(t *testing.T) {
	home := t.TempDir()
	if _, err := AcquireInstance(home, "sess-1", 100); err != nil {
		t.Fatalf("acquire gen1: %v", err)
	}
	if err := MarkCompletedTurn(home); err != nil {
		t.Fatalf("mark gen1: %v", err)
	}
	// Next acquisition (a relaunch / new generation) must reset CompletedTurn.
	st2, err := AcquireInstance(home, "sess-2", 200)
	if err != nil {
		t.Fatalf("acquire gen2: %v", err)
	}
	if st2.CompletedTurn {
		t.Fatalf("gen2 carried CompletedTurn forward; want false on a fresh generation")
	}
	persisted, _ := ReadInstance(home)
	if persisted.CompletedTurn {
		t.Fatalf("persisted gen2 has CompletedTurn=true; want false")
	}
}
