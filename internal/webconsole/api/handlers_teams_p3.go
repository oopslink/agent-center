package api

import (
	"net/http"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/team"
)

// Team WebUI Phase-1 facade — P3 slice (task follow-up to 48bf401d). Completes
// the remaining teams.ts hooks the UDE drives so all 22 can swap fixtures→fetch:
//
//   - disassociate_project  DELETE /api/orgs/{slug}/teams/{id}/projects/{project_id}
//   - save_template         POST   /api/orgs/{slug}/team-templates/save
//   - import_template       POST   /api/orgs/{slug}/team-templates/import
//   - template_instances    GET    /api/orgs/{slug}/team-templates/{tid}/instances
//   - directory_agents      GET    /api/orgs/{slug}/directory/agents
//   - directory_humans      GET    /api/orgs/{slug}/directory/humans
//
// Auth: the same web-session gate as the rest of /api (requireOrgMember /
// teamGuard, resolved from {slug}). Response JSON is field-for-field the teams.ts
// types (TeamProjectLink / TeamTemplate / TeamView / DirectoryAgent /
// DirectoryHuman) so the swap is queryFn/mutationFn body-only.

// ---------------------------------------------------------------------------
// disassociate_project
// ---------------------------------------------------------------------------

// disassociateTeamProjectHandler serves
// DELETE /api/orgs/{slug}/teams/{id}/projects/{project_id} — the inverse of the
// associate POST. Removes the Team↔Project link. Cross-org team id → 404;
// unlinked project → 404 (not_found). Returns {ok, team_id, project_id}.
func (s *Server) disassociateTeamProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	t, err := getTeamInOrg(r, d, orgID, r.PathValue("id"))
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	projectID := r.PathValue("project_id")
	if err := d.TeamService.DisassociateProject(r.Context(), t.ID(), projectID); err != nil {
		mapTeamWebError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"team_id":    string(t.ID()),
		"project_id": projectID,
	})
}

// ---------------------------------------------------------------------------
// team templates — save / import (in-memory, org-scoped)
// ---------------------------------------------------------------------------

// templateSlotsFromReq maps the FE RoleSlot inputs onto domain RoleSlots.
func templateSlotsFromReq(roles []templateRoleReq) []team.RoleSlot {
	slots := make([]team.RoleSlot, 0, len(roles))
	for _, rr := range roles {
		slots = append(slots, team.RoleSlot{
			Config: team.RoleConfig{
				Role: rr.Role, CLI: rr.CLI, Model: rr.Model,
				CapabilityTags: rr.CapabilityTags, MaxConcurrency: rr.MaxConcurrency,
			},
			Count: rr.Count,
		})
	}
	return slots
}

// saveTemplateReq is the SaveTemplateInput body (teams.ts useSaveTemplate):
// {name, description, source, source_kind, roles}. save persists a CURATED
// template (the FE flow gates a manual curation pass before save, unlike the raw
// create which authors an un-curated draft).
type saveTemplateReq struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Source      string            `json:"source"`
	SourceKind  string            `json:"source_kind"`
	Roles       []templateRoleReq `json:"roles"`
}

// saveTemplateHandler serves POST /api/orgs/{slug}/team-templates/save → TeamTemplate (201).
func (s *Server) saveTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req saveTemplateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	tmpl, err := team.NewTemplate(team.NewTemplateInput{
		ID:          facadeIDGen.NewEntityID("teamtmpl"),
		OrgID:       orgID,
		Name:        req.Name,
		Description: req.Description,
		Roles:       templateSlotsFromReq(req.Roles),
		Curated:     true, // save persists the curated draft (design §9)
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	sourceKind := req.SourceKind
	if sourceKind == "" {
		sourceKind = "manual"
	}
	st := &storedTemplate{tmpl: tmpl, source: req.Source, sourceKind: sourceKind}
	s.teamTemplates.add(orgID, st)
	writeJSON(w, http.StatusCreated, teamTemplateView(st))
}

// importTemplateRoleReq is one role in an exported envelope. Same shape as a
// template RoleSlot, but every field is optional (a cross-org envelope may be
// partial) — defaults mirror the FE useImportTemplate re-home.
type importTemplateRoleReq struct {
	Role           string   `json:"role"`
	CLI            string   `json:"cli"`
	Model          string   `json:"model"`
	CapabilityTags []string `json:"capability_tags"`
	MaxConcurrency int      `json:"max_concurrency"`
	Count          int      `json:"count"`
}

// importTemplateReq is the exported envelope (exportTemplateEnvelope output). Only
// the portable fields are read; source_org_id/source_id are informational.
type importTemplateReq struct {
	Name                string                  `json:"name"`
	Description         string                  `json:"description"`
	Roles               []importTemplateRoleReq `json:"roles"`
	WorkflowTemplateRef string                  `json:"workflow_template_ref"`
}

// importTemplateHandler serves POST /api/orgs/{slug}/team-templates/import →
// TeamTemplate (201). Re-homes an exported envelope into this org as an
// UN-curated template (source_kind=import) — the importer must re-run curation
// before it can be re-exported (design §9). Missing fields fall back to the same
// defaults the FE useImportTemplate applied.
func (s *Server) importTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req importTemplateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	slots := make([]team.RoleSlot, 0, len(req.Roles))
	for _, rr := range req.Roles {
		role := firstNonEmpty(rr.Role, "coder")
		cli := firstNonEmpty(rr.CLI, "claude-code")
		model := firstNonEmpty(rr.Model, "sonnet-5")
		tags := rr.CapabilityTags
		if tags == nil {
			tags = []string{}
		}
		maxConc := rr.MaxConcurrency
		if maxConc <= 0 {
			maxConc = 1
		}
		count := rr.Count
		if count <= 0 {
			count = 1
		}
		slots = append(slots, team.RoleSlot{
			Config: team.RoleConfig{Role: role, CLI: cli, Model: model, CapabilityTags: tags, MaxConcurrency: maxConc},
			Count:  count,
		})
	}
	tmpl, err := team.NewTemplate(team.NewTemplateInput{
		ID:                  facadeIDGen.NewEntityID("teamtmpl"),
		OrgID:               orgID,
		Name:                firstNonEmpty(req.Name, "imported-template"),
		Description:         req.Description,
		Roles:               slots,
		WorkflowTemplateRef: firstNonEmpty(req.WorkflowTemplateRef, "plan-builtin"),
		Curated:             false, // an imported template must be re-curated before export
		CreatedAt:           time.Now().UTC(),
	})
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	st := &storedTemplate{tmpl: tmpl, source: "导入 · cross-org JSON", sourceKind: "import"}
	s.teamTemplates.add(orgID, st)
	writeJSON(w, http.StatusCreated, teamTemplateView(st))
}

// firstNonEmpty returns a if non-empty else b (envelope-default helper).
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// template instances
// ---------------------------------------------------------------------------

// templateInstancesHandler serves
// GET /api/orgs/{slug}/team-templates/{tid}/instances → TeamView[]. Returns the
// teams instantiated from the template (tracked in-memory by instantiate). Teams
// since deleted (or moved out of the org) are skipped — a stale instance ref is
// not an error.
func (s *Server) templateInstancesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	ids, found := s.teamTemplates.instanceIDs(orgID, r.PathValue("tid"))
	if !found {
		writeError(w, http.StatusNotFound, "template_not_found", "team template not found")
		return
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		t, err := getTeamInOrg(r, d, orgID, id)
		if err != nil {
			continue // instance gone (deleted / cross-org) → skip, not an error
		}
		members, err := d.TeamService.ListMembers(r.Context(), t.ID())
		if err != nil {
			mapTeamWebError(w, err)
			return
		}
		projects, err := d.TeamService.ListProjects(r.Context(), t.ID())
		if err != nil {
			mapTeamWebError(w, err)
			return
		}
		out = append(out, teamViewMap(t, members, len(projects)))
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// directory (agents / humans)
// ---------------------------------------------------------------------------

// dirMembership is a person's team-membership rollup for the directory: the names
// of the teams they're on, and their (first) declared role.
type dirMembership struct {
	teams []string
	role  string
}

// directoryMembership rolls up every team member in the org keyed by bare
// identity id (the team member ref is "<kind>:<identityID>"). Both directory
// endpoints share it: an agent is exclusive to one team, a human may be on many.
func (s *Server) directoryMembership(r *http.Request, d HandlerDeps, orgID string) map[string]*dirMembership {
	out := map[string]*dirMembership{}
	teams, err := d.TeamService.ListTeams(r.Context(), orgID)
	if err != nil {
		return out
	}
	for _, t := range teams {
		members, err := d.TeamService.ListMembers(r.Context(), t.ID())
		if err != nil {
			continue
		}
		for _, m := range members {
			bare := refBareID(conversation.IdentityRef(string(m.Ref)))
			e := out[bare]
			if e == nil {
				e = &dirMembership{}
				out[bare] = e
			}
			e.teams = append(e.teams, t.Name())
			if e.role == "" {
				e.role = m.Role
			}
		}
	}
	return out
}

// nonNilTeams returns the membership's team names as a never-null array.
func nonNilTeams(m *dirMembership) []string {
	if m == nil || m.teams == nil {
		return []string{}
	}
	return m.teams
}

// directoryAgentsHandler serves GET /api/orgs/{slug}/directory/agents →
// DirectoryAgent[]. Enumerates the org's agent members, cross-referenced with
// team membership. Phase-1: load/backlog/last are placeholders (0/0/"—") — no
// runtime telemetry aggregate is wired yet (UDE-agreed contract).
func (s *Server) directoryAgentsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	if d.MemberRepo == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	members, err := d.MemberRepo.ListByOrganization(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	membership := s.directoryMembership(r, d, orgID)
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		ident := s.resolveIdentity(r, d, m.IdentityID())
		if ident == nil || ident.Kind() != identity.KindAgent {
			continue
		}
		model, status := "", "idle"
		if d.AgentSvc != nil {
			if ag, aerr := d.AgentSvc.ResolveAgent(r.Context(), m.IdentityID()); aerr == nil && ag != nil {
				model = ag.Profile().Model
				if ag.Lifecycle() == agentbc.LifecycleRunning {
					status = "working"
				}
			}
		}
		mem := membership[m.IdentityID()]
		role := ""
		if mem != nil {
			role = mem.role
		}
		out = append(out, map[string]any{
			// ref is the canonical member ref (agent:<identityID>) the FE feeds
			// straight into add-member / role-assign — without it the picker had to
			// fabricate/truncate a ref, which polluted the DB (tester3 round-2 #2).
			"ref":     string(ident.Kind()) + ":" + m.IdentityID(),
			"name":    ident.DisplayName(),
			"status":  status,
			"role":    role,
			"teams":   nonNilTeams(mem),
			"model":   model,
			"load":    0,
			"backlog": 0,
			"last":    "—",
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// directoryHumansHandler serves GET /api/orgs/{slug}/directory/humans →
// DirectoryHuman[]. Enumerates the org's human members; email/created come from
// the real identity, teams/role from team membership. Phase-1: last is the
// identity's last session ("—" when never), status maps the member state.
func (s *Server) directoryHumansHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	if d.MemberRepo == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	members, err := d.MemberRepo.ListByOrganization(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	membership := s.directoryMembership(r, d, orgID)
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		ident := s.resolveIdentity(r, d, m.IdentityID())
		if ident == nil || ident.Kind() != identity.KindUser {
			continue
		}
		email := ""
		if e := ident.Email(); e != nil {
			email = *e
		}
		last := "—"
		if ls := ident.LastSessionAt(); ls != nil {
			last = ls.UTC().Format(time.RFC3339)
		}
		status := "Invited"
		if m.Status() == identity.MemberJoined {
			status = "Joined"
		}
		mem := membership[m.IdentityID()]
		role := ""
		if mem != nil {
			role = mem.role
		}
		out = append(out, map[string]any{
			// ref is the canonical member ref (user:<identityID>) the FE feeds
			// straight into add-member — same anti-truncation contract as the
			// agents directory (tester3 round-2 #2).
			"ref":     string(ident.Kind()) + ":" + m.IdentityID(),
			"name":    ident.DisplayName(),
			"role":    role,
			"status":  status,
			"email":   email,
			"created": ident.CreatedAt().UTC().Format(time.RFC3339),
			"last":    last,
			"teams":   nonNilTeams(mem),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// resolveIdentity best-effort loads an identity (nil when the repo is unwired or
// the id does not resolve — the caller skips unresolvable members).
func (s *Server) resolveIdentity(r *http.Request, d HandlerDeps, id string) *identity.Identity {
	if d.IdentityRepo == nil {
		return nil
	}
	ident, err := d.IdentityRepo.GetByID(r.Context(), id)
	if err != nil {
		return nil
	}
	return ident
}
