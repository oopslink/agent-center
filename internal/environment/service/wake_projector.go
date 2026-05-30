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
}

// NewWakeProjector constructs the projector.
func NewWakeProjector(d WakeProjectorDeps) *WakeProjector {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WakeProjector{
		db:         d.DB,
		workItems:  d.WorkItems,
		agents:     d.Agents,
		controlLog: d.ControlLog,
		applied:    d.Applied,
		clock:      clk,
		convRepo:   d.ConvRepo,
		msgRepo:    d.MsgRepo,
		readState:  d.ReadState,
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
	// Defensive: the producer only emits for task conversations, but guard here
	// too so a stray non-task event is a clean no-op.
	if !strings.HasPrefix(pl.OwnerRef, ownerRefTasksPrefix) {
		return nil
	}
	taskRef := pl.OwnerRef

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

	// Self-exclusion: the agent's own message (sender == "agent:<id>") never wakes
	// it — this keeps request_input's same-tx question from re-waking the asker.
	if pl.Sender == "agent:"+string(agentID) {
		return nil
	}

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
// Returns nil (no-op, still MarkApplied) on any "nothing to do" condition:
// deps not wired, no conversation id, no unread, WorkItem no longer waiting_input,
// agent unresolved / no worker.
func (p *WakeProjector) flushAwaitingInput(ctx context.Context, pl awaitingInputPayload) error {
	if p.controlLog == nil || p.agents == nil || p.msgRepo == nil || p.readState == nil {
		return nil // batch delivery not wired (e.g. test fixtures / e-i-only build)
	}
	agentID := strings.TrimSpace(pl.AgentID)
	convID := strings.TrimSpace(pl.ConversationID)
	if agentID == "" || convID == "" || strings.TrimSpace(pl.WorkItemID) == "" {
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
	wi, err := p.resolveWaitingWorkItem(ctx, pl)
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
			"agent_id", agentID, "work_item_id", pl.WorkItemID, "err", err)
		return nil
	}
	workerID := a.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("wake projector: batch flush skipped (agent has no worker binding)",
			"agent_id", agentID, "work_item_id", pl.WorkItemID)
		return nil
	}

	// (f) merge into ONE sender-labeled text; the newest id is the cursor target.
	mergedText := mergeMessages(unread)
	lastID := string(unread[len(unread)-1].ID())

	payload, err := json.Marshal(wakeCommandPayload{
		AgentID:        agentID,
		WorkItemID:     pl.WorkItemID,
		TaskRef:        pl.TaskRef,
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
		IdempotencyKey: "agent.wake:" + pl.WorkItemID + ":batch:" + lastID,
	})
	return err
}

// resolveWaitingWorkItem returns the agent's WorkItem named by the payload IFF it
// is still waiting_input; nil otherwise (already woken / superseded / not found).
func (p *WakeProjector) resolveWaitingWorkItem(ctx context.Context, pl awaitingInputPayload) (*agent.AgentWorkItem, error) {
	items, err := p.workItems.ListByTask(ctx, pl.TaskRef)
	if err != nil {
		return nil, err
	}
	for _, wi := range items {
		if wi.ID() != pl.WorkItemID {
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

var _ outbox.Projector = (*WakeProjector)(nil)
