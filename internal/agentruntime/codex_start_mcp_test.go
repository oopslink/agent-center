package agentruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/sessioninstance"
	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/mcphost"
)

type fakeSandboxManager struct {
	binding SandboxBinding
}

func (f fakeSandboxManager) EnsureAgentSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error) {
	return f.binding, nil
}

func (f fakeSandboxManager) GetAgentSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, bool, error) {
	if f.binding.SandboxID == "" {
		return SandboxBinding{}, false, nil
	}
	return f.binding, true, nil
}

func (f fakeSandboxManager) StartSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error) {
	return f.binding, nil
}

func (f fakeSandboxManager) SuspendSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error) {
	return f.binding, nil
}

func (f fakeSandboxManager) ResetSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error) {
	return f.binding, nil
}

func (f fakeSandboxManager) DeleteSandbox(context.Context, SandboxEnsureRequest) (SandboxBinding, error) {
	return f.binding, nil
}

func (f fakeSandboxManager) Health(context.Context, SandboxEnsureRequest) (SandboxBinding, error) {
	return f.binding, nil
}

func (f fakeSandboxManager) ComputerUseEndpoint(context.Context, SandboxEnsureRequest) (string, error) {
	return f.binding.ComputerUseEndpoint, nil
}

// TestStartCodex_WritesMCPConfigAndCodexHome pins the T972 supervisor-MCP wiring:
// Start(CLI=codex) generates the canonical mcp_config.runtime.json (agent-center host
// + per-agent creds), translates it into $CODEX_HOME/config.toml, and hands the codex
// starter the CODEX_HOME dir so codex loads the [mcp_servers.agent-center] table.
func TestStartCodex_WritesMCPConfigAndCodexHome(t *testing.T) {
	base := t.TempDir()
	var got CodexSpec
	cfg := LocalRuntimeConfig{
		AgentID:       "agent-x",
		Reporter:      &nopReporter{},
		Log:           func(string, ...any) {},
		WorkerID:      "worker-1",
		AgentHomeBase: base,
		BinaryPath:    "/opt/agent-center-worker",
		AdminURL:      "https://127.0.0.1:9443",
		WorkerToken:   "tok-secret",
		CodexBinary:   "/usr/local/bin/codex",
		CodexStarter: func(_ context.Context, spec CodexSpec) (Session, error) {
			got = spec
			return &fakeSession{}, nil
		},
	}
	rt := NewLocalRuntime(cfg, &SessionState{})

	if err := rt.Start(context.Background(), StartSpec{
		AgentID: "agent-x",
		Version: 1,
		CLI:     CLICodex,
		Model:   "gpt-5-codex",
	}); err != nil {
		t.Fatalf("Start(codex): %v", err)
	}

	home := filepath.Join(base, "agents", "agent-x")
	wantCodexHome := filepath.Join(home, "codex-home")
	if got.CodexHome != wantCodexHome {
		t.Fatalf("CodexSpec.CodexHome = %q, want %q", got.CodexHome, wantCodexHome)
	}
	// The codex binary + tasks dir are threaded through as well.
	if got.Binary != "/usr/local/bin/codex" {
		t.Errorf("CodexSpec.Binary = %q, want the configured codex binary", got.Binary)
	}

	cfgPath := filepath.Join(wantCodexHome, "config.toml")
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	s := string(b)
	// The agent-center MCP server table is present so the codex supervisor can call
	// create_task/complete_task/post_message. Codex gets the full MCP startup
	// catalog because its native tool_search indexes only startup-listed MCP tools.
	for _, want := range []string{
		"[mcp_servers.agent-center]",
		`command = "/opt/agent-center-worker"`,
		`"worker"`, `"mcp-host"`,
		`AC_MCP_TIER_TOOLS = "false"`,
		`AC_MCP_GENERATION = "1"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config.toml missing %q; got:\n%s", want, s)
		}
	}
	// The per-agent worker token reaches the codex-launched MCP host (a supervisor
	// carries center creds — that is how it authenticates its tool calls).
	if !strings.Contains(s, "tok-secret") {
		t.Errorf("config.toml missing per-agent worker token; got:\n%s", s)
	}
}

func TestStartCodex_InheritsComputerUseCodexHomeResources(t *testing.T) {
	base := t.TempDir()
	sourceCodexHome := t.TempDir()
	t.Setenv("CODEX_HOME", sourceCodexHome)
	nodeRoot := t.TempDir()
	nodeRepl := filepath.Join(nodeRoot, "node_repl")
	node := filepath.Join(nodeRoot, "node")
	modules := filepath.Join(nodeRoot, "node_modules")
	service := filepath.Join(sourceCodexHome, "computer-use", "Codex Computer Use.app", "Contents", "MacOS", "SkyComputerUseService")
	for _, p := range []string{nodeRepl, node, service} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	oldNodeRepl, oldNode, oldModules, oldService := codexNodeReplCommand, codexNodeCommand, codexNodeModuleDirs, codexComputerUseService
	t.Cleanup(func() {
		codexNodeReplCommand = oldNodeRepl
		codexNodeCommand = oldNode
		codexNodeModuleDirs = oldModules
		codexComputerUseService = oldService
	})
	codexNodeReplCommand = nodeRepl
	codexNodeCommand = node
	codexNodeModuleDirs = modules
	codexComputerUseService = filepath.Join(t.TempDir(), "missing-service")
	if err := os.WriteFile(filepath.Join(sourceCodexHome, "auth.json"), []byte(`{"token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceCodexHome, "config.toml"), []byte(`
notify = ["/Users/oopslink/.codex/computer-use/Codex Computer Use.app/Contents/SharedSupport/SkyComputerUseClient.app/Contents/MacOS/SkyComputerUseClient", "turn-ended"]

[mcp_servers.agent-center]
command = "/stale/agent-center"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range codexInheritedResourceDirs {
		if err := os.MkdirAll(filepath.Join(sourceCodexHome, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var got CodexSpec
	cfg := LocalRuntimeConfig{
		AgentID:       "agent-x",
		Reporter:      &nopReporter{},
		Log:           func(string, ...any) {},
		WorkerID:      "worker-1",
		AgentHomeBase: base,
		BinaryPath:    "/opt/agent-center-worker",
		AdminURL:      "https://127.0.0.1:9443",
		WorkerToken:   "tok-secret",
		SandboxManager: fakeSandboxManager{binding: SandboxBinding{
			SandboxID:           "sbx-agent-x",
			AgentID:             "agent-x",
			WorkerID:            "worker-1",
			Provider:            SandboxProviderTartMacOSVM,
			VMName:              "ac-agent-x",
			State:               SandboxStateRunning,
			ComputerUseEndpoint: "vm://agent-x",
		}},
		CodexStarter: func(_ context.Context, spec CodexSpec) (Session, error) {
			got = spec
			return &fakeSession{}, nil
		},
	}
	rt := NewLocalRuntime(cfg, &SessionState{})

	if err := rt.Start(context.Background(), StartSpec{
		AgentID: "agent-x",
		Version: 1,
		CLI:     CLICodex,
		Sandbox: SandboxConfig{Enabled: true, Provider: SandboxProviderTartMacOSVM},
	}); err != nil {
		t.Fatalf("Start(codex): %v", err)
	}

	b, err := os.ReadFile(filepath.Join(got.CodexHome, codexConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "SkyComputerUseClient") {
		t.Fatalf("codex config must inherit Computer Use notify hook; got:\n%s", s)
	}
	if strings.Contains(s, "/stale/agent-center") {
		t.Fatalf("codex config kept stale source MCP table; got:\n%s", s)
	}
	for _, want := range []string{
		"[mcp_servers.node_repl]",
		`command = "` + nodeRepl + `"`,
		`SKY_CUA_ENDPOINT = "vm://agent-x"`,
		`SKY_CUA_SERVICE_PATH = "` + service + `"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("codex config missing Computer Use node_repl entry %q; got:\n%s", want, s)
		}
	}
	if !strings.Contains(got.ExtraSystemPrompt, "== Computer Use ==") {
		t.Fatalf("Codex extra system prompt must include Computer Use instructions when available; got:\n%s", got.ExtraSystemPrompt)
	}
	for _, name := range codexInheritedResourceDirs {
		target, err := os.Readlink(filepath.Join(got.CodexHome, name))
		if err != nil {
			t.Fatalf("%s should be linked into codex-home: %v", name, err)
		}
		if target != filepath.Join(sourceCodexHome, name) {
			t.Fatalf("%s link target = %q, want source dir", name, target)
		}
	}
}

func TestStartCodex_PreflightsFullCatalogForNativeToolSearch(t *testing.T) {
	base := t.TempDir()
	var gotTierTools bool
	var gotRequired []string
	cfg := LocalRuntimeConfig{
		AgentID:       "agent-x",
		Reporter:      &nopReporter{},
		Log:           func(string, ...any) {},
		WorkerID:      "worker-1",
		AgentHomeBase: base,
		BinaryPath:    "/opt/agent-center-worker",
		AdminURL:      "https://127.0.0.1:9443",
		WorkerToken:   "tok-secret",
		CodexStarter: func(_ context.Context, spec CodexSpec) (Session, error) {
			return &fakeSession{}, nil
		},
		MCPPreflight: func(_ context.Context, cfg mcphost.Config, required ...string) error {
			gotTierTools = cfg.TierTools
			gotRequired = append([]string(nil), required...)
			return nil
		},
	}
	rt := NewLocalRuntime(cfg, &SessionState{})

	if err := rt.Start(context.Background(), StartSpec{
		AgentID: "agent-x",
		Version: 1,
		CLI:     CLICodex,
	}); err != nil {
		t.Fatalf("Start(codex): %v", err)
	}

	if gotTierTools {
		t.Fatal("codex supervisor preflight must use TierTools=false so every MCP tool is in the startup catalog")
	}
	for _, want := range []string{"post_message", "list_my_tasks", "get_my_profile", "get_plan", "list_task_executions", "runtime_deploy_restart"} {
		if !slices.Contains(gotRequired, want) {
			t.Fatalf("codex preflight required tools missing %q: %v", want, gotRequired)
		}
	}
	if slices.Contains(gotRequired, "search_tools") {
		t.Fatalf("codex preflight must not require mcp-host search_tools: %v", gotRequired)
	}
}

func TestStartCodex_BlocksWhenSupervisorMCPPreflightFails(t *testing.T) {
	base := t.TempDir()
	calledStarter := false
	cfg := LocalRuntimeConfig{
		AgentID:       "agent-x",
		Reporter:      &nopReporter{},
		Log:           func(string, ...any) {},
		WorkerID:      "worker-1",
		AgentHomeBase: base,
		BinaryPath:    "/opt/agent-center-worker",
		AdminURL:      "https://127.0.0.1:9443",
		WorkerToken:   "tok-secret",
		CodexStarter: func(_ context.Context, spec CodexSpec) (Session, error) {
			calledStarter = true
			return &fakeSession{}, nil
		},
		MCPPreflight: func(context.Context, mcphost.Config, ...string) error {
			return errors.New("missing post_message")
		},
	}
	rt := NewLocalRuntime(cfg, &SessionState{})

	err := rt.Start(context.Background(), StartSpec{
		AgentID: "agent-x",
		Version: 1,
		CLI:     CLICodex,
	})
	if err == nil || !strings.Contains(err.Error(), "supervisor MCP preflight failed") || !strings.Contains(err.Error(), "missing post_message") {
		t.Fatalf("Start(codex) error = %v, want mcp preflight failure", err)
	}
	if calledStarter {
		t.Fatal("CodexStarter was called even though MCP preflight failed")
	}
}

func TestStartClaude_BlocksWhenSupervisorMCPPreflightFails(t *testing.T) {
	base := t.TempDir()
	calledStarter := false
	cfg := LocalRuntimeConfig{
		AgentID:       "agent-x",
		Reporter:      &nopReporter{},
		Log:           func(string, ...any) {},
		WorkerID:      "worker-1",
		AgentHomeBase: base,
		BinaryPath:    "/opt/agent-center-worker",
		AdminURL:      "https://127.0.0.1:9443",
		WorkerToken:   "tok-secret",
		Starter: func(_ context.Context, cfg SupervisorSessionConfig) (Session, error) {
			calledStarter = true
			return &fakeSession{}, nil
		},
		MCPPreflight: func(context.Context, mcphost.Config, ...string) error {
			return errors.New("missing post_message")
		},
	}
	rt := NewLocalRuntime(cfg, &SessionState{})

	err := rt.Start(context.Background(), StartSpec{
		AgentID: "agent-x",
		Version: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "supervisor MCP preflight failed") || !strings.Contains(err.Error(), "missing post_message") {
		t.Fatalf("Start(claude) error = %v, want mcp preflight failure", err)
	}
	if calledStarter {
		t.Fatal("Starter was called even though MCP preflight failed")
	}
}

func TestStartCodex_DoesNotResumePriorThreadWithoutHealthyResume(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "agents", "agent-x")
	if _, err := sessioninstance.AcquireInstance(home, "th_poisoned", 1234); err != nil {
		t.Fatalf("seed prior instance: %v", err)
	}

	var got CodexSpec
	cfg := LocalRuntimeConfig{
		AgentID:       "agent-x",
		Reporter:      &nopReporter{},
		Log:           func(string, ...any) {},
		WorkerID:      "worker-1",
		AgentHomeBase: base,
		BinaryPath:    "/opt/agent-center-worker",
		AdminURL:      "https://127.0.0.1:9443",
		WorkerToken:   "tok-secret",
		CodexStarter: func(_ context.Context, spec CodexSpec) (Session, error) {
			got = spec
			return &fakeSession{}, nil
		},
	}
	rt := NewLocalRuntime(cfg, &SessionState{})

	if err := rt.Start(context.Background(), StartSpec{
		AgentID: "agent-x",
		Version: 1,
		CLI:     CLICodex,
		// Resume is false on the boot-reconcile Codex path; a prior SessionID alone is
		// not enough to resume because it can point at a poisoned incomplete turn.
		Resume: false,
	}); err != nil {
		t.Fatalf("Start(codex): %v", err)
	}

	if got.ResumeThreadID != "" {
		t.Fatalf("CodexSpec.ResumeThreadID = %q, want empty fresh session", got.ResumeThreadID)
	}
	persisted, err := sessioninstance.ReadInstance(home)
	if err != nil {
		t.Fatalf("read instance: %v", err)
	}
	if persisted.SessionID != "" {
		t.Fatalf("persisted SessionID = %q, want cleared by fresh generation", persisted.SessionID)
	}
}

func TestStartCodex_IncludesMemoryHarnessInFreshPrompt(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "agents", "agent-x")
	mem := memory.NewEngine(filepath.Join(home, "memory"), "")
	ctx := context.Background()
	if err := mem.EnsureRootInit(ctx); err != nil {
		t.Fatalf("EnsureRootInit: %v", err)
	}
	if err := mem.WriteScoped(ctx, memory.MemoryScope{Kind: memory.MemScopeGlobal}, "GLOBAL-BRAIN\n", "t", "t@x", "seed"); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	var got CodexSpec
	cfg := LocalRuntimeConfig{
		AgentID:       "agent-x",
		Reporter:      &nopReporter{},
		Log:           func(string, ...any) {},
		WorkerID:      "worker-1",
		AgentHomeBase: base,
		BinaryPath:    "/opt/agent-center-worker",
		AdminURL:      "https://127.0.0.1:9443",
		WorkerToken:   "tok-secret",
		CodexStarter: func(_ context.Context, spec CodexSpec) (Session, error) {
			got = spec
			return &fakeSession{}, nil
		},
	}
	rt := NewLocalRuntime(cfg, &SessionState{})

	if err := rt.Start(ctx, StartSpec{
		AgentID:           "agent-x",
		Version:           1,
		CLI:               CLICodex,
		PromptDescription: "Persona note.",
	}); err != nil {
		t.Fatalf("Start(codex): %v", err)
	}

	for _, want := range []string{"Persona note.", "== Your memory ==", "GLOBAL-BRAIN"} {
		if !strings.Contains(got.ExtraSystemPrompt, want) {
			t.Fatalf("ExtraSystemPrompt missing %q; got:\n%s", want, got.ExtraSystemPrompt)
		}
	}
	if !strings.Contains(got.ExtraSystemPrompt, "use Codex's native tool_search") ||
		strings.Contains(got.ExtraSystemPrompt, "search_tools with an empty query") {
		t.Fatalf("codex prompt must use native tool_search, got:\n%s", got.ExtraSystemPrompt)
	}
}
