// Package agentcontrol is the worker→agent-process control-command transport (T854
// D6, design §4.5): HTTP over a per-agent unix-domain socket.
//
// WHY this exists: the center's control stream is WORKER-scoped (one cursor per
// worker), so N per-agent processes cannot each self-subscribe. The worker keeps the
// single center stream + cursor and PROXIES each command to the target agent's
// process over this transport. The agent process runs a Server (its "命令入口");
// the worker's controller holds a Client per agent.
//
// RELIABILITY (PD ruling): the worker must NOT advance the center cursor until a
// command is reliably delivered. Client.Deliver returns an error whenever the agent
// process is down / restarting / rejects the command, so the controller leaves the
// command un-acked and retries next tick (the launcher meanwhile rebuilds the
// process). At-least-once delivery + idempotent command entries ⇒ no lost work
// across an agent restart window.
//
// Unix socket now, k8s-translatable later: swap the unix listener/dialer for a TCP
// address (worker→agent-pod HTTP) behind the same Server/Client — the command
// envelope and semantics are unchanged.
package agentcontrol

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

const (
	// controlPath is the command-ingress HTTP route the server exposes.
	controlPath = "/control"
	// healthPath is the liveness/identity route (T860 gap5): a worker restart probes it to
	// decide adopt-vs-respawn. It returns the agent id the process is serving, which is the
	// AUTHORITATIVE survivor signal — a bare "socket accepts a connection" is not enough
	// (a recycled pid / stale socket could answer), so the worker re-adopts a surviving
	// agent process ONLY when this route echoes back the expected agent id.
	healthPath = "/health"
	// concurrencyPath exposes the runtime's live executor snapshot to the worker heartbeat.
	concurrencyPath = "/concurrency"
	// executionStatePath exposes the supervisor-local task/executor/mapping view.
	executionStatePath = "/execution-state"
)

// HealthResponse is the body of a GET /health probe: the agent id this process serves.
type HealthResponse struct {
	AgentID        string    `json:"agent_id"`
	Build          BuildInfo `json:"build,omitempty"`
	PID            int       `json:"pid,omitempty"`
	ParentPID      int       `json:"parent_pid,omitempty"`
	StartedAt      string    `json:"started_at,omitempty"`
	ExecutablePath string    `json:"executable_path,omitempty"`
}

// BuildInfo is the build identity reported by a live agent-runtime process.
// It is intentionally additive on /health: adoption only needs AgentID, while
// deployed-binary smoke can use the richer fields to detect current/live skew.
type BuildInfo struct {
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Branch  string `json:"branch,omitempty"`
	BuiltAt string `json:"built_at,omitempty"`
}

// SocketName returns a SHORT, collision-resistant control-socket filename for an
// agent. A unix socket PATH has an OS length cap (~104 darwin / 108 linux) and an
// agent id can be long, so the filename is a fixed 25-char hash-derived name. The
// worker and the agent-runtime process derive the same name from the agent id; keep
// the DIRECTORY short too (a per-worker runtime dir, NOT the deep agent home) so the
// full path fits. Both sides must join it with the same short dir.
func SocketName(agentID string) string {
	sum := sha1.Sum([]byte(agentID))
	return "acs-" + hex.EncodeToString(sum[:8]) + ".sock" // "acs-"(4) + 16 hex + ".sock"(5) = 25
}

// Command is one control command proxied worker→agent — a single-agent projection of
// the center's ControlCommand. Payload is the type-specific body the agent's Handler
// decodes; Seq is the center cursor position (for logging / idempotency).
type Command struct {
	Type           string          `json:"type"`
	AgentID        string          `json:"agent_id"`
	Seq            int64           `json:"seq"`
	ID             string          `json:"id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Status         string          `json:"status,omitempty"`
	CreatedAt      string          `json:"created_at,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

// Handler is the agent-side command sink (the runtime's command entry). Returning an
// error makes the Server reply 5xx, so the worker treats the command as UNDELIVERED
// and retries — i.e. a handler must return nil ONLY when it has durably accepted the
// command (matching the center's at-least-once contract).
type Handler interface {
	Handle(ctx context.Context, cmd Command) error
}

// Snapshotter is the optional extension a Handler may implement to expose live executor
// state. A handler without it is treated as active=0.
type Snapshotter interface {
	SnapshotConcurrency() []concurrency.ExecutorSnapshot
}

// AgentSnapshotter is the slot-aware snapshot extension. When present, it is the
// source for /concurrency; Snapshotter remains for older handlers/tests.
type AgentSnapshotter interface {
	SnapshotAgentConcurrency() concurrency.AgentSnapshot
}

// ExecutionStateSnapshotter exposes the runtime-authoritative supervisor control
// view. Unlike /concurrency, this is read by the supervisor's MCP host, not by the
// center heartbeat.
type ExecutionStateSnapshotter interface {
	SnapshotExecutionState(ctx context.Context) (concurrency.ExecutionStateSnapshot, error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, cmd Command) error

// Handle calls f.
func (f HandlerFunc) Handle(ctx context.Context, cmd Command) error { return f(ctx, cmd) }

// Server listens on a unix socket and dispatches POSTed Commands to a Handler. It is
// the agent-runtime process's control-plane ingress.
type Server struct {
	sockPath string
	agentID  string
	health   HealthResponse
	handler  Handler
	log      func(format string, args ...any)
	srv      *http.Server
	ln       net.Listener
}

// ServerOption customizes additive health metadata.
type ServerOption func(*Server)

// WithHealthInfo attaches build/process diagnostics to GET /health.
func WithHealthInfo(info HealthResponse) ServerOption {
	return func(s *Server) {
		info.AgentID = s.agentID
		s.health = info
	}
}

// NewServer binds a Server to sockPath (removing a stale socket first). agentID is
// echoed by GET /health so a worker restart can authoritatively identify the surviving
// agent process on this socket (T860 gap5). Call Serve to run it and Close to stop.
func NewServer(sockPath, agentID string, h Handler, log func(format string, args ...any), opts ...ServerOption) (*Server, error) {
	if sockPath == "" {
		return nil, errors.New("agentcontrol: server socket path required")
	}
	if agentID == "" {
		return nil, errors.New("agentcontrol: server agent_id required")
	}
	if h == nil {
		return nil, errors.New("agentcontrol: server handler required")
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	// A leftover socket from a prior (crashed) incarnation blocks bind — remove it.
	_ = removeSocket(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("agentcontrol: listen %s: %w", sockPath, err)
	}
	s := &Server{
		sockPath: sockPath,
		agentID:  agentID,
		health: HealthResponse{
			AgentID:        agentID,
			PID:            os.Getpid(),
			ParentPID:      os.Getppid(),
			ExecutablePath: executablePath(),
		},
		handler: h,
		log:     log,
		ln:      ln,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	s.health.AgentID = agentID
	mux := http.NewServeMux()
	mux.HandleFunc(controlPath, s.serveControl)
	mux.HandleFunc(healthPath, s.serveHealth)
	mux.HandleFunc(concurrencyPath, s.serveConcurrency)
	mux.HandleFunc(executionStatePath, s.serveExecutionState)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return s, nil
}

// serveHealth answers the liveness/identity probe with the served agent id.
func (s *Server) serveHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.health)
}

func (s *Server) serveConcurrency(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if snapper, ok := s.handler.(AgentSnapshotter); ok {
		snap := snapper.SnapshotAgentConcurrency()
		if snap.Executors == nil {
			snap.Executors = []concurrency.ExecutorSnapshot{}
		}
		if snap.Active == 0 {
			snap.Active = len(snap.Executors)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snap)
		return
	}
	var execs []concurrency.ExecutorSnapshot
	if snapper, ok := s.handler.(Snapshotter); ok {
		execs = snapper.SnapshotConcurrency()
	}
	if execs == nil {
		execs = []concurrency.ExecutorSnapshot{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(concurrency.AgentSnapshot{Active: len(execs), Executors: execs})
}

func (s *Server) serveExecutionState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snapper, ok := s.handler.(ExecutionStateSnapshotter)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(concurrency.ExecutionStateSnapshot{
			AgentID:             s.agentID,
			AvailableTasks:      []concurrency.TaskAuthorityRow{},
			ActiveTasks:         []concurrency.ExecutionTaskRow{},
			TaskExecutorMapping: []concurrency.TaskExecutorBinding{},
			Executors:           []concurrency.ExecutorStateRow{},
			Integrity:           "degraded",
			IntegrityError:      "handler does not expose execution state",
			UpdatedAt:           time.Now().UTC(),
		})
		return
	}
	snap, err := snapper.SnapshotExecutionState(r.Context())
	if err != nil {
		http.Error(w, "execution state failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if snap.AvailableTasks == nil {
		snap.AvailableTasks = []concurrency.TaskAuthorityRow{}
	}
	if snap.ActiveTasks == nil {
		snap.ActiveTasks = []concurrency.ExecutionTaskRow{}
	}
	if snap.TaskExecutorMapping == nil {
		snap.TaskExecutorMapping = []concurrency.TaskExecutorBinding{}
	}
	if snap.Executors == nil {
		snap.Executors = []concurrency.ExecutorStateRow{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snap)
}

func executablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

func (s *Server) serveControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cmd Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, "bad command json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.handler.Handle(r.Context(), cmd); err != nil {
		// Undelivered → 5xx so the worker keeps the command un-acked and retries.
		s.log("agentcontrol: handle cmd type=%s seq=%d: %v", cmd.Type, cmd.Seq, err)
		http.Error(w, "handle failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Serve runs the HTTP server until Close (or the listener errors). Returns nil on a
// clean Close.
func (s *Server) Serve() error {
	err := s.srv.Serve(s.ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close stops the server and removes the socket.
func (s *Server) Close(ctx context.Context) error {
	err := s.srv.Shutdown(ctx)
	_ = removeSocket(s.sockPath)
	return err
}

// Client delivers Commands to one agent's Server over its unix socket. Safe for
// concurrent use.
type Client struct {
	http *http.Client
}

// NewClient builds a Client dialing sockPath. timeout bounds a single delivery (zero
// → 5s).
func NewClient(sockPath string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialer := &net.Dialer{}
	return &Client{
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, "unix", sockPath)
				},
			},
		},
	}
}

// Deliver POSTs cmd to the agent's control server. It returns an error whenever the
// command was NOT accepted — the agent is down / restarting (dial fails) or the
// handler rejected it (5xx). The controller uses that error to leave the center
// command un-acked (no cursor advance) and retry, so no command is lost across an
// agent restart window (PD reliability ruling).
func (c *Client) Deliver(ctx context.Context, cmd Command) error {
	body, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("agentcontrol: marshal cmd: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent"+controlPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("agentcontrol: deliver type=%s: %w", cmd.Type, err) // agent down/unreachable
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("agentcontrol: deliver type=%s: agent returned %s", cmd.Type, resp.Status)
	}
	return nil
}

// Probe GETs /health and returns the agent id the process on the socket is serving.
// A worker restart uses this as the AUTHORITATIVE survivor check (T860 gap5): it adopts
// a surviving agent process only when Probe succeeds AND returns the expected agent id.
// An error (dial fails / non-2xx / bad body) means "no live matching process" → respawn.
func (c *Client) Probe(ctx context.Context) (string, error) {
	hr, err := c.Info(ctx)
	if err != nil {
		return "", err
	}
	return hr.AgentID, nil
}

// Info GETs /health and returns the full live-runtime health payload. Probe keeps
// the historical agent-id-only API for the worker adoption path.
func (c *Client) Info(ctx context.Context) (HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent"+healthPath, nil)
	if err != nil {
		return HealthResponse{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return HealthResponse{}, fmt.Errorf("agentcontrol: probe: %w", err) // agent down/unreachable
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return HealthResponse{}, fmt.Errorf("agentcontrol: probe: agent returned %s", resp.Status)
	}
	var hr HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		return HealthResponse{}, fmt.Errorf("agentcontrol: probe: decode: %w", err)
	}
	return hr, nil
}

// SnapshotConcurrency returns the agent runtime's live executor snapshot.
func (c *Client) SnapshotConcurrency(ctx context.Context) (concurrency.AgentSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent"+concurrencyPath, nil)
	if err != nil {
		return concurrency.AgentSnapshot{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return concurrency.AgentSnapshot{}, fmt.Errorf("agentcontrol: concurrency: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return concurrency.AgentSnapshot{}, fmt.Errorf("agentcontrol: concurrency: agent returned %s", resp.Status)
	}
	var snap concurrency.AgentSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return concurrency.AgentSnapshot{}, fmt.Errorf("agentcontrol: concurrency: decode: %w", err)
	}
	return snap, nil
}

// SnapshotExecutionState returns the supervisor-local execution state from the
// agent runtime control socket.
func (c *Client) SnapshotExecutionState(ctx context.Context) (concurrency.ExecutionStateSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent"+executionStatePath, nil)
	if err != nil {
		return concurrency.ExecutionStateSnapshot{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return concurrency.ExecutionStateSnapshot{}, fmt.Errorf("agentcontrol: execution state: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return concurrency.ExecutionStateSnapshot{}, fmt.Errorf("agentcontrol: execution state: agent returned %s", resp.Status)
	}
	var snap concurrency.ExecutionStateSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return concurrency.ExecutionStateSnapshot{}, fmt.Errorf("agentcontrol: execution state: decode: %w", err)
	}
	return snap, nil
}

// removeSocket unlinks a unix socket path, ignoring a missing file.
func removeSocket(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
