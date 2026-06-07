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
	ExecutionID string
	Prompt      string
	// SystemPrompt (v2.8.1 #278 D PR4a) is an optional persistent system
	// instruction appended at launch (claude --append-system-prompt). Unlike
	// Prompt (the initial conversation turn), it is re-applied on EVERY launch
	// (fresh/resume/crash-relaunch) and is NOT part of conversation history — so
	// it is idempotent (never duplicated). Empty → no system prompt added.
	SystemPrompt string
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

	// v2 (ADR-0030 § 2)

	// Probe is called by the worker daemon at online to check whether the
	// CLI binary is installed and return its version string. ctx may carry
	// a timeout; callers should cap probe at a few seconds.
	Probe(ctx context.Context) (available bool, version string, err error)

	// SupportedFeatures returns the v2 high-level feature flags this CLI
	// supports. DispatchService uses these to reject agent instances whose
	// AgentInstance.config requires a feature the worker can't provide
	// (per ADR-0030 § 5).
	SupportedFeatures() FeatureSet

	// BuildMCPConfigArg translates the canonical mcp_config.runtime.json
	// path into the CLI-specific invocation (flag / env / copy-to-path)
	// per ADR-0027 § 7. Returns zero MCPSetup if SupportedFeatures.SupportsMCP
	// is false (worker daemon checks the flag before calling).
	BuildMCPConfigArg(runtimeJSONPath string) (MCPSetup, error)

	// BuildSkillMountSetup translates the home_dir/skills/ source directory
	// into the CLI-specific mount (CLI arg / symlink HOME) per ADR-0028 § 7.
	// execDir is the per-execution working directory (worktree path) used by
	// the symlink fallback to put the skills under a CLI-discoverable home
	// alias. Returns zero SkillMountSetup if SupportedFeatures.SupportsSkills
	// is false.
	BuildSkillMountSetup(homeDirSkills, execDir string) (SkillMountSetup, error)
}

// FeatureSet captures the v2 high-level feature flags an adapter supports
// (per ADR-0030 § 2).
type FeatureSet struct {
	SupportsMCP     bool
	SupportsSkills  bool
	SupportsSession bool
}

// MCPSetup is the per-CLI translation of mcp_config.runtime.json into the
// command-line invocation (per ADR-0030 § 2 + ADR-0027 § 7).
//
// Worker daemon assembly:
//   - append Args to CmdSpec.Args
//   - merge Env into CmdSpec.Env
//   - if CopyTo is non-empty, copy the supplied runtime.json there before spawn
type MCPSetup struct {
	Args   []string          // additional CLI args to append (e.g. ["--mcp-config", "/path"])
	Env    map[string]string // additional env vars to merge
	CopyTo string            // absolute path to copy runtime.json to (CLI reads from fixed location)
}

// SkillMountMode is the discriminator of SkillMountSetup (per ADR-0030 § 2 +
// ADR-0028 § 7).
type SkillMountMode int

const (
	// SkillMountCLIArg passes skills via a CLI flag (e.g. --skill-path).
	SkillMountCLIArg SkillMountMode = iota
	// SkillMountSymlinkHomeClaude exports HOME=execDir and creates a
	// symlink ~/.claude/skills → home_dir/skills/ inside that HOME. Used
	// for CLIs that only read from a fixed in-home path.
	SkillMountSymlinkHomeClaude
)

// SkillMountSetup is the per-CLI translation of home_dir/skills/ into the
// command-line invocation + pre-spawn setup (per ADR-0030 § 2).
type SkillMountSetup struct {
	Mode     SkillMountMode
	Args     []string          // mode=CLIArg appends to CmdSpec.Args
	Env      map[string]string // both modes merge into CmdSpec.Env
	PreSpawn func() error      // mode=SymlinkHomeClaude creates the symlink; nil = no action
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
