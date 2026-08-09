package api

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/cognition/memory/teammemory"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	"gopkg.in/yaml.v3"
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
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Roles       *[]roleInputReq `json:"roles"`
}

// updateTeamHandler serves PATCH /api/orgs/{slug}/teams/{id} → TeamView.
func (s *Server) updateTeamHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, member, orgID, ok := teamGuardMember(w, r, d)
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
	var configs *[]team.RoleConfig
	if req.Roles != nil {
		converted := make([]team.RoleConfig, 0, len(*req.Roles))
		for _, ri := range *req.Roles {
			converted = append(converted, team.RoleConfig{Role: ri.Role, CLI: ri.CLI, Model: ri.Model,
				CapabilityTags: splitTags(ri.Tags), MaxConcurrency: ri.MaxConcurrency})
		}
		configs = &converted
	}
	t, err := d.TeamService.UpdateTeam(r.Context(), team.TeamID(r.PathValue("id")), teamservice.UpdateTeamInput{
		Name:        req.Name,
		Description: req.Description,
		Roles:       configs,
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
	writeJSON(w, http.StatusOK, withMemoryPermissions(teamViewMap(t, members, len(projects)), member.Role(), d.TeamGitHost != nil))
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
	_, member, orgID, ok := teamGuardMember(w, r, d)
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
	writeJSON(w, http.StatusCreated, withMemoryPermissions(instantiatedTeamView(t, countByRole), member.Role(), d.TeamGitHost != nil))
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
// team memory
// ---------------------------------------------------------------------------

type teamMemorySnapshot struct {
	Commit    string
	Entries   []centergit.Entry
	Rules     []centergit.Rule
	Proposals []teammemory.ProposalView
}

// teamMemoryIndexHandler serves GET /api/orgs/{slug}/teams/{id}/memory →
// MemoryIndexEntry[]. It exposes the canonical target CAS fields Web needs to
// create update/disable/delete proposals, plus pending/completed proposals from
// the shared TeamMemory service. Git host unwired or an unprovisioned team → []
// (an absent history is empty, not an error).
func (s *Server) teamMemoryIndexHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, _, orgID, ok := teamGuardMember(w, r, d)
	if !ok {
		return
	}
	tm, err := getTeamInOrg(r, d, orgID, r.PathValue("id"))
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	if d.TeamGitHost == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	snap, ok := s.readTeamMemorySnapshot(w, r, d, tm, webActorRef(caller))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, teamMemoryIndexPayload(snap))
}

// teamMemoryDocHandler serves GET /api/orgs/{slug}/teams/{id}/memory/{slug} →
// MemoryDoc for MEMORY.md, an entry, a rule, or a proposal. Proposal documents
// are read through TeamMemoryService; entry/rule documents use the canonical
// Store parser so frontmatter and CAS metadata do not drift.
func (s *Server) teamMemoryDocHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, _, orgID, ok := teamGuardMember(w, r, d)
	if !ok {
		return
	}
	tm, err := getTeamInOrg(r, d, orgID, r.PathValue("id"))
	if err != nil {
		mapTeamWebError(w, err)
		return
	}
	if d.TeamGitHost == nil {
		writeError(w, http.StatusNotFound, "memory_not_found", "memory entry not found")
		return
	}
	actorRef := webActorRef(caller)
	slug := strings.TrimSpace(r.PathValue("entry"))
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind == "proposal" {
		s.writeTeamMemoryProposalDoc(w, r, d, tm.ID().String(), actorRef, slug)
		return
	}
	snap, ok := s.readTeamMemorySnapshot(w, r, d, tm, actorRef)
	if !ok {
		return
	}
	if slug == "MEMORY.md" || kind == "index" {
		writeJSON(w, http.StatusOK, teamMemoryIndexDoc(tm, snap))
		return
	}
	if kind != "rule" {
		for _, e := range snap.Entries {
			if memoryItemMatches(e.Slug, e.SourcePath, slug) {
				writeJSON(w, http.StatusOK, teamMemoryEntryDoc(e))
				return
			}
		}
	}
	if kind != "entry" {
		for _, rule := range snap.Rules {
			if memoryItemMatches(rule.Slug, rule.SourcePath, slug) {
				writeJSON(w, http.StatusOK, teamMemoryRuleDoc(rule))
				return
			}
		}
		for _, proposal := range snap.Proposals {
			if proposal.Proposal.ProposalID == slug {
				writeJSON(w, http.StatusOK, teamMemoryProposalDoc(proposal))
				return
			}
		}
	}
	writeError(w, http.StatusNotFound, "memory_not_found", "memory entry not found")
}

type createTeamMemoryProposalReq struct {
	Operation      string                `json:"operation"`
	TargetKind     string                `json:"target_kind"`
	Kind           string                `json:"kind"`
	Target         *teammemory.TargetRef `json:"target"`
	Candidate      *teammemory.Candidate `json:"candidate"`
	Rationale      string                `json:"rationale"`
	EvidenceRefs   []string              `json:"evidence_refs"`
	IdempotencyKey string                `json:"idempotency_key"`
}

func (s *Server) createTeamMemoryProposalHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, _, tm, svc, projector, ok := s.requireWebTeamMemoryService(w, r, d, true)
	if !ok {
		return
	}
	var req createTeamMemoryProposalReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	kind := strings.TrimSpace(req.TargetKind)
	if kind == "" {
		kind = req.Kind
	}
	res, err := svc.ProposeForTeam(r.Context(), tm.ID().String(), teammemory.ProposeCommand{
		ActorRef:       webActorRef(caller),
		IdempotencyKey: req.IdempotencyKey,
		Operation:      teammemory.Operation(strings.ToLower(strings.TrimSpace(req.Operation))),
		TargetKind:     teammemory.TargetKind(strings.ToLower(strings.TrimSpace(kind))),
		Target:         req.Target,
		Candidate:      req.Candidate,
		Rationale:      req.Rationale,
		EvidenceRefs:   req.EvidenceRefs,
	})
	if err != nil {
		mapTeamMemoryWebError(w, err)
		return
	}
	_ = projector.ReconcileTeam(r.Context(), res.TeamID)
	writeJSON(w, http.StatusCreated, teamMemoryResultPayload(res))
}

type webReviewTeamMemoryReq struct {
	Action                 string   `json:"action"`
	ExpectedRepoCommit     string   `json:"expected_repo_commit"`
	ExpectedProposalStatus string   `json:"expected_proposal_status"`
	Comment                string   `json:"comment"`
	AcknowledgeWarnings    []string `json:"acknowledge_warnings"`
}

func (s *Server) reviewTeamMemoryProposalHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	caller, _, tm, svc, projector, ok := s.requireWebTeamMemoryService(w, r, d, true)
	if !ok {
		return
	}
	var req webReviewTeamMemoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	proposalID := r.PathValue("proposal_id")
	actorRef := webActorRef(caller)
	cmd := teammemory.ReviewCommand{
		ActorRef:               actorRef,
		ProposalID:             proposalID,
		Action:                 teammemory.ReviewAction(strings.ToLower(strings.TrimSpace(req.Action))),
		ExpectedRepoCommit:     req.ExpectedRepoCommit,
		ExpectedProposalStatus: teammemory.ProposalStatus(strings.ToLower(strings.TrimSpace(req.ExpectedProposalStatus))),
		Comment:                req.Comment,
		AcknowledgeWarnings:    req.AcknowledgeWarnings,
	}
	res, err := svc.Review(r.Context(), tm.ID().String(), cmd)
	if err != nil {
		if errors.Is(err, teammemory.ErrTeamMemoryVersionConflict) {
			actual := ""
			if view, getErr := svc.Get(r.Context(), teammemory.GetCommand{ActorRef: actorRef, TeamID: tm.ID().String(), ProposalID: proposalID}); getErr == nil {
				actual = view.RepoCommit
			}
			_ = projector.EmitPromotionConflict(r.Context(), tm.ID().String(), proposalID, actorRef, req.ExpectedRepoCommit, actual)
		}
		mapTeamMemoryWebError(w, err)
		return
	}
	_ = projector.ReconcileTeam(r.Context(), res.TeamID)
	writeJSON(w, http.StatusOK, teamMemoryResultPayload(res))
}

func (s *Server) readTeamMemorySnapshot(w http.ResponseWriter, r *http.Request, d HandlerDeps, tm *team.Team, actorRef string) (teamMemorySnapshot, bool) {
	consumer := centergit.NewTeamMemoryConsumer(d.TeamGitHost, nil)
	entries, _, err := consumer.ReadTeam(r.Context(), tm.ID().String())
	if err != nil {
		mapTeamMemoryWebError(w, err)
		return teamMemorySnapshot{}, false
	}
	rules, _, err := consumer.ReadTeamAllRules(r.Context(), tm.ID().String())
	if err != nil {
		mapTeamMemoryWebError(w, err)
		return teamMemorySnapshot{}, false
	}
	repo := centergit.NewTeamMemoryRepository(d.TeamGitHost, nil)
	svc := teammemory.NewService(repo, teammemory.NewTeamPolicyAuthorizationFromService(d.TeamService, d.MemberRepo))
	list, err := svc.List(r.Context(), teammemory.ListCommand{
		ActorRef: actorRef,
		TeamID:   tm.ID().String(),
		Status: []teammemory.ProposalStatus{
			teammemory.StatusPending,
			teammemory.StatusPromoted,
			teammemory.StatusRejected,
			teammemory.StatusSuperseded,
		},
		Limit: 100,
	})
	if err != nil {
		mapTeamMemoryWebError(w, err)
		return teamMemorySnapshot{}, false
	}
	return teamMemorySnapshot{Commit: list.RepoCommit, Entries: entries, Rules: rules, Proposals: list.Proposals}, true
}

func (s *Server) writeTeamMemoryProposalDoc(w http.ResponseWriter, r *http.Request, d HandlerDeps, teamID, actorRef, proposalID string) {
	if d.TeamGitHost == nil {
		writeError(w, http.StatusNotFound, "memory_not_found", "memory entry not found")
		return
	}
	repo := centergit.NewTeamMemoryRepository(d.TeamGitHost, nil)
	svc := teammemory.NewService(repo, teammemory.NewTeamPolicyAuthorizationFromService(d.TeamService, d.MemberRepo))
	view, err := svc.Get(r.Context(), teammemory.GetCommand{ActorRef: actorRef, TeamID: teamID, ProposalID: proposalID})
	if err != nil {
		mapTeamMemoryWebError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, teamMemoryProposalDoc(view))
}

func (s *Server) requireWebTeamMemoryService(w http.ResponseWriter, r *http.Request, d HandlerDeps, requireManage bool) (*identity.Identity, *identity.Member, *team.Team, *teammemory.Service, *teammemory.Projector, bool) {
	caller, member, orgID, ok := teamGuardMember(w, r, d)
	if !ok {
		return nil, nil, nil, nil, nil, false
	}
	if requireManage && !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "not_memory_curator", "team memory changes require org owner/admin")
		return nil, nil, nil, nil, nil, false
	}
	tm, err := getTeamInOrg(r, d, orgID, r.PathValue("id"))
	if err != nil {
		mapTeamWebError(w, err)
		return nil, nil, nil, nil, nil, false
	}
	if d.TeamGitHost == nil {
		writeError(w, http.StatusNotImplemented, "team_memory_not_wired", "team memory git host is not wired")
		return nil, nil, nil, nil, nil, false
	}
	repo := centergit.NewTeamMemoryRepository(d.TeamGitHost, nil)
	svc := teammemory.NewService(repo, teammemory.NewTeamPolicyAuthorizationFromService(d.TeamService, d.MemberRepo))
	projector := teammemory.NewProjector(nil, nil, nil, nil)
	if seq, ok := d.EventRepo.(observability.SeqAllocator); ok && d.EventRepo != nil {
		projector = teammemory.NewProjector(d.DB, repo, d.EventRepo, seq)
	}
	return caller, member, tm, svc, projector, true
}

func teamMemoryIndexPayload(snap teamMemorySnapshot) []map[string]any {
	out := make([]map[string]any, 0, 3+len(snap.Entries)+len(snap.Rules)+len(snap.Proposals))
	out = append(out, map[string]any{
		"slug":   "MEMORY.md",
		"path":   "MEMORY.md",
		"pinned": true,
		"kind":   "index",
		"commit": snap.Commit,
	})
	out = append(out, map[string]any{"group": "entries/"})
	for _, e := range snap.Entries {
		out = append(out, map[string]any{
			"slug":        e.Slug,
			"path":        e.SourcePath,
			"source_path": e.SourcePath,
			"kind":        "entry",
			"uuid":        e.UUID,
			"blob_hash":   e.BlobHash,
			"title":       e.Title,
			"description": e.Description,
			"type":        e.Type,
			"commit":      snap.Commit,
		})
	}
	out = append(out, map[string]any{"group": "rules/"})
	for _, rule := range snap.Rules {
		out = append(out, map[string]any{
			"slug":        rule.Slug,
			"path":        rule.SourcePath,
			"source_path": rule.SourcePath,
			"kind":        "rule",
			"uuid":        rule.UUID,
			"blob_hash":   rule.BlobHash,
			"title":       rule.Title,
			"description": rule.Description,
			"enabled":     rule.Enabled,
			"applies_to":  append([]string(nil), rule.AppliesTo...),
			"commit":      snap.Commit,
		})
	}
	out = append(out, map[string]any{"group": "proposals/"})
	for _, view := range snap.Proposals {
		p := view.Proposal
		out = append(out, map[string]any{
			"slug":                     p.ProposalID,
			"path":                     p.SourcePath,
			"source_path":              p.SourcePath,
			"kind":                     "proposal",
			"proposal_id":              p.ProposalID,
			"operation":                p.Operation,
			"target_kind":              p.TargetKind,
			"status":                   p.Status,
			"repo_commit":              view.RepoCommit,
			"current_target_blob_hash": view.CurrentTargetBlobHash,
			"created_at":               p.CreatedAt,
		})
	}
	return out
}

func teamMemoryIndexDoc(tm *team.Team, snap teamMemorySnapshot) map[string]any {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s memory\n\n", tm.Name())
	fmt.Fprintf(&b, "Repository commit: `%s`\n\n", snap.Commit)
	if len(snap.Entries) > 0 {
		b.WriteString("## Entries\n")
		for _, e := range snap.Entries {
			fmt.Fprintf(&b, "- `%s` - %s\n", e.SourcePath, e.Description)
		}
		b.WriteString("\n")
	}
	if len(snap.Rules) > 0 {
		b.WriteString("## Rules\n")
		for _, rule := range snap.Rules {
			status := "disabled"
			if rule.Enabled {
				status = "enabled"
			}
			fmt.Fprintf(&b, "- `%s` - %s (%s)\n", rule.SourcePath, rule.Description, status)
		}
		b.WriteString("\n")
	}
	if len(snap.Proposals) > 0 {
		b.WriteString("## Proposals\n")
		for _, view := range snap.Proposals {
			p := view.Proposal
			fmt.Fprintf(&b, "- `%s` - %s %s (%s)\n", p.ProposalID, p.Operation, p.TargetKind, p.Status)
		}
	}
	return map[string]any{
		"slug":        "MEMORY.md",
		"path":        "MEMORY.md",
		"title":       tm.Name() + " memory",
		"kind":        "index",
		"frontmatter": nil,
		"body":        b.String(),
		"repo_commit": snap.Commit,
	}
}

func teamMemoryEntryDoc(e centergit.Entry) map[string]any {
	fm := struct {
		Name        string `yaml:"name"`
		Title       string `yaml:"title,omitempty"`
		Description string `yaml:"description"`
		UUID        string `yaml:"uuid"`
		Type        string `yaml:"type,omitempty"`
		BlobHash    string `yaml:"blob_hash,omitempty"`
	}{Name: e.Slug, Title: e.Title, Description: e.Description, UUID: e.UUID, Type: e.Type, BlobHash: e.BlobHash}
	title := e.Title
	if title == "" {
		title = e.Slug
	}
	return map[string]any{
		"slug":        e.Slug,
		"path":        e.SourcePath,
		"title":       title,
		"kind":        "entry",
		"frontmatter": yamlFrontmatter(fm),
		"body":        e.Body,
		"source_path": e.SourcePath,
		"uuid":        e.UUID,
		"blob_hash":   e.BlobHash,
	}
}

func teamMemoryRuleDoc(rule centergit.Rule) map[string]any {
	fm := struct {
		Name        string   `yaml:"name"`
		Title       string   `yaml:"title,omitempty"`
		Description string   `yaml:"description"`
		UUID        string   `yaml:"uuid"`
		Enabled     bool     `yaml:"enabled"`
		AppliesTo   []string `yaml:"applies_to"`
		BlobHash    string   `yaml:"blob_hash,omitempty"`
	}{Name: rule.Slug, Title: rule.Title, Description: rule.Description, UUID: rule.UUID, Enabled: rule.Enabled, AppliesTo: rule.AppliesTo, BlobHash: rule.BlobHash}
	title := rule.Title
	if title == "" {
		title = rule.Slug
	}
	return map[string]any{
		"slug":        rule.Slug,
		"path":        rule.SourcePath,
		"title":       title,
		"kind":        "rule",
		"frontmatter": yamlFrontmatter(fm),
		"body":        rule.Body,
		"source_path": rule.SourcePath,
		"uuid":        rule.UUID,
		"blob_hash":   rule.BlobHash,
		"enabled":     rule.Enabled,
		"applies_to":  append([]string(nil), rule.AppliesTo...),
	}
}

func teamMemoryProposalDoc(view teammemory.ProposalView) map[string]any {
	p := view.Proposal
	fm := struct {
		ProposalID            string                    `yaml:"proposal_id"`
		Operation             teammemory.Operation      `yaml:"operation"`
		TargetKind            teammemory.TargetKind     `yaml:"target_kind"`
		Status                teammemory.ProposalStatus `yaml:"status"`
		AuthorRef             string                    `yaml:"author_ref,omitempty"`
		ReviewerRef           string                    `yaml:"reviewer_ref,omitempty"`
		RepoCommit            string                    `yaml:"repo_commit,omitempty"`
		CurrentTargetBlobHash string                    `yaml:"current_target_blob_hash,omitempty"`
		PromotionCommit       string                    `yaml:"promotion_commit,omitempty"`
		EffectiveRefresh      string                    `yaml:"effective_refresh,omitempty"`
	}{ProposalID: p.ProposalID, Operation: p.Operation, TargetKind: p.TargetKind, Status: p.Status, AuthorRef: p.AuthorRef, ReviewerRef: p.ReviewerRef, RepoCommit: view.RepoCommit, CurrentTargetBlobHash: view.CurrentTargetBlobHash, PromotionCommit: p.PromotionCommit}
	if p.Status == teammemory.StatusPromoted {
		fm.EffectiveRefresh = teammemory.EffectiveForNewSessionsAndForks
	}
	body := proposalMarkdown(view)
	return map[string]any{
		"slug":                     p.ProposalID,
		"path":                     p.SourcePath,
		"title":                    p.ProposalID,
		"kind":                     "proposal",
		"frontmatter":              yamlFrontmatter(fm),
		"body":                     body,
		"proposal":                 teamMemoryViewPayload(view),
		"repo_commit":              view.RepoCommit,
		"current_target_blob_hash": view.CurrentTargetBlobHash,
	}
}

func teamMemoryResultPayload(res teammemory.Result) map[string]any {
	return map[string]any{
		"team_id":       res.TeamID,
		"proposal_id":   res.ProposalID,
		"status":        res.Status,
		"repo_commit":   res.RepoCommit,
		"source_path":   res.SourcePath,
		"warnings":      res.Warnings,
		"effective_for": res.EffectiveFor,
		"old_commit":    res.OldCommit,
		"new_commit":    res.NewCommit,
	}
}

func teamMemoryViewPayload(view teammemory.ProposalView) map[string]any {
	p := view.Proposal
	return map[string]any{
		"team_id":                  p.TeamID,
		"proposal_id":              p.ProposalID,
		"operation":                p.Operation,
		"target_kind":              p.TargetKind,
		"target":                   p.Target,
		"candidate":                p.Candidate,
		"rationale":                p.Rationale,
		"evidence_refs":            p.EvidenceRefs,
		"author_ref":               p.AuthorRef,
		"created_at":               p.CreatedAt,
		"idempotency_key":          p.IdempotencyKey,
		"status":                   p.Status,
		"warnings":                 p.Warnings,
		"reviewer_ref":             p.ReviewerRef,
		"review_comment":           p.ReviewComment,
		"reviewed_at":              p.ReviewedAt,
		"promotion_commit":         p.PromotionCommit,
		"source_path":              p.SourcePath,
		"repo_commit":              view.RepoCommit,
		"current_target_blob_hash": view.CurrentTargetBlobHash,
		"diff_preview":             view.DiffPreview,
	}
}

func proposalMarkdown(view teammemory.ProposalView) string {
	p := view.Proposal
	var b strings.Builder
	fmt.Fprintf(&b, "## %s %s\n\n", p.Operation, p.TargetKind)
	fmt.Fprintf(&b, "- Status: `%s`\n", p.Status)
	fmt.Fprintf(&b, "- Repository commit: `%s`\n", view.RepoCommit)
	if p.Target != nil {
		fmt.Fprintf(&b, "- Target: `%s`\n", p.Target.SourcePath)
		fmt.Fprintf(&b, "- Target UUID: `%s`\n", p.Target.UUID)
		fmt.Fprintf(&b, "- Expected blob: `%s`\n", p.Target.ExpectedBlobHash)
	}
	if view.CurrentTargetBlobHash != "" {
		fmt.Fprintf(&b, "- Current target blob: `%s`\n", view.CurrentTargetBlobHash)
	}
	if p.Rationale != "" {
		fmt.Fprintf(&b, "\n### Rationale\n\n%s\n", p.Rationale)
	}
	if len(p.Warnings) > 0 {
		b.WriteString("\n### Warnings\n\n")
		for _, warning := range p.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
	}
	if view.DiffPreview != "" {
		fmt.Fprintf(&b, "\n### Diff Preview\n\n```diff\n%s\n```\n", view.DiffPreview)
	}
	if p.Candidate != nil {
		fmt.Fprintf(&b, "\n### Candidate Body\n\n%s\n", p.Candidate.Body)
	}
	return b.String()
}

func yamlFrontmatter(v any) string {
	out, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func webActorRef(i *identity.Identity) string {
	if i == nil {
		return ""
	}
	return "user:" + i.ID()
}

func memoryItemMatches(slug, sourcePath, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	if slug == want || sourcePath == want {
		return true
	}
	base := path.Base(sourcePath)
	return base == want || strings.TrimSuffix(base, ".md") == want
}

func mapTeamMemoryWebError(w http.ResponseWriter, err error) {
	reason := teammemory.Reason(err)
	switch reason {
	case "team_memory_not_wired":
		writeError(w, http.StatusNotImplemented, reason, err.Error())
	case "not_team_member", "proposal_not_found":
		writeError(w, http.StatusNotFound, reason, err.Error())
	case "not_memory_curator":
		writeError(w, http.StatusForbidden, reason, err.Error())
	case "invalid_candidate", "secret_detected":
		writeError(w, http.StatusBadRequest, reason, err.Error())
	case "warning_unacknowledged", "target_changed", "proposal_not_pending", "idempotency_conflict", "team_memory_version_conflict":
		writeError(w, http.StatusConflict, reason, err.Error())
	case "git_unavailable":
		writeError(w, http.StatusServiceUnavailable, reason, err.Error())
	case "team_memory_error":
		mapTeamWebError(w, err)
	default:
		mapTeamWebError(w, err)
	}
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
