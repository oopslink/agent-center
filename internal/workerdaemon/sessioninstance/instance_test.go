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
