package api

import (
	"bytes"
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
	agentbc "github.com/oopslink/agent-center/internal/agent"
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
		return sandboxVNCOriginAllowed(r)
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
	s.appendSandboxActionActivity(r.Context(), d, string(a.ID()), action, "accepted", evt.ID(), "", "")
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

func (s *Server) appendSandboxActionActivity(ctx context.Context, d HandlerDeps, agentID, action, status, commandID, reason, detail string) {
	if d.AgentSvc == nil {
		return
	}
	payload := map[string]any{
		"event":      "sandbox." + strings.TrimSpace(action),
		"action":     strings.TrimSpace(action),
		"scope":      strings.TrimSpace(status),
		"status":     strings.TrimSpace(status),
		"command_id": strings.TrimSpace(commandID),
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		payload["reason"] = reason
	}
	if detail = strings.TrimSpace(detail); detail != "" {
		payload["detail"] = detail
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = d.AgentSvc.AppendActivity(ctx, agentbc.NewActivityEventInput{
		AgentID:        agentbc.AgentID(agentID),
		InteractionRef: "sandbox:" + strings.TrimSpace(commandID),
		EventType:      agentbc.EventTypeLifecycle,
		Payload:        string(raw),
		OccurredAt:     time.Now().UTC(),
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
	if conn, err := (&net.Dialer{Timeout: 300 * time.Millisecond}).DialContext(r.Context(), "tcp", endpoint); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":             false,
			"status":         "unreachable",
			"agent_id":       agentFacingID(a),
			"websocket_url":  "",
			"endpoint_state": "unreachable",
			"endpoint":       maskVNCEndpoint(endpoint),
			"message":        "Desktop viewer is configured but not reachable. Start the sandbox and try again.",
		})
		return
	} else {
		_ = conn.Close()
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
	if err := negotiateVNCForWeb(r.Context(), ws, tcp); err != nil {
		return
	}
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

func negotiateVNCForWeb(ctx context.Context, ws *websocket.Conn, tcp net.Conn) error {
	deadline := time.Now().Add(10 * time.Second)
	_ = tcp.SetDeadline(deadline)
	_ = ws.SetReadDeadline(deadline)
	defer func() {
		_ = tcp.SetDeadline(time.Time{})
		_ = ws.SetReadDeadline(time.Time{})
	}()
	serverVersion := make([]byte, 12)
	if _, err := io.ReadFull(tcp, serverVersion); err != nil {
		return err
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, serverVersion); err != nil {
		return err
	}
	clientVersion, err := readVNCClientBytes(ctx, ws, 12)
	if err != nil {
		return err
	}
	if _, err := tcp.Write(clientVersion); err != nil {
		return err
	}
	if !bytes.HasPrefix(clientVersion, []byte("RFB 003.007")) && !bytes.HasPrefix(clientVersion, []byte("RFB 003.008")) {
		return nil
	}
	var count [1]byte
	if _, err := io.ReadFull(tcp, count[:]); err != nil {
		return err
	}
	if count[0] == 0 {
		if err := ws.WriteMessage(websocket.BinaryMessage, count[:]); err != nil {
			return err
		}
		return nil
	}
	types := make([]byte, int(count[0]))
	if _, err := io.ReadFull(tcp, types); err != nil {
		return err
	}
	advertised := vncSecurityTypesForWeb(types)
	if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{byte(len(advertised))}, advertised...)); err != nil {
		return err
	}
	if len(advertised) == 1 && advertised[0] == 2 && bytes.Contains(types, []byte{2}) {
		selection, err := readVNCClientBytes(ctx, ws, 1)
		if err != nil {
			return err
		}
		if _, err := tcp.Write(selection); err != nil {
			return err
		}
	}
	return nil
}

func readVNCClientBytes(ctx context.Context, ws *websocket.Conn, want int) ([]byte, error) {
	var buf []byte
	for len(buf) < want {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		mt, payload, err := ws.ReadMessage()
		if err != nil {
			return nil, err
		}
		if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
			continue
		}
		buf = append(buf, payload...)
	}
	return buf[:want], nil
}

func vncSecurityTypesForWeb(types []byte) []byte {
	if bytes.Contains(types, []byte{2}) {
		return []byte{2}
	}
	return append([]byte(nil), types...)
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

func sandboxVNCOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	for _, allowed := range sandboxVNCAllowedOrigins(r) {
		if origin == allowed {
			return true
		}
	}
	return false
}

func sandboxVNCAllowedOrigins(r *http.Request) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(scheme, host string) {
		scheme = strings.TrimSpace(strings.ToLower(scheme))
		host = strings.TrimSpace(host)
		if scheme == "" || host == "" {
			return
		}
		origin := scheme + "://" + host
		if !seen[origin] {
			seen[origin] = true
			out = append(out, origin)
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	add(scheme, r.Host)
	for _, proto := range splitForwardedHeader(r.Header.Get("X-Forwarded-Proto")) {
		add(proto, r.Host)
	}
	for _, host := range splitForwardedHeader(r.Header.Get("X-Forwarded-Host")) {
		add(scheme, host)
		for _, proto := range splitForwardedHeader(r.Header.Get("X-Forwarded-Proto")) {
			add(proto, host)
		}
	}
	return out
}

func splitForwardedHeader(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func formatSandboxTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
