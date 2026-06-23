package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/conversation/replyguard"
)

// ReplyNudge is one ready-to-inject reply reminder for an agent that owes a
// directed reply. The worker injects Prompt into the agent's session at
// turn-end + TrueIdle (方案 A — the agent itself discharges the obligation).
type ReplyNudge struct {
	ConversationID   conversation.ConversationID
	ConversationKind conversation.ConversationKind
	TriggerMessageID conversation.MessageID
	SenderRef        conversation.IdentityRef
	ActorKind        conversation.MentionActorKind
	NudgeCount       int    // 1-based count INCLUDING this nudge
	Prompt           string // the text to inject
}

// ReplyNudgeService is the server-side enforcement half of the reply-guardrail
// (方案 A only — no fallback backfill). For each outstanding obligation it
// decides whether to emit a re-inject nudge:
//
//   - HUMAN-authored  → always nudge (human intent must deliver), bounded by
//     MaxNudges and NudgeCooldown.
//   - AGENT-authored  → gated through the SHARED wake-guardrail (the SAME *Guard
//     instance as wake delivery, so reply nudges and wakes share the depth/cycle/
//     rate/cost anti-storm state). If the guardrail drops the hop, the nudge is
//     suppressed and the obligation is RELEASED ("被 wake-guardrail 拦下就不用回").
//
// It is safe for concurrent use. Observability mirrors cognition.wake.dropped's
// log-based trace (conversation.directed_reply.nudged / .missed / .suppressed).
type ReplyNudgeService struct {
	obl   *ReplyObligationService
	guard *wakeguard.Guard // SHARED with wake delivery; nil → agent nudges fall back to human-only (never an ungoverned ping-pong)
	cfgFn func() replyguard.Config
	clk   clock.Clock

	mu    sync.Mutex
	state map[string]*nudgeCounter
}

type nudgeCounter struct {
	count       int
	lastNudgeAt int64 // unix nanos of the last nudge; 0 = never
	missedLog   bool  // missed already logged once (avoid spam after exhaustion)
}

// NewReplyNudgeService constructs the service. guard MAY be nil (then
// agent-authored obligations are never nudged — fail-safe: no ungoverned
// agent↔agent ping-pong). cfgFn resolves the live config per call (settings seam,
// no restart). clk is injected for deterministic tests.
func NewReplyNudgeService(
	obl *ReplyObligationService,
	guard *wakeguard.Guard,
	cfgFn func() replyguard.Config,
	clk clock.Clock,
) *ReplyNudgeService {
	if cfgFn == nil {
		cfgFn = replyguard.DefaultConfig
	}
	return &ReplyNudgeService{
		obl:   obl,
		guard: guard,
		cfgFn: cfgFn,
		clk:   clk,
		state: map[string]*nudgeCounter{},
	}
}

// NudgesForAgent derives the agent's outstanding obligations and returns the
// nudges the worker should inject NOW (after bounding + cooldown + wake-guardrail
// gating). agentID is the execution-entity id; identityMemberID is the optional
// identity-member id (either may appear as a participant/sender ref — same dual-ref
// contract as the inbox); displayName is the @-handle for mention matching.
func (s *ReplyNudgeService) NudgesForAgent(
	ctx context.Context, agentID, identityMemberID, orgID, displayName string,
) ([]ReplyNudge, error) {
	refs := agentRefs(agentID, identityMemberID)
	cfg := s.cfgFn()
	now := s.clk.Now()
	obs, err := s.obl.OutstandingForIdentity(ctx, refs, orgID, displayName, cfg.ObligationTTL, cfg.IdleGrace, now)
	if err != nil {
		return nil, fmt.Errorf("reply nudges: derive obligations: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ReplyNudge, 0, len(obs))
	for _, ob := range obs {
		key := agentID + "|" + string(ob.ConversationID) + "|" + string(ob.TriggerMessageID)
		st := s.state[key]
		if st == nil {
			st = &nudgeCounter{}
			s.state[key] = st
		}
		// Cooldown: don't re-nudge the same obligation too soon.
		if st.lastNudgeAt != 0 && now.UnixNano()-st.lastNudgeAt < int64(cfg.NudgeCooldown) {
			continue
		}
		// Exhausted: give up (方案 A only — no backfill). Log missed ONCE.
		if st.count >= cfg.MaxNudges {
			if !st.missedLog {
				st.missedLog = true
				slog.Info("conversation.directed_reply.missed",
					"agent", agentID, "conversation_id", string(ob.ConversationID),
					"trigger_message_id", string(ob.TriggerMessageID),
					"actor_kind", string(ob.ActorKind), "nudges", st.count)
			}
			continue
		}
		// Agent-authored obligations are gated through the SHARED wake-guardrail.
		if ob.ActorKind == conversation.ActorKindAgent {
			if s.guard == nil {
				continue // fail-safe: no guard → never nudge agent→agent
			}
			from := strings.TrimPrefix(string(ob.SenderRef), agentParticipantPrefix)
			tr := s.guard.EvaluateHop(from, agentID, string(ob.TriggerMessageID), now)
			if !tr.Allowed {
				slog.Info("conversation.directed_reply.suppressed",
					"agent", agentID, "from", from, "gate", string(tr.Gate),
					"conversation_id", string(ob.ConversationID), "reason", tr.Reason)
				continue // 被 wake-guardrail 拦下就不用回
			}
		}
		st.count++
		st.lastNudgeAt = now.UnixNano()
		nudge := ReplyNudge{
			ConversationID:   ob.ConversationID,
			ConversationKind: ob.ConversationKind,
			TriggerMessageID: ob.TriggerMessageID,
			SenderRef:        ob.SenderRef,
			ActorKind:        ob.ActorKind,
			NudgeCount:       st.count,
			Prompt:           buildReplyNudgePrompt(ob),
		}
		out = append(out, nudge)
		slog.Info("conversation.directed_reply.nudged",
			"agent", agentID, "conversation_id", string(ob.ConversationID),
			"trigger_message_id", string(ob.TriggerMessageID),
			"actor_kind", string(ob.ActorKind), "nudge_count", st.count)
	}
	return out, nil
}

// agentParticipantPrefix is the ADR-0033 agent ref prefix.
const agentParticipantPrefix = "agent:"

// agentRefs builds the dual identity refs (execution-entity + identity-member)
// for an agent, skipping blanks and duplicates.
func agentRefs(agentID, identityMemberID string) []conversation.IdentityRef {
	refs := make([]conversation.IdentityRef, 0, 2)
	if agentID != "" {
		refs = append(refs, conversation.IdentityRef(agentParticipantPrefix+agentID))
	}
	if identityMemberID != "" && identityMemberID != agentID {
		refs = append(refs, conversation.IdentityRef(agentParticipantPrefix+identityMemberID))
	}
	return refs
}

// buildReplyNudgePrompt renders the re-inject text. It names the source
// conversation + sender and instructs the agent to discharge (reply or explicitly
// decline) WHERE the message came from (§5-④), mirroring workAvailableNudge's
// terse, action-oriented style.
func buildReplyNudgePrompt(ob ReplyObligation) string {
	where := string(ob.ConversationKind)
	if ob.ConversationName != "" {
		where = string(ob.ConversationKind) + " \"" + ob.ConversationName + "\""
	}
	return fmt.Sprintf(
		"📨 You have an unanswered directed message from %s in %s (conversation %s) that you perceived but never replied to. "+
			"Now that you're idle, reply in THAT conversation with post_message — accept (and say what you'll do), decline with a reason, or answer the question. "+
			"A silent mark_seen does not count as a reply.",
		ob.SenderRef, where, ob.ConversationID)
}
