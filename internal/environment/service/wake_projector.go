package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// commandTypeAgentWake is the D2-e-i immediate-wakeup command the WakeProjector
// enqueues onto a waiting_input agent's Worker control stream when a human (or
// another agent) posts a message into the agent's TASK conversation (OQ5). The
// daemon AgentController interprets it (injects the message into the long-lived
// claude session + reports the WorkItem active); D1's NoopHandler acks it today —
// fully additive, the control loop stays DORMANT (ControlClient nil).
const commandTypeAgentWake = "agent.wake"

// commandTypeAgentConverse is the v2.7 #185 (FINDING-H) conversational-wake
// command: a HUMAN posts a message into a DM or Channel where an agent is a
// participant (DM: directed at the agent; Channel: @mentions the agent's
// display_name) → the WakeProjector enqueues this onto the agent's Worker
// control stream. The daemon AgentController injects the message into the
// agent's running session WITHOUT a WorkItem (DM/channel have no WorkItem) and
// advances the agent's read-state cursor. Distinct from agent.wake (which is
// task-WorkItem-keyed). LOOP-BREAK: only HUMAN (`user:`) senders trigger it, so
// an agent's own reply never wakes any agent.
const commandTypeAgentConverse = "agent.converse"

// userParticipantPrefix is the IdentityRef scheme for a human participant. v2.7
// #185 wakes agents ONLY on messages from a human sender — this is the
// structural loop-break (an agent-sender or system message never wakes an
// agent) and the "agents reply to humans only" rule.
const userParticipantPrefix = "user:"

// EvtAgentAwaitingInput is the D2-e-ii (OQ5 method 甲) outbox event emitted by
// the request_input admin handler IN THE SAME TX as the agent's WorkItem moving
// active→waiting_input. The WakeProjector consumes it to flush ALL of the
// agent's UNREAD messages (since its read-state cursor) in the task conversation
// as ONE merged stdin injection — "deliver all unread whenever an agent ENTERS
// waiting_input". Combined with the e-i immediate wake (a message arriving WHILE
// already waiting_input), this gives buffer-when-active + merge-simultaneous.
const EvtAgentAwaitingInput = "agent.awaiting_input"

// ownerRefTasksPrefix is the task-owned conversation owner_ref scheme.
const ownerRefTasksPrefix = "pm://tasks/"

// agentParticipantPrefix is the IdentityRef scheme for an agent participant's
// read-state cursor (the read_state repo is keyed by IdentityRef, so "agent:<id>"
// resolves the agent's own cursor in the task conversation).
const agentParticipantPrefix = "agent:"

// WakeProjector turns a `conversation.message_added` outbox event for a TASK
// conversation into `agent.wake` control commands for every agent whose
// AgentWorkItem on that task is currently waiting_input (v2.7 D2-e-i / OQ5). It
// mirrors AgentControlProjector's same-tx idempotency exactly: the side effect
// (ControlLog.AppendCommand) AND AppliedStore.MarkApplied run in ONE tx, so a
// re-delivered outbox event enqueues nothing the second time.
//
// SCOPE (e-i only — immediate wake): it handles ONLY WorkItems already in
// waiting_input. The busy-buffering + merge-on-next-waiting (read-cursor batch)
// path is the NEXT slice (e-ii) and is intentionally NOT built here.
//
// SELF-EXCLUSION: an agent never wakes itself — when the message sender is the
// agent owning the WorkItem (sender == "agent:<id>"), that agent is skipped.
// This is what keeps request_input (the agent's own question, sender=agent:<id>,
// posted in the same tx as the WaitInput) from immediately re-waking the asker.
type WakeProjector struct {
	db         *sql.DB
	workItems  agent.WorkItemRepository
	agents     agent.Repository
	controlLog *environment.ControlLog
	applied    outbox.AppliedStore
	clock      clock.Clock

	// D2-e-ii batch-flush deps (nil → the agent.awaiting_input branch degrades
	// to a no-op, like the e-i nil-ControlLog guard; the immediate path is
	// unaffected). convRepo resolves the task conversation, msgRepo reads its
	// messages + the cursor message's posted_at, readState reads the agent's
	// read-state cursor.
	convRepo  conversation.ConversationRepository
	msgRepo   conversation.MessageRepository
	readState conversation.UserConversationReadStateRepository

	// v2.7 #185 conversational-wake deps (nil → DM/channel→agent path is a
	// no-op, like the other optional deps). displayName resolves an agent
	// participant's identity_id → display_name for channel @mention matching;
	// systemNotify posts a system message into a conversation (the
	// "agent not running" signal when a DM/channel targets a stopped agent).
	displayName  func(ctx context.Context, identityID string) (string, bool)
	systemNotify func(ctx context.Context, conversationID, text string) error

	// v2.7.1 #224: resolves an issue/task conversation's owner_ref → the owning
	// project's AGENT member-ids (stripped of the "agent:" prefix), so an agent
	// that is a PROJECT MEMBER (not necessarily a conversation participant) is a
	// valid @mention wake target. nil → only explicit participants are candidates
	// (the pre-#224 behavior). Channel/DM owner_refs resolve to no project → empty.
	projectAgentMembers func(ctx context.Context, ownerRef string) ([]string, error)
}

// WakeProjectorDeps bundles the projector's dependencies.
type WakeProjectorDeps struct {
	DB         *sql.DB
	WorkItems  agent.WorkItemRepository
	Agents     agent.Repository
	ControlLog *environment.ControlLog
	Applied    outbox.AppliedStore
	Clock      clock.Clock

	// D2-e-ii batch-flush deps (optional; nil → awaiting_input branch no-op).
	ConvRepo  conversation.ConversationRepository
	MsgRepo   conversation.MessageRepository
	ReadState conversation.UserConversationReadStateRepository

	// v2.7 #185 conversational-wake deps (optional; nil → DM/channel→agent no-op).
	DisplayName  func(ctx context.Context, identityID string) (string, bool)
	SystemNotify func(ctx context.Context, conversationID, text string) error

	// v2.7.1 #224 (optional; nil → only conversation participants are @mention wake
	// candidates). owner_ref → owning project's agent member-ids.
	ProjectAgentMembers func(ctx context.Context, ownerRef string) ([]string, error)
}

// NewWakeProjector constructs the projector.
func NewWakeProjector(d WakeProjectorDeps) *WakeProjector {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WakeProjector{
		db:                  d.DB,
		workItems:           d.WorkItems,
		agents:              d.Agents,
		controlLog:          d.ControlLog,
		applied:             d.Applied,
		clock:               clk,
		convRepo:            d.ConvRepo,
		msgRepo:             d.MsgRepo,
		readState:           d.ReadState,
		displayName:         d.DisplayName,
		systemNotify:        d.SystemNotify,
		projectAgentMembers: d.ProjectAgentMembers,
	}
}

// Name is the AppliedStore key (its own namespace, separate from the other
// projectors consuming the outbox).
func (p *WakeProjector) Name() string { return "conv-agent-wake" }

// messageAddedPayload mirrors the JSON keys MessageWriter.AddMessage writes for
// the EvtConversationMessageAdded outbox event.
type messageAddedPayload struct {
	ConversationID string `json:"conversation_id"`
	OwnerRef       string `json:"owner_ref"`
	MessageID      string `json:"message_id"`
	Sender         string `json:"sender"`
	Text           string `json:"text"`
}

// wakeCommandPayload is the agent.wake command payload the daemon AgentController
// consumes to inject the message into the agent's running session.
//
// D2-e-ii: ConversationID is carried so the controller can advance the agent's
// read-state cursor after inject (mark-seen). MessageID is the NEWEST delivered
// message id (the cursor target); MessageText is a single message in the e-i
// immediate path, or the merged sender-labeled batch in the e-ii flush path.
type wakeCommandPayload struct {
	AgentID        string `json:"agent_id"`
	WorkItemID     string `json:"work_item_id"`
	TaskRef        string `json:"task_ref"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	MessageText    string `json:"message_text"`
}

// converseCommandPayload is the agent.converse command payload (v2.7 #185). It
// carries everything the daemon needs to inject a DM/channel message into the
// agent's running session and let the agent reply — WITHOUT a WorkItem. ConvKind
// is "dm"/"channel" so the injected brief reads naturally; SenderDisplay is the
// human's display name for the brief; ConversationID is where the agent replies
// (via the post_message MCP tool) and the cursor to advance after inject.
type converseCommandPayload struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	ConvKind       string `json:"conv_kind"`
	ConvName       string `json:"conv_name,omitempty"`
	SenderRef      string `json:"sender_ref"`
	SenderDisplay  string `json:"sender_display"`
	MessageID      string `json:"message_id"`
	MessageText    string `json:"message_text"`
}

// awaitingInputPayload mirrors the JSON keys the request_input admin handler
// writes for the EvtAgentAwaitingInput outbox event (the batch-flush trigger).
type awaitingInputPayload struct {
	AgentID        string `json:"agent_id"`
	WorkItemID     string `json:"work_item_id"`
	TaskRef        string `json:"task_ref"`
	ConversationID string `json:"conversation_id"`
}

// Project enqueues an agent.wake command for each waiting_input WorkItem on the
// task whose conversation received the message (OQ5 immediate wake).
//
//   - Only conversation.message_added events are handled (else no-op).
//   - owner_ref must be a task ref (pm://tasks/{id}); else no-op (defensive — the
//     producer already filters to task conversations).
//   - For each WorkItem on the task that is waiting_input: resolve the agent,
//     EXCLUDE the message's own sender (no self-wake), resolve the worker (skip +
//     log when unresolved / no worker binding), and enqueue agent.wake keyed by
//     "agent.wake:<workItemID>:<messageID>" so re-projection never double-enqueues.
func (p *WakeProjector) Project(ctx context.Context, e outbox.Event) error {
	switch e.EventType {
	case convservice.EvtConversationMessageAdded:
		return p.projectMessageAdded(ctx, e)
	case EvtAgentAwaitingInput:
		return p.projectAwaitingInput(ctx, e)
	default:
		return nil
	}
}

// projectMessageAdded is the e-i immediate-wake path: a message posted into a
// task conversation wakes every waiting_input WorkItem (self-excluded).
func (p *WakeProjector) projectMessageAdded(ctx context.Context, e outbox.Event) error {
	var pl messageAddedPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	// Task conversations → the WorkItem-keyed wake below. Everything else
	// (DM / Channel) → the v2.7 #185 conversational-wake path.
	if !strings.HasPrefix(pl.OwnerRef, ownerRefTasksPrefix) {
		return p.projectConversationMessage(ctx, e, pl)
	}
	taskRef := pl.OwnerRef
	// v2.7.1 #220: a task conversation is also a conversation — besides the WorkItem
	// request_input wake below, @mentioned agent participants get the conversational
	// wake (same applied-mark). Load the conv best-effort (nil → only WorkItem wake).
	var taskConv *conversation.Conversation
	if p.convRepo != nil {
		taskConv, _ = p.convRepo.FindByID(ctx, conversation.ConversationID(pl.ConversationID))
	}

	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		workItems, err := p.workItems.ListByTask(txCtx, taskRef)
		if err != nil {
			return err
		}
		for _, wi := range workItems {
			// e-i: ONLY immediate (already waiting_input) wake. Other statuses
			// (active/queued/terminal) are out of scope (active-buffering is e-ii).
			if wi.Status() != agent.WorkItemWaitingInput {
				continue
			}
			if err := p.enqueueWake(txCtx, wi, taskRef, pl); err != nil {
				return err
			}
		}
		// v2.7.1 #220: also wake @mentioned agent participants of the task conversation
		// (conversational path), human-only + loop-break (an agent/system message never
		// triggers it) — mirrors projectConversationMessage's sender gate.
		if taskConv != nil && strings.HasPrefix(pl.Sender, userParticipantPrefix) {
			if err := p.wakeConversationParticipants(txCtx, taskConv, pl); err != nil {
				return err
			}
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// enqueueWake appends an agent.wake command for one waiting_input WorkItem (same
// tx as the caller). Self-exclusion: when the message sender IS the agent that
// owns this WorkItem, no command is enqueued (an agent never wakes itself). When
// the agent can't be resolved or has no worker binding, it logs + skips rather
// than failing the projection (mirrors work_item_projector.enqueueWork).
func (p *WakeProjector) enqueueWake(ctx context.Context, wi *agent.AgentWorkItem, taskRef string, pl messageAddedPayload) error {
	agentID := wi.AgentID()

	if p.controlLog == nil || p.agents == nil {
		return nil // wake delivery not wired (e.g. test fixtures)
	}
	a, err := p.agents.FindByID(ctx, agentID)
	if err != nil {
		// Could not resolve the agent — skip the wake rather than stall the
		// projection (the WorkItem state is unaffected by skipping the signal).
		slog.Warn("wake projector: agent.wake enqueue skipped (agent lookup failed)",
			"agent_id", string(agentID), "work_item_id", wi.ID(), "err", err)
		return nil
	}

	// Self-exclusion: the agent's own message never wakes it (keeps
	// request_input's same-tx question from re-waking the asker). The sender ref
	// may be the entity id OR the identity-member id (#185 — task participants now
	// carry the member ref), so exclude on EITHER form.
	if pl.Sender == agentParticipantPrefix+string(agentID) ||
		(a.IdentityMemberID() != "" && pl.Sender == agentParticipantPrefix+a.IdentityMemberID()) {
		return nil
	}
	workerID := a.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("wake projector: agent.wake enqueue skipped (agent has no worker binding)",
			"agent_id", string(agentID), "work_item_id", wi.ID())
		return nil
	}
	payload, err := json.Marshal(wakeCommandPayload{
		AgentID:        string(agentID),
		WorkItemID:     wi.ID(),
		TaskRef:        taskRef,
		ConversationID: pl.ConversationID, // D2-e-ii backfill: cursor advance after inject.
		MessageID:      pl.MessageID,
		MessageText:    pl.Text,
	})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    commandTypeAgentWake,
		Payload:        string(payload),
		IdempotencyKey: "agent.wake:" + wi.ID() + ":" + pl.MessageID,
	})
	return err
}

// projectConversationMessage is the v2.7 #185 (FINDING-H) conversational-wake
// path: a HUMAN posts a message into a DM or Channel where an agent is a
// participant. DM → wake the agent participant(s); Channel → wake the agent
// participants whose display_name is @mentioned. A running agent gets an
// agent.converse command (non-WorkItem inject); a stopped agent gets a visible
// "not running" system message (no silent black hole). LOOP-BREAK: only a human
// (user:) sender triggers this, so an agent's own reply never wakes any agent.
func (p *WakeProjector) projectConversationMessage(ctx context.Context, e outbox.Event, pl messageAddedPayload) error {
	if p.controlLog == nil || p.agents == nil || p.convRepo == nil {
		return nil // conversational-wake deps not wired → clean no-op
	}
	// LOOP-BREAK + human-only: only a user: sender wakes agents. An agent reply
	// (agent:) or a system message never triggers — structurally no ping-pong.
	if !strings.HasPrefix(pl.Sender, userParticipantPrefix) {
		return nil
	}
	conv, err := p.convRepo.FindByID(ctx, conversation.ConversationID(pl.ConversationID))
	if err != nil {
		return nil // conversation gone/unreadable → nothing to wake (don't fail)
	}
	kind := conv.Kind()
	// v2.7.1 #220: DM / Channel / Issue are handled here (conversational @mention
	// wake). TASK is handled in projectMessageAdded — it ALSO runs the WorkItem
	// request_input wake, so both wakes share one applied-mark there (the applied
	// idempotency key is (projector, event), so they cannot run as two separate
	// passes). Other kinds: ignore.
	if kind != conversation.ConversationKindDM &&
		kind != conversation.ConversationKindChannel &&
		kind != conversation.ConversationKindIssue {
		return nil
	}

	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if err := p.wakeConversationParticipants(txCtx, conv, pl); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// wakeConversationParticipants wakes each active agent participant of conv that the
// kind's @mention policy selects (v2.7 #185 + v2.7.1 #220): a DM wakes its single
// peer directly; group-like kinds (channel / issue / task) wake ONLY agents
// explicitly @mentioned by display_name. The caller owns the tx + applied
// idempotency — shared by projectConversationMessage (DM/Channel/Issue) and the
// task branch of projectMessageAdded (which also runs the WorkItem wake under the
// same applied-mark).
func (p *WakeProjector) wakeConversationParticipants(ctx context.Context, conv *conversation.Conversation, pl messageAddedPayload) error {
	kind := conv.Kind()

	// Candidate agent rawIDs = active agent PARTICIPANTS + (v2.7.1 #224, issue/task
	// only) the owning project's agent MEMBERS — so an agent that is a project member
	// is a valid @mention wake target even when not an explicit participant.
	var rawIDs []string
	for _, part := range conv.Participants() {
		if part.IsActive() && strings.HasPrefix(string(part.IdentityID), agentParticipantPrefix) {
			rawIDs = append(rawIDs, strings.TrimPrefix(string(part.IdentityID), agentParticipantPrefix))
		}
	}
	if p.projectAgentMembers != nil &&
		(kind == conversation.ConversationKindIssue || kind == conversation.ConversationKindTask) {
		if memberIDs, err := p.projectAgentMembers(ctx, pl.OwnerRef); err != nil {
			slog.Warn("wake projector: project-member lookup failed",
				"owner_ref", pl.OwnerRef, "conversation_id", pl.ConversationID, "err", err)
		} else {
			rawIDs = append(rawIDs, memberIDs...)
		}
	}

	// Resolve + @mention-gate + deliver, deduped by the resolved execution-entity id
	// (an agent may be BOTH a participant and a project member — wake it once).
	delivered := map[agent.AgentID]bool{}
	for _, rawID := range rawIDs {
		// FINDING-J: the ref may carry EITHER the execution-entity id OR the
		// identity-member id ("agent-<ulid>", #157). Resolve tolerantly so both the
		// @mention lookup (display_name on the member) and the deliver path (worker
		// binding on the entity) work regardless of which id the ref holds.
		a, ok := p.resolveAgent(ctx, rawID)
		if !ok {
			slog.Warn("wake projector: agent.converse skipped (agent candidate unresolved)",
				"raw_id", rawID, "conversation_id", pl.ConversationID)
			continue
		}
		if delivered[a.ID()] {
			continue
		}
		// Group-like kinds (channel/issue/task): only wake agents explicitly
		// @mentioned by display_name. DM (1:1): wake the peer directly.
		if kind != conversation.ConversationKindDM && !p.mentionsAgent(ctx, a, rawID, pl.Text) {
			continue
		}
		if err := p.deliverConverse(ctx, conv, a, rawID, pl); err != nil {
			return err
		}
		delivered[a.ID()] = true
	}
	return nil
}

// resolveAgent resolves an agent participant's stripped ref (the part after
// "agent:") to the execution-entity Agent, tolerating EITHER id form (#185
// FINDING-J): it tries the execution-entity id first, then the identity-member
// id ("agent-<ulid>", #157). Returns false when neither resolves.
func (p *WakeProjector) resolveAgent(ctx context.Context, rawID string) (*agent.Agent, bool) {
	a, err := p.agents.FindByID(ctx, agent.AgentID(rawID))
	if err == nil {
		return a, true
	}
	if !errors.Is(err, agent.ErrAgentNotFound) {
		// A real lookup error (not "absent") — log + fall through to the
		// identity-member branch rather than masking it.
		slog.Warn("wake projector: agent FindByID failed", "raw_id", rawID, "err", err)
	}
	if a, err := p.agents.FindByIdentityMemberID(ctx, rawID); err == nil {
		return a, true
	}
	return nil, false
}

// mentionsAgent reports whether text @mentions the agent's display_name
// (case-insensitive, token-bounded so @Bot ≠ @Bottom). The name is resolved via
// agentDisplayName (identity display_name preferred, profile name fallback); when
// only the raw id is available there is nothing meaningful to @mention, so it
// returns false. Channel gating (#185 / FINDING-J).
func (p *WakeProjector) mentionsAgent(ctx context.Context, a *agent.Agent, rawID, text string) bool {
	name, ok := p.agentDisplayName(ctx, a, rawID)
	if !ok || strings.TrimSpace(name) == "" {
		return false
	}
	return mentionTokenPresent(strings.ToLower(text), "@"+strings.ToLower(strings.TrimSpace(name)))
}

// agentDisplayName resolves an agent's user-facing name for @mention matching and
// system-message rendering (#185 / FINDING-J + Rule 2 — never show a raw id when a
// name exists). Preference order:
//  1. identity display_name via the identity-member id (#157, the canonical name)
//     — this is the (a) fix: it resolves the name even when the participant ref
//     carried the EXECUTION-ENTITY id (DisplayName can't resolve an entity id);
//  2. identity display_name via the raw participant ref (ref already a member id);
//  3. the agent's profile name (standalone execution agents with no member);
//  4. the raw id (last resort, avoids empty) — ok=false signals this fallback.
func (p *WakeProjector) agentDisplayName(ctx context.Context, a *agent.Agent, rawID string) (string, bool) {
	if m := strings.TrimSpace(a.IdentityMemberID()); m != "" {
		if n, ok := p.lookupDisplayName(ctx, agentParticipantPrefix+m); ok {
			return n, true
		}
	}
	if n, ok := p.lookupDisplayName(ctx, agentParticipantPrefix+rawID); ok {
		return n, true
	}
	if pn := strings.TrimSpace(a.Profile().Name); pn != "" {
		return pn, true
	}
	return rawID, false
}

// lookupDisplayName safely calls the optional displayName resolver, returning the
// trimmed name and ok=false when the resolver is unwired or yields nothing.
func (p *WakeProjector) lookupDisplayName(ctx context.Context, ref string) (string, bool) {
	if p.displayName == nil {
		return "", false
	}
	n, ok := p.displayName(ctx, ref)
	if !ok || strings.TrimSpace(n) == "" {
		return "", false
	}
	return strings.TrimSpace(n), true
}

// mentionTokenPresent reports whether needle ("@name", lowercased) appears in
// text (lowercased) as a bounded token: the char after the match must be end-of
// -string or a non-word char, so "@bot" does not match "@bottom". The leading
// "@" is itself the start boundary.
func mentionTokenPresent(text, needle string) bool {
	from := 0
	for {
		i := strings.Index(text[from:], needle)
		if i < 0 {
			return false
		}
		end := from + i + len(needle)
		if end >= len(text) || !isMentionWordChar(text[end]) {
			return true
		}
		from = from + i + 1
	}
}

func isMentionWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}

// deliverConverse enqueues an agent.converse command for a RUNNING agent, or
// posts a visible "not running" system message for a stopped one (avoid the
// silent black hole). The agent is already resolved by the caller (resolveAgent,
// FINDING-J); rawID is the participant ref's stripped id used only as the
// display-name fallback. The enqueued AgentID is the EXECUTION-ENTITY id
// (a.ID()) — the worker daemon keys its running sessions by it, so an
// identity-member ref must NOT leak into the command. Runs in the caller's tx.
func (p *WakeProjector) deliverConverse(ctx context.Context, conv *conversation.Conversation, a *agent.Agent, rawID string, pl messageAddedPayload) error {
	entityID := string(a.ID())
	if a.Lifecycle() != agent.LifecycleRunning {
		// Visible signal instead of silence (#185 Tester refinement). The name
		// resolves via agentDisplayName so the notice reads "@AgentBeta", not the
		// raw entity id, even when the participant ref carried the entity id
		// (FINDING-J / Rule 2).
		if p.systemNotify != nil {
			name, _ := p.agentDisplayName(ctx, a, rawID)
			return p.systemNotify(ctx, pl.ConversationID,
				"@"+name+" is not running and won't reply until it is started.")
		}
		return nil
	}
	workerID := a.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("wake projector: agent.converse skipped (agent has no worker binding)",
			"agent_id", entityID)
		return nil
	}
	senderDisplay, _ := p.displayNameOr(ctx, pl.Sender, pl.Sender)
	payload, err := json.Marshal(converseCommandPayload{
		AgentID:        entityID,
		ConversationID: pl.ConversationID,
		ConvKind:       string(conv.Kind()),
		ConvName:       conv.Name(),
		SenderRef:      pl.Sender,
		SenderDisplay:  senderDisplay,
		MessageID:      pl.MessageID,
		MessageText:    pl.Text,
	})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    commandTypeAgentConverse,
		Payload:        string(payload),
		IdempotencyKey: "agent.converse:" + pl.ConversationID + ":" + pl.MessageID + ":" + entityID,
	})
	return err
}

// displayNameOr resolves identityID → display_name, falling back to the given
// string when no resolver is wired or no name is found.
func (p *WakeProjector) displayNameOr(ctx context.Context, identityID, fallback string) (string, bool) {
	if p.displayName == nil {
		return fallback, false
	}
	if n, ok := p.displayName(ctx, identityID); ok && strings.TrimSpace(n) != "" {
		return n, true
	}
	return fallback, false
}

// projectAwaitingInput is the D2-e-ii batch-flush path: when an agent ENTERS
// waiting_input (request_input emitted agent.awaiting_input in the same tx as the
// WaitInput), deliver ALL of the agent's UNREAD messages in the task conversation
// (since its read-state cursor) as ONE merged, sender-labeled stdin injection.
//
// Same-tx idempotent (IsApplied/MarkApplied in one tx), mirroring the e-i path.
// Steps:
//   - compute the agent's cursor (read-state LastSeenMessageID; empty if absent);
//   - read the conversation messages with posted_at >= cursor's posted_at, then
//     filter to ULID strictly > cursor (same-millisecond tie-safe) + self-exclude
//     the agent's own messages;
//   - no unread → MarkApplied + no wake;
//   - re-check the WorkItem is STILL waiting_input (it may have been woken by an
//     interleaved e-i message); merge unread → one agent.wake keyed
//     "agent.wake:{wi}:batch:{lastMessageID}".
func (p *WakeProjector) projectAwaitingInput(ctx context.Context, e outbox.Event) error {
	var pl awaitingInputPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if err := p.flushAwaitingInput(txCtx, pl); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// flushAwaitingInput does the body of the batch flush inside the caller's tx.
// It resolves the conversation id from the event payload and delegates the
// recompute+enqueue core to the shared flushUnread (so the push e-ii path and
// the D2-e-iii poll-fallback loop produce the SAME enqueue + the SAME batch key).
// Returns nil (no-op, still MarkApplied) on any "nothing to do" condition.
func (p *WakeProjector) flushAwaitingInput(ctx context.Context, pl awaitingInputPayload) error {
	return p.flushUnread(ctx, pl.AgentID, pl.WorkItemID, pl.TaskRef, pl.ConversationID)
}

// flushUnread recomputes the agent's UNREAD qualifying messages in its task
// conversation from the read-state cursor and enqueues ONE merged agent.wake.
//
// tx-agnostic: it uses ExecutorFromCtx (via the repos) for the read-state read,
// message scan, and ControlLog enqueue — the CALLER provides the tx (the
// projector wraps it with IsApplied/MarkApplied; the loop wraps each WorkItem in
// its own RunInTx).
//
// IDENTICAL semantics + IDENTICAL batch key as the e-ii push path (so push/poll
// converge: a push-delivered batch advanced the cursor → no unread here; a
// push-enqueued-but-unconsumed batch → same key → ControlLog dedups → never
// double).
//
// Returns nil (no-op) on any "nothing to do" condition: deps not wired, no
// conversation id, no unread, WorkItem no longer waiting_input, agent
// unresolved / no worker.
func (p *WakeProjector) flushUnread(ctx context.Context, agentID, workItemID, taskRef, convID string) error {
	if p.controlLog == nil || p.agents == nil || p.msgRepo == nil || p.readState == nil {
		return nil // batch delivery not wired (e.g. test fixtures / e-i-only build)
	}
	agentID = strings.TrimSpace(agentID)
	convID = strings.TrimSpace(convID)
	workItemID = strings.TrimSpace(workItemID)
	if agentID == "" || convID == "" || workItemID == "" {
		return nil
	}
	conversationID := conversation.ConversationID(convID)
	participant := conversation.IdentityRef(agentParticipantPrefix + agentID)

	// (a) resolve the agent's read-state cursor (empty = never seen → all unread).
	var cursorID conversation.MessageID
	rs, err := p.readState.FindByUserAndConv(ctx, participant, conversationID)
	if err != nil && !errors.Is(err, conversation.ErrReadStateNotFound) {
		return err
	}
	if rs != nil {
		cursorID = rs.LastSeenMessageID
	}

	// (b) resolve the cursor message's posted_at to bound the Since scan. A cursor
	// pointing at a since-deleted/absent message degrades to Since=nil (full scan);
	// the strictly-after-ULID filter below still excludes already-seen ids.
	filter := conversation.MessageFilter{}
	if cursorID != "" {
		cm, ferr := p.msgRepo.FindByID(ctx, cursorID)
		if ferr != nil && !errors.Is(ferr, conversation.ErrMessageNotFound) {
			return ferr
		}
		if cm != nil {
			since := cm.PostedAt()
			filter.Since = &since
		}
	}

	msgs, err := p.msgRepo.FindByConversationID(ctx, conversationID, filter)
	if err != nil {
		return err
	}

	// (c) filter to UNREAD (ULID strictly > cursor; all when cursor empty) and
	// self-exclude the agent's own messages. Sort by posted_at ASC for a stable
	// merge order (the repo already returns ASC, but be defensive on ties).
	selfSender := agentParticipantPrefix + agentID
	var unread []*conversation.Message
	for _, m := range msgs {
		if cursorID != "" && string(m.ID()) <= string(cursorID) {
			continue
		}
		if string(m.SenderIdentityID()) == selfSender {
			continue
		}
		unread = append(unread, m)
	}
	if len(unread) == 0 {
		return nil // nothing unread → no wake (still MarkApplied by the caller).
	}
	sort.SliceStable(unread, func(i, j int) bool {
		if unread[i].PostedAt().Equal(unread[j].PostedAt()) {
			return string(unread[i].ID()) < string(unread[j].ID())
		}
		return unread[i].PostedAt().Before(unread[j].PostedAt())
	})

	// (d) re-check the WorkItem is STILL waiting_input (an interleaved e-i message
	// may have already woken it). Skip if not found / not waiting_input.
	wi, err := p.resolveWaitingWorkItem(ctx, taskRef, workItemID)
	if err != nil {
		return err
	}
	if wi == nil {
		return nil
	}

	// (e) resolve the agent → worker (skip + log if unresolved / no binding).
	a, err := p.agents.FindByID(ctx, agent.AgentID(agentID))
	if err != nil {
		slog.Warn("wake projector: batch flush skipped (agent lookup failed)",
			"agent_id", agentID, "work_item_id", workItemID, "err", err)
		return nil
	}
	workerID := a.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("wake projector: batch flush skipped (agent has no worker binding)",
			"agent_id", agentID, "work_item_id", workItemID)
		return nil
	}

	// (f) merge into ONE sender-labeled text; the newest id is the cursor target.
	mergedText := mergeMessages(unread)
	lastID := string(unread[len(unread)-1].ID())

	payload, err := json.Marshal(wakeCommandPayload{
		AgentID:        agentID,
		WorkItemID:     workItemID,
		TaskRef:        taskRef,
		ConversationID: convID,
		MessageID:      lastID,
		MessageText:    mergedText,
	})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    commandTypeAgentWake,
		Payload:        string(payload),
		IdempotencyKey: "agent.wake:" + workItemID + ":batch:" + lastID,
	})
	return err
}

// resolveWaitingWorkItem returns the WorkItem named by workItemID on taskRef IFF
// it is still waiting_input; nil otherwise (already woken / superseded / not
// found).
func (p *WakeProjector) resolveWaitingWorkItem(ctx context.Context, taskRef, workItemID string) (*agent.AgentWorkItem, error) {
	items, err := p.workItems.ListByTask(ctx, taskRef)
	if err != nil {
		return nil, err
	}
	for _, wi := range items {
		if wi.ID() != workItemID {
			continue
		}
		if wi.Status() != agent.WorkItemWaitingInput {
			return nil, nil
		}
		return wi, nil
	}
	return nil, nil
}

// mergeMessages renders the unread batch as ONE plain-text injection, each
// message sender-labeled on its own line(s): "[<sender>] <content>". Plain text
// (NOT structured JSON) — claude reads it as the human/peer turn. Order is the
// caller's (posted_at ASC).
func mergeMessages(msgs []*conversation.Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteByte('[')
		b.WriteString(string(m.SenderIdentityID()))
		b.WriteString("] ")
		b.WriteString(m.Content())
	}
	return b.String()
}

// ReconcileOnce is the poll-fallback sweep (D2-e-iii): for every waiting_input
// AgentWorkItem, recompute unread from the cursor and enqueue any pending batch —
// independent of whether an awaiting_input/message_added event ever fired
// (self-heals a never-enqueued silent bug). Each WorkItem runs in its OWN
// RunInTx; a per-item error is logged and the sweep continues (one bad item never
// stalls the rest).
//
// No AppliedStore here (the loop is not outbox-driven; idempotency comes from the
// batch key + cursor — same key as the push path → ControlLog dedups). If the
// batch-flush deps (convRepo/msgRepo/readState/controlLog/agents) are nil (test
// fixtures / e-i-only build) it no-ops gracefully.
func (p *WakeProjector) ReconcileOnce(ctx context.Context) error {
	if p.workItems == nil || p.convRepo == nil || p.msgRepo == nil ||
		p.readState == nil || p.controlLog == nil || p.agents == nil {
		return nil // poll fallback not wired
	}
	items, err := p.workItems.ListByStatus(ctx, agent.WorkItemWaitingInput)
	if err != nil {
		return err
	}
	for _, wi := range items {
		taskID := strings.TrimPrefix(wi.TaskRef(), ownerRefTasksPrefix)
		if taskID == wi.TaskRef() || strings.TrimSpace(taskID) == "" {
			// Not a pm://tasks/{id} ref (or empty id) — skip + log, continue.
			slog.Info("wake reconcile: skip WorkItem (task_ref not a task ref)",
				"work_item_id", wi.ID(), "task_ref", wi.TaskRef())
			continue
		}
		conv, err := p.convRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(taskID))
		if err != nil {
			// No conversation for this task (or lookup failed) — skip + log, the
			// sweep continues (one unresolvable item never stalls the rest).
			slog.Info("wake reconcile: skip WorkItem (conversation unresolved)",
				"work_item_id", wi.ID(), "task_ref", wi.TaskRef(), "err", err)
			continue
		}
		convID := string(conv.ID())
		agentID := string(wi.AgentID())
		workItemID := wi.ID()
		taskRef := wi.TaskRef()
		if err := persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
			return p.flushUnread(txCtx, agentID, workItemID, taskRef, convID)
		}); err != nil {
			slog.Warn("wake reconcile: flushUnread failed (sweep continues)",
				"work_item_id", workItemID, "agent_id", agentID, "err", err)
			continue
		}
	}
	return nil
}

var _ outbox.Projector = (*WakeProjector)(nil)
