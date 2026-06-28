package workerdaemon

import "testing"

// TestBuildExecutorEngine_WiresWriteback covers the W2 branch of buildExecutorEngine
// that builds the center writeback when the controller has an agent-tool caller.
func TestBuildExecutorEngine_WiresWriteback(t *testing.T) {
	trueBin := lookTrue(t)
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.BinaryPath = trueBin
	c.cfg.ToolCaller = &fakeToolCaller{} // W2: enables the real center Writeback path

	home, _, _, err := c.agentPaths("a-wb")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	pl := reconcilePayload{
		AgentID:              "a-wb",
		MaxConcurrentTasks:   1,
		AllowedModels:        []string{"m"},
		DefaultExecutorModel: "d",
	}
	ee, err := c.buildExecutorEngine(home, pl)
	if err != nil {
		t.Fatalf("buildExecutorEngine with ToolCaller: %v", err)
	}
	if ee == nil || ee.monitor == nil || ee.engine == nil {
		t.Fatal("engine/monitor should be built")
	}
}
