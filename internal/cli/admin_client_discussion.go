// Package cli — admin_client_discussion.go: Client methods for the
// Discussion BC admin surface (issue lifecycle / comment / bind / link).
// Mirrors internal/admin/api/discussion.go 1:1.
//
// Naming: methods on Client are named <Resource><Verb> to match the
// admin route segments (e.g. `IssueOpen` for
// `POST /admin/discussion/issue/open`). Read methods return typed DTO
// structs whose JSON tags match the JSON keys emitted by the admin
// endpoint's `issueMap` projection helper.
package cli

import "context"

// =============================================================================
// DTOs — JSON shape returned by admin/api/discussion.go::issueMap.
// =============================================================================

// IssueDTO mirrors admin api issueMap.
type IssueDTO struct {
	ID                     string   `json:"id"`
	ProjectID              string   `json:"project_id"`
	Title                  string   `json:"title"`
	Description            string   `json:"description"`
	DescriptionBlobRef     string   `json:"description_blob_ref"`
	Status                 string   `json:"status"`
	OpenedBy               string   `json:"opened_by"`
	Origin                 string   `json:"origin"`
	OpenedAt               string   `json:"opened_at"`
	ConversationID         string   `json:"conversation_id"`
	RelatedConversationIDs []string `json:"related_conversation_ids"`
	Version                int      `json:"version"`
	ConcludedAt            string   `json:"concluded_at,omitempty"`
	ConcludedBy            string   `json:"concluded_by,omitempty"`
}

// =============================================================================
// Request payloads — match admin/api request structs.
// =============================================================================

// IssueOpenRequest mirrors api issueOpenReq.
type IssueOpenRequest struct {
	ProjectID          string `json:"project_id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	DescriptionBlobRef string `json:"description_blob_ref"`
	OpenedBy           string `json:"opened_by"`
	Origin             string `json:"origin"`
	PrimaryChannelHint string `json:"primary_channel_hint"`
}

// IssueOpenResponse mirrors api success body.
type IssueOpenResponse struct {
	IssueID        string `json:"issue_id"`
	ConversationID string `json:"conversation_id"`
	EventID        string `json:"event_id"`
}

// IssueConcludeTaskSpec mirrors dispatch.IssueConcludeTaskSpec for the
// admin-wire shape. We define it locally so the Client doesn't take a
// compile dependency on the dispatch package; handlers convert.
type IssueConcludeTaskSpec struct {
	LocalID           string   `json:"local_id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Priority          string   `json:"priority"`
	RequiresWorktree  bool     `json:"requires_worktree"`
	DependsOnLocalIDs []string `json:"depends_on_local_ids,omitempty"`
}

// IssueConcludeRequest mirrors api issueConcludeReq.
type IssueConcludeRequest struct {
	IssueID     string                  `json:"issue_id"`
	Kind        string                  `json:"kind"`
	Summary     string                  `json:"summary"`
	Tasks       []IssueConcludeTaskSpec `json:"tasks"`
	ConcludedBy string                  `json:"concluded_by"`
}

// IssueConcludeResponse mirrors api success body.
type IssueConcludeResponse struct {
	IssueID  string   `json:"issue_id"`
	TaskIDs  []string `json:"task_ids"`
	EventIDs []string `json:"event_ids"`
}

// IssueWithdrawRequest mirrors api issueWithdrawReq.
type IssueWithdrawRequest struct {
	IssueID     string `json:"issue_id"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
	WithdrawnBy string `json:"withdrawn_by"`
}

// IssueCommentRequest mirrors api issueCommentReq.
type IssueCommentRequest struct {
	IssueID          string `json:"issue_id"`
	Content          string `json:"content"`
	ContentKind      string `json:"content_kind"`
	SenderIdentityID string `json:"sender_identity_id"`
	Direction        string `json:"direction"`
}

// IssueCommentResponse mirrors api success body.
type IssueCommentResponse struct {
	MessageID string `json:"message_id"`
}

// IssueBindAutoRequest mirrors api issueBindAutoReq.
type IssueBindAutoRequest struct {
	IssueID string `json:"issue_id"`
	Channel string `json:"channel"`
}

// IssueBindAutoResponse mirrors api success body.
type IssueBindAutoResponse struct {
	IssueID        string `json:"issue_id"`
	ConversationID string `json:"conversation_id"`
}

// IssueBindToRequest mirrors api issueBindToReq.
type IssueBindToRequest struct {
	IssueID        string `json:"issue_id"`
	ConversationID string `json:"conversation_id"`
}

// IssueBindToResponse mirrors api success body.
type IssueBindToResponse struct {
	IssueID        string `json:"issue_id"`
	ConversationID string `json:"conversation_id"`
}

// IssueLinkRequest mirrors api issueLinkReq.
type IssueLinkRequest struct {
	IssueID        string `json:"issue_id"`
	ConversationID string `json:"conversation_id"`
}

// IssueLinkResponse mirrors api success body.
type IssueLinkResponse struct {
	IssueID        string `json:"issue_id"`
	ConversationID string `json:"conversation_id"`
}

// =============================================================================
// IssueRepo — FindByID / FindByProject / FindByStatus reads
// =============================================================================

// IssueFindByID GETs /admin/discussion/issue/find-by-id?id=…
func (c *Client) IssueFindByID(ctx context.Context, id string) (IssueDTO, error) {
	var out IssueDTO
	err := c.getJSON(ctx, "/admin/discussion/issue/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// IssueFindByProject GETs /admin/discussion/issue/find-by-project?project_id=…&status=…
func (c *Client) IssueFindByProject(ctx context.Context, projectID, status string) ([]IssueDTO, error) {
	var out []IssueDTO
	err := c.getJSON(ctx, "/admin/discussion/issue/find-by-project"+
		buildQuery("project_id", projectID, "status", status), &out)
	return out, err
}

// IssueFindByStatus GETs /admin/discussion/issue/find-by-status?status=…
func (c *Client) IssueFindByStatus(ctx context.Context, status string) ([]IssueDTO, error) {
	var out []IssueDTO
	err := c.getJSON(ctx, "/admin/discussion/issue/find-by-status"+buildQuery("status", status), &out)
	return out, err
}

// =============================================================================
// IssueLifecycleSvc — Open / Conclude / Withdraw
// =============================================================================

// IssueOpen POSTs /admin/discussion/issue/open.
func (c *Client) IssueOpen(ctx context.Context, req IssueOpenRequest) (IssueOpenResponse, error) {
	var res IssueOpenResponse
	err := c.postJSON(ctx, "/admin/discussion/issue/open", req, &res)
	return res, err
}

// IssueConclude POSTs /admin/discussion/issue/conclude.
func (c *Client) IssueConclude(ctx context.Context, req IssueConcludeRequest) (IssueConcludeResponse, error) {
	var res IssueConcludeResponse
	err := c.postJSON(ctx, "/admin/discussion/issue/conclude", req, &res)
	return res, err
}

// IssueWithdraw POSTs /admin/discussion/issue/withdraw.
func (c *Client) IssueWithdraw(ctx context.Context, req IssueWithdrawRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/discussion/issue/withdraw", req, &res)
	return res, err
}

// =============================================================================
// IssueCommentSvc — Comment
// =============================================================================

// IssueComment POSTs /admin/discussion/issue/comment.
func (c *Client) IssueComment(ctx context.Context, req IssueCommentRequest) (IssueCommentResponse, error) {
	var res IssueCommentResponse
	err := c.postJSON(ctx, "/admin/discussion/issue/comment", req, &res)
	return res, err
}

// =============================================================================
// IssueBindConversationSvc — BindAuto / BindTo
// =============================================================================

// IssueBindAuto POSTs /admin/discussion/issue/bind-auto.
func (c *Client) IssueBindAuto(ctx context.Context, req IssueBindAutoRequest) (IssueBindAutoResponse, error) {
	var res IssueBindAutoResponse
	err := c.postJSON(ctx, "/admin/discussion/issue/bind-auto", req, &res)
	return res, err
}

// IssueBindTo POSTs /admin/discussion/issue/bind-to.
func (c *Client) IssueBindTo(ctx context.Context, req IssueBindToRequest) (IssueBindToResponse, error) {
	var res IssueBindToResponse
	err := c.postJSON(ctx, "/admin/discussion/issue/bind-to", req, &res)
	return res, err
}

// =============================================================================
// IssueLinkConversationSvc — Link
// =============================================================================

// IssueLink POSTs /admin/discussion/issue/link.
func (c *Client) IssueLink(ctx context.Context, req IssueLinkRequest) (IssueLinkResponse, error) {
	var res IssueLinkResponse
	err := c.postJSON(ctx, "/admin/discussion/issue/link", req, &res)
	return res, err
}
