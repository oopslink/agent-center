package authorization

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestService_AccessGraphPaginationCompletenessRedactionAndParity(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedProject(t, db, "project-graph", "org-1")
	seedProjectMember(t, db, "pm-graph-owner", "project-graph", "user:user-owner", "owner")
	seedGraphTask(t, db, "task-graph", "project-graph", "user:user-owner", "user:user-owner")

	graph, err := svc.ListAccessGraph(ctx, AccessGraphRequest{
		SubjectRef:     "user:user-owner",
		OrgID:          "org-1",
		Layer:          "permissions",
		Limit:          2,
		RedactEvidence: true,
		ShadowParity:   true,
	})
	if err != nil {
		t.Fatalf("ListAccessGraph: %v", err)
	}
	if graph.Complete || !graph.Completeness.HasMore || graph.NextCursor == "" {
		t.Fatalf("pagination completeness = %#v next=%q complete=%v", graph.Completeness, graph.NextCursor, graph.Complete)
	}
	if graph.Completeness.Returned != 2 || graph.Completeness.Total <= 2 {
		t.Fatalf("completeness = %#v", graph.Completeness)
	}
	if graph.ParityShadow.Checked == 0 || graph.ParityShadow.Mismatches != 0 || !graph.ParityShadow.Complete {
		t.Fatalf("parity shadow = %#v", graph.ParityShadow)
	}
	for _, p := range graph.Permissions {
		if !p.Evidence.Redacted {
			t.Fatalf("permission evidence not redacted: %#v", p.Evidence)
		}
		if p.Evidence.Ref == "members:mem-owner" || p.Evidence.Ref == "pm_project_members:pm-graph-owner" {
			t.Fatalf("raw evidence leaked: %#v", p.Evidence)
		}
		if p.Evidence.Hash == "" {
			t.Fatalf("redacted evidence missing stable hash: %#v", p.Evidence)
		}
	}

	next, err := svc.ListAccessGraph(ctx, AccessGraphRequest{
		SubjectRef:     "user:user-owner",
		OrgID:          "org-1",
		Layer:          "permissions",
		Cursor:         graph.NextCursor,
		Limit:          100,
		RedactEvidence: true,
	})
	if err != nil {
		t.Fatalf("ListAccessGraph next page: %v", err)
	}
	if !next.Complete || next.Completeness.HasMore {
		t.Fatalf("next page completeness = %#v complete=%v", next.Completeness, next.Complete)
	}
}

func TestService_AccessGraphWorkerTokenRiskAndInternalParity(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedGraphWorker(t, db, "worker-graph", "org-1", "offline")
	seedGraphAdminToken(t, db, "tok-worker", "worker:worker-graph", `["task:*","*"]`, false, "worker-graph")

	graph, err := svc.ListAccessGraph(ctx, AccessGraphRequest{
		SubjectRef:     "worker:worker-graph",
		OrgID:          "org-1",
		Layer:          "permissions",
		Limit:          100,
		RedactEvidence: true,
		ShadowParity:   true,
	})
	if err != nil {
		t.Fatalf("ListAccessGraph worker: %v", err)
	}
	if graph.RiskSummary.ActiveWorkerTokens != 1 || graph.RiskSummary.WildcardAdminTokens == 0 {
		t.Fatalf("risk summary = %#v", graph.RiskSummary)
	}
	if graph.RiskSummary.WorkerStatus != "offline" || graph.RiskSummary.WorkerDisabledCapabilities != 1 {
		t.Fatalf("worker risk summary = %#v", graph.RiskSummary)
	}
	if graph.ParityShadow.Checked == 0 || graph.ParityShadow.Mismatches != 0 {
		t.Fatalf("internal parity shadow = %#v", graph.ParityShadow)
	}
	var sawInternal bool
	for _, p := range graph.Permissions {
		if p.Key == "task.internal.report" && p.Source == SourceAdminTokenScope {
			sawInternal = true
			if p.Evidence.Ref == "admin_tokens:tok-worker/scope:task:*" {
				t.Fatalf("raw token evidence leaked: %#v", p.Evidence)
			}
		}
	}
	if !sawInternal {
		t.Fatalf("missing task.internal.report grant in graph: %#v", graph.Permissions)
	}
}

func TestService_AccessGraphCrossOrgWorkerSubjectIsOpaqueNotFound(t *testing.T) {
	ctx := context.Background()
	db, svc := newAuthzTestService(t)
	seedAuthzBase(t, db)
	seedGraphWorker(t, db, "worker-cross", "org-2", "online")
	seedGraphAdminToken(t, db, "tok-cross-worker", "worker:worker-cross", `["task:*"]`, false, "worker-cross")

	_, err := svc.ListAccessGraph(ctx, AccessGraphRequest{
		SubjectRef: "worker:worker-cross",
		OrgID:      "org-1",
	})
	if err != ErrNotFound {
		t.Fatalf("cross-org worker graph err=%v, want ErrNotFound", err)
	}
}

func seedGraphTask(t *testing.T, db *sql.DB, id, projectID, assignee, createdBy string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db,
		`INSERT INTO pm_tasks (id, project_id, title, description, status, assignee, created_by, created_at, updated_at)
		 VALUES (?, ?, 'Graph Task', '', 'open', ?, ?, ?, ?)`,
		id, projectID, assignee, createdBy, now, now,
	)
}

func seedGraphWorker(t *testing.T, db *sql.DB, id, orgID, status string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	execMany(t, db,
		`INSERT INTO workers
		 (id, name, status, concurrency_json, discovery_json, capabilities_json, enrolled_at, created_at, updated_at, organization_id)
		 VALUES (?, ?, ?, '{"per_agent_type":2}', '{"scan_paths":[],"exclude":[],"scan_interval":"1h"}',
		 '[{"agent_cli":"codex","detected":true,"enabled":false}]', ?, ?, ?, ?)`,
		id, id, status, now, now, now, orgID,
	)
}

func seedGraphAdminToken(t *testing.T, db *sql.DB, id, owner, scopesJSON string, enroll bool, workerID string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	isEnroll := 0
	if enroll {
		isEnroll = 1
	}
	execMany(t, db,
		`INSERT INTO admin_tokens
		 (id, owner, scopes_json, value_hash, created_at, created_by, is_enroll, worker_id)
		 VALUES (?, ?, ?, ?, ?, 'test', ?, ?)`,
		id, owner, scopesJSON, []byte(id+"-hash-value-32-bytes"), now, isEnroll, workerID,
	)
}
