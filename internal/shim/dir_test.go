package shim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDir_AtomicWriteEnvelope(t *testing.T) {
	root := t.TempDir()
	d, err := NewDir(root, "E-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.WriteEnvelope([]byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"x":1}` {
		t.Fatalf("got %s", got)
	}
	// confirm no stray .tmp left
	files, _ := os.ReadDir(d.Path())
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".tmp" {
			t.Fatalf("stray tmp file: %s", f.Name())
		}
	}
}

func TestDir_StatusRoundtrip(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	s := Status{
		ExecutionID:    "E-1",
		Phase:          PhaseRunning,
		ShimPID:        100,
		ShimStartTime:  now,
		AgentPID:       200,
		AgentStartTime: now.Add(time.Second),
	}
	if err := d.WriteStatus(s); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadStatus()
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != PhaseRunning || got.AgentPID != 200 {
		t.Fatalf("status: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected updated_at set")
	}
}

func TestDir_PIDFile(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	if err := d.WritePID(PIDFile{PID: 42, StartTime: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadPID()
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 42 {
		t.Fatalf("pid: %d", got.PID)
	}
}

func TestDir_AppendEventsLines(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	for i := 0; i < 5; i++ {
		if err := d.AppendEvent([]byte(`{"seq":` + string(rune('0'+i)) + "}")); err != nil {
			t.Fatal(err)
		}
	}
	count, err := d.CountEvents()
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("count: %d", count)
	}
}

func TestDir_CountEventsEmpty(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	count, err := d.CountEvents()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestDir_ExistsAndRemove(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	if !d.Exists() {
		t.Fatal("expected exists")
	}
	if err := d.Remove(); err != nil {
		t.Fatal(err)
	}
	if d.Exists() {
		t.Fatal("expected gone")
	}
}

func TestDir_RequiresArgs(t *testing.T) {
	if _, err := NewDir("", "E"); err == nil {
		t.Fatal("expected root error")
	}
	if _, err := NewDir("/x", ""); err == nil {
		t.Fatal("expected execution_id error")
	}
}

func TestWriteAtomic_BadDirReturnsErr(t *testing.T) {
	// Writing to a path inside a non-existent directory must error.
	if err := writeAtomic("/no/such/dir/file", []byte("x"), 0o644); err == nil {
		t.Fatal("expected error")
	}
}

func TestReadPID_NotFound(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	if _, err := d.ReadPID(); err == nil {
		t.Fatal("expected not found")
	}
}

func TestReadEnvelope_Empty(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	if _, err := d.ReadEnvelope(); err == nil {
		t.Fatal("expected not found")
	}
}

func TestReadStatus_NotFound(t *testing.T) {
	d, _ := NewDir(t.TempDir(), "E-1")
	if _, err := d.ReadStatus(); err == nil {
		t.Fatal("expected not found")
	}
}

func TestWriteAtomic_NoPartialOnDecodeFail(t *testing.T) {
	root := t.TempDir()
	d, _ := NewDir(root, "E-1")
	if err := d.WriteEnvelope([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	// Read should always be either present-with-full-data or not present;
	// verify JSON parses
	got, err := d.ReadEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatal(err)
	}
}
