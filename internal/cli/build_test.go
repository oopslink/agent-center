package cli

import (
	"testing"
)

func TestBuildRouter_FullTreeBuilds(t *testing.T) {
	router, cfgPath, err := BuildRouter("v-test", "abc123", []string{})
	if err != nil {
		t.Fatalf("BuildRouter: %v", err)
	}
	if router == nil {
		t.Fatal("router nil")
	}
	// Top-level command count: we expect at least version + server +
	// migrate + admin + bootstrap + supervisor + others.
	names := map[string]bool{}
	for _, c := range router.Root.Subcommands {
		names[c.Name] = true
	}
	for _, want := range []string{"version", "server", "migrate", "admin", "bootstrap",
		"supervisor", "worker", "task", "issue", "identity",
		"project", "conversation", "channel", "inspect", "query", "ps", "stats",
		"logs", "peek-trace"} {
		if !names[want] {
			t.Errorf("missing command: %s", want)
		}
	}
	if cfgPath != "" {
		t.Errorf("unexpected cfg path: %s", cfgPath)
	}
}

func TestBuildRouter_WithConfigFlag(t *testing.T) {
	_, cfgPath, err := BuildRouter("", "", []string{"--config", "/tmp/c.yaml", "version"})
	if err != nil {
		t.Fatal(err)
	}
	if cfgPath != "/tmp/c.yaml" {
		t.Errorf("cfg path: %s", cfgPath)
	}
}
