// Package workerdaemon: AdminClient is the worker-daemon-side HTTP
// client that talks to the center process's admin endpoint over a unix
// domain socket. This is the v2.2-C transport that turns
// `cmd/worker-daemon` into a real consumer of the center AppService
// surface (conventions § 0.4 — AppService is the only entry).
//
// Scope: only the methods the worker daemon itself needs.
//   - Enroll (idempotent — also serves as heartbeat in v2.2 single-host)
//   - PullDispatches (drain envelope queue)
//   - PullKills (drain kill queue; daemon filters by owned executions)
//   - ReportProgress / ReportFailure / ReportArtifact (agent → center)
//
// Wider AppService surfaces (task create, conversation, identity, etc.)
// belong to the Phase B CLI client, not the worker daemon.
package workerdaemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
	"github.com/oopslink/agent-center/internal/mcphost"
	"github.com/oopslink/agent-center/internal/workforce"
)

// Compile-time assertion: *AdminClient is the real transport behind the
// per-agent MCP host's AdminCaller seam (v2.7 b3-i).
var _ mcphost.AdminCaller = (*AdminClient)(nil)

// AdminClient wraps an *http.Client whose Transport dials either the
// unix admin socket (v2.2 default) or a TCP+TLS admin endpoint with
// SSH-style fingerprint pinning (v2.3-7b, task #27).
type AdminClient struct {
	socketPath string // legacy: unix socket path (empty when TCP)
	baseURL    string // "http://unix" or "https://host:port"
	httpc      *http.Client
	// token is the bearer attached to every request via
	// `Authorization: Bearer <token>`. Wired by cmd/worker-daemon via
	// WithToken; v2.3-3a (task #28) requires it on every non-public
	// endpoint.
	token string
}

// NewAdminClient constructs a client targeting the given unix socket.
// timeout is applied per-request; pass 0 for the default of 30s.
//
// Preserved unchanged for backward compat. v2.3-7b callers wanting
// TCP+TLS should use NewAdminClientFromTarget.
func NewAdminClient(socketPath string, timeout time.Duration) *AdminClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
		// Reasonable defaults; admin endpoint runs over loopback.
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
	}
	return &AdminClient{
		socketPath: socketPath,
		baseURL:    "http://unix",
		httpc: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
	}
}

// NewAdminClientFromTarget constructs from a parsed transport target
// (unix or tcp) + optional fingerprint (mandatory for tcp). v2.3-7b.
func NewAdminClientFromTarget(target clienttransport.Target, fingerprint string, timeout time.Duration) (*AdminClient, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tr, err := clienttransport.NewHTTPTransport(target, fingerprint, timeout)
	if err != nil {
		return nil, err
	}
	c := &AdminClient{
		baseURL: target.BaseURL(),
		httpc: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
	}
	if target.Kind == clienttransport.KindUnix {
		c.socketPath = target.Address
	}
	return c, nil
}

// SocketPath returns the configured socket path (for logging/diagnostics).
// Empty when constructed from a TCP target.
func (c *AdminClient) SocketPath() string { return c.socketPath }

// WithToken sets the bearer token attached to every request. Returns
// the receiver to allow chaining at construction sites.
func (c *AdminClient) WithToken(t string) *AdminClient {
	if c == nil {
		return c
	}
	c.token = strings.TrimSpace(t)
	return c
}

// Enroll POSTs to /admin/workforce/worker/enroll. The center's
// WorkerEnrollService is idempotent on worker_id — re-calling Enroll
// is the v2.2 single-host heartbeat semantic (per task spec).
//
// Kept for source compat. The v2.4-D path prefers EnrollWithExchange,
// which captures the long-term admin token returned by the server.
func (c *AdminClient) Enroll(ctx context.Context, workerID string, capabilities []string) error {
	_, err := c.EnrollWithExchange(ctx, workerID, "", capabilities)
	return err
}

// EnrollResponse mirrors the fields the admin endpoint returns. The
// AdminToken + AdminTokenID fields are only populated on the v2.4-D
// path; older deployments leave them empty (caller continues using
// whatever bearer it was constructed with).
type EnrollResponse struct {
	WorkerID        string `json:"worker_id"`
	EventID         string `json:"event_id"`
	Version         int    `json:"version"`
	AdminToken      string `json:"admin_token,omitempty"`
	AdminTokenID    string `json:"admin_token_id,omitempty"`
	AdminTokenError string `json:"admin_token_error,omitempty"`
}

// EnrollWithExchange POSTs to /admin/workforce/worker/enroll and
// returns the parsed response, including the long-term admin token
// minted for this worker (v2.4-D B5 fix). The token is what the
// daemon should swap into its AdminClient bearer + persist locally;
// continuing to use the enroll token after this call will 401
// because the AuthMiddleware burned it during the same request.
//
// `name` is the operator-facing friendly label (v2.4-D-X1 @oopslink).
// Empty falls back server-side to worker_id.
func (c *AdminClient) EnrollWithExchange(ctx context.Context, workerID, name string, capabilities []string) (EnrollResponse, error) {
	if strings.TrimSpace(workerID) == "" {
		return EnrollResponse{}, errors.New("adminclient: worker_id required")
	}
	if capabilities == nil {
		capabilities = []string{}
	}
	body := map[string]any{
		"worker_id":    workerID,
		"name":         name,
		"capabilities": capabilities,
	}
	var out EnrollResponse
	if err := c.doJSON(ctx, http.MethodPost, "/admin/workforce/worker/enroll", body, &out); err != nil {
		return EnrollResponse{}, err
	}
	return out, nil
}

// ReportCapabilities POSTs the worker's freshly-probed capability list to
// /admin/workforce/worker/capabilities (v2.7 #147). Unlike enroll (which only
// runs on first boot), this is called on EVERY online so a newly-installed CLI
// is auto-discovered. The rich workforce.Capability shape is sent verbatim so
// probe version + feature flags survive the wire; the center merges onto the
// stored set, preserving operator Enabled toggles.
func (c *AdminClient) ReportCapabilities(ctx context.Context, workerID string, capabilities []workforce.Capability) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("adminclient: worker_id required")
	}
	if capabilities == nil {
		capabilities = []workforce.Capability{}
	}
	body := map[string]any{
		"worker_id":    workerID,
		"capabilities": capabilities,
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/workforce/worker/capabilities", body, nil)
}

// Heartbeat asserts liveness for an already-enrolled worker.
//
// v2.3-1 (task #24): now POSTs to the dedicated
// /admin/workforce/worker/heartbeat endpoint that calls
// WorkerEnrollService.Heartbeat (idempotent, no event emit). The
// `capabilities` parameter is accepted for source compat but ignored
// by this endpoint — capabilities only mutate on enroll. If the
// server returns 404 (worker not found, e.g. cold restart wiped state)
// the caller should fall back to Enroll on the next tick.
func (c *AdminClient) Heartbeat(ctx context.Context, workerID string, capabilities []string) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("adminclient: worker_id required")
	}
	_ = capabilities // intentional: server ignores; kept for v2.2 ABI compat
	body := map[string]any{
		"worker_id":                  workerID,
		"additional_working_seconds": 0,
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/workforce/worker/heartbeat", body, nil)
}

// ResolveSecret POSTs to /admin/secret/user-secret/resolve and returns
// the plaintext bytes. Caller must wipe the returned slice after use
// per ADR-0026 § 5 (plaintext never lingers).
//
// The admin endpoint requires `secret:resolve` scope on the bearer.
// 401 / 403 / 404 are surfaced verbatim via *AdminError so the caller
// can decide whether to retry / report failure.
func (c *AdminClient) ResolveSecret(ctx context.Context, name string) ([]byte, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("adminclient: secret name required")
	}
	body := map[string]any{"name": name}
	var out struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		PlaintextBase64 string `json:"plaintext_base64"`
	}
	if err := c.doJSON(ctx, http.MethodPost,
		"/admin/secret/user-secret/resolve", body, &out); err != nil {
		return nil, err
	}
	plain, err := base64.StdEncoding.DecodeString(out.PlaintextBase64)
	if err != nil {
		return nil, fmt.Errorf("adminclient: decode plaintext: %w", err)
	}
	return plain, nil
}

// BlobPut POSTs to /admin/blob/put with base64-encoded content. Returns
// nil on success; on non-2xx the typed *AdminError is returned so the
// caller can surface scope / validation errors verbatim.
//
// Requires `blob:put` scope on the bearer.
func (c *AdminClient) BlobPut(ctx context.Context, relPath string, content []byte) error {
	if strings.TrimSpace(relPath) == "" {
		return errors.New("adminclient: rel_path required")
	}
	body := map[string]any{
		"rel_path":       relPath,
		"content_base64": base64.StdEncoding.EncodeToString(content),
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/blob/put", body, nil)
}

// CallAgentTool POSTs `body` to /admin/agent-tools/<tool> and writes the raw
// admin JSON response into *out. It is the transport seam the per-agent
// `mcp-host` server (v2.7 b3-i, internal/mcphost) calls: the MCP tool
// handlers build a body carrying the process-fixed agent_id and forward it
// here. Rides the same authed transport as the rest of this client (bearer
// owner worker:<id>) — the center re-checks requireAgentOnWorker.
//
// On non-2xx it returns a *mcphost.AdminToolError (status + body verbatim)
// so the MCP handler can surface the failure to the model as an IsError
// result instead of a silent protocol error. This makes *AdminClient
// satisfy mcphost.AdminCaller.
func (c *AdminClient) CallAgentTool(ctx context.Context, tool string, body any, out *json.RawMessage) error {
	raw, status, err := c.doRaw(ctx, http.MethodPost, "/admin/agent-tools/"+tool, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &mcphost.AdminToolError{Status: status, Body: string(raw)}
	}
	if out != nil {
		*out = append((*out)[:0], raw...)
	}
	return nil
}

// doRaw is the sibling of doJSON that surfaces the raw response body +
// status instead of decoding into a typed struct / mapping non-2xx to
// *AdminError. CallAgentTool needs both the raw JSON (to hand to the model)
// and the status (to build the typed mcphost error). Same transport +
// bearer wiring as doJSON.
func (c *AdminClient) doRaw(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("adminclient: marshal %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(buf)
	}
	base := c.baseURL
	if base == "" {
		base = "http://unix"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("adminclient: build %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("adminclient: do %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// =============================================================================
// Environment BC — worker-initiated control channel (v2.7 D1, ADR-0050,
// task #102). ADDITIVE: these methods ride the SAME authed transport as the
// legacy worker surface above — they reuse doJSON, which attaches
// `Authorization: Bearer <token>` on every request. No new auth / enrollment
// is introduced. The center-side endpoints live on the admin API under
// /admin/environment/worker/... (bearer-authed, owner worker:<id>).
//
// These exercise the LOG layer: ordered + replayable command stream, cumulative
// ack, per-command idempotency. Actually EXECUTING commands is D2's
// AgentController; D1 wires a no-op handler (see control_loop.go).
// =============================================================================

// ControlCommand is one entry in the worker's ordered control-command log,
// as returned by /admin/environment/worker/commands. Offset is the cumulative
// cursor the worker acks against; IdempotencyKey lets D2 skip re-executing a
// command it already applied after a reconnect.
type ControlCommand struct {
	ID             string `json:"id"`
	Offset         int64  `json:"offset"`
	IdempotencyKey string `json:"idempotency_key"`
	CommandType    string `json:"command_type"`
	Payload        string `json:"payload"`
	CreatedAt      string `json:"created_at"`
}

// ConnectControl POSTs to /admin/environment/worker/connect and returns the
// worker's last_acked_offset — the cursor the control loop resumes polling
// from. Marks the worker online server-side. Reuses the authed transport.
func (c *AdminClient) ConnectControl(ctx context.Context, workerID string) (int64, error) {
	if strings.TrimSpace(workerID) == "" {
		return 0, errors.New("adminclient: worker_id required")
	}
	body := map[string]any{"worker_id": workerID}
	var out struct {
		WorkerID        string `json:"worker_id"`
		LastAckedOffset int64  `json:"last_acked_offset"`
		Status          string `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/admin/environment/worker/connect", body, &out); err != nil {
		return 0, err
	}
	return out.LastAckedOffset, nil
}

// PullCommands GETs /admin/environment/worker/commands?worker_id=X&after=N and
// returns the commands with offset > after, ascending. Returns an empty slice
// (not nil) when nothing is pending. Reuses the authed transport.
func (c *AdminClient) PullCommands(ctx context.Context, workerID string, after int64) ([]ControlCommand, error) {
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("adminclient: worker_id required")
	}
	path := fmt.Sprintf("/admin/environment/worker/commands?worker_id=%s&after=%d", workerID, after)
	var out struct {
		Commands []ControlCommand `json:"commands"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if out.Commands == nil {
		out.Commands = []ControlCommand{}
	}
	return out.Commands, nil
}

// AckControl POSTs to /admin/environment/worker/ack to advance the worker's
// cumulative last_acked_offset. Reuses the authed transport.
func (c *AdminClient) AckControl(ctx context.Context, workerID string, offset int64) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("adminclient: worker_id required")
	}
	body := map[string]any{"worker_id": workerID, "offset": offset}
	return c.doJSON(ctx, http.MethodPost, "/admin/environment/worker/ack", body, nil)
}

// =============================================================================
// Environment BC — worker boot-resume (v2.7 D2-f s4 client side, ADR-0049/0050).
// On (re)start with the control-stream path active, the daemon asks the center
// which of THIS worker's agents should be running + their in-flight WorkItems,
// then reconciles those claude sessions (the AgentController.ReconcileOnBoot
// path, s4b). Rides the SAME authed transport (worker bearer); the center derives
// the worker from the token owner and requires body.worker_id == it (only-ask
// -self → 403).
// =============================================================================

// ResumeState is the parsed /admin/environment/worker/resume-state response: the
// resumable agents on this worker (running OR with ≥1 in-flight WorkItem).
type ResumeState struct {
	Agents []ResumeAgent `json:"agents"`
}

// ResumeAgent is one resumable agent: its desired lifecycle + version (+
// reset_scope reserved for f-3) and its in-flight WorkItems.
type ResumeAgent struct {
	AgentID          string       `json:"agent_id"`
	DesiredLifecycle string       `json:"desired_lifecycle"`
	Model            string       `json:"model"`
	Version          int          `json:"version"`
	ResetScope       string       `json:"reset_scope"`
	Tasks            []ResumeTask `json:"tasks"`
}

// ResumeTask is one in-flight WorkItem (status ∈ {active, waiting_input}).
type ResumeTask struct {
	TaskID  string `json:"task_id"`
	TaskRef string `json:"task_ref"`
	Status  string `json:"status"`
}

// resumeStateQuerier is the seam the AgentController depends on for the boot-
// resume query (s4b). *AdminClient satisfies it; the controller's ReconcileOnBoot
// test injects a fake. Defined here so the controller depends on the interface,
// not the concrete transport.
type resumeStateQuerier interface {
	ResumeState(ctx context.Context, workerID string) (ResumeState, error)
}

// var _ resumeStateQuerier asserts *AdminClient satisfies the controller seam.
var _ resumeStateQuerier = (*AdminClient)(nil)

// ResumeState POSTs to /admin/environment/worker/resume-state with this worker's
// id and returns the resumable agents. The body worker_id MUST equal the
// authenticated worker (the server enforces only-ask-self → 403). Reuses the
// authed transport.
func (c *AdminClient) ResumeState(ctx context.Context, workerID string) (ResumeState, error) {
	if strings.TrimSpace(workerID) == "" {
		return ResumeState{}, errors.New("adminclient: worker_id required")
	}
	body := map[string]any{"worker_id": workerID}
	var out ResumeState
	if err := c.doJSON(ctx, http.MethodPost, "/admin/environment/worker/resume-state", body, &out); err != nil {
		return ResumeState{}, err
	}
	return out, nil
}

// HeartbeatControl POSTs to /admin/environment/worker/heartbeat to assert
// liveness on the control channel. Reuses the authed transport. Optional —
// the control loop can call it on a separate cadence if desired.
func (c *AdminClient) HeartbeatControl(ctx context.Context, workerID string) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("adminclient: worker_id required")
	}
	body := map[string]any{"worker_id": workerID}
	return c.doJSON(ctx, http.MethodPost, "/admin/environment/worker/heartbeat", body, nil)
}

// =============================================================================
// Environment BC — controller→center RESULT feedback (v2.7 D2-c-i client side,
// ADR-0049/0050). The daemon AgentController (D2-c-ii) has NO DB; it reports
// agent activity / lifecycle / work-item transitions back to the center via
// these ADDITIVE endpoints under /admin/environment/agent/*. They ride the SAME
// authed transport (doJSON attaches the worker bearer). The center re-checks
// requireAgentOnWorker (worker derived from the token owner, not the body).
//
// Field names below MUST match the request structs in
// internal/admin/api/environment_agent.go EXACTLY (recording-fake unit tests in
// D2-c-ii won't catch a drift — D2-g will; verified by reading the handlers).
// =============================================================================

// feedbackReporter is the seam the AgentController depends on for posting RESULT
// feedback to the center. *AdminClient satisfies it; D2-c-ii tests inject a
// recording fake. Defined here so the controller depends on the interface, not
// the concrete transport.
type feedbackReporter interface {
	// ReportAgentActivity posts a single AgentActivityEvent (the stdout→activity
	// sink — observation only; it does NOT post to any Conversation).
	ReportAgentActivity(ctx context.Context, agentID, eventType, payloadJSON, taskRef, interactionRef string, at time.Time) error
	// ReportAgentLifecycle posts a lifecycle RESULT (state "running" recovery |
	// "stopped" | "error" | "failed").
	ReportAgentLifecycle(ctx context.Context, agentID, state, errMsg string, at time.Time) error
	// v2.14.0 F7 (issue I14): ReportWorkItemState removed — AgentWorkItem retired
	// (the work-item-state feedback endpoint is gone; the daemon surfaces L2 errors
	// via the lifecycle/converse-error reporters instead).
	// ReportMarkSeen advances the agent participant's read-state cursor in a task
	// conversation to messageID (monotonic, server-side). v2.7 D2-e-ii (OQ5): after
	// a wake inject the controller marks the delivered batch seen so the next batch
	// flush won't re-deliver it.
	ReportMarkSeen(ctx context.Context, agentID, conversationID, messageID string, at time.Time) error
	// ReportConverseError posts a VISIBLE system message into a conversation when
	// an agent.converse turn ended is_error (UX Rule 9 — no silent black hole for
	// a DM/channel reply that failed, e.g. invalid model → claude 404). summary is
	// a short failure description (subtype + bounded result text).
	ReportConverseError(ctx context.Context, agentID, conversationID, summary string, at time.Time) error
	// FetchReplyNudges asks the center, at turn-end, which directed replies the
	// agent still owes (T341 方案 A). The server derives them from the message log +
	// read-state, gates agent-authored ones through the shared wake-guardrail, and
	// returns bounded re-inject prompts the controller injects so the agent itself
	// discharges the obligation. An empty slice means "nothing owed" (the common
	// case); errors are best-effort (logged, never fatal — the guardrail is a
	// safety net, not a critical path).
	FetchReplyNudges(ctx context.Context, agentID string) ([]string, error)
}

// var _ feedbackReporter asserts *AdminClient satisfies the controller seam.
var _ feedbackReporter = (*AdminClient)(nil)

// ReportAgentActivity POSTs to /admin/environment/agent/activity. Empty
// taskRef/interactionRef/at are omitted server-side (omitempty). A zero
// `at` lets the AppService stamp its own clock.
func (c *AdminClient) ReportAgentActivity(ctx context.Context, agentID, eventType, payloadJSON, taskRef, interactionRef string, at time.Time) error {
	if strings.TrimSpace(agentID) == "" {
		return errors.New("adminclient: agent_id required")
	}
	body := map[string]any{
		"agent_id":   agentID,
		"event_type": eventType,
		"payload":    payloadJSON,
	}
	if taskRef != "" {
		body["task_ref"] = taskRef
	}
	if interactionRef != "" {
		body["interaction_ref"] = interactionRef
	}
	if !at.IsZero() {
		body["occurred_at"] = at.UTC().Format(time.RFC3339Nano)
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/environment/agent/activity", body, nil)
}

// ReportAgentLifecycle POSTs to /admin/environment/agent/lifecycle-feedback.
// state is "stopped" or "error"; errMsg is only meaningful for "error".
func (c *AdminClient) ReportAgentLifecycle(ctx context.Context, agentID, state, errMsg string, at time.Time) error {
	if strings.TrimSpace(agentID) == "" {
		return errors.New("adminclient: agent_id required")
	}
	body := map[string]any{
		"agent_id": agentID,
		"state":    state,
	}
	if errMsg != "" {
		body["error"] = errMsg
	}
	if !at.IsZero() {
		body["at"] = at.UTC().Format(time.RFC3339Nano)
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/environment/agent/lifecycle-feedback", body, nil)
}

// v2.14.0 F7 (issue I14): ReportWorkItemState (POST
// /admin/environment/agent/work-item-state) removed — AgentWorkItem retired.

// ReportMarkSeen POSTs to /admin/environment/agent/mark-seen. Monotonically
// advances the agent participant's read-state cursor in conversationID to
// messageID (v2.7 D2-e-ii). The server (requireAgentOnWorker-gated) never
// regresses the cursor — an older/equal id is a no-op.
func (c *AdminClient) ReportMarkSeen(ctx context.Context, agentID, conversationID, messageID string, at time.Time) error {
	if strings.TrimSpace(agentID) == "" {
		return errors.New("adminclient: agent_id required")
	}
	if strings.TrimSpace(conversationID) == "" {
		return errors.New("adminclient: conversation_id required")
	}
	if strings.TrimSpace(messageID) == "" {
		return errors.New("adminclient: message_id required")
	}
	body := map[string]any{
		"agent_id":        agentID,
		"conversation_id": conversationID,
		"message_id":      messageID,
	}
	if !at.IsZero() {
		body["at"] = at.UTC().Format(time.RFC3339Nano)
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/environment/agent/mark-seen", body, nil)
}

// ReportConverseError POSTs to /admin/environment/agent/converse-error. The
// server (requireAgentOnWorker-gated) posts a system message into the
// conversation announcing the agent's turn failed (UX Rule 9). agent_id is the
// execution-entity id (the worker's view); the server resolves it + the agent's
// display name when formatting the system message.
func (c *AdminClient) ReportConverseError(ctx context.Context, agentID, conversationID, summary string, at time.Time) error {
	if strings.TrimSpace(agentID) == "" {
		return errors.New("adminclient: agent_id required")
	}
	if strings.TrimSpace(conversationID) == "" {
		return errors.New("adminclient: conversation_id required")
	}
	body := map[string]any{
		"agent_id":        agentID,
		"conversation_id": conversationID,
		"error":           summary,
	}
	if !at.IsZero() {
		body["at"] = at.UTC().Format(time.RFC3339Nano)
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/environment/agent/converse-error", body, nil)
}

// FetchReplyNudges POSTs to /admin/environment/agent/reply-nudges (T341). The
// controller calls it at turn-end + TrueIdle; the server derives the agent's
// outstanding directed replies, gates agent-authored ones through the shared
// wake-guardrail, and returns the bounded re-inject prompts the controller injects
// (方案 A). agent_id is the execution-entity id; the server resolves the agent's
// org/display-name/identity-member. Returns the prompts (possibly empty).
func (c *AdminClient) FetchReplyNudges(ctx context.Context, agentID string) ([]string, error) {
	if strings.TrimSpace(agentID) == "" {
		return nil, errors.New("adminclient: agent_id required")
	}
	var resp struct {
		Prompts []string `json:"prompts"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/admin/environment/agent/reply-nudges",
		map[string]any{"agent_id": agentID}, &resp); err != nil {
		return nil, err
	}
	return resp.Prompts, nil
}

// doJSON is the shared request helper. Returns a typed error on non-2xx
// so the caller can decide whether to retry / log / abort.
func (c *AdminClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("adminclient: marshal %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(buf)
	}
	base := c.baseURL
	if base == "" {
		base = "http://unix"
	}
	url := base + path
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("adminclient: build %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		// v2.3-3a (task #28): admin endpoint requires bearer auth on
		// every non-public path. cmd/worker-daemon plumbs the token
		// from --admin-token / AGENT_CENTER_ADMIN_TOKEN.
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("adminclient: do %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &AdminError{
			Method: method, Path: path,
			Status: resp.StatusCode,
			Body:   string(respBody),
		}
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("adminclient: decode %s %s: %w (body=%s)", method, path, err, string(respBody))
		}
	}
	return nil
}

// AdminError is returned on non-2xx admin endpoint responses.
type AdminError struct {
	Method string
	Path   string
	Status int
	Body   string
}

// Error implements error.
func (e *AdminError) Error() string {
	return fmt.Sprintf("admin endpoint %s %s: status=%d body=%s",
		e.Method, e.Path, e.Status, e.Body)
}
