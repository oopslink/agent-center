package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

// =============================================================================
// IssueRepo — FindByID / FindByProject / FindByStatus / FindByOpener
// =============================================================================

func (s *Server) issueFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueRepo == nil {
		writeError(w, http.StatusNotImplemented, "issue_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	i, err := d.IssueRepo.FindByID(r.Context(), discussion.IssueID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueMap(i))
}

func (s *Server) issueFindByProjectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueRepo == nil {
		writeError(w, http.StatusNotImplemented, "issue_repo_not_wired", "")
		return
	}
	pid := r.URL.Query().Get("project_id")
	if pid == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	filter := discussion.IssueFilter{Limit: 200}
	if v := r.URL.Query().Get("status"); v != "" {
		st := discussion.Status(v)
		filter.Status = &st
	}
	list, err := d.IssueRepo.FindByProject(r.Context(), pid, filter)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, it := range list {
		out[i] = issueMap(it)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) issueFindByStatusHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueRepo == nil {
		writeError(w, http.StatusNotImplemented, "issue_repo_not_wired", "")
		return
	}
	st := r.URL.Query().Get("status")
	if st == "" {
		writeError(w, http.StatusBadRequest, "missing_status", "")
		return
	}
	list, err := d.IssueRepo.FindByStatus(r.Context(), discussion.Status(st),
		discussion.IssueFilter{Limit: 200})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, it := range list {
		out[i] = issueMap(it)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// IssueLifecycleSvc — Open / Conclude / Withdraw / RecordDiscussionStart
// =============================================================================

type issueOpenReq struct {
	ProjectID          string `json:"project_id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	DescriptionBlobRef string `json:"description_blob_ref"`
	OpenedBy           string `json:"opened_by"`
	Origin             string `json:"origin"`
	PrimaryChannelHint string `json:"primary_channel_hint"`
}

func (s *Server) issueOpenHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueLifecycleSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_lifecycle_svc_not_wired", "")
		return
	}
	var req issueOpenReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	opener := req.OpenedBy
	if opener == "" {
		opener = string(d.Actor)
	}
	res, err := d.IssueLifecycleSvc.Open(r.Context(), disservice.OpenIssueCommand{
		ProjectID:          req.ProjectID,
		Title:              req.Title,
		Description:        req.Description,
		DescriptionBlobRef: req.DescriptionBlobRef,
		OpenedByIdentityID: opener,
		Origin:             discussion.Origin(req.Origin),
		PrimaryChannelHint: req.PrimaryChannelHint,
		Actor:              d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":        string(res.IssueID),
		"conversation_id": string(res.ConversationID),
		"event_id":        string(res.EventID),
	})
}

type issueConcludeReq struct {
	IssueID     string                            `json:"issue_id"`
	Kind        string                            `json:"kind"`
	Summary     string                            `json:"summary"`
	Tasks       []dispatch.IssueConcludeTaskSpec  `json:"tasks"`
	ConcludedBy string                            `json:"concluded_by"`
}

func (s *Server) issueConcludeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueLifecycleSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_lifecycle_svc_not_wired", "")
		return
	}
	var req issueConcludeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	concBy := req.ConcludedBy
	if concBy == "" {
		concBy = string(d.Actor)
	}
	res, err := d.IssueLifecycleSvc.Conclude(r.Context(), disservice.ConcludeIssueCommand{
		IssueID: discussion.IssueID(req.IssueID),
		Resolution: discussion.Resolution{
			Kind:    discussion.ResolutionKind(req.Kind),
			Summary: req.Summary,
			Tasks:   req.Tasks,
		},
		ConcludedBy: concBy,
		Actor:       d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	taskIDs := make([]string, len(res.TaskIDs))
	for i, t := range res.TaskIDs {
		taskIDs[i] = string(t)
	}
	evIDs := make([]string, len(res.EventIDs))
	for i, e := range res.EventIDs {
		evIDs[i] = string(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":  string(res.IssueID),
		"task_ids":  taskIDs,
		"event_ids": evIDs,
	})
}

type issueWithdrawReq struct {
	IssueID     string `json:"issue_id"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
	WithdrawnBy string `json:"withdrawn_by"`
}

func (s *Server) issueWithdrawHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueLifecycleSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_lifecycle_svc_not_wired", "")
		return
	}
	var req issueWithdrawReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	by := req.WithdrawnBy
	if by == "" {
		by = string(d.Actor)
	}
	evID, err := d.IssueLifecycleSvc.Withdraw(r.Context(), disservice.WithdrawIssueCommand{
		IssueID:     discussion.IssueID(req.IssueID),
		Reason:      req.Reason,
		Message:     req.Message,
		WithdrawnBy: by,
		Actor:       d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// =============================================================================
// IssueCommentSvc — Comment
// =============================================================================

type issueCommentReq struct {
	IssueID          string `json:"issue_id"`
	Content          string `json:"content"`
	ContentKind      string `json:"content_kind"`
	SenderIdentityID string `json:"sender_identity_id"`
	Direction        string `json:"direction"`
}

func (s *Server) issueCommentHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueCommentSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_comment_svc_not_wired", "")
		return
	}
	var req issueCommentReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	sender := conversation.IdentityRef(req.SenderIdentityID)
	if req.SenderIdentityID == "" {
		sender = conversation.IdentityRef(d.Actor)
	}
	ck := req.ContentKind
	if ck == "" {
		ck = string(conversation.MessageContentText)
	}
	dir := req.Direction
	if dir == "" {
		dir = string(conversation.DirectionInternal)
	}
	res, err := d.IssueCommentSvc.Comment(r.Context(), disservice.CommentInput{
		IssueID:          discussion.IssueID(req.IssueID),
		Content:          req.Content,
		ContentKind:      conversation.MessageContentKind(ck),
		SenderIdentityID: sender,
		Direction:        conversation.MessageDirection(dir),
		Actor:            d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": string(res.MessageID)})
}

// =============================================================================
// IssueBindConversationSvc — BindAuto / BindTo
// =============================================================================

type issueBindAutoReq struct {
	IssueID string `json:"issue_id"`
	Channel string `json:"channel"`
}

func (s *Server) issueBindAutoHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueBindConversationSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_bind_svc_not_wired", "")
		return
	}
	var req issueBindAutoReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	convID, err := d.IssueBindConversationSvc.BindAuto(r.Context(), disservice.BindAutoInput{
		IssueID: discussion.IssueID(req.IssueID),
		Channel: req.Channel,
		Actor:   d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":        req.IssueID,
		"conversation_id": string(convID),
	})
}

type issueBindToReq struct {
	IssueID        string `json:"issue_id"`
	ConversationID string `json:"conversation_id"`
}

func (s *Server) issueBindToHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueBindConversationSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_bind_svc_not_wired", "")
		return
	}
	var req issueBindToReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.IssueBindConversationSvc.BindTo(r.Context(), disservice.BindToInput{
		IssueID:        discussion.IssueID(req.IssueID),
		ConversationID: conversation.ConversationID(req.ConversationID),
		Actor:          d.Actor,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":        req.IssueID,
		"conversation_id": req.ConversationID,
	})
}

// =============================================================================
// IssueLinkConversationSvc — Link
// =============================================================================

type issueLinkReq struct {
	IssueID        string `json:"issue_id"`
	ConversationID string `json:"conversation_id"`
}

func (s *Server) issueLinkHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IssueLinkConversationSvc == nil {
		writeError(w, http.StatusNotImplemented, "issue_link_svc_not_wired", "")
		return
	}
	var req issueLinkReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.IssueLinkConversationSvc.Link(r.Context(), disservice.LinkInput{
		IssueID:        discussion.IssueID(req.IssueID),
		ConversationID: conversation.ConversationID(req.ConversationID),
		Actor:          d.Actor,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":        req.IssueID,
		"conversation_id": req.ConversationID,
	})
}

// =============================================================================
// Projection helpers
// =============================================================================

func issueMap(i *discussion.Issue) map[string]any {
	related := i.RelatedConversationIDs()
	relatedStrs := make([]string, len(related))
	for idx, c := range related {
		relatedStrs[idx] = string(c)
	}
	m := map[string]any{
		"id":                       string(i.ID()),
		"project_id":               i.ProjectID(),
		"title":                    i.Title(),
		"description":              i.Description(),
		"description_blob_ref":     i.DescriptionBlobRef(),
		"status":                   string(i.Status()),
		"opened_by":                i.OpenedByIdentityID(),
		"origin":                   string(i.Origin()),
		"opened_at":                i.OpenedAt().Format(time.RFC3339Nano),
		"conversation_id":          string(i.ConversationID()),
		"related_conversation_ids": relatedStrs,
		"version":                  i.Version(),
	}
	if ca := i.ConcludedAt(); ca != nil {
		m["concluded_at"] = ca.Format(time.RFC3339Nano)
		m["concluded_by"] = i.ConcludedByIdentityID()
	}
	return m
}
