package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
)

// Team WebUI Phase-1 facade — P2 slice (plan-32dd9107 follow-up, task-be4670ce).
// Browser-facing REST under /api/orgs/{slug}/... that extends handlers_teams.go
// (teams CRUD + members + projects) with the remaining team operations the UI
// (web/src/api/teams.ts) drives:
//
//   - update_team          PATCH  /api/orgs/{slug}/teams/{id}
//   - instantiate_team     POST   /api/orgs/{slug}/teams/instantiate
//   - extract_from_team    GET    /api/orgs/{slug}/teams/{id}/extract
//   - team memory (r/o)    GET    /api/orgs/{slug}/teams/{id}/memory
//                          GET    /api/orgs/{slug}/teams/{id}/memory/{slug}
//   - team templates       GET/POST /api/orgs/{slug}/team-templates
//                          GET      /api/orgs/{slug}/team-templates/{tid}
//
// Auth: the same web-session gate as the rest of /api (requireOrgMember, resolved
// from {slug}) — NOT the worker-token /admin surface. Every response is
// field-for-field the TS types in teams.ts so the UDE swap is queryFn/mutationFn
// body-only (zero hook/testid/route churn).
//
// Domain reuse only (no re-implemented business logic): update/instantiate call
// the team.Service; extract calls the pure team.ExtractFromTeam; memory reads the
// center-hosted git repo via centergit. Templates are org-level artifacts with no
// server catalog in Phase-1 (design §6, "可 in-memory 照现状") — a per-Server
// in-memory store keeps create → list → get coherent for the UI. Where a
// dependency is unwired (git host nil) the endpoint degrades exactly like the
// /admin tools do (empty memory / roles-only draft), never 500s.

// facadeIDGen mints template / draft ids for the in-memory template store and the
// extract draft. This is id minting, not domain logic — the team.Service owns its
// own generator for real team/member ids.
var facadeIDGen = idgen.NewGenerator(clock.SystemClock{})

// ---------------------------------------------------------------------------
// update_team
// ---------------------------------------------------------------------------

// updateTeamReq is the update body — name/description are optional (nil = leave
// unchanged), mirroring teamservice.UpdateTeamInput.
type updateTeamReq struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// updateTeamHandler serves PATCH /api/orgs/{slug}/teams/{id} → TeamView.
func (s *Server) updateTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	// enforce org ownership before mutate (cross-org id → 404).
	if _, err := getTeamInOrg(r, d, orgID, r.PathValue("id")); err != nil {
		mapTeamWebError(w, err)
		return
	}
	var req updateTeamReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	t, err := d.TeamService.UpdateTeam(r.Context(), team.TeamID(r.PathValue("id")), teamservice.UpdateTeamInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		mapTeamWebError(w, err)
		return
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
	writeJSON(w, http.StatusOK, teamViewMap(t, members, len(projects)))
}

// ---------------------------------------------------------------------------
// instantiate_team
// ---------------------------------------------------------------------------

// instantiateTeamReq is the InstantiateTeam body (teams.ts useInstantiateTeam):
// {template_id, team_name, roles}. instantiation is PROJECT-INDEPENDENT
// (issue-c4dccae0) — the team is created at org level with no project binding;
// associating a project is a separate associate_project step.
type instantiateTeamReq struct {
	TemplateID string         `json:"template_id"`
	TeamName   string         `json:"team_name"`
	Roles      []roleInputReq `json:"roles"`
}

// instantiateTeamHandler serves POST /api/orgs/{slug}/teams/instantiate → TeamView (201).
// Reuses team.Service.CreateTeam with the requested role composition. Per-role
// count/config is honoured: the response roles echo the requested count (the
// composition the caller asked for), not the live member count (0 on a fresh
// team) — this is what the FE builder renders.
func (s *Server) instantiateTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	var req instantiateTeamReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	roles := req.Roles
	// Fall back to the stored template's roles when the caller passes a template id
	// but no explicit role overrides.
	if len(roles) == 0 && req.TemplateID != "" {
		if st, found := s.teamTemplates.get(orgID, req.TemplateID); found {
			for _, sl := range st.tmpl.Roles {
				roles = append(roles, roleInputReq{
					Role: sl.Config.Role, CLI: sl.Config.CLI, Model: sl.Config.Model,
					MaxConcurrency: sl.Config.MaxConcurrency, Count: sl.Count,
					Tags: strings.Join(sl.Config.CapabilityTags, ","),
				})
			}
		}
	}

	configs := make([]team.RoleConfig, 0, len(roles))
	countByRole := make(map[string]int, len(roles))
	for _, ri := range roles {
		configs = append(configs, team.RoleConfig{
			Role: ri.Role, CLI: ri.CLI, Model: ri.Model,
			CapabilityTags: splitTags(ri.Tags), MaxConcurrency: ri.MaxConcurrency,
		})
		count := ri.Count
		if count <= 0 {
			count = 1
		}
		countByRole[ri.Role] = count
	}

	name := req.TeamName
	t, err := d.TeamService.CreateTeam(r.Context(), teamservice.CreateTeamInput{
		OrgID: orgID, Name: name, Description: "", Roles: configs,
	})
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	// Track the instantiation against its source template (FE instances_count +
	// the template-instances list).
	if req.TemplateID != "" {
		s.teamTemplates.addInstance(orgID, req.TemplateID, string(t.ID()))
	}
	writeJSON(w, http.StatusCreated, instantiatedTeamView(t, countByRole))
}

// instantiatedTeamView renders the TeamView for a freshly instantiated team: the
// per-role count echoes the requested composition (countByRole), status is
// 'active' (an instantiated team is live), and members/projects are 0 (Phase-1
// facade provisions no agents — runtime enrollment is a separate operator step).
func instantiatedTeamView(t *team.Team, countByRole map[string]int) map[string]any {
	roles := make([]map[string]any, 0, len(t.Roles()))
	for _, rc := range t.Roles() {
		roles = append(roles, roleViewMap(rc, countByRole[rc.Role]))
	}
	return map[string]any{
		"id":             string(t.ID()),
		"org_id":         t.OrgID(),
		"name":           t.Name(),
		"description":    t.Description(),
		"roles":          roles,
		"version":        t.Version(),
		"glyph":          teamMonogram(t.Name()),
		"status":         "active",
		"members_count":  0,
		"projects_count": 0,
		"created":        t.CreatedAt().UTC().Format(time.RFC3339),
	}
}

// ---------------------------------------------------------------------------
// extract_from_team
// ---------------------------------------------------------------------------

// extractFromTeamHandler serves GET /api/orgs/{slug}/teams/{id}/extract →
// {draft, scrub_findings, dropped_project, curated}. Snapshots the live team into
// a DRAFT template + runs the scrub pass (team.ExtractFromTeam, pure). When the
// git host is wired it reads the team's accumulated memory experiences; otherwise
// it degrades to a roles-only draft (mirrors the /admin extract degrade). The
// draft is always Curated=false — extraction never produces an export-ready
// template on its own (design §9, manual curation is load-bearing).
func (s *Server) extractFromTeamHandler(w http.ResponseWriter, r *http.Request) {
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

	var experiences []team.Experience
	if d.TeamGitHost != nil {
		entries, _, rErr := centergit.NewTeamMemoryConsumer(d.TeamGitHost, nil).ReadTeam(r.Context(), t.ID().String())
		if rErr != nil {
			mapTeamWebError(w, rErr)
			return
		}
		experiences = experiencesFromMemoryEntries(entries)
	}

	res, err := team.ExtractFromTeam(team.TeamSnapshot{
		Team:        t,
		Experiences: experiences,
	}, facadeIDGen.NewEntityID("teamtmpl"), nil, time.Now().UTC())
	if err != nil {
		mapTeamWebError(w, err)
		return
	}

	findings := make([]map[string]any, 0, len(res.ScrubFindings))
	for _, f := range res.ScrubFindings {
		findings = append(findings, map[string]any{
			"experience_slug": f.ExperienceSlug,
			"kind":            string(f.Kind),
			"token":           f.Token,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"draft":           draftTemplateView(res.Draft),
		"scrub_findings":  findings,
		"dropped_project": res.DroppedProject,
		"curated":         res.Draft.Curated,
	})
}

// experiencesFromMemoryEntries maps center-hosted memory entries onto template
// experiences. The entry Type carries the memory scope (round-trips into
// ExperienceScope), so ExtractFromTeam keeps the portable layer and drops project
// scope.
func experiencesFromMemoryEntries(in []centergit.Entry) []team.Experience {
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

// ---------------------------------------------------------------------------
// team memory (read-only)
// ---------------------------------------------------------------------------

// teamMemoryIndexHandler serves GET /api/orgs/{slug}/teams/{id}/memory →
// MemoryIndexEntry[]. Reads the team's center-hosted memory repo (one entry per
// experience). Git host unwired or an unprovisioned team → [] (an absent history
// is empty, not an error — matches centergit.ReadTeam's contract).
func (s *Server) teamMemoryIndexHandler(w http.ResponseWriter, r *http.Request) {
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
	entries, ok := s.readTeamMemory(w, r, d, t)
	if !ok {
		return // readTeamMemory wrote an error
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{"slug": e.Slug})
	}
	writeJSON(w, http.StatusOK, out)
}

// teamMemoryDocHandler serves GET /api/orgs/{slug}/teams/{id}/memory/{slug} →
// MemoryDoc (the on-demand entry read). 404 memory_not_found when the slug is
// absent (or memory is unwired).
func (s *Server) teamMemoryDocHandler(w http.ResponseWriter, r *http.Request) {
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
	slug := r.PathValue("entry")
	entries, ok := s.readTeamMemory(w, r, d, t)
	if !ok {
		return
	}
	for _, e := range entries {
		if e.Slug == slug {
			writeJSON(w, http.StatusOK, map[string]any{
				"slug":        e.Slug,
				"path":        e.Slug,
				"title":       e.Title,
				"frontmatter": nil,
				"body":        e.Body,
			})
			return
		}
	}
	writeError(w, http.StatusNotFound, "memory_not_found", "memory entry not found")
}

// readTeamMemory reads the team's center-hosted memory entries, degrading to an
// empty slice when the git host is unwired. Returns ok=false (after writing the
// HTTP error) on a read failure so callers stop.
func (s *Server) readTeamMemory(w http.ResponseWriter, r *http.Request, d HandlerDeps, t *team.Team) ([]centergit.Entry, bool) {
	if d.TeamGitHost == nil {
		return []centergit.Entry{}, true
	}
	entries, _, err := centergit.NewTeamMemoryConsumer(d.TeamGitHost, nil).ReadTeam(r.Context(), t.ID().String())
	if err != nil {
		mapTeamWebError(w, err)
		return nil, false
	}
	return entries, true
}

// ---------------------------------------------------------------------------
// team templates (in-memory, org-scoped)
// ---------------------------------------------------------------------------

// storedTemplate wraps a domain template with the FE-only display extras
// (source / source_kind / instances_count) that teams.ts TeamTemplate carries but
// the domain does not encode. instances holds the ids of the teams instantiated
// from this template (Phase-1 in-memory) — its length is instances_count and it
// backs GET .../team-templates/{tid}/instances (→ TeamView[]).
type storedTemplate struct {
	tmpl       *team.TeamTemplate
	source     string
	sourceKind string
	instances  []string
}

// teamTemplateStore is the Phase-1 in-memory, org-scoped team-template catalog.
// Templates are org-level artifacts with no server-side persistence yet
// (design §6); this keeps create → list → get coherent within a Server lifetime
// so the UI is end-to-end real. Per-Server (test-isolated). Lost on restart.
type teamTemplateStore struct {
	mu    sync.Mutex
	byOrg map[string][]*storedTemplate
}

func newTeamTemplateStore() *teamTemplateStore {
	return &teamTemplateStore{byOrg: make(map[string][]*storedTemplate)}
}

func (st *teamTemplateStore) add(orgID string, t *storedTemplate) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.byOrg[orgID] = append(st.byOrg[orgID], t)
}

func (st *teamTemplateStore) list(orgID string) []*storedTemplate {
	st.mu.Lock()
	defer st.mu.Unlock()
	src := st.byOrg[orgID]
	out := make([]*storedTemplate, len(src))
	copy(out, src)
	return out
}

func (st *teamTemplateStore) get(orgID, id string) (*storedTemplate, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, t := range st.byOrg[orgID] {
		if t.tmpl.ID == id {
			return t, true
		}
	}
	return nil, false
}

// addInstance records that teamID was instantiated from template id (no-op when
// the template is unknown — a raw create-team with a stale template_id).
func (st *teamTemplateStore) addInstance(orgID, id, teamID string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, t := range st.byOrg[orgID] {
		if t.tmpl.ID == id {
			t.instances = append(t.instances, teamID)
			return
		}
	}
}

// instanceIDs returns a copy of the template's instantiated team ids (under the
// lock, so the caller can range without racing addInstance). found=false when the
// template id is unknown in the org.
func (st *teamTemplateStore) instanceIDs(orgID, id string) ([]string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, t := range st.byOrg[orgID] {
		if t.tmpl.ID == id {
			out := make([]string, len(t.instances))
			copy(out, t.instances)
			return out, true
		}
	}
	return nil, false
}

// createTeamTemplateReq is the SaveTemplateInput body (teams.ts). Templates only
// need org membership (they do not touch the team service).
type createTeamTemplateReq struct {
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Source              string            `json:"source"`
	SourceKind          string            `json:"source_kind"`
	WorkflowTemplateRef string            `json:"workflow_template_ref"`
	Roles               []templateRoleReq `json:"roles"`
}

// templateRoleReq is a template RoleSlot input (teams.ts RoleSlot): the role
// config + per-role count. capability_tags is already a []string (unlike the
// create-team RoleInput's comma-string).
type templateRoleReq struct {
	Role           string   `json:"role"`
	CLI            string   `json:"cli"`
	Model          string   `json:"model"`
	CapabilityTags []string `json:"capability_tags"`
	MaxConcurrency int      `json:"max_concurrency"`
	Count          int      `json:"count"`
}

// listTeamTemplatesHandler serves GET /api/orgs/{slug}/team-templates → TeamTemplate[].
func (s *Server) listTeamTemplatesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	stored := s.teamTemplates.list(orgID)
	out := make([]map[string]any, 0, len(stored))
	for _, st := range stored {
		out = append(out, teamTemplateView(st))
	}
	writeJSON(w, http.StatusOK, out)
}

// getTeamTemplateHandler serves GET /api/orgs/{slug}/team-templates/{tid} → TeamTemplate.
func (s *Server) getTeamTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	st, found := s.teamTemplates.get(orgID, r.PathValue("tid"))
	if !found {
		writeError(w, http.StatusNotFound, "template_not_found", "team template not found")
		return
	}
	writeJSON(w, http.StatusOK, teamTemplateView(st))
}

// templateScrubHandler serves GET /api/orgs/{slug}/team-templates/{tid}/scrub →
// {scrub_findings}. Runs the curation-assist scrub (team.ScrubExperience, pure)
// over the template's seed-memory experiences (design §6 block ③) and returns the
// suspected-proprietary tokens for the Curation & 来源 pane to highlight. This is
// the template-level analogue of GET /teams/{id}/extract's scrub pass and honors
// the same truthful-token contract: only {experience_slug, kind, token} are
// returned — the FE enriches risk/loc/reason/default_action display-only. A
// template with no experiences yields an empty (never fixture) findings list.
func (s *Server) templateScrubHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	st, found := s.teamTemplates.get(orgID, r.PathValue("tid"))
	if !found {
		writeError(w, http.StatusNotFound, "template_not_found", "team template not found")
		return
	}

	findings := make([]map[string]any, 0)
	for _, e := range st.tmpl.Experiences {
		for _, f := range team.ScrubExperience(e) {
			findings = append(findings, map[string]any{
				"experience_slug": f.ExperienceSlug,
				"kind":            string(f.Kind),
				"token":           f.Token,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"scrub_findings": findings})
}

// createTeamTemplateHandler serves POST /api/orgs/{slug}/team-templates → TeamTemplate (201).
func (s *Server) createTeamTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req createTeamTemplateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	slots := make([]team.RoleSlot, 0, len(req.Roles))
	for _, rr := range req.Roles {
		slots = append(slots, team.RoleSlot{
			Config: team.RoleConfig{
				Role: rr.Role, CLI: rr.CLI, Model: rr.Model,
				CapabilityTags: rr.CapabilityTags, MaxConcurrency: rr.MaxConcurrency,
			},
			Count: rr.Count,
		})
	}
	// Create authors an UN-curated template (curate/export is the /admin cross-org
	// path, not a Phase-1 UI flow). team.NewTemplate validates + normalizes.
	tmpl, err := team.NewTemplate(team.NewTemplateInput{
		ID:                  facadeIDGen.NewEntityID("teamtmpl"),
		OrgID:               orgID,
		Name:                req.Name,
		Description:         req.Description,
		Roles:               slots,
		WorkflowTemplateRef: req.WorkflowTemplateRef,
		Curated:             false,
		CreatedAt:           time.Now().UTC(),
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

// teamTemplateView renders the TS TeamTemplate: the domain template fields +
// the FE display extras (source / source_kind / version_label / instances_count).
func teamTemplateView(st *storedTemplate) map[string]any {
	t := st.tmpl
	versionLabel := "v" + strconv.Itoa(t.Version)
	if t.Curated {
		versionLabel += " · curated"
	}
	return map[string]any{
		"id":                    t.ID,
		"org_id":                t.OrgID,
		"name":                  t.Name,
		"description":           t.Description,
		"roles":                 templateRoleViews(t.Roles),
		"workflow_template_ref": t.WorkflowTemplateRef,
		"curated":               t.Curated,
		"source":                st.source,
		"source_kind":           st.sourceKind,
		"version_label":         versionLabel,
		"instances_count":       len(st.instances),
	}
}

// draftTemplateView renders the base TeamTemplate fields for an extract draft
// (no store-side FE extras — a draft is not yet a catalog entry).
func draftTemplateView(t *team.TeamTemplate) map[string]any {
	return map[string]any{
		"id":                    t.ID,
		"org_id":                t.OrgID,
		"name":                  t.Name,
		"description":           t.Description,
		"roles":                 templateRoleViews(t.Roles),
		"workflow_template_ref": t.WorkflowTemplateRef,
		"curated":               t.Curated,
	}
}

// templateRoleViews renders a template's RoleSlots as the TS RoleSlot shape
// (RoleView + count). capability_tags is always a (possibly empty) array.
func templateRoleViews(slots []team.RoleSlot) []map[string]any {
	out := make([]map[string]any, 0, len(slots))
	for _, sl := range slots {
		tags := sl.Config.CapabilityTags
		if tags == nil {
			tags = []string{}
		}
		out = append(out, map[string]any{
			"role":            sl.Config.Role,
			"cli":             sl.Config.CLI,
			"model":           sl.Config.Model,
			"capability_tags": tags,
			"max_concurrency": sl.Config.MaxConcurrency,
			"count":           sl.Count,
		})
	}
	return out
}
