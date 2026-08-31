package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/runtimedeploy"
)

type runtimeDeployRestartReq struct {
	AgentID        string `json:"agent_id"`
	RepoURL        string `json:"repo_url"`
	TargetRef      string `json:"target_ref"`
	TargetSHA      string `json:"target_sha"`
	BaseRef        string `json:"base_ref,omitempty"`
	Mode           string `json:"mode,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
	TimeoutMS      int    `json:"timeout_ms,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type runtimeDeployStatusReq struct {
	AgentID        string `json:"agent_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type RuntimeDeployVerifier interface {
	VerifyRemote(ctx context.Context, req runtimedeploy.Request) (runtimedeploy.VerifiedRef, error)
}

type defaultRuntimeDeployVerifier struct{}

func (defaultRuntimeDeployVerifier) VerifyRemote(ctx context.Context, req runtimedeploy.Request) (runtimedeploy.VerifiedRef, error) {
	return runtimedeploy.VerifyRemote(ctx, req)
}

func (s *Server) runtimeDeployRestartHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_svc_not_wired", "")
		return
	}
	var req runtimeDeployRestartReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "center"
	}
	if mode != "center" && mode != "worker" {
		writeError(w, http.StatusBadRequest, "invalid_mode", "mode must be center or worker")
		return
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = legacyRuntimeDeployIdempotencyKey(a.WorkerID(), mode, req.TargetRef, req.TargetSHA)
	}
	verifyCtx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	verifier := d.RuntimeDeployVerifier
	if verifier == nil {
		verifier = defaultRuntimeDeployVerifier{}
	}
	verified, err := verifier.VerifyRemote(verifyCtx, runtimedeploy.Request{
		RepoURL: req.RepoURL, TargetRef: req.TargetRef, TargetSHA: req.TargetSHA, BaseRef: req.BaseRef,
	})
	cancel()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "remote_ref_verification_failed", err.Error())
		return
	}
	payload := runtimedeploy.Request{
		AgentID:           string(a.ID()),
		RepoURL:           strings.TrimSpace(req.RepoURL),
		TargetRef:         strings.TrimSpace(req.TargetRef),
		TargetSHA:         strings.ToLower(strings.TrimSpace(req.TargetSHA)),
		BaseRef:           strings.TrimSpace(req.BaseRef),
		Mode:              mode,
		Prefix:            strings.TrimSpace(req.Prefix),
		TimeoutMS:         req.TimeoutMS,
		VerifiedTargetSHA: verified.TargetSHA,
		VerifiedBaseSHA:   verified.BaseSHA,
		VerifiedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	key := runtimeDeployIdempotencyKey(a.WorkerID(), string(a.ID()), idempotencyKey)
	evt, err := d.EnvControlSvc.EnqueueCommand(r.Context(), environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(a.WorkerID()),
		CommandType:    runtimedeploy.CommandType,
		Payload:        string(body),
		IdempotencyKey: key,
		AgentID:        string(a.ID()),
		Status:         environment.CommandStatusPending,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if !runtimeDeploySamePayload(evt.Payload(), payload) {
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency_key was already used for a different runtime deploy request")
		return
	}
	writeJSON(w, http.StatusAccepted, runtimeDeployAttemptStatus(evt, idempotencyKey))
}

func (s *Server) runtimeDeployStatusHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_svc_not_wired", "")
		return
	}
	var req runtimeDeployStatusReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotency_key is required")
		return
	}
	evt, err := d.EnvControlSvc.CommandByIdempotencyKey(r.Context(), environment.WorkerID(a.WorkerID()), runtimeDeployIdempotencyKey(a.WorkerID(), string(a.ID()), idempotencyKey))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if evt == nil {
		writeError(w, http.StatusNotFound, "runtime_deploy_attempt_not_found", "unknown runtime deploy attempt")
		return
	}
	writeJSON(w, http.StatusOK, runtimeDeployAttemptStatus(evt, idempotencyKey))
}

func runtimeDeployIdempotencyKey(workerID, agentID, key string) string {
	return fmt.Sprintf("runtime.deploy_restart:%s:%s:%s", strings.TrimSpace(workerID), strings.TrimSpace(agentID), strings.TrimSpace(key))
}

func legacyRuntimeDeployIdempotencyKey(workerID, mode, targetRef, targetSHA string) string {
	return fmt.Sprintf("%s:%s:%s:%s", strings.TrimSpace(workerID), strings.TrimSpace(mode), strings.TrimSpace(targetRef), strings.ToLower(strings.TrimSpace(targetSHA)))
}

func runtimeDeploySamePayload(existingPayload string, payload runtimedeploy.Request) bool {
	var existing runtimedeploy.Request
	if err := json.Unmarshal([]byte(existingPayload), &existing); err != nil {
		return false
	}
	existing.VerifiedAt = ""
	payload.VerifiedAt = ""
	return existing == payload
}

func runtimeDeployAttemptStatus(evt *environment.WorkerControlEvent, idempotencyKey string) runtimedeploy.AttemptStatus {
	out := runtimedeploy.AttemptStatus{
		Accepted:       true,
		AttemptID:      evt.ID(),
		CommandID:      evt.ID(),
		Offset:         evt.Offset(),
		CommandStatus:  evt.Status(),
		StatusReason:   evt.StatusReason(),
		StatusDetail:   evt.StatusDetail(),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
		Terminal:       environment.CommandStatusTerminal(evt.Status()),
	}
	var payload runtimedeploy.Request
	if err := json.Unmarshal([]byte(evt.Payload()), &payload); err == nil {
		out.VerifiedTargetSHA = payload.VerifiedTargetSHA
		out.VerifiedBaseSHA = payload.VerifiedBaseSHA
	}
	var rawDetail map[string]json.RawMessage
	var detail runtimedeploy.Result
	if err := json.Unmarshal([]byte(evt.StatusDetail()), &rawDetail); err == nil && len(rawDetail) > 0 {
		_ = json.Unmarshal([]byte(evt.StatusDetail()), &detail)
		out.RunningSHA = detail.RunningSHA
		if strings.TrimSpace(out.RunningSHA) == "" {
			out.RunningSHA = detail.RunningCommit
		}
		out.RunningVersion = detail.RunningVersion
		if _, ok := rawDetail["restart_terminal_success"]; ok {
			out.RestartTerminalSuccess = &detail.RestartTerminalSuccess
		}
		out.PostRestartHealthStatus = detail.PostRestartHealthStatus
	}
	return out
}
