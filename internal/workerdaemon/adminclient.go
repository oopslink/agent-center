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

	"github.com/oopslink/agent-center/internal/admin/dispatchq"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

// AdminClient wraps an *http.Client whose Transport dials the configured
// unix domain socket regardless of URL host. The Host in URLs is a
// placeholder ("unix") required by net/http URL parsing.
type AdminClient struct {
	socketPath string
	httpc      *http.Client
	// token is the bearer attached to every request via
	// `Authorization: Bearer <token>`. Wired by cmd/worker-daemon via
	// WithToken; v2.3-3a (task #28) requires it on every non-public
	// endpoint.
	token string
}

// NewAdminClient constructs a client targeting the given unix socket.
// timeout is applied per-request; pass 0 for the default of 30s.
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
		httpc: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
	}
}

// SocketPath returns the configured socket path (for logging/diagnostics).
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
func (c *AdminClient) Enroll(ctx context.Context, workerID string, capabilities []string) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("adminclient: worker_id required")
	}
	if capabilities == nil {
		capabilities = []string{}
	}
	body := map[string]any{
		"worker_id":    workerID,
		"capabilities": capabilities,
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/workforce/worker/enroll", body, nil)
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

// PullDispatches GETs /admin/dispatch/queue/pull?worker_id=X and decodes
// the returned envelope array. Returns an empty slice (not nil) when
// nothing is pending.
func (c *AdminClient) PullDispatches(ctx context.Context, workerID string) ([]dispatch.DispatchEnvelope, error) {
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("adminclient: worker_id required")
	}
	path := "/admin/dispatch/queue/pull?worker_id=" + workerID
	var out []dispatch.DispatchEnvelope
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []dispatch.DispatchEnvelope{}
	}
	return out, nil
}

// PullKills GETs /admin/kill/queue/pull and returns pending kill
// requests. The server returns ALL unrouted kills (per dispatchq
// design); the daemon filters by its owned executions on the receive
// side.
func (c *AdminClient) PullKills(ctx context.Context) ([]dispatchq.KillRequest, error) {
	var out []dispatchq.KillRequest
	if err := c.doJSON(ctx, http.MethodGet, "/admin/kill/queue/pull", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []dispatchq.KillRequest{}
	}
	return out, nil
}

// NotifyWorking POSTs to /admin/taskruntime/exec/notify-working. Flips
// the execution state machine submitted → working (v2.2 Phase D state-
// machine fix). Idempotent — the server returns 200 on repeat calls
// against an already-working execution.
func (c *AdminClient) NotifyWorking(ctx context.Context, executionID, cwd, branchName string) error {
	body := map[string]any{
		"execution_id": executionID,
		"cwd":          cwd,
		"branch_name":  branchName,
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/taskruntime/exec/notify-working", body, nil)
}

// Conclude POSTs to /admin/taskruntime/exec/conclude. Closes the state
// machine on clean agent exit: working → completed + task → done.
// Idempotent.
func (c *AdminClient) Conclude(ctx context.Context, executionID, message string) error {
	body := map[string]any{
		"execution_id": executionID,
		"message":      message,
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/taskruntime/exec/conclude", body, nil)
}

// ReportProgress POSTs to /admin/taskruntime/exec/report-progress. The
// `milestone` field maps to the server's `kind` parameter (agent trace
// event kind: `started` | `progress` | `done` | etc.).
func (c *AdminClient) ReportProgress(ctx context.Context, executionID, milestone, content string) error {
	body := map[string]any{
		"execution_id": executionID,
		"kind":         milestone,
		"content":      content,
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/taskruntime/exec/report-progress", body, nil)
}

// ReportFailure POSTs to /admin/taskruntime/exec/report-failure.
func (c *AdminClient) ReportFailure(ctx context.Context, executionID, reason, message string) error {
	body := map[string]any{
		"execution_id": executionID,
		"reason":       reason,
		"message":      message,
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/taskruntime/exec/report-failure", body, nil)
}

// ReportArtifact POSTs to /admin/taskruntime/artifact/append.
//
// v2.3-3b (task #29): when `blob` is non-empty, the bytes are first
// pushed through BlobPut to land them in the center's BlobStore, and
// the returned rel_path is sent as `blob_ref` to artifact/append. When
// `blob` is empty the call degrades to a metadata-only append (legacy
// fakeagent path that pre-resolves refs locally).
//
// rel_path convention: `artifacts/<execution_id>/<kind>-<unix_nanos>`.
// The unique suffix is needed because a single execution can emit
// multiple artifacts of the same kind.
func (c *AdminClient) ReportArtifact(ctx context.Context, executionID string, blob []byte, kind string) error {
	blobRef := ""
	if len(blob) > 0 {
		safeKind := kind
		if safeKind == "" {
			safeKind = "artifact"
		}
		relPath := fmt.Sprintf("artifacts/%s/%s-%d",
			executionID, safeKind, time.Now().UnixNano())
		if err := c.BlobPut(ctx, relPath, blob); err != nil {
			return fmt.Errorf("adminclient: blob_put %s: %w", relPath, err)
		}
		blobRef = relPath
	}
	body := map[string]any{
		"execution_id":  executionID,
		"kind":          kind,
		"title":         kind, // default title = kind
		"blob_ref":      blobRef,
		"url":           "",
		// `inline_size` kept for backwards-compat with v2.2 tests that
		// only checked the metadata payload existed.
		"metadata_json": fmt.Sprintf(`{"inline_size":%d}`, len(blob)),
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/taskruntime/artifact/append", body, nil)
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
	url := "http://unix" + path
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
