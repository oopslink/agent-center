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
	AgentID   string `json:"agent_id"`
	RepoURL   string `json:"repo_url"`
	TargetRef string `json:"target_ref"`
	ExactSHA  string `json:"exact_sha"`
	BaseRef   string `json:"base_ref,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
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
	verifyCtx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	verified, err := runtimedeploy.VerifyRemote(verifyCtx, runtimedeploy.Request{
		RepoURL: req.RepoURL, TargetRef: req.TargetRef, ExactSHA: req.ExactSHA, BaseRef: req.BaseRef,
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
		ExactSHA:          strings.ToLower(strings.TrimSpace(req.ExactSHA)),
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
	key := fmt.Sprintf("runtime.deploy_restart:%s:%s:%s:%s", a.WorkerID(), mode, payload.TargetRef, verified.TargetSHA)
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
	finalEvt := evt
	wait := runtimedeploy.Timeout(payload, 0)
	if wait > 0 {
		deadline := time.Now().Add(wait)
		for time.Now().Before(deadline) {
			got, gerr := d.EnvControlSvc.CommandByID(r.Context(), evt.ID())
			if gerr != nil {
				mapDomainError(w, gerr)
				return
			}
			if got != nil {
				finalEvt = got
				if environment.CommandStatusTerminal(got.Status()) {
					break
				}
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	out := map[string]any{
		"accepted":            true,
		"command_id":          evt.ID(),
		"offset":              evt.Offset(),
		"command_status":      finalEvt.Status(),
		"verified_target_sha": verified.TargetSHA,
		"verified_base_sha":   verified.BaseSHA,
	}
	if finalEvt.StatusReason() != "" {
		out["status_reason"] = finalEvt.StatusReason()
	}
	if finalEvt.StatusDetail() != "" {
		out["status_detail"] = finalEvt.StatusDetail()
	}
	writeJSON(w, http.StatusOK, out)
}
