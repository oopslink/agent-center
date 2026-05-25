// Package cli — admin_client_conversation.go: Client methods for the
// Conversation BC admin surface (ConvRepo / MsgRepo / MessageWriter /
// ChannelMgmtSvc / ParticipantMgmtSvc / CarryOverSvc / DerivationSvc /
// ConvRefRepo). Mirrors internal/admin/api/conversation.go 1:1.
//
// 19 endpoints registered in internal/admin/api/server.go under the
// `/admin/conversation/` prefix. Naming: methods on Client are named
// <Resource><Verb> to match the admin route segments
// (e.g. `ChannelCreate` for `POST /admin/conversation/channel/create`).
// Read methods return typed DTO structs whose JSON tags match the JSON
// keys emitted by the admin endpoint's projection helpers
// (convMap, messageMap, refsToMap) exactly.
//
// Mismatch report (flagged per task scope, NOT fixed here):
//   - admin endpoint has NO `participant/leave` route — the
//     ParticipantMgmtSvc.Leave service exists and `channel leave` CLI
//     calls it, but there is no admin proxy. The dual-mode handler keeps
//     the legacy direct-service path; once an admin endpoint is added a
//     `ParticipantLeave` method should be appended here to match.
//   - admin endpoint has NO `msg/find-recent` route — MsgRepo.FindRecent
//     is used by `conversation read --tail=N` and `conversation tail`.
//     We approximate by calling MsgFindByConversationID (which returns
//     up to 200 messages newest-last) and trimming client-side; the
//     existing admin endpoint hard-codes MessageFilter{Limit: 200} so
//     we have no `since` / arbitrary-tail control over the wire.
package cli

import (
	"context"

	"github.com/oopslink/agent-center/internal/conversation"
)

// =============================================================================
// DTOs — JSON shape returned by admin/api/conversation.go projection helpers.
// Field names match the JSON keys in convMap / messageMap / refsToMap exactly.
// =============================================================================

// ParticipantDTO mirrors one entry of the admin api convMap "participants".
type ParticipantDTO struct {
	IdentityID string `json:"identity_id"`
	Role       string `json:"role"`
	JoinedAt   any    `json:"joined_at"`
	JoinedBy   string `json:"joined_by"`
	LeftAt     any    `json:"left_at,omitempty"`
	LeftReason string `json:"left_reason,omitempty"`
}

// ConversationDTO mirrors admin api convMap.
type ConversationDTO struct {
	ID                   string           `json:"id"`
	Kind                 string           `json:"kind"`
	Name                 string           `json:"name"`
	Description          string           `json:"description"`
	Status               string           `json:"status"`
	ParentConversationID string           `json:"parent_conversation_id"`
	CreatedBy            string           `json:"created_by"`
	CreatedAt            string           `json:"created_at"`
	UpdatedAt            string           `json:"updated_at"`
	Version              int              `json:"version"`
	ArchivedAt           string           `json:"archived_at,omitempty"`
	ArchivedBy           string           `json:"archived_by,omitempty"`
	Participants         []ParticipantDTO `json:"participants"`
}

// MessageDTO mirrors admin api messageMap.
type MessageDTO struct {
	ID               string `json:"id"`
	ConversationID   string `json:"conversation_id"`
	SenderIdentityID string `json:"sender_identity_id"`
	ContentKind      string `json:"content_kind"`
	Content          string `json:"content"`
	Direction        string `json:"direction"`
	InputRequestRef  string `json:"input_request_ref"`
	PostedAt         string `json:"posted_at"`
}

// ConversationMessageReferenceDTO mirrors admin api refsToMap entries.
type ConversationMessageReferenceDTO struct {
	ID                   string `json:"id"`
	ChildConversationID  string `json:"child_conversation_id"`
	SourceConversationID string `json:"source_conversation_id"`
	SourceMessageID      string `json:"source_message_id"`
	CreatedBy            string `json:"created_by"`
	CreatedAt            string `json:"created_at"`
}

// =============================================================================
// Request payloads — match admin/api request structs (kept local so the
// Client doesn't take a compile dependency on the api package).
// =============================================================================

// MsgAppendRequest is the POST body for /admin/conversation/msg/append.
type MsgAppendRequest struct {
	ConversationID   string `json:"conversation_id"`
	SenderIdentityID string `json:"sender_identity_id"`
	ContentKind      string `json:"content_kind"`
	Content          string `json:"content"`
	Direction        string `json:"direction"`
	InputRequestRef  string `json:"input_request_ref"`
}

// MsgAppendResponse mirrors the success projection.
type MsgAppendResponse struct {
	MessageID string `json:"message_id"`
	EventID   string `json:"event_id"`
}

// ConversationOpenRequest is the POST body for message-writer/open.
type ConversationOpenRequest struct {
	Kind                 string                            `json:"kind"`
	Name                 string                            `json:"name"`
	Description          string                            `json:"description"`
	ParentConversationID string                            `json:"parent_conversation_id"`
	Participants         []conversation.ParticipantElement `json:"participants"`
	CreatedBy            string                            `json:"created_by"`
}

// ConversationOpenResponse mirrors the success projection.
type ConversationOpenResponse struct {
	ConversationID string `json:"conversation_id"`
	EventID        string `json:"event_id"`
}

// ConversationCloseRequest is the POST body for message-writer/close.
type ConversationCloseRequest struct {
	ConversationID string `json:"conversation_id"`
	Version        int    `json:"version"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
}

// ConversationArchiveRequest is the POST body for message-writer/archive.
type ConversationArchiveRequest struct {
	ConversationID string `json:"conversation_id"`
	Version        int    `json:"version"`
	ArchivedBy     string `json:"archived_by"`
}

// ChannelCreateRequest mirrors api createChannelReq.
type ChannelCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by"`
}

// ChannelCreateResponse mirrors the success projection.
type ChannelCreateResponse struct {
	ConversationID string `json:"conversation_id"`
	EventID        string `json:"event_id"`
}

// ChannelArchiveRequest mirrors api archiveChannelReq.
type ChannelArchiveRequest struct {
	Name       string `json:"name"`
	ArchivedBy string `json:"archived_by"`
}

// ParticipantInviteRequest mirrors api inviteParticipantReq.
type ParticipantInviteRequest struct {
	ConversationName string `json:"conversation_name"`
	IdentityID       string `json:"identity_id"`
	Role             string `json:"role"`
	InvitedBy        string `json:"invited_by"`
}

// ParticipantKickRequest mirrors api kickParticipantReq.
type ParticipantKickRequest struct {
	ConversationName string `json:"conversation_name"`
	IdentityID       string `json:"identity_id"`
	KickedBy         string `json:"kicked_by"`
	Reason           string `json:"reason"`
}

// DeriveIssueRequest mirrors api deriveIssueReq.
type DeriveIssueRequest struct {
	SourceConversationID string   `json:"source_conversation_id"`
	SourceMessageIDs     []string `json:"source_message_ids"`
	ProjectID            string   `json:"project_id"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	CreatedBy            string   `json:"created_by"`
}

// DeriveIssueResponse mirrors the success projection.
type DeriveIssueResponse struct {
	IssueID          string `json:"issue_id"`
	ConversationID   string `json:"conversation_id"`
	ReferenceCount   int    `json:"reference_count"`
	IssueEventID     string `json:"issue_event_id"`
	CarryOverEventID string `json:"carry_over_event_id"`
}

// DeriveTaskRequest mirrors api deriveTaskReq.
type DeriveTaskRequest struct {
	SourceConversationID string   `json:"source_conversation_id"`
	SourceMessageIDs     []string `json:"source_message_ids"`
	ProjectID            string   `json:"project_id"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	AgentInstanceID      string   `json:"agent_instance_id"`
	CreatedBy            string   `json:"created_by"`
}

// DeriveTaskResponse mirrors the success projection.
type DeriveTaskResponse struct {
	TaskID           string `json:"task_id"`
	ConversationID   string `json:"conversation_id"`
	ReferenceCount   int    `json:"reference_count"`
	TaskEventID      string `json:"task_event_id"`
	CarryOverEventID string `json:"carry_over_event_id"`
}

// =============================================================================
// ConvRepo — Find / FindByID / FindByName
// =============================================================================

// ConversationFind GETs /admin/conversation/conv/find?kind=…&status=…
func (c *Client) ConversationFind(ctx context.Context, kind, status string) ([]ConversationDTO, error) {
	var out []ConversationDTO
	err := c.getJSON(ctx, "/admin/conversation/conv/find"+
		buildQuery("kind", kind, "status", status), &out)
	return out, err
}

// ConversationFindByID GETs /admin/conversation/conv/find-by-id?id=…
func (c *Client) ConversationFindByID(ctx context.Context, id string) (ConversationDTO, error) {
	var out ConversationDTO
	err := c.getJSON(ctx, "/admin/conversation/conv/find-by-id"+
		buildQuery("id", id), &out)
	return out, err
}

// ConversationFindByName GETs /admin/conversation/conv/find-by-name?name=…
func (c *Client) ConversationFindByName(ctx context.Context, name string) (ConversationDTO, error) {
	var out ConversationDTO
	err := c.getJSON(ctx, "/admin/conversation/conv/find-by-name"+
		buildQuery("name", name), &out)
	return out, err
}

// =============================================================================
// MsgRepo — FindByID / FindByConversationID / Append
// =============================================================================

// MessageFindByID GETs /admin/conversation/msg/find-by-id?id=…
func (c *Client) MessageFindByID(ctx context.Context, id string) (MessageDTO, error) {
	var out MessageDTO
	err := c.getJSON(ctx, "/admin/conversation/msg/find-by-id"+
		buildQuery("id", id), &out)
	return out, err
}

// MessageFindByConversationID GETs /admin/conversation/msg/find-by-conversation-id?conversation_id=…
// Server hard-codes MessageFilter{Limit: 200}; arbitrary filters not yet
// proxied (see file-header mismatch note).
func (c *Client) MessageFindByConversationID(ctx context.Context, convID string) ([]MessageDTO, error) {
	var out []MessageDTO
	err := c.getJSON(ctx, "/admin/conversation/msg/find-by-conversation-id"+
		buildQuery("conversation_id", convID), &out)
	return out, err
}

// MessageAppend POSTs /admin/conversation/msg/append.
func (c *Client) MessageAppend(ctx context.Context, req MsgAppendRequest) (MsgAppendResponse, error) {
	var res MsgAppendResponse
	err := c.postJSON(ctx, "/admin/conversation/msg/append", req, &res)
	return res, err
}

// =============================================================================
// MessageWriter — OpenConversation / Close / Archive
// (AddMessage path lives on /msg/append above; same service.)
// =============================================================================

// ConversationOpen POSTs /admin/conversation/message-writer/open.
func (c *Client) ConversationOpen(ctx context.Context, req ConversationOpenRequest) (ConversationOpenResponse, error) {
	var res ConversationOpenResponse
	err := c.postJSON(ctx, "/admin/conversation/message-writer/open", req, &res)
	return res, err
}

// ConversationClose POSTs /admin/conversation/message-writer/close.
func (c *Client) ConversationClose(ctx context.Context, req ConversationCloseRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/conversation/message-writer/close", req, &res)
	return res, err
}

// ConversationArchive POSTs /admin/conversation/message-writer/archive.
func (c *Client) ConversationArchive(ctx context.Context, req ConversationArchiveRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/conversation/message-writer/archive", req, &res)
	return res, err
}

// =============================================================================
// ChannelMgmtSvc — CreateChannel / ArchiveChannel
// =============================================================================

// ChannelCreate POSTs /admin/conversation/channel/create.
func (c *Client) ChannelCreate(ctx context.Context, req ChannelCreateRequest) (ChannelCreateResponse, error) {
	var res ChannelCreateResponse
	err := c.postJSON(ctx, "/admin/conversation/channel/create", req, &res)
	return res, err
}

// ChannelArchive POSTs /admin/conversation/channel/archive.
func (c *Client) ChannelArchive(ctx context.Context, req ChannelArchiveRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/conversation/channel/archive", req, &res)
	return res, err
}

// =============================================================================
// ParticipantMgmtSvc — Invite / Kick
// (NOTE: no /participant/leave proxy yet — see file-header mismatch.)
// =============================================================================

// ParticipantInvite POSTs /admin/conversation/participant/invite.
func (c *Client) ParticipantInvite(ctx context.Context, req ParticipantInviteRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/conversation/participant/invite", req, &res)
	return res, err
}

// ParticipantKick POSTs /admin/conversation/participant/kick.
func (c *Client) ParticipantKick(ctx context.Context, req ParticipantKickRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/conversation/participant/kick", req, &res)
	return res, err
}

// =============================================================================
// CarryOverSvc — FindByChildConv / FindBySourceMsg
// =============================================================================

// CarryOverFindByChildConv GETs /admin/conversation/carry-over/find-by-child-conv?child_conversation_id=…
func (c *Client) CarryOverFindByChildConv(ctx context.Context, childConvID string) ([]ConversationMessageReferenceDTO, error) {
	var out []ConversationMessageReferenceDTO
	err := c.getJSON(ctx, "/admin/conversation/carry-over/find-by-child-conv"+
		buildQuery("child_conversation_id", childConvID), &out)
	return out, err
}

// CarryOverFindBySourceMsg GETs /admin/conversation/carry-over/find-by-source-msg?source_message_id=…
func (c *Client) CarryOverFindBySourceMsg(ctx context.Context, sourceMsgID string) ([]ConversationMessageReferenceDTO, error) {
	var out []ConversationMessageReferenceDTO
	err := c.getJSON(ctx, "/admin/conversation/carry-over/find-by-source-msg"+
		buildQuery("source_message_id", sourceMsgID), &out)
	return out, err
}

// =============================================================================
// DerivationSvc — DeriveIssue / DeriveTask
// =============================================================================

// DerivationDeriveIssue POSTs /admin/conversation/derivation/derive-issue.
func (c *Client) DerivationDeriveIssue(ctx context.Context, req DeriveIssueRequest) (DeriveIssueResponse, error) {
	var res DeriveIssueResponse
	err := c.postJSON(ctx, "/admin/conversation/derivation/derive-issue", req, &res)
	return res, err
}

// DerivationDeriveTask POSTs /admin/conversation/derivation/derive-task.
func (c *Client) DerivationDeriveTask(ctx context.Context, req DeriveTaskRequest) (DeriveTaskResponse, error) {
	var res DeriveTaskResponse
	err := c.postJSON(ctx, "/admin/conversation/derivation/derive-task", req, &res)
	return res, err
}

// =============================================================================
// ConvRefRepo — FindByChildConvID / FindBySourceMsgID
// (raw repo path; CarryOverSvc proxies above wrap the same data.)
// =============================================================================

// ConvRefFindByChildConvID GETs /admin/conversation/conv-ref/find-by-child-conv-id?child_conversation_id=…
func (c *Client) ConvRefFindByChildConvID(ctx context.Context, childConvID string) ([]ConversationMessageReferenceDTO, error) {
	var out []ConversationMessageReferenceDTO
	err := c.getJSON(ctx, "/admin/conversation/conv-ref/find-by-child-conv-id"+
		buildQuery("child_conversation_id", childConvID), &out)
	return out, err
}

// ConvRefFindBySourceMsgID GETs /admin/conversation/conv-ref/find-by-source-msg-id?source_message_id=…
func (c *Client) ConvRefFindBySourceMsgID(ctx context.Context, sourceMsgID string) ([]ConversationMessageReferenceDTO, error) {
	var out []ConversationMessageReferenceDTO
	err := c.getJSON(ctx, "/admin/conversation/conv-ref/find-by-source-msg-id"+
		buildQuery("source_message_id", sourceMsgID), &out)
	return out, err
}
