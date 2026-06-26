package taskexec

import (
	"fmt"
	"os"
	"path/filepath"
)

const planMetaFile = "plan.json"

// PlanMeta is the plan-level local metadata (design §3: "Plan 级公共信息").
type PlanMeta struct {
	PlanID     string   `json:"plan_id"`
	Notes      string   `json:"notes,omitempty"`
	FailureLog []string `json:"failure_log,omitempty"`
}

// WritePlanMeta persists plan metadata to plans/{plan_id}/plan.json.
func WritePlanMeta(plansDir string, meta PlanMeta) error {
	if err := validatePathComponent("plan_id", meta.PlanID); err != nil {
		return err
	}
	dir := filepath.Join(plansDir, meta.PlanID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("taskexec: mkdir plan %s: %w", meta.PlanID, err)
	}
	return writeJSONAtomic(filepath.Join(dir, planMetaFile), meta)
}

// ReadPlanMeta reads plan metadata from plans/{plan_id}/plan.json.
func ReadPlanMeta(plansDir, planID string) (PlanMeta, error) {
	if err := validatePathComponent("plan_id", planID); err != nil {
		return PlanMeta{}, err
	}
	var meta PlanMeta
	path := filepath.Join(plansDir, planID, planMetaFile)
	if err := readJSON(path, &meta); err != nil {
		return PlanMeta{}, fmt.Errorf("taskexec: read plan %s: %w", planID, err)
	}
	return meta, nil
}

// ListPlanMetas lists all plan metadata in plansDir.
func ListPlanMetas(plansDir string) []PlanMeta {
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		return nil
	}
	var result []PlanMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var meta PlanMeta
		if err := readJSON(filepath.Join(plansDir, e.Name(), planMetaFile), &meta); err != nil {
			continue
		}
		result = append(result, meta)
	}
	return result
}
