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
	// migrate + admin + bootstrap + others.
	names := map[string]bool{}
	for _, c := range router.Root.Subcommands {
		names[c.Name] = true
	}
	// v2.7 #162: only deployment/lifecycle/operator commands remain.
	for _, want := range []string{"version", "server", "migrate", "admin", "bootstrap",
		"worker", "install", "uninstall", "upgrade"} {
		if !names[want] {
			t.Errorf("missing command: %s", want)
		}
	}
	// v2.7 #162: data-management + data-read CLI commands are retired.
	for _, gone := range []string{
		"project", "conversation", "channel", "message", "agent", "secret",
		"inspect", "query", "ps", "stats", "logs", "peek-trace"} {
		if names[gone] {
			t.Errorf("retired command still present: %s", gone)
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
