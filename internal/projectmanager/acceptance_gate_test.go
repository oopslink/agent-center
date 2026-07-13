package projectmanager

import (
	"testing"
	"time"
)

// mkTask is a tiny constructor for the detection unit tests below.
func mkTask(t *testing.T, title string, tags []string) *Task {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	task, err := NewTask(NewTaskInput{
		ID: "task-1", ProjectID: "proj-1", Title: title, CreatedBy: "user:a", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if len(tags) > 0 {
		if err := task.SetTags(tags, now); err != nil {
			t.Fatalf("SetTags: %v", err)
		}
	}
	return task
}

// TestRequiresAcceptance_Detection is the merge-to-main marker-detection lock
// (issue-d2f14e0e): the plan builder auto-stamps TagMergeToMain on a node iff
// RequiresAcceptance is true. It must catch explicit marker tags and canonical
// "merge … main" titles, and must NOT trip on incidental co-occurrence.
func TestRequiresAcceptance_Detection(t *testing.T) {
	cases := []struct {
		name  string
		title string
		tags  []string
		want  bool
	}{
		// explicit marker tags (authoritative)
		{"tag merge", "do the thing", []string{"merge"}, true},
		{"tag ship", "do the thing", []string{"ship"}, true},
		{"tag merge-to-main", "do the thing", []string{TagMergeToMain}, true},
		{"tag needs-acceptance", "do the thing", []string{"needs-acceptance"}, true},
		{"tag case-insensitive", "do the thing", []string{"Merge"}, true},
		// canonical title phrasing
		{"title merge to main", "Merge to main", nil, true},
		{"title merge-to-main hyphen", "merge-to-main and tag release", nil, true},
		{"title merge into main", "Merge into main branch", nil, true},
		{"title merge to master", "merge to master", nil, true},
		{"title chinese 合并 main", "合并到 main 分支", nil, true},
		// arrow-style ship naming — the repo's MAINSTREAM convention (pd's real-title
		// probes). main/master is the arrow TARGET → must gate.
		{"P67 real title arrow release", "merge(release): dev/team-phase1 → main for v2.43.0", nil, true},
		{"integrate arrow no-space", "[P0安全] 集成 block熔断 feat→main", nil, true},
		{"integrate arrow spaced", "集成 feat/block-fuse-executor → main", nil, true},
		{"ship ascii arrow", "Ship: dev/v2.44.0 -> main", nil, true},
		{"fat arrow to main", "release v3 => main", nil, true},
		{"chinese 合并到 main no space", "合并到main", nil, true},
		// DIRECTION negatives — main is the SOURCE, not the target → must NOT gate.
		{"sync main into dev (main is source)", "merge(integrate): sync origin/main into dev/foo", nil, false},
		{"arrow main to dev (main is source)", "sync main → dev/hotfix", nil, false},
		// NOT a merge node — must stay ungated (zero-regression)
		{"plain dev task", "Implement the feature", nil, false},
		{"incidental main+merge non-adjacent", "explain the mainframe merge process", nil, false},
		{"arrow to mainframe (boundary)", "migrate app → mainframe cluster", nil, false},
		{"review task", "Review the PR", nil, false},
		{"unrelated tag", "do the thing", []string{"urgent"}, false},
		{"main only", "update the main config", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RequiresAcceptance(mkTask(t, tc.title, tc.tags))
			if got != tc.want {
				t.Fatalf("RequiresAcceptance(title=%q tags=%v) = %v, want %v", tc.title, tc.tags, got, tc.want)
			}
		})
	}
	if RequiresAcceptance(nil) {
		t.Fatal("RequiresAcceptance(nil) = true, want false")
	}
}
