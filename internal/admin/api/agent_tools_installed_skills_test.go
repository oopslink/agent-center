package api

import (
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// report_installed_skills (issue-4a45e9cc): the agent-runtime uploads its OBSERVED
// skill set; the center replaces the agent's agent_installed_skills rows.
func TestReportInstalledSkills(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t)

	status, body := postBearer(t, s.URL, "/admin/agent-tools/report_installed_skills", "acat_w1",
		map[string]any{
			"agent_id":     atAgent1,
			"collected_at": "2026-07-05T09:00:00Z",
			"skills": []map[string]any{
				{"layer": "built-in", "name": "review", "description": "builtin"},
				{"layer": "project", "name": "review", "description": "proj"},
				{"layer": "user", "name": "solo", "description": "u"},
			},
		})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if body["count"] != float64(3) {
		t.Fatalf("count = %v, want 3", body["count"])
	}

	// Persisted + normalized: project review effective, built-in review shadowed.
	got, err := f.deps.AgentSvc.ListInstalledSkills(t.Context(), agent.AgentID(atAgent1))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 stored, got %d", len(got))
	}
	var builtinShadowed, projectEffective bool
	for _, sk := range got {
		if sk.Layer == agent.SkillLayerBuiltin && sk.Name == "review" && sk.Shadowed {
			builtinShadowed = true
		}
		if sk.Layer == agent.SkillLayerProject && sk.Name == "review" && !sk.Shadowed {
			projectEffective = true
		}
	}
	if !builtinShadowed || !projectEffective {
		t.Fatalf("shadow recompute wrong: %+v", got)
	}
}

func TestReportInstalledSkills_RejectsForeignAgent(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t)
	// atAgent2 is bound to atWorker2 — worker1's token may not report for it.
	status, _ := postBearer(t, s.URL, "/admin/agent-tools/report_installed_skills", "acat_w1",
		map[string]any{"agent_id": atAgent2, "skills": []map[string]any{}})
	if status != http.StatusForbidden {
		t.Fatalf("cross-worker report should be 403, got %d", status)
	}
}

func TestReportInstalledSkills_InvalidLayer(t *testing.T) {
	f := newAgentToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	s := f.server(t)
	status, _ := postBearer(t, s.URL, "/admin/agent-tools/report_installed_skills", "acat_w1",
		map[string]any{"agent_id": atAgent1, "skills": []map[string]any{{"layer": "nope", "name": "x"}}})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid layer should be 400, got %d", status)
	}
}
