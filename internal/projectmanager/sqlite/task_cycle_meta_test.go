package sqlite

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestTaskRepo_CycleMeta_RoundTrip covers the v2.13.0 I18/F2 columns (branch,
// base, skip_merge_check) PLUS the I18/F3 role column: all survive Save+FindByID,
// and an Update of them (including clearing branch + changing role) round-trips.
// Default rows (never stamped) read back as "" / "" / false / "" (no role).
func TestTaskRepo_CycleMeta_RoundTrip(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)

	// A node stamped at create (the scaffold_cycle_plan path).
	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: "T1", ProjectID: "P1", Title: "F1 · Integrate", CreatedBy: "user:a", CreatedAt: t0,
		Branch: "f1-spec", Base: "dev/v2.13.0", SkipMergeCheck: false, Role: pm.CycleRoleIntegrate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := tr.FindByID(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch() != "f1-spec" || got.Base() != "dev/v2.13.0" || got.SkipMergeCheck() || got.Role() != pm.CycleRoleIntegrate {
		t.Fatalf("cycle meta round-trip = branch:%q base:%q skip:%v role:%q, want f1-spec / dev/v2.13.0 / false / integrate",
			got.Branch(), got.Base(), got.SkipMergeCheck(), got.Role())
	}

	// Update path: re-stamp via SetCycleMeta (clear branch, flip skip, change role), re-read.
	if err := got.SetCycleMeta(pm.CycleRoleDev, "", "dev/v2.13.0", true, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := tr.FindByID(ctx, "T1")
	if re.Branch() != "" || re.Base() != "dev/v2.13.0" || !re.SkipMergeCheck() || re.Role() != pm.CycleRoleDev {
		t.Fatalf("updated cycle meta = branch:%q base:%q skip:%v role:%q, want empty / dev/v2.13.0 / true / dev",
			re.Branch(), re.Base(), re.SkipMergeCheck(), re.Role())
	}

	// A default (never-stamped) task reads back empty/false/no-role.
	d, err := pm.NewTask(pm.NewTaskInput{ID: "T2", ProjectID: "P1", Title: "plain", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	got2, _ := tr.FindByID(ctx, "T2")
	if got2.Branch() != "" || got2.Base() != "" || got2.SkipMergeCheck() || got2.Role() != "" {
		t.Fatalf("default task cycle meta = branch:%q base:%q skip:%v role:%q, want all empty/false",
			got2.Branch(), got2.Base(), got2.SkipMergeCheck(), got2.Role())
	}
}
