package api

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// TestPM_NestedTaskFlow_EndToEnd is the B3 spot-check path: POST a nested task
// → the participant projector creates the task Conversation + syncs the creator
// participant; POST assign agent → the work-item projector creates a queued
// AgentWorkItem. All over the real HTTP handlers + outbox relay.
func TestPM_NestedTaskFlow_EndToEnd(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	// Seed a pm project owned by the session caller (creator → owner member).
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Acme", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}

	// POST a nested task via HTTP.
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks", `{"title":"do the thing"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create task status=%d body=%s", resp.StatusCode, b)
	}
	var task map[string]any
	json.NewDecoder(resp.Body).Decode(&task)
	tid, _ := task["id"].(string)
	if tid == "" {
		t.Fatal("no task id returned")
	}

	// Seed agent AG1 in the session org so #5a's cross-org guard resolves its org
	// (assigning a Task to an agent grants it project membership; the AgentDirectory
	// verifies agent.Org == project.Org).
	ag1, aerr := agentpkg.NewAgent(agentpkg.NewAgentInput{
		ID: "AG1", OrganizationID: sess.OrgID, Profile: agentpkg.Profile{Name: "AG1"},
		WorkerID: "W-test", CreatedBy: agentpkg.IdentityRef("user:" + sess.IdentityID), CreatedAt: time.Unix(1_700_000_000, 0),
	})
	if aerr != nil {
		t.Fatal(aerr)
	}
	if serr := agentsql.NewAgentRepo(db).Save(ctx, ag1); serr != nil {
		t.Fatal(serr)
	}

	// Assign the task to an Agent via HTTP.
	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks/"+tid+"/assign", `{"assignee":"agent:AG1"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("assign status=%d body=%s", resp.StatusCode, b)
	}

	// Drain the outbox (the server runs this as the Pump; here we run it once).
	applied := outboxsql.NewAppliedRepo(db)
	convRepo := convsqlite.NewConversationRepo(db)
	wiRepo := agentsql.NewWorkItemRepo(db)
	gen := idgen.NewGenerator(clock.SystemClock{})
	relay := outbox.NewRelay(outboxsql.NewOutboxRepo(db), applied, clock.SystemClock{},
		pmservice.NewParticipantProjector(db, convRepo, applied, gen, clock.SystemClock{}),
		pmservice.NewWorkItemProjector(db, wiRepo, applied, gen, clock.SystemClock{}))
	for i := 0; i < 5; i++ {
		n, err := relay.RunOnce(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}

	// The participant projector created the task Conversation by owner_ref with
	// the creator as a participant.
	conv, err := convRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(tid))
	if err != nil {
		t.Fatalf("task Conversation not created by projector: %v", err)
	}
	if conv.Kind() != conversation.ConversationKindTask {
		t.Fatalf("conv kind = %s, want task", conv.Kind())
	}
	foundCreator := false
	for _, p := range conv.Participants() {
		if string(p.IdentityID) == string(caller) {
			foundCreator = true
		}
	}
	if !foundCreator {
		t.Fatalf("creator %s not synced as participant: %v", caller, conv.Participants())
	}

	// The work-item projector created a queued WorkItem for the agent.
	items, _ := wiRepo.ListByTask(ctx, "pm://tasks/"+tid)
	if len(items) != 1 || items[0].AgentID() != "AG1" {
		t.Fatalf("expected 1 WorkItem for AG1, got %+v", items)
	}
}

// TestPM_FlatProjectLifecycle covers the flat /api/projects surface that the
// retired Workforce project routes were repointed to in B3-c: create → list →
// get → update (rename/describe) → archive (DELETE = lifecycle, not hard
// delete). This keeps the previously-removed ListProjects/ShowProject coverage
// from going naked now that those routes serve the pm Service.
func TestPM_FlatProjectLifecycle(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	// Create via HTTP (caller becomes owner member).
	resp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"Acme","description":"d1"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status=%d body=%s", resp.StatusCode, b)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	pid, _ := created["id"].(string)
	if pid == "" || created["status"] != "active" {
		t.Fatalf("unexpected create body: %+v", created)
	}

	// List → contains the new project.
	resp = orgScopedGet(t, s.URL+"/api/projects", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var listed struct {
		Projects []map[string]any `json:"projects"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	if len(listed.Projects) != 1 || listed.Projects[0]["id"] != pid {
		t.Fatalf("list did not return created project: %+v", listed.Projects)
	}

	// Get by id.
	resp = orgScopedGet(t, s.URL+"/api/projects/"+pid, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("get status=%d", resp.StatusCode)
	}

	// Update (rename + describe).
	resp = orgScopedPatch(t, s.URL+"/api/projects/"+pid, `{"name":"Acme2","description":"d2"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update status=%d body=%s", resp.StatusCode, b)
	}
	var updated map[string]any
	json.NewDecoder(resp.Body).Decode(&updated)
	if updated["name"] != "Acme2" || updated["description"] != "d2" {
		t.Fatalf("update not applied: %+v", updated)
	}

	// Archive (DELETE = lifecycle active→archived).
	resp = orgScopedDelete(t, s.URL+"/api/projects/"+pid, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("archive status=%d", resp.StatusCode)
	}
	resp = orgScopedGet(t, s.URL+"/api/projects/"+pid, sess)
	json.NewDecoder(resp.Body).Decode(&created)
	if created["status"] != "archived" {
		t.Fatalf("project not archived: %+v", created)
	}
}

// v2.9 #298 (@oopslink): GET /api/orgs/{slug}/projects excludes archived by DEFAULT
// (sidebar / default views don't show archived); ?status=archived returns only
// archived (Projects-page "Archived" group); ?status=all returns both.
func TestPM_ListProjects_StatusFilter(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	mkProject := func(name string) string {
		resp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"`+name+`"}`, sess)
		if resp.StatusCode != 200 {
			t.Fatalf("create %s status=%d", name, resp.StatusCode)
		}
		var c map[string]any
		json.NewDecoder(resp.Body).Decode(&c)
		return c["id"].(string)
	}
	active := mkProject("Active")
	archived := mkProject("ToArchive")

	// Archive the second (DELETE = active→archived).
	if resp := orgScopedDelete(t, s.URL+"/api/projects/"+archived, sess); resp.StatusCode != 200 {
		t.Fatalf("archive status=%d", resp.StatusCode)
	}

	list := func(query string) []map[string]any {
		resp := orgScopedGet(t, s.URL+"/api/projects"+query, sess)
		if resp.StatusCode != 200 {
			t.Fatalf("list %q status=%d", query, resp.StatusCode)
		}
		var l struct {
			Projects []map[string]any `json:"projects"`
		}
		json.NewDecoder(resp.Body).Decode(&l)
		return l.Projects
	}

	// Default → only the ACTIVE project (archived excluded).
	if def := list(""); len(def) != 1 || def[0]["id"] != active {
		t.Fatalf("default list should be [active], got %+v", def)
	}
	// ?status=archived → only the ARCHIVED project.
	if arch := list("?status=archived"); len(arch) != 1 || arch[0]["id"] != archived {
		t.Fatalf("?status=archived should be [archived], got %+v", arch)
	}
	// ?status=all → both.
	if all := list("?status=all"); len(all) != 2 {
		t.Fatalf("?status=all should be 2, got %d", len(all))
	}
}

// TestPM_ProjectNesting_CrossOrgGuard proves the v2.9 org-routing-explicit
// security guard: with path-based org scoping (/api/orgs/{slug}/projects/{pid}/...)
// a caller could combine a slug they ARE a member of (org-A) with a project_id
// that lives in ANOTHER org (org-B) — 借-project_id 越权. pmRequireProjectInOrg
// must reject it (project.OrganizationID != resolved orgID → 404, no existence
// leak). Sanity: the org-B owner reaching the same project via org-B's slug 200s.
func TestPM_ProjectNesting_CrossOrgGuard(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sessA := setupTestSession(t, db, deps)                            // org-A, caller is owner
	sessB := setupNamedTestSession(t, db, "victimuser", "victim-org") // org-B
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// A project that lives in org-B (owned by the org-B user).
	pidB, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sessB.OrgID, Name: "VictimProj", CreatedBy: pm.IdentityRef("user:" + sessB.IdentityID),
	})
	if err != nil {
		t.Fatal(err)
	}

	// org-A caller uses org-A's slug (which they ARE a member of) + org-B's
	// project_id → cross-org borrow → 404 (guarded, not leaked).
	// orgScopedGet rewrites the bare path to /api/orgs/<sessA.slug>/projects/<pidB>.
	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pidB), sessA)
	if resp.StatusCode != 404 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("cross-org project borrow: got %d want 404 body=%s", resp.StatusCode, b)
	}
	// A nested read (tasks) must be guarded the same way.
	resp = orgScopedGet(t, s.URL+"/api/projects/"+string(pidB)+"/tasks", sessA)
	if resp.StatusCode != 404 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("cross-org nested borrow: got %d want 404 body=%s", resp.StatusCode, b)
	}

	// Sanity: the legitimate org-B owner reaching it via org-B's slug → 200.
	resp = orgScopedGet(t, s.URL+"/api/projects/"+string(pidB), sessB)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("own-org project access: got %d want 200 body=%s", resp.StatusCode, b)
	}
}

// TestPM_RemoveProjectMember covers DELETE /api/projects/{id}/members/{identity_id}
// (v2.7 #207/#208): add a member, remove it (with the %3A-encoded identity ref to
// prove the ServeMux wildcard decode round-trips the colon, matching the
// frontend's encodeURIComponent), then the owner-protection (409) + not-member
// (404) error paths.
func TestPM_RemoveProjectMember(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "Acme", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	base := s.URL + "/api/projects/" + string(pid)

	// Add a plain member via HTTP.
	if resp := orgScopedPost(t, base+"/members", `{"identity_id":"user:bob","role":"member"}`, sess); resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("add member status=%d body=%s", resp.StatusCode, b)
	}

	// Remove it — identity_id sent %3A-encoded (encodeURIComponent style).
	if resp := orgScopedDelete(t, base+"/members/user%3Abob", sess); resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("remove member status=%d body=%s", resp.StatusCode, b)
	}
	// Gone: list returns only the owner.
	resp := orgScopedGet(t, base+"/members", sess)
	var listed struct {
		Members []map[string]any `json:"members"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	if len(listed.Members) != 1 || listed.Members[0]["identity_id"] != string(caller) {
		t.Fatalf("after remove, want only owner, got %+v", listed.Members)
	}

	// Owner cannot be removed → 409 cannot_remove_owner (code the UI keys on).
	if resp := orgScopedDelete(t, base+"/members/user%3A"+sess.IdentityID, sess); resp.StatusCode != 409 {
		t.Fatalf("remove owner → want 409, got %d", resp.StatusCode)
	} else if code := errorCode(t, resp.Body); code != "cannot_remove_owner" {
		t.Fatalf("remove owner → want code cannot_remove_owner, got %q", code)
	}
	// Non-member target → 404 not_member (code the UI keys on).
	if resp := orgScopedDelete(t, base+"/members/user%3Aghost", sess); resp.StatusCode != 404 {
		t.Fatalf("remove non-member → want 404, got %d", resp.StatusCode)
	} else if code := errorCode(t, resp.Body); code != "not_member" {
		t.Fatalf("remove non-member → want code not_member, got %q", code)
	}
}

// errorCode decodes the {"error":"<code>","message":...} body writeError emits
// and returns the error code string ("" if absent).
func errorCode(t *testing.T, body io.Reader) string {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(body).Decode(&m); err != nil {
		return ""
	}
	if c, ok := m["error"].(string); ok {
		return c
	}
	return ""
}

// TestPM_IssueGetAndMetadataEdit exercises the B3-b prerequisite endpoints:
// GET single issue (the new symmetric route) + PATCH metadata edit on both
// Issue and Task (title/description, not a state transition).
func TestPM_IssueGetAndMetadataEdit(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "Acme", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	base := s.URL + "/api/projects/" + string(pid)

	// Create an issue via HTTP, then GET it by id (new route).
	resp := orgScopedPost(t, base+"/issues", `{"title":"bug","description":"d0"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create issue status=%d body=%s", resp.StatusCode, b)
	}
	var issue map[string]any
	json.NewDecoder(resp.Body).Decode(&issue)
	iid, _ := issue["id"].(string)
	if iid == "" {
		t.Fatal("no issue id")
	}
	resp = orgScopedGet(t, base+"/issues/"+iid, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("get issue status=%d", resp.StatusCode)
	}
	// PATCH issue metadata.
	resp = orgScopedPatch(t, base+"/issues/"+iid, `{"title":"bug2","description":"d1"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("patch issue status=%d body=%s", resp.StatusCode, b)
	}
	json.NewDecoder(resp.Body).Decode(&issue)
	if issue["title"] != "bug2" || issue["description"] != "d1" {
		t.Fatalf("issue patch not applied: %+v", issue)
	}

	// Create a task, PATCH its metadata.
	resp = orgScopedPost(t, base+"/tasks", `{"title":"do","description":"t0"}`, sess)
	var task map[string]any
	json.NewDecoder(resp.Body).Decode(&task)
	tid, _ := task["id"].(string)
	resp = orgScopedPatch(t, base+"/tasks/"+tid, `{"title":"do2"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("patch task status=%d body=%s", resp.StatusCode, b)
	}
	json.NewDecoder(resp.Body).Decode(&task)
	if task["title"] != "do2" || task["description"] != "t0" {
		t.Fatalf("task patch not applied: %+v", task)
	}

	// Unknown issue id in a real project → 404.
	resp = orgScopedGet(t, base+"/issues/ghost", sess)
	if resp.StatusCode != 404 {
		t.Fatalf("unknown issue: status=%d, want 404", resp.StatusCode)
	}
}

// TestPM_Gating covers the org+project membership gate: an org member who is
// NOT a project member is rejected (403), and an unknown/foreign project is 404.
func TestPM_Gating(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// Project owned by a DIFFERENT identity (caller is an org member but not a
	// project member).
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Other", CreatedBy: "user:someone-else",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks", `{"title":"x"}`, sess)
	if resp.StatusCode != 403 {
		t.Fatalf("non-project-member create task: status=%d, want 403", resp.StatusCode)
	}

	// Unknown project id → 404 (cross-org / non-existent hidden).
	resp = orgScopedPost(t, s.URL+"/api/projects/ghost/tasks", `{"title":"x"}`, sess)
	if resp.StatusCode != 404 {
		t.Fatalf("unknown project: status=%d, want 404", resp.StatusCode)
	}
}

// TestPM_ListProjects_Counts is the v2.10.0 #T81 (§3.4.1, finding D1) guard:
// the GET /api/projects list cards carry per-project task/issue/plan/repo
// counts (the mockup's "12 tasks · 3 issues · …" meta). Create a project, seed
// 2 tasks + 1 issue over the real HTTP handlers, and assert the list response
// reports task_count=2 / issue_count=1 / plan_count=0 / repo_count=0. The
// single-project GET stays count-free.
func TestPM_ListProjects_Counts(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"Counted"}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("create project status=%d", resp.StatusCode)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	pid, _ := created["id"].(string)
	if pid == "" {
		t.Fatalf("no project id: %+v", created)
	}

	base := s.URL + "/api/projects/" + pid
	for _, title := range []string{"t1", "t2"} {
		if r := orgScopedPost(t, base+"/tasks", `{"title":"`+title+`"}`, sess); r.StatusCode != 200 {
			t.Fatalf("create task %s status=%d", title, r.StatusCode)
		}
	}
	if r := orgScopedPost(t, base+"/issues", `{"title":"i1"}`, sess); r.StatusCode != 200 {
		t.Fatalf("create issue status=%d", r.StatusCode)
	}

	resp = orgScopedGet(t, s.URL+"/api/projects", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var listed struct {
		Projects []map[string]any `json:"projects"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	var got map[string]any
	for _, p := range listed.Projects {
		if p["id"] == pid {
			got = p
			break
		}
	}
	if got == nil {
		t.Fatalf("project %s not in list: %+v", pid, listed.Projects)
	}
	// JSON numbers decode as float64.
	want := map[string]float64{"task_count": 2, "issue_count": 1, "plan_count": 0, "repo_count": 0}
	for k, v := range want {
		gotV, ok := got[k].(float64)
		if !ok {
			t.Fatalf("list card missing %s (got %T %v): %+v", k, got[k], got[k], got)
		}
		if gotV != v {
			t.Errorf("%s = %v, want %v", k, gotV, v)
		}
	}

	// The single-project GET stays count-free (counts are a list-card concern).
	resp = orgScopedGet(t, base, sess)
	var single map[string]any
	json.NewDecoder(resp.Body).Decode(&single)
	if _, present := single["task_count"]; present {
		t.Errorf("single project GET should not carry task_count: %+v", single)
	}
}
