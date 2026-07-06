package taskexec

import (
	"path/filepath"
	"testing"
)

func TestPlanMeta_WriteAndRead(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	meta := PlanMeta{
		PlanID: "plan-abc",
		Notes:  "some lessons learned",
	}
	if err := WritePlanMeta(plansDir, meta); err != nil {
		t.Fatalf("WritePlanMeta: %v", err)
	}
	got, err := ReadPlanMeta(plansDir, "plan-abc")
	if err != nil {
		t.Fatalf("ReadPlanMeta: %v", err)
	}
	if got.PlanID != "plan-abc" || got.Notes != "some lessons learned" {
		t.Errorf("got = %+v", got)
	}
}

func TestPlanMeta_ReadMissing(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	_, err := ReadPlanMeta(plansDir, "nonexistent")
	if err == nil {
		t.Error("expected error for missing plan")
	}
}

func TestListPlanMetas(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	WritePlanMeta(plansDir, PlanMeta{PlanID: "p-1", Notes: "a"})
	WritePlanMeta(plansDir, PlanMeta{PlanID: "p-2", Notes: "b"})
	metas := ListPlanMetas(plansDir)
	if len(metas) != 2 {
		t.Fatalf("ListPlanMetas = %d, want 2", len(metas))
	}
}

func TestWritePlanMeta_EmptyPlanID(t *testing.T) {
	plansDir := t.TempDir()
	meta := PlanMeta{
		PlanID: "",
		Notes:  "test",
	}
	err := WritePlanMeta(plansDir, meta)
	if err == nil {
		t.Error("expected error for empty plan_id")
	}
}

func TestWritePlanMeta_WithFailureLog(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	meta := PlanMeta{
		PlanID:     "plan-with-log",
		Notes:      "failed execution",
		FailureLog: []string{"error 1", "error 2"},
	}
	if err := WritePlanMeta(plansDir, meta); err != nil {
		t.Fatalf("WritePlanMeta: %v", err)
	}
	got, err := ReadPlanMeta(plansDir, "plan-with-log")
	if err != nil {
		t.Fatalf("ReadPlanMeta: %v", err)
	}
	if len(got.FailureLog) != 2 || got.FailureLog[0] != "error 1" {
		t.Errorf("FailureLog not preserved: %v", got.FailureLog)
	}
}

func TestListPlanMetas_Empty(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	metas := ListPlanMetas(plansDir)
	if metas != nil && len(metas) != 0 {
		t.Errorf("ListPlanMetas on empty dir should be empty or nil, got %v", metas)
	}
}

func TestListPlanMetas_NonexistentDir(t *testing.T) {
	metas := ListPlanMetas("/nonexistent/dir/path")
	if metas != nil && len(metas) != 0 {
		t.Errorf("ListPlanMetas on nonexistent dir should be empty or nil, got %v", metas)
	}
}

func TestWritePlanMeta_Update(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	// Write initial
	meta := PlanMeta{
		PlanID: "plan-update",
		Notes:  "initial",
	}
	if err := WritePlanMeta(plansDir, meta); err != nil {
		t.Fatalf("WritePlanMeta: %v", err)
	}
	// Update
	meta.Notes = "updated"
	meta.FailureLog = []string{"new error"}
	if err := WritePlanMeta(plansDir, meta); err != nil {
		t.Fatalf("WritePlanMeta update: %v", err)
	}
	got, err := ReadPlanMeta(plansDir, "plan-update")
	if err != nil {
		t.Fatalf("ReadPlanMeta: %v", err)
	}
	if got.Notes != "updated" || len(got.FailureLog) != 1 {
		t.Errorf("Update failed: got %+v", got)
	}
}

func TestListPlanMetas_SkipsMalformedFiles(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	// Write valid plan
	WritePlanMeta(plansDir, PlanMeta{PlanID: "valid-plan", Notes: "good"})
	// Write malformed plan by creating a dir and bad JSON
	badDir := filepath.Join(plansDir, "bad-plan")
	if err := mkdirAll(badDir); err != nil {
		t.Fatalf("mkdirAll: %v", err)
	}
	if err := writeJSONAtomic(filepath.Join(badDir, planMetaFile), "not valid json"); err != nil {
		t.Fatalf("write: %v", err)
	}
	metas := ListPlanMetas(plansDir)
	if len(metas) != 1 {
		t.Fatalf("ListPlanMetas should skip malformed files, got %d", len(metas))
	}
	if metas[0].PlanID != "valid-plan" {
		t.Errorf("Expected valid-plan, got %s", metas[0].PlanID)
	}
}
