package authorization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
)

func TestRegistryAndValueObjects(t *testing.T) {
	defs := Definitions()
	if len(defs) < 50 {
		t.Fatalf("definitions = %d, want complete registry", len(defs))
	}
	defs[0].Key = "mutated"
	if got, _ := Definition("org.read"); got.Key != "org.read" {
		t.Fatal("Definitions returned registry backing storage")
	}
	if _, ok := Definition("missing"); ok || PermissionDefinedForResource("org.read", "project") {
		t.Fatal("undefined or inapplicable permission accepted")
	}
	for scope, want := range map[string]PermissionKey{
		"admin:token": "admin_token.manage", "secret:resolve": "secret.resolve", "blob:put": "blob.put",
		"dispatch:pull": "dispatch.pull", "task:*": "task.internal.report", "workforce:enroll": "worker.enroll",
	} {
		if got, ok := PermissionForBearerScope(scope); !ok || got != want {
			t.Fatalf("scope %q = %q,%v want %q,true", scope, got, ok, want)
		}
	}
	if _, ok := PermissionForBearerScope("unknown"); ok {
		t.Fatal("unknown bearer scope mapped")
	}
	if !BearerScopeAllows([]string{"task:*"}, "task:report") || !BearerScopeAllows([]string{"*"}, "anything") || BearerScopeAllows([]string{"blob:put"}, "secret:resolve") {
		t.Fatal("bearer wildcard/exact applicability mismatch")
	}
	for _, ref := range []SubjectRef{"system", "user:u", "agent:a", "worker:w"} {
		if err := ref.Validate(); err != nil {
			t.Fatalf("%q rejected: %v", ref, err)
		}
	}
	for _, ref := range []SubjectRef{"", "user:", "team:t"} {
		if !errors.Is(ref.Validate(), ErrInvalid) {
			t.Fatalf("%q accepted", ref)
		}
	}
	if UserSubject(" u ") != "user:u" || AgentSubject(" a ") != "agent:a" || WorkerSubject(" w ") != "worker:w" {
		t.Fatal("subject constructors did not normalize IDs")
	}
	if WorkerSubject("w").BareID() != "w" || SubjectRef("system").BareID() != "system" || !WorkerSubject("w").IsWorker() {
		t.Fatal("subject helpers mismatch")
	}
	if k, id := (ResourceScope{Kind: "file", URI: " ac://f "}).Key(); k != "file" || id != "ac://f" {
		t.Fatalf("file key = %q/%q", k, id)
	}
}

func TestServiceEffectiveAcrossResourceKinds(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-owner", "project-1", "user:user-owner", "owner")
	seedTeam(t, db)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db, `INSERT INTO pm_tasks (id, project_id, title, status, assignee, created_by, created_at, updated_at) VALUES ('task-1','project-1','T','running','agent:mem-agent','user:user-owner',?,?)`, now, now)
	execMany(t, db, `INSERT INTO pm_issues (id, project_id, title, status, created_by, created_at, updated_at) VALUES ('issue-1','project-1','I','open','user:user-owner',?,?)`, now, now)
	execMany(t, db, `INSERT INTO pm_plans (id, project_id, name, status, creator_ref, created_at, updated_at) VALUES ('plan-1','project-1','P','active','user:user-owner',?,?)`, now, now)
	execMany(t, db, `INSERT INTO conversations (id, kind, status, opened_at, created_at, updated_at, organization_id, participants) VALUES ('conv-1','dm','open',?,?,?,'org-1','[{"identity_id":"user:user-owner"},{"identity_id":"user:user-admin","left_at":"2026-01-01T00:00:00Z"}]')`, now, now, now)
	execMany(t, db, `INSERT INTO agents (id, organization_id, name, worker_id, lifecycle, created_by, identity_member_id, created_at, updated_at) VALUES ('agent-runtime','org-1','A','worker-1','running','system','mem-agent',?,?)`, now, now)
	for id, scope := range map[string]FileRef{
		"f-uploader": {Scope: "uploader", ScopeID: "user:user-owner"},
		"f-project":  {Scope: "project", ScopeID: "project-1"},
		"f-task":     {Scope: "task", ScopeID: "task-1"},
		"f-issue":    {Scope: "issue", ScopeID: "issue-1"},
		"f-conv":     {Scope: "conversation", ScopeID: "conv-1"},
	} {
		execMany(t, db, `INSERT INTO file_references (id,file_uri,scope,scope_id,created_by,created_at) VALUES (?, ?, ?, ?, 'system', ?)`, "ref-"+id, "ac://"+id, scope.Scope, scope.ScopeID, now)
	}

	checks := []CheckRequest{
		{SubjectRef: "agent:mem-agent", Permission: "task.complete.self", Resource: ResourceScope{Kind: "task", ID: "task-1"}},
		{SubjectRef: "user:user-owner", Permission: "task.write", Resource: ResourceScope{Kind: "task", ID: "task-1"}},
		{SubjectRef: "user:user-owner", Permission: "issue.write", Resource: ResourceScope{Kind: "issue", ID: "issue-1"}},
		{SubjectRef: "user:user-owner", Permission: "plan.read", Resource: ResourceScope{Kind: "plan", ID: "plan-1"}},
		{SubjectRef: "user:user-owner", Permission: "conversation.post", Resource: ResourceScope{Kind: "conversation", ID: "conv-1"}},
		{SubjectRef: "worker:worker-1", Permission: "agent.operate.self", Resource: ResourceScope{Kind: "agent", ID: "agent-runtime"}},
		{SubjectRef: "agent:mem-agent", Permission: "git.agent.write.self", Resource: ResourceScope{Kind: "agent", ID: "agent-runtime"}},
		{SubjectRef: "worker:worker-1", Permission: "worker.heartbeat", Resource: ResourceScope{Kind: "worker", ID: "worker-1"}},
		{SubjectRef: "user:user-owner", Permission: "git.global.read", Resource: ResourceScope{Kind: "git"}},
	}
	for _, req := range checks {
		if decision, err := svc.Check(ctx, req); err != nil || !decision.Allowed {
			t.Errorf("%s on %s denied: %#v %v", req.Permission, req.Resource.Kind, decision, err)
		}
	}
	for _, uri := range []string{"ac://f-uploader", "ac://f-project", "ac://f-task", "ac://f-issue", "ac://f-conv"} {
		if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-owner", Permission: "file.download", Resource: ResourceScope{Kind: "file", URI: uri}}); err != nil {
			t.Errorf("reachable file %s denied: %v", uri, err)
		}
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-admin", Permission: "conversation.read", Resource: ResourceScope{Kind: "conversation", ID: "conv-1"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("left participant err=%v, want denied", err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "worker:other", Permission: "agent.operate.self", Resource: ResourceScope{Kind: "agent", ID: "agent-runtime"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-worker err=%v, want denied", err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "worker:other", Permission: "worker.heartbeat", Resource: ResourceScope{Kind: "worker", ID: "worker-1"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("worker owner mismatch err=%v, want denied", err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-owner", Permission: "file.download", Resource: ResourceScope{Kind: "file", URI: "ac://unknown", Refs: []FileRef{{Scope: "unknown", ScopeID: "x"}}}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("unknown file scope err=%v, want denied", err)
	}
	eff, err := svc.ListEffective(ctx, "user:user-owner", ResourceScope{Kind: "project", ID: "project-1"})
	if err != nil || len(eff.Permissions) == 0 {
		t.Fatalf("ListEffective = %#v, %v", eff, err)
	}
	if _, err := svc.ListEffective(ctx, "user:user-owner", ResourceScope{Kind: "project", ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListEffective missing project err=%v", err)
	}
	for _, resource := range []ResourceScope{
		{Kind: "task", ID: "task-1", OrgID: "org-2"},
		{Kind: "issue", ID: "issue-1", OrgID: "org-2"},
		{Kind: "plan", ID: "plan-1", OrgID: "org-2"},
		{Kind: "conversation", ID: "conv-1", OrgID: "org-2"},
		{Kind: "agent", ID: "agent-runtime", OrgID: "org-2"},
	} {
		if _, _, err := svc.resolveResource(ctx, resource); err == nil {
			t.Errorf("cross-org %s resource resolved", resource.Kind)
		}
	}
}

func TestServiceValidationAndResourceConcealment(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedTeam(t, db)
	tests := []struct {
		name string
		req  CheckRequest
		want error
	}{
		{"invalid subject", CheckRequest{SubjectRef: "bad", Permission: "org.read", Resource: ResourceScope{Kind: "org", ID: "org-1"}}, ErrInvalid},
		{"missing permission", CheckRequest{SubjectRef: "user:user-owner", Resource: ResourceScope{Kind: "org", ID: "org-1"}}, ErrInvalid},
		{"missing kind", CheckRequest{SubjectRef: "user:user-owner", Permission: "org.read"}, ErrInvalid},
		{"missing org", CheckRequest{SubjectRef: "user:user-owner", Permission: "org.read", Resource: ResourceScope{Kind: "org"}}, ErrInvalid},
		{"unknown org", CheckRequest{SubjectRef: "user:user-owner", Permission: "org.read", Resource: ResourceScope{Kind: "org", ID: "missing"}}, ErrNotFound},
		{"unknown project", CheckRequest{SubjectRef: "user:user-owner", Permission: "project.read", Resource: ResourceScope{Kind: "project", ID: "missing"}}, ErrNotFound},
		{"cross org team", CheckRequest{SubjectRef: "user:user-owner", Permission: "team.read", Resource: ResourceScope{Kind: "team", ID: "team-1", OrgID: "org-2"}}, ErrNotFound},
		{"unknown kind", CheckRequest{SubjectRef: "user:user-owner", Permission: "org.read", Resource: ResourceScope{Kind: "nope", ID: "x"}}, ErrInvalid},
		{"inapplicable", CheckRequest{SubjectRef: "user:user-owner", Permission: "org.read", Resource: ResourceScope{Kind: "project", ID: "project-1"}}, ErrPermissionUndefined},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Check(ctx, tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
	if d, err := svc.Check(ctx, CheckRequest{SubjectRef: "system", Permission: "anything", Resource: ResourceScope{Kind: "nope"}}); err != nil || !d.Allowed || d.Source != SourceSystem {
		t.Fatalf("system decision=%#v err=%v", d, err)
	}
	var nilSvc *Service
	if _, err := nilSvc.Check(ctx, CheckRequest{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("nil service err=%v", err)
	}
	if defs, err := New(Deps{}).ListDefinitions(ctx); err != nil || len(defs) == 0 {
		t.Fatalf("unwired registry fallback defs=%d err=%v", len(defs), err)
	}
}

func TestBatchPreviewRollbackIdempotencyAndStoreFailures(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	previewReq := BatchRequest{ActorRef: "user:user-owner", OrgID: "org-1", Operations: []BatchOperation{{Type: "upsert_role", Role: RoleInput{ID: "role-preview", Name: "preview"}}}}
	preview, err := svc.PreviewBatch(ctx, previewReq)
	if err != nil || preview.Operations[0].Status != "would_create" {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_roles WHERE id='role-preview'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("preview persisted role count=%d err=%v", count, err)
	}
	rollback := BatchRequest{IdempotencyKey: "rollback", ActorRef: "user:user-owner", OrgID: "org-1", Operations: []BatchOperation{
		{Type: "upsert_role", Role: RoleInput{ID: "role-rollback", Name: "rollback"}},
		{Type: "unknown"},
	}}
	if _, err := svc.ApplyBatch(ctx, rollback); !errors.Is(err, ErrInvalid) {
		t.Fatalf("rollback batch err=%v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM authorization_roles WHERE id='role-rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed batch left role count=%d err=%v", count, err)
	}
	if _, err := svc.ApplyBatch(ctx, BatchRequest{ActorRef: "user:user-owner", OrgID: "org-1"}); !errors.Is(err, ErrIdempotencyRequired) {
		t.Fatalf("missing idempotency err=%v", err)
	}
	good := BatchRequest{IdempotencyKey: "same-key", ActorRef: "user:user-owner", OrgID: "org-1", Operations: []BatchOperation{{Type: "upsert_role", Role: RoleInput{ID: "role-one", Name: "one"}}}}
	if _, err := svc.ApplyBatch(ctx, good); err != nil {
		t.Fatal(err)
	}
	good.Operations[0].Role = RoleInput{ID: "role-two", Name: "two"}
	if _, err := svc.ApplyBatch(ctx, good); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict err=%v", err)
	}
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "invalid-actor", ActorRef: "bad", OrgID: "org-1"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid actor err=%v", err)
	}
	if _, err := svc.PreviewBatch(ctx, BatchRequest{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid preview err=%v", err)
	}
	if _, err := NewStore(nil).ListDefinitions(ctx); err == nil {
		t.Fatal("nil store unexpectedly succeeded")
	}
}

func TestAssignmentSafetyHighRiskAgentAndLastOwner(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)

	// System owner permissions are delegatable by the legacy owner, but must
	// never be used to turn an execution Agent into an organization owner.
	_, err := svc.ApplyBatch(ctx, BatchRequest{
		IdempotencyKey: "agent-owner-escalation", ActorRef: "user:user-owner", OrgID: "org-1",
		Operations: []BatchOperation{{Type: "assign_role", Assignment: AssignmentInput{
			ID: "asgn-agent-owner", SubjectRef: "agent:mem-agent", RoleID: "sys-org-owner",
			Resource: ResourceScope{Kind: "org", ID: "org-1"},
		}}},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("high-risk Agent grant err=%v, want invalid", err)
	}
	expired := time.Date(2026, 8, 14, 11, 0, 0, 0, time.UTC)
	execMany(t, db, `INSERT INTO authorization_roles (id,org_id,kind,name,created_by,created_at,updated_at) VALUES ('role-expired','org-1','custom','expired','system',?,?)`, expired.Format(time.RFC3339Nano), expired.Format(time.RFC3339Nano))
	execMany(t, db, `INSERT INTO authorization_role_permissions (role_id,permission_key,resource_kind,delegatable,created_at) VALUES ('role-expired','org.settings.manage','org',0,?)`, expired.Format(time.RFC3339Nano))
	if _, _, err := svc.store.assignRole(ctx, RoleAssignment{ID: "asgn-expired", OrgID: "org-1", SubjectRef: "user:user-member", RoleID: "role-expired", ResourceKind: "org", ResourceID: "org-1", CreatedBy: "system", ExpiresAt: &expired}, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "org.settings.manage", Resource: ResourceScope{Kind: "org", ID: "org-1"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired assignment err=%v, want denied", err)
	}

	// Cross-org and non-member subjects fail closed before any assignment row.
	for name, input := range map[string]AssignmentInput{
		"cross-org resource": {ID: "asgn-cross", SubjectRef: "user:user-owner", RoleID: "sys-org-member", Resource: ResourceScope{Kind: "org", ID: "org-2"}},
		"unknown subject":    {ID: "asgn-unknown", SubjectRef: "user:unknown", RoleID: "sys-org-member", Resource: ResourceScope{Kind: "org", ID: "org-1"}},
		"worker subject":     {ID: "asgn-worker", SubjectRef: "worker:w", RoleID: "sys-org-member", Resource: ResourceScope{Kind: "org", ID: "org-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "safety-" + name, ActorRef: "user:user-owner", OrgID: "org-1", Operations: []BatchOperation{{Type: "assign_role", Assignment: input}}})
			if err == nil {
				t.Fatal("unsafe assignment succeeded")
			}
		})
	}

	// Model the RBAC data-core case with no legacy owner and one explicit owner.
	if _, err := db.ExecContext(ctx, `UPDATE members SET role='member' WHERE organization_id='org-1' AND role='owner'`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	execMany(t, db, `INSERT INTO authorization_role_assignments
		(id,org_id,subject_ref,role_id,resource_kind,resource_id,created_by,created_at)
		VALUES ('asgn-last-owner','org-1','user:user-owner','sys-org-owner','org','org-1','system',?)`, now)
	_, err = svc.RevokeBatch(ctx, BatchRequest{IdempotencyKey: "last-owner", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Revoke: RevokeInput{AssignmentID: "asgn-last-owner"}}}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("last owner revoke err=%v, want conflict", err)
	}
}

func TestStoreRoleAssignmentLifecycleAndScopedRevoke(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	s := svc.store
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	defs, err := svc.ListDefinitions(ctx)
	if err != nil || len(defs) < 50 {
		t.Fatalf("ListDefinitions=%d err=%v", len(defs), err)
	}
	role, status, err := s.upsertCustomRole(ctx, Role{OrgID: "org-1", Name: "auditor", Description: "v1", CreatedBy: "user:user-owner"}, now)
	if err != nil || status != "created" || role.ID == "" {
		t.Fatalf("create role=%#v status=%s err=%v", role, status, err)
	}
	found, err := s.findCustomRoleByName(ctx, "org-1", "auditor")
	if err != nil || found.ID != role.ID {
		t.Fatalf("find role=%#v err=%v", found, err)
	}
	role.Description = "v2"
	updated, status, err := s.upsertCustomRole(ctx, role, now.Add(time.Minute))
	if err != nil || status != "updated" || updated.Version <= role.Version || updated.Description != "v2" {
		t.Fatalf("update role=%#v status=%s err=%v", updated, status, err)
	}
	if err := s.replaceRolePermissions(ctx, role.ID, []RolePermissionInput{
		{PermissionKey: "org.read", ResourceKind: "org", Delegatable: true},
		{PermissionKey: "org.read", ResourceKind: "org", Delegatable: true},
	}, now); err != nil {
		t.Fatal(err)
	}
	perms, err := s.rolePermissions(ctx, role.ID)
	if err != nil || len(perms) != 1 || !perms[0].Delegatable {
		t.Fatalf("permissions=%#v err=%v", perms, err)
	}
	expires := now.Add(24 * time.Hour)
	assignment := RoleAssignment{OrgID: "org-1", SubjectRef: "user:user-member", RoleID: role.ID, ResourceKind: "org", ResourceID: "org-1", CreatedBy: "user:user-owner", ExpiresAt: &expires}
	a, status, err := s.assignRole(ctx, assignment, now)
	if err != nil || status != "created" || a.ID == "" || a.ExpiresAt == nil || !a.ExpiresAt.Equal(expires) {
		t.Fatalf("assign=%#v status=%s err=%v", a, status, err)
	}
	_, status, err = s.assignRole(ctx, assignment, now)
	if err != nil || status != "unchanged" {
		t.Fatalf("duplicate assignment status=%s err=%v", status, err)
	}
	foundAssignment, err := s.findActiveAssignment(ctx, "org-1", assignment.SubjectRef, role.ID, "org", "org-1")
	if err != nil || foundAssignment.ID != a.ID {
		t.Fatalf("find assignment=%#v err=%v", foundAssignment, err)
	}
	revoked, status, err := s.revokeAssignment(ctx, RevokeInput{SubjectRef: assignment.SubjectRef, RoleID: role.ID, Resource: ResourceScope{Kind: "org", ID: "org-1"}, Reason: "done"}, "user:user-owner", "org-1", now.Add(time.Hour))
	if err != nil || status != "revoked" || revoked.RevokedAt == nil || revoked.RevokedReason != "done" {
		t.Fatalf("revoke=%#v status=%s err=%v", revoked, status, err)
	}
	if _, _, err := s.revokeAssignment(ctx, RevokeInput{AssignmentID: a.ID}, "user:user-owner", "org-2", now); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("cross-org store revoke err=%v", err)
	}
	if got := parseDBTime("2026-08-14 12:00:00"); got.IsZero() {
		t.Fatal("legacy DB time did not parse")
	}
	if got := parseDBTime("invalid"); !got.IsZero() {
		t.Fatal("invalid DB time parsed")
	}
	if shortHash("stable") != shortHash("stable") || len(shortHash("stable")) != 8 {
		t.Fatal("shortHash is not stable 8-char digest")
	}
}

func TestBatchOperationValidationAndPreviewApplyConsistency(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	req := BatchRequest{ActorRef: "user:user-owner", OrgID: "org-1", Operations: []BatchOperation{
		{ID: "role", Type: "upsert_role", Role: RoleInput{ID: "role-consistent", Name: "consistent"}},
		{ID: "perms", Type: "set_role_permissions", Role: RoleInput{ID: "role-consistent"}, Permissions: []RolePermissionInput{{PermissionKey: "org.read", ResourceKind: "org"}}},
	}}
	preview, err := svc.PreviewBatch(ctx, req)
	if err != nil || preview.Operations[0].RoleID != "role-consistent" || preview.Operations[1].RoleID != "role-consistent" {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	req.IdempotencyKey = "consistent-apply"
	applied, err := svc.ApplyBatch(ctx, req)
	if err != nil || applied.Operations[0].RoleID != preview.Operations[0].RoleID || applied.Operations[1].RoleID != preview.Operations[1].RoleID {
		t.Fatalf("apply=%#v preview=%#v err=%v", applied, preview, err)
	}

	bad := []BatchOperation{
		{Type: "set_role_permissions"},
		{Type: "set_role_permissions", Role: RoleInput{ID: "role-consistent"}, Permissions: []RolePermissionInput{{PermissionKey: "missing", ResourceKind: "org"}}},
		{Type: "set_role_permissions", Role: RoleInput{ID: "sys-org-owner"}, Permissions: []RolePermissionInput{{PermissionKey: "org.read", ResourceKind: "org"}}},
		{Type: "assign_role", Assignment: AssignmentInput{RoleID: "missing", SubjectRef: "user:user-member", Resource: ResourceScope{Kind: "org", ID: "org-1"}}},
		{Type: "assign_role", Assignment: AssignmentInput{RoleID: "role-consistent", SubjectRef: "user:user-member"}},
		{Type: "assign_role", Assignment: AssignmentInput{RoleID: "role-consistent", SubjectRef: "bad", Resource: ResourceScope{Kind: "org", ID: "org-1"}}},
		{Type: "revoke_assignment", Revoke: RevokeInput{}},
	}
	for i, op := range bad {
		res, err := svc.PreviewBatch(ctx, BatchRequest{ActorRef: "user:user-owner", OrgID: "org-1", Operations: []BatchOperation{op}})
		if err != nil || len(res.Operations) != 1 || res.Operations[0].Status != "denied" {
			t.Errorf("bad operation %d result=%#v err=%v", i, res, err)
		}
	}
}

func TestAuditWritesLedgerAndDomainEventRefs(t *testing.T) {
	ctx := context.Background()
	db, base := newAuthzTestService(t)
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGeneratorWithReader(fc, idgen.DeterministicReader(11))
	sink := observability.NewEventSink(er, er, gen, fc)
	svc := New(Deps{DB: db, IDGen: gen, Clock: fc, EventSink: sink})
	for _, event := range []auditEvent{
		{EventType: "authorization.test.team", ActorRef: "system", ResourceKind: "team", ResourceID: "team-1"},
		{EventType: "authorization.test.project", ActorRef: "system", SubjectRef: "user:u", PermissionKey: "project.read", ResourceKind: "project", ResourceID: "project-1", RoleID: "role-1", AssignmentID: "asgn-1", Payload: map[string]any{"status": "tested"}},
	} {
		if err := svc.audit(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	var ledger, events int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_audit_events WHERE event_type LIKE 'authorization.test.%'`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE event_type LIKE 'authorization.test.%'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if ledger != 2 || events != 2 {
		t.Fatalf("ledger=%d events=%d", ledger, events)
	}
	if base == nil {
		t.Fatal("test setup returned nil service")
	}
}

func TestClosedDatabaseFailsClosedAcrossStoreAndResolvers(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := svc.store
	now := time.Now()
	checks := []func() error{
		func() error { _, err := s.ListDefinitions(ctx); return err },
		func() error { _, err := s.getRole(ctx, "x"); return err },
		func() error { _, err := s.findCustomRoleByName(ctx, "o", "x"); return err },
		func() error {
			_, _, err := s.upsertCustomRole(ctx, Role{OrgID: "o", Name: "x", CreatedBy: "system"}, now)
			return err
		},
		func() error { return s.replaceRolePermissions(ctx, "x", nil, now) },
		func() error { _, err := s.rolePermissions(ctx, "x"); return err },
		func() error {
			_, _, err := s.assignRole(ctx, RoleAssignment{OrgID: "o", SubjectRef: "user:u", RoleID: "r", ResourceKind: "org", ResourceID: "o", CreatedBy: "system"}, now)
			return err
		},
		func() error { _, err := s.getAssignmentInOrg(ctx, "o", "x"); return err },
		func() error { _, err := s.findActiveAssignment(ctx, "o", "user:u", "r", "org", "o"); return err },
		func() error { _, err := s.activeAssignmentsFor(ctx, "o", "user:u", "org", "o"); return err },
		func() error {
			_, _, err := s.revokeAssignment(ctx, RevokeInput{AssignmentID: "x"}, "system", "o", now)
			return err
		},
		func() error { _, _, err := s.beginIdempotency(ctx, "k", "system", "apply", "h", now); return err },
		func() error { return s.completeIdempotency(ctx, "k", []byte("{}"), now) },
		func() error {
			return s.appendAudit(ctx, auditEvent{ID: "e", EventType: "authorization.test", ActorRef: "system", CreatedAt: now})
		},
		func() error { return svc.ensureOrg(ctx, "o") },
		func() error { _, err := svc.projectOrg(ctx, "p"); return err },
		func() error { _, err := svc.parentProject(ctx, "task", "t"); return err },
		func() error { _, err := svc.teamOrg(ctx, "t"); return err },
		func() error { _, err := svc.conversationOrg(ctx, "c"); return err },
		func() error { _, _, err := svc.agentOrg(ctx, "a"); return err },
	}
	for i, check := range checks {
		if err := check(); err == nil {
			t.Errorf("closed database check %d unexpectedly succeeded", i)
		}
	}
}

func TestRemainingAuthorizationBranchesAreBehavioral(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedProjectMember(t, db, "pm-agent-owner", "project-1", "agent:mem-agent", "owner")
	seedProjectMember(t, db, "pm-admin-member", "project-1", "user:user-admin", "member")
	seedTeam(t, db)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db, `INSERT INTO pm_tasks (id,project_id,title,status,created_by,created_at,updated_at) VALUES ('task-created','project-1','T','open','user:user-member',?,?)`, now, now)
	execMany(t, db, `INSERT INTO conversations (id,kind,status,opened_at,created_at,updated_at,organization_id,participants) VALUES ('conv-bad','dm','open',?,?,?,'org-1','not-json')`, now, now, now)

	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "task.read", Resource: ResourceScope{Kind: "task", ID: "task-created"}}); err != nil {
		t.Fatalf("task creator should read: %v", err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-member", Permission: "task.read", Resource: ResourceScope{Kind: "task", ID: "missing"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing task err=%v", err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "agent:other", Permission: "team.memory.read", Resource: ResourceScope{Kind: "team", ID: "team-1"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("non-team agent err=%v", err)
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-owner", Permission: "conversation.read", Resource: ResourceScope{Kind: "conversation", ID: "conv-bad"}}); err == nil {
		t.Fatal("invalid participant projection did not fail closed")
	}
	if _, err := svc.Check(ctx, CheckRequest{SubjectRef: "user:user-owner", Permission: "file.download", Resource: ResourceScope{Kind: "file", URI: "ac://explicit", Refs: []FileRef{{Scope: "project", ScopeID: "project-1"}}}}); !errors.Is(err, ErrDenied) {
		// owner is not a project member in this fixture, so an explicit project
		// ref must not inherit mere org ownership.
		t.Fatalf("non-project member file err=%v, want denied", err)
	}
	for _, req := range []CheckRequest{
		{SubjectRef: "user:user-owner", Permission: "team.read", Resource: ResourceScope{Kind: "team", ID: "missing"}},
		{SubjectRef: "user:user-owner", Permission: "conversation.read", Resource: ResourceScope{Kind: "conversation", ID: "missing"}},
		{SubjectRef: "worker:w", Permission: "agent.operate.self", Resource: ResourceScope{Kind: "agent", ID: "missing"}},
		{SubjectRef: "user:user-owner", Permission: "file.download", Resource: ResourceScope{Kind: "file"}},
	} {
		if _, err := svc.Check(ctx, req); err == nil {
			t.Errorf("missing/invalid %s resource unexpectedly succeeded", req.Resource.Kind)
		}
	}

	// A project member whose source permission is non-delegatable cannot pass
	// that same system role onward.
	_, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "nondelegatable", ActorRef: "user:user-admin", OrgID: "org-1", Operations: []BatchOperation{{Type: "assign_role", Assignment: AssignmentInput{SubjectRef: "user:user-member", RoleID: "sys-project-member", Resource: ResourceScope{Kind: "project", ID: "project-1"}}}}})
	if !errors.Is(err, ErrNotDelegatable) {
		t.Fatalf("nondelegatable grant err=%v", err)
	}

	// A delegatable project owner source may revoke within that exact project,
	// even though the actor is not an organization owner.
	execMany(t, db, `INSERT INTO authorization_role_assignments (id,org_id,subject_ref,role_id,resource_kind,resource_id,created_by,created_at) VALUES ('asgn-project-owner','org-1','user:user-member','sys-project-owner','project','project-1','system',?)`, now)
	result, err := svc.RevokeBatch(ctx, BatchRequest{IdempotencyKey: "delegated-revoke", ActorRef: "agent:mem-agent", OrgID: "org-1", Operations: []BatchOperation{{Revoke: RevokeInput{SubjectRef: "user:user-member", RoleID: "sys-project-owner", Resource: ResourceScope{Kind: "project", ID: "project-1"}}}}})
	if err != nil || result.Operations[0].Status != "revoked" {
		t.Fatalf("delegated revoke=%#v err=%v", result, err)
	}
	execMany(t, db, `INSERT INTO authorization_role_assignments (id,org_id,subject_ref,role_id,resource_kind,resource_id,created_by,created_at) VALUES ('asgn-project-by-id','org-1','user:user-member','sys-project-owner','project','project-1','system',?)`, now)
	result, err = svc.RevokeBatch(ctx, BatchRequest{IdempotencyKey: "delegated-revoke-by-id", ActorRef: "agent:mem-agent", OrgID: "org-1", Operations: []BatchOperation{{Revoke: RevokeInput{AssignmentID: "asgn-project-by-id"}}}})
	if err != nil || result.Operations[0].Status != "revoked" {
		t.Fatalf("delegated revoke by id=%#v err=%v", result, err)
	}

	wants := map[string]string{"created": "would_create", "updated": "would_update", "set": "would_set", "revoked": "would_revoke", "unchanged": "would_leave_unchanged", "custom": "would_custom"}
	for input, want := range wants {
		if got := previewStatus(input); got != want {
			t.Errorf("previewStatus(%q)=%q want %q", input, got, want)
		}
	}
}

func TestStoreInvariantErrorsAndReplayPendingState(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	s := svc.store
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	if _, _, err := s.upsertCustomRole(ctx, Role{}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty custom role err=%v", err)
	}
	if _, _, err := s.upsertCustomRole(ctx, Role{ID: "sys-org-owner", OrgID: "org-1", Name: "overwrite", CreatedBy: "system"}, now); !errors.Is(err, ErrSystemRoleImmutable) {
		t.Fatalf("system role overwrite err=%v", err)
	}
	role, _, err := s.upsertCustomRole(ctx, Role{ID: "role-org-one", OrgID: "org-1", Name: "one", CreatedBy: "system"}, now)
	if err != nil {
		t.Fatal(err)
	}
	role.OrgID = "org-2"
	if _, _, err := s.upsertCustomRole(ctx, role, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("role org move err=%v", err)
	}
	if err := s.replaceRolePermissions(ctx, "sys-org-owner", nil, now); !errors.Is(err, ErrSystemRoleImmutable) {
		t.Fatalf("system permission replacement err=%v", err)
	}
	if err := s.replaceRolePermissions(ctx, "role-org-one", []RolePermissionInput{{PermissionKey: "", ResourceKind: "org"}}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty permission err=%v", err)
	}
	if _, _, err := s.assignRole(ctx, RoleAssignment{}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty assignment err=%v", err)
	}
	if _, _, err := s.beginIdempotency(ctx, "", "system", "apply", "hash", now); !errors.Is(err, ErrIdempotencyRequired) {
		t.Fatalf("empty store idempotency err=%v", err)
	}
	if _, _, err := s.beginIdempotency(ctx, "pending", "system", "apply", "hash", now); err != nil {
		t.Fatal(err)
	}
	if _, replay, err := s.beginIdempotency(ctx, "pending", "system", "apply", "hash", now); !replay || !errors.Is(err, ErrConflict) {
		t.Fatalf("pending replay=%v err=%v", replay, err)
	}
	if err := s.appendAudit(ctx, auditEvent{ID: "bad-payload", EventType: "authorization.test", ActorRef: "system", Payload: map[string]any{"bad": make(chan int)}, CreatedAt: now}); err == nil {
		t.Fatal("unmarshalable audit payload succeeded")
	}
}

func TestSystemBatchDefaultsDelegationValidationAndOwnerSuccession(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)

	created, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "system-defaults", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{
		{Type: "upsert_role", Role: RoleInput{Name: "generated"}},
	}})
	if err != nil || created.Operations[0].RoleID == "" {
		t.Fatalf("system default role=%#v err=%v", created, err)
	}
	roleID := created.Operations[0].RoleID
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "system-perms", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Type: "set_role_permissions", Assignment: AssignmentInput{RoleID: roleID}, Permissions: []RolePermissionInput{{PermissionKey: "org.read", ResourceKind: "org"}}}}}); err != nil {
		t.Fatal(err)
	}
	assigned, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "system-assign", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Type: "assign_role", Assignment: AssignmentInput{SubjectRef: "user:user-member", RoleID: roleID, Resource: ResourceScope{Kind: "org", ID: "org-1"}}}}})
	if err != nil || assigned.Operations[0].AssignmentID == "" {
		t.Fatalf("system assignment=%#v err=%v", assigned, err)
	}
	// The owner now has org.read from both the legacy role and the custom role;
	// effective/explain must preserve both evidence sources deterministically.
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "system-assign-owner", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Type: "assign_role", Assignment: AssignmentInput{SubjectRef: "user:user-owner", RoleID: roleID, Resource: ResourceScope{Kind: "org", ID: "org-1"}}}}}); err != nil {
		t.Fatal(err)
	}
	effective, err := svc.ListEffective(ctx, "user:user-owner", ResourceScope{Kind: "org", ID: "org-1"})
	if err != nil {
		t.Fatal(err)
	}
	var orgReadSources int
	for _, permission := range effective.Permissions {
		if permission.Key == "org.read" {
			orgReadSources++
		}
	}
	if orgReadSources != 2 {
		t.Fatalf("org.read sources=%d want legacy+custom", orgReadSources)
	}

	execMany(t, db, `INSERT INTO authorization_roles (id,org_id,kind,name,created_by,created_at,updated_at) VALUES ('role-empty','org-1','custom','empty','system',?,?)`, now, now)
	if err := svc.requireDelegatableRole(ctx, "user:user-owner", "role-empty", ResourceScope{Kind: "org", ID: "org-1", OrgID: "org-1"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty role delegation err=%v", err)
	}
	execMany(t, db, `INSERT INTO authorization_role_permissions (role_id,permission_key,resource_kind,delegatable,created_at) VALUES ('role-empty','project.read','project',1,?)`, now)
	if err := svc.requireDelegatableRole(ctx, "user:user-owner", "role-empty", ResourceScope{Kind: "org", ID: "org-1", OrgID: "org-1"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("scope mismatch delegation err=%v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE authorization_role_permissions SET permission_key='undefined.permission' WHERE role_id='role-empty'`); err != nil {
		t.Fatal(err)
	}
	if err := svc.requireDelegatableRole(ctx, "user:user-owner", "role-empty", ResourceScope{Kind: "project", ID: "project-1", OrgID: "org-1"}); !errors.Is(err, ErrPermissionUndefined) {
		t.Fatalf("undefined delegation err=%v", err)
	}

	// A successor means the earlier explicit owner is revocable.
	execMany(t, db, `INSERT INTO authorization_role_assignments (id,org_id,subject_ref,role_id,resource_kind,resource_id,created_by,created_at) VALUES ('owner-a','org-1','user:user-owner','sys-org-owner','org','org-1','system',?)`, now)
	execMany(t, db, `INSERT INTO authorization_role_assignments (id,org_id,subject_ref,role_id,resource_kind,resource_id,created_by,created_at) VALUES ('owner-b','org-1','user:user-admin','sys-org-owner','org','org-1','system',?)`, now)
	if _, err := svc.RevokeBatch(ctx, BatchRequest{IdempotencyKey: "owner-successor", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Revoke: RevokeInput{AssignmentID: "owner-a"}}}}); err != nil {
		t.Fatalf("owner with successor revoke: %v", err)
	}
	if err := svc.requireRevokeAllowed(ctx, "user:user-member", "org-1", RevokeInput{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("revoke without target err=%v", err)
	}
}

func TestBatchBoundaryValidation(t *testing.T) {
	ctx := context.Background()
	var nilSvc *Service
	if _, err := nilSvc.PreviewBatch(ctx, BatchRequest{}); err == nil {
		t.Fatal("nil preview service succeeded")
	}
	_, svc := newAuthzTestService(t)
	if _, err := svc.PreviewBatch(ctx, BatchRequest{ActorRef: "bad", OrgID: "org-1"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid preview actor err=%v", err)
	}
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "missing-actor", OrgID: "org-1"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing apply actor err=%v", err)
	}
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "bad-actor", ActorRef: "bad", OrgID: "org-1"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid apply actor err=%v", err)
	}
}

func TestFailClosedDerivationAndDelegationEdges(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-1", "org-1")
	seedTeam(t, db)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)

	if _, err := db.ExecContext(ctx, `UPDATE permission_definitions SET resource_kinds_json='not-json' WHERE key='org.read'`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListDefinitions(ctx); err == nil {
		t.Fatal("malformed registry projection did not fail")
	}
	if _, ok, err := svc.orgMember(ctx, "org-1", "worker:w"); err != nil || ok {
		t.Fatalf("worker was treated as org membership: ok=%v err=%v", ok, err)
	}
	if _, err := svc.orgDisabled(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing org disabled lookup err=%v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE permission_definitions SET resource_kinds_json='["org"]' WHERE key='org.read'`); err != nil {
		t.Fatal(err)
	}
	execMany(t, db, `INSERT INTO conversations (id,kind,status,opened_at,created_at,updated_at,organization_id,participants) VALUES ('conv-malformed','dm','open',?,?,?,'org-1','bad-json')`, now, now, now)
	if _, err := svc.ListEffective(ctx, "user:user-owner", ResourceScope{Kind: "conversation", ID: "conv-malformed"}); err == nil {
		t.Fatal("malformed membership projection did not fail effective listing")
	}

	// Agent subject is applicable to a safe team role; system bypasses only
	// delegatability, not subject/resource validation.
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "safe-agent-role", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Type: "assign_role", Assignment: AssignmentInput{SubjectRef: "agent:mem-agent", RoleID: "sys-team-member", Resource: ResourceScope{Kind: "team", ID: "team-1"}}}}}); err != nil {
		t.Fatalf("safe Agent role denied: %v", err)
	}
	// A subject with no project access cannot delegate even a role whose bits
	// are marked delegatable.
	if err := svc.requireDelegatableRole(ctx, "user:user-member", "sys-project-owner", ResourceScope{Kind: "project", ID: "project-1", OrgID: "org-1"}); !errors.Is(err, ErrNotDelegatable) {
		t.Fatalf("unauthorized delegation err=%v", err)
	}

	execMany(t, db, `INSERT INTO authorization_roles (id,org_id,kind,name,created_by,created_at,updated_at) VALUES ('foreign-role','org-2','custom','foreign','system',?,?)`, now, now)
	if _, err := svc.ApplyBatch(ctx, BatchRequest{IdempotencyKey: "foreign-role-assignment", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Type: "assign_role", Assignment: AssignmentInput{SubjectRef: "user:user-member", RoleID: "foreign-role", Resource: ResourceScope{Kind: "org", ID: "org-1"}}}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign custom role assignment err=%v", err)
	}
}

func TestRevokeAndRegistryPersistenceErrorsFailClosed(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	if _, err := svc.resolveRevokeTarget(ctx, "", RevokeInput{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty revoke org err=%v", err)
	}
	if _, err := svc.resolveRevokeTarget(ctx, "org-1", RevokeInput{AssignmentID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing revoke target err=%v", err)
	}
	if _, err := svc.store.assignmentForRevoke(ctx, "", RevokeInput{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("store empty revoke org err=%v", err)
	}
	if _, err := svc.store.assignmentForRevoke(ctx, "org-1", RevokeInput{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("store empty revoke target err=%v", err)
	}

	execMany(t, db, `INSERT INTO authorization_roles (id,org_id,kind,name,created_by,created_at,updated_at) VALUES ('role-trigger','org-1','custom','trigger','system',?,?)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	execMany(t, db, `INSERT INTO authorization_role_assignments (id,org_id,subject_ref,role_id,resource_kind,resource_id,created_by,created_at) VALUES ('asgn-trigger','org-1','user:user-member','role-trigger','org','org-1','system',?)`, now.Format(time.RFC3339Nano))
	execMany(t, db, `CREATE TRIGGER reject_authz_revoke BEFORE UPDATE OF revoked_at ON authorization_role_assignments BEGIN SELECT RAISE(ABORT, 'reject revoke'); END`)
	if _, _, err := svc.store.revokeAssignment(ctx, RevokeInput{AssignmentID: "asgn-trigger"}, "system", "org-1", now); err == nil {
		t.Fatal("revoke update storage error did not fail closed")
	}
	if _, err := svc.RevokeBatch(ctx, BatchRequest{IdempotencyKey: "trigger-revoke", ActorRef: "system", OrgID: "org-1", Operations: []BatchOperation{{Revoke: RevokeInput{AssignmentID: "asgn-trigger"}}}}); err == nil {
		t.Fatal("service revoke storage error did not roll back")
	}

	for column, restore := range map[string]string{
		"actions_json":        `["read"]`,
		"legacy_sources_json": `["members"]`,
	} {
		if _, err := db.ExecContext(ctx, `UPDATE permission_definitions SET `+column+`='bad-json' WHERE key='org.read'`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ListDefinitions(ctx); err == nil {
			t.Fatalf("malformed %s did not fail", column)
		}
		if _, err := db.ExecContext(ctx, `UPDATE permission_definitions SET `+column+`=? WHERE key='org.read'`, restore); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOwnerCountResourceResolutionAndAuditSinkErrorsFailClosed(t *testing.T) {
	t.Run("legacy owner query", func(t *testing.T) {
		db, svc := newAuthzTestService(t)
		if _, err := db.Exec(`DROP TABLE members`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.remainingOrgOwners(context.Background(), "org-1", "x"); err == nil {
			t.Fatal("missing membership storage did not fail owner count")
		}
	})
	t.Run("assignment owner query", func(t *testing.T) {
		db, svc := newAuthzTestService(t)
		if _, err := db.Exec(`DROP TABLE authorization_role_assignments`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.remainingOrgOwners(context.Background(), "org-1", "x"); err == nil {
			t.Fatal("missing assignment storage did not fail owner count")
		}
	})
	t.Run("invalid persisted resource", func(t *testing.T) {
		ctx := context.Background()
		db, svc := newAuthzTestService(t)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		execMany(t, db, `INSERT INTO authorization_roles (id,org_id,kind,name,created_by,created_at,updated_at) VALUES ('role-invalid-resource','org-1','custom','invalid','system',?,?)`, now, now)
		execMany(t, db, `INSERT INTO authorization_role_assignments (id,org_id,subject_ref,role_id,resource_kind,resource_id,created_by,created_at) VALUES ('asgn-invalid-resource','org-1','user:u','role-invalid-resource','invalid','x','system',?)`, now)
		if _, err := svc.resolveRevokeTarget(ctx, "org-1", RevokeInput{AssignmentID: "asgn-invalid-resource"}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid persisted resource err=%v", err)
		}
	})
	t.Run("event sink", func(t *testing.T) {
		ctx := context.Background()
		db, base := newAuthzTestService(t)
		svc := New(Deps{DB: db, EventSink: observability.NewEventSink(nil, nil, nil, nil)})
		if err := svc.audit(ctx, auditEvent{EventType: "authorization.test.sink", ActorRef: "system", CreatedAt: time.Now()}); err == nil {
			t.Fatal("broken domain event sink did not fail audit")
		}
		if base == nil {
			t.Fatal("nil setup service")
		}
	})
}
