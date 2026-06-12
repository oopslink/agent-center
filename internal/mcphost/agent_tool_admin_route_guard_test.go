package mcphost

import (
	"os"
	"regexp"
	"testing"
)

// TestAgentFacingTool_HasAdminRoute is the (2) reverse-lockstep half of the
// agent-facing tool parity guard — the complement to TestAgentFacingToolParity
// (3) (live ListTools == AgentFacingToolNames). Every canonical agent-facing tool
// name EXCEPT FilesSeamTools (which move bytes through the FileMover seam, not via
// callAdmin) must have a registered POST /admin/agent-tools/<name> admin route;
// otherwise the agent's MCP tool proxies via callAdmin to a 404 (the admin handler
// was never wired). This guards the #285/#299 seam from the OTHER direction: a
// tool that is exposed + canonical but whose admin handler is missing. Parses
// internal/admin/api/server.go by source (avoids an mcphost→admin import cycle).
//
// Inverse-mutation: drop a /admin/agent-tools/<name> registration → that name
// FAILS here.
func TestAgentFacingTool_HasAdminRoute(t *testing.T) {
	seam := map[string]bool{}
	for _, s := range FilesSeamTools {
		seam[s] = true
	}
	src, err := os.ReadFile("../admin/api/server.go")
	if err != nil {
		t.Fatalf("read admin server.go: %v", err)
	}
	re := regexp.MustCompile(`/admin/agent-tools/([a-z_]+)`)
	routes := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		routes[m[1]] = true
	}
	if len(routes) < 30 {
		t.Fatalf("only %d admin agent-tool routes parsed — regex likely stale vs server.go", len(routes))
	}
	for _, name := range AgentFacingToolNames {
		if seam[name] {
			continue // moves bytes via the FileMover seam, not a callAdmin route
		}
		if !routes[name] {
			t.Errorf("agent-facing tool %q has NO /admin/agent-tools/%s route — the MCP tool "+
				"proxies via callAdmin to a 404 (admin handler unwired). Register it in "+
				"admin/api/server.go, or if it is a non-callAdmin seam tool add it to FilesSeamTools.", name, name)
		}
	}
}
