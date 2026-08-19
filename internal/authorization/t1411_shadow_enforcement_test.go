package authorization

import (
	"context"
	"errors"
	"testing"
)

func TestT1411EnforcementModesRejectLongTermORAllow(t *testing.T) {
	if got := NormalizeEnforcementMode(""); got != EnforcementShadow {
		t.Fatalf("default mode = %q want shadow", got)
	}
	for _, raw := range []string{"or", "dual_allow", "fallback"} {
		if _, err := ParseEnforcementMode(raw); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mode %q err=%v want ErrInvalid", raw, err)
		}
	}
	if got, err := ParseEnforcementMode("enforce"); err != nil || got != EnforcementEnforce {
		t.Fatalf("parse enforce = %q err=%v", got, err)
	}
}

func TestT1411ShadowCompareBuiltinRoleEquivalenceAndEnforce(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-member", "project-1", "user:user-member", "member")
	seedTeam(t, db)

	reqs := []CheckRequest{
		{SubjectRef: "user:user-member", Permission: "org.read", Resource: ResourceScope{Kind: "org", ID: "org-1"}},
		{SubjectRef: "user:user-member", Permission: "project.write", Resource: ResourceScope{Kind: "project", ID: "project-1"}},
		{SubjectRef: "user:user-member", Permission: "team.member.manage", Resource: ResourceScope{Kind: "team", ID: "team-1"}},
		{SubjectRef: "agent:mem-agent", Permission: "team.git.write", Resource: ResourceScope{Kind: "team", ID: "team-1"}},
	}

	for _, req := range reqs {
		if _, err := svc.Check(ctx, req); err != nil {
			t.Fatalf("shadow legacy decision for %s/%s: %v", req.Resource.Kind, req.Permission, err)
		}
	}
	if metrics := svc.ShadowMetrics(); metrics.Checks != int64(len(reqs)) || metrics.Mismatches != 0 {
		t.Fatalf("shadow metrics after equivalent checks = %+v", metrics)
	}

	enforced := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementEnforce})
	for _, req := range reqs {
		if _, err := enforced.Check(ctx, req); err != nil {
			t.Fatalf("enforce equivalent decision for %s/%s: %v", req.Resource.Kind, req.Permission, err)
		}
	}

	legacy := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementLegacy})
	if _, err := legacy.Check(ctx, reqs[0]); err != nil {
		t.Fatalf("legacy rollback decision: %v", err)
	}
	if metrics := legacy.ShadowMetrics(); metrics.Checks != 0 {
		t.Fatalf("legacy mode should not shadow-compare, metrics=%+v", metrics)
	}
}

func TestT1411ShadowMetricsExposeLegacyEquivalentDrift(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedTeam(t, db)
	execMany(t, db, `DELETE FROM authorization_role_permissions WHERE role_id = 'sys-team-web-member' AND permission_key = 'team.member.manage'`)

	req := CheckRequest{
		SubjectRef: "user:user-member",
		Permission: "team.member.manage",
		Resource:   ResourceScope{Kind: "team", ID: "team-1"},
	}
	if _, err := svc.Check(ctx, req); err != nil {
		t.Fatalf("shadow mode must preserve legacy allow despite drift: %v", err)
	}
	metrics := svc.ShadowMetrics()
	if metrics.Checks != 1 || metrics.Mismatches != 1 || metrics.LegacyOnly == 0 {
		t.Fatalf("shadow metrics did not capture drift: %+v", metrics)
	}

	enforced := New(Deps{DB: db, Store: svc.store, IDGen: svc.gen, Clock: svc.clock, Mode: EnforcementEnforce})
	if _, err := enforced.Check(ctx, req); !errors.Is(err, ErrDenied) {
		t.Fatalf("enforce should fail closed on equivalent drift, err=%v", err)
	}
}
