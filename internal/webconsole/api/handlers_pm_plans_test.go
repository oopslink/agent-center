package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// planAPIFixture overrides deps.PM with a fully Plan-capable pm Service (Plans
// repo + a REAL PlanDispatcher over deps.MessageWriter + a permissive
// AgentDirectory) over the same test DB, and returns a relay to materialize the
// plan conversation. The HTTP advance handler runs synchronously; the relay is
// drained by the test after CreatePlan so the plan conversation exists for
// dispatch.
type planAPIFixture struct {
	deps  HandlerDeps
	relay *outbox.Relay
}

func setupPlanAPI(t *testing.T, deps HandlerDeps) *planAPIFixture {
	t.Helper()
	db := deps.DB
	clk := clock.SystemClock{}
	gen := idgen.NewGenerator(clk)
	ob := outboxsql.NewOutboxRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	convRepo := convsqlite.NewConversationRepo(db)
	plans := pmsql.NewPlanRepo(db)
	deps.PM = pmservice.New(pmservice.Deps{
		DB:           db,
		Projects:     pmsql.NewProjectRepo(db),
		Members:      pmsql.NewProjectMemberRepo(db),
		Issues:       pmsql.NewIssueRepo(db),
		Tasks:        pmsql.NewTaskRepo(db),
		TaskSubs:     pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:    pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Plans:        plans,
		Outbox:       ob,
		IDGen:        gen,
		Clock:        clk,
		AgentDir:     allAgentsDir{},
		PlanDispatcher: convservice.NewPlanDispatchAdapter(deps.MessageWriter, func(_ context.Context, ref string) (string, bool) {
			if i := strings.IndexByte(ref, ':'); i >= 0 {
				ref = ref[i+1:]
			}
			if strings.TrimSpace(ref) == "" {
				return "", false
			}
			return ref, true
		}),
	})
	taskProj := pmservice.NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := pmservice.NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)
	return &planAPIFixture{deps: deps, relay: relay}
}

// allAgentsDir maps every agent to the test session's org so agent assignees
// resolve in StartPlan (§9.6c).
type allAgentsDir struct{}

func (allAgentsDir) OrgOfAgent(_ context.Context, _ string) (string, error) { return "", nil }

func (f *planAPIFixture) drain(t *testing.T) {
	t.Helper()
	for {
		n, err := f.relay.RunOnce(context.Background(), 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode body: %v\nbody=%s", err, b)
	}
	return m
}

// HTTP create→get returns the DERIVED node read model; start + advance happy
// path posts an @mention into the plan conversation.
func TestPlanAPI_CreateGetStartAdvance(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}

	// POST /plans (create).
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans", `{"name":"v3.0","description":"goal"}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("create plan status=%d", resp.StatusCode)
	}
	created := decodeBody(t, resp)
	planID := created["id"].(string)
	if created["status"] != "draft" {
		t.Fatalf("new plan status = %v, want draft", created["status"])
	}
	if _, ok := created["nodes"]; !ok {
		t.Fatal("create response must carry derived nodes")
	}
	prog := created["progress"].(map[string]any)
	if prog["total"].(float64) != 0 {
		t.Fatalf("empty plan total = %v, want 0", prog["total"])
	}
	// Materialize the plan conversation (binds conversation_id back onto the plan).
	fx.drain(t)

	// Seed two assigned tasks selected into the plan, with B depends_on A.
	taskA := fx.seedSelectedTask(t, sess, pid, pm.PlanID(planID), "A", "user:"+sess.IdentityID)
	taskB := fx.seedSelectedTask(t, sess, pid, pm.PlanID(planID), "B", "user:"+sess.IdentityID)
	if err := fx.deps.PM.AddPlanDependency(ctx, pm.PlanID(planID), taskB, taskA, caller); err != nil {
		t.Fatal(err)
	}
	fx.drain(t)

	// GET /plans/{id} returns derived nodes + ready-set + has_failed + progress.
	resp = orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+planID, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("get plan status=%d", resp.StatusCode)
	}
	got := decodeBody(t, resp)
	nodes := got["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("nodes=%d want 2", len(nodes))
	}
	// In draft (not started) both nodes are blocked/ready by derivation: A ready
	// (no upstream), B blocked (A not done). Verify node_status is present + derived.
	statusByTask := map[string]string{}
	for _, n := range nodes {
		nm := n.(map[string]any)
		statusByTask[nm["task_id"].(string)] = nm["node_status"].(string)
	}
	if statusByTask[string(taskA)] != "ready" {
		t.Fatalf("A node_status=%s want ready", statusByTask[string(taskA)])
	}
	if statusByTask[string(taskB)] != "blocked" {
		t.Fatalf("B node_status=%s want blocked", statusByTask[string(taskB)])
	}

	// POST /start (§9.6 happy path).
	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+planID+"/start", `{}`, sess)
	if resp.StatusCode != 200 {
		body := decodeBody(t, resp)
		t.Fatalf("start status=%d body=%v", resp.StatusCode, body)
	}
	started := decodeBody(t, resp)
	if started["status"] != "running" {
		t.Fatalf("started plan status=%v want running", started["status"])
	}

	// POST /advance: A is ready → dispatched (one @mention posted to the conversation).
	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+planID+"/advance", `{}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("advance status=%d", resp.StatusCode)
	}
	adv := decodeBody(t, resp)
	dispatched := adv["dispatched"].([]any)
	if len(dispatched) != 1 || dispatched[0].(string) != string(taskA) {
		t.Fatalf("advance dispatched=%v want [A]", dispatched)
	}

	// The @mention landed in the plan conversation.
	conv, _ := convsqlite.NewConversationRepo(db).FindByOwnerRef(ctx, conversation.NewPlanOwnerRef(planID))
	msgs, _ := convsqlite.NewMessageRepo(db).FindRecent(ctx, conv.ID(), 100)
	if len(msgs) != 1 {
		t.Fatalf("plan conversation messages=%d want 1 (@A ready)", len(msgs))
	}

	// Re-advance is idempotent: no second @mention.
	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+planID+"/advance", `{}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("re-advance status=%d", resp.StatusCode)
	}
	adv2 := decodeBody(t, resp)
	if len(adv2["dispatched"].([]any)) != 0 {
		t.Fatalf("re-advance dispatched=%v want none (§9.3)", adv2["dispatched"])
	}
	msgs2, _ := convsqlite.NewMessageRepo(db).FindRecent(ctx, conv.ID(), 100)
	if len(msgs2) != 1 {
		t.Fatalf("re-advance posted extra messages: total=%d want 1", len(msgs2))
	}
}

// createPlan POSTs /plans and drains so the plan conversation binds; returns the
// plan id.
func (f *planAPIFixture) createPlan(t *testing.T, s *httptest.Server, sess testSession, pid pm.ProjectID, name string) pm.PlanID {
	t.Helper()
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans", `{"name":"`+name+`"}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("create plan %q status=%d", name, resp.StatusCode)
	}
	planID := decodeBody(t, resp)["id"].(string)
	f.drain(t)
	return pm.PlanID(planID)
}

// TestPlanAPI_ListSummaries_BoardEnrich asserts the LIST endpoint enriches each
// Plan with the Work Board read model (progress/has_failed/node_count/capped
// nodes_preview) so the board renders every column from ONE call — and that the
// list's derived node_status matches GetPlanDetail (derive once, same mapping).
func TestPlanAPI_ListSummaries_BoardEnrich(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}

	// planMixed: 3 tasks — one completed (done), one discarded (failed), one open.
	// → progress {1,3}, has_failed true, node_count 3.
	planMixed := fx.createPlan(t, s, sess, pid, "mixed")
	tDone := fx.seedSelectedTask(t, sess, pid, planMixed, "done-task", "user:"+sess.IdentityID)
	fx.seedSelectedTask(t, sess, pid, planMixed, "fail-task", "user:"+sess.IdentityID)
	fx.seedSelectedTask(t, sess, pid, planMixed, "open-task", "user:"+sess.IdentityID)
	if err := fx.deps.PM.SetTaskStatus(ctx, tDone, pm.TaskCompleted, caller); err != nil {
		t.Fatal(err)
	}
	// discard the second task
	tasks, _ := fx.deps.PM.ListTasks(ctx, pid)
	var tFail pm.TaskID
	for _, tk := range tasks {
		if tk.Title() == "fail-task" {
			tFail = tk.ID()
		}
	}
	if err := fx.deps.PM.SetTaskStatus(ctx, tFail, pm.TaskDiscarded, caller); err != nil {
		t.Fatal(err)
	}

	// planBig: 6 tasks → nodes_preview capped at 4, node_count 6.
	planBig := fx.createPlan(t, s, sess, pid, "big")
	for i := 0; i < 6; i++ {
		fx.seedSelectedTask(t, sess, pid, planBig, "big-"+string(rune('a'+i)), "user:"+sess.IdentityID)
	}

	// planEmpty: no tasks → progress {0,0}, nodes_preview [].
	planEmpty := fx.createPlan(t, s, sess, pid, "empty")

	// GET list — ONE call returns all three enriched plans.
	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	plans := body["plans"].([]any)
	// 3 structured plans + the ADR-0047 auto-created built-in pool.
	if len(plans) != 4 {
		t.Fatalf("list returned %d plans, want 4", len(plans))
	}
	byID := map[string]map[string]any{}
	for _, p := range plans {
		pl := p.(map[string]any)
		byID[pl["id"].(string)] = pl
	}

	// --- planMixed: progress{1,3}, has_failed, node_count 3 -------------------
	mixed := byID[string(planMixed)]
	if mixed == nil {
		t.Fatal("mixed plan missing from list")
	}
	mp := mixed["progress"].(map[string]any)
	if mp["done"].(float64) != 1 || mp["total"].(float64) != 3 {
		t.Fatalf("mixed progress=%v want {done:1,total:3}", mp)
	}
	if mixed["has_failed"].(bool) != true {
		t.Fatalf("mixed has_failed=%v want true", mixed["has_failed"])
	}
	if mixed["node_count"].(float64) != 3 {
		t.Fatalf("mixed node_count=%v want 3", mixed["node_count"])
	}
	mprev := mixed["nodes_preview"].([]any)
	if len(mprev) != 3 {
		t.Fatalf("mixed nodes_preview len=%d want 3", len(mprev))
	}
	for _, n := range mprev {
		nm := n.(map[string]any)
		// FULL node contract — same fields a detail node carries (no fork). In
		// particular task_status (the board StatusChip reads it → must exist) and
		// depends_on must be present, not just the old summary subset.
		for _, f := range []string{"task_id", "title", "assignee_ref", "task_status", "node_status", "depends_on"} {
			if _, ok := nm[f]; !ok {
				t.Fatalf("preview node missing %q: %v", f, nm)
			}
		}
		if nm["assignee_ref"].(string) == "" {
			t.Fatalf("preview node assignee_ref empty: %v", nm)
		}
		if nm["task_status"].(string) == "" {
			t.Fatalf("preview node task_status empty (StatusChip would crash): %v", nm)
		}
	}

	// --- planBig: preview capped at 4, node_count 6 ---------------------------
	big := byID[string(planBig)]
	if big["node_count"].(float64) != 6 {
		t.Fatalf("big node_count=%v want 6", big["node_count"])
	}
	if got := len(big["nodes_preview"].([]any)); got != planListNodePreviewCap {
		t.Fatalf("big nodes_preview len=%d want %d (capped)", got, planListNodePreviewCap)
	}

	// --- planEmpty: progress{0,0}, preview [] ---------------------------------
	empty := byID[string(planEmpty)]
	ep := empty["progress"].(map[string]any)
	if ep["done"].(float64) != 0 || ep["total"].(float64) != 0 {
		t.Fatalf("empty progress=%v want {0,0}", ep)
	}
	if empty["has_failed"].(bool) != false {
		t.Fatalf("empty has_failed=%v want false", empty["has_failed"])
	}
	if empty["node_count"].(float64) != 0 {
		t.Fatalf("empty node_count=%v want 0", empty["node_count"])
	}
	if len(empty["nodes_preview"].([]any)) != 0 {
		t.Fatalf("empty nodes_preview=%v want []", empty["nodes_preview"])
	}

	// --- consistency: a list-row preview node == the SAME GetPlanDetail node,
	// FIELD-BY-FIELD (same shape, no fork). The list preview and the detail DTO are
	// rendered through the same pmPlanNodeMap helper, so every key/value must match.
	detResp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planMixed), sess)
	if detResp.StatusCode != 200 {
		t.Fatalf("detail status=%d", detResp.StatusCode)
	}
	detailNodes := map[string]map[string]any{}
	for _, n := range decodeBody(t, detResp)["nodes"].([]any) {
		nm := n.(map[string]any)
		detailNodes[nm["task_id"].(string)] = nm
	}
	for _, n := range mprev {
		ln := n.(map[string]any)
		tid := ln["task_id"].(string)
		dn := detailNodes[tid]
		if dn == nil {
			t.Fatalf("preview node %s absent from detail nodes", tid)
		}
		// Same key SET (no extra/missing field in either direction).
		if len(ln) != len(dn) {
			t.Fatalf("node %s field-count mismatch: list keys=%v detail keys=%v", tid, keysOf(ln), keysOf(dn))
		}
		// Same VALUE for every field — reflect.DeepEqual handles the depends_on
		// slice + the optional dispatched_at string.
		if !reflect.DeepEqual(ln, dn) {
			t.Fatalf("node %s list!=detail field-by-field:\n list=%#v\n detail=%#v", tid, ln, dn)
		}
	}
}

// keysOf returns the sorted keys of a node map for readable mismatch diagnostics.
func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// TestPlanAPI_ListSummaries_OrgGate confirms the enriched LIST endpoint still
// 404s a project outside the caller's org (pmRequireProjectInOrg preserved).
func TestPlanAPI_ListSummaries_OrgGate(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()

	// A project in a DIFFERENT org — the session is not a member.
	otherPID, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: "other-org", Name: "X", CreatedBy: "user:someone"})
	if err != nil {
		t.Fatal(err)
	}
	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(otherPID)+"/plans", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org list status=%d want 404", resp.StatusCode)
	}
}

// TestPlanAPI_RemoveDependency_ViaQuery exercises the REAL FE chain: the edge is
// passed in the DELETE query string (the FE api.del client is path/query-only, no
// body). Regression guard for the body-vs-query seam where the handler read the
// JSON body → from/to empty → the edge was never removed (A1 remove-edge bug).
func TestPlanAPI_RemoveDependency_ViaQuery(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	planID := fx.createPlan(t, s, sess, pid, "edges")
	fx.drain(t)
	taskA := fx.seedSelectedTask(t, sess, pid, planID, "A", "user:"+sess.IdentityID)
	taskB := fx.seedSelectedTask(t, sess, pid, planID, "B", "user:"+sess.IdentityID)
	if err := fx.deps.PM.AddPlanDependency(ctx, planID, taskB, taskA, caller); err != nil {
		t.Fatal(err)
	}
	fx.drain(t)

	// DELETE the edge via the query string (NOT a body) — the FE-client shape.
	url := s.URL + "/api/projects/" + string(pid) + "/plans/" + string(planID) +
		"/dependencies?from_task_id=" + string(taskB) + "&to_task_id=" + string(taskA)
	resp := orgScopedDelete(t, url, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("remove dependency via query status=%d, want 200", resp.StatusCode)
	}
	got := decodeBody(t, resp)
	for _, n := range got["nodes"].([]any) {
		node := n.(map[string]any)
		if node["task_id"] == string(taskB) {
			if dep, ok := node["depends_on"].([]any); ok && len(dep) != 0 {
				t.Fatalf("edge not removed: node B still depends_on %v", dep)
			}
		}
	}
}

// TestPlanAPI_Delete_NonRunning: DELETE /plans/{id} on a draft plan returns 200
// + {deleted:true}, removes the plan, and unloads its task back to the backlog.
func TestPlanAPI_Delete_NonRunning(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	planID := fx.createPlan(t, s, sess, pid, "doomed")
	tid := fx.seedSelectedTask(t, sess, pid, planID, "a", "user:"+sess.IdentityID)

	resp := orgScopedDelete(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID), sess)
	if resp.StatusCode != 200 {
		t.Fatalf("delete status=%d want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["deleted"] != true {
		t.Fatalf("delete body=%v want deleted:true", body)
	}
	fx.drain(t)

	// Plan gone (GET → 404).
	get := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID), sess)
	if get.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted plan status=%d want 404", get.StatusCode)
	}
	// Task unloaded to backlog (still exists, plan_id="").
	tk, err := fx.deps.PM.GetTask(ctx, tid)
	if err != nil {
		t.Fatalf("task must survive (unloaded): %v", err)
	}
	if tk.PlanID() != "" {
		t.Fatalf("task plan_id=%q want \"\"", tk.PlanID())
	}
}

// TestPlanAPI_Delete_RunningRejected_409: DELETE on a running plan → 409.
func TestPlanAPI_Delete_RunningRejected_409(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, _ := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	planID := fx.createPlan(t, s, sess, pid, "live")
	fx.seedSelectedTask(t, sess, pid, planID, "a", "user:"+sess.IdentityID)
	if r := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/start", `{}`, sess); r.StatusCode != 200 {
		t.Fatalf("start status=%d", r.StatusCode)
	}
	fx.drain(t)

	resp := orgScopedDelete(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID), sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete running plan status=%d want 409", resp.StatusCode)
	}
}

// TestPlanAPI_RemoveTask_NonDraft_409 locks the v2.9 ErrPlanNotDraft→409 unification:
// editing the task-set of a RUNNING plan is a STATE-conflict (was 400 invalid_request),
// now 409 plan_conflict — same class + code as ErrPlanRunning/Archived, consistent with MCP.
func TestPlanAPI_RemoveTask_NonDraft_409(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, _ := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	planID := fx.createPlan(t, s, sess, pid, "live")
	taskA := fx.seedSelectedTask(t, sess, pid, planID, "a", "user:"+sess.IdentityID)
	if r := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/start", `{}`, sess); r.StatusCode != 200 {
		t.Fatalf("start status=%d", r.StatusCode)
	}
	fx.drain(t)

	// Remove a task from the RUNNING plan → ErrPlanNotDraft → 409 (was 400).
	resp := orgScopedDelete(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/tasks/"+string(taskA), sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("remove task from running plan status=%d want 409 (ErrPlanNotDraft state-conflict)", resp.StatusCode)
	}
}

// TestPlanAPI_Delete_OrgGate: DELETE a plan in another org → 404 (project gate).
func TestPlanAPI_Delete_OrgGate(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()

	otherPID, _ := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: "other-org", Name: "X", CreatedBy: "user:someone"})
	resp := orgScopedDelete(t, s.URL+"/api/projects/"+string(otherPID)+"/plans/PL-nope", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org delete status=%d want 404", resp.StatusCode)
	}
}

// TestPlanAPI_Archive_NonRunning: POST /plans/{id}/archive on a draft plan → 200,
// plan status archived, its task archived (status preserved).
func TestPlanAPI_Archive_NonRunning(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, _ := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	planID := fx.createPlan(t, s, sess, pid, "shelf")
	tid := fx.seedSelectedTask(t, sess, pid, planID, "a", "user:"+sess.IdentityID)

	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/archive", `{}`, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("archive status=%d want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["status"] != "archived" {
		t.Fatalf("archived plan status=%v want archived", body["status"])
	}
	fx.drain(t)

	tk, _ := fx.deps.PM.GetTask(ctx, tid)
	if !tk.IsArchived() {
		t.Fatal("cascade: task must be archived")
	}
	if tk.Status() != pm.TaskOpen {
		t.Fatalf("task status=%q want open (orthogonal preserved)", tk.Status())
	}
}

// TestPlanAPI_Archive_RunningRejected_409: POST archive on a running plan → 409.
func TestPlanAPI_Archive_RunningRejected_409(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, _ := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	planID := fx.createPlan(t, s, sess, pid, "live")
	fx.seedSelectedTask(t, sess, pid, planID, "a", "user:"+sess.IdentityID)
	if r := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/start", `{}`, sess); r.StatusCode != 200 {
		t.Fatalf("start status=%d", r.StatusCode)
	}
	fx.drain(t)

	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/archive", `{}`, sess)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("archive running plan status=%d want 409", resp.StatusCode)
	}
}

// seedSelectedTask creates+assigns a task and selects it into the plan via the
// service (then drains so the assignee becomes a plan-conversation participant).
func (f *planAPIFixture) seedSelectedTask(t *testing.T, sess testSession, pid pm.ProjectID, planID pm.PlanID, title, assignee string) pm.TaskID {
	t.Helper()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)
	tid, err := f.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	a := assignee
	if err := f.deps.PM.BatchUpdateTask(ctx, tid, pmservice.BatchTaskPatch{Assignee: &a}, caller); err != nil {
		t.Fatal(err)
	}
	if err := f.deps.PM.SelectTaskIntoPlan(ctx, planID, tid, caller); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	return tid
}
