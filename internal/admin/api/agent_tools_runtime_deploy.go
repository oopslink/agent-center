package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/workforce"
)

const cmdTypeRuntimeDeployRestart = "runtime.deploy_restart"

var fullGitSHARe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type deployRuntimeReq struct {
	AgentID          string `json:"agent_id"`
	CommitSHA        string `json:"commit_sha"`
	AncestorSHA      string `json:"ancestor_sha"`
	PushedRef        string `json:"pushed_ref"`
	ExactSHAVerified bool   `json:"exact_sha_verified"`
	AncestorVerified bool   `json:"ancestor_verified"`
	IdempotencyKey   string `json:"idempotency_key"`
}

type runtimeStatusReq struct {
	AgentID   string `json:"agent_id"`
	CommandID string `json:"command_id,omitempty"`
}

func (s *Server) deployRuntimeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req deployRuntimeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	workerID := a.WorkerID()
	if !s.requireRuntimePermission(w, r, d, a, "runtime.deploy") {
		return
	}
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_svc_not_wired", "")
		return
	}
	if errCode, msg := validateRuntimeDeployEvidence(req); errCode != "" {
		writeError(w, http.StatusBadRequest, errCode, msg)
		return
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		key = "runtime_deploy:" + workerID + ":" + strings.ToLower(req.CommitSHA)
	}
	payload, _ := json.Marshal(map[string]any{
		"requested_by_agent_id": req.AgentID,
		"agent_id":              req.AgentID,
		"worker_id":             workerID,
		"commit_sha":            strings.ToLower(req.CommitSHA),
		"ancestor_sha":          strings.ToLower(req.AncestorSHA),
		"pushed_ref":            strings.TrimSpace(req.PushedRef),
		"exact_sha_verified":    req.ExactSHAVerified,
		"ancestor_verified":     req.AncestorVerified,
		"restart_semantics":     "complete only after command_status=succeeded and runtime_readback.build_commit equals commit_sha",
	})
	evt, err := d.EnvControlSvc.EnqueueCommand(r.Context(), environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    cmdTypeRuntimeDeployRestart,
		IdempotencyKey: key,
		Payload:        string(payload),
		AgentID:        req.AgentID,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":               "accepted",
		"completion_semantics": "accepted/enqueued is not deployed; complete only when get_runtime_status reports command_status=succeeded and actual.build_commit equals target.commit_sha",
		"command":              runtimeControlEventMap(evt),
		"runtime":              s.runtimeReadback(r.Context(), d, workerID, evt.ID()),
	})
}

func (s *Server) getRuntimeStatusHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req runtimeStatusReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if !s.requireRuntimePermission(w, r, d, a, "runtime.status.read") {
		return
	}
	writeJSON(w, http.StatusOK, s.runtimeReadback(r.Context(), d, a.WorkerID(), strings.TrimSpace(req.CommandID)))
}

func (s *Server) requireRuntimePermission(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, permission authz.PermissionKey) bool {
	if d.Authorizer == nil {
		writeError(w, http.StatusForbidden, "authorization_not_wired", "runtime deploy/status requires authorization service")
		return false
	}
	auth, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "auth_missing", "endpoint requires authenticated bearer")
		return false
	}
	decision, err := checkAdminAuthorization(r.Context(), d, authz.CheckRequest{
		SubjectRef: authz.WorkerSubject(a.WorkerID()),
		Transport:  authz.TransportMCP,
		Permission: permission,
		Resource: authz.ResourceScope{
			Kind:  "worker",
			ID:    a.WorkerID(),
			OrgID: a.OrganizationID(),
		},
		RequestID: string(auth.TokenID),
	})
	if err != nil || !decision.Allowed {
		writeAuthorizationError(w, decision, err)
		return false
	}
	return true
}

func validateRuntimeDeployEvidence(req deployRuntimeReq) (string, string) {
	if !fullGitSHARe.MatchString(strings.TrimSpace(req.CommitSHA)) {
		return "invalid_commit_sha", "commit_sha must be a full 40-character git SHA"
	}
	if !fullGitSHARe.MatchString(strings.TrimSpace(req.AncestorSHA)) {
		return "invalid_ancestor_sha", "ancestor_sha must be a full 40-character git SHA"
	}
	if strings.TrimSpace(req.PushedRef) == "" {
		return "missing_pushed_ref", "pushed_ref is required as push evidence"
	}
	if !req.ExactSHAVerified {
		return "exact_sha_not_verified", "exact_sha_verified must be true"
	}
	if !req.AncestorVerified {
		return "ancestor_not_verified", "ancestor_verified must be true"
	}
	return "", ""
}

func (s *Server) runtimeReadback(ctx context.Context, d HandlerDeps, workerID, commandID string) map[string]any {
	out := map[string]any{
		"worker_id": workerID,
		"health":    "unknown",
		"actual":    map[string]any{},
	}
	if d.WorkerRepo != nil {
		if wk, err := d.WorkerRepo.FindByID(ctx, workforce.WorkerID(workerID)); err == nil {
			info := wk.SystemInfo()
			out["health"] = string(wk.Status())
			out["actual"] = map[string]any{
				"worker_version": info.WorkerVersion,
				"build_commit":   info.BuildCommit,
				"build_branch":   info.BuildBranch,
				"build_built_at": info.BuildBuiltAt,
				"pid":            info.PID,
				"parent_pid":     info.ParentPID,
				"started_at":     info.StartedAt,
				"install_path":   info.InstallPath,
				"read_at":        time.Now().UTC().Format(time.RFC3339Nano),
			}
		}
	}
	if d.EnvControlSvc != nil && commandID != "" {
		if cmds, err := d.EnvControlSvc.CommandsAfter(ctx, environment.WorkerID(workerID), 0); err == nil {
			for _, cmd := range cmds {
				if cmd.ID() == commandID {
					out["command"] = runtimeControlEventMap(cmd)
					break
				}
			}
		}
	}
	return out
}

func runtimeControlEventMap(e *environment.WorkerControlEvent) map[string]any {
	out := controlEventMap(e)
	if strings.TrimSpace(e.Status()) == "" {
		out["status"] = environment.CommandStatusPending
	}
	return out
}
