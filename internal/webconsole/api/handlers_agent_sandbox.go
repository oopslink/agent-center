package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/workforce"
)

const cmdTypeAgentSandboxAction = "agent.sandbox_action"

var allowedSandboxActions = map[string]bool{
	"ensure":       true,
	"provision":    true,
	"start":        true,
	"resume":       true,
	"suspend":      true,
	"reset":        true,
	"delete":       true,
	"health":       true,
	"open_console": true,
	"open_browser": true,
}

var sandboxVNCUpgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		want := "http://" + r.Host
		if r.TLS != nil {
			want = "https://" + r.Host
		}
		return origin == want
	},
}

func (s *Server) agentSandboxActionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	action := strings.TrimSpace(strings.ToLower(r.PathValue("action")))
	if action == "" || !allowedSandboxActions[action] {
		writeError(w, http.StatusBadRequest, "invalid_sandbox_action", "unsupported sandbox action")
		return
	}
	if !a.Profile().SandboxEnabled {
		writeError(w, http.StatusConflict, "sandbox_disabled", "enable sandbox before running sandbox actions")
		return
	}
	workerID := strings.TrimSpace(a.WorkerID())
	if workerID == "" {
		writeError(w, http.StatusConflict, "worker_required", "agent must be bound to a worker")
		return
	}
	if d.WorkerRepo != nil {
		wk, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(workerID))
		if err != nil {
			mapDomainError(w, err)
			return
		}
		if wk != nil && wk.Status() != workforce.WorkerOnline {
			writeError(w, http.StatusConflict, "worker_offline", "agent worker is offline")
			return
		}
	}
	if d.EnvControl == nil {
		writeError(w, http.StatusNotImplemented, "env_control_not_wired", "")
		return
	}
	payload, err := json.Marshal(map[string]any{
		"agent_id": string(a.ID()),
		"action":   action,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	evt, err := d.EnvControl.EnqueueCommand(r.Context(), environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    cmdTypeAgentSandboxAction,
		Payload:        string(payload),
		IdempotencyKey: fmt.Sprintf("sandbox_action:%s:%s:%d", a.ID(), action, time.Now().UnixNano()),
		AgentID:        string(a.ID()),
		Status:         environment.CommandStatusPending,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":             true,
		"status":         "accepted",
		"agent_id":       agentFacingID(a),
		"worker_id":      workerID,
		"action":         action,
		"command_id":     evt.ID(),
		"offset":         evt.Offset(),
		"command_type":   evt.CommandType(),
		"command_status": evt.Status(),
	})
}

func (s *Server) agentSandboxCommandHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	if d.EnvControl == nil {
		writeError(w, http.StatusNotImplemented, "env_control_not_wired", "")
		return
	}
	commandID := strings.TrimSpace(r.PathValue("command_id"))
	evt, err := d.EnvControl.CommandByID(r.Context(), commandID)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if evt == nil || evt.AgentID() != string(a.ID()) || evt.CommandType() != cmdTypeAgentSandboxAction {
		writeError(w, http.StatusNotFound, "not_found", "sandbox command not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"agent_id":          agentFacingID(a),
		"worker_id":         string(evt.WorkerID()),
		"command_id":        evt.ID(),
		"offset":            evt.Offset(),
		"command_type":      evt.CommandType(),
		"command_status":    evt.Status(),
		"status_reason":     evt.StatusReason(),
		"status_detail":     evt.StatusDetail(),
		"status_updated_at": formatSandboxTime(evt.StatusUpdatedAt()),
		"created_at":        evt.CreatedAt().UTC().Format(time.RFC3339Nano),
	})
}

func (s *Server) agentSandboxDesktopSessionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	if !a.Profile().SandboxEnabled {
		writeError(w, http.StatusConflict, "sandbox_disabled", "enable sandbox before opening desktop")
		return
	}
	endpoint, configured := sandboxVNCEndpoint(a.ID().String(), agentFacingID(a))
	if !configured {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":             false,
			"status":         "not_configured",
			"agent_id":       agentFacingID(a),
			"websocket_url":  "",
			"endpoint_state": "missing",
			"message":        "VNC endpoint is not configured for this sandbox",
		})
		return
	}
	wsPath := fmt.Sprintf("/api/orgs/%s/agents/%s/sandbox/desktop/ws", r.PathValue("slug"), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"status":         "ready",
		"agent_id":       agentFacingID(a),
		"websocket_url":  wsPath,
		"endpoint_state": "configured",
		"endpoint":       maskVNCEndpoint(endpoint),
	})
}

func (s *Server) agentSandboxDesktopWSHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	if !a.Profile().SandboxEnabled {
		writeError(w, http.StatusConflict, "sandbox_disabled", "enable sandbox before opening desktop")
		return
	}
	endpoint, configured := sandboxVNCEndpoint(a.ID().String(), agentFacingID(a))
	if !configured {
		writeError(w, http.StatusConflict, "vnc_not_configured", "VNC endpoint is not configured for this sandbox")
		return
	}
	tcp, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(r.Context(), "tcp", endpoint)
	if err != nil {
		writeError(w, http.StatusBadGateway, "vnc_unreachable", err.Error())
		return
	}
	defer tcp.Close()
	ws, err := sandboxVNCUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			mt, payload, err := ws.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
				continue
			}
			if _, err := tcp.Write(payload); err != nil {
				cancel()
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, err := tcp.Read(buf)
			if n > 0 {
				if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					cancel()
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					cancel()
				}
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
	case <-done:
	}
}

func sandboxVNCEndpoint(agentIDs ...string) (string, bool) {
	for _, id := range agentIDs {
		if id == "" {
			continue
		}
		key := "AC_SANDBOX_VNC_ENDPOINT_" + sandboxEnvSuffix(id)
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v, true
		}
	}
	if v := strings.TrimSpace(os.Getenv("AC_SANDBOX_VNC_ENDPOINT")); v != "" {
		return v, true
	}
	return "", false
}

func sandboxEnvSuffix(id string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(id)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func maskVNCEndpoint(endpoint string) string {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "configured"
	}
	if host == "" || host == "127.0.0.1" || host == "localhost" {
		return "localhost:" + port
	}
	return "configured:" + port
}

func formatSandboxTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
