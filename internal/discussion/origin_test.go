package discussion

import (
	"errors"
	"testing"
)

func TestOrigin_IsValidAndBranch(t *testing.T) {
	for _, o := range []Origin{OriginCLI, OriginWebConsole, OriginSupervisor, OriginAgentOpenIssue, OriginDerivedFromConversation} {
		if !o.IsValid() {
			t.Errorf("expected valid: %s", o)
		}
	}
	if Origin("nope").IsValid() {
		t.Fatal("nope should not be valid")
	}
	// v1 origins dropped per ADR-0031: feishu_at must not validate.
	if Origin("feishu_at").IsValid() {
		t.Fatal("feishu_at is a dropped v1 origin and must not validate")
	}
	// Sync-build origins
	if !OriginWebConsole.NeedsSyncConversationBuild() ||
		!OriginSupervisor.NeedsSyncConversationBuild() ||
		!OriginDerivedFromConversation.NeedsSyncConversationBuild() {
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
	for _, s := range []string{"cli", "web_console", "supervisor", "agent_open_issue", "derived_from_conversation"} {
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
	// v1 origin dropped per ADR-0031.
	if _, err := ParseOrigin("feishu_at"); !errors.Is(err, ErrInvalidOrigin) {
		t.Fatalf("expected feishu_at to be rejected; got %v", err)
	}
}

func TestOrigin_String(t *testing.T) {
	if OriginCLI.String() != "cli" {
		t.Fatal("string mismatch")
	}
}
