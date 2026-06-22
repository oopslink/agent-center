package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// v2.8 #258/#260: org-scoped cross-project issue/task aggregation endpoints.
// Tester's blocking correctness gates: org-scope (no cross-org leak) +
// aggregation == sum of the org's projects. Plus the DTO contract (issue
// assignee always null, project{id,name} no slug, org_ref) + the default
// "all open" status filter + updated DESC sort.

// --- pure helper unit tests -------------------------------------------------

func TestStatusPasses_260_DefaultOpenVsExplicit(t *testing.T) {
	issueTerm := issueTerminalStatus
	// default (empty explicit): open-set passes, terminal excluded.
	for _, open := range []string{"open", "in_progress", "reopened"} {
		if !statusPasses(open, map[string]bool{}, issueTerm) {
			t.Errorf("default-open should pass %q", open)
		}
	}
	// T330: "discarded" (was the stale "withdrawn") must be excluded by default.
	for _, term := range []string{"resolved", "closed", "discarded"} {
		if statusPasses(term, map[string]bool{}, issueTerm) {
			t.Errorf("default-open should exclude terminal %q", term)
		}
	}
	// explicit filter: only members pass (even a terminal one if explicitly asked).
	explicit := map[string]bool{"closed": true}
	if !statusPasses("closed", explicit, issueTerm) {
		t.Errorf("explicit closed should pass")
	}
	if statusPasses("open", explicit, issueTerm) {
		t.Errorf("explicit {closed} should exclude open")
	}
}

// TestStatusPasses_AllSentinel_IncludesTerminal (T62/task-336335c5, reused by
// T76/task-c780999a) pins the `status=all` escape hatch: it passes EVERY status —
// terminal included — so the message task-ref / T-number linkify resolver can
// resolve a completed/discarded task instead of leaving it plain text. The
// default (empty explicit) must STILL exclude terminal (class-guard).
func TestStatusPasses_AllSentinel_IncludesTerminal(t *testing.T) {
	term := taskTerminalStatus
	all := map[string]bool{"all": true}
	for _, s := range []string{"open", "running", "reopened", "completed", "discarded"} {
		if !statusPasses(s, all, term) {
			t.Errorf("status=all must include %q (terminal included)", s)
		}
	}
	if statusPasses("completed", map[string]bool{}, term) {
		t.Errorf("default (empty explicit) must STILL exclude terminal 'completed'")
	}
	if !statusPasses("discarded", map[string]bool{"all": true, "open": true}, term) {
		t.Errorf("status=all,open must still include terminal 'discarded'")
	}
}

func TestParseSetParam_260_CommaAndRepeated(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/issues?status=open,running&status=blocked&project=", nil)
	got := parseSetParam(req, "status")
	for _, want := range []string{"open", "running", "blocked"} {
		if !got[want] {
			t.Errorf("status set missing %q (got %v)", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("status set = %v, want 3 entries", got)
	}
	// empty project param → empty set (no filter).
	if p := parseSetParam(req, "project"); len(p) != 0 {
		t.Errorf("empty project param should yield empty set, got %v", p)
	}
}

func TestAssigneeMatches_260(t *testing.T) {
	if !assigneeMatches("agent:agent-1a2b", "agent:agent-1a2b") {
		t.Error("full ref should match")
	}
	if !assigneeMatches("agent:agent-1a2b", "agent-1a2b") {
		t.Error("bare member-id should match the prefixed ref")
	}
	if assigneeMatches("agent:agent-1a2b", "agent-zzzz") {
		t.Error("different id should not match")
	}
	if assigneeMatches("", "agent-1a2b") {
		t.Error("empty assignee should not match")
	}
}

func TestSortItemsUpdatedDesc_260(t *testing.T) {
	items := []map[string]any{
		{"updated_at": "2026-06-01T00:00:00Z"},
		{"updated_at": "2026-06-03T00:00:00Z"},
		{"updated_at": "2026-06-02T00:00:00Z"},
	}
	sortItemsUpdatedDesc(items)
	if items[0]["updated_at"] != "2026-06-03T00:00:00Z" || items[2]["updated_at"] != "2026-06-01T00:00:00Z" {
		t.Errorf("not sorted desc: %v", items)
	}
}

func TestOrgIssueRow_260_AssigneeNullProjectNoSlug(t *testing.T) {
	// orgIssueRow must always emit assignee=null (issues aren't assignable) and
	// project={id,name} (no slug — pm.Project has none).
	// Build a real issue + project via the service to exercise the actual types.
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	_ = db
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "Acme", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	iid, err := deps.PM.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	proj, _ := deps.PM.GetProject(ctx, pid)
	iss, _ := deps.PM.GetIssue(ctx, iid)
	row := orgIssueRow(iss, proj)
	if row["assignee"] != nil {
		t.Errorf("issue assignee must be null, got %v", row["assignee"])
	}
	projMap, _ := row["project"].(map[string]any)
	if projMap["id"] != string(pid) || projMap["name"] != "Acme" {
		t.Errorf("project map = %v, want {id,name}", projMap)
	}
	if _, hasSlug := projMap["slug"]; hasSlug {
		t.Errorf("project map must not contain slug")
	}
}

// TestEnrichAssignee_260 covers the task assignee enrichment path (a prefixed
// ref → complete-consumable {ref, display_name, member_id}) that the org-tasks
// DTO relies on. Tester's black-box de-risk left this to Dev unit coverage
// (their seeded tasks were all unassigned), so it is pinned here against a real
// IdentityRepo.
func TestEnrichAssignee_260(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	_ = db
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)

	// Unassigned → nil (UI renders "—").
	if got := s.enrichAssignee(req, deps, ""); got != nil {
		t.Errorf("empty assignee should enrich to nil, got %v", got)
	}

	// Assigned to the session user (a real identity with a display name) →
	// {ref, member_id, display_name} all populated; member_id is the bare id.
	ref := "user:" + sess.IdentityID
	got := s.enrichAssignee(req, deps, ref)
	if got == nil {
		t.Fatal("assigned ref should enrich to a non-nil map")
	}
	if got["ref"] != ref {
		t.Errorf("ref = %v, want %q (full prefixed ref preserved)", got["ref"], ref)
	}
	if got["member_id"] != sess.IdentityID {
		t.Errorf("member_id = %v, want %q (bare id)", got["member_id"], sess.IdentityID)
	}
	if dn, _ := got["display_name"].(string); dn == "" {
		t.Errorf("display_name should be resolved (non-empty) for a real identity")
	}
}

// --- handler correctness test (blocking gates) ------------------------------

func TestListOrgIssuesTasks_260_OrgScopeAndAggregation(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	// Two projects in the caller's org, each with 1 issue + 1 task.
	p1, _ := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "Alpha", CreatedBy: caller})
	p2, _ := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "Beta", CreatedBy: caller})
	for _, pid := range []pm.ProjectID{p1, p2} {
		if _, err := deps.PM.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: pid, Title: "iss", CreatedBy: caller}); err != nil {
			t.Fatal(err)
		}
		if _, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "tsk", CreatedBy: caller}); err != nil {
			t.Fatal(err)
		}
	}

	// A project in a DIFFERENT org (distinct org_id) with an issue — must NOT
	// leak into org 1's view. Org-scope isolation is enforced by ListProjects
	// returning only the caller-org's projects, so a project under another
	// org_id never aggregates.
	op, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: "org-other-9999", Name: "OtherCo", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.PM.CreateIssue(ctx, pmservice.CreateIssueCommand{ProjectID: op, Title: "secret", CreatedBy: caller}); err != nil {
		t.Fatal(err)
	}

	// GET /api/issues for org 1 → aggregation == sum of org 1's projects (2),
	// no cross-org leak (OtherCo's issue absent).
	issues := decodeItems(t, orgScopedGet(t, s.URL+"/api/issues", sess))
	if len(issues) != 2 {
		t.Fatalf("org issues = %d, want 2 (sum of 2 projects, no cross-org leak)", len(issues))
	}
	for _, it := range issues {
		if it["title"] == "secret" {
			t.Fatal("CROSS-ORG LEAK: other org's issue appeared")
		}
		if it["assignee"] != nil {
			t.Errorf("issue assignee must be null, got %v", it["assignee"])
		}
		proj, _ := it["project"].(map[string]any)
		if proj["name"] == nil || proj["id"] == nil {
			t.Errorf("issue project must carry {id,name}: %v", proj)
		}
	}

	// GET /api/tasks → 2 (sum of org 1's projects).
	tasks := decodeItems(t, orgScopedGet(t, s.URL+"/api/tasks", sess))
	if len(tasks) != 2 {
		t.Fatalf("org tasks = %d, want 2", len(tasks))
	}
}

func decodeItems(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	var env struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != len(env.Items) {
		t.Errorf("total %d != len(items) %d", env.Total, len(env.Items))
	}
	return env.Items
}
