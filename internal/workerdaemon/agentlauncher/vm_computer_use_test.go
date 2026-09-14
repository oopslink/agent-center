package agentlauncher

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTartGuestComputerUseSetupLimitsGUIEnvironment(t *testing.T) {
	command := tartGuestComputerUseSetup(tartGuestMountPlan{codexHome: "/source with spaces"}, "/agent home", "agent-1", []string{
		"HTTP_PROXY=http://127.0.0.1:7897", "AC_MCP_ADMIN_TOKEN=must-not-reach-gui", "HOME=/host/user", "TMPDIR=/host/temp",
	})
	for _, unwanted := range []string{"must-not-reach-gui", "/host/user", "/host/temp"} {
		if strings.Contains(command, unwanted) {
			t.Fatalf("GUI setup inherited host-only value %q", unwanted)
		}
	}
	for _, required := range []string{"/agent home/codex-home", "/Users/admin/agent-center/state/agent-1/codex-sqlite", "http://127.0.0.1:7897"} {
		if !strings.Contains(command, required) {
			t.Fatalf("GUI setup missing %q", required)
		}
	}
}

func TestTartGuestComputerUseSetupWithoutAppStillCreatesLocalSQLite(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	dir := filepath.Join(t.TempDir(), "local sqlite")
	payload, _ := json.Marshal(map[string]any{"source": "", "env": map[string]string{"CODEX_SQLITE_HOME": dir}})
	if out, err := exec.Command(python, "-c", tartGuestComputerUseSetupScript, string(payload)).CombinedOutput(); err != nil {
		t.Fatalf("guest setup failed: %v: %s", err, out)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("guest SQLite directory not created: %v", err)
	}
}
