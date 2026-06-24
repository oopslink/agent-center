package api

import (
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// T461: find_org_agent enriches each roster row with profile (capability tags),
// status (lifecycle + bound-worker liveness), and load (non-terminal task counts
// by assignee) so the PD can dispatch to a capable AND least-busy agent in one
// call. This is the root-fix for uneven assignment.
func TestFindOrgAgent_Enriched_T461(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)

	// Give AG1 dispatch tags (load → set → persist via the repo round-trip).
	a1, err := f.agents.FindByID(t.Context(), atAgent1)
	if err != nil {
		t.Fatal(err)
	}
	a1.SetCapabilityTags([]string{"FE", "platform", "fe"}, atNow) // "fe" dup dropped
	if err := f.agents.Update(t.Context(), a1); err != nil {
		t.Fatal(err)
	}

	// Wire a real PMService with a Tasks repo + seed AG1's load: 1 running + 2 open
	// → load {running:1, open:2, total:3}. (A unique index caps an agent at one
	// non-blocked running task, migration 0072.) AG1 has no identity-member id, so
	// its assignee ref is "agent:AG1".
	taskRepo := pmsql.NewTaskRepo(f.db)
	const ag1Ref = pm.IdentityRef("agent:" + atAgent1)
	seedTask := func(id string, running bool) {
		tk, terr := pm.NewTask(pm.NewTaskInput{
			ID: pm.TaskID(id), ProjectID: "proj-1", Title: id, CreatedBy: "system", CreatedAt: atNow,
		})
		if terr != nil {
			t.Fatal(terr)
		}
		if terr := tk.Assign(ag1Ref, atNow); terr != nil {
			t.Fatal(terr)
		}
		if running {
			if terr := tk.Start(atNow); terr != nil {
				t.Fatal(terr)
			}
		}
		if terr := taskRepo.Save(t.Context(), tk); terr != nil {
			t.Fatal(terr)
		}
	}
	seedTask("task-r1", true)
	seedTask("task-o1", false)
	seedTask("task-o2", false)
	f.deps.PMService = pmservice.New(pmservice.Deps{Tasks: taskRepo})

	s := f.server(t)
	status, body := postBearer(t, s.URL, "/admin/agent-tools/find_org_agent", "acat_w1",
		map[string]any{"agent_id": atAgent1, "name": "AG1"})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	agents, _ := body["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("want 1 agent named AG1, got %v", body["agents"])
	}
	row := agents[0].(map[string]any)

	// Back-compat top-level fields preserved.
	if row["id"] != atAgent1 || row["name"] != atAgent1 || row["assignee_ref"] != "agent:"+atAgent1 {
		t.Fatalf("back-compat fields wrong: %v", row)
	}

	// profile: display_name + agent_ref + capability_tags (dedup, order preserved).
	prof, _ := row["profile"].(map[string]any)
	if prof == nil {
		t.Fatalf("missing profile: %v", row)
	}
	if prof["display_name"] != atAgent1 || prof["agent_ref"] != "agent:"+atAgent1 {
		t.Fatalf("profile identity wrong: %v", prof)
	}
	tags, _ := prof["capability_tags"].([]any)
	if len(tags) != 2 || tags[0] != "FE" || tags[1] != "platform" {
		t.Fatalf("capability_tags=%v, want [FE platform] (case-insensitive dup dropped)", prof["capability_tags"])
	}

	// status: lifecycle (stopped) + bound worker health (offline, no heartbeat yet).
	st, _ := row["status"].(map[string]any)
	if st == nil {
		t.Fatalf("missing status: %v", row)
	}
	if st["lifecycle"] != "stopped" {
		t.Fatalf("status.lifecycle=%v, want stopped", st["lifecycle"])
	}
	if st["worker_status"] != "offline" {
		t.Fatalf("status.worker_status=%v, want offline (seeded worker, no heartbeat)", st["worker_status"])
	}

	// load: 1 running + 2 open = 3 total (JSON numbers decode as float64).
	ld, _ := row["load"].(map[string]any)
	if ld == nil {
		t.Fatalf("missing load: %v", row)
	}
	if ld["running"].(float64) != 1 || ld["open"].(float64) != 2 || ld["total"].(float64) != 3 {
		t.Fatalf("load=%v, want running=1 open=2 total=3", ld)
	}
}

// Degraded wiring (nil PMService): find_org_agent still returns the roster with a
// zero load and lifecycle status — the enrichment is best-effort, never a hard dep.
func TestFindOrgAgent_Enriched_Degraded_T461(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t) // no PMService wired

	status, body := postBearer(t, s.URL, "/admin/agent-tools/find_org_agent", "acat_w1",
		map[string]any{"agent_id": atAgent1, "name": "AG1"})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	agents, _ := body["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("want 1, got %v", body["agents"])
	}
	row := agents[0].(map[string]any)
	ld, _ := row["load"].(map[string]any)
	if ld == nil || ld["total"].(float64) != 0 {
		t.Fatalf("degraded load should be zero, got %v", row["load"])
	}
	// capability_tags is an empty array (never null) when unset.
	prof, _ := row["profile"].(map[string]any)
	if tags, ok := prof["capability_tags"].([]any); !ok || len(tags) != 0 {
		t.Fatalf("capability_tags should be empty array, got %v", prof["capability_tags"])
	}
}
