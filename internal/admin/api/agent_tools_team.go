package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamtool "github.com/oopslink/agent-center/internal/team/tool"
)

// =============================================================================
// Team BC agent-tools (Team Phase-1 wiring — design §4/§6/§7/§9). These land the
// internal/team domain (S1 service + tool facade, S3 template/instantiate/
// roleassign) onto the live /admin/agent-tools/<name> surface, the same proxy
// the mcphost per-agent catalog forwards to via callAdmin.
//
// Auth mirrors the other agent tools: requireAgentOnWorker gates the operating
// agent (worker token owner, agent bound to worker); the OWNING ORG is resolved
// FROM the agent (never the body), so a team is always created/read within the
// caller's org. d.TeamSvc nil → team_not_wired (501).
//
// CRUD (create/update/delete/get/list_team, add/remove_member, associate_project)
// go through the teamtool.Tools facade so the design's stable tool surface is the
// real call path — not dead code. The S3 tools (create_team_template /
// instantiate_team / assign_roles) call the pure template/instantiate/roleassign
// helpers directly and, on instantiate, provision the team's center-hosted git
// repo + seed its shared memory.
// =============================================================================

// teamTools builds the tool facade over the wired team service.
func teamTools(d HandlerDeps) *teamtool.Tools { return teamtool.NewTools(d.TeamSvc) }

// mapTeamError maps Team BC sentinels onto HTTP status codes.
func mapTeamError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, team.ErrTeamNotFound), errors.Is(err, team.ErrMemberNotFound):
		writeError(w, http.StatusNotFound, "team_not_found", err.Error())
	case errors.Is(err, team.ErrTeamNameTaken):
		writeError(w, http.StatusConflict, "team_name_taken", err.Error())
	case errors.Is(err, team.ErrAgentAlreadyInTeam):
		writeError(w, http.StatusConflict, "agent_already_in_team", err.Error())
	case errors.Is(err, team.ErrMemberAlreadyInTeam):
		writeError(w, http.StatusConflict, "member_already_in_team", err.Error())
	case errors.Is(err, team.ErrProjectAlreadyAssociated):
		writeError(w, http.StatusConflict, "project_already_associated", err.Error())
	case errors.Is(err, team.ErrRoleNotDeclared),
		errors.Is(err, team.ErrInvalidRole),
		errors.Is(err, team.ErrInvalidTeam),
		errors.Is(err, team.ErrInvalidMemberRef),
		errors.Is(err, team.ErrInvalidProject),
		errors.Is(err, team.ErrInvalidTemplate),
		errors.Is(err, team.ErrInstantiateNeedsProject),
		errors.Is(err, team.ErrRoleNotStaffed),
		errors.Is(err, team.ErrConstraintUnsatisfiable),
		errors.Is(err, team.ErrCyclicAvoid),
		errors.Is(err, team.ErrUnknownNodeRef):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
	default:
		mapDomainError(w, err)
	}
}

// requireTeamAgent runs the standard agent gate AND asserts the team service is
// wired. Returns the resolved agent + true on success (else it wrote the error).
func (s *Server) requireTeamAgent(w http.ResponseWriter, r *http.Request, d HandlerDeps, agentID string) (*agent.Agent, bool) {
	a, ok := s.requireAgentOnWorker(w, r, d, agentID)
	if !ok {
		return nil, false
	}
	if d.TeamSvc == nil {
		writeError(w, http.StatusNotImplemented, "team_not_wired", "team service is not wired on this center")
		return nil, false
	}
	return a, true
}

// requireOwnedTeam loads a team by id and asserts it belongs to the agent's org
// (else 404 — no cross-org read/write). Writes the error + returns nil on miss.
func (s *Server) requireOwnedTeam(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, teamID string) *team.Team {
	t, err := d.TeamSvc.GetTeam(r.Context(), team.TeamID(teamID))
	if err != nil {
		mapTeamError(w, err)
		return nil
	}
	if t.OrgID() != string(a.OrganizationID()) {
		writeError(w, http.StatusNotFound, "team_not_found", "not found")
		return nil
	}
	return t
}

// --- create_team -------------------------------------------------------------

type createTeamReq struct {
	AgentID     string             `json:"agent_id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Roles       []teamtool.RoleArg `json:"roles"`
}

func (s *Server) createTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createTeamReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	view, err := teamTools(d).CreateTeam(r.Context(), teamtool.CreateTeamArgs{
		OrgID:       string(a.OrganizationID()),
		Name:        req.Name,
		Description: req.Description,
		Roles:       req.Roles,
	})
	if err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

// --- update_team -------------------------------------------------------------

type updateTeamReq struct {
	AgentID     string  `json:"agent_id"`
	TeamID      string  `json:"team_id"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (s *Server) updateTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req updateTeamReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if s.requireOwnedTeam(w, r, d, a, req.TeamID) == nil {
		return
	}
	view, err := teamTools(d).UpdateTeam(r.Context(), teamtool.UpdateTeamArgs{
		TeamID:      req.TeamID,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// --- delete_team -------------------------------------------------------------

type teamIDReq struct {
	AgentID string `json:"agent_id"`
	TeamID  string `json:"team_id"`
}

func (s *Server) deleteTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req teamIDReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if s.requireOwnedTeam(w, r, d, a, req.TeamID) == nil {
		return
	}
	if err := teamTools(d).DeleteTeam(r.Context(), req.TeamID); err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "team_id": req.TeamID})
}

// --- get_team ----------------------------------------------------------------

func (s *Server) getTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req teamIDReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if s.requireOwnedTeam(w, r, d, a, req.TeamID) == nil {
		return
	}
	view, err := teamTools(d).GetTeam(r.Context(), req.TeamID)
	if err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// --- list_teams --------------------------------------------------------------

type listTeamsReq struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) listTeamsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listTeamsReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	// Org-scoped: an agent only ever sees its own org's teams.
	views, err := teamTools(d).ListTeams(r.Context(), string(a.OrganizationID()))
	if err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": views})
}

// --- add_member --------------------------------------------------------------

type addMemberReq struct {
	AgentID   string `json:"agent_id"`
	TeamID    string `json:"team_id"`
	MemberRef string `json:"member_ref"`
	Role      string `json:"role"`
}

func (s *Server) addMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req addMemberReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if s.requireOwnedTeam(w, r, d, a, req.TeamID) == nil {
		return
	}
	view, err := teamTools(d).AddMember(r.Context(), teamtool.AddMemberArgs{
		TeamID:    req.TeamID,
		MemberRef: req.MemberRef,
		Role:      req.Role,
	})
	if err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

// --- remove_member -----------------------------------------------------------

type removeMemberReq struct {
	AgentID   string `json:"agent_id"`
	TeamID    string `json:"team_id"`
	MemberRef string `json:"member_ref"`
}

func (s *Server) removeMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req removeMemberReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if s.requireOwnedTeam(w, r, d, a, req.TeamID) == nil {
		return
	}
	if err := teamTools(d).RemoveMember(r.Context(), req.TeamID, req.MemberRef); err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "team_id": req.TeamID, "member_ref": req.MemberRef})
}

// --- associate_project -------------------------------------------------------

type associateProjectReq struct {
	AgentID   string `json:"agent_id"`
	TeamID    string `json:"team_id"`
	ProjectID string `json:"project_id"`
}

func (s *Server) associateProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req associateProjectReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if s.requireOwnedTeam(w, r, d, a, req.TeamID) == nil {
		return
	}
	if err := teamTools(d).AssociateProject(r.Context(), req.TeamID, req.ProjectID); err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "team_id": req.TeamID, "project_id": req.ProjectID})
}

// =============================================================================
// S3 tools: template authoring, instantiation, role→agent resolution.
// =============================================================================

// roleSlotReq is a template role slot (role config + instance count/配比).
type roleSlotReq struct {
	Role           string   `json:"role"`
	CLI            string   `json:"cli"`
	Model          string   `json:"model"`
	CapabilityTags []string `json:"capability_tags"`
	MaxConcurrency int      `json:"max_concurrency"`
	Count          int      `json:"count"`
}

// experienceReq is a portable experience carried in a template.
type experienceReq struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	Scope       string   `json:"scope"`
	Tags        []string `json:"tags"`
}

func toRoleSlots(in []roleSlotReq) []team.RoleSlot {
	out := make([]team.RoleSlot, 0, len(in))
	for _, r := range in {
		out = append(out, team.RoleSlot{
			Config: team.RoleConfig{
				Role:           r.Role,
				CLI:            r.CLI,
				Model:          r.Model,
				CapabilityTags: r.CapabilityTags,
				MaxConcurrency: r.MaxConcurrency,
			},
			Count: r.Count,
		})
	}
	return out
}

func toExperiences(in []experienceReq) []team.Experience {
	out := make([]team.Experience, 0, len(in))
	for _, e := range in {
		out = append(out, team.Experience{
			Slug:        e.Slug,
			Title:       e.Title,
			Description: e.Description,
			Body:        e.Body,
			Scope:       team.ExperienceScope(e.Scope),
			Tags:        e.Tags,
		})
	}
	return out
}

func templateView(t *team.TeamTemplate) map[string]any {
	roles := make([]map[string]any, 0, len(t.Roles))
	for _, sl := range t.Roles {
		roles = append(roles, map[string]any{
			"role": sl.Config.Role, "cli": sl.Config.CLI, "model": sl.Config.Model,
			"capability_tags": sl.Config.CapabilityTags, "max_concurrency": sl.Config.MaxConcurrency,
			"count": sl.Count,
		})
	}
	return map[string]any{
		"id": t.ID, "org_id": t.OrgID, "name": t.Name, "description": t.Description,
		"roles": roles, "workflow_template_ref": t.WorkflowTemplateRef,
		"experience_count": len(t.Experiences), "curated": t.Curated, "version": t.Version,
	}
}

// --- create_team_template ----------------------------------------------------

type createTeamTemplateReq struct {
	AgentID             string          `json:"agent_id"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	Roles               []roleSlotReq   `json:"roles"`
	WorkflowTemplateRef string          `json:"workflow_template_ref"`
	Experiences         []experienceReq `json:"experiences"`
}

func (s *Server) createTeamTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createTeamTemplateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	tmpl, err := team.NewTemplate(team.NewTemplateInput{
		ID:                  d.TeamIDGen.NewEntityID("teamtmpl"),
		OrgID:               string(a.OrganizationID()),
		Name:                req.Name,
		Description:         req.Description,
		Roles:               toRoleSlots(req.Roles),
		WorkflowTemplateRef: req.WorkflowTemplateRef,
		Experiences:         toExperiences(req.Experiences),
		CreatedAt:           time.Now().UTC(),
	})
	if err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, templateView(tmpl))
}

// --- instantiate_team --------------------------------------------------------

type instantiateTeamReq struct {
	AgentID   string `json:"agent_id"`
	ProjectID string `json:"project_id"`
	TeamName  string `json:"team_name"`
	// Template is the (inline) team template to instantiate — role composition +
	// per-role config + portable experiences. Templates are org-level artifacts
	// (no server-side catalog in phase 1), so the caller supplies it here.
	Template createTeamTemplateReq `json:"template"`
}

func (s *Server) instantiateTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req instantiateTeamReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	orgID := string(a.OrganizationID())
	now := time.Now().UTC()

	tmpl, err := team.NewTemplate(team.NewTemplateInput{
		ID:                  d.TeamIDGen.NewEntityID("teamtmpl"),
		OrgID:               orgID,
		Name:                req.Template.Name,
		Description:         req.Template.Description,
		Roles:               toRoleSlots(req.Template.Roles),
		WorkflowTemplateRef: req.Template.WorkflowTemplateRef,
		Experiences:         toExperiences(req.Template.Experiences),
		CreatedAt:           now,
	})
	if err != nil {
		mapTeamError(w, err)
		return
	}

	// Plan the instantiation (pure): identity+config+memory-seed + the SEPARATE
	// runtime-provisioning plan (design §9). We then apply the identity/config
	// part here; the runtime enrollment is returned for the operator's next step.
	instPlan, rtPlan, err := team.PlanInstantiation(team.InstantiateInput{
		Template:  tmpl,
		OrgID:     orgID,
		ProjectID: req.ProjectID,
		TeamName:  req.TeamName,
		Minter:    d.TeamIDGen,
		Now:       now,
	})
	if err != nil {
		mapTeamError(w, err)
		return
	}

	// Persist the instantiated team + its role composition through the service.
	teamName := req.TeamName
	if teamName == "" {
		teamName = tmpl.Name
	}
	created, err := d.TeamSvc.CreateTeam(r.Context(), teamservice.CreateTeamInput{
		OrgID:       orgID,
		Name:        teamName,
		Description: tmpl.Description,
		Roles:       tmpl.RoleConfigs(),
	})
	if err != nil {
		mapTeamError(w, err)
		return
	}
	if err := d.TeamSvc.AssociateProject(r.Context(), created.ID(), req.ProjectID); err != nil {
		mapTeamError(w, err)
		return
	}
	agents := make([]map[string]any, 0, len(instPlan.Agents))
	for _, spec := range instPlan.Agents {
		if _, mErr := d.TeamSvc.AddMember(r.Context(), created.ID(), spec.DerivedRef(), spec.Role); mErr != nil {
			mapTeamError(w, mErr)
			return
		}
		agents = append(agents, map[string]any{
			"agent_id": spec.AgentID, "role": spec.Role, "cli": spec.CLI,
			"model": spec.Model, "ordinal": spec.Ordinal, "member_ref": spec.DerivedRef().String(),
		})
	}

	// Repo provisioning + team-scoped memory seed (design §4.3/§9). Best-effort:
	// a git failure must not roll back the (already-committed) team — the caller
	// gets memory_seeded=false and can re-run provisioning. Skipped when git is
	// not wired.
	memorySeeded := false
	if d.TeamGitHost != nil {
		prod := centergit.NewTeamMemoryProducer(d.TeamGitHost, nil)
		if _, seedErr := prod.SeedTeam(r.Context(), created.ID().String(), toMemoryEntries(instPlan.MemorySeed)); seedErr == nil {
			memorySeeded = true
		}
	}

	enrollments := make([]map[string]any, 0, len(rtPlan.Enrollments))
	for _, e := range rtPlan.Enrollments {
		enrollments = append(enrollments, map[string]any{
			"agent_id": e.AgentID, "role": e.Role, "cli": e.CLI, "model": e.Model,
		})
	}

	view, gErr := teamTools(d).GetTeam(r.Context(), created.ID().String())
	if gErr != nil {
		mapTeamError(w, gErr)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"team":                  view,
		"project_id":            instPlan.ProjectID,
		"workflow_template_ref": instPlan.WorkflowTemplateRef,
		"agents":                agents,
		"memory_seed_count":     len(instPlan.MemorySeed),
		"memory_seeded":         memorySeeded,
		// The runtime-provisioning plan is a SEPARATE step (design §9): the
		// template carries no runtime/auth, so the operator runs enroll for each.
		"runtime_provisioning": map[string]any{"enrollments": enrollments},
	})
}

// toMemoryEntries maps portable template experiences onto centergit memory
// entries (one file per experience — design §9).
func toMemoryEntries(in []team.Experience) []centergit.Entry {
	out := make([]centergit.Entry, 0, len(in))
	for _, e := range in {
		out = append(out, centergit.Entry{
			Slug:        e.Slug,
			Title:       e.Title,
			Description: e.Description,
			Body:        e.Body,
			Type:        string(e.Scope),
		})
	}
	return out
}

// --- extract_from_team -------------------------------------------------------

type extractFromTeamReq struct {
	AgentID string `json:"agent_id"`
	TeamID  string `json:"team_id"`
	// Counts optionally overrides per-role instance counts (role → count) in the
	// draft; absent roles default to 1.
	Counts map[string]int `json:"counts"`
}

// extractFromTeamHandler snapshots a LIVE team into a DRAFT template (design §6/§9
// "从活 team 抽经验草稿"): it copies the team's role composition, reads the team's
// accumulated experiences from its center-hosted memory repo, keeps only the
// portable (team/global-scope) layer, and runs the scrub pass that HIGHLIGHTS
// suspected proprietary tokens for the human curator. The draft is returned
// Curated=false — extraction never produces an export-ready template on its own.
func (s *Server) extractFromTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req extractFromTeamReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	t := s.requireOwnedTeam(w, r, d, a, req.TeamID)
	if t == nil {
		return
	}

	// Read the live team's experiences from its center-hosted memory repo. Git not
	// wired (client/test mode) → degrade to a roles-only draft rather than erroring,
	// mirroring instantiate_team's memory_seeded=false degrade.
	var experiences []team.Experience
	if d.TeamGitHost != nil {
		entries, rErr := centergit.NewTeamMemoryConsumer(d.TeamGitHost, nil).ReadTeam(r.Context(), t.ID().String())
		if rErr != nil {
			mapDomainError(w, rErr)
			return
		}
		experiences = experiencesFromEntries(entries)
	}

	res, err := team.ExtractFromTeam(team.TeamSnapshot{
		Team:        t,
		Experiences: experiences,
		Counts:      req.Counts,
	}, d.TeamIDGen.NewEntityID("teamtmpl"), nil, time.Now().UTC())
	if err != nil {
		mapTeamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, extractView(res))
}

// experiencesFromEntries maps center-hosted memory entries back onto template
// experiences. The entry's Type carries the memory scope (toMemoryEntries wrote
// it), so it round-trips into ExperienceScope — which ExtractFromTeam then uses to
// keep the portable layer and drop project scope.
func experiencesFromEntries(in []centergit.Entry) []team.Experience {
	out := make([]team.Experience, 0, len(in))
	for _, e := range in {
		out = append(out, team.Experience{
			Slug:        e.Slug,
			Title:       e.Title,
			Description: e.Description,
			Body:        e.Body,
			Scope:       team.ExperienceScope(e.Type),
		})
	}
	return out
}

// extractView renders the draft template + the scrub findings a human must review
// + the count of project-scoped experiences dropped.
func extractView(res *team.ExtractResult) map[string]any {
	findings := make([]map[string]any, 0, len(res.ScrubFindings))
	for _, f := range res.ScrubFindings {
		findings = append(findings, map[string]any{
			"experience_slug": f.ExperienceSlug, "kind": string(f.Kind), "token": f.Token,
		})
	}
	return map[string]any{
		"draft":           templateView(res.Draft),
		"scrub_findings":  findings,
		"dropped_project": res.DroppedProject,
		// A draft is NEVER export-ready: manual curation (design §9) is still
		// required before create_team_template / export accepts it.
		"curated": res.Draft.Curated,
	}
}

// --- assign_roles ------------------------------------------------------------

type assignRoleReqNode struct {
	NodeKey    string   `json:"node_key"`
	Role       string   `json:"role"`
	AvoidNodes []string `json:"avoid_nodes"`
}

type assignRolesReq struct {
	AgentID  string              `json:"agent_id"`
	TeamID   string              `json:"team_id"`
	Strategy string              `json:"strategy"`
	Requests []assignRoleReqNode `json:"requests"`
}

func (s *Server) assignRolesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req assignRolesReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireTeamAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if s.requireOwnedTeam(w, r, d, a, req.TeamID) == nil {
		return
	}
	members, err := d.TeamSvc.ListMembers(r.Context(), team.TeamID(req.TeamID))
	if err != nil {
		mapTeamError(w, err)
		return
	}
	roster := team.NewRoster(members)
	reqs := make([]team.NodeAssignRequest, 0, len(req.Requests))
	for _, n := range req.Requests {
		reqs = append(reqs, team.NodeAssignRequest{NodeKey: n.NodeKey, Role: n.Role, AvoidNodes: n.AvoidNodes})
	}
	assignments, err := team.ResolveRoles(roster, reqs, nil, team.AssignStrategy(req.Strategy))
	if err != nil {
		mapTeamError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(assignments))
	for _, as := range assignments {
		out = append(out, map[string]any{"node_key": as.NodeKey, "role": as.Role, "agent": as.Agent.String()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"assignments": out})
}
