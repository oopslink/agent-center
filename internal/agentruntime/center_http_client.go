package agentruntime

// center_http_client.go — the runtime's OWN center transport (design §4.2). Until now
// the runtime reached the center only through the daemon-injected *AdminClient
// (cfg.ToolCaller). For a self-contained, k8s-ready runtime the LocalRuntime must be
// able to build its OWN authed center client from its config (AdminURL / WorkerToken /
// ServerFingerprint) without depending on the daemon handing one in. The daemon MAY
// still inject one (cfg.ToolCaller) — this is the fallback path when it does not.
//
// It rides the SAME transport the daemon's *AdminClient uses (clienttransport: unix
// socket or TCP+TLS with SSH-style fingerprint pinning) + the SAME worker-bearer auth,
// and POSTs to the SAME /admin/agent-tools/<tool> surface — so the center's
// requireAgentOnWorker check behaves identically. It lives in agentruntime (imports
// only clienttransport, no import cycle) and implements the narrow ToolCaller seam, so
// newCenterClient / centerClientAdapter compose over it unchanged.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
)

// CenterHTTPClient is a self-contained authed center transport built from the
// runtime's own config. It satisfies ToolCaller (CallAgentTool), so it drops straight
// into newCenterClient / the reconcile lister.
type CenterHTTPClient struct {
	httpc   *http.Client
	baseURL string
	token   string
}

// compile-time check: the self-built client is a ToolCaller.
var _ ToolCaller = (*CenterHTTPClient)(nil)

// NewCenterHTTPClient builds a center client from the runtime's config. adminURL is a
// clienttransport target spec (`unix:/path`, a bare `/path`, or `tcp://host:port`);
// fingerprint is mandatory for a TCP target (SSH-style pinning) and ignored for unix;
// token is the worker bearer attached to every request. timeout ≤ 0 defaults to 30s.
func NewCenterHTTPClient(adminURL, fingerprint, token string, timeout time.Duration) (*CenterHTTPClient, error) {
	if strings.TrimSpace(adminURL) == "" {
		return nil, fmt.Errorf("agentruntime: center client: admin_url required")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	target, err := clienttransport.ParseTarget(adminURL)
	if err != nil {
		return nil, fmt.Errorf("agentruntime: center client: parse target: %w", err)
	}
	tr, err := clienttransport.NewHTTPTransport(target, strings.TrimSpace(fingerprint), timeout)
	if err != nil {
		return nil, fmt.Errorf("agentruntime: center client: transport: %w", err)
	}
	base := target.BaseURL()
	if base == "" {
		base = "http://unix"
	}
	return &CenterHTTPClient{
		httpc:   &http.Client{Transport: tr, Timeout: timeout},
		baseURL: base,
		token:   strings.TrimSpace(token),
	}, nil
}

// CallAgentTool POSTs body to /admin/agent-tools/<tool> and writes the raw JSON
// response into *out (when non-nil). On non-2xx it returns an error carrying the
// status + body verbatim (the reconcile caller only needs the data / a failure signal,
// not the typed mcphost.AdminToolError the MCP proxy path builds). Mirrors
// AdminClient.CallAgentTool/doRaw so behavior matches the daemon-injected path.
func (c *CenterHTTPClient) CallAgentTool(ctx context.Context, tool string, body any, out *json.RawMessage) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("agentruntime: center client: marshal %s: %w", tool, err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/admin/agent-tools/"+tool, reader)
	if err != nil {
		return fmt.Errorf("agentruntime: center client: build %s: %w", tool, err)
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
		return fmt.Errorf("agentruntime: center client: do %s: %w", tool, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("agentruntime: center client: %s: status %d: %s", tool, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		*out = append((*out)[:0], raw...)
	}
	return nil
}
