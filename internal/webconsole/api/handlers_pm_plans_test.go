package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
		DB:             db,
		Projects:       pmsql.NewProjectRepo(db),
		Members:        pmsql.NewProjectMemberRepo(db),
		Issues:         pmsql.NewIssueRepo(db),
		Tasks:          pmsql.NewTaskRepo(db),
		TaskSubs:       pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:      pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs:   pmsql.NewCodeRepoRefRepo(db),
		Plans:          plans,
		Outbox:         ob,
		IDGen:          gen,
		Clock:          clk,
		AgentDir:       allAgentsDir{},
		PlanDispatcher: convservice.NewPlanDispatchAdapter(deps.MessageWriter),
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
