package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
)

// =============================================================================
// v2.9 P3 Stage C — agent MCP Plan passthrough tools (create_plan,
// add_task_to_plan, remove_task_from_plan, add_plan_dependency,
// remove_plan_dependency, start_plan, stop_plan, get_plan, list_plans).
//
// These reuse the writeToolsFixture (now Plan-capable: Plans repo + a REAL
// PlanDispatcher + plan participant projector). The tests assert the PASSTHROUGH
// WIRING — args parsed, the right pm AppService called with actor=agent, plan
// domain errors mapped, and the requireAgentOnWorker guardrail enforced — NOT the
// plan domain itself (covered in internal/projectmanager).
// =============================================================================

// seedPlanMember creates a project + draft Plan as the AGENT (actor=agent:AG1, a
// member via #5a after seedMemberProject), draining the relay so the plan
// conversation exists. Returns the project id + plan id.
func (f *writeToolsFixture) seedPlanMember(t *testing.T) (pm.ProjectID, string) {
	t.Helper()
	ctx := context.Background()
	pid, _ := f.seedMemberProject(t) // AG1 is now a member of pid.
	planID, err := f.pmSvc.CreatePlan(ctx, pmservice.CreatePlanCommand{
		ProjectID: pid, Name: "Plan A", CreatedBy: pm.IdentityRef("agent:" + atAgent1), OwnerRef: pm.IdentityRef("agent:" + atAgent1),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	return pid, string(planID)
}

// seedPlanTask creates a fresh task in pid, assigns it to AG1 (so a started plan's
// §9.6c assignee check passes), selects it into the plan, and drains. Returns tid.
func (f *writeToolsFixture) seedPlanTask(t *testing.T, pid pm.ProjectID, planID string) string {
	t.Helper()
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "node", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.AssignTask(ctx, tid, pm.IdentityRef("agent:"+atAgent1), owner); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.SelectTaskIntoPlan(ctx, pm.PlanID(planID), tid, pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	return string(tid)
}

func createPlanRuleTeam(t *testing.T, baseURL string) string {
	t.Helper()
	st, body := postBearer(t, baseURL, "/admin/agent-tools/create_team", "acat_w1", map[string]any{
		"agent_id": atAgent1, "name": "plan-rules-team", "roles": []map[string]any{{"role": "dev"}},
	})
	if st != http.StatusCreated {
		t.Fatalf("create_team status=%d body=%v", st, body)
	}
	teamID, _ := body["id"].(string)
	if teamID == "" {
		t.Fatalf("create_team returned no id: %v", body)
	}
	st, body = postBearer(t, baseURL, "/admin/agent-tools/add_member", "acat_w1", map[string]any{
		"agent_id": atAgent1, "team_id": teamID, "member_ref": "agent:" + atAgent1, "role": "dev",
	})
	if st != http.StatusCreated {
		t.Fatalf("add_member status=%d body=%v", st, body)
	}
	return teamID
}

func planAuditDetail(t *testing.T, f *writeToolsFixture, planID string, change pm.AuditChangeType) map[string]any {
	t.Helper()
	entries, _, err := f.pmSvc.ListObjectAudit(context.Background(), pm.AuditObjectPlan, planID, "", 0)
	if err != nil {
		t.Fatalf("ListObjectAudit: %v", err)
	}
	for _, e := range entries {
		if e.ChangeType != change {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal([]byte(e.Detail), &detail); err != nil {
			t.Fatalf("audit detail decode: %v; detail=%s", err, e.Detail)
		}
		return detail
	}
	t.Fatalf("missing plan audit change %s; entries=%+v", change, entries)
	return nil
}

func pushMalformedPlanRule(t *testing.T, gitHost *centergit.Host, teamID string) {
	t.Helper()
	bareDir, err := gitHost.RepoDir(centergit.TeamRepo(teamID))
	if err != nil {
		t.Fatalf("team repo dir: %v", err)
	}
	work := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("clone", bareDir, "repo")
	repoDir := filepath.Join(work, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, "rules"), 0o700); err != nil {
		t.Fatalf("mkdir rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "rules", "bad.md"), []byte("not frontmatter\n"), 0o600); err != nil {
		t.Fatalf("write bad rule: %v", err)
	}
	cmd := exec.Command("git", "-C", repoDir, "add", "rules/bad.md")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add bad rule: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-C", repoDir, "-c", "user.name=agent-center", "-c", "user.email=test@example.invalid", "commit", "-m", "add malformed rule")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit bad rule: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-C", repoDir, "push", "origin", "HEAD:main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git push bad rule: %v\n%s", err, out)
	}
}

// --- create_plan -------------------------------------------------------------

func TestCreatePlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "My Plan", "description": "d", "owner_ref": "agent:" + atAgent1})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	planID, _ := body["plan_id"].(string)
	if planID == "" {
		t.Fatalf("no plan_id in body: %v", body)
	}
	// The plan exists with actor=agent as creator.
	p, err := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(p.CreatorRef()); got != "agent:"+atAgent1 {
		t.Fatalf("creator_ref = %q, want agent:%s", got, atAgent1)
	}
	if p.Status() != pm.PlanPending {
		t.Fatalf("status = %s, want draft", p.Status())
	}
}

func TestCreatePlan_AutoLoadsPlanTeamRules_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv, gitHost := wireTeam(t, f)
	pid, _ := f.seedMemberProject(t)
	teamID := createPlanRuleTeam(t, srv.URL)

	written, err := centergit.NewTeamMemoryProducer(gitHost, nil).SeedTeam(t.Context(), teamID, nil, []centergit.Rule{
		{Slug: "plan-shape", Description: "shape the DAG", Body: "Keep the plan DAG explicit.", Enabled: true, AppliesTo: []string{"plan"}},
		{Slug: "execute-only", Description: "execute only", Body: "Do not load for plan.", Enabled: true, AppliesTo: []string{"execute"}},
	})
	if err != nil {
		t.Fatalf("SeedTeam rules: %v", err)
	}
	if written != 2 {
		t.Fatalf("SeedTeam wrote %d rules, want 2", written)
	}

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "Rules Plan", "owner_ref": "agent:" + atAgent1})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	planID, _ := body["plan_id"].(string)
	rulesView, _ := body["team_rules"].(map[string]any)
	if rulesView["team_id"] != teamID || rulesView["phase"] != "plan" || rulesView["source"] != "team_memory" {
		t.Fatalf("team_rules metadata = %v", rulesView)
	}
	if commit, _ := rulesView["commit"].(string); commit == "" {
		t.Fatalf("team_rules missing commit: %v", rulesView)
	}
	if got := rulesView["refresh_semantics"].(string); !strings.Contains(got, "once per MCP planning session") {
		t.Fatalf("team_rules refresh_semantics = %q", got)
	}
	rules, _ := rulesView["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("team_rules rules=%v want one phase=plan rule", rulesView["rules"])
	}
	rule, _ := rules[0].(map[string]any)
	if rule["slug"] != "plan-shape" || rule["body"] != nil || rule["body_bytes"] == nil || rule["source_path"] == "" {
		t.Fatalf("unexpected plan rule payload: %v", rule)
	}

	detail := planAuditDetail(t, f, planID, pm.AuditPlanCreated)
	auditRules, _ := detail["team_rules"].(map[string]any)
	if auditRules["team_id"] != teamID || auditRules["commit"] != rulesView["commit"] {
		t.Fatalf("audit team_rules = %v, response = %v", auditRules, rulesView)
	}
	auditList, _ := auditRules["rules"].([]any)
	if len(auditList) != 1 {
		t.Fatalf("audit rules = %v", auditRules["rules"])
	}
	auditRule, _ := auditList[0].(map[string]any)
	if auditRule["body"] != nil || auditRule["body_bytes"] == nil {
		t.Fatalf("audit rules = %v", auditRules["rules"])
	}
}

func TestEditPlanTopology_UsesFrozenPlanningRulesFromRequest(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid, err := f.pmSvc.CreateTask(context.Background(), pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "new node", CreatedBy: pm.IdentityRef("user:owner"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	srv := f.server(t)
	p, err := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if err != nil {
		t.Fatal(err)
	}

	frozen := map[string]any{
		"team_id":             "team-frozen",
		"phase":               "plan",
		"commit":              "frozen-commit",
		"source":              "mcp_plan_tool",
		"planning_session_id": "agent:agent-1/generation:9",
		"planning_generation": 9,
		"refresh_semantics":   "frozen for this MCP planning session",
		"rules": []map[string]any{{
			"slug": "frozen-plan", "description": "frozen", "body": "Use the frozen snapshot.", "enabled": true,
			"applies_to": []string{"plan"}, "source_path": "rules/frozen-plan.md",
		}},
		"skipped_nonstandard": []string{},
	}
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/edit_plan_topology", "acat_w1", map[string]any{
		"agent_id": atAgent1, "plan_id": planID, "base_version": p.Version(),
		"ops":            []map[string]any{{"op": "add_node", "task_id": string(tid)}},
		"planning_rules": frozen,
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	rulesView, _ := body["team_rules"].(map[string]any)
	if rulesView["commit"] != "frozen-commit" || rulesView["source"] != "mcp_plan_tool" || rulesView["planning_generation"] != float64(9) {
		t.Fatalf("response team_rules = %v", rulesView)
	}
	detail := planAuditDetail(t, f, planID, pm.AuditPlanTopologyCommit)
	auditRules, _ := detail["team_rules"].(map[string]any)
	if auditRules["commit"] != "frozen-commit" || auditRules["source"] != "mcp_plan_tool" {
		t.Fatalf("audit team_rules = %v", auditRules)
	}
	auditRuleList, _ := auditRules["rules"].([]any)
	if len(auditRuleList) != 1 {
		t.Fatalf("audit rule list = %v", auditRules["rules"])
	}
	if _, leaked := auditRuleList[0].(map[string]any)["body"]; leaked {
		t.Fatalf("audit rule leaked body = %v", auditRuleList[0])
	}
}

func TestEvolvePlanGeneration_AsMemberIdempotentAPI(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.StartPlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	f.drain(t)
	if err := f.pmSvc.PausePlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatalf("PausePlan: %v", err)
	}
	srv := f.server(t)
	p, err := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if err != nil {
		t.Fatal(err)
	}
	if p.ActiveGenerationID() == "" {
		t.Fatal("started plan has no G0 active generation")
	}

	req := map[string]any{
		"agent_id": atAgent1, "plan_id": planID, "parent_generation_id": string(p.ActiveGenerationID()),
		"base_version": p.Version(), "idempotency_key": "api-evo-1",
		"reason": "add api task", "evidence": "route test",
		"diff": map[string]any{
			"node_decisions": []map[string]any{},
			"tasks":          []map[string]any{{"ref": "api-c", "title": "API C", "assignee_ref": "agent:" + atAgent1, "delivery_contract": "code_change", "detached": true}},
			"edges":          []map[string]any{},
		},
	}
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/evolve_plan_generation", "acat_w1", req)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["duplicate"] != false {
		t.Fatalf("duplicate = %v, want false", body["duplicate"])
	}
	gen, _ := body["generation"].(map[string]any)
	genID, _ := gen["id"].(string)
	if genID == "" || gen["creator_ref"] != "agent:"+atAgent1 || gen["reason"] != "add api task" {
		t.Fatalf("generation response = %v", gen)
	}
	if dispatched, _ := body["dispatched"].([]any); len(dispatched) != 0 {
		t.Fatalf("pending evolution dispatched %v, want none", dispatched)
	}
	plans := pmsql.NewPlanRepo(f.db)
	reloadedPlan, err := plans.FindByID(context.Background(), pm.PlanID(planID))
	if err != nil {
		t.Fatal(err)
	}
	if reloadedPlan.ActiveGenerationID() != pm.PlanGenerationID(genID) || reloadedPlan.Version() != p.Version()+1 {
		t.Fatalf("active generation/version = %s/%d, want %s/%d", reloadedPlan.ActiveGenerationID(), reloadedPlan.Version(), genID, p.Version()+1)
	}
	stored, err := plans.FindGenerationByID(context.Background(), pm.PlanGenerationID(genID))
	if err != nil {
		t.Fatal(err)
	}
	var apiTaskFound bool
	for _, task := range stored.Snapshot.Tasks {
		if task.Title == "API C" {
			apiTaskFound = true
		}
	}
	if stored.CreatorRef != pm.IdentityRef("agent:"+atAgent1) || len(stored.Snapshot.Tasks) != 2 || !apiTaskFound {
		t.Fatalf("stored generation = %+v", stored)
	}

	status, dupBody := postBearer(t, srv.URL, "/admin/agent-tools/evolve_plan_generation", "acat_w1", req)
	if status != http.StatusOK {
		t.Fatalf("duplicate status = %d, want 200; body = %v", status, dupBody)
	}
	dupGen, _ := dupBody["generation"].(map[string]any)
	if dupBody["duplicate"] != true || dupGen["id"] != genID {
		t.Fatalf("duplicate body = %v, want same generation %s", dupBody, genID)
	}

	req["evidence"] = "changed with same idempotency key"
	status, conflict := postBearer(t, srv.URL, "/admin/agent-tools/evolve_plan_generation", "acat_w1", req)
	if status != http.StatusConflict || conflict["error"] != "plan_conflict" {
		t.Fatalf("idempotency conflict status=%d body=%v, want 409 plan_conflict", status, conflict)
	}
}

func TestCreatePlan_TeamRuleIsolation_NoTeamNoRepoCrossOrgAndBadRules(t *testing.T) {
	t.Run("no_team", func(t *testing.T) {
		f := newWriteToolsFixture(t)
		f.addWorkerToken(t, "acat_w1", atWorker1)
		srv, _ := wireTeam(t, f)
		pid, _ := f.seedMemberProject(t)
		status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
			map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "No Team", "owner_ref": "agent:" + atAgent1})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%v", status, body)
		}
		rulesView := body["team_rules"].(map[string]any)
		if rulesView["team_id"] != "" || len(rulesView["rules"].([]any)) != 0 {
			t.Fatalf("no_team team_rules = %v", rulesView)
		}
	})

	t.Run("no_repo", func(t *testing.T) {
		f := newWriteToolsFixture(t)
		f.addWorkerToken(t, "acat_w1", atWorker1)
		srv, _ := wireTeam(t, f)
		pid, _ := f.seedMemberProject(t)
		teamID := createPlanRuleTeam(t, srv.URL)
		status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
			map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "No Repo", "owner_ref": "agent:" + atAgent1})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%v", status, body)
		}
		rulesView := body["team_rules"].(map[string]any)
		if rulesView["team_id"] != teamID || rulesView["commit"] != "" || len(rulesView["rules"].([]any)) != 0 {
			t.Fatalf("no_repo team_rules = %v", rulesView)
		}
	})

	t.Run("cross_org", func(t *testing.T) {
		f := newWriteToolsFixture(t)
		f.addWorkerToken(t, "acat_w1", atWorker1)
		srv, _ := wireTeam(t, f)
		pid, _ := f.seedMemberProject(t)
		foreign, err := f.deps.TeamSvc.CreateTeam(t.Context(), teamservice.CreateTeamInput{
			OrgID: "org-foreign", Name: "foreign", Roles: []team.RoleConfig{{Role: "dev"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.deps.TeamSvc.AddMember(t.Context(), foreign.ID(), team.MemberRef("agent:"+atAgent1), "dev"); err != nil {
			t.Fatal(err)
		}
		status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
			map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "Cross Org", "owner_ref": "agent:" + atAgent1})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%v", status, body)
		}
		rulesView := body["team_rules"].(map[string]any)
		if rulesView["team_id"] != "" || rulesView["commit"] != "" || len(rulesView["rules"].([]any)) != 0 {
			t.Fatalf("cross_org leaked foreign team data: %v", rulesView)
		}
	})

	t.Run("bad_rules", func(t *testing.T) {
		f := newWriteToolsFixture(t)
		f.addWorkerToken(t, "acat_w1", atWorker1)
		srv, gitHost := wireTeam(t, f)
		pid, _ := f.seedMemberProject(t)
		teamID := createPlanRuleTeam(t, srv.URL)
		if _, err := centergit.NewTeamMemoryProducer(gitHost, nil).SeedTeam(t.Context(), teamID, nil, []centergit.Rule{
			{Slug: "good-plan", Description: "good", Body: "Keep going.", Enabled: true, AppliesTo: []string{"plan"}},
		}); err != nil {
			t.Fatalf("SeedTeam: %v", err)
		}
		pushMalformedPlanRule(t, gitHost, teamID)
		status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
			map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "Bad Rule", "owner_ref": "agent:" + atAgent1})
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%v", status, body)
		}
		rulesView := body["team_rules"].(map[string]any)
		if len(rulesView["rules"].([]any)) != 1 || len(rulesView["skipped_nonstandard"].([]any)) != 1 {
			t.Fatalf("bad_rules team_rules = %v", rulesView)
		}
	})
}

func TestCreatePlan_ForeignProject_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t) // AG1 resolves (is a member somewhere).
	pid := f.seedForeignProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "name": "nope", "owner_ref": "agent:" + atAgent1})
	// requireProjectMember in the AppService → ErrNotMember → 403.
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (pm ErrNotMember); body = %v", status, body)
	}
}

func TestCreatePlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)
	// W1 token operating AG2 (bound to W2) → guardrail 403, no AppService call.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "project_id": string(pid), "name": "x"})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

func TestCreateStage_RejectsMissingHumanGateContract(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_stage", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "name": "Acceptance", "evaluator_kind": "human"})
	if status != http.StatusBadRequest || body["error"] != "missing_gate_contract" {
		t.Fatalf("status=%d body=%v, want 400 missing_gate_contract", status, body)
	}
}

func TestCreateStage_PersistsHumanGateContract(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_stage", "acat_w1", map[string]any{
		"agent_id": atAgent1, "plan_id": planID, "name": "Acceptance",
		"evaluator_kind": "human", "assignee_ref": "agent:" + atAgent1,
		"role_ref": "reviewer", "acceptance_contract": "Verify API, DB, and browser evidence.",
		"pass_route": "downstream", "reject_route": "reopen_stage", "exhausted_route": "escalate",
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	detail, err := f.pmSvc.GetStage(context.Background(), pm.StageID(body["stage_id"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	if got := detail.Stage.GateSpec(); got.AcceptanceContract != "Verify API, DB, and browser evidence." || got.RoleRef != "reviewer" {
		t.Fatalf("gate spec = %+v", got)
	}
}

// --- add_task_to_plan / remove_task_from_plan --------------------------------

func TestAddTaskToPlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid, _ := f.pmSvc.CreateTask(context.Background(), pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "to select", CreatedBy: pm.IdentityRef("user:owner"),
	})
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/add_task_to_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "task_id": string(tid)})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	tk, _ := f.pmSvc.GetTask(context.Background(), tid)
	if string(tk.PlanID()) != planID {
		t.Fatalf("task plan_id = %q, want %s", tk.PlanID(), planID)
	}
}

func TestRemoveTaskFromPlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID) // selected into the plan.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/remove_task_from_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "task_id": tid})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	tk, _ := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if tk.PlanID() != "" {
		t.Fatalf("task plan_id = %q, want empty after removal", tk.PlanID())
	}
}

func TestAddTaskToPlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/add_task_to_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "plan_id": planID, "task_id": "T-x"})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- add_plan_dependency / remove_plan_dependency ----------------------------

func TestAddPlanDependency_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	from := f.seedPlanTask(t, pid, planID)
	to := f.seedPlanTask(t, pid, planID)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/add_plan_dependency", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "from_task_id": from, "to_task_id": to})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
}

// TestAddPlanDependency_Cycle_Surfaced: from→to then to→from would cycle →
// ErrPlanCycle must be surfaced as a tool error (422 invalid_transition).
func TestAddPlanDependency_Cycle_Surfaced(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	a := f.seedPlanTask(t, pid, planID)
	b := f.seedPlanTask(t, pid, planID)
	// a depends_on b (legal).
	if err := f.pmSvc.AddPlanDependency(context.Background(), pm.PlanID(planID),
		pm.TaskID(a), pm.TaskID(b), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)
	// b depends_on a → cycle.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/add_plan_dependency", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "from_task_id": b, "to_task_id": a})
	if status != http.StatusUnprocessableEntity || body["error"] != "invalid_transition" {
		t.Fatalf("status = %d err=%v, want 422 invalid_transition (ErrPlanCycle)", status, body["error"])
	}
}

func TestRemovePlanDependency_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	from := f.seedPlanTask(t, pid, planID)
	to := f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.AddPlanDependency(context.Background(), pm.PlanID(planID),
		pm.TaskID(from), pm.TaskID(to), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/remove_plan_dependency", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID, "from_task_id": from, "to_task_id": to})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
}

// --- start_plan / pause_plan / resume_plan / discard_plan -------------------

func TestStartPlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID) // ≥1 task, assigned to AG1 (resolvable).
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/start_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d body=%v, want 200 ok=true", status, body)
	}
	p, _ := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if p.Status() != pm.PlanRunning {
		t.Fatalf("status = %s, want running", p.Status())
	}
}

// TestStartPlan_NoTasks_Surfaced: starting an empty draft plan → ErrPlanNoTasks
// surfaced as a tool error (422). Exercises start-validation passthrough.
func TestStartPlan_NoTasks_Surfaced(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t) // no tasks selected.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/start_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusUnprocessableEntity || body["error"] != "invalid_transition" {
		t.Fatalf("status = %d err=%v, want 422 invalid_transition (ErrPlanNoTasks)", status, body["error"])
	}
}

func TestPauseResumeDiscardPlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.StartPlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/pause_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["ok"] != true || body["status"] != "paused" {
		t.Fatalf("pause status = %d body=%v", status, body)
	}
	p, _ := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if p.Status() != pm.PlanPaused {
		t.Fatalf("status = %s, want paused", p.Status())
	}

	status, body = postBearer(t, srv.URL, "/admin/agent-tools/resume_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["status"] != "running" {
		t.Fatalf("resume status = %d body=%v", status, body)
	}

	status, body = postBearer(t, srv.URL, "/admin/agent-tools/discard_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["status"] != "discarded" {
		t.Fatalf("discard status = %d body=%v", status, body)
	}
	p, _ = f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if p.Status() != pm.PlanDiscarded {
		t.Fatalf("status = %s, want discarded", p.Status())
	}
}

func TestReopenPlan_DoneAsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID)
	actor := pm.IdentityRef("agent:" + atAgent1)
	if err := f.pmSvc.StartPlan(context.Background(), pm.PlanID(planID), actor); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.SetTaskStatus(context.Background(), pm.TaskID(tid), pm.TaskCompleted, actor); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if err := f.pmSvc.CompletePlan(context.Background(), pm.PlanID(planID), actor); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/reopen_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["ok"] != true || body["status"] != "paused" {
		t.Fatalf("reopen status = %d body=%v", status, body)
	}
	p, _ := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if p.Status() != pm.PlanPaused {
		t.Fatalf("status = %s, want paused", p.Status())
	}
}

func TestStartPlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/start_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "plan_id": planID})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- delete_plan -------------------------------------------------------------

func TestDeletePlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID) // a task selected into the plan.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/delete_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK || body["deleted"] != true {
		t.Fatalf("status = %d body=%v, want 200 deleted=true", status, body)
	}
	// The plan is gone.
	if _, err := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID)); err == nil {
		t.Fatalf("plan still exists after delete")
	}
	// The task is unloaded back to the backlog (not deleted), plan_id cleared.
	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatalf("task should survive plan delete: %v", err)
	}
	if tk.PlanID() != "" {
		t.Fatalf("task plan_id = %q, want empty after plan delete", tk.PlanID())
	}
}

// TestDeletePlan_Running_Surfaced: a RUNNING plan can't be deleted → ErrPlanRunning
// surfaced as 409 plan_conflict.
func TestDeletePlan_Running_Surfaced(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.StartPlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/delete_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusConflict || body["error"] != "plan_conflict" {
		t.Fatalf("status = %d err=%v, want 409 plan_conflict (ErrPlanRunning)", status, body["error"])
	}
}

func TestDeletePlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/delete_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "plan_id": planID})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- archive_plan ------------------------------------------------------------

func TestArchivePlan_AsMember_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	tid := f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.DiscardPlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/archive_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	// Returns the archived Plan detail (same shape get_plan emits).
	if body["id"] != planID || body["status"] != string(pm.PlanDiscarded) || body["archived_at"] == nil {
		t.Fatalf("archive body id/status/archived_at = %v/%v/%v", body["id"], body["status"], body["archived_at"])
	}
	p, err := f.pmSvc.GetPlan(context.Background(), pm.PlanID(planID))
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != pm.PlanDiscarded || !p.IsArchived() {
		t.Fatalf("plan status = %s archived=%v", p.Status(), p.IsArchived())
	}
	// Plan archive is orthogonal and does not cascade Task archive markers.
	tk, _ := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if tk.IsArchived() || tk.Status() != pm.TaskDiscarded {
		t.Fatalf("task archive/status = %v/%s", tk.IsArchived(), tk.Status())
	}
}

// TestArchivePlan_Running_Surfaced: a RUNNING plan can't be archived → ErrPlanRunning
// surfaced as 409 plan_conflict.
func TestArchivePlan_Running_Surfaced(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	if err := f.pmSvc.StartPlan(context.Background(), pm.PlanID(planID), pm.IdentityRef("agent:"+atAgent1)); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/archive_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "plan_id": planID})
	if status != http.StatusConflict || body["error"] != "plan_conflict" {
		t.Fatalf("status = %d err=%v, want 409 plan_conflict (ErrPlanNotTerminal)", status, body["error"])
	}
}

func TestArchivePlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/archive_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "plan_id": planID})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- get_plan ----------------------------------------------------------------

func TestGetPlan_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	f.seedPlanTask(t, pid, planID)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "plan_id": planID})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if body["id"] != planID {
		t.Fatalf("id = %v, want %s", body["id"], planID)
	}
	// Spot-check the DERIVED Plan DTO shape (nodes + ready_set + has_failed + progress).
	for _, k := range []string{"project_id", "name", "status", "nodes", "ready_set", "has_failed", "progress"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("plan detail missing %q: %v", k, body)
		}
	}
}

// TestGetPlan_WrongProject_404: a plan named under the wrong project is not found.
func TestGetPlan_WrongProject_404(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, planID := f.seedPlanMember(t)
	other := f.seedForeignProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(other), "plan_id": planID})
	if status != http.StatusNotFound || body["error"] != "not_found" {
		t.Fatalf("status = %d err=%v, want 404 not_found (plan not in project)", status, body["error"])
	}
}

// TestGetPlan_NonMember_403: issue I44 — get_plan now enforces caller membership
// (GetPlanDetailForMember), closing the prior gap where only a plan-in-project name
// match was checked (any agent on a worker could read a plan whose project_id +
// plan_id it could name). An agent that is NOT a member of the plan's project gets
// 403 (ErrNotMember → terminal), even when it names the plan's true project_id.
func TestGetPlan_NonMember_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedMemberProject(t) // AG1 resolves (member of SOME project)…
	// …but the plan lives in a project AG1 is NOT a member of.
	foreign := f.seedForeignProject(t)
	planID, err := f.pmSvc.CreatePlan(context.Background(), pmservice.CreatePlanCommand{
		ProjectID: foreign, Name: "secret plan", CreatedBy: pm.IdentityRef("user:other"), OwnerRef: pm.IdentityRef("user:other"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_plan", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(foreign), "plan_id": string(planID)})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d body=%v, want 403 (non-member may not read a foreign project's plan)", status, body)
	}
}

func TestGetPlan_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, planID := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_plan", "acat_w1",
		map[string]any{"agent_id": atAgent2, "project_id": string(pid), "plan_id": planID})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- list_plans --------------------------------------------------------------

func TestListPlans_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedPlanMember(t) // one plan in pid.
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_plans", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid)})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	plans, _ := body["plans"].([]any)
	// "Plan A" + the ADR-0047 auto-created "[Built-in]" pool.
	if len(plans) != 2 {
		t.Fatalf("plans len = %d, want 2; body = %v", len(plans), body)
	}
	// Locate the structured "Plan A" row (not the built-in pool).
	var row map[string]any
	for _, p := range plans {
		m, _ := p.(map[string]any)
		if m["name"] == "Plan A" {
			row = m
		}
	}
	if row == nil {
		t.Fatalf("Plan A missing from list: %v", plans)
	}
	for _, k := range []string{"id", "name", "status", "progress", "has_failed", "node_count", "nodes_preview"} {
		if _, ok := row[k]; !ok {
			t.Fatalf("plan summary missing %q: %v", k, row)
		}
	}
}

func TestListPlans_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedPlanMember(t)
	srv := f.server(t)
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/list_plans", "acat_w1",
		map[string]any{"agent_id": atAgent2, "project_id": string(pid)})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// TestListPlans_Pagination verifies the page window + total/has_more + cap for
// list_plans. The page is applied to the plan rows before view derivation; the
// builtin pool plan (if any) counts toward total, so the test pages through and
// asserts the unique ids collected equal the reported total.
func TestListPlans_Pagination(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := f.pmSvc.CreatePlan(ctx, pmservice.CreatePlanCommand{
			ProjectID: pid, Name: "P", CreatedBy: pm.IdentityRef("agent:" + atAgent1), OwnerRef: pm.IdentityRef("agent:" + atAgent1),
		}); err != nil {
			t.Fatal(err)
		}
		f.drain(t)
	}
	srv := f.server(t)

	call := func(body map[string]any) map[string]any {
		status, resp := postBearer(t, srv.URL, "/admin/agent-tools/list_plans", "acat_w1", body)
		if status != http.StatusOK {
			t.Fatalf("list_plans status=%d body=%v", status, resp)
		}
		return resp
	}
	ids := func(resp map[string]any) []string {
		raw, _ := resp["plans"].([]any)
		out := make([]string, 0, len(raw))
		for _, x := range raw {
			out = append(out, x.(map[string]any)["id"].(string))
		}
		return out
	}

	p1 := call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 2})
	total := int(p1["total"].(float64))
	if total < 4 { // the 4 created (plus possibly the builtin pool)
		t.Fatalf("plans total=%d want >=4", total)
	}
	if int(p1["page_size"].(float64)) != 2 {
		t.Fatalf("plans page_size=%v want 2", p1["page_size"])
	}
	// Page through; the unique ids collected must equal the reported total (no dups).
	seen := map[string]bool{}
	for off := 0; off < total; off += 2 {
		for _, id := range ids(call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 2, "offset": off})) {
			if seen[id] {
				t.Fatalf("duplicate plan id across pages: %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != total {
		t.Fatalf("paged unique plans=%d want %d", len(seen), total)
	}
	capped := call(map[string]any{"agent_id": atAgent1, "project_id": string(pid), "page_size": 100000})
	if int(capped["page_size"].(float64)) != agentListMaxPageSize {
		t.Fatalf("plans page_size cap=%v want %d", capped["page_size"], agentListMaxPageSize)
	}
}
