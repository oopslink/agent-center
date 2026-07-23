package api

import (
	"context"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	orchsql "github.com/oopslink/agent-center/internal/projectmanager/orchestration/sqlite"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// setupPlanGraphAPI mirrors setupPlanAPI but ALSO wires the T768 orchestration
// engine (Deps.Orch) into deps.PM, so StartPlan builds a graph and the T769
// GET …/plans/{id}/graph handler serves the real engine graph.
func setupPlanGraphAPI(t *testing.T, deps HandlerDeps) *planAPIFixture {
	t.Helper()
	db := deps.DB
	clk := clock.SystemClock{}
	gen := idgen.NewGenerator(clk)
	ob := outboxsql.NewOutboxRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	convRepo := convsqlite.NewConversationRepo(db)
	plans := pmsql.NewPlanRepo(db)
	orchSvc := orch.NewService(orch.ServiceDeps{
		DB: db, Graphs: orchsql.NewGraphRepo(db), Nodes: orchsql.NewNodeRepo(db),
		Edges: orchsql.NewEdgeRepo(db), IDGen: gen, Clock: clk,
	})
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
		Stages:       pmsql.NewStageRepo(db), // T981: wire the Stage aggregate so stage-level reads work
		Outbox:       ob,
		IDGen:        gen,
		Clock:        clk,
		OrgSeq:       pmsql.NewOrgSequenceRepo(db),
		AgentDir:     allAgentsDir{},
		Orch:         orchSvc,
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

// TestPlanGraphAPI_StartedPlan_ServesEngineGraph (T769): a STARTED plan (graph
// built by T768) → GET …/plans/{id}/graph returns has_graph:true with the control
// nodes (start/end) + business nodes bound to tasks (org_ref) + edges tagged seq.
func TestPlanGraphAPI_StartedPlan_ServesEngineGraph(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "graphed", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	fx.drain(t)

	mk := func(title, who string) pm.TaskID {
		tid, terr := fx.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: caller})
		if terr != nil {
			t.Fatalf("CreateTask %s: %v", title, terr)
		}
		a := who
		if berr := fx.deps.PM.BatchUpdateTask(ctx, tid, pmservice.BatchTaskPatch{Assignee: &a}, caller); berr != nil {
			t.Fatalf("assign %s: %v", title, berr)
		}
		if serr := fx.deps.PM.SelectTaskIntoPlan(ctx, planID, tid, caller); serr != nil {
			t.Fatalf("select %s: %v", title, serr)
		}
		return tid
	}
	a := mk("A", "user:a1")
	b := mk("B", "user:b1")
	if err := fx.deps.PM.AddPlanDependency(ctx, planID, b, a, caller); err != nil {
		t.Fatal(err)
	}
	fx.drain(t)
	if err := fx.deps.PM.StartPlan(ctx, planID, caller); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}

	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/graph", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("graph status=%d", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["has_graph"] != true {
		t.Fatalf("has_graph=%v want true; body=%v", body["has_graph"], body)
	}
	if body["graph_id"] == "" || body["graph_id"] == nil {
		t.Fatalf("graph_id missing; body=%v", body)
	}
	nodes, _ := body["nodes"].([]any)
	var start, end, business int
	orgRefs := 0
	for _, raw := range nodes {
		n := raw.(map[string]any)
		switch n["category"] {
		case "control":
			switch n["control_kind"] {
			case "start":
				start++
			case "end":
				end++
			}
		case "business":
			business++
			if _, ok := n["task_id"].(string); !ok {
				t.Fatalf("business node missing task_id: %v", n)
			}
			if ref, ok := n["org_ref"].(string); ok && ref != "" {
				orgRefs++
			}
		}
	}
	if start != 1 || end != 1 {
		t.Fatalf("control nodes start=%d end=%d want 1/1; nodes=%v", start, end, nodes)
	}
	if business != 2 {
		t.Fatalf("business nodes=%d want 2; nodes=%v", business, nodes)
	}
	if orgRefs != 2 {
		t.Fatalf("business nodes with org_ref=%d want 2 (bound task org_ref)", orgRefs)
	}
	edges, _ := body["edges"].([]any)
	if len(edges) == 0 {
		t.Fatalf("no edges; want the B→A seq edge; body=%v", body)
	}
	for _, raw := range edges {
		e := raw.(map[string]any)
		if e["kind"] == nil || e["kind"] == "" {
			t.Fatalf("edge missing kind: %v", e)
		}
	}
}

// TestPlanGraphAPI_NoGraph_FallbackShape is the NON-BREAKING HTTP guard: a plan
// with NO graph (never started) → GET …/plans/{id}/graph returns 200
// {has_graph:false}, the signal the FE uses to fall back to the legacy DAG.
func TestPlanGraphAPI_NoGraph_FallbackShape(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "draft", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	fx.drain(t)

	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/graph", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("graph status=%d want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["has_graph"] != false {
		t.Fatalf("has_graph=%v want false (legacy fallback)", body["has_graph"])
	}
}

// TestPlanStagesAPI_StagedPlan_ServesProjection (T981, plan-stage-model §7): a plan with
// a stage → GET …/plans/{id}/stages returns the stage-level DERIVED read model (id/name/
// status/rounds/max_rounds/members), so the FE can render "Stage x/y" + per-stage progress.
func TestPlanStagesAPI_StagedPlan_ServesProjection(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "staged", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	fx.drain(t)

	tid, err := fx.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "A1", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	who := "user:a1"
	if err := fx.deps.PM.BatchUpdateTask(ctx, tid, pmservice.BatchTaskPatch{Assignee: &who}, caller); err != nil {
		t.Fatal(err)
	}
	if err := fx.deps.PM.SelectTaskIntoPlan(ctx, planID, tid, caller); err != nil {
		t.Fatal(err)
	}
	stageID, err := fx.deps.PM.CreateStage(ctx, pmservice.CreateStageCommand{PlanID: planID, Name: "Alpha", MaxRounds: 3, Actor: caller})
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.deps.PM.AssignTaskToStage(ctx, planID, tid, stageID, caller); err != nil {
		t.Fatal(err)
	}
	fx.drain(t)
	if err := fx.deps.PM.StartPlan(ctx, planID, caller); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}

	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/stages", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("stages status=%d want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	stages, _ := body["stages"].([]any)
	if len(stages) != 1 {
		t.Fatalf("stages len=%d want 1; body=%v", len(stages), body)
	}
	st := stages[0].(map[string]any)
	if st["name"] != "Alpha" {
		t.Fatalf("stage name=%v want Alpha", st["name"])
	}
	if st["status"] == "" || st["status"] == nil {
		t.Fatalf("stage status missing (projection); stage=%v", st)
	}
	if st["max_rounds"].(float64) != 3 {
		t.Fatalf("stage max_rounds=%v want 3", st["max_rounds"])
	}
	members, _ := st["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("members len=%d want work+gate; stage=%v", len(members), st)
	}
	if st["gate_task_id"] == "" || st["gate_task_id"] == nil {
		t.Fatalf("gate_task_id missing; stage=%v", st)
	}
	found := false
	for _, raw := range members {
		if raw.(map[string]any)["task_id"] == string(tid) {
			found = true
		}
	}
	if !found {
		t.Fatalf("business member %s missing; members=%v", tid, members)
	}
}

// TestPlanStagesAPI_NoStage_EmptyShape (T981 §8 zero-regression): a plan with NO stages
// → GET …/plans/{id}/stages returns {stages:[]}, the signal the FE uses to render the
// legacy no-stage view unchanged.
func TestPlanStagesAPI_NoStage_EmptyShape(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "nostage", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	fx.drain(t)

	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/plans/"+string(planID)+"/stages", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("stages status=%d want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	stages, ok := body["stages"].([]any)
	if !ok || len(stages) != 0 {
		t.Fatalf("stages=%v want empty array (§8 zero-regression)", body["stages"])
	}
}
