// Package workercontroller is the worker's launcher/controller brain (T854 D6,
// design §4.5): it turns the center's desired agent set into launched agent
// PROCESSES (via an AgentLauncher) and proxies each control command to the target
// agent's process (via agentcontrol), instead of hosting N runtimes in-process.
//
// Command delivery is cursor-gated for the PD reliability ruling: Deliver returns an
// error whenever the agent process has not accepted the command, and the worker's
// control loop leaves that command un-acked (no center cursor advance) and retries —
// so a command issued while an agent is down/restarting is not lost. The launcher
// concurrently rebuilds the process; the retry lands once it is back up.
package workercontroller

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentlauncher"
)

// controlClient is the subset of agentcontrol.Client the controller uses (seam for
// tests).
type controlClient interface {
	Deliver(ctx context.Context, cmd agentcontrol.Command) error
}

// clientFactory builds a control client for an agent's socket (seam for tests;
// production → agentcontrol.NewClient).
type clientFactory func(sockPath string) controlClient

// Controller reconciles desired agents → launched processes and proxies commands.
type Controller struct {
	launcher  agentlauncher.AgentLauncher
	sockDir   string
	newClient clientFactory
	log       func(format string, args ...any)

	mu      sync.Mutex
	clients map[string]controlClient // agentID → control client
}

// Config wires a Controller.
type Config struct {
	// Launcher creates/rebuilds agent processes (required).
	Launcher agentlauncher.AgentLauncher
	// SockDir is the SHORT per-worker runtime dir the agent control sockets live in
	// (must match what the agent-runtime process binds; kept short for the unix path
	// limit). Required.
	SockDir string
	// NewClient builds a control client for a socket path (nil → agentcontrol.NewClient
	// with DeliverTimeout).
	NewClient func(sockPath string) controlClient
	// DeliverTimeout bounds one command delivery (zero → 5s).
	DeliverTimeout time.Duration
	// Log is optional.
	Log func(format string, args ...any)
}

// New builds a Controller.
func New(cfg Config) (*Controller, error) {
	if cfg.Launcher == nil {
		return nil, errors.New("workercontroller: launcher required")
	}
	if cfg.SockDir == "" {
		return nil, errors.New("workercontroller: sock_dir required")
	}
	log := cfg.Log
	if log == nil {
		log = func(string, ...any) {}
	}
	nc := cfg.NewClient
	if nc == nil {
		to := cfg.DeliverTimeout
		if to <= 0 {
			to = 5 * time.Second
		}
		nc = func(sock string) controlClient { return agentcontrol.NewClient(sock, to) }
	}
	return &Controller{
		launcher:  cfg.Launcher,
		sockDir:   cfg.SockDir,
		newClient: nc,
		log:       log,
		clients:   make(map[string]controlClient),
	}, nil
}

// sockPathFor returns the control socket path for an agent (short dir + hashed name).
func (c *Controller) sockPathFor(agentID string) string {
	return filepath.Join(c.sockDir, agentcontrol.SocketName(agentID))
}

// clientFor returns (memoized) the control client for an agent.
func (c *Controller) clientFor(agentID string) controlClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	cl, ok := c.clients[agentID]
	if !ok {
		cl = c.newClient(c.sockPathFor(agentID))
		c.clients[agentID] = cl
	}
	return cl
}

// Reconcile makes exactly `desired` the launched set: Ensure each desired agent
// (idempotent — no-op if already up) and Stop any launched agent no longer desired.
// Best-effort per agent (one failure is logged, the rest proceed) so a single bad
// agent does not wedge the whole worker.
func (c *Controller) Reconcile(desired []string) {
	want := make(map[string]struct{}, len(desired))
	for _, id := range desired {
		if id == "" {
			continue
		}
		want[id] = struct{}{}
		if err := c.launcher.Ensure(agentlauncher.AgentSpec{AgentID: id}); err != nil {
			c.log("workercontroller: ensure agent=%s: %v", id, err)
		}
	}
	for _, id := range c.launcher.Running() {
		if _, ok := want[id]; !ok {
			if err := c.launcher.Stop(id); err != nil {
				c.log("workercontroller: stop agent=%s: %v", id, err)
			}
			c.mu.Lock()
			delete(c.clients, id)
			c.mu.Unlock()
		}
	}
}

// EnsureAgent launches one agent if not already up (used when a command targets an
// agent the controller hasn't reconciled yet — e.g. a work_available arriving before
// the reconcile). Idempotent.
func (c *Controller) EnsureAgent(agentID string) error {
	if agentID == "" {
		return errors.New("workercontroller: ensure requires agent_id")
	}
	return c.launcher.Ensure(agentlauncher.AgentSpec{AgentID: agentID})
}

// Deliver proxies one command to its target agent's process. It first ensures the
// agent is launched (so a command for a not-yet-running agent brings it up), then
// delivers. A delivery failure (agent down/restarting/rejecting) returns an error so
// the caller (the control loop) does NOT advance the center cursor and retries.
func (c *Controller) Deliver(ctx context.Context, cmd agentcontrol.Command) error {
	if cmd.AgentID == "" {
		return errors.New("workercontroller: command missing agent_id")
	}
	if err := c.EnsureAgent(cmd.AgentID); err != nil {
		return err // can't even launch → keep the command un-acked, retry
	}
	return c.clientFor(cmd.AgentID).Deliver(ctx, cmd)
}

// Running returns the launched agent ids (sorted).
func (c *Controller) Running() []string {
	ids := c.launcher.Running()
	sort.Strings(ids)
	return ids
}

// Shutdown stops all agent processes (worker drain).
func (c *Controller) Shutdown(ctx context.Context) error {
	return c.launcher.Shutdown(ctx)
}
