package conversation

import "testing"

// I7-D2: IdentityRef.ActorKind classifies a sender (human/agent/system) so the
// inbox can attach the reply obligation — human directed = must reply, agent
// mention = SilentAck-eligible.
func TestIdentityRef_ActorKind(t *testing.T) {
	cases := []struct {
		ref  IdentityRef
		want MentionActorKind
	}{
		{"user:alice", ActorKindHuman},
		{"agent:bot-1", ActorKindAgent},
		{"system", ActorKindSystem},
		{"user:", ActorKindSystem},  // malformed (no id) → not human
		{"agent:", ActorKindSystem}, // malformed (no id) → not agent
		{"", ActorKindSystem},
	}
	for _, c := range cases {
		if got := c.ref.ActorKind(); got != c.want {
			t.Errorf("ActorKind(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
}
