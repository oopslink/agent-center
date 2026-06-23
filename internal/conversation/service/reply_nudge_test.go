package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/replyguard"
)

// buildNudgeSvc wires a ReplyNudgeService over the fixture's repos with the given
// guard + config, sharing the fixture clock (so derivation + nudging agree on now).
func (f *oblFixture) buildNudgeSvc(guard *wakeguard.Guard, cfg replyguard.Config) *ReplyNudgeService {
	return NewReplyNudgeService(f.svc, guard, func() replyguard.Config { return cfg }, f.clock)
}

func nudgeCfg(maxNudges int, cooldown time.Duration) replyguard.Config {
	c := replyguard.DefaultConfig()
	c.MaxNudges = maxNudges
	c.NudgeCooldown = cooldown
	return c
}

func (f *oblFixture) nudges(t *testing.T, svc *ReplyNudgeService, agentID, member, org, name string) []ReplyNudge {
	t.Helper()
	ns, err := svc.NudgesForAgent(context.Background(), agentID, member, org, name)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

// A perceived-unanswered human obligation produces one nudge with a non-empty prompt.
func TestNudge_Human_ProducesNudge(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "please reply")
	f.markSeen(t, bot, "dm-1", "dm-1-m1")

	svc := f.buildNudgeSvc(wakeguard.NewGuard(wakeguard.DefaultConfig()), nudgeCfg(3, time.Minute))
	ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot")
	if len(ns) != 1 || ns[0].NudgeCount != 1 || ns[0].Prompt == "" {
		t.Fatalf("want 1 nudge count=1 with prompt, got %+v", ns)
	}
}

// Bounded: after MaxNudges the obligation is no longer nudged (方案 A only — give up).
func TestNudge_BoundedByMaxNudges(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "please reply")
	f.markSeen(t, bot, "dm-1", "dm-1-m1")

	cooldown := time.Second
	svc := f.buildNudgeSvc(wakeguard.NewGuard(wakeguard.DefaultConfig()), nudgeCfg(2, cooldown))
	// 3 attempts, each past the cooldown; only the first 2 nudge.
	var counts []int
	for i := 0; i < 3; i++ {
		ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot")
		counts = append(counts, len(ns))
		f.clock.Advance(2 * cooldown)
	}
	if counts[0] != 1 || counts[1] != 1 || counts[2] != 0 {
		t.Fatalf("want nudges [1 1 0], got %v", counts)
	}
}

// Cooldown: a second attempt within NudgeCooldown does not re-nudge.
func TestNudge_Cooldown(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "please reply")
	f.markSeen(t, bot, "dm-1", "dm-1-m1")

	svc := f.buildNudgeSvc(wakeguard.NewGuard(wakeguard.DefaultConfig()), nudgeCfg(5, time.Minute))
	if ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot"); len(ns) != 1 {
		t.Fatalf("first attempt should nudge, got %d", len(ns))
	}
	// within cooldown → no nudge
	f.clock.Advance(10 * time.Second)
	if ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot"); len(ns) != 0 {
		t.Fatalf("within cooldown should not re-nudge, got %d", len(ns))
	}
	// past cooldown → nudge again
	f.clock.Advance(2 * time.Minute)
	if ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot"); len(ns) != 1 {
		t.Fatalf("past cooldown should re-nudge, got %d", len(ns))
	}
}

// Agent-authored obligation: ALLOWED by the wake-guardrail → nudge fires.
func TestNudge_AgentAuthored_GuardAllows_Nudges(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "ch-1", conversation.ConversationKindChannel, "org-1", bot, "agent:bot-2")
	f.seedMsg(t, "ch-1-m1", "ch-1", "agent:bot-2", "@Bot please confirm")

	svc := f.buildNudgeSvc(wakeguard.NewGuard(wakeguard.DefaultConfig()), nudgeCfg(3, time.Minute))
	// advance past idleGrace so the unread agent-authored mention is perceived
	f.clock.Advance(replyguard.DefaultConfig().IdleGrace + time.Second)
	ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot")
	if len(ns) != 1 || ns[0].ActorKind != conversation.ActorKindAgent {
		t.Fatalf("want 1 agent-authored nudge, got %+v", ns)
	}
}

// Agent-authored obligation: DROPPED by the wake-guardrail → suppressed, no nudge
// ("被 wake-guardrail 拦下就不用回"). A MaxDepth=0 guard denies every agent hop.
func TestNudge_AgentAuthored_GuardDenies_Suppressed(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "ch-1", conversation.ConversationKindChannel, "org-1", bot, "agent:bot-2")
	f.seedMsg(t, "ch-1-m1", "ch-1", "agent:bot-2", "@Bot please confirm")

	denyGuard := wakeguard.NewGuard(wakeguard.Config{
		MaxDepth: 0, CycleWindow: time.Minute, CycleN: 5, RatePerMin: 10, TokenBudget: 10,
	})
	svc := f.buildNudgeSvc(denyGuard, nudgeCfg(3, time.Minute))
	f.clock.Advance(replyguard.DefaultConfig().IdleGrace + time.Second)
	if ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot"); len(ns) != 0 {
		t.Fatalf("guardrail-denied agent nudge should be suppressed, got %d", len(ns))
	}
}

// Fail-safe: a nil guard never nudges agent-authored obligations (no ungoverned
// agent↔agent ping-pong), but human obligations still nudge.
func TestNudge_NilGuard_AgentSuppressed_HumanStillNudges(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "ch-1", conversation.ConversationKindChannel, "org-1", bot, "agent:bot-2", "user:alice")
	f.seedMsg(t, "ch-1-m1", "ch-1", "agent:bot-2", "@Bot from agent")
	f.seedMsg(t, "ch-1-m2", "ch-1", "user:alice", "@Bot from human")

	svc := f.buildNudgeSvc(nil, nudgeCfg(3, time.Minute))
	f.clock.Advance(replyguard.DefaultConfig().IdleGrace + time.Second)
	ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot")
	// one obligation per conversation = the latest unanswered = the human m2.
	if len(ns) != 1 || ns[0].ActorKind != conversation.ActorKindHuman {
		t.Fatalf("nil guard should still nudge the human obligation, got %+v", ns)
	}
}

// A nil cfgFn falls back to DefaultConfig (no panic), and the identity-member ref
// is honored: a message sent to the agent's MEMBER ref still produces a nudge for
// the same agent (dual-ref contract).
func TestNudge_NilCfgFn_AndMemberRef(t *testing.T) {
	f := setupObl(t)
	const member = conversation.IdentityRef("agent:bot-1-member")
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", member, "user:alice")
	f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "reply please")
	f.markSeen(t, member, "dm-1", "dm-1-m1")

	// nil cfgFn → DefaultConfig; pass the member id as identityMemberID.
	svc := NewReplyNudgeService(f.svc, wakeguard.NewGuard(wakeguard.DefaultConfig()), nil, f.clock)
	ns, err := svc.NudgesForAgent(context.Background(), "bot-1", "bot-1-member", "org-1", "Bot")
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 {
		t.Fatalf("member-ref obligation should nudge with default cfg, got %d", len(ns))
	}
}

// Once the agent replies, the obligation is discharged → no nudge.
func TestNudge_DischargedAfterReply(t *testing.T) {
	f := setupObl(t)
	f.seedConv(t, "dm-1", conversation.ConversationKindDM, "org-1", bot, "user:alice")
	f.seedMsg(t, "dm-1-m1", "dm-1", "user:alice", "please reply")
	f.markSeen(t, bot, "dm-1", "dm-1-m1")
	f.seedMsg(t, "dm-1-m2", "dm-1", bot, "here is my reply")

	svc := f.buildNudgeSvc(wakeguard.NewGuard(wakeguard.DefaultConfig()), nudgeCfg(3, time.Minute))
	if ns := f.nudges(t, svc, "bot-1", "", "org-1", "Bot"); len(ns) != 0 {
		t.Fatalf("discharged obligation should not nudge, got %d", len(ns))
	}
}
