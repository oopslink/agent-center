package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// resolveCallerAndOrg is the strict org-scope resolver for member endpoints.
// It delegates to requireOrgMember (v2.6 X1 §1): NO first-org fallback. Missing
// or unknown org scope → 400; non-member → 403; unauthenticated → 401. On
// failure the error response is already written and ok=false is returned.
func (s *Server) resolveCallerAndOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps) (callerIdentity *identity.Identity, callerMember *identity.Member, orgID string, ok bool) {
	return requireOrgMember(w, r, d)
}

// listMembersHandler handles GET /api/orgs/{slug}/members (org via path).
func (s *Server) listMembersHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	members, err := d.MemberRepo.ListByOrganization(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	// v2.6 X1 §4: enrich agent members with "running on {worker}". Build an
	// identity_id → worker_id map from the org's AgentInstances (an agent
	// member may not yet have a bound AgentInstance — then worker is empty).
	agentWorker := map[string]string{}
	if d.AgentInstanceRepo != nil {
		instances, aerr := d.AgentInstanceRepo.FindAll(r.Context(), workforce.AgentInstanceFilter{OrganizationID: orgID})
		if aerr == nil {
			for _, ai := range instances {
				if ai.IdentityID() != "" && ai.WorkerID() != nil {
					agentWorker[ai.IdentityID()] = string(*ai.WorkerID())
				}
			}
		}
	}
	arr := make([]map[string]any, 0, len(members))
	for _, m := range members {
		row := memberPublicMap(m)
		if wid, ok := agentWorker[m.IdentityID()]; ok {
			row["worker_id"] = wid
		}
		// v2.7 #160: enrich with the identity's display name. The Member AR only
		// carries identity_id; the UI needs a human name to render message senders
		// + the participant list (else it shows the raw "user:<id>" ref).
		// Best-effort — a miss leaves display_name unset and the UI falls back to
		// the ref.
		if d.IdentityRepo != nil {
			if ident, err := d.IdentityRepo.GetByID(r.Context(), m.IdentityID()); err == nil && ident != nil {
				row["display_name"] = ident.DisplayName()
				// v2.7.1 #214: the Humans page shows email / created / last-active for
				// human members. Emit explicit null (not "" / omitted) for the nullable
				// fields so the UI's null-handling is unambiguous (Tester seam contract).
				if ident.Kind() == identity.KindUser {
					addUserProfileFields(row, ident)
				} else if ident.Kind() == identity.KindAgent && d.AgentSvc != nil {
					// v2.7.1 #226: the worker binding lives on the Agent AR (the unified
					// CreateAgent sets WorkerID but does NOT create a legacy AgentInstance),
					// so the agentWorker map above is empty for unified-create agents and the
					// Members→Agents "Running on" column showed "Not bound" forever. Resolve
					// worker_id read-time from the Agent BC (the source of truth), overriding
					// the legacy-map value. Best-effort — unresolved leaves the prior value.
					if ag, aerr := d.AgentSvc.ResolveAgent(r.Context(), m.IdentityID()); aerr == nil && ag != nil {
						if wid := ag.WorkerID(); wid != "" {
							row["worker_id"] = wid
						}
					}
				}
			}
		}
		arr = append(arr, row)
	}
	writeJSON(w, http.StatusOK, arr)
}

// addMemberHandler handles POST /api/members(org via /api/orgs/{slug} path).
// Creates a NEW user identity (with temp passcode) + member row, OR adds an
// existing identity by display_name when ?reuse=true. Default behavior is
// "create new user" per v2.6 acceptance plan §3.
// Requires owner or admin role.
func (s *Server) addMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberCreateUserSvc == nil && d.MemberAddSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member add not configured")
		return
	}
	callerID, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can add members")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Reuse       bool   `json:"reuse"` // when true, add existing identity instead of creating new
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if body.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name_required", "display_name is required")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if string(callerMember.Role()) == "admin" && body.Role == "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "admin cannot add owner-role members")
		return
	}

	// Path A: reuse existing identity (backwards-compat).
	if body.Reuse {
		if d.MemberAddSvc == nil {
			writeError(w, http.StatusNotImplemented, "not_configured", "reuse path not configured")
			return
		}
		member, err := d.MemberAddSvc.Add(r.Context(), orgID, body.DisplayName, body.Role, callerID.ID())
		if err != nil {
			if errors.Is(err, identity.ErrIdentityNotFound) {
				writeError(w, http.StatusNotFound, "identity_not_found", "identity not found")
				return
			}
			if errors.Is(err, identity.ErrMemberAlreadyExists) {
				writeError(w, http.StatusConflict, "already_member", "identity is already a member")
				return
			}
			writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, memberPublicMap(member))
		return
	}

	// Path B (default v2.6 §3): create new user identity + member with temp passcode.
	res, err := d.MemberCreateUserSvc.Create(r.Context(), orgID, body.DisplayName, body.Role, callerID.ID())
	if err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	result := memberPublicMap(res.Member)
	result["temp_passcode"] = res.TempPasscode // returned ONCE; UI must show then never echo again
	result["display_name"] = res.Identity.DisplayName()
	writeJSON(w, http.StatusCreated, result)
}

// addAgentMemberHandler handles POST /api/members/agent(org via /api/orgs/{slug} path).
// Creates a new agent identity + member.
// defaultAgentModel is the API-layer fallback applied when an agent is created
// without an explicit model (v2.7.1 #236). Mirrors the frontend
// DEFAULT_AGENT_MODEL constant — kept in sync so the visible prefill and the
// backend floor agree.
const defaultAgentModel = "claude-opus-4-8"

func (s *Server) addAgentMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentProvisionSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "agent provision not configured")
		return
	}
	callerID, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if !callerMember.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can add agents")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Role        string `json:"role"`
		// v2.7 #157: when present, Members→Add Agent ALSO creates the execution
		// Agent in one step (the unified create). worker_id is required for an
		// execution agent (immutable binding, ADR-0049). Absent → identity-member
		// only (legacy path).
		Model string `json:"model"`
		CLI   string `json:"cli"`
		// T236: optional LLM tuning at create time ("" = runtime/center default).
		Reasoning string            `json:"reasoning"`
		Mode      string            `json:"mode"`
		Provider  string            `json:"provider"`
		WorkerID  string            `json:"worker_id"`
		EnvVars   map[string]string `json:"env_vars"`
		Skills    []string          `json:"skills"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if body.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name_required", "display_name is required")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	// v2.7.1 #236: API-layer model default. An empty model stores null → the
	// AgentDetail Profile renders blank (@oopslink dogfood; recurred via the
	// MemberNew create path that the #232 frontend prefill missed). BOTH create
	// UIs (AgentCreateModal + MemberNew) POST here — the v2.7 single create path
	// — so defaulting at this one boundary is the bulletproof floor for every
	// caller (both UIs + direct API + any future entry), complementing the
	// frontend prefill that supplies the visible UX. Mirrors the worker_id policy
	// below: an API-LAYER choice, deliberately NOT a new domain invariant
	// (don't push an implementation constraint across the model boundary).
	if strings.TrimSpace(body.Model) == "" {
		body.Model = defaultAgentModel
	}
	// v2.6 ship-block fix (X1 §3): admin cannot create owner-role agent.
	if string(callerMember.Role()) == "admin" && body.Role == "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "admin cannot add owner-role agents")
		return
	}
	// v2.7 business-layer policy (#185 / no-middle-state): agent provision REQUIRES
	// worker_id — validated up front so no identity/member is provisioned for a
	// request that can't yield a runnable agent. This is an API-LAYER implementation
	// choice, deliberately NOT baked into the domain as a new invariant (@oopslink:
	// don't push an implementation constraint across the model boundary). When cloud
	// agents land (v2.8+), this check relaxes HERE (deferred-with-trigger). NOTE: the
	// agent AR also requires a worker today (ADR-0049 §5); that pre-existing domain
	// invariant relaxes together with this in the cloud-agent work.
	if strings.TrimSpace(body.WorkerID) == "" {
		writeError(w, http.StatusBadRequest, "worker_id_required",
			"worker_id is required (v2.7: an execution agent must bind a worker)")
		return
	}

	// v2.7 #157: unified one-step create. The agent identity + Member (identity BC)
	// and the execution Agent (agent BC) are created in ONE outer transaction.
	// persistence.RunInTx is reentrant, so both services' inner RunInTx join this
	// outer tx — if the execution-agent create fails (e.g. worker not in org), the
	// identity+member provision ROLLS BACK too: both-or-neither, no orphan.
	var (
		member     *identity.Member
		idn        *identity.Identity
		agentID    agentbc.AgentID
		agentPhase bool // true once we're in the execution-agent create (error mapping)
	)
	txErr := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		provRes, perr := d.AgentProvisionSvc.Provision(txCtx,
			identity.AgentProvisionForm{
				DisplayName: body.DisplayName,
				Description: body.Description,
				Role:        identity.MemberRole(body.Role),
			},
			orgID, callerID.ID())
		if perr != nil {
			return perr
		}
		member, idn = provRes.Member, provRes.Identity
		// worker_id is guaranteed non-empty (validated before the tx) — agent
		// provision always creates the execution agent now (no identity-only path).
		if d.AgentSvc == nil {
			writeError(w, http.StatusNotImplemented, "agent_not_wired", "")
			return errAbortHandled
		}
		agentPhase = true
		aid, aerr := d.AgentSvc.CreateAgent(txCtx, agentsvc.CreateAgentCommand{
			OrganizationID:   orgID,
			Name:             body.DisplayName,
			Description:      body.Description,
			Model:            body.Model,
			CLI:              body.CLI,
			Reasoning:        body.Reasoning,
			Mode:             body.Mode,
			Provider:         body.Provider,
			EnvVars:          body.EnvVars,
			Skills:           body.Skills,
			WorkerID:         body.WorkerID,
			CreatedBy:        agentCallerRef(callerID),
			IdentityMemberID: idn.ID(),
		})
		if aerr != nil {
			return aerr
		}
		agentID = aid
		return nil
	})
	if txErr != nil {
		if errors.Is(txErr, errAbortHandled) {
			return // response already written
		}
		if agentPhase {
			mapAgentError(w, txErr)
			return
		}
		writeError(w, mapIdentityError(txErr), identityErrCode(txErr), txErr.Error())
		return
	}
	result := memberPublicMap(member)
	result["display_name"] = idn.DisplayName()
	// v2.7 #185: the member id (result["id"], == idn.ID()) IS the business-layer
	// agent id. The execution-entity id (agentID) is internal and must NOT be
	// exposed; callers that need the created agent use the member id.
	_ = agentID
	writeJSON(w, http.StatusCreated, result)
}

// errAbortHandled signals a RunInTx closure already wrote the HTTP response and
// the tx should roll back without further error mapping.
var errAbortHandled = errors.New("handler: response already written")

// requireTargetMemberInOrg validates that memberID exists and belongs to orgID.
// Returns the target Member on success; on failure writes the error response.
// Prevents cross-org member mutations (PM X1 §2 ship-block).
func (s *Server) requireTargetMemberInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps, memberID, orgID string) (*identity.Member, bool) {
	target, err := d.MemberRepo.GetByID(r.Context(), memberID)
	if err != nil || target == nil {
		writeError(w, http.StatusNotFound, "member_not_found", "target member not found")
		return nil, false
	}
	if target.OrganizationID() != orgID {
		writeError(w, http.StatusForbidden, "forbidden", "target member is not in this organization")
		return nil, false
	}
	return target, true
}

// changeMemberRoleHandler handles PATCH /api/members/{id}/role(org via /api/orgs/{slug} path).
// Requires owner role (only owners can change roles to prevent privilege escalation).
func (s *Server) changeMemberRoleHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberRoleChangeSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "role change not configured")
		return
	}
	callerID, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", "only owners can change member roles")
		return
	}
	memberID := r.PathValue("id")
	// v2.6 ship-block fix (X1 §2): verify target member is in the same org as the caller's resolved scope.
	if _, ok := s.requireTargetMemberInOrg(w, r, d, memberID, orgID); !ok {
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := d.MemberRoleChangeSvc.Change(r.Context(), memberID, identity.MemberRole(body.Role), callerID.ID()); err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// disableMemberHandler handles POST /api/members/{id}/disable(org via /api/orgs/{slug} path).
// Requires owner or admin role.
func (s *Server) disableMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberDisableSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member disable not configured")
		return
	}
	_, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can disable members")
		return
	}
	memberID := r.PathValue("id")
	if _, ok := s.requireTargetMemberInOrg(w, r, d, memberID, orgID); !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &body)
	if err := d.MemberDisableSvc.Disable(r.Context(), memberID, body.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, "disable_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reEnableMemberHandler handles POST /api/members/{id}/reenable(org via /api/orgs/{slug} path).
// Requires owner or admin role.
func (s *Server) reEnableMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberDisableSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member reenable not configured")
		return
	}
	_, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can re-enable members")
		return
	}
	memberID := r.PathValue("id")
	if _, ok := s.requireTargetMemberInOrg(w, r, d, memberID, orgID); !ok {
		return
	}
	if err := d.MemberDisableSvc.ReEnable(r.Context(), memberID); err != nil {
		writeError(w, http.StatusInternalServerError, "reenable_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// dropMemberHandler handles DELETE /api/members/{id}(org via /api/orgs/{slug} path).
// Requires owner or admin role. The domain service currently performs a soft
// removal so audit history remains attached to the org.
func (s *Server) dropMemberHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.MemberRemoveSvc == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "member remove not configured")
		return
	}
	caller, callerMember, orgID, ok := s.resolveCallerAndOrg(w, r, d)
	if !ok {
		return
	}
	if string(callerMember.Role()) == "member" {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can drop members")
		return
	}
	memberID := r.PathValue("id")
	if _, ok := s.requireTargetMemberInOrg(w, r, d, memberID, orgID); !ok {
		return
	}
	if err := d.MemberRemoveSvc.Remove(r.Context(), memberID, caller.ID()); err != nil {
		writeError(w, mapIdentityError(err), identityErrCode(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func memberPublicMap(m *identity.Member) map[string]any {
	// Infer kind from identity_id prefix ("user-" or "agent-").
	kind := "user"
	if len(m.IdentityID()) >= 6 && m.IdentityID()[:6] == "agent-" {
		kind = "agent"
	}
	return map[string]any{
		"id":              m.ID(),
		"organization_id": m.OrganizationID(),
		"identity_id":     m.IdentityID(),
		"kind":            kind,
		"role":            string(m.Role()),
		"status":          string(m.Status()),
		"joined_at":       m.JoinedAt().Format("2006-01-02T15:04:05Z"),
	}
}
