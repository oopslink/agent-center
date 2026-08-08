package cli

import "testing"

func TestMCPHostTierToolsFromEnv(t *testing.T) {
	t.Setenv("AC_MCP_TIER_TOOLS", "")
	if got, err := mcpHostTierToolsFromEnv(); err != nil || !got {
		t.Fatalf("unset AC_MCP_TIER_TOOLS = %t, %v; want true, nil", got, err)
	}

	t.Setenv("AC_MCP_TIER_TOOLS", "false")
	if got, err := mcpHostTierToolsFromEnv(); err != nil || got {
		t.Fatalf("false AC_MCP_TIER_TOOLS = %t, %v; want false, nil", got, err)
	}

	t.Setenv("AC_MCP_TIER_TOOLS", "not-a-bool")
	if _, err := mcpHostTierToolsFromEnv(); err == nil {
		t.Fatal("invalid AC_MCP_TIER_TOOLS must error")
	}
}

func TestMCPHostGenerationFromEnv(t *testing.T) {
	t.Setenv("AC_MCP_GENERATION", "")
	if got, err := mcpHostGenerationFromEnv(); err != nil || got != 0 {
		t.Fatalf("unset AC_MCP_GENERATION = %d, %v; want 0, nil", got, err)
	}

	t.Setenv("AC_MCP_GENERATION", "7")
	if got, err := mcpHostGenerationFromEnv(); err != nil || got != 7 {
		t.Fatalf("AC_MCP_GENERATION=7 -> %d, %v; want 7, nil", got, err)
	}

	t.Setenv("AC_MCP_GENERATION", "-1")
	if _, err := mcpHostGenerationFromEnv(); err == nil {
		t.Fatal("negative AC_MCP_GENERATION must error")
	}

	t.Setenv("AC_MCP_GENERATION", "nope")
	if _, err := mcpHostGenerationFromEnv(); err == nil {
		t.Fatal("invalid AC_MCP_GENERATION must error")
	}
}
