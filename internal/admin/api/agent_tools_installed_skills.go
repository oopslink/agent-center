package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
)

// report_installed_skills — the agent-runtime's OBSERVED skill report
// (issue-4a45e9cc). The runtime walks the four claude-code skill layers (built-in /
// plugin / user / project), resolves the effective set + shadowing, fingerprints it,
// and POSTs the full set here on boot and whenever the fingerprint changes. The center
// replaces the agent's whole agent_installed_skills row set with the report.
//
// This rides the SAME per-agent authed channel as the other agent tools
// (requireAgentOnWorker: worker:<id> bearer + agent-bound-to-worker guard), and stays
// entirely within the Agent BC — no cross-BC write.

type reportInstalledSkillsReq struct {
	AgentID string `json:"agent_id"`
	// CollectedAt is the runtime's collection timestamp (RFC3339). Optional — the
	// service stamps s.clock.Now() when empty/unparseable.
	CollectedAt string                      `json:"collected_at"`
	Skills      []reportInstalledSkillEntry `json:"skills"`
}

type reportInstalledSkillEntry struct {
	Layer       string `json:"layer"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Shadowed    bool   `json:"shadowed"`
}

func (s *Server) reportInstalledSkillsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req reportInstalledSkillsReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	skills := make([]agent.InstalledSkill, 0, len(req.Skills))
	for _, e := range req.Skills {
		skills = append(skills, agent.InstalledSkill{
			Layer:       agent.SkillLayer(e.Layer),
			Name:        e.Name,
			Description: e.Description,
			Shadowed:    e.Shadowed,
		})
	}
	collectedAt, _ := time.Parse(time.RFC3339Nano, req.CollectedAt)
	if err := d.AgentSvc.ReportInstalledSkills(r.Context(), a.ID(), skills, collectedAt); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"agent_id": req.AgentID,
		"count":    len(skills),
	})
}
