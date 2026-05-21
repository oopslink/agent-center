package discussion

import (
	"errors"
	"testing"
)

func TestOrigin_IsValidAndBranch(t *testing.T) {
	for _, o := range []Origin{OriginCLI, OriginWebConsole, OriginFeishuAt, OriginSupervisor, OriginAgentOpenIssue} {
		if !o.IsValid() {
			t.Errorf("expected valid: %s", o)
		}
	}
	if Origin("nope").IsValid() {
		t.Fatal("nope should not be valid")
	}
	// Sync-build origins
	if !OriginWebConsole.NeedsSyncConversationBuild() ||
		!OriginFeishuAt.NeedsSyncConversationBuild() ||
		!OriginSupervisor.NeedsSyncConversationBuild() {
		t.Error("expected sync build")
	}
	// Lazy-create origins
	if OriginCLI.NeedsSyncConversationBuild() ||
		OriginAgentOpenIssue.NeedsSyncConversationBuild() {
		t.Error("expected lazy create")
	}
	if Origin("bogus").NeedsSyncConversationBuild() {
		t.Error("unknown origin must not be sync-build")
	}
}

func TestParseOrigin(t *testing.T) {
	for _, s := range []string{"cli", "web_console", "feishu_at", "supervisor", "agent_open_issue"} {
		o, err := ParseOrigin(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if string(o) != s {
			t.Fatalf("roundtrip mismatch %s", s)
		}
	}
	_, err := ParseOrigin("bogus")
	if !errors.Is(err, ErrInvalidOrigin) {
		t.Fatalf("expected ErrInvalidOrigin, got %v", err)
	}
}

func TestOrigin_String(t *testing.T) {
	if OriginCLI.String() != "cli" {
		t.Fatal("string mismatch")
	}
}
