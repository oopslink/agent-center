package taskexec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirManager_Create_And_Read(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{
		TaskID: "task-abc", Status: StatusPending, PlanID: "plan-1",
		CreatedAt: now, UpdatedAt: now,
	}
	execCtx := ExecutionContext{SessionID: "sess-1", LLMModel: "claude-4"}
	if err := dm.Create(tasksDir, meta, execCtx); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := dm.Read(tasksDir, "task-abc")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Meta.TaskID != "task-abc" || got.Meta.Status != StatusPending {
		t.Errorf("Read meta = %+v", got.Meta)
	}
	if got.ExecCtx.SessionID != "sess-1" {
		t.Errorf("Read execCtx = %+v", got.ExecCtx)
	}
}

func TestDirManager_UpdateStatus(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{TaskID: "task-1", Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	dm.Create(tasksDir, meta, ExecutionContext{})
	if err := dm.UpdateStatus(tasksDir, "task-1", StatusRunning); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := dm.Read(tasksDir, "task-1")
	if got.Meta.Status != StatusRunning {
		t.Errorf("Status = %q, want running", got.Meta.Status)
	}
}

func TestDirManager_ScanActive(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	// Create standard dirs
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-2", Status: StatusPaused, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	active := dm.ScanActive(tasksDir)
	if len(active) != 2 {
		t.Fatalf("ScanActive = %d entries, want 2", len(active))
	}
}

func TestDirManager_ScanActive_IgnoresAborted(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})
	// Create an aborted-looking directory manually
	abortedDir := filepath.Join(tasksDir, "t-2__aborted_20260626T100000Z")
	if err := mkdirAll(abortedDir); err != nil {
		t.Fatal(err)
	}

	active := dm.ScanActive(tasksDir)
	if len(active) != 1 {
		t.Fatalf("ScanActive = %d entries (should skip aborted), want 1", len(active))
	}
}

func TestDirManager_Create_ValidationError(t *testing.T) {
	dm := NewDirManager()
	// empty TaskID should fail Validate()
	err := dm.Create(t.TempDir(), TaskExecutionMeta{Status: StatusPending}, ExecutionContext{})
	if err == nil {
		t.Fatal("expected error for empty TaskID, got nil")
	}
}

func TestDirManager_Create_MkdirError(t *testing.T) {
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{TaskID: "t-x", Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	// Use a file as parent so MkdirAll fails
	f, err := os.CreateTemp("", "notadir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())
	if err := dm.Create(f.Name(), meta, ExecutionContext{}); err == nil {
		t.Fatal("expected error when tasksDir is a file")
	}
}

func TestDirManager_Read_MissingFile(t *testing.T) {
	dm := NewDirManager()
	_, err := dm.Read(t.TempDir(), "nonexistent")
	if err == nil {
		t.Fatal("expected error reading nonexistent task")
	}
}

func TestDirManager_Read_CorruptJSON(t *testing.T) {
	tasksDir := t.TempDir()
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{TaskID: "t-corrupt", Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := dm.Create(tasksDir, meta, ExecutionContext{}); err != nil {
		t.Fatal(err)
	}
	// Corrupt the execution.json (Read silently ignores it) and task.json
	taskFile := filepath.Join(tasksDir, "t-corrupt", taskMetaFile)
	if err := os.WriteFile(taskFile, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := dm.Read(tasksDir, "t-corrupt")
	if err == nil {
		t.Fatal("expected error on corrupt task.json")
	}
}

func TestDirManager_Read_CorruptExecJSON(t *testing.T) {
	tasksDir := t.TempDir()
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{TaskID: "t-exec", Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := dm.Create(tasksDir, meta, ExecutionContext{}); err != nil {
		t.Fatal(err)
	}
	// Corrupt execution.json — Read should now return an error (§17: no swallowed errors)
	execFile := filepath.Join(tasksDir, "t-exec", execContextFile)
	if err := os.WriteFile(execFile, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, readErr := dm.Read(tasksDir, "t-exec")
	if readErr == nil {
		t.Fatal("Read with corrupt execution.json should return an error")
	}
}

func TestDirManager_UpdateStatus_MissingTask(t *testing.T) {
	dm := NewDirManager()
	err := dm.UpdateStatus(t.TempDir(), "no-such-task", StatusRunning)
	if err == nil {
		t.Fatal("expected error updating nonexistent task")
	}
}

func TestDirManager_ScanActive_NonexistentDir(t *testing.T) {
	dm := NewDirManager()
	result := dm.ScanActive("/tmp/does-not-exist-agent-center-test")
	if result != nil {
		t.Errorf("expected nil result for nonexistent dir, got %v", result)
	}
}

func TestDirManager_ScanActive_SkipsFiles(t *testing.T) {
	tasksDir := t.TempDir()
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-ok", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})
	// Place a plain file and a dir with no task.json — both should be skipped
	os.WriteFile(filepath.Join(tasksDir, "somefile.txt"), []byte("x"), 0o600)
	os.MkdirAll(filepath.Join(tasksDir, "no-meta"), 0o700)

	active := dm.ScanActive(tasksDir)
	if len(active) != 1 {
		t.Fatalf("ScanActive = %d, want 1", len(active))
	}
}

func TestWriteJSONAtomic_MarshalError(t *testing.T) {
	// json.Marshal fails on channels
	ch := make(chan int)
	err := writeJSONAtomic(filepath.Join(t.TempDir(), "out.json"), ch)
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestWriteJSONAtomic_WriteToUnwritableDir(t *testing.T) {
	// Write to a path whose parent doesn't exist
	err := writeJSONAtomic("/tmp/no-such-dir-agent-center/out.json", map[string]string{"k": "v"})
	if err == nil {
		t.Fatal("expected error writing to nonexistent directory")
	}
}

func TestReadJSON_UnmarshalError(t *testing.T) {
	f, err := os.CreateTemp("", "badjson")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("not-json"))
	f.Close()
	defer os.Remove(f.Name())

	var m map[string]string
	err = readJSON(f.Name(), &m)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	var synErr *json.SyntaxError
	// just confirm it's a real error, not necessarily a syntax error type check
	_ = synErr
}

func TestIsStandardTaskDir(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"task-abc", true},
		{"01JXYZ", true},
		{"task-1__aborted_20260626T100000Z", false},
		{"task-1__aborted_20260626T100000Z__gc_deleting", false},
		{".hidden", false},
	}
	for _, tt := range tests {
		if got := IsStandardTaskDir(tt.name); got != tt.want {
			t.Errorf("IsStandardTaskDir(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
