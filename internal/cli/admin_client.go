// Package cli — admin_client.go: HTTP-over-unix-socket Client that talks
// to the center process's admin endpoint.
//
// Per conventions § 0.4 "AppService is the only entry": every CLI command
// outside the `server` boot path and a couple of schema-migration tools
// MUST round-trip through this Client rather than reach into the BC's
// Repositories / Services directly. The Client mirrors the admin endpoint
// surface registered in internal/admin/api/server.go (~79 methods grouped
// by BC).
//
// Mirror of internal/workerdaemon/AdminClient (Phase C); we don't reuse
// that one because the CLI needs a much wider method surface and the
// worker-daemon transport intentionally exposes only the 5 methods the
// daemon itself needs.
//
// Sub-files by BC:
//   - admin_client_workforce.go      (workers, proposals, agents, projects)
//   - admin_client_conversation.go   (conv/msg/channel/participant/derivation)
//   - admin_client_taskruntime.go    (task/exec/IR/artifact/dispatch/kill)
//   - admin_client_discussion.go     (issue lifecycle + bind/link)
//   - admin_client_secret.go         (user_secret CRUD)
//   - admin_client_identity.go       (identity register / find)
//   - admin_client_observability.go  (query / inspect / fleet / stats / logs)
//   - admin_client_cognition.go      (supervisor spawn / invocation / decision)
//
// All sub-files share the doJSON / doPOST / doGET helpers defined here.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
)

// Client is the CLI-side admin transport. It dials either a unix-domain
// socket (default; cfg.Server.AdminSocketPath) or a TCP+TLS endpoint
// with SSH-style fingerprint pinning (v2.3-7b, task #27) — both go
// through the same code path; the kind is captured at construct-time.
//
// Construct one per CLI invocation via NewClient (unix) or
// NewClientFromTarget (any). Zero value is invalid; methods that try
// to use it return ErrClientNotConfigured.
type Client struct {
	socketPath string // legacy: unix socket path (empty when TCP)
	baseURL    string // "http://unix" or "https://host:port"
	httpc      *http.Client
	// token is the bearer attached to every request via the
	// `Authorization: Bearer <token>` header. Populated via WithToken
	// (or left empty for tests that pre-date v2.3-3a auth; production
	// build() pulls from env / bootstrap file).
	token string
}

// NewClient returns a Client targeting the given unix socket path.
// timeout is applied per request; pass 0 for the default 30s.
//
// Preserved unchanged for backward compat with v2.2 callers + tests.
// v2.3-7b callers wanting TCP+TLS should use NewClientFromTarget.
func NewClient(socketPath string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
	}
	return &Client{
		socketPath: socketPath,
		baseURL:    "http://unix",
		httpc: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
	}
}

// NewClientFromTarget constructs a Client from a parsed transport
// target (unix or tcp) + optional fingerprint (mandatory for tcp).
// v2.3-7b (task #27). Returns an error rather than panicking when the
// fingerprint is missing/malformed for tcp — security-sensitive path.
func NewClientFromTarget(target clienttransport.Target, fingerprint string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tr, err := clienttransport.NewHTTPTransport(target, fingerprint, timeout)
	if err != nil {
		return nil, err
	}
	c := &Client{
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

// SocketPath returns the configured socket path (diagnostics).
// Empty when the Client was constructed from a TCP target.
func (c *Client) SocketPath() string {
	if c == nil {
		return ""
	}
	return c.socketPath
}

// WithToken sets the bearer token attached to every subsequent request.
// Returns the receiver to allow chaining at construction sites.
//
// Empty strings clear the token (so tests can deliberately exercise the
// unauthenticated path).
func (c *Client) WithToken(t string) *Client {
	if c == nil {
		return c
	}
	c.token = strings.TrimSpace(t)
	return c
}

// Token exposes the configured bearer (for diagnostics / tests).
func (c *Client) Token() string {
	if c == nil {
		return ""
	}
	return c.token
}

// ErrClientNotConfigured is returned from Client methods when the
// Client wasn't constructed (e.g. CLI invoked without an admin socket
// configured).
var ErrClientNotConfigured = errors.New("admin client: not configured " +
	"(server.admin_socket_path missing or server not running)")

// ErrServerUnreachable is wrapped around network errors that indicate
// the admin socket isn't accepting connections. Handlers translate this
// into a user-facing "is the server running?" hint.
var ErrServerUnreachable = errors.New("admin server unreachable")

// ClientError carries non-2xx admin endpoint responses.
type ClientError struct {
	Method  string
	Path    string
	Status  int
	Code    string // server-side error code (from the JSON envelope)
	Message string // server-side message
	Body    string // raw body for unrecognised shapes
}

// Error implements error.
func (e *ClientError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("admin %s %s: %s: %s (status=%d)",
			e.Method, e.Path, e.Code, e.Message, e.Status)
	}
	return fmt.Sprintf("admin %s %s: status=%d body=%s",
		e.Method, e.Path, e.Status, e.Body)
}

// IsNotFound reports whether the error is a 404 response.
func (e *ClientError) IsNotFound() bool { return e.Status == http.StatusNotFound }

// IsConflict reports whether the error is a 409 response.
func (e *ClientError) IsConflict() bool { return e.Status == http.StatusConflict }

// IsClientNotConfigured reports whether err signals an absent Client.
func IsClientNotConfigured(err error) bool {
	return errors.Is(err, ErrClientNotConfigured)
}

// IsServerUnreachable reports whether err looks like the server isn't
// accepting connections (socket missing, ECONNREFUSED, etc.).
func IsServerUnreachable(err error) bool {
	if errors.Is(err, ErrServerUnreachable) {
		return true
	}
	if err == nil {
		return false
	}
	// net.OpError + syscall.ECONNREFUSED / ENOENT both turn into "connect:"
	// or "no such file or directory" in the wrapped message; we use
	// substring sniffs because the Go net package doesn't always expose
	// these cleanly across platforms.
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "no such file") ||
		strings.Contains(s, "connect: ")
}

// postJSON marshals body, POSTs to path, and decodes JSON into out (if non-nil).
func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// getJSON GETs path (no body) and decodes JSON into out.
//
// Query parameters should be encoded into path by the caller via
// buildQuery / url.Values for clarity at the call site.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// do is the shared request/response helper.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if c == nil || c.httpc == nil {
		return ErrClientNotConfigured
	}
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("admin client: marshal %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(buf)
	}
	base := c.baseURL
	if base == "" {
		// Backward compat with zero-value / pre-7b Clients.
		base = "http://unix"
	}
	reqURL := base + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return fmt.Errorf("admin client: build %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		// v2.3-3a (task #28): admin endpoint requires bearer auth.
		// Tokens carry an `acat_` prefix so the server can recognise
		// them in logs / grep without inspecting the raw bytes.
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		// Translate dial/connect errors into ErrServerUnreachable so
		// handlers can give a clean "server not running" hint.
		if IsServerUnreachable(err) {
			return fmt.Errorf("admin %s %s: %w (%v)", method, path, ErrServerUnreachable, err)
		}
		return fmt.Errorf("admin client: do %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		ce := &ClientError{
			Method: method, Path: path,
			Status: resp.StatusCode,
			Body:   string(respBody),
		}
		// Try to parse server-side error envelope (`{"error": "...", "message": "..."}`).
		var env struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if jerr := json.Unmarshal(respBody, &env); jerr == nil {
			ce.Code = env.Error
			ce.Message = env.Message
		}
		return ce
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("admin client: decode %s %s: %w (body=%s)",
				method, path, err, string(respBody))
		}
	}
	return nil
}

// buildQuery is a small helper that turns key/value pairs into a query
// string suffix. Pairs whose value is empty are omitted (so optional
// filters drop cleanly).
//
//	buildQuery("foo", "1", "bar", "")    -> "?foo=1"
//	buildQuery()                          -> ""
func buildQuery(pairs ...string) string {
	if len(pairs)%2 != 0 {
		return ""
	}
	v := url.Values{}
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			continue
		}
		v.Set(pairs[i], pairs[i+1])
	}
	enc := v.Encode()
	if enc == "" {
		return ""
	}
	return "?" + enc
}

// EnsureSocketExists is a courtesy preflight: if the configured socket
// doesn't exist on disk, return a friendly error pointing at how to
// start the server. Network-level failures (connect refused after the
// socket exists) surface as ErrServerUnreachable during the first call.
//
// Returns nil when the socket file is present OR when c is nil
// (handlers that can run without the client should be unaffected).
func EnsureSocketExists(c *Client) error {
	if c == nil || c.socketPath == "" {
		return nil
	}
	if _, err := os.Stat(c.socketPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: server not running at %s; "+
				"start it with: agent-center server --config=<path>",
				ErrServerUnreachable, c.socketPath)
		}
		return fmt.Errorf("admin client: stat socket %s: %w", c.socketPath, err)
	}
	return nil
}
