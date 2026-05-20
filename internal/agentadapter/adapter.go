// Package agentadapter hosts the agent CLI adapter abstraction (Claude
// Code / Codex / OpenCode). Per ADR-0002 / 05-agent-adapters.
package agentadapter

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"
)

// SpawnRequest is the daemon → adapter request describing how to invoke
// the agent CLI (05-agent-adapters § 2).
type SpawnRequest struct {
	ExecutionID  string
	Prompt       string
	WorkingDir   string
	SkillFiles   []string
	AgentLogPath string
	Env          map[string]string
	Timeout      time.Duration
}

// CmdSpec is the platform-agnostic command description the shim feeds into
// os/exec.
type CmdSpec struct {
	Binary string
	Args   []string
	Env    []string
	Stdin  io.Reader
}

// Adapter is the interface each agent CLI implements.
type Adapter interface {
	Name() string
	BuildCommand(req SpawnRequest) (CmdSpec, error)
	ParseEvent(line []byte) (AgentTraceEvent, error)
	SupportsSession() bool
}

// Sentinel errors.
var (
	ErrAdapterNotFound  = errors.New("agentadapter: adapter not found")
	ErrNotImplemented   = errors.New("agentadapter: not implemented in v1")
	ErrInvalidEventJSON = errors.New("agentadapter: invalid event JSON")
)

// Registry is a process-global registry of agent adapters. Subpackages
// (claudecode, codex, opencode) self-register via init().
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// DefaultRegistry is the process-global registry; init() functions of
// agent CLI subpackages call Register on it.
var DefaultRegistry = &Registry{adapters: make(map[string]Adapter)}

// Register adds an Adapter to the registry. Last-write-wins on duplicate
// names (used to allow tests to inject mocks for known names).
func (r *Registry) Register(a Adapter) {
	if a == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Name()] = a
}

// Get returns the adapter for the given name.
func (r *Registry) Get(name string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

// Names returns the registered adapter names sorted for deterministic
// output.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.adapters))
	for n := range r.adapters {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Reset clears the registry (test-only).
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters = make(map[string]Adapter)
}

// Register is a package-level shortcut for DefaultRegistry.Register.
func Register(a Adapter) { DefaultRegistry.Register(a) }

// Get is a package-level shortcut for DefaultRegistry.Get.
func Get(name string) (Adapter, bool) { return DefaultRegistry.Get(name) }

// Names is a package-level shortcut for DefaultRegistry.Names.
func Names() []string { return DefaultRegistry.Names() }

// CmdSpecWithoutStdin returns a CmdSpec usable from contexts that cannot
// hold a non-nil io.Reader (debug rendering / logs).
func CmdSpecWithoutStdin(spec CmdSpec) CmdSpec {
	spec.Stdin = nil
	return spec
}

// AdapterContext is a placeholder for future per-spawn context (logging
// helpers etc.).
type AdapterContext struct {
	Ctx context.Context
}
