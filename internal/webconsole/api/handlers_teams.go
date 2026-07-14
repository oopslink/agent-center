package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
)

// Team WebUI Phase-1 facade — browser-facing REST under /api/orgs/{slug}/teams/...
// (plan-32dd9107). Response JSON is field-for-field the TS types in
// web/src/api/teams.ts @ 8ee83d33 (the code-anchored contract UDE/PD ratified).
// requireOrgMember gates + resolves the org from {slug}; a nil TeamService degrades
// every endpoint to 501 (mirrors the model-catalog handlers). This file lands the P1
// slice (teams CRUD + members + projects-associate) — the ops backed by existing
// team.Service methods; P2 adds templates / instantiate / memory / extract / directory.

// teamMonogram derives the ≤2-char UPPERCASE display glyph the FE Glyph component
// renders as characters (NOT an emoji — hard project constraint). Rule (agreed with
// UDE): multi-word → the first letter of each of the first two words ("Agent Core"→AC,
// "Dev Experience Team"→DE); single word → its first two letters ("Growth"→GR);
// non-letters are skipped; nothing derivable → "" (the FE falls back on the name).
func teamMonogram(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !isLetterRune(r) })
	// keep only words that start with a letter (already ensured by FieldsFunc split)
	if len(words) >= 2 {
		return strings.ToUpper(firstLetters(words[0], 1) + firstLetters(words[1], 1))
	}
	if len(words) == 1 {
		return strings.ToUpper(firstLetters(words[0], 2))
	}
	return ""
}

func isLetterRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// firstLetters returns up to n leading runes of s (s is already letter-only from the
// FieldsFunc split, so this is a rune-safe prefix).
func firstLetters(s string, n int) string {
	rs := []rune(s)
	if len(rs) > n {
		rs = rs[:n]
	}
	return string(rs)
}

// splitTags parses the FE's comma/space-separated capability_tags string (RoleInput.tags)
// into the domain's []string. Empty entries are dropped.
func splitTags(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// roleViewMap renders a team's RoleConfig as the TS RoleView, stamping count = the
// number of members holding that role (per-role real member count, TeamDetail renders
// "×{count}"). capability_tags is always a (possibly empty) array, never null.
func roleViewMap(rc team.RoleConfig, count int) map[string]any {
	tags := rc.CapabilityTags
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"role":            rc.Role,
		"cli":             rc.CLI,
		"model":           rc.Model,
		"capability_tags": tags,
		"max_concurrency": rc.MaxConcurrency,
		"count":           count,
	}
}

// teamViewMap renders the TS TeamView. members drives members_count + the per-role
// count; projectsCount is passed in (ListProjects len). status is 'active' when the
// team has ≥1 member else 'draft' (no backend status column, Phase-1 derivation).
func teamViewMap(t *team.Team, members []*team.TeamMember, projectsCount int) map[string]any {
	perRole := make(map[string]int, len(t.Roles()))
	for _, m := range members {
		perRole[m.Role]++
	}
	roles := make([]map[string]any, 0, len(t.Roles()))
	for _, rc := range t.Roles() {
		roles = append(roles, roleViewMap(rc, perRole[rc.Role]))
	}
	status := "draft"
	if len(members) > 0 {
		status = "active"
	}
	return map[string]any{
		"id":             string(t.ID()),
		"org_id":         t.OrgID(),
		"name":           t.Name(),
		"description":    t.Description(),
		"roles":          roles,
		"version":        t.Version(),
		"glyph":          teamMonogram(t.Name()),
		"status":         status,
		"members_count":  len(members),
		"projects_count": projectsCount,
		"created":        t.CreatedAt().UTC().Format(time.RFC3339),
	}
}

// memberViewMap renders the TS MemberView. name is the resolved identity display name
// (best-effort — "" when unresolvable). tags/cli/model/concurrency are taken from the
// team's RoleConfig for the member's role (the role declaration, not the identity).
// exclusive is always false in Phase-1 (no team-exclusivity field yet).
func memberViewMap(m *team.TeamMember, roleByName map[string]team.RoleConfig, name string) map[string]any {
	rc := roleByName[m.Role]
	tags := rc.CapabilityTags
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"team_id":     string(m.TeamID),
		"member_ref":  string(m.Ref),
		"kind":        string(m.Kind),
		"role":        m.Role,
		"name":        name,
		"tags":        tags,
		"cli":         rc.CLI,
		"model":       rc.Model,
		"concurrency": strconv.Itoa(rc.MaxConcurrency),
		"exclusive":   false,
	}
}

// projectLinkMap renders the TS TeamProjectLink. name/glyph come from the resolved
// project; repo is a Phase-1 placeholder ("" — the code-repo label resolution lands in
// P2). relation is 'primary' for the team's first (oldest) association else 'linked'.
func projectLinkMap(tp *team.TeamProject, projectName string, relation string) map[string]any {
	return map[string]any{
		"team_id":    string(tp.TeamID),
		"project_id": tp.ProjectID,
		"name":       projectName,
		"glyph":      teamMonogram(projectName),
		"repo":       "",
		"relation":   relation,
	}
}

// mapTeamWebError maps team-domain sentinels to HTTP responses.
func mapTeamWebError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, team.ErrMemberIdentityNotFound):
		// Well-formed ref but no such (matching-kind, same-org) identity — the
		// add-member hardening reject. Distinct code so the FE can surface it.
		writeError(w, http.StatusNotFound, "identity_not_found", err.Error())
	case errors.Is(err, team.ErrTeamNotFound), errors.Is(err, team.ErrMemberNotFound),
		errors.Is(err, team.ErrProjectNotAssociated):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, team.ErrTeamNameTaken), errors.Is(err, team.ErrMemberAlreadyInTeam),
		errors.Is(err, team.ErrAgentAlreadyInTeam), errors.Is(err, team.ErrProjectAlreadyAssociated):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, team.ErrInvalidTeam), errors.Is(err, team.ErrInvalidMemberRef),
		errors.Is(err, team.ErrInvalidRole), errors.Is(err, team.ErrRoleNotDeclared),
		errors.Is(err, team.ErrInvalidProject):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}

// teamGuard runs the shared nil-check + org auth for a team endpoint, returning the
// resolved orgID (and false when a response was already written).
func teamGuard(w http.ResponseWriter, r *http.Request, d HandlerDeps) (string, bool) {
	if d.TeamService == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "team service not wired")
		return "", false
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	return orgID, ok
}

// getTeamInOrg loads a team and enforces it belongs to the request's org (a team id
// from another org is treated as not-found — no cross-org read).
func getTeamInOrg(r *http.Request, d HandlerDeps, orgID, id string) (*team.Team, error) {
	t, err := d.TeamService.GetTeam(r.Context(), team.TeamID(id))
	if err != nil {
		return nil, err
	}
	if t.OrgID() != orgID {
		return nil, team.ErrTeamNotFound
	}
	return t, nil
}

// ---- teams CRUD ----

// listTeamsHandler serves GET /api/orgs/{slug}/teams → TeamView[].
func (s *Server) listTeamsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	teams, err := d.TeamService.ListTeams(r.Context(), orgID)
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(teams))
	for _, t := range teams {
		members, merr := d.TeamService.ListMembers(r.Context(), t.ID())
		if merr != nil {
			mapTeamWebError(w, merr)
			return
		}
		projects, perr := d.TeamService.ListProjects(r.Context(), t.ID())
		if perr != nil {
			mapTeamWebError(w, perr)
			return
		}
		out = append(out, teamViewMap(t, members, len(projects)))
	}
	writeJSON(w, http.StatusOK, out)
}

// getTeamHandler serves GET /api/orgs/{slug}/teams/{id} → TeamView.
func (s *Server) getTeamHandler(w http.ResponseWriter, r *http.Request) {
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

// createTeamReq is the CreateTeamInput body (teams.ts). visibility is accepted but not
// yet persisted (Phase-1 — no visibility column).
type createTeamReq struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Visibility  string         `json:"visibility"`
	Roles       []roleInputReq `json:"roles"`
}

type roleInputReq struct {
	Role           string `json:"role"`
	CLI            string `json:"cli"`
	Model          string `json:"model"`
	MaxConcurrency int    `json:"max_concurrency"`
	Count          int    `json:"count"`
	Tags           string `json:"tags"`
	Description    string `json:"description"`
}

// createTeamHandler serves POST /api/orgs/{slug}/teams → TeamView (201).
func (s *Server) createTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	var req createTeamReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	roles := make([]team.RoleConfig, 0, len(req.Roles))
	for _, ri := range req.Roles {
		roles = append(roles, team.RoleConfig{
			Role: ri.Role, CLI: ri.CLI, Model: ri.Model,
			CapabilityTags: splitTags(ri.Tags), MaxConcurrency: ri.MaxConcurrency,
		})
	}
	t, err := d.TeamService.CreateTeam(r.Context(), teamservice.CreateTeamInput{
		OrgID: orgID, Name: req.Name, Description: req.Description, Roles: roles,
	})
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	// fresh team → no members, no projects.
	writeJSON(w, http.StatusCreated, teamViewMap(t, nil, 0))
}

// deleteTeamHandler serves DELETE /api/orgs/{slug}/teams/{id}.
func (s *Server) deleteTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	orgID, ok := teamGuard(w, r, d)
	if !ok {
		return
	}
	// enforce org ownership before delete.
	if _, err := getTeamInOrg(r, d, orgID, r.PathValue("id")); err != nil {
		mapTeamWebError(w, err)
		return
	}
	if err := d.TeamService.DeleteTeam(r.Context(), team.TeamID(r.PathValue("id"))); err != nil {
		mapTeamWebError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- members ----

// listMembersHandler serves GET /api/orgs/{slug}/teams/{id}/members → MemberView[].
func (s *Server) listTeamMembersHandler(w http.ResponseWriter, r *http.Request) {
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
	members, err := d.TeamService.ListMembers(r.Context(), t.ID())
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	roleByName := rolesByName(t)
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		name := resolveDisplayName(r, d, conversation.IdentityRef(string(m.Ref)))
		out = append(out, memberViewMap(m, roleByName, name))
	}
	writeJSON(w, http.StatusOK, out)
}

func rolesByName(t *team.Team) map[string]team.RoleConfig {
	m := make(map[string]team.RoleConfig, len(t.Roles()))
	for _, rc := range t.Roles() {
		m[rc.Role] = rc
	}
	return m
}

// addMemberReq is the AddMemberInput body (teams.ts). name/kind are FE-side (the
// facade resolves the authoritative name for the response). migrate_from, when
// non-empty, is the SOURCE team id: the request is an agent migration (remove
// from the source + add here, atomically) rather than a plain add — an agent is
// single-team, so joining a second team without leaving the first is otherwise a
// 409. The field is snake_case to match member_ref/team_id (the FE previously sent
// camelCase migrateFrom, which the backend silently dropped).
type addMemberReq struct {
	TeamID      string `json:"team_id"`
	MemberRef   string `json:"member_ref"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Role        string `json:"role"`
	MigrateFrom string `json:"migrate_from"`
}

// addTeamMemberHandler serves POST /api/orgs/{slug}/teams/{id}/members → MemberView (201).
func (s *Server) addTeamMemberHandler(w http.ResponseWriter, r *http.Request) {
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
	var req addMemberReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// migrate_from set → atomic cross-team migration (remove from source + add
	// here, single tx); empty → plain add. Both go through the same identity
	// hardening in the service.
	var m *team.TeamMember
	if req.MigrateFrom != "" {
		m, err = d.TeamService.MoveMember(r.Context(), team.TeamID(req.MigrateFrom), t.ID(), team.MemberRef(req.MemberRef), req.Role)
	} else {
		m, err = d.TeamService.AddMember(r.Context(), t.ID(), team.MemberRef(req.MemberRef), req.Role)
	}
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	name := resolveDisplayName(r, d, conversation.IdentityRef(string(m.Ref)))
	writeJSON(w, http.StatusCreated, memberViewMap(m, rolesByName(t), name))
}

// removeTeamMemberHandler serves DELETE /api/orgs/{slug}/teams/{id}/members/{ref}.
func (s *Server) removeTeamMemberHandler(w http.ResponseWriter, r *http.Request) {
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
	if err := d.TeamService.RemoveMember(r.Context(), t.ID(), team.MemberRef(r.PathValue("ref"))); err != nil {
		mapTeamWebError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- projects ----

// listTeamProjectsHandler serves GET /api/orgs/{slug}/teams/{id}/projects → TeamProjectLink[].
func (s *Server) listTeamProjectsHandler(w http.ResponseWriter, r *http.Request) {
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
	projects, err := d.TeamService.ListProjects(r.Context(), t.ID())
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	// oldest association = 'primary', rest = 'linked' (Phase-1 relation derivation).
	sort.SliceStable(projects, func(i, j int) bool { return projects[i].CreatedAt.Before(projects[j].CreatedAt) })
	out := make([]map[string]any, 0, len(projects))
	for i, tp := range projects {
		relation := "linked"
		if i == 0 {
			relation = "primary"
		}
		out = append(out, projectLinkMap(tp, s.resolveProjectName(r, d, tp.ProjectID), relation))
	}
	writeJSON(w, http.StatusOK, out)
}

// resolveProjectName best-effort resolves a project's display name (falls back to the
// id when no ProjectRepo is wired or the project is gone).
func (s *Server) resolveProjectName(r *http.Request, d HandlerDeps, projectID string) string {
	if d.ProjectRepo == nil {
		return projectID
	}
	p, err := d.ProjectRepo.FindByID(r.Context(), pm.ProjectID(projectID))
	if err != nil || p == nil {
		return projectID
	}
	return p.Name()
}

// associateProjectReq is the associate body ({team_id, project_id, name}).
type associateProjectReq struct {
	TeamID    string `json:"team_id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

// associateTeamProjectHandler serves POST /api/orgs/{slug}/teams/{id}/projects → TeamProjectLink (201).
func (s *Server) associateTeamProjectHandler(w http.ResponseWriter, r *http.Request) {
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
	var req associateProjectReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.TeamService.AssociateProject(r.Context(), t.ID(), req.ProjectID); err != nil {
		mapTeamWebError(w, err)
		return
	}
	// relation: 'primary' iff this is now the team's only association.
	projects, _ := d.TeamService.ListProjects(r.Context(), t.ID())
	relation := "linked"
	if len(projects) <= 1 {
		relation = "primary"
	}
	name := req.Name
	if name == "" {
		name = s.resolveProjectName(r, d, req.ProjectID)
	}
	tp := &team.TeamProject{TeamID: t.ID(), ProjectID: req.ProjectID, CreatedAt: time.Now().UTC()}
	writeJSON(w, http.StatusCreated, projectLinkMap(tp, name, relation))
}
