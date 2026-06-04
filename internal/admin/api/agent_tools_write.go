package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// errNoLiveWorkItem signals that the agent holds a WorkItem for the task but
// none of them are non-terminal — there is nothing to park in waiting_input, so
// request_input is rejected and its whole tx rolls back.
var errNoLiveWorkItem = errors.New("agent has no live work item for this task")

// =============================================================================
// Agent MCP write tools — explicit human-visible communication (v2.7 D2-b2,
// ADR-0049). The agent (via its Worker daemon) posts to the task it is WORKING:
//
//	post_task_message  — append a message to the task Conversation        (1 write)
//	request_input      — post a question + park the WorkItem waiting_input (ATOMIC)
//	block_task         — post a reason + pm.BlockTask(blocked)            (ATOMIC)
//	complete_task      — post a summary + pm.CompleteTask(completed)      (ATOMIC)
//
// Every tool goes through requireAgentOnWorker (the b1 guardrail: worker proven
// by the TOKEN OWNER, target agent bound to it) and then the per-agent own-work
// scope check (the agent must hold a WorkItem for pm://tasks/{task_id}). The
// composite tools wrap BOTH BC writes in ONE outer persistence.RunInTx so the
// conversation message and the state change commit or roll back together — the
// agent never leaves a half-applied trace (a posted question with no parked
// WorkItem, or a posted reason with the Task still running).
// =============================================================================

// taskRefFor builds the canonical agent WorkItem task ref for a pm task id.
func taskRefFor(taskID string) string { return "pm://tasks/" + taskID }

// agentActor returns the agent's BUSINESS identity ref used as the message
// sender, the observability Actor, and the pm IdentityRef. v2.7 #185 (FINDING-L):
// this is the identity-MEMBER ref ("agent:<member-id>"), NOT the execution-entity
// id — conversation participants, project membership, and post_message authz all
// key on the member id (the business-layer agent identity). Using the entity id
// here made the agent's reply fail authz (agentIsActiveParticipant compared an
// entity ref against a member-ref participant) and leaked the entity ULID as the
// message sender. Falls back to the entity id only for an agent with no identity
// member (should not occur after no-middle-state).
func agentActor(a *agent.Agent) string {
	if m := strings.TrimSpace(a.IdentityMemberID()); m != "" {
		return "agent:" + m
	}
	return "agent:" + string(a.ID())
}

// findOwnWorkItems returns the agent's WorkItems for pm://tasks/{taskID} — the
// per-agent own-work scope (OQ4/OQ6, tightest scope: the agent acts on its own
// task). We read the agent's items and filter to the task ref; an empty result
// means the agent does not own this task. Reads honor the ambient tx when one
// is present (ExecutorFromCtx in the repo).
func (s *Server) findOwnWorkItems(ctx context.Context, repo agent.WorkItemRepository, a *agent.Agent, taskID string) ([]*agent.AgentWorkItem, error) {
	all, err := repo.ListByAgent(ctx, a.ID())
	if err != nil {
		return nil, err
	}
	ref := taskRefFor(taskID)
	var own []*agent.AgentWorkItem
	for _, wi := range all {
		if wi.TaskRef() == ref {
			own = append(own, wi)
		}
	}
	return own, nil
}

// postAgentMessage appends a message to the task Conversation as the agent.
// It must run inside the caller's tx context (AddMessage nests its own RunInTx).
// Returns the new message id. The Conversation is resolved by owner_ref
// (pm://tasks/{taskID}); ErrConversationNotFound surfaces if the task has no
// bound Conversation yet (the participant projector creates it on task create).
func (s *Server) postAgentMessage(ctx context.Context, d HandlerDeps, a *agent.Agent, taskID, content string) (conversation.MessageID, error) {
	conv, err := d.ConvRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(taskID))
	if err != nil {
		return "", err
	}
	res, err := d.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   conv.ID(),
		SenderIdentityID: conversation.IdentityRef(agentActor(a)),
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionOutbound,
		Content:          content,
		Actor:            observability.Actor(agentActor(a)),
	})
	if err != nil {
		return "", err
	}
	return res.MessageID, nil
}

// requireOwnTask runs the per-agent own-work scope check: the agent must hold at
// least one WorkItem for pm://tasks/{taskID}. On failure it writes the error
// envelope (403 — not the agent's task) and returns false. taskID-missing is a
// 400. wired-checks for the deps it needs are 501.
func (s *Server) requireOwnTask(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, taskID string) bool {
	if strings.TrimSpace(taskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return false
	}
	if d.AgentWorkItemRepo == nil {
		writeError(w, http.StatusNotImplemented, "work_item_repo_not_wired", "")
		return false
	}
	own, err := s.findOwnWorkItems(r.Context(), d.AgentWorkItemRepo, a, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return false
	}
	if len(own) == 0 {
		writeError(w, http.StatusForbidden, "not_agents_task",
			"agent has no work item for this task")
		return false
	}
	return true
}

// --- post_task_message -------------------------------------------------------

type postTaskMessageReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	Content string `json:"content"`
}

// postTaskMessageHandler appends a human-visible message to the task
// Conversation as the agent. Single write (AddMessage is itself atomic).
func (s *Server) postTaskMessageHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req postTaskMessageReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.MessageWriter == nil || d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "conversation_not_wired", "")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "missing_content", "")
		return
	}
	if !s.requireOwnTask(w, r, d, a, req.TaskID) {
		return
	}
	msgID, err := s.postAgentMessage(r.Context(), d, a, req.TaskID, req.Content)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": string(msgID)})
}

// --- post_message (v2.7 #185) ------------------------------------------------

type postMessageReq struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
}

// postMessageHandler appends a message, as the agent, to ANY conversation
// (DM/channel/task) the agent is an ACTIVE participant of — resolved by
// conversation_id. v2.7 #185: this is the agent's reply path for DM/channel
// conversations (post_task_message is task-owner-scoped + requires a WorkItem;
// this is participant-scoped, no WorkItem needed). Authz = active participant.
func (s *Server) postMessageHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req postMessageReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.MessageWriter == nil || d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "conversation_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ConversationID) == "" {
		writeError(w, http.StatusBadRequest, "missing_conversation_id", "")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "missing_content", "")
		return
	}
	conv, err := d.ConvRepo.FindByID(r.Context(), conversation.ConversationID(req.ConversationID))
	if err != nil {
		// v2.7.1 #246 (a): a missing/typo'd conversation_id gets a clear, actionable
		// 404 (not an opaque error) — point the agent at find_org_channel to get a
		// valid id rather than hallucinate one.
		if errors.Is(err, conversation.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "conversation_not_found",
				"conversation "+req.ConversationID+" not found — use find_org_channel to resolve a channel name to its id")
			return
		}
		mapDomainError(w, err)
		return
	}
	if !agentIsActiveParticipant(conv, a) {
		// v2.7.1 #246 (3): precise not-member message for channels (the write-gate is
		// unchanged — #224/#227 participant gate still 403s; only the wording is
		// actionable). β boundary HELD: visibility via find_org_channel does NOT grant
		// write — a non-member must still be added.
		if conv.Kind() == conversation.ConversationKindChannel {
			writeError(w, http.StatusForbidden, "not_a_channel_member",
				"not a member of channel "+conv.Name()+" — ask an owner to add you before posting")
			return
		}
		writeError(w, http.StatusForbidden, "not_a_participant",
			"agent is not an active participant of this conversation")
		return
	}
	res, err := d.MessageWriter.AddMessage(r.Context(), convservice.AddMessageCommand{
		ConversationID:   conv.ID(),
		SenderIdentityID: conversation.IdentityRef(agentActor(a)),
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionOutbound,
		Content:          req.Content,
		Actor:            observability.Actor(agentActor(a)),
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": string(res.MessageID)})
}

// agentIsActiveParticipant reports whether agent a is an active (non-left)
// participant of conv (v2.7 #185 post_message authz).
func agentIsActiveParticipant(conv *conversation.Conversation, a *agent.Agent) bool {
	want := conversation.IdentityRef(agentActor(a))
	for _, p := range conv.Participants() {
		if p.IdentityID == want && p.IsActive() {
			return true
		}
	}
	return false
}

// --- request_input -----------------------------------------------------------

type requestInputReq struct {
	AgentID  string `json:"agent_id"`
	TaskID   string `json:"task_id"`
	Question string `json:"question"`
}

// requestInputHandler posts the agent's question to the task Conversation AND
// parks the agent's live WorkItem in waiting_input — ATOMICALLY. Both writes run
// inside ONE outer persistence.RunInTx(deps.DB): AddMessage nests its own
// RunInTx (reuses the ambient tx) and WorkItemRepo.Update joins via
// ExecutorFromCtx. If the WaitInput step fails (e.g. no non-terminal WorkItem,
// or an illegal transition), the whole tx rolls back and no message is written.
//
// WorkItem targeting (the agent's NON-TERMINAL WorkItem for the task):
//   - exactly one  → WaitInput(now) + Update
//   - zero         → error (the agent is not actively working the task)
//   - multiple     → shouldn't happen; pick the newest + log a warning
func (s *Server) requestInputHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req requestInputReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.MessageWriter == nil || d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "conversation_not_wired", "")
		return
	}
	if d.DB == nil {
		writeError(w, http.StatusNotImplemented, "db_not_wired", "")
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		writeError(w, http.StatusBadRequest, "missing_question", "")
		return
	}
	if !s.requireOwnTask(w, r, d, a, req.TaskID) {
		return
	}

	var parked *agent.AgentWorkItem
	err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		// (a) post the question to the task Conversation (nests the ambient tx).
		if _, err := s.postAgentMessage(txCtx, d, a, req.TaskID, req.Question); err != nil {
			return err
		}
		// (b) target the agent's NON-TERMINAL WorkItem for the task and park it.
		own, err := s.findOwnWorkItems(txCtx, d.AgentWorkItemRepo, a, req.TaskID)
		if err != nil {
			return err
		}
		var live []*agent.AgentWorkItem
		for _, wi := range own {
			if !wi.Status().IsTerminal() {
				live = append(live, wi)
			}
		}
		if len(live) == 0 {
			// Agent isn't actively working the task — nothing to park. Roll
			// back the message too (atomicity).
			return errNoLiveWorkItem
		}
		target := live[0]
		if len(live) > 1 {
			// Should not happen (one live item per task per agent). Pick the
			// newest by updated_at and warn so the anomaly is auditable.
			for _, wi := range live[1:] {
				if wi.UpdatedAt().After(target.UpdatedAt()) {
					target = wi
				}
			}
			log.Printf("agent-tools request_input: agent=%s task=%s has %d live work items; parking newest %s",
				a.ID(), req.TaskID, len(live), target.ID())
		}
		if err := target.WaitInput(time.Now().UTC()); err != nil {
			return err
		}
		if err := d.AgentWorkItemRepo.Update(txCtx, target); err != nil {
			return err
		}
		// (c) v2.7 D2-e-ii (OQ5 method 甲): emit `agent.awaiting_input` IN THIS
		// SAME outer tx (the outbox repo joins via ExecutorFromCtx). request_input
		// is the ONLY active→waiting_input path, so this is the batch-flush trigger:
		// the WakeProjector consumes it to deliver all the agent's UNREAD messages
		// in the task conversation as ONE merged stdin injection. Atomic with the
		// message + WaitInput — the trigger commits iff the WorkItem is parked. A nil
		// outbox dep (test fixtures not exercising wake) skips silently.
		if err := s.emitAwaitingInput(txCtx, d, a, req.TaskID, target); err != nil {
			return err
		}
		parked = target
		return nil
	})
	if err != nil {
		if errors.Is(err, errNoLiveWorkItem) {
			writeError(w, http.StatusConflict, "no_live_work_item", err.Error())
			return
		}
		if errors.Is(err, agent.ErrWorkItemIllegalMove) {
			// The WorkItem isn't in a state that can move to waiting_input
			// (e.g. still queued — not activated). 422, like other illegal
			// transitions.
			writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())
			return
		}
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"work_item_id": parked.ID(),
		"status":       string(parked.Status()),
	})
}

// awaitingInputOutboxPayload mirrors the JSON the WakeProjector's
// awaitingInputPayload decodes (env service). Kept local so the admin handler
// does not import the env payload type.
type awaitingInputOutboxPayload struct {
	AgentID        string `json:"agent_id"`
	WorkItemID     string `json:"work_item_id"`
	TaskRef        string `json:"task_ref"`
	ConversationID string `json:"conversation_id"`
}

// emitAwaitingInput appends the `agent.awaiting_input` batch-flush trigger to the
// cross-BC outbox INSIDE the caller's tx (request_input's outer RunInTx), so it
// commits atomically with the message + WaitInput. Resolves the task's
// conversation by owner_ref (pm://tasks/{taskID}). A nil OutboxRepo skips the
// emit (nil-tolerant, mirrors MessageWriter.WithOutbox).
func (s *Server) emitAwaitingInput(ctx context.Context, d HandlerDeps, a *agent.Agent, taskID string, wi *agent.AgentWorkItem) error {
	if d.OutboxRepo == nil {
		return nil
	}
	conv, err := d.ConvRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(taskID))
	if err != nil {
		return err
	}
	taskRef := taskRefFor(taskID)
	pb, err := json.Marshal(awaitingInputOutboxPayload{
		AgentID:        string(a.ID()),
		WorkItemID:     wi.ID(),
		TaskRef:        taskRef,
		ConversationID: string(conv.ID()),
	})
	if err != nil {
		return err
	}
	refs, _ := json.Marshal(map[string]string{
		"agent_id":        string(a.ID()),
		"work_item_id":    wi.ID(),
		"task_ref":        taskRef,
		"conversation_id": string(conv.ID()),
	})
	return d.OutboxRepo.Append(ctx, outbox.Event{
		ID:        idgen.MustNewULID(),
		EventType: envservice.EvtAgentAwaitingInput,
		Refs:      string(refs),
		Payload:   string(pb),
		CreatedAt: time.Now().UTC(),
	})
}

// --- block_task --------------------------------------------------------------

type blockTaskReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	Reason  string `json:"reason"`
}

// blockTaskHandler posts the block reason to the task Conversation AND moves the
// Task to blocked via pm.BlockTask — ATOMICALLY (one outer RunInTx; both
// AddMessage and the pm service nest into it). reason is REQUIRED (400 if empty).
func (s *Server) blockTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req blockTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.MessageWriter == nil || d.ConvRepo == nil || d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_or_conversation_not_wired", "")
		return
	}
	if d.DB == nil {
		writeError(w, http.StatusNotImplemented, "db_not_wired", "")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "missing_reason", "blocked requires a reason")
		return
	}
	if !s.requireOwnTask(w, r, d, a, req.TaskID) {
		return
	}
	err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		if _, err := s.postAgentMessage(txCtx, d, a, req.TaskID, req.Reason); err != nil {
			return err
		}
		return d.PMService.BlockTask(txCtx, pm.TaskID(req.TaskID), req.Reason,
			pm.IdentityRef(agentActor(a)))
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "blocked"})
}

// --- complete_task -----------------------------------------------------------

type completeTaskReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	Summary string `json:"summary"`
}

// completeTaskHandler optionally posts a summary to the task Conversation AND
// moves the Task to completed via pm.CompleteTask — ATOMICALLY. The summary is
// optional; when given it is posted in the SAME outer RunInTx as the completion.
func (s *Server) completeTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req completeTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.MessageWriter == nil || d.ConvRepo == nil || d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_or_conversation_not_wired", "")
		return
	}
	if d.DB == nil {
		writeError(w, http.StatusNotImplemented, "db_not_wired", "")
		return
	}
	if !s.requireOwnTask(w, r, d, a, req.TaskID) {
		return
	}
	err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		if strings.TrimSpace(req.Summary) != "" {
			if _, err := s.postAgentMessage(txCtx, d, a, req.TaskID, req.Summary); err != nil {
				return err
			}
		}
		return d.PMService.CompleteTask(txCtx, pm.TaskID(req.TaskID),
			pm.IdentityRef(agentActor(a)))
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "completed"})
}
