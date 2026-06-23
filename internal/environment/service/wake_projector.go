package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/mention"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

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

// v2.14.0 F7 (issue I14): EvtAgentAwaitingInput + the D2-e-ii batch-flush /
// awaiting_input wake path (and the WorkItem-keyed task wake) removed —
// AgentWorkItem retired. request_input now blocks the Task (input_required) via
// the PM F6 path; the wake projector keeps only the conversation @mention wakes.

// ownerRefTasksPrefix is the task-owned conversation owner_ref scheme.
const ownerRefTasksPrefix = "pm://tasks/"

// ownerRefPlansPrefix is the plan-owned conversation owner_ref scheme (T250 —
// used by the plan-failure creator wake to build a plan owner_ref; live
// owner-title resolution otherwise goes through conversation.ResolveOwnerContext).
const ownerRefPlansPrefix = "pm://plans/"

// agentParticipantPrefix is the IdentityRef scheme for an agent participant's
// read-state cursor (the read_state repo is keyed by IdentityRef, so "agent:<id>"
// resolves the agent's own cursor in the task conversation).
const agentParticipantPrefix = "agent:"

// WakeProjector turns a `conversation.message_added` outbox event into
// `agent.converse` / `agent.wake` control commands for the conversation's
// @mentioned (or DM peer) agent participants (v2.7 #185 / #220). It mirrors the
// same-tx idempotency exactly: the side effect (ControlLog.AppendCommand) AND
// AppliedStore.MarkApplied run in ONE tx, so a re-delivered outbox event
// enqueues nothing the second time.
//
// v2.14.0 F7 (issue I14): the WorkItem-keyed task wake (per waiting_input
// AgentWorkItem) and the D2-e-ii awaiting_input batch flush were removed —
// AgentWorkItem retired. A task conversation now only drives the @mention/
// participant conversational wake (shared with DM/channel/issue/plan).
type WakeProjector struct {
	db         *sql.DB
	agents     agent.Repository
	controlLog *environment.ControlLog
	applied    outbox.AppliedStore
	clock      clock.Clock

	// Conversation-read deps for the conversational wake (nil → the participant
	// wake degrades to a no-op). convRepo resolves the conversation; msgRepo /
	// readState remain wired for parity with the deps struct but the batch-flush
	// path that used them was removed (F7).
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

	// T250: resolves a plan_id → the plan's human name, used to label a plan-chat
	// converse brief ("this conversation belongs to plan ⟨name⟩(plan_id)"). nil or a
	// miss → the brief falls back to the plan_id alone (still disambiguates). Resolved
	// live at wake time (not denormalized onto the conversation) so a renamed plan
	// always reads correctly — matching the task/issue convention of resolving the
	// owning entity's title rather than copying it onto the conversation.
	planName func(ctx context.Context, planID string) (string, bool)

	// T255 (I19/OQ2): issue/task title live resolvers, mirroring planName. An
	// issue/task chat carries owner_ref pm://issues|tasks/{id}; resolveOwnerName
	// dispatches on the OwnerContext kind (T254 table) so the converse brief header
	// shows the real title — resolved live (a renamed issue/task reads correctly), a
	// miss falls back to conv.Name() then the id (a name miss never blocks a wake).
	// No projectName field: project chat has NO converse wake path today (OQ1 — there
	// is no ConversationKindProject and pm://projects/ is only a channel soft-label),
	// so a project-title resolver would be dead wiring. See resolveOwnerName's project
	// case for the TODO if project chats ever gain a wake path.
	issueTitle func(ctx context.Context, issueID string) (string, bool)
	taskTitle  func(ctx context.Context, taskID string) (string, bool)

	// I7-D1/T227: the wake-chain circuit breaker. nil → agent→agent wakes are NOT
	// gated (pre-T227 behavior). When set, an agent-sender wake is run through the
	// four gates (depth/cycle/rate/cost) before delivery; human/system senders
	// always bypass. wakeGuard holds the rate/cycle runtime state, so it MUST be a
	// process singleton shared across deliveries (wired once in app composition).
	wakeGuard *wakeguard.Guard
}

// WakeProjectorDeps bundles the projector's dependencies.
type WakeProjectorDeps struct {
	DB         *sql.DB
	Agents     agent.Repository
	ControlLog *environment.ControlLog
	Applied    outbox.AppliedStore
	Clock      clock.Clock

	// Conversation-read deps for the conversational wake (optional).
	ConvRepo  conversation.ConversationRepository
	MsgRepo   conversation.MessageRepository
	ReadState conversation.UserConversationReadStateRepository

	// v2.7 #185 conversational-wake deps (optional; nil → DM/channel→agent no-op).
	DisplayName  func(ctx context.Context, identityID string) (string, bool)
	SystemNotify func(ctx context.Context, conversationID, text string) error

	// v2.7.1 #224 (optional; nil → only conversation participants are @mention wake
	// candidates). owner_ref → owning project's agent member-ids.
	ProjectAgentMembers func(ctx context.Context, ownerRef string) ([]string, error)

	// T250 (optional; nil → plan-chat brief falls back to plan_id only). plan_id →
	// the plan's name, for labeling a plan-chat converse brief.
	PlanName func(ctx context.Context, planID string) (string, bool)

	// T255 (optional; nil → issue/task brief falls back to conv.Name()/id). Live
	// title resolvers for issue/task chats, mirroring PlanName.
	IssueTitle func(ctx context.Context, issueID string) (string, bool)
	TaskTitle  func(ctx context.Context, taskID string) (string, bool)

	// I7-D1/T227 wake-chain circuit breaker (optional; nil → agent→agent wakes
	// ungated). A process singleton (holds rate/cycle state) built from the
	// settings-driven Config in app composition.
	WakeGuard *wakeguard.Guard
}

// NewWakeProjector constructs the projector.
func NewWakeProjector(d WakeProjectorDeps) *WakeProjector {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WakeProjector{
		db:                  d.DB,
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
		planName:            d.PlanName,
		issueTitle:          d.IssueTitle,
		taskTitle:           d.TaskTitle,
		wakeGuard:           d.WakeGuard,
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
	// RootMessageID (v2.9.1 Thread F4) is the thread root of the triggering message
	// (empty if top-level); carried through to the agent so its reply lands in-thread.
	RootMessageID string `json:"root_message_id,omitempty"`
	// AttachmentCount (v2.10.0 [T74]) — how many attachments the message carries.
	AttachmentCount int `json:"attachment_count,omitempty"`
	// Attachments (v2.10.1 [T103]) — the inbound attachments' file_uri + metadata,
	// rendered INLINE into the woken agent's brief so it can download_file directly
	// (the push wake also advances the read cursor → a later get_my_unread is empty,
	// so the uri MUST ride the push). Mirrors the producer JSON keys (BC boundary:
	// env mirrors the keys, it does not import the conversation struct).
	Attachments []wakeAttachment `json:"attachments,omitempty"`
}

// wakeAttachment mirrors the conversation MessageAttachment JSON (uri/filename/
// mime_type/size) carried on the message-added event (v2.10.1 [T103]).
type wakeAttachment struct {
	URI      string `json:"uri"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
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
	// RootMessageID (F4): thread root of the triggering message → agent replies in-thread.
	RootMessageID string `json:"root_message_id,omitempty"`
	// AttachmentCount (v2.10.0 [T74]) → the brief tells the agent about file(s).
	AttachmentCount int `json:"attachment_count,omitempty"`
	// OwnerRef (T250) is the source conversation's owner_ref (pm://plans/{id} for a
	// plan chat, empty for a DM). It rides the converse command so the daemon brief
	// can tell the agent WHICH plan a plan-chat message belongs to — without it the
	// brief only carries conversation_id and the agent cannot disambiguate "this
	// plan" across concurrent plan chats. For a plan conversation ConvName carries
	// the resolved plan name (the conversation itself has no name).
	OwnerRef string `json:"owner_ref,omitempty"`
}

// planCreatorFailureWakePayload mirrors the JSON the PM Service writes for the
// EvtPlanCreatorFailureWake outbox event (v2.9 P3 failure→agent-creator-wake). It
// is the env-side copy of pmservice.planCreatorFailureWakePayload (that type is
// unexported; mirroring its keys keeps the BC boundary clean — env consumes the
// event, it does not depend on PM's struct). CreatorRef is the agent ref
// ("agent:<id>"); ConversationID is the plan conversation; MessageID is the failure
// @mention id (the converse idempotency anchor).
type planCreatorFailureWakePayload struct {
	CreatorRef     string `json:"creator_ref"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	PlanID         string `json:"plan_id"`
	TaskID         string `json:"task_id"`
	OrganizationID string `json:"organization_id"`
}

// Project enqueues an agent.converse / agent.wake command for the conversation's
// @mentioned (or DM peer) agent participants when a message is added (v2.7 #185 /
// #220), and handles the plan-creator failure wake.
//
//   - Only conversation.message_added events are handled (else no-op).
//   - The conversational wake path applies to every conversation kind
//     (DM/channel/issue/task/plan) — see projectConversationMessage.
func (p *WakeProjector) Project(ctx context.Context, e outbox.Event) error {
	switch e.EventType {
	case convservice.EvtConversationMessageAdded:
		return p.projectMessageAdded(ctx, e)
	case pmservice.EvtPlanCreatorFailureWake:
		return p.projectPlanCreatorWake(ctx, e)
	default:
		return nil
	}
}

// projectMessageAdded routes a message-added event to the conversational wake
// path. v2.14.0 F7 (issue I14): the WorkItem-keyed task wake was removed —
// AgentWorkItem retired; a task conversation now only drives the @mention/
// participant wake (shared with DM/channel/issue/plan).
func (p *WakeProjector) projectMessageAdded(ctx context.Context, e outbox.Event) error {
	var pl messageAddedPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	return p.projectConversationMessage(ctx, e, pl)
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
	// LOOP-BREAK + sender gate. A human (user:) sender always wakes — human intent
	// must deliver. An AGENT (agent:) sender wakes ONLY in a DM (1:1 agent↔agent),
	// and every such wake runs through the wake-chain four-gate guard in
	// wakeConversationParticipants so an A↔B ping-pong self-extinguishes (T289: the
	// send side shipped in T291 but the wake side was never plumbed — without this an
	// agent's DM never wakes its peer). A system message never wakes (no agent storm).
	isHumanSender := strings.HasPrefix(pl.Sender, userParticipantPrefix)
	isAgentSender := strings.HasPrefix(pl.Sender, agentParticipantPrefix)
	if !isHumanSender && !isAgentSender {
		return nil
	}
	conv, err := p.convRepo.FindByID(ctx, conversation.ConversationID(pl.ConversationID))
	if err != nil {
		return nil // conversation gone/unreadable → nothing to wake (don't fail)
	}
	kind := conv.Kind()
	// T289 + T333: an agent sender wakes only when the wake-chain guard is wired —
	// the four-gate circuit breaker (depth/cycle/rate/cost, evaluated per hop in
	// wakeConversationParticipants) is what makes an agent→agent wake safe, so
	// without it we keep the #185 human-only loop-break rather than open an
	// unprotected ping-pong. T289 opened this to DMs only; T333 extends it to the
	// group-like kinds (channel/issue/plan/task) too, where the @mention gate (only
	// an explicit @display_name wakes a target) + self-exclusion still apply, and
	// @all stays human-only (broadcastAll gates on a user: sender). A system sender
	// never reaches this branch.
	if isAgentSender && p.wakeGuard == nil {
		return nil
	}
	// v2.7.1 #220: DM / Channel / Issue handled here (conversational @mention wake).
	// v2.9: PLAN conversations too — a human @mentioning a plan-conversation
	// participant agent (creator/assignee, joined via #284) must wake it, exactly
	// like DM/Channel.
	// v2.14.0 F7 (issue I14): TASK conversations are now handled here as well — the
	// WorkItem request_input wake that used to run in projectMessageAdded was
	// removed (AgentWorkItem retired), so a task chat is treated like any other
	// @mention/participant conversational wake. Other kinds: ignore.
	if kind != conversation.ConversationKindDM &&
		kind != conversation.ConversationKindChannel &&
		kind != conversation.ConversationKindIssue &&
		kind != conversation.ConversationKindTask &&
		kind != conversation.ConversationKindPlan {
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
	// participantEntity tracks which resolved agents are ALREADY active participants
	// (in either ref form) so the #227 auto-join below doesn't double-join one.
	participantEntity := map[agent.AgentID]bool{}
	var rawIDs []string
	for _, part := range conv.Participants() {
		if !part.IsActive() || !strings.HasPrefix(string(part.IdentityID), agentParticipantPrefix) {
			continue
		}
		r := strings.TrimPrefix(string(part.IdentityID), agentParticipantPrefix)
		rawIDs = append(rawIDs, r)
		if a, ok := p.resolveAgent(ctx, r); ok {
			participantEntity[a.ID()] = true
		}
	}
	if p.projectAgentMembers != nil &&
		(kind == conversation.ConversationKindIssue || kind == conversation.ConversationKindTask ||
			kind == conversation.ConversationKindPlan) {
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
	var toJoin []conversation.ParticipantElement // v2.7.1 #227 auto-join batch
	joinedAt := p.clock.Now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	// @all broadcast (per @oopslink): a HUMAN message that @all-mentions wakes EVERY
	// candidate agent (skip the per-agent @display_name gate below). @all is
	// HUMAN-ONLY: T333 opened agent→agent @mention wake to the group kinds, but an
	// agent writing @all must still wake no one (an agent cannot broadcast-storm the
	// room), so gate broadcastAll on a human (user:) sender. An agent sender falls
	// through to the per-agent @display_name gate — only an explicit @name (never
	// @all) wakes a peer, and each such hop still runs through the wake-chain guard.
	broadcastAll := strings.HasPrefix(pl.Sender, userParticipantPrefix) && mention.MentionsAll(pl.Text)
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
		// @mentioned by display_name — OR every agent when the message @all-broadcasts.
		// DM (1:1): wake the peer directly.
		if kind != conversation.ConversationKindDM && !broadcastAll && !p.mentionsAgent(ctx, a, rawID, pl.Text) {
			continue
		}
		// T289: an AGENT sender reaches here only via a DM (projectConversationMessage
		// gates agent senders to DM kind). Such a wake must (a) never wake the sender
		// itself and (b) pass the wake-chain four-gate guard (depth/cycle/rate/cost) so
		// an A↔B ping-pong self-extinguishes. Human/system senders are unaffected.
		if from, isAgentSender := strings.CutPrefix(pl.Sender, agentParticipantPrefix); isAgentSender {
			if string(a.ID()) == from || a.IdentityMemberID() == from {
				continue // self-exclusion: never wake the sender on its own message
			}
			if p.wakeGuard != nil {
				rootMsg := pl.RootMessageID
				if rootMsg == "" {
					rootMsg = pl.MessageID
				}
				tr := p.wakeGuard.EvaluateHop(from, string(a.ID()), rootMsg, p.clock.Now())
				if !tr.Allowed {
					slog.Info("wake projector: agent→agent DM wake suppressed by wake-chain guard",
						"from", from, "to", string(a.ID()), "gate", string(tr.Gate),
						"depth", tr.Depth, "reason", tr.Reason,
						"conversation_id", pl.ConversationID, "message_id", pl.MessageID)
					continue
				}
			}
		}
		// v2.7.1 #227: a woken agent that is NOT yet an active participant (a project
		// member, #224) is auto-joined as a participant so the DOWNSTREAM gates that
		// require participancy pass: the emit gate (future messages) + the post gate
		// (agent_tools_write.go agentIsActiveParticipant — else its reply 403s). Single
		// source of truth: the agent becomes a real participant, all gates use the
		// standard path. Idempotent (deduped by entity).
		if !participantEntity[a.ID()] {
			toJoin = append(toJoin, conversation.ParticipantElement{
				IdentityID: conversation.IdentityRef(agentParticipantPrefix + rawID),
				Role:       "member", JoinedAt: joinedAt, JoinedBy: conversation.IdentityRef("system"),
			})
			participantEntity[a.ID()] = true
		}
		if err := p.deliverConverse(ctx, conv, a, rawID, pl); err != nil {
			return err
		}
		delivered[a.ID()] = true
	}
	// v2.7.1 #227: persist the auto-joins once (one UpdateParticipants = one version
	// bump). Same tx as deliver + the caller's applied-mark, so the agent is a
	// participant the moment its converse reply lands.
	if len(toJoin) > 0 && p.convRepo != nil {
		updated := append(conv.Participants(), toJoin...)
		if err := p.convRepo.UpdateParticipants(ctx, conv.ID(), updated, conv.Version(), p.clock.Now()); err != nil {
			return err
		}
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
	return mention.Present(text, name)
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

// @mention token matching lives in internal/mention (single source shared
// with the v2.8 #268 unread mention_count) so a mention badge counts exactly
// the messages that would wake the user.

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
	// T250/T255: a plan/issue/task chat's conversation has no reliable name of its
	// own, so resolve the owning object's title LIVE from its owner_ref and carry it
	// (plus owner_ref) so the daemon brief can tell the agent WHICH object this
	// message belongs to. A miss (or a kind with no resolver) keeps conv.Name(); the
	// brief itself falls back to the id when ConvName is empty, so a title miss never
	// blocks the wake. DM/channel owner_refs resolve to no title → conv.Name() stands.
	convName := conv.Name()
	if name, ok := p.resolveOwnerName(ctx, pl.OwnerRef); ok {
		convName = name
	}
	payload, err := json.Marshal(converseCommandPayload{
		AgentID:        entityID,
		ConversationID: pl.ConversationID,
		ConvKind:       string(conv.Kind()),
		ConvName:       convName,
		SenderRef:      pl.Sender,
		SenderDisplay:  senderDisplay,
		MessageID:      pl.MessageID,
		// T103: append the inbound attachment file_uri(s) inline so the agent can
		// download_file directly (the converse inject advances the read cursor).
		MessageText:     pl.Text + renderInboundAttachments(pl.Attachments),
		RootMessageID:   pl.RootMessageID,   // F4: carry thread root → agent replies in-thread
		AttachmentCount: pl.AttachmentCount, // T74: carry attachment count → brief hints at file(s)
		OwnerRef:        pl.OwnerRef,        // T250: carry owner_ref → brief disambiguates "this plan"
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

// resolveOwnerName resolves an owner_ref → the owning object's live title/name,
// for the converse brief header (T255/OQ2). It routes through the SINGLE
// OwnerContext table (T254) so the {kind → resolver} mapping stays in one place:
// plan→planName, issue→issueTitle, task→taskTitle. It returns ("", false) for a
// kind with no wired resolver, a resolver miss, an empty/unknown owner_ref, or a
// channel ref — callers keep conv.Name() in that case (the brief then falls back
// to the id), so a title miss never blocks a wake.
func (p *WakeProjector) resolveOwnerName(ctx context.Context, ownerRef string) (string, bool) {
	oc, ok := conversation.ResolveOwnerContext(ownerRef)
	if !ok {
		return "", false // dm/channel/unknown → no live title
	}
	var resolver func(context.Context, string) (string, bool)
	switch oc.Kind {
	case conversation.OwnerKindPlan:
		resolver = p.planName
	case conversation.OwnerKindIssue:
		resolver = p.issueTitle
	case conversation.OwnerKindTask:
		resolver = p.taskTitle
	case conversation.OwnerKindProject:
		// OQ1: project chat has NO converse wake path today (no ConversationKindProject;
		// pm://projects/ is only a channel soft-label), so deliverConverse is never
		// reached with a project owner_ref and no projectName resolver is wired. TODO:
		// wire a projectName resolver here if/when project chats gain a wake path.
		return "", false
	default:
		return "", false
	}
	if resolver == nil {
		return "", false
	}
	return resolver(ctx, oc.ID)
}

// projectPlanCreatorWake is the v2.9 P3 failure→agent-creator-wake path (§9.1 /
// decision-1): the PlanOrchestratorProjector emitted EvtPlanCreatorFailureWake
// because a plan task FAILED and the plan's creator is an AGENT — and a SYSTEM
// @mention can never wake an agent (#220 / v2.7 #185 wakes agents ONLY on `user:`
// senders). This is the SANCTIONED DIRECT system wake: resolve the agent-creator →
// its worker binding (mirroring deliverConverse) and enqueue ONE agent.converse
// pointing at the plan conversation, so the agent wakes, reads the failure @mention,
// and self-handles (adjust DAG / escalate via the Stage C MCP plan tools).
//
// Same-tx idempotent (IsApplied/MarkApplied in one tx), exactly like the other
// projector paths. The agent.converse idempotency key embeds the failure @mention's
// MessageID + the creator's resolved EXECUTION-ENTITY id, so a redelivered wake
// event on the SAME failure transition never double-wakes the creator.
//
// LOOP-SAFE (does NOT widen #185): this is a one-shot system→agent wake on a
// DETERMINED creator for a DETERMINED failure event — NOT a chat agent→agent reply.
// The woken creator READS the plan conversation and acts via MCP tools; that
// reading/acting does NOT re-emit EvtPlanCreatorFailureWake (only a NEW task→failed
// transition emits it from the orchestrator) → there is no wake loop.
//
// Resolve-or-skip: if the creator agent can't be resolved, isn't running, or has no
// worker binding, it logs + skips (no error — a missing wake target must not stall
// the projector, mirroring deliverConverse / enqueueWake). MarkApplied still runs.
func (p *WakeProjector) projectPlanCreatorWake(ctx context.Context, e outbox.Event) error {
	var pl planCreatorFailureWakePayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if p.controlLog == nil || p.agents == nil {
		return nil // wake delivery not wired (e.g. test fixtures) → clean no-op
	}
	creatorRef := strings.TrimSpace(pl.CreatorRef)
	convID := strings.TrimSpace(pl.ConversationID)
	failureMsgID := strings.TrimSpace(pl.MessageID)
	// Defensive: only an agent creator should have produced this event; ignore a
	// malformed/non-agent ref or a missing conversation rather than fail.
	if !strings.HasPrefix(creatorRef, agentParticipantPrefix) || convID == "" {
		slog.Info("wake projector: plan-creator wake skipped (non-agent creator ref or no conversation)",
			"creator_ref", creatorRef, "conversation_id", convID, "plan_id", pl.PlanID)
		return nil
	}
	rawID := strings.TrimPrefix(creatorRef, agentParticipantPrefix)

	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if err := p.deliverCreatorWake(txCtx, rawID, convID, failureMsgID, pl); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// deliverCreatorWake resolves the agent-creator and enqueues the agent.converse for
// the plan-failure wake (runs in the caller's tx). It MIRRORS deliverConverse:
// resolve tolerantly (FINDING-J: rawID may be the entity id OR the identity-member
// id), require LifecycleRunning + a worker binding, and enqueue the converse on the
// EXECUTION-ENTITY id (a.ID()). Resolve/running/worker failures log + skip (no error).
//
// IDEMPOTENCY-KEY: "agent.converse:<conv>:<failureMsgID>:<creatorEntity>" — the same
// shape deliverConverse uses (conv:msg:entity), with the failure @mention id as the
// message anchor so a replayed wake event dedups at the ControlLog (never double-wake).
func (p *WakeProjector) deliverCreatorWake(ctx context.Context, rawID, convID, failureMsgID string, pl planCreatorFailureWakePayload) error {
	a, ok := p.resolveAgent(ctx, rawID)
	if !ok {
		slog.Warn("wake projector: plan-creator wake skipped (agent-creator unresolved)",
			"raw_id", rawID, "conversation_id", convID, "plan_id", pl.PlanID)
		return nil
	}
	entityID := string(a.ID())
	if a.Lifecycle() != agent.LifecycleRunning {
		// A stopped agent-creator cannot be woken to self-handle now. Unlike the
		// DM/channel converse (which posts a visible "not running" notice to a HUMAN
		// peer), here the failure @mention already sits in the plan conversation for
		// the creator to read whenever it next runs — so just log + skip (no extra
		// system noise into the plan conversation).
		slog.Info("wake projector: plan-creator wake skipped (agent-creator not running)",
			"agent_id", entityID, "conversation_id", convID, "plan_id", pl.PlanID)
		return nil
	}
	workerID := a.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("wake projector: plan-creator wake skipped (agent-creator has no worker binding)",
			"agent_id", entityID, "conversation_id", convID, "plan_id", pl.PlanID)
		return nil
	}
	// Resolve the plan conversation for kind/name in the brief (best-effort; the
	// converse still delivers if the lookup is unwired/fails).
	convKind, convName := "", ""
	if p.convRepo != nil {
		if conv, err := p.convRepo.FindByID(ctx, conversation.ConversationID(convID)); err == nil && conv != nil {
			convKind = string(conv.Kind())
			convName = conv.Name()
		}
	}
	// T250: this wake always targets a plan conversation, so carry owner_ref and the
	// resolved plan name so the brief names WHICH plan failed (the plan conversation
	// has no name of its own — resolve it live like deliverConverse).
	ownerRef := ownerRefPlansPrefix + pl.PlanID
	if p.planName != nil {
		if name, ok := p.planName(ctx, pl.PlanID); ok {
			convName = name
		}
	}
	payload, err := json.Marshal(converseCommandPayload{
		AgentID:        entityID,
		ConversationID: convID,
		ConvKind:       convKind,
		ConvName:       convName,
		SenderRef:      "system",
		SenderDisplay:  "system",
		MessageID:      failureMsgID,
		MessageText:    "A task in your plan failed — read the plan conversation and self-handle (adjust the DAG or escalate).",
		OwnerRef:       ownerRef,
	})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    commandTypeAgentConverse,
		Payload:        string(payload),
		IdempotencyKey: "agent.converse:" + convID + ":" + failureMsgID + ":" + entityID,
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

// renderInboundAttachments renders inbound attachment file_uris + metadata as
// plain-text lines appended to a woken agent's brief (v2.10.1 [T103]), so the
// agent can download_file the file(s) directly. Empty input → "". Each line:
//
//	[attachment: <uri> <filename> (<mime>, <n> bytes)]
//
// Authorization is fail-closed at fetch time: the uri only reaches the
// conversation's own participant agents (the wake recipients), and download_file
// independently re-checks conversation membership (a non-participant gets 403).
func renderInboundAttachments(atts []wakeAttachment) string {
	if len(atts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, a := range atts {
		b.WriteString("\n[attachment: ")
		b.WriteString(a.URI)
		if strings.TrimSpace(a.Filename) != "" {
			b.WriteByte(' ')
			b.WriteString(a.Filename)
		}
		b.WriteString(" (")
		b.WriteString(a.MimeType)
		b.WriteString(", ")
		b.WriteString(strconv.FormatInt(a.Size, 10))
		b.WriteString(" bytes)]")
	}
	return b.String()
}

// ReconcileOnce is the poll-fallback sweep hook for the WakeReconcileLoop.
//
// v2.14.0 F7 (issue I14): the D2-e-iii poll-fallback sweep recomputed unread for
// every waiting_input AgentWorkItem and re-enqueued any pending batch flush. With
// AgentWorkItem retired (input-required is now a Task-level block, not a
// per-WorkItem waiting_input state) there is no WorkItem set to sweep, so this is
// a no-op. The loop wiring is kept so re-introducing a task-level sweep later is a
// one-method change.
func (p *WakeProjector) ReconcileOnce(_ context.Context) error {
	return nil
}

var _ outbox.Projector = (*WakeProjector)(nil)
