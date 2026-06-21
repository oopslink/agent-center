package conversation

import "testing"

func part(id, leftAt string) ParticipantElement {
	return ParticipantElement{IdentityID: IdentityRef(id), Role: "member", JoinedAt: "t", LeftAt: leftAt}
}

func TestDMKey_OrderIndependentAndStable(t *testing.T) {
	a := DMKey([]ParticipantElement{part("user:hayang", ""), part("agent:pd", "")})
	b := DMKey([]ParticipantElement{part("agent:pd", ""), part("user:hayang", "")})
	if a != b {
		t.Fatalf("DMKey must be order-independent: %q != %q", a, b)
	}
	if a != "agent:pd\x1fuser:hayang" {
		t.Fatalf("unexpected key %q", a)
	}
}

func TestDMKey_IgnoresLeftParticipants(t *testing.T) {
	// A participant who left is not part of the active set / dedup key.
	got := DMKey([]ParticipantElement{part("user:h", ""), part("agent:x", "2026-01-01T00:00:00Z")})
	if got != "user:h" {
		t.Fatalf("left participant must be excluded, got %q", got)
	}
}

func TestDMKey_EmptyWhenNoActive(t *testing.T) {
	if got := DMKey(nil); got != "" {
		t.Fatalf("nil participants ⇒ empty key, got %q", got)
	}
	if got := DMKey([]ParticipantElement{part("agent:x", "2026-01-01T00:00:00Z")}); got != "" {
		t.Fatalf("all-left ⇒ empty key, got %q", got)
	}
}

func TestDMKey_SingleParticipant(t *testing.T) {
	// A one-sided DM (e.g. system→agent reminder delivery) still keys on its single
	// active participant, so repeat deliveries dedup to one conversation.
	if got := DMKey([]ParticipantElement{part("agent:remindee", "")}); got != "agent:remindee" {
		t.Fatalf("single-participant key = %q", got)
	}
}
