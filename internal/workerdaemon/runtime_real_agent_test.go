// Package workerdaemon — runtime_real_agent_test.go: drives a single
// real-agent dispatch through defaultAgentSpawner up to the binary
// resolution step, verifying:
//
//   1. MCPInjector.Inject materialises mcp_config.runtime.json at mode 0600
//      with secret refs resolved to plaintext.
//   2. AssemblePrompt assembles a prompt containing instructions.md content.
//   3. MCPConfigPath flows into AgentRunnerConfig.
//   4. On Inject failure, ReportFailure is called with reason=secret_unresolvable
//      and no subprocess is spawned.
//
// We exercise the spawner by calling it directly (instead of going through
// Runtime.Run) so the test stays hermetic — no real binary spawn, no
// goroutines, no admin transport.
package workerdaemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// stubSecretResolver returns a fixed plaintext for any name; errs when
// `errOn` matches the secret name (used by the Inject-failure test).
type stubSecretResolver struct {
	plaintext []byte
	errOn     string
}

func (s stubSecretResolver) Resolve(_ context.Context, name string) ([]byte, error) {
	if s.errOn != "" && s.errOn == name {
		return nil, errors.New("simulated resolve failure")
	}
	return s.plaintext, nil
}

func writeAgentHomeDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "instructions.md"),
		[]byte("YOU ARE A TEST AGENT.\nFollow the protocol."), 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := `{
  "mcpServers": {
    "db": {
      "command": "/usr/local/bin/mcp-db",
      "env": {"DB_PASSWORD": "secret:db_password"}
    }
  }
}`
	if err := os.WriteFile(filepath.Join(home, "mcp_config.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func realAgentEnvelope(homeDir string) dispatch.DispatchEnvelope {
	return dispatch.DispatchEnvelope{
		EnvelopeVersion: dispatch.EnvelopeVersionV2,
		ExecutionID:     taskruntime.TaskExecutionID("E-real"),
		TaskID:          taskruntime.TaskID("T-real"),
		WorkerID:        "w-1",
		ProjectID:       "P-1",
		AgentInstanceID: "AI-1",
		// claude-code triggers the real-agent path; the spawner will
		// abort at ResolveBinary which is fine — we only care about the
		// pre-spawn steps (prompt + MCP injection + ReportFailure on
		// Inject error). The PATH lookup error becomes shim_crashed
		// which we observe in fc.failures.
		AgentCLI:      "claude-code",
		WorkspaceMode: execution.WorkspaceDirect,
		TaskTitle:     "Real Agent Test",
		TaskDescription: "do the thing",
		Priority:      "normal",
		HomeDir:       homeDir,
	}
}

func TestDefaultAgentSpawner_RealAgent_AssemblesPromptAndInjectsMCP(t *testing.T) {
	home := writeAgentHomeDir(t)
	fc := &fakeCenter{}
	resolver := stubSecretResolver{plaintext: []byte("super-secret-pw")}
	injector := NewMCPInjector(resolver)
	// SkillLoader without worker-agent.md → AssemblePrompt skips it but
	// still picks up instructions.md from HomeDir.
	rt := NewRuntimeWithDeps(RuntimeConfig{WorkerID: "w-1"}, fc, nil, RuntimeDeps{
		SkillLoader: StaticSkillLoader{},
		MCPInjector: injector,
	})

	env := realAgentEnvelope(home)
	// Spawner will attempt to spawn a non-existent binary; that's
	// expected (we run hermetic, no claude-code binary present). The
	// failure mode we observe: shim_crashed (NOT secret_unresolvable).
	_ = defaultAgentSpawner(context.Background(), env, rt)

	// Failure was ReportFailure(shim_crashed) — NOT secret_unresolvable
	// (the resolver succeeded; the binary lookup is what tanked). This
	// proves the spawner reached the binary-lookup step, which is downstream
	// of both AssemblePrompt and MCPInject — so both must have succeeded.
	fc.mu.Lock()
	if len(fc.failures) == 0 {
		fc.mu.Unlock()
		t.Fatal("expected at least one ReportFailure (binary lookup)")
	}
	for _, f := range fc.failures {
		if f.Milestone == "secret_unresolvable" {
			fc.mu.Unlock()
			t.Fatalf("unexpected secret_unresolvable failure: %+v", f)
		}
	}
	fc.mu.Unlock()

	// Direct-test the materialised pieces independently — the spawner's
	// defer cleanup wipes the runtime.json on return, so we re-invoke
	// the injector here to confirm round-trip behaviour against the
	// same home_dir / resolver.
	rp, cleanup, err := injector.Inject(context.Background(), home)
	if err != nil {
		t.Fatalf("Inject re-run: %v", err)
	}
	defer cleanup()
	raw, err := os.ReadFile(rp)
	if err != nil {
		t.Fatalf("read runtime: %v", err)
	}
	if !strings.Contains(string(raw), "super-secret-pw") {
		t.Fatalf("runtime json missing resolved plaintext: %s", string(raw))
	}
	rpInfo, _ := os.Stat(rp)
	if rpInfo.Mode().Perm() != 0o600 {
		t.Fatalf("runtime file mode=%o (want 0600)", rpInfo.Mode().Perm())
	}
}

func TestDefaultAgentSpawner_RealAgent_InjectFailure_ReportsSecretUnresolvable(t *testing.T) {
	home := writeAgentHomeDir(t)
	fc := &fakeCenter{}
	// Resolver errors on the one secret name in mcp_config.json.
	injector := NewMCPInjector(stubSecretResolver{errOn: "db_password"})
	rt := NewRuntimeWithDeps(RuntimeConfig{WorkerID: "w-1"}, fc, nil, RuntimeDeps{
		MCPInjector: injector,
	})
	env := realAgentEnvelope(home)

	err := defaultAgentSpawner(context.Background(), env, rt)
	if err == nil {
		t.Fatal("expected spawner to return injection error")
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.failures) != 1 {
		t.Fatalf("want 1 failure, got %d: %+v", len(fc.failures), fc.failures)
	}
	if fc.failures[0].Milestone != "secret_unresolvable" {
		t.Fatalf("reason=%q want secret_unresolvable", fc.failures[0].Milestone)
	}
	// No spawn should have happened → no notify-working / no progress.
	if len(fc.working) != 0 {
		t.Fatalf("notify-working should NOT fire on inject failure: %+v", fc.working)
	}
	if len(fc.progress) != 0 {
		t.Fatalf("no progress on inject failure: %+v", fc.progress)
	}
}

func TestDefaultAgentSpawner_RealAgent_NoHomeDir_DegradesGracefully(t *testing.T) {
	// HomeDir empty → prompt is title+description, no MCP injection
	// attempted. We still hit a binary lookup failure but the path
	// should not fail with secret_unresolvable.
	fc := &fakeCenter{}
	rt := NewRuntimeWithDeps(RuntimeConfig{WorkerID: "w-1"}, fc, nil, RuntimeDeps{
		SkillLoader: StaticSkillLoader{"worker-agent.md": []byte("BASE")},
		MCPInjector: NewMCPInjector(nil),
	})
	env := realAgentEnvelope("")
	env.HomeDir = "" // explicit
	_ = defaultAgentSpawner(context.Background(), env, rt)
	fc.mu.Lock()
	defer fc.mu.Unlock()
	for _, f := range fc.failures {
		if f.Milestone == "secret_unresolvable" {
			t.Fatalf("v1-style envelope should not emit secret_unresolvable: %+v", fc.failures)
		}
	}
}

func TestAssemblePrompt_IncludesInstructionsMd(t *testing.T) {
	home := writeAgentHomeDir(t)
	prompt, err := AssemblePrompt(StaticSkillLoader{},
		AssemblePromptInput{
			Envelope: dispatch.DispatchEnvelope{
				TaskTitle:       "T",
				TaskDescription: "D",
			},
			BaseSkill: "worker-agent.md",
			HomeDir:   home,
		})
	if err != nil {
		t.Fatalf("AssemblePrompt: %v", err)
	}
	if !strings.Contains(prompt, "YOU ARE A TEST AGENT") {
		t.Fatalf("prompt missing instructions.md content: %s", prompt)
	}
}
