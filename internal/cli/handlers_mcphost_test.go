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
