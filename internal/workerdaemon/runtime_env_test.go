package workerdaemon

import "testing"

func TestRuntimeAgentEnv_MergesIdentityDisplayNameAndProfileEnv(t *testing.T) {
	got := runtimeAgentEnv("agent-35ac0e16", "agent-center-dev4", map[string]string{
		"FOO":             "bar",
		"GIT_AUTHOR_NAME": "profile-wins",
	})
	if got["FOO"] != "bar" {
		t.Fatalf("FOO = %q, want bar", got["FOO"])
	}
	if got["GIT_AUTHOR_NAME"] != "profile-wins" {
		t.Fatalf("GIT_AUTHOR_NAME = %q, want profile override", got["GIT_AUTHOR_NAME"])
	}
	if got["GIT_COMMITTER_NAME"] != "agent-center-dev4" {
		t.Fatalf("GIT_COMMITTER_NAME = %q, want display name override", got["GIT_COMMITTER_NAME"])
	}
	if got["GIT_AUTHOR_EMAIL"] == "" || got["GIT_COMMITTER_EMAIL"] == "" {
		t.Fatalf("git identity email env missing: %v", got)
	}
}

func TestCloneEnvVars_ReturnsIndependentCopy(t *testing.T) {
	in := map[string]string{"FOO": "bar"}
	got := cloneEnvVars(in)
	in["FOO"] = "changed"
	if got["FOO"] != "bar" {
		t.Fatalf("clone changed with source: %v", got)
	}
	if cloneEnvVars(nil) != nil {
		t.Fatal("cloneEnvVars(nil) must return nil")
	}
}
