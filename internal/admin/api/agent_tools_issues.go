package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// Agent MCP issue-management tools (v2.10.3 T170) — give an agent the SAME
// issue lifecycle a human has in the Web Console: open → discuss (@/thread) →
// edit/close → derive tasks. Before T170 the agent side had only a read-only,
// own-scoped get_issue and had to (mis)use create_task to carry a discussion.
//
// All tools are thin wrappers over the pm AppServices, mirroring the task tools
// in agent_tools_passthrough.go / agent_tools_write.go:
//   - create_issue / update_issue / close_issue / reopen_issue : project-member
//     WRITE-gated INSIDE the AppService (requireProjectMember), so a foreign
//     project → ErrNotMember (403) and a missing project/issue → 404.
//   - commenting on an issue : T200 WS4 routes through the unified post_message
//     (target{type:"issue", id}); the GetIssueForMember gate + the
//     postAgentIssueMessage primitive (owner_ref pm://issues/{id}) live here.
//   - list_issues / list_tasks_of_issue : project-member READ-gated.
// get_issue (relaxed to project-member scope) lives in agent_tools_passthrough.go.
//
// Every tool first runs requireAgentOnWorker (the b1 guardrail: worker proven by
// the TOKEN OWNER, target agent bound to it); agent_id is process-fixed by the
// MCP host, never the model.
// =============================================================================

// --- create_issue ------------------------------------------------------------

type createIssueReq struct {
	AgentID     string   `json:"agent_id"`
	ProjectID   string   `json:"project_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// createIssueHandler creates an Issue via pm.CreateIssue with actor=agent. The
// AppService's requireProjectMember bounds the agent to its own projects (a
// foreign project → ErrNotMember → 403, a missing one → ErrProjectNotFound → 404).
func (s *Server) createIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createIssueReq
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
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "missing_title", "")
		return
	}
	issueID, err := d.PMService.CreateIssue(r.Context(), pmservice.CreateIssueCommand{
		ProjectID:   pm.ProjectID(req.ProjectID),
		Title:       req.Title,
		Description: req.Description,
		Tags:        req.Tags,
		CreatedBy:   pm.IdentityRef(agentActor(a)),
	})
	if err != nil {
		// Mirror create_task's precise project errors (#239): missing project is a
		// 404 (not the misleading "not a member"), foreign project is a 403.
		switch {
		case errors.Is(err, pm.ErrProjectNotFound):
			writeError(w, http.StatusNotFound, "project_not_found",
				"project "+req.ProjectID+" not found"+availableProjectsHint(r.Context(), d, a.OrganizationID(), a.IdentityMemberID()))
			return
		case errors.Is(err, pmservice.ErrNotMember):
			writeError(w, http.StatusForbidden, "not_a_project_member",
				"not a member of project "+req.ProjectID+", please ask an owner to add you")
			return
		}
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issue_id": string(issueID)})
}

// --- update_issue ------------------------------------------------------------

// updateIssueReq uses pointers so an OMITTED field is left unchanged (dirty-only
// patch), matching pmservice.BatchIssuePatch. Editable fields are
// title/description/status/tags (issues have no assignee).
type updateIssueReq struct {
	AgentID     string    `json:"agent_id"`
	IssueID     string    `json:"issue_id"`
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Status      *string   `json:"status"`
	Tags        *[]string `json:"tags"`
}

// updateIssueHandler applies a dirty-only patch to an Issue via pm.BatchUpdateIssue
// (all-or-none in one tx). Project-member gated inside the AppService. An invalid
// status enum → ErrInvalidStatus → 422; a missing issue → 404.
func (s *Server) updateIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req updateIssueReq
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
	if strings.TrimSpace(req.IssueID) == "" {
		writeError(w, http.StatusBadRequest, "missing_issue_id", "")
		return
	}
	if req.Title == nil && req.Description == nil && req.Status == nil && req.Tags == nil {
		writeError(w, http.StatusBadRequest, "empty_patch",
			"update_issue needs at least one of: title, description, status, tags")
		return
	}
	err := d.PMService.BatchUpdateIssue(r.Context(), pm.IssueID(req.IssueID), pmservice.BatchIssuePatch{
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		Tags:        req.Tags,
	}, pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- close_issue / reopen_issue ----------------------------------------------

// closeIssueHandler / reopenIssueHandler are convenience wrappers over
// pm.SetIssueStatus (free status set, NO adjacency enforcement — state is
// self-reported, v2.8.1 @oopslink). close → "closed", reopen → "open" (the
// actionable state). Both are project-member gated inside the AppService.
func (s *Server) closeIssueHandler(w http.ResponseWriter, r *http.Request) {
	s.setIssueStatusHandler(w, r, pm.IssueClosed)
}

func (s *Server) reopenIssueHandler(w http.ResponseWriter, r *http.Request) {
	s.setIssueStatusHandler(w, r, pm.IssueOpen)
}

func (s *Server) setIssueStatusHandler(w http.ResponseWriter, r *http.Request, target pm.IssueStatus) {
	d := hd(r)
	agentID, issueID, ok := readAgentTool2(w, r, "issue_id")
	if !ok {
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, agentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if err := d.PMService.SetIssueStatus(r.Context(), pm.IssueID(issueID), target, pm.IdentityRef(agentActor(a))); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": string(target)})
}

// --- issue commenting --------------------------------------------------------
//
// T200 WS4: the dedicated post_issue_message tool is GONE — agents comment on an
// issue via the unified post_message with target{type:"issue", id:issue_id}
// (postMessageHandler, agent_tools_write.go). That branch runs the SAME
// GetIssueForMember gate this handler used; the postAgentIssueMessage helper
// below is its conversation-resolution + write primitive.

// postAgentIssueMessage appends a message to the issue Conversation as the agent
// (owner_ref pm://issues/{id}) — the issue dual of postAgentMessage. The
// Conversation is created by the issue.created projector, so it exists by the
// time an agent can read/comment.
func (s *Server) postAgentIssueMessage(ctx context.Context, d HandlerDeps, a *agent.Agent, issueID, content string, parentID conversation.MessageID) (conversation.MessageID, error) {
	conv, err := d.ConvRepo.FindByOwnerRef(ctx, conversation.NewIssueOwnerRef(issueID))
	if err != nil {
		return "", err
	}
	res, err := d.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   conv.ID(),
		SenderIdentityID: conversation.IdentityRef(agentActor(a)),
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionOutbound,
		Content:          content,
		ParentMessageID:  parentID,
		Actor:            observability.Actor(agentActor(a)),
	})
	if err != nil {
		return "", err
	}
	return res.MessageID, nil
}

// --- list_issues -------------------------------------------------------------

type listIssuesReq struct {
	AgentID   string   `json:"agent_id"`
	ProjectID string   `json:"project_id"`
	Status    []string `json:"status"`    // optional; one or more issue statuses
	Author    string   `json:"author"`    // optional; exact created_by identity ref
	PageSize  int      `json:"page_size"` // optional; page window (default 50, max 100)
	Offset    int      `json:"offset"`    // optional; rows to skip (default 0)
}

// listIssuesHandler lists a project's issues (optionally filtered by status
// and/or author) — the Issue analogue of list_tasks. SQL-paginated (page_size
// default 50 / max 100, offset), newest-touched first, returning
// {issues,total,page_size,offset,has_more}. Project-member guarded.
func (s *Server) listIssuesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listIssuesReq
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
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = agentListDefaultPageSize
	}
	if pageSize > agentListMaxPageSize {
		pageSize = agentListMaxPageSize
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	statuses := make([]string, 0, len(req.Status))
	for _, st := range req.Status {
		if t := strings.TrimSpace(st); t != "" {
			statuses = append(statuses, t)
		}
	}
	q := pm.OrgListQuery{
		Statuses:  statuses,
		CreatedBy: strings.TrimSpace(req.Author), // exact author (created_by) filter
		SortDesc:  true,                          // newest-touched first (→ updated_at)
		Limit:     pageSize,
		Offset:    offset,
	}
	issues, total, err := d.PMService.ListProjectIssuesPageForMember(r.Context(),
		pm.ProjectID(req.ProjectID), pm.IdentityRef(agentActor(a)), q)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(issues))
	for _, i := range issues {
		out = append(out, agentIssueMap(i))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issues":    out,
		"total":     total,
		"page_size": pageSize,
		"offset":    offset,
		"has_more":  offset+len(out) < total,
	})
}

// --- list_tasks_of_issue -----------------------------------------------------

// listTasksOfIssueHandler lists the tasks DERIVED from an issue (the reverse of
// create_task's derived_from_issue link) — project-member guarded. A missing
// issue → 404.
func (s *Server) listTasksOfIssueHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	agentID, issueID, ok := readAgentTool2(w, r, "issue_id")
	if !ok {
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, agentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	tasks, err := d.PMService.ListTasksDerivedFromIssueForMember(r.Context(),
		pm.IssueID(issueID), pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, agentTaskMap(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out, "total": len(out)})
}
