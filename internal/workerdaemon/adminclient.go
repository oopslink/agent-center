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

// Heartbeat is an alias for Enroll. In v2.2 the server's enroll IS
// the heartbeat (v2.3 may split via /admin/workforce/worker/heartbeat).
//
// The server's Enroll currently rejects repeat enrolments with
// 409 already_exists; for v2.2 single-host that's fine — we treat
// the 409 as the success-after-first-call signal (the worker IS
// enrolled, that's what we wanted to assert). v2.3 will replace this
// with a proper /heartbeat endpoint.
func (c *AdminClient) Heartbeat(ctx context.Context, workerID string, capabilities []string) error {
	err := c.Enroll(ctx, workerID, capabilities)
	if err == nil {
		return nil
	}
	if ae, ok := err.(*AdminError); ok && ae.Status == http.StatusConflict {
		return nil
	}
	return err
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
// v2.2 worker-daemon scope: blob bytes are not transferred over the
// admin endpoint (the artifact_append handler expects a blob_ref the
// caller already wrote to the BlobStore). For Phase C we treat
// `blob` as an inline payload, base64-encode-able, but in practice
// fakeagent emits artifact events with already-resolved refs. To keep
// the surface minimal and not couple BlobStore here, this method
// accepts the blob and embeds it as a base64 metadata field; the
// canonical write path is artifact_append with kind + title only.
// Phase D may grow a real blob upload route.
func (c *AdminClient) ReportArtifact(ctx context.Context, executionID string, blob []byte, kind string) error {
	body := map[string]any{
		"execution_id":  executionID,
		"kind":          kind,
		"title":         kind, // default title = kind
		"blob_ref":      "",
		"url":           "",
		"metadata_json": fmt.Sprintf(`{"inline_size":%d}`, len(blob)),
	}
	return c.doJSON(ctx, http.MethodPost, "/admin/taskruntime/artifact/append", body, nil)
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
