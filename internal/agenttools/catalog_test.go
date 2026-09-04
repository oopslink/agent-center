package agenttools

import "testing"

func TestCoreToolNamesDerivedFromFullMinusDeferred(t *testing.T) {
	full := map[string]bool{}
	for _, name := range AgentFacingToolNames {
		if full[name] {
			t.Fatalf("duplicate agent-facing tool %q", name)
		}
		full[name] = true
	}
	deferred := map[string]bool{}
	for _, tool := range SecondaryTools {
		if !full[tool.Name] {
			t.Fatalf("deferred tool %q is not in AgentFacingToolNames", tool.Name)
		}
		if deferred[tool.Name] {
			t.Fatalf("duplicate deferred tool %q", tool.Name)
		}
		deferred[tool.Name] = true
	}
	core := CoreToolNames()
	if len(core) != len(AgentFacingToolNames)-len(deferred) {
		t.Fatalf("core tool count = %d, want full-deferred = %d", len(core), len(AgentFacingToolNames)-len(deferred))
	}
	coreSet := map[string]bool{}
	for _, name := range core {
		if deferred[name] {
			t.Fatalf("deferred tool %q leaked into core tools", name)
		}
		if !full[name] {
			t.Fatalf("core tool %q is not in AgentFacingToolNames", name)
		}
		coreSet[name] = true
	}
	for _, name := range []string{"list_my_tasks", "get_task", "start_task", "post_message"} {
		if !coreSet[name] {
			t.Fatalf("required fresh-runtime core tool %q missing from derived core catalog", name)
		}
	}
}
