package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

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
// #185 wakes agents on messages from a human sender — one of the loop-break rules
// (an AGENT sender wakes only through the four-gate guard; see below).
const userParticipantPrefix = "user:"

// systemSenderRef is the sentinel IdentityRef of the system singleton (ADR-0033 —
// the bare literal "system", no scheme). quick-fix (@oopslink, system-msg-activity):
// a system-authored TEXT message that @mentions an agent MUST be delivered to it
// exactly like a normal message — the same agent.converse inject that records a
// "Received" activity (message_delivered) and advances the read cursor. Before this
// the WakeProjector woke ONLY user:/agent: senders, so system MESSAGES (e.g. a plan
// "task ready" @mention) never entered the agent's context and left no receive
// record. LOOP-SAFE: a system message is machine-emitted by a projector, never an
// agent replying, so it cannot start an agent↔agent storm (no wake-guard needed);
// @all stays human-only (broadcastAll gates on user:). See projectConversationMessage.
const systemSenderRef = "system"

// systemMessageContentKind is the content_kind of a real (deliverable) system
// message. Only content_kind=text system messages are delivered like normal
// messages; system NOTIFICATION CHROME (content_kind=system — e.g. the "@X is not
// running" notice) is NOT injected. Mirrors conversation.MessageContentText without
// importing it (this payload is a BC-boundary JSON mirror).
const systemMessageContentKind = "text"

// v2.14.0 F7 (issue I14): EvtAgentAwaitingInput + the D2-e-ii batch-flush /
// awaiting_input wake path (and the WorkItem-keyed task wake) removed —
// AgentWorkItem retired. The input-required await is now block_task(input_required)
// via the PM F6 path; the wake projector keeps only the conversation @mention wakes.

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
	// systemNotify posts a system notification-style message into a conversation
	// (the "agent not running" signal when a DM/channel targets a stopped agent).
	displayName func(ctx context.Context, identityID string) (string, bool)
	// systemNotify / systemMessage return the POSTED message id (quick-fix
	// system-msg-activity): the sanctioned system→agent nudge paths (lease-nudge /
	// issue-derived-done) key their converse idempotency on this id so it MATCHES the
	// generic system-message wake's converse key (deliverConverse) — the ControlLog
	// then collapses the two into ONE delivery instead of double-injecting. "" =
	// nothing posted (dep unwired) → the caller falls back to its legacy key.
	systemNotify func(ctx context.Context, conversationID, text string) (string, error)
	// systemMessage posts a normal text message authored by the system singleton.
	// Use this for actionable workflow messages that humans/agents should handle as
	// ordinary conversation content, not notification chrome.
	systemMessage func(ctx context.Context, conversationID, text string) (string, error)

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

	// T335 follow-up — server-side periodic session-heal sweep (see wake_sweep.go).
	// sweepCandidates lists agents that are desired-running but have queued runnable
	// work and no running task (≈ a dropped/idle session). nil → ReconcileOnce stays a
	// no-op (dormant until wired, preserving the post-F7 behavior). sweepGrace is the
	// per-agent debounce window (<=0 → defaultSweepGrace). sweepGiveUp raises the
	// visible signal when an agent crosses the give-up cap (nil → silent give-up).
	// sweepState/sweepMu hold the per-agent debounce+backoff memory across ticks
	// (in-memory only; reset on restart).
	sweepCandidates func(ctx context.Context) ([]SweepCandidate, error)
	sweepGrace      time.Duration
	sweepGiveUp     func(ctx context.Context, c SweepCandidate)
	sweepMu         sync.Mutex
	sweepState      map[string]*sweepAgentState
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
	DisplayName func(ctx context.Context, identityID string) (string, bool)
	// SystemNotify / SystemMessage return the posted message id (quick-fix
	// system-msg-activity) so the sanctioned nudge paths can align their converse
	// idempotency key with the generic system-message wake (dedup). See the struct
	// fields for the full rationale.
	SystemNotify func(ctx context.Context, conversationID, text string) (string, error)
	// SystemMessage posts a normal text message authored by system. If nil, callers
	// that need ordinary message semantics fall back to SystemNotify for compatibility.
	SystemMessage func(ctx context.Context, conversationID, text string) (string, error)

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

	// T335 follow-up — server-side session-heal sweep (optional). SweepCandidates
	// nil → ReconcileOnce is a no-op (dormant). SweepGrace <=0 → defaultSweepGrace.
	// SweepGiveUp nil → give-up is silent (no escalation signal).
	SweepCandidates func(ctx context.Context) ([]SweepCandidate, error)
	SweepGrace      time.Duration
	SweepGiveUp     func(ctx context.Context, c SweepCandidate)
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
		systemMessage:       d.SystemMessage,
		projectAgentMembers: d.ProjectAgentMembers,
		planName:            d.PlanName,
		issueTitle:          d.IssueTitle,
		taskTitle:           d.TaskTitle,
		wakeGuard:           d.WakeGuard,
		sweepCandidates:     d.SweepCandidates,
		sweepGrace:          d.SweepGrace,
		sweepGiveUp:         d.SweepGiveUp,
		sweepState:          make(map[string]*sweepAgentState),
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
	// ContentKind (quick-fix system-msg-activity) is the producer's message
	// content_kind. It gates the system-sender wake: a system-authored TEXT message
	// (content_kind=text — e.g. a plan "task ready" @mention) is delivered like a
	// normal message; system notification CHROME (content_kind=system — e.g. the
	// "@X is not running" notice) is NOT. Empty → treated as a plain message.
	ContentKind string `json:"content_kind,omitempty"`
	// RootMessageID (v2.9.1 Thread F4) is the thread root of the triggering message
	// (empty if top-level); carried through to the agent so its reply lands in-thread.
	RootMessageID string `json:"root_message_id,omitempty"`
	// MentionRefs (T460 ①) are explicit, typo-proof agent mention refs ("agent:<id>")
	// the sender passed structurally to post_message — a machine-reliable @mention
	// (assignee_ref never mistyped). A candidate agent named here wakes even when the
	// text carries no matching @display_name, but STILL only within the conversation's
	// existing wake scope (participants ∪ project members) and STILL through the
	// four-gate wake guard — refs typo-proof the mention, they do not widen who is reachable.
	MentionRefs []string `json:"mention_refs,omitempty"`
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

// taskLeaseExpiredNudgePayload mirrors the JSON the PM Service writes for the T456
// EvtTaskLeaseExpiredNudge outbox event (env-side copy — that pm type is unexported;
// mirroring its keys keeps the BC boundary clean). AssigneeRef is "agent:<member-id>";
// OwnerRef is pm://tasks/{id} (used to resolve the task's bound conversation).
type taskLeaseExpiredNudgePayload struct {
	TaskID      string `json:"task_id"`
	ProjectID   string `json:"project_id"`
	OwnerRef    string `json:"owner_ref"`
	AssigneeRef string `json:"assignee_ref"`
}

// issueDerivedTasksDonePayload mirrors the JSON the PM Service writes for the T464
// EvtIssueDerivedTasksDone outbox event (env-side copy of the unexported pm type).
// OwnerRef is pm://issues/{id} (resolves the issue's bound conversation); OwnerIdentity
// is the issue owner's identity ref (created_by — "agent:<member>" or "user:<id>"), the
// @mention + converse target. Total/Completed/Discarded drive the message wording.
type issueDerivedTasksDonePayload struct {
	IssueID       string `json:"issue_id"`
	ProjectID     string `json:"project_id"`
	OwnerRef      string `json:"owner_ref"`
	OwnerIdentity string `json:"owner_identity"`
	Total         int    `json:"total"`
	Completed     int    `json:"completed"`
	Discarded     int    `json:"discarded"`
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
	case pmservice.EvtTaskLeaseExpiredNudge:
		return p.projectLeaseExpiredNudge(ctx, e)
	case pmservice.EvtIssueDerivedTasksDone:
		return p.projectIssueDerivedTasksDone(ctx, e)
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
	// agent's DM never wakes its peer).
	//
	// quick-fix (@oopslink, system-msg-activity) — DO NOT REVERT to "system never
	// wakes": a SYSTEM sender now ALSO delivers, but ONLY for a real MESSAGE
	// (content_kind=text — e.g. a plan "task ready" @mention), NOT for notification
	// CHROME (content_kind=system — e.g. the "@X is not running" notice). Rationale:
	// a system TEXT message directed at an agent must reach it exactly like a normal
	// message — the same agent.converse inject that (a) enters the agent's context
	// ("delivered like a normal message") and (b) records a "Received" activity
	// (message_delivered) + advances the read cursor. Previously such messages only
	// woke the assignee via a content-free agent.work_available (pull), so the actual
	// message never entered context and left NO receive record — exactly the gap this
	// fixes. LOOP-SAFE: a system message is projector-emitted, never an agent replying,
	// so it cannot start an agent-storm; it needs no wake-guard, and @all stays
	// human-only (broadcastAll gates on user:). DEDUP with the sanctioned system→agent
	// wakes (plan-creator / lease-nudge / issue-derived-done, which ALSO enqueue a
	// converse for the same posted message): those key their converse on the SAME
	// (conversation,message,entity) tuple as deliverConverse, so the ControlLog
	// idempotency collapses the two into ONE delivery (see deliverLeaseNudge / the
	// plan-creator + issue-done paths).
	isHumanSender := strings.HasPrefix(pl.Sender, userParticipantPrefix)
	isAgentSender := strings.HasPrefix(pl.Sender, agentParticipantPrefix)
	isSystemSender := pl.Sender == systemSenderRef && pl.ContentKind == systemMessageContentKind
	if !isHumanSender && !isAgentSender && !isSystemSender {
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
	// WorkItem-keyed input-required wake that used to run in projectMessageAdded was
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
	// T460 ①: structural mention_refs ("agent:<id>") explicitly name wake targets,
	// typo-proof (assignee_ref never mistyped). A candidate named here passes the
	// @mention gate even when the text has no matching @display_name — but it must
	// already BE a candidate (participant ∪ project member), so refs only bypass the
	// text gate, they never widen the reachable set (承载性 handoff dispatch is out of
	// scope). Normalized to the bare id (scheme stripped, lowercased) for matching.
	mentionRefSet := map[string]bool{}
	for _, ref := range pl.MentionRefs {
		r := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), agentParticipantPrefix)))
		if r != "" {
			mentionRefSet[r] = true
		}
	}
	explicitRef := func(a *agent.Agent, rawID string) bool {
		if len(mentionRefSet) == 0 {
			return false
		}
		for _, id := range []string{string(a.ID()), a.IdentityMemberID(), rawID} {
			if id != "" && mentionRefSet[strings.ToLower(strings.TrimSpace(id))] {
				return true
			}
		}
		return false
	}
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
		if kind != conversation.ConversationKindDM && !broadcastAll && !explicitRef(a, rawID) && !p.mentionsAgent(ctx, a, rawID, pl.Text) {
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

// mentionsAgent reports whether text @mentions the agent — by its display_name
// (case-insensitive, token-bounded so @Bot ≠ @Bottom) OR, per T460, by its agent
// id/ref. The id/ref forms close the silent-failure gap where a handle written as
// "@agent-center-ba6bc42a" or "agent:agent-ba6bc42a" (an id/ref, not the
// display_name) matched nobody and woke no one. A match on ANY of these is
// equivalent to a display_name hit (same wake path):
//   - display_name @mention (agentDisplayName: identity name preferred, profile fallback);
//   - a bare "agent:<id>" colon-ref for the entity id, member id, or participant ref;
//   - an @token that equals, or contains the unique id-fragment of, any of those ids.
//
// Channel gating (#185 / FINDING-J).
func (p *WakeProjector) mentionsAgent(ctx context.Context, a *agent.Agent, rawID, text string) bool {
	if name, ok := p.agentDisplayName(ctx, a, rawID); ok && strings.TrimSpace(name) != "" {
		if mention.Present(text, name) {
			return true
		}
	}
	tokens := mention.ExtractTokens(text)
	for _, id := range []string{string(a.ID()), a.IdentityMemberID(), rawID} {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if mention.ContainsRef(text, agentParticipantPrefix+id) {
			return true
		}
		for _, tok := range tokens {
			if mention.TokenMatchesID(tok, id) {
				return true
			}
		}
	}
	return false
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
		// quick-fix (system-msg-activity): a SYSTEM-sender message to a stopped agent
		// skips SILENTLY — no "@X is not running" chrome. The durable message already
		// sits in the conversation for the agent to read on its next run, and a system
		// nudge posting "not running" back into the plan/task chat is just noise (it
		// also matches the sanctioned system→agent paths, which log+skip a stopped
		// target). The "not running" notice exists to tell a HUMAN peer their DM/channel
		// message won't be answered now — keep it ONLY for human/agent senders.
		if pl.Sender == systemSenderRef {
			slog.Info("wake projector: system message target not running — skip (durable message stays for next run)",
				"agent_id", entityID, "conversation_id", pl.ConversationID)
			return nil
		}
		// Visible signal instead of silence (#185 Tester refinement). The name
		// resolves via agentDisplayName so the notice reads "@AgentBeta", not the
		// raw entity id, even when the participant ref carried the entity id
		// (FINDING-J / Rule 2).
		if p.systemNotify != nil {
			name, _ := p.agentDisplayName(ctx, a, rawID)
			_, err := p.systemNotify(ctx, pl.ConversationID,
				"@"+name+" is not running and won't reply until it is started.")
			return err
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
		// 引用 (quote): also inline the quoted-message context so a woken agent
		// perceives WHAT the sender quoted — the push wake advances the read cursor,
		// so a later get_my_unread is empty; the quote MUST ride the push like attachments.
		MessageText:     pl.Text + p.renderQuotedContext(ctx, pl.MessageID) + renderInboundAttachments(pl.Attachments),
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
//
// quick-fix (system-msg-activity) — natural dedup, DO NOT change the key shape: the
// failure @mention is a system TEXT message (PostMention), so the generic
// system-message wake (projectConversationMessage → deliverConverse) ALSO enqueues a
// converse for it — with EXACTLY this key (failureMsgID == the posted message id). The
// two collapse into one delivery. Keeping this sanctioned path is belt-and-suspenders
// (it still wakes even if the generic path is unwired in a given composition).
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

// projectLeaseExpiredNudge is the T456 lapsed-lease nudge path (issue-21ba5b78/I30):
// the lease-checker emitted EvtTaskLeaseExpiredNudge because a running task's
// execution lease lapsed — and it must NOT be reclaimed (no open/assignee-cleared
// orphan). This SANCTIONED system→agent wake (mirroring projectPlanCreatorWake) (a)
// posts a visible @assignee nudge message into the task's bound conversation and (b)
// enqueues an agent.converse so the SAME owner is woken to continue. A system @mention
// alone can never wake an agent (the #185 human-only loop-break), hence the direct
// converse enqueue. The task stays running and the assignee is unchanged.
//
// Same-tx idempotent (IsApplied/MarkApplied), exactly like the other projector paths,
// so a redelivered event posts the message + wakes at most once.
func (p *WakeProjector) projectLeaseExpiredNudge(ctx context.Context, e outbox.Event) error {
	var pl taskLeaseExpiredNudgePayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if p.controlLog == nil || p.agents == nil || p.convRepo == nil {
		return nil // wake/conversation delivery not wired (e.g. test fixtures) → no-op
	}
	assigneeRef := strings.TrimSpace(pl.AssigneeRef)
	if !strings.HasPrefix(assigneeRef, agentParticipantPrefix) ||
		!strings.HasPrefix(pl.OwnerRef, ownerRefTasksPrefix) {
		slog.Info("wake projector: lease-nudge skipped (non-agent assignee or non-task owner_ref)",
			"assignee_ref", assigneeRef, "owner_ref", pl.OwnerRef, "task_id", pl.TaskID)
		return nil
	}
	rawID := strings.TrimPrefix(assigneeRef, agentParticipantPrefix)
	taskID := strings.TrimPrefix(pl.OwnerRef, ownerRefTasksPrefix)

	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if err := p.deliverLeaseNudge(txCtx, e, rawID, taskID, pl); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// deliverLeaseNudge resolves the task's bound conversation, posts the visible
// @assignee nudge as a normal system-authored text message, and enqueues an
// agent.converse for a RUNNING assignee so the owner is woken to continue. Runs in
// the caller's tx. A missing conversation / unresolved-or-stopped assignee / no
// worker binding is a logged skip (no error — a missing wake target must not stall
// the projector); the posted message persists for the agent to read when it next
// runs (e.g. after self-heal relaunch).
func (p *WakeProjector) deliverLeaseNudge(ctx context.Context, e outbox.Event, rawID, taskID string, pl taskLeaseExpiredNudgePayload) error {
	conv, err := p.convRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(taskID))
	if err != nil {
		if errors.Is(err, conversation.ErrConversationNotFound) {
			slog.Info("wake projector: lease-nudge skipped (no bound task conversation)", "task_id", taskID)
			return nil // no conversation to nudge into → drain the event (MarkApplied)
		}
		return err
	}
	convID := string(conv.ID())

	// Resolve the assignee for the @mention display name (tolerant: rawID may be the
	// entity id OR the identity-member id, like deliverConverse).
	a, ok := p.resolveAgent(ctx, rawID)
	name := rawID
	if ok {
		if n, nok := p.agentDisplayName(ctx, a, rawID); nok {
			name = n
		}
	}
	nudgeText := "@" + name + " ⏰ your run on this task may have stalled — its execution lease lapsed. " +
		"The task is still yours (assignee unchanged); please continue, or complete/block it if you're done."

	// (a) The visible, durable @assignee nudge message (sender=system,
	// content_kind=text). quick-fix (system-msg-activity): this posted TEXT message
	// now ALSO drives the generic system-message wake (projectConversationMessage →
	// deliverConverse), which is what delivers it + records the "Received" activity.
	// We capture its message id (postedMsgID) so the converse we enqueue below shares
	// the SAME idempotency key the generic path uses — the two collapse into ONE
	// delivery at the ControlLog. Fall back to the legacy SystemNotify port only for
	// old test wiring.
	postSystemMessage := p.systemMessage
	if postSystemMessage == nil {
		postSystemMessage = p.systemNotify
	}
	var postedMsgID string
	if postSystemMessage != nil {
		mid, err := postSystemMessage(ctx, convID, nudgeText)
		if err != nil {
			return err
		}
		postedMsgID = mid
	}

	// (b) Wake a RUNNING assignee. Resolve/running/worker failures log + skip (the
	// posted message already persists for the agent to read on its next run).
	if !ok {
		slog.Warn("wake projector: lease-nudge wake skipped (assignee unresolved)", "raw_id", rawID, "task_id", taskID)
		return nil
	}
	entityID := string(a.ID())
	if a.Lifecycle() != agent.LifecycleRunning {
		slog.Info("wake projector: lease-nudge wake skipped (assignee not running)", "agent_id", entityID, "task_id", taskID)
		return nil
	}
	workerID := a.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("wake projector: lease-nudge wake skipped (assignee has no worker binding)", "agent_id", entityID, "task_id", taskID)
		return nil
	}
	// Resolve the task title LIVE for the converse brief header (T255 convention; a
	// miss falls back to conv.Name() then the id).
	convName := conv.Name()
	if p.taskTitle != nil {
		if title, tok := p.taskTitle(ctx, taskID); tok {
			convName = title
		}
	}
	// quick-fix (system-msg-activity): anchor the converse on the POSTED message id
	// (postedMsgID) so this sanctioned converse and the generic system-message wake's
	// converse for the SAME message share one idempotency key → the ControlLog keeps
	// exactly one (no double-inject). Fall back to the per-episode event id (e.ID)
	// only when no message was posted (systemMessage unwired), preserving the legacy
	// behavior for old test wiring.
	converseMsgID := postedMsgID
	idemAnchor := "agent.converse:" + convID + ":" + postedMsgID + ":" + entityID
	if postedMsgID == "" {
		converseMsgID = e.ID // unique per nudge episode → distinct converse per lapse
		idemAnchor = "agent.converse:" + convID + ":lease-nudge:" + e.ID + ":" + entityID
	}
	payload, err := json.Marshal(converseCommandPayload{
		AgentID:        entityID,
		ConversationID: convID,
		ConvKind:       string(conv.Kind()),
		ConvName:       convName,
		SenderRef:      "system",
		SenderDisplay:  "system",
		MessageID:      converseMsgID,
		MessageText:    nudgeText,
		OwnerRef:       pl.OwnerRef,
	})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    commandTypeAgentConverse,
		Payload:        string(payload),
		IdempotencyKey: idemAnchor,
	})
	return err
}

// projectIssueDerivedTasksDone is the T464 path (issue-41aceddb): the PM hook emitted
// EvtIssueDerivedTasksDone because a task's conclusion made ALL of its issue's derived
// tasks terminal. This SANCTIONED system→agent wake (mirroring projectLeaseExpiredNudge)
// (a) posts a visible @owner message into the issue's bound conversation and (b) — when
// the owner is an agent — enqueues an agent.converse so the owner is woken to REVIEW and
// close the issue (close_issue). TRIGGER-ONLY: nothing here changes the issue's status.
// A HUMAN owner is notified by the @mention in the conversation (no converse to enqueue).
//
// Same-tx idempotent (IsApplied/MarkApplied), so a redelivered event posts the message
// + wakes at most once.
func (p *WakeProjector) projectIssueDerivedTasksDone(ctx context.Context, e outbox.Event) error {
	var pl issueDerivedTasksDonePayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if p.controlLog == nil || p.agents == nil || p.convRepo == nil {
		return nil // wake/conversation delivery not wired (e.g. test fixtures) → no-op
	}
	if strings.TrimSpace(pl.IssueID) == "" || strings.TrimSpace(pl.OwnerIdentity) == "" {
		slog.Info("wake projector: issue-derived-done skipped (missing issue id or owner)",
			"issue_id", pl.IssueID, "owner", pl.OwnerIdentity)
		return nil
	}
	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if err := p.deliverIssueDerivedTasksDone(txCtx, e, pl); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// deliverIssueDerivedTasksDone resolves the issue's bound conversation, posts the
// visible @owner "all derived tasks concluded — please review and close" message as a
// normal system-authored text message (NOT systemNotify), and — when the owner is a
// RUNNING agent — enqueues an agent.converse so the owner is woken to act. Runs in the
// caller's tx. A missing conversation / human owner / unresolved-or-stopped agent owner
// / no worker binding is a logged skip (no error); the posted message persists for the
// owner to read (a human via the UI unread, an agent on its next run).
func (p *WakeProjector) deliverIssueDerivedTasksDone(ctx context.Context, e outbox.Event, pl issueDerivedTasksDonePayload) error {
	conv, err := p.convRepo.FindByOwnerRef(ctx, conversation.NewIssueOwnerRef(pl.IssueID))
	if err != nil {
		if errors.Is(err, conversation.ErrConversationNotFound) {
			slog.Info("wake projector: issue-derived-done skipped (no bound issue conversation)", "issue_id", pl.IssueID)
			return nil
		}
		return err
	}
	convID := string(conv.ID())

	ownerRef := strings.TrimSpace(pl.OwnerIdentity)
	isAgentOwner := strings.HasPrefix(ownerRef, agentParticipantPrefix)
	rawOwner := ownerRef
	if i := strings.IndexByte(ownerRef, ':'); i >= 0 {
		rawOwner = ownerRef[i+1:]
	}
	// Resolve the owner's display name for the @mention (agent: via the agent entity;
	// otherwise via the identity display-name resolver; raw id as last resort).
	var ownerAgent *agent.Agent
	var agentOK bool
	name := rawOwner
	if isAgentOwner {
		if a, ok := p.resolveAgent(ctx, rawOwner); ok {
			ownerAgent, agentOK = a, true
			if n, nok := p.agentDisplayName(ctx, a, rawOwner); nok {
				name = n
			}
		}
	} else if n, ok := p.displayNameOr(ctx, ownerRef, rawOwner); ok {
		name = n
	}

	msg := "@" + name + " all " + strconv.Itoa(pl.Total) + " task(s) derived from this issue are now "
	if pl.Discarded == 0 {
		msg += "complete."
	} else {
		msg += "concluded (" + strconv.Itoa(pl.Completed) + " completed, " + strconv.Itoa(pl.Discarded) + " discarded)."
	}
	msg += " Please review and close the issue (close_issue) if it's resolved."

	// (a) The visible, durable @owner message (sender=system, content_kind=text).
	// It does NOT wake by itself (system sender, #185); for an agent owner the
	// converse below wakes it, for a human owner the @mention notifies via the UI
	// unread. Fall back to the legacy SystemNotify port only for old test wiring.
	postSystemMessage := p.systemMessage
	if postSystemMessage == nil {
		postSystemMessage = p.systemNotify
	}
	var postedMsgID string
	if postSystemMessage != nil {
		mid, err := postSystemMessage(ctx, convID, msg)
		if err != nil {
			return err
		}
		postedMsgID = mid
	}

	// (b) Wake a RUNNING agent owner. A human owner / unresolved / stopped / no-worker
	// owner is a logged skip — the posted message already carries the signal.
	if !isAgentOwner || !agentOK {
		return nil
	}
	entityID := string(ownerAgent.ID())
	if ownerAgent.Lifecycle() != agent.LifecycleRunning {
		slog.Info("wake projector: issue-derived-done wake skipped (owner agent not running)", "agent_id", entityID, "issue_id", pl.IssueID)
		return nil
	}
	workerID := ownerAgent.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("wake projector: issue-derived-done wake skipped (owner agent has no worker binding)", "agent_id", entityID, "issue_id", pl.IssueID)
		return nil
	}
	// Resolve the issue title LIVE for the converse brief header (T255 convention).
	convName := conv.Name()
	if p.issueTitle != nil {
		if title, tok := p.issueTitle(ctx, pl.IssueID); tok {
			convName = title
		}
	}
	// quick-fix (system-msg-activity): anchor the converse on the POSTED message id so
	// it dedups against the generic system-message wake's converse for the SAME
	// message (one delivery at the ControlLog). Legacy per-episode e.ID key only when
	// nothing was posted (systemMessage unwired).
	converseMsgID := postedMsgID
	idemAnchor := "agent.converse:" + convID + ":" + postedMsgID + ":" + entityID
	if postedMsgID == "" {
		converseMsgID = e.ID // unique per episode → distinct converse per fill-cycle
		idemAnchor = "agent.converse:" + convID + ":issue-derived-done:" + e.ID + ":" + entityID
	}
	payload, err := json.Marshal(converseCommandPayload{
		AgentID:        entityID,
		ConversationID: convID,
		ConvKind:       string(conv.Kind()),
		ConvName:       convName,
		SenderRef:      "system",
		SenderDisplay:  "system",
		MessageID:      converseMsgID,
		MessageText:    msg,
		OwnerRef:       pl.OwnerRef,
	})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    commandTypeAgentConverse,
		Payload:        string(payload),
		IdempotencyKey: idemAnchor,
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
// wakeQuoteSnippetMaxRunes bounds the quoted-message preview text inlined into a
// converse brief so a huge quoted message can't bloat the wake payload.
const wakeQuoteSnippetMaxRunes = 120

// wakeQuoteSnippet is a single-line, rune-safe truncation of a quoted message's
// content for the brief (newlines collapse to spaces; over the cap → ellipsis).
func wakeQuoteSnippet(content string) string {
	s := strings.Join(strings.Fields(content), " ")
	r := []rune(s)
	if len(r) <= wakeQuoteSnippetMaxRunes {
		return s
	}
	return string(r[:wakeQuoteSnippetMaxRunes]) + "…"
}

// renderQuotedContext (引用) returns the quote-preview block appended to a woken
// agent's converse brief, so the agent perceives WHAT the sender quoted — not just
// the bare reply. "" when the triggering message quotes nothing, the msg repo is
// unwired, or the triggering message can't be re-read (never blocks the wake). A
// deleted / absent quoted target renders an "original unavailable" stub so the
// reference is surfaced, never silently dropped. Resolved center-side (the repo is
// here) so no quoted_message_id plumbing has to cross the control-log boundary.
func (p *WakeProjector) renderQuotedContext(ctx context.Context, triggeringMessageID string) string {
	if p.msgRepo == nil || strings.TrimSpace(triggeringMessageID) == "" {
		return ""
	}
	trig, err := p.msgRepo.FindByID(ctx, conversation.MessageID(triggeringMessageID))
	if err != nil {
		return ""
	}
	qid := trig.QuotedMessageID()
	if qid == "" {
		return ""
	}
	q, err := p.msgRepo.FindByID(ctx, qid)
	if err != nil {
		return "\n\n[引用 (quote) → original unavailable]"
	}
	who, _ := p.displayNameOr(ctx, string(q.SenderIdentityID()), string(q.SenderIdentityID()))
	return "\n\n[引用 (quote) @" + who + ": " + wakeQuoteSnippet(q.Content()) + "]"
}

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
// v2.14.0 F7 (issue I14) emptied this when AgentWorkItem retired (the old
// waiting_input WorkItem sweep had nothing left to scan). T335 follow-up re-homes a
// TASK-level sweep here: the SERVER-side session-heal backstop (see wake_sweep.go).
// Each tick it asks sweepCandidates for agents that are desired-running but have
// queued runnable work and no running task (≈ a dropped/idle session whose wake was
// lost), applies the per-agent grace + backoff + give-up debounce, and re-emits
// agent.work_available for the due ones — which routes through the T335
// workAvailable→relaunchForWake path that relaunches a down session (a same-version
// agent.reconcile would instead be swallowed by the daemon's appliedVersion guard).
//
// Dormant when SweepCandidates is nil (the post-F7 no-op). Best-effort and
// non-wedging: a per-candidate append error is collected but never aborts the rest,
// and the loop logs whatever is returned and retries next tick (with a fresh epoch
// key, so a transient failure is cleanly re-attempted).
func (p *WakeProjector) ReconcileOnce(ctx context.Context) error {
	if p.sweepCandidates == nil {
		return nil // dormant until wired
	}
	cands, err := p.sweepCandidates(ctx)
	if err != nil {
		return err
	}
	due, giveUp := p.selectDueSweeps(cands)

	for _, c := range giveUp {
		// Escalate ONCE: the sweep nudged this desired-running agent the cap's worth of
		// times and it still has no running session — surface it (the hook logs warn /
		// raises a human-facing obstacle) instead of slow-retrying silently forever.
		if p.sweepGiveUp != nil {
			p.sweepGiveUp(ctx, c)
		}
	}
	if len(due) == 0 {
		return nil
	}

	// One epoch per tick → a fresh idempotency key each tick (the ControlLog dedups on
	// UNIQUE(worker_id, idempotency_key); a stable key would fold re-emits into one row
	// and a down agent would be nudged at most once). UnixNano advances between 60s
	// ticks trivially; within a tick all candidates share the epoch but differ by
	// agent/task, so keys stay unique.
	epoch := strconv.FormatInt(p.clock.Now().UnixNano(), 10)
	var firstErr error
	for _, c := range due {
		payload, mErr := json.Marshal(sweepWakePayload{AgentID: c.AgentID, TaskID: c.TaskID})
		if mErr != nil {
			if firstErr == nil {
				firstErr = mErr
			}
			continue
		}
		if _, aErr := p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
			WorkerID:       environment.WorkerID(c.WorkerID),
			CommandType:    commandTypeWorkAvailable,
			Payload:        string(payload),
			IdempotencyKey: "sweep.wake:" + c.AgentID + ":" + c.TaskID + ":" + epoch,
		}); aErr != nil {
			if firstErr == nil {
				firstErr = aErr
			}
			continue
		}
	}
	return firstErr
}

var _ outbox.Projector = (*WakeProjector)(nil)
