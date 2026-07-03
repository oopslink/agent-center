package agentruntime

import (
	"encoding/json"
	"testing"
)

// TestCenterTaskDetail_RepoHintDecodes covers the v2.31.0 (issue-9f749a19 Phase 1)
// additive repo-hint projection: a get_task response with a primary repo populates
// centerTaskDetail.Repo + BaseRef; one without leaves Repo nil + BaseRef empty
// (old-center forward-compat), and unknown keys are ignored. (Ported from P1's
// concurrent_work_available_test.go when 0c moved centerTaskDetail into agentruntime.)
func TestCenterTaskDetail_RepoHintDecodes(t *testing.T) {
	t.Run("with primary repo", func(t *testing.T) {
		raw := `{
			"id":"task-9","title":"Fix","status":"open",
			"repo":{"ref_id":"reporef-1","repo_id":"repo-1","label":"app",
				"url":"https://github.com/o/app","provider":"github",
				"default_branch":"main","is_primary":true,"description":"ignored"},
			"base_ref":"main"
		}`
		var d centerTaskDetail
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if d.BaseRef != "main" {
			t.Errorf("BaseRef = %q, want main", d.BaseRef)
		}
		if d.Repo == nil {
			t.Fatal("Repo = nil, want populated")
		}
		if d.Repo.RepoID != "repo-1" || d.Repo.URL != "https://github.com/o/app" ||
			d.Repo.Provider != "github" || d.Repo.DefaultBranch != "main" || !d.Repo.IsPrimary {
			t.Errorf("Repo = %+v", d.Repo)
		}
	})
	t.Run("no repo hint (old center / no primary)", func(t *testing.T) {
		var d centerTaskDetail
		if err := json.Unmarshal([]byte(`{"id":"task-1","title":"x","status":"open"}`), &d); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if d.Repo != nil {
			t.Errorf("Repo = %+v, want nil", d.Repo)
		}
		if d.BaseRef != "" {
			t.Errorf("BaseRef = %q, want empty", d.BaseRef)
		}
	})
}
