package api

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/clock"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/identity"
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
		OrgMembers:   deps.MemberRepo,
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

type generationAPIPlan struct {
	projectID pm.ProjectID
	planID    pm.PlanID
	a         pm.TaskID
	b         pm.TaskID
	g0        pm.PlanGenerationID
	version   int
}

func setupGenerationAPIPlan(t *testing.T, fx *planAPIFixture, sess testSession) generationAPIPlan {
	t.Helper()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)
	pid, err := fx.deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := fx.deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{ProjectID: pid, Name: "generations", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	fx.drain(t)
	makeTask := func(title, assignee string) pm.TaskID {
		id, createErr := fx.deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: caller})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := fx.deps.PM.BatchUpdateTask(ctx, id, pmservice.BatchTaskPatch{Assignee: &assignee}, caller); updateErr != nil {
			t.Fatal(updateErr)
		}
		if selectErr := fx.deps.PM.SelectTaskIntoPlan(ctx, planID, id, caller); selectErr != nil {
			t.Fatal(selectErr)
		}
		return id
	}
	a := makeTask("A", "user:a1")
	b := makeTask("B", "user:b1")
	if err := fx.deps.PM.AddPlanDependency(ctx, planID, b, a, caller); err != nil {
		t.Fatal(err)
	}
	if err := fx.deps.PM.StartPlan(ctx, planID, caller); err != nil {
		t.Fatal(err)
	}
	if dispatched, err := fx.deps.PM.AdvancePlan(ctx, planID, caller); err != nil || len(dispatched) != 1 || dispatched[0] != a {
		t.Fatalf("AdvancePlan dispatched=%v err=%v want [%s]", dispatched, err, a)
	}
	plan, err := fx.deps.PM.GetPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	return generationAPIPlan{projectID: pid, planID: planID, a: a, b: b, g0: plan.ActiveGenerationID(), version: plan.Version()}
}

func TestPlanGenerationAPI_G0GnLineageSnapshotReplayAndStaleConflicts(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	fixture := setupGenerationAPIPlan(t, fx, sess)
	url := s.URL + "/api/projects/" + string(fixture.projectID) + "/plans/" + string(fixture.planID)

	g0Resp := orgScopedGet(t, url+"/generations", sess)
	if g0Resp.StatusCode != 200 {
		t.Fatalf("G0 generations status=%d body=%v", g0Resp.StatusCode, decodeBody(t, g0Resp))
	}
	g0Body := decodeBody(t, g0Resp)
	if g0Body["active_generation_id"] != string(fixture.g0) || g0Body["plan_version"].(float64) != float64(fixture.version) {
		t.Fatalf("G0 read identity/version=%v", g0Body)
	}
	g0Rows := g0Body["generations"].([]any)
	g0 := g0Rows[0].(map[string]any)
	if g0["id"] != string(fixture.g0) || g0["parent_generation_id"] != "" || g0["active"] != true {
		t.Fatalf("G0 row=%v", g0)
	}
	if len(g0["snapshot"].(map[string]any)["tasks"].([]any)) != 2 {
		t.Fatalf("G0 snapshot=%v", g0["snapshot"])
	}

	body := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"add focused verification","evidence":"review found an uncovered path","idempotency_key":"web-evolve-1","diff":{"node_decisions":[{"task_id":%q,"action":"preserve","reason":"already dispatched"}],"tasks":[{"ref":"c","title":"C","assignee_ref":"user:c1"}],"edges":[{"from":"c","to":%q,"kind":"seq"}]}}`, fixture.g0, fixture.version, fixture.a, fixture.a)
	first := orgScopedPost(t, url+"/evolution", body, sess)
	if first.StatusCode != 200 {
		t.Fatalf("evolution status=%d body=%v", first.StatusCode, decodeBody(t, first))
	}
	firstBody := decodeBody(t, first)
	if firstBody["duplicate"] != false || firstBody["active_generation_id"] == string(fixture.g0) {
		t.Fatalf("first evolution=%v", firstBody)
	}
	g1ID := firstBody["active_generation_id"].(string)
	generation := firstBody["generation"].(map[string]any)
	if generation["id"] != g1ID || generation["parent_generation_id"] != string(fixture.g0) {
		t.Fatalf("G1 lineage=%v", generation)
	}
	diff := generation["diff"].(map[string]any)
	if len(diff["node_decisions"].([]any)) != 1 || len(diff["tasks"].([]any)) != 1 || len(diff["edges"].([]any)) != 1 {
		t.Fatalf("G1 diff=%v", diff)
	}

	replay := orgScopedPost(t, url+"/evolution", body, sess)
	if replay.StatusCode != 200 {
		t.Fatalf("replay status=%d body=%v", replay.StatusCode, decodeBody(t, replay))
	}
	replayBody := decodeBody(t, replay)
	if replayBody["duplicate"] != true || replayBody["active_generation_id"] != g1ID {
		t.Fatalf("replay=%v want duplicate same generation", replayBody)
	}

	historyResp := orgScopedGet(t, url+"/generations", sess)
	history := decodeBody(t, historyResp)
	if history["active_generation_id"] != g1ID || len(history["generations"].([]any)) != 2 {
		t.Fatalf("history=%v", history)
	}
	ownedBy := map[string]string{}
	for _, raw := range history["nodes"].([]any) {
		node := raw.(map[string]any)
		ownedBy[node["task_id"].(string)] = node["generation_id"].(string)
	}
	if ownedBy[string(fixture.a)] != string(fixture.g0) || ownedBy[string(fixture.b)] != string(fixture.g0) {
		t.Fatalf("G0 node ownership=%v", ownedBy)
	}

	currentVersion := int(firstBody["version"].(float64))
	staleParent := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"stale parent","evidence":"old lineage","idempotency_key":"stale-parent","diff":{"node_decisions":[],"tasks":[],"edges":[]}}`, fixture.g0, currentVersion)
	if resp := orgScopedPost(t, url+"/evolution", staleParent, sess); resp.StatusCode != 409 {
		t.Fatalf("stale parent status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	staleVersion := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"stale version","evidence":"old version","idempotency_key":"stale-version","diff":{"node_decisions":[],"tasks":[],"edges":[]}}`, g1ID, fixture.version)
	if resp := orgScopedPost(t, url+"/evolution", staleVersion, sess); resp.StatusCode != 409 {
		t.Fatalf("stale version status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	idempotencyConflict := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"different payload","evidence":"different evidence","idempotency_key":"web-evolve-1","diff":{"node_decisions":[],"tasks":[],"edges":[]}}`, g1ID, currentVersion)
	if resp := orgScopedPost(t, url+"/evolution", idempotencyConflict, sess); resp.StatusCode != 409 {
		t.Fatalf("idempotency conflict status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	emptyEvidence := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"invalid","evidence":"","idempotency_key":"empty-evidence","diff":{"node_decisions":[],"tasks":[],"edges":[]}}`, g1ID, currentVersion)
	if resp := orgScopedPost(t, url+"/evolution", emptyEvidence, sess); resp.StatusCode != 400 {
		t.Fatalf("empty evidence status=%d want 400 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	incompleteDiff := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"invalid","evidence":"missing node decisions","idempotency_key":"incomplete-diff","diff":{"tasks":[],"edges":[]}}`, g1ID, currentVersion)
	if resp := orgScopedPost(t, url+"/evolution", incompleteDiff, sess); resp.StatusCode != 400 {
		t.Fatalf("incomplete diff status=%d want 400 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
}

func TestPlanGenerationAPI_InFlightConflictRejectsWholeRequest(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	fixture := setupGenerationAPIPlan(t, fx, sess)
	url := s.URL + "/api/projects/" + string(fixture.projectID) + "/plans/" + string(fixture.planID)
	body := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"illegal in-flight rewrite","evidence":"A already dispatched","idempotency_key":"web-inflight","diff":{"node_decisions":[],"tasks":[{"ref":"c","title":"must roll back","assignee_ref":"user:c1"}],"edges":[{"from":%q,"to":"c","kind":"seq"}]}}`, fixture.g0, fixture.version, fixture.a)
	resp := orgScopedPost(t, url+"/evolution", body, sess)
	if resp.StatusCode != 409 {
		t.Fatalf("in-flight status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}

	readResp := orgScopedGet(t, url+"/generations", sess)
	read := decodeBody(t, readResp)
	if read["active_generation_id"] != string(fixture.g0) || read["plan_version"].(float64) != float64(fixture.version) || len(read["generations"].([]any)) != 1 {
		t.Fatalf("rejected request changed ledger=%v", read)
	}
	tasks, err := fx.deps.PM.ListTasks(context.Background(), fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("rejected request left %d tasks, want original 2", len(tasks))
	}
	for _, task := range tasks {
		if task.Title() == "must roll back" {
			t.Fatalf("rejected request persisted task %s", task.ID())
		}
	}
}

func TestPlanGenerationAPI_EvolutionAtomicallyResolvesBlockedOnContext(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	fixture := setupGenerationAPIPlan(t, fx, sess)
	ctx := context.Background()
	plans := pmsql.NewPlanRepo(db)
	blocked := pm.BlockedOn{
		TaskID:           fixture.b,
		PlanID:           fixture.planID,
		WaitType:         pm.WaitUpstreamCompletion,
		WaitKeys:         []string{string(fixture.a)},
		TriggerCondition: "A completes",
		WaitedSince:      time.Now().UTC().Add(-time.Hour),
	}
	if err := plans.UpsertBlockedOn(ctx, blocked); err != nil {
		t.Fatal(err)
	}

	url := s.URL + "/api/projects/" + string(fixture.projectID) + "/plans/" + string(fixture.planID)
	body := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"replace blocked branch","evidence":"owner selected replacement","idempotency_key":"web-evolve-resolve-1","resolve_block_event_id":%q,"resolution_kind":"replace","resolution_note":"replace blocked downstream with verification node","diff":{"node_decisions":[{"task_id":%q,"action":"preserve","reason":"already dispatched"}],"tasks":[{"ref":"c","title":"C","assignee_ref":"user:c1"}],"edges":[{"from":"c","to":%q,"kind":"seq"}]}}`, fixture.g0, fixture.version, fixture.b, fixture.a, fixture.a)
	resp := orgScopedPost(t, url+"/evolution", body, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("evolution resolve status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	resolvedBody := decodeBody(t, resp)
	if resolvedBody["active_generation_id"] == string(fixture.g0) || resolvedBody["duplicate"] != false {
		t.Fatalf("evolution resolve body=%v", resolvedBody)
	}
	if _, ok, err := plans.GetBlockedOn(ctx, fixture.planID, fixture.b); err != nil || ok {
		t.Fatalf("blocked context after success ok=%v err=%v, want cleared", ok, err)
	}

	read := decodeBody(t, orgScopedGet(t, url+"/generations", sess))
	if len(read["generations"].([]any)) != 2 {
		t.Fatalf("generation count after success=%v", read)
	}
}

func TestPlanGenerationAPI_EvolutionResolutionRollbackOnConflict(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	fixture := setupGenerationAPIPlan(t, fx, sess)
	ctx := context.Background()
	plans := pmsql.NewPlanRepo(db)
	blocked := pm.BlockedOn{
		TaskID:           fixture.b,
		PlanID:           fixture.planID,
		WaitType:         pm.WaitUpstreamCompletion,
		WaitKeys:         []string{string(fixture.a)},
		TriggerCondition: "A completes",
		WaitedSince:      time.Now().UTC().Add(-time.Hour),
	}
	if err := plans.UpsertBlockedOn(ctx, blocked); err != nil {
		t.Fatal(err)
	}

	url := s.URL + "/api/projects/" + string(fixture.projectID) + "/plans/" + string(fixture.planID)
	body := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"bypass blocked branch","evidence":"owner accepted bypass","idempotency_key":"web-evolve-resolve-rollback","resolve_block_event_id":%q,"resolution_kind":"bypass","resolution_note":"bypass blocked branch","diff":{"node_decisions":[],"tasks":[{"ref":"c","title":"must roll back","assignee_ref":"user:c1"}],"edges":[{"from":%q,"to":"c","kind":"seq"}]}}`, fixture.g0, fixture.version, fixture.b, fixture.a)
	resp := orgScopedPost(t, url+"/evolution", body, sess)
	if resp.StatusCode != 409 {
		t.Fatalf("conflict status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	if _, ok, err := plans.GetBlockedOn(ctx, fixture.planID, fixture.b); err != nil || !ok {
		t.Fatalf("blocked context after rollback ok=%v err=%v, want preserved", ok, err)
	}
	read := decodeBody(t, orgScopedGet(t, url+"/generations", sess))
	if read["active_generation_id"] != string(fixture.g0) || len(read["generations"].([]any)) != 1 {
		t.Fatalf("failed request changed ledger=%v", read)
	}
	tasks, err := fx.deps.PM.ListTasks(ctx, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("failed request left %d tasks, want original 2", len(tasks))
	}
}

func TestPlanGenerationAPI_NonOwnerCannotCommitEvolution(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.Authorizer = authz.New(authz.Deps{DB: db, Mode: authz.EnforcementEnforce})
	owner := setupTestSession(t, db, deps)
	member := addOrgMemberSession(t, db, owner, identity.RoleMember, "plan-non-owner")
	fx := setupPlanGraphAPI(t, deps)
	s := newTestServer(t, fx.deps)
	defer s.Close()
	fixture := setupGenerationAPIPlan(t, fx, owner)
	if _, err := fx.deps.PM.AddProjectMember(context.Background(), pmservice.AddProjectMemberCommand{
		ProjectID:  fixture.projectID,
		IdentityID: pm.IdentityRef("user:" + member.IdentityID),
		Actor:      pm.IdentityRef("user:" + owner.IdentityID),
	}); err != nil {
		t.Fatal(err)
	}

	url := s.URL + "/api/projects/" + string(fixture.projectID) + "/plans/" + string(fixture.planID)
	body := fmt.Sprintf(`{"parent_generation_id":%q,"base_version":%d,"reason":"member tries evolution","evidence":"should be owner gated","idempotency_key":"web-evolve-403","diff":{"node_decisions":[],"tasks":[],"edges":[]}}`, fixture.g0, fixture.version)
	resp := orgScopedPost(t, url+"/evolution", body, member)
	if resp.StatusCode != 403 {
		t.Fatalf("non-owner evolution status=%d want 403 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	read := decodeBody(t, orgScopedGet(t, url+"/generations", owner))
	if read["active_generation_id"] != string(fixture.g0) || len(read["generations"].([]any)) != 1 {
		t.Fatalf("non-owner request changed ledger=%v", read)
	}
}
