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
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// errNoLiveWorkItem signals that the agent holds a WorkItem for the task but
// none of them are non-terminal — there is nothing to park in waiting_input, so
// request_input is rejected and its whole tx rolls back.
var errNoLiveWorkItem = errors.New("agent has no live work item for this task")

// =============================================================================
// Agent MCP write tools — explicit human-visible communication (v2.7 D2-b2,
// ADR-0049). The agent (via its Worker daemon) posts to the task it is WORKING:
//
//	post_message       — append a message to a DM/channel, task, or issue   (1 write)
//	                     (T200 WS4: target{type,id} unifies the former post_message/
//	                     post_task_message/post_issue_message trio)
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
func (s *Server) postAgentMessage(ctx context.Context, d HandlerDeps, a *agent.Agent, taskID, content string, parentID conversation.MessageID) (conversation.MessageID, error) {
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
		ParentMessageID:  parentID, // F4: thread the reply when replying inside a thread
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
	if len(own) > 0 {
		return true
	}
	// Pool-claim path: a task claimed via claim_task is ASSIGNED to the agent and
	// moved to running, but ClaimPoolTask mints NO WorkItem (pool tasks have none).
	// Without this fallback the work-item scope alone would 403 the claimer from
	// completing / blocking / reading its OWN claimed task (the deterministic
	// not_agents_task bug). Accept the agent when it is the task's current assignee
	// — the pool-claim owner. assign_task-assigned tasks already pass above via the
	// WorkItem the assign flow mints, so this newly-permits only the legitimate
	// pool claimer. Unwired PMService degrades to the stricter work-item-only scope.
	if d.PMService != nil {
		if task, terr := d.PMService.GetTask(r.Context(), pm.TaskID(taskID)); terr == nil {
			if string(task.Assignee()) == agentActor(a) {
				return true
			}
		}
	}
	writeError(w, http.StatusForbidden, "not_agents_task",
		"agent has no work item for this task")
	return false
}

// requireTaskAccess is the RELAXED task gate (T183) for the tools an agent may use
// on a task it can SEE but is not actively WORKING — post_task_message and
// discard_task. The agent is authorized when ANY of:
//
//	(a) it holds a WorkItem for pm://tasks/{taskID}  (own-work scope), OR
//	(b) it CREATED the task                          (creator), OR
//	(c) it is a MEMBER of the task's project         (project membership).
//
// This closes the gap (PD-reported) where an agent that create_task'd a task could
// neither post to nor discard it without first self-assigning to mint a WorkItem
// (the hacky assign_task workaround) — both tools previously went through
// requireOwnTask and 403'd not_agents_task. Authz keys on the agent's identity-
// member ref (agentActor), exactly like the pm requireProjectMember + the
// get_my_profile membership scan. Fail-closed: a non-member of another
// project/org still gets 403, and existence is NOT disclosed (a missing/unseeable
// task is the same 403 as no-access). taskID-missing is 400; an unwired PMService
// degrades to the work-item-only scope (stricter, never looser).
//
// For discard_task the pm service ALSO enforces requireProjectMember, so this gate
// only widens the early check to match; for post_task_message there is no further
// service-side gate (MessageWriter.AddMessage does not check participation), so
// this IS the authorization boundary — hence the strict creator/member fail-close.
func (s *Server) requireTaskAccess(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, taskID string) bool {
	if strings.TrimSpace(taskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return false
	}
	if d.AgentWorkItemRepo == nil {
		writeError(w, http.StatusNotImplemented, "work_item_repo_not_wired", "")
		return false
	}
	ctx := r.Context()
	// (a) own-work scope — the agent holds a WorkItem for the task.
	own, err := s.findOwnWorkItems(ctx, d.AgentWorkItemRepo, a, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return false
	}
	if len(own) > 0 {
		return true
	}
	const denyMsg = "agent is neither working this task nor the creator/a member of its project"
	// (b)+(c) creator or project member — needs the task (creator + project) and
	// its members. Without a wired PMService we can only honor the work-item scope.
	if d.PMService == nil {
		writeError(w, http.StatusForbidden, "not_agents_task", denyMsg)
		return false
	}
	task, err := d.PMService.GetTask(ctx, pm.TaskID(taskID))
	if err != nil {
		if errors.Is(err, pm.ErrTaskNotFound) {
			// Non-disclosure: a missing/unseeable task is the same 403 as no-access.
			writeError(w, http.StatusForbidden, "not_agents_task", denyMsg)
			return false
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return false
	}
	self := agentActor(a)
	// (b) creator.
	if string(task.CreatedBy()) == self {
		return true
	}
	// (c) project member.
	members, err := d.PMService.ListMembers(ctx, task.ProjectID())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return false
	}
	for _, m := range members {
		if string(m.IdentityID()) == self {
			return true
		}
	}
	writeError(w, http.StatusForbidden, "not_agents_task", denyMsg)
	return false
}

// backlogActionMsg builds the UNIFIED T190 guidance shown when an agent acts on a
// BACKLOG (inert) task — claim / start / complete / block. `action` is the verb-ing
// ("claiming", "starting", "completing", "blocking"); the base text is
// pm.BacklogNotActionableHint so the message has ONE source across all surfaces.
func backlogActionMsg(action string) string {
	return pm.BacklogNotActionableHint + " before " + action
}

// writeBacklogNotActionable writes the unified 409 `task_backlog_not_actionable`
// envelope (T190) — the single code returned by claim_task / start_task /
// complete_task / block_task when the target is a backlog (inert) task.
func writeBacklogNotActionable(w http.ResponseWriter, action string) {
	writeError(w, http.StatusConflict, "task_backlog_not_actionable", backlogActionMsg(action))
}

// rejectIfBacklog is the SHARED backlog gate (T190) reused by claim_task /
// complete_task / block_task. It loads the task and, when it is INERT BACKLOG, writes
// the unified `task_backlog_not_actionable` error and returns true (the caller must
// return). "Inert backlog" = pm.IsBacklogInert(planID) (no plan) AND the task is
// still in a PRE-START state (open / reopened) — i.e. it was never placed for work.
// A task already `running` is IN MOTION (the agent is working it), so block/complete
// are legitimate self-reports and are NOT gated here; a terminal task falls through
// to the normal path's illegal_transition. (In production a running task always has
// planID!="" — it reached running via a plan or the pool — so the status guard only
// ever spares in-motion tasks, never a truly inert one.)
//
// It returns false — WITHOUT writing — when the task is in a plan/pool, is past the
// pre-start phase, OR cannot be loaded; the caller's normal gate (ClaimPoolTask /
// requireOwnTask) then surfaces the right error (not_found, not_agents_task, …). A
// nil PMService degrades to "can't tell" (false), never looser. start_task converges
// on the same envelope via writeWorkStateError (its backlog signal is the agent-BC
// ErrWorkItemTaskNotRunnable sentinel from the runnable gate, not a task load).
func (s *Server) rejectIfBacklog(w http.ResponseWriter, r *http.Request, d HandlerDeps, taskID, action string) bool {
	if d.PMService == nil {
		return false
	}
	task, err := d.PMService.GetTask(r.Context(), pm.TaskID(taskID))
	if err != nil {
		return false // not-found / load error → let the caller's normal gate surface it
	}
	if !pm.IsBacklogInert(task.PlanID()) {
		return false // in a real plan or dispatched into the pool → actionable
	}
	switch task.Status() {
	case pm.TaskOpen, pm.TaskReopened:
		writeBacklogNotActionable(w, action)
		return true
	default:
		return false // running / terminal — in motion, not inert
	}
}

// --- post_message (v2.7 #185; T200 WS4 unification) --------------------------

// agentAttachmentReq is the wire shape for one file an agent attaches to a
// message (T44) — a reference to an ALREADY-UPLOADED blob (ac://files/{ulid})
// plus display metadata. The agent uploads via upload_file first, then names the
// returned file_uri here. Mirrors the human msgAttachmentJSON.
type agentAttachmentReq struct {
	URI      string `json:"uri"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// postTargetReq is the T200 WS4 discriminated target of post_message: it replaces
// the three sibling tools (post_message-by-conversation_id, post_task_message,
// post_issue_message) with ONE tool that routes by Type. Only the ENTRY POINT is
// unified — each branch runs the SAME conversation resolution + authorization
// gate the dedicated tool used to run, so behavior and the authz red-line are
// byte-for-byte preserved (issue I10: "只动外形,不动底层行为与授权语义").
type postTargetReq struct {
	// Type is one of postTarget{Conversation,Task,Issue}. "conversation" covers
	// both a DM and a channel (addressed by conversation_id); "task"/"issue"
	// resolve the owner-ref conversation behind the scenes.
	Type string `json:"type"`
	// ID is the conversation_id (conversation), task_id (task), or issue_id (issue).
	ID string `json:"id"`
}

// The three post_message target kinds.
const (
	postTargetConversation = "conversation"
	postTargetTask         = "task"
	postTargetIssue        = "issue"
)

type postMessageReq struct {
	AgentID string        `json:"agent_id"`
	Target  postTargetReq `json:"target"`
	Content string        `json:"content"`
	// ParentMessageID (v2.9.1 Thread F4): when the agent was @mentioned inside a
	// thread, the thread root id — so its reply lands in-thread instead of at
	// conversation top-level. Empty for an ordinary top-level message.
	ParentMessageID string `json:"parent_message_id"`
	// Attachments (T44): already-uploaded files to attach to this message — the
	// agent-side dual of the human chat-box attachment. Optional.
	Attachments []agentAttachmentReq `json:"attachments"`
}

type startDMReq struct {
	AgentID       string `json:"agent_id"`
	TargetAgent   string `json:"target_agent"`
	TargetAgentID string `json:"target_agent_id"`
	Content       string `json:"content"`
	Reason        string `json:"reason"`
}

// startDMHandler lets an agent open or reuse a same-org 1:1 DM with another
// agent and place the first message in it. It deliberately goes through
// MessageWriter.OpenConversation so T288's DM-key dedup and the normal
// conversation.message_added outbox/wake path are reused instead of duplicated.
func (s *Server) startDMHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req startDMReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.MessageWriter == nil || d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_dm_not_wired", "")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "missing_content", "")
		return
	}
	target := strings.TrimSpace(req.TargetAgent)
	if target == "" {
		target = strings.TrimSpace(req.TargetAgentID)
	}
	if target == "" {
		writeError(w, http.StatusBadRequest, "missing_target_agent", "target_agent is required")
		return
	}
	target = strings.TrimPrefix(target, "agent:")
	targetAgent, err := d.AgentSvc.ResolveAgent(r.Context(), target)
	if err != nil {
		if errors.Is(err, agent.ErrAgentNotFound) {
			writeError(w, http.StatusNotFound, "target_agent_not_found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if targetAgent.OrganizationID() != a.OrganizationID() {
		writeError(w, http.StatusForbidden, "cross_org_agent_dm_forbidden", "target agent is not in the same organization")
		return
	}
	selfRef := conversation.IdentityRef(agentActor(a))
	targetRef := conversation.IdentityRef(agentActor(targetAgent))
	if selfRef == targetRef {
		writeError(w, http.StatusBadRequest, "self_dm_not_allowed", "target_agent must be another agent")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	parts := []conversation.ParticipantElement{
		{IdentityID: selfRef, Role: "owner", JoinedAt: now, JoinedBy: selfRef},
		{IdentityID: targetRef, Role: "member", JoinedAt: now, JoinedBy: selfRef},
	}
	var open convservice.OpenResult
	var msg convservice.AddMessageResult
	run := func(ctx context.Context) error {
		var oerr error
		open, oerr = d.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
			Kind:           conversation.ConversationKindDM,
			OrganizationID: a.OrganizationID(),
			Participants:   parts,
			CreatedBy:      selfRef,
			Actor:          observability.Actor(selfRef),
		})
		if oerr != nil {
			return oerr
		}
		var merr error
		msg, merr = d.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   open.ConversationID,
			SenderIdentityID: selfRef,
			ContentKind:      conversation.MessageContentText,
			Direction:        conversation.DirectionOutbound,
			Content:          content,
			Actor:            observability.Actor(selfRef),
		})
		return merr
	}
	if d.DB != nil {
		err = persistence.RunInTx(r.Context(), d.DB, run)
	} else {
		err = run(r.Context())
	}
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id":  string(open.ConversationID),
		"message_id":       string(msg.MessageID),
		"reused":           open.Existing,
		"target_agent_id":  string(targetAgent.ID()),
		"target_agent_ref": string(targetRef),
	})
}

// postMessageHandler appends a message, as the agent, to a DM/channel, a task,
// or an issue — selected by req.Target.Type (T200 WS4). It UNIFIES the former
// post_message / post_task_message / post_issue_message trio: one tool, one
// surface, three resolution+authz branches kept VERBATIM:
//
//   - conversation: resolve by conversation_id, authz = active participant
//     (v2.7 #185 — the DM/channel reply path; #246 actionable not-found/not-member).
//   - task: authz = requireTaskAccess (T183 own-work OR creator OR project member),
//     resolve the task's owner-ref conversation.
//   - issue: authz = PMService.GetIssueForMember (project membership), resolve the
//     issue's owner-ref conversation.
//
// T44: the message may carry attachments — the agent-side dual of the human
// chat-box attachment. Each attachment names an already-uploaded blob the agent
// can reach in its OWN domain (agentReachable); the message stores the attachment
// metadata AND a {ScopeConversation, convID} reference is added in the SAME tx so
// the file is downloadable by the conversation's other participants (humans +
// agents). A not-reachable attachment is 403'd before the message lands.
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
	targetType := strings.TrimSpace(req.Target.Type)
	targetID := strings.TrimSpace(req.Target.ID)
	if targetType == "" || targetID == "" {
		writeError(w, http.StatusBadRequest, "missing_target",
			"target.type (one of conversation|task|issue) and target.id are required")
		return
	}
	// Content is required UNLESS the message carries at least one attachment — an
	// attachment-only message (just a file, no text) is valid, mirroring the human
	// chat box.
	if strings.TrimSpace(req.Content) == "" && len(req.Attachments) == 0 {
		writeError(w, http.StatusBadRequest, "missing_content", "")
		return
	}

	// Resolve the target → its Conversation, running the SAME per-target authz gate
	// the dedicated tool used to run (issue I10: shape-only, authz unchanged). Each
	// branch writes its own error envelope and returns on denial.
	var conv *conversation.Conversation
	switch targetType {
	case postTargetConversation:
		c, err := d.ConvRepo.FindByID(r.Context(), conversation.ConversationID(targetID))
		if err != nil {
			// v2.7.1 #246 (a): a missing/typo'd conversation_id gets a clear, actionable
			// 404 (not an opaque error) — point the agent at find_org_channel to get a
			// valid id rather than hallucinate one.
			if errors.Is(err, conversation.ErrConversationNotFound) {
				writeError(w, http.StatusNotFound, "conversation_not_found",
					"conversation "+targetID+" not found — use find_org_channel to resolve a channel name to its id")
				return
			}
			mapDomainError(w, err)
			return
		}
		if !agentIsActiveParticipant(c, a) {
			// v2.7.1 #246 (3): precise not-member message for channels (the write-gate is
			// unchanged — #224/#227 participant gate still 403s; only the wording is
			// actionable). β boundary HELD: visibility via find_org_channel does NOT grant
			// write — a non-member must still be added.
			if c.Kind() == conversation.ConversationKindChannel {
				writeError(w, http.StatusForbidden, "not_a_channel_member",
					"not a member of channel "+c.Name()+" — ask an owner to add you before posting")
				return
			}
			writeError(w, http.StatusForbidden, "not_a_participant",
				"agent is not an active participant of this conversation")
			return
		}
		conv = c
	case postTargetTask:
		// T183 relaxed gate — own-work OR creator OR project member (writes its own
		// 400/403/501 on denial; this IS the authorization boundary for task posts).
		if !s.requireTaskAccess(w, r, d, a, targetID) {
			return
		}
		c, err := d.ConvRepo.FindByOwnerRef(r.Context(), conversation.NewTaskOwnerRef(targetID))
		if err != nil {
			mapDomainError(w, err)
			return
		}
		conv = c
	case postTargetIssue:
		if d.PMService == nil {
			writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
			return
		}
		// Membership gate (also resolves ErrIssueNotFound → 404). Same project-member
		// scope as get_issue, so a commenter can always read.
		if _, err := d.PMService.GetIssueForMember(r.Context(), pm.IssueID(targetID), pm.IdentityRef(agentActor(a))); err != nil {
			mapDomainError(w, err)
			return
		}
		c, err := d.ConvRepo.FindByOwnerRef(r.Context(), conversation.NewIssueOwnerRef(targetID))
		if err != nil {
			mapDomainError(w, err)
			return
		}
		conv = c
	default:
		writeError(w, http.StatusBadRequest, "invalid_target_type",
			"target.type must be one of: conversation, task, issue")
		return
	}

	// T44: resolve + authorize attachments BEFORE any write. Each named file must
	// parse and be reachable in the agent's OWN domain (e.g. uploaded by the agent
	// via upload_file). A bad URI or an unreachable file → 403 before the message
	// lands (atomic: no partial post).
	atts, fileURIs, ok := s.resolveAgentAttachments(w, r, d, a, req.Attachments)
	if !ok {
		return
	}

	add := func(ctx context.Context) (convservice.AddMessageResult, error) {
		res, aerr := d.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   conv.ID(),
			SenderIdentityID: conversation.IdentityRef(agentActor(a)),
			ContentKind:      conversation.MessageContentText,
			Direction:        conversation.DirectionOutbound,
			Content:          req.Content,
			ParentMessageID:  conversation.MessageID(req.ParentMessageID), // F4: reply in-thread
			Attachments:      atts,
			Actor:            observability.Actor(agentActor(a)),
		})
		if aerr != nil {
			return convservice.AddMessageResult{}, aerr
		}
		// Place a conversation-scope reference per attachment so the file becomes
		// downloadable by the conversation's OTHER participants (humans reach it via
		// their conversation membership; agents via agentParticipantConvScopes). Same
		// tx as AddMessage — mirrors the human sendMessageHandler. Idempotent: if the
		// agent already anchored this file in THIS conversation (e.g. it called
		// upload_file with scope=conversation,scope_id=convID), we skip — AddReference
		// does not dedup, so without this the natural upload→post flow would leave two
		// identical live references.
		for i, fileURI := range fileURIs {
			att := req.Attachments[i]
			exists, cerr := s.convRefExists(ctx, d, fileURI, conv.ID())
			if cerr != nil {
				return convservice.AddMessageResult{}, cerr
			}
			if exists {
				continue
			}
			if _, rerr := d.FilesSvc.AddReference(ctx, filesservice.AddReferenceCmd{
				FileURI:   fileURI,
				Scope:     files.ScopeConversation,
				ScopeID:   string(conv.ID()),
				Filename:  att.Filename,
				MimeType:  att.MimeType,
				SizeBytes: att.Size,
				CreatedBy: agentActor(a),
			}); rerr != nil {
				return convservice.AddMessageResult{}, rerr
			}
		}
		return res, nil
	}

	var res convservice.AddMessageResult
	var err error
	if len(fileURIs) > 0 {
		if d.DB == nil {
			writeError(w, http.StatusNotImplemented, "db_not_wired", "database not wired")
			return
		}
		err = persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
			var txErr error
			res, txErr = add(txCtx)
			return txErr
		})
	} else {
		res, err = add(r.Context())
	}
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": string(res.MessageID)})
}

// resolveAgentAttachments validates + maps the request attachments to domain
// MessageAttachments and parsed FileURIs, enforcing per-file own-domain
// reachability. On any failure it writes the error envelope and returns ok=false.
// An empty input is a no-op success (nil, nil, true). Requires FilesSvc when any
// attachment is present.
func (s *Server) resolveAgentAttachments(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, in []agentAttachmentReq) ([]conversation.MessageAttachment, []files.FileURI, bool) {
	if len(in) == 0 {
		return nil, nil, true
	}
	if d.FilesSvc == nil {
		writeError(w, http.StatusNotImplemented, "files_not_wired", "files service not wired")
		return nil, nil, false
	}
	atts := make([]conversation.MessageAttachment, 0, len(in))
	fileURIs := make([]files.FileURI, 0, len(in))
	for _, att := range in {
		fileURI, perr := files.ParseFileURI(att.URI)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid_file_uri", perr.Error())
			return nil, nil, false
		}
		reachable, rerr := s.agentReachable(d, r, a, fileURI)
		if rerr != nil {
			writeError(w, http.StatusInternalServerError, "reachability_failed", rerr.Error())
			return nil, nil, false
		}
		if !reachable {
			// Fail-closed: the agent named a file it cannot reach in its own domain
			// (not uploaded by it / not in a task or conversation it is in).
			writeError(w, http.StatusForbidden, "attachment_not_reachable",
				"attachment "+att.URI+" is not reachable in the agent's own domain — upload it first (upload_file) or attach a file already in your domain")
			return nil, nil, false
		}
		atts = append(atts, conversation.MessageAttachment{
			URI: att.URI, Filename: att.Filename, MimeType: att.MimeType, Size: att.Size,
		})
		fileURIs = append(fileURIs, fileURI)
	}
	return atts, fileURIs, true
}

// convRefExists reports whether a LIVE {ScopeConversation, convID} reference to
// fileURI already exists — used to keep post_message's reference-add idempotent
// (AddReference does not dedup). A nil FilesSvc → false (the caller would not
// reach here). A repo error is propagated so the enclosing tx rolls back.
func (s *Server) convRefExists(ctx context.Context, d HandlerDeps, fileURI files.FileURI, convID conversation.ConversationID) (bool, error) {
	if d.FilesSvc == nil {
		return false, nil
	}
	refs, err := d.FilesSvc.ListReferences(ctx, fileURI)
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		if ref.Scope == files.ScopeConversation && ref.ScopeID == string(convID) && ref.IsLive() {
			return true, nil
		}
	}
	return false, nil
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
		if _, err := s.postAgentMessage(txCtx, d, a, req.TaskID, req.Question, ""); err != nil {
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
	AgentID    string `json:"agent_id"`
	TaskID     string `json:"task_id"`
	Reason     string `json:"reason"`
	ReasonType string `json:"reason_type"`
}

// blockTaskHandler posts the block reason to the task Conversation AND records the
// blocked annotation via pm.BlockTask — ATOMICALLY (one outer RunInTx; both
// AddMessage and the pm service nest into it). reason is REQUIRED (400 if empty).
// reason_type classifies the block (v2.14.0 I14 §四): input_required (the agent
// needs a user reply) or obstacle (an external blocker needs owner/PM
// intervention); it defaults to obstacle when omitted.
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
	// reason_type defaults to obstacle when omitted; the pm service rejects any
	// other invalid value (pm.ErrInvalidBlockReasonType → 4xx).
	reasonType := pm.BlockReasonType(req.ReasonType)
	if strings.TrimSpace(req.ReasonType) == "" {
		reasonType = pm.BlockReasonObstacle
	}
	// T190: a backlog (inert) task cannot be blocked — surface the unified
	// add-to-plan/pool guidance instead of the misleading not_agents_task (a backlog
	// task has no WorkItem, so requireOwnTask would otherwise 403 it).
	if s.rejectIfBacklog(w, r, d, req.TaskID, "blocking") {
		return
	}
	if !s.requireOwnTask(w, r, d, a, req.TaskID) {
		return
	}
	err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		if _, err := s.postAgentMessage(txCtx, d, a, req.TaskID, req.Reason, ""); err != nil {
			return err
		}
		return d.PMService.BlockTask(txCtx, pm.TaskID(req.TaskID), req.Reason, reasonType,
			pm.IdentityRef(agentActor(a)))
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "blocked"})
}

// --- unblock_task (v2.9.1 P0 recovery) ---------------------------------------

type unblockTaskReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

// unblockTaskHandler RECOVERS a blocked task: blocked→running plus a fresh
// re-dispatch (UnblockTask emits pm.task.assigned → the WorkItemProjector mints a
// NEW WorkItem, re-waking the assignee). This is the recovery entry point for the
// "restart / stale-release → deadlocked blocked" class (v2.9.1 P0): a Task blocked
// with reason "agent execution failed" otherwise had no path back to executable.
//
// Cross-agent BY DESIGN — an owner/PD recovers ANOTHER agent's stuck task — so it
// does NOT requireOwnTask; the pm service enforces project membership (+ rejects an
// archived project). Unblocking a non-blocked task is an illegal transition (4xx).
func (s *Server) unblockTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req unblockTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if err := d.PMService.UnblockTask(r.Context(), pm.TaskID(req.TaskID), pm.IdentityRef(agentActor(a))); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "running"})
}

// --- rerun_failed_node (v2.9.1 P0 recovery, plan-aware) ----------------------

type rerunFailedNodeReq struct {
	AgentID string `json:"agent_id"`
	PlanID  string `json:"plan_id"`
	TaskID  string `json:"task_id"`
}

// rerunFailedNodeHandler clears a plan node's dispatch record (pm RerunFailedNode)
// so the next plan advance re-dispatches it — the plan-aware companion to
// unblock_task for recovering a stuck node (v2.9.1 P0). Project-member guarded by
// the service.
func (s *Server) rerunFailedNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req rerunFailedNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if err := d.PMService.RerunFailedNode(r.Context(), pm.PlanID(req.PlanID), pm.TaskID(req.TaskID), pm.IdentityRef(agentActor(a))); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- resume_paused_node (T53) -----------------------------------------------

type resumePausedNodeReq struct {
	AgentID string `json:"agent_id"`
	PlanID  string `json:"plan_id"`
	TaskID  string `json:"task_id"`
}

// resumePausedNodeHandler is the operator recovery action: a project-member agent
// (PD) resumes a plan node whose agent paused its work item and went idle (shown
// `paused` since T53). pm authorizes (project member + plan running) then resumes
// the node's paused work item + wakes its agent so it continues. Authz = the
// calling agent's project membership (agentActor), exactly like rerun_failed_node.
func (s *Server) resumePausedNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req resumePausedNodeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if err := d.PMService.ResumePausedNode(r.Context(), pm.PlanID(req.PlanID), pm.TaskID(req.TaskID), pm.IdentityRef(agentActor(a))); err != nil {
		writeResumePausedNodeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// writeResumePausedNodeError maps the T53 resume errors to HTTP. ErrNodeNotPaused
// → 409 (nothing to resume), ErrAgentHasActiveWork → 409 (the agent is busy),
// ErrPlanNotRunning → 409, ErrTaskNotInPlan → 404; the rest fall through to the
// shared domain mapper (project-member 403 → 404 existence-non-disclosure, etc.).
func writeResumePausedNodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pmservice.ErrNodeNotPaused):
		writeError(w, http.StatusConflict, "node_not_paused", "the plan node has no paused work item to resume")
	case errors.Is(err, agent.ErrAgentHasActiveWork):
		writeError(w, http.StatusConflict, "agent_busy", "the node's agent is busy on another work item; try again after it settles")
	case errors.Is(err, pm.ErrPlanNotRunning):
		writeError(w, http.StatusConflict, "plan_not_running", "the plan is not running")
	case errors.Is(err, pmservice.ErrTaskNotInPlan):
		writeError(w, http.StatusNotFound, "task_not_in_plan", "the task is not a node of this plan")
	case errors.Is(err, pmservice.ErrNodeResumerUnavailable):
		writeError(w, http.StatusNotImplemented, "resume_not_wired", "paused-node resume is not available")
	default:
		mapDomainError(w, err)
	}
}

// --- complete_task -----------------------------------------------------------

type completeTaskReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	Summary string `json:"summary"`
	// Outcome (v2.13.0 I18/B1): for a DECISION node, the outcome label (e.g.
	// "pass"/"reject") that routes its conditional/loopback out-edges. Empty for an
	// ordinary task complete (no routing).
	Outcome string `json:"outcome"`
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
	// T190: a backlog (inert) task cannot be completed — unified guidance before the
	// own-work scope check (which would 403 not_agents_task for the missing WorkItem).
	if s.rejectIfBacklog(w, r, d, req.TaskID, "completing") {
		return
	}
	if !s.requireOwnTask(w, r, d, a, req.TaskID) {
		return
	}
	// B3 (v2.13.0 I18): when the agent completes a DECISION node WITHOUT an explicit
	// outcome, auto-derive it from the §-1 gate verdict + open review comments. This
	// is read-only + does git/CI I/O, so it runs BEFORE the tx (like F3's
	// guardIntegrateMerge pre-check); the derived outcome is recorded INSIDE the tx.
	// Best-effort: any error / non-decision node leaves auto empty ⇒ pre-B3 behaviour
	// (the agent's manual outcome, if any). A decision that B3 cannot resolve
	// (auto.IsDecision && !auto.Decided) is notified to a human AFTER the tx.
	manualOutcome := strings.TrimSpace(req.Outcome)
	var auto pmservice.AutoDecision
	if manualOutcome == "" {
		auto, _ = d.PMService.ComputeAutoDecision(r.Context(), pm.TaskID(req.TaskID))
	}
	err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		if strings.TrimSpace(req.Summary) != "" {
			if _, err := s.postAgentMessage(txCtx, d, a, req.TaskID, req.Summary, ""); err != nil {
				return err
			}
		}
		if err := d.PMService.CompleteTask(txCtx, pm.TaskID(req.TaskID),
			pm.IdentityRef(agentActor(a))); err != nil {
			return err
		}
		// Record the decision node's outcome in the SAME tx, so the subsequent
		// auto-advance routes its conditional/loopback edges. The manual outcome (B1)
		// wins; absent one, B3's auto-derived outcome (when decided) is used. No-op
		// for an ordinary task / an undecided decision (left for a human).
		outcome := manualOutcome
		if outcome == "" && auto.Decided {
			outcome = auto.Outcome
		}
		if outcome != "" {
			return d.PMService.SetDecisionOutcome(txCtx, pm.TaskID(req.TaskID),
				outcome, pm.IdentityRef(agentActor(a)))
		}
		return nil
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	// B3: a decision node B3 could not auto-resolve → ping a human to rule manually
	// (best-effort, post-commit so it never blocks/aborts the completion).
	if manualOutcome == "" && auto.IsDecision && !auto.Decided {
		_ = d.PMService.NotifyDecisionDeferred(r.Context(), pm.TaskID(req.TaskID), auto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "completed"})
}

// --- discard_task (T119) -----------------------------------------------------

type discardTaskReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	Reason  string `json:"reason"`
}

// discardTaskHandler is the agent-facing path to DISCARD a non-terminal Task
// (open/running → discarded), for a superseded or mis-created task. It mirrors
// complete_task: an OPTIONAL reason is posted to the task Conversation first, then
// pm.DiscardTask runs — ATOMICALLY (one outer RunInTx). The pm service rejects a
// terminal task (completed/discarded → illegal transition → 4xx). Closes the
// agent-tools gap (the discard capability existed in the service + Web UI only).
func (s *Server) discardTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req discardTaskReq
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
	// T183: relaxed gate — the creator / a project member may discard even without
	// a WorkItem. pm.DiscardTask ALSO enforces requireProjectMember, so this early
	// gate just widens to match (and gives a clean 403 + non-disclosure).
	if !s.requireTaskAccess(w, r, d, a, req.TaskID) {
		return
	}
	err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		if strings.TrimSpace(req.Reason) != "" {
			if _, err := s.postAgentMessage(txCtx, d, a, req.TaskID, req.Reason, ""); err != nil {
				return err
			}
		}
		return d.PMService.DiscardTask(txCtx, pm.TaskID(req.TaskID),
			pm.IdentityRef(agentActor(a)))
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "discarded"})
}

// --- set_task_issue (T192) ---------------------------------------------------

type setTaskIssueReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	// IssueID is the derived_from_issue to link; "" CLEARS the link. The issue must
	// EXIST and belong to the task's project (validated in UpdateTask).
	IssueID string `json:"issue_id"`
}

// setTaskIssueHandler is the agent-facing path to (re)set or CLEAR a task's
// derived_from_issue AFTER creation (T192) — the link was previously create-only, so
// a missed/wrong link could not be corrected. It always sends a non-nil
// DerivedFromIssue (issue_id, possibly "") so the field is applied. Authorized by the
// relaxed requireTaskAccess gate (creator / project member / own-work) — the same
// surface as discard_task: a PD that created or owns the task may fix its link without
// holding a WorkItem. UpdateTask enforces existence + same-project (404 not_found /
// 409 derived_issue_project_mismatch). Returns the resulting link.
func (s *Server) setTaskIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req setTaskIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	// Relaxed gate — creator / project member may fix the link without a WorkItem.
	if !s.requireTaskAccess(w, r, d, a, req.TaskID) {
		return
	}
	issueID := pm.IssueID(strings.TrimSpace(req.IssueID))
	if err := d.PMService.UpdateTask(r.Context(), pmservice.UpdateTaskCommand{
		TaskID: pm.TaskID(req.TaskID), DerivedFromIssue: &issueID,
		Actor: pm.IdentityRef(agentActor(a)),
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "task_id": req.TaskID, "derived_from_issue": string(issueID),
	})
}
