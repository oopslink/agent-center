// Package cli — admin_client_taskruntime.go: Client methods for the
// TaskRuntime BC admin surface (task / exec / IR / artifact / dispatch /
// kill). Mirrors internal/admin/api/taskruntime.go 1:1.
//
// Naming: methods on Client are named <Resource><Verb> to match the
// admin route segments (e.g. `TaskCreate` for
// `POST /admin/taskruntime/task/create`). Read methods return typed DTO
// structs whose JSON tags match the JSON keys emitted by the admin
// endpoint's projection helpers (`taskMap`, `executionMap`, `irMap`,
// `artifactMap`).
//
// v2.3-1 (task #24) closed the prior v2.2 mismatch: a proper
// `GET /admin/taskruntime/task/read-context` endpoint now exists →
// TaskReadContext method below; `read-task-context` CLI goes through
// Client uniformly (no more ExitNotImplemented in Client mode).
package cli

import (
	"context"
	"encoding/json"
	"strconv"
)

// =============================================================================
// DTOs — JSON shape returned by admin/api/taskruntime.go projection helpers.
// Field names match the JSON keys in taskMap / executionMap / irMap /
// artifactMap exactly.
// =============================================================================

// TaskDTO mirrors admin api taskMap.
type TaskDTO struct {
	ID                 string   `json:"id"`
	ProjectID          string   `json:"project_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Status             string   `json:"status"`
	Priority           string   `json:"priority"`
	RequiresWorktree   bool     `json:"requires_worktree"`
	ParentTaskID       string   `json:"parent_task_id"`
	FromIssueID        string   `json:"from_issue_id"`
	ConversationID     string   `json:"conversation_id"`
	CurrentExecutionID string   `json:"current_execution_id"`
	DependsOnTaskIDs   []string `json:"depends_on_task_ids"`
	CreatedBy          string   `json:"created_by"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	Version            int      `json:"version"`
}

// ExecutionDTO mirrors admin api executionMap.
type ExecutionDTO struct {
	ID                        string `json:"id"`
	TaskID                    string `json:"task_id"`
	WorkerID                  string `json:"worker_id"`
	AgentCLI                  string `json:"agent_cli"`
	WorkspaceMode             string `json:"workspace_mode"`
	Status                    string `json:"status"`
	FailedReason              string `json:"failed_reason"`
	FailedMessage             string `json:"failed_message"`
	BaseBranch                string `json:"base_branch"`
	CWD                       string `json:"cwd"`
	BranchName                string `json:"branch_name"`
	StartedAt                 string `json:"started_at"`
	WorkingSecondsAccumulated int64  `json:"working_seconds_accumulated"`
	Version                   int    `json:"version"`
}

// InputRequestDTO mirrors admin api irMap.
type InputRequestDTO struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	ExecutionID string   `json:"execution_id"`
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Urgency     string   `json:"urgency"`
	CreatedAt   string   `json:"created_at"`
	Answer      string   `json:"answer,omitempty"`
	DecidedBy   string   `json:"decided_by,omitempty"`
	DecidedAt   string   `json:"decided_at,omitempty"`
}

// ArtifactDTO mirrors admin api artifactMap.
type ArtifactDTO struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	ExecutionID  string `json:"execution_id"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	BlobRef      string `json:"blob_ref"`
	URL          string `json:"url"`
	MetadataJSON string `json:"metadata_json"`
	CreatedAt    string `json:"created_at"`
	CreatedBy    string `json:"created_by"`
}

// =============================================================================
// Request payloads — match admin/api request structs (kept local so the
// Client doesn't take a compile dependency on the api package).
// =============================================================================

// TaskCreateRequest mirrors api taskCreateReq.
type TaskCreateRequest struct {
	ProjectID         string   `json:"project_id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	ParentTaskID      string   `json:"parent_task_id"`
	FromIssueID       string   `json:"from_issue_id"`
	Priority          string   `json:"priority"`
	RequiresWorktree  bool     `json:"requires_worktree"`
	DependsOnTaskIDs  []string `json:"depends_on_task_ids"`
	WithConversation  bool     `json:"with_conversation"`
	ConversationTitle string   `json:"conversation_title"`
}

// TaskCreateResponse mirrors the success projection emitted by
// taskCreateHandler.
type TaskCreateResponse struct {
	TaskID         string `json:"task_id"`
	ConversationID string `json:"conversation_id"`
}

// TaskBindConversationRequest mirrors api taskBindConvReq.
type TaskBindConversationRequest struct {
	TaskID         string `json:"task_id"`
	Mode           string `json:"mode"`
	ExistingConvID string `json:"existing_conversation_id"`
	Title          string `json:"title"`
	ChannelHint    string `json:"channel_hint"`
}

// TaskBindConversationResponse mirrors the projection.
type TaskBindConversationResponse struct {
	TaskID         string `json:"task_id"`
	ConversationID string `json:"conversation_id"`
}

// IRCreateRequest mirrors api irCreateReq.
type IRCreateRequest struct {
	ExecutionID string   `json:"execution_id"`
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Urgency     string   `json:"urgency"`
}

// IRCreateResponse mirrors the success projection.
type IRCreateResponse struct {
	InputRequestID string `json:"input_request_id"`
	ConversationID string `json:"conversation_id"`
}

// IRRespondRequest mirrors api irRespondReq.
type IRRespondRequest struct {
	InputRequestID string `json:"input_request_id"`
	Answer         string `json:"answer"`
	DecidedBy      string `json:"decided_by"`
}

// IRRespondResponse mirrors the success projection.
type IRRespondResponse struct {
	Answered bool `json:"answered"`
}

// IRCancelRequest mirrors api irCancelReq.
type IRCancelRequest struct {
	InputRequestID string `json:"input_request_id"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
}

// IRCancelResponse mirrors the success projection.
type IRCancelResponse struct {
	Cancelled bool `json:"cancelled"`
}

// ArtifactAppendRequest mirrors api artifactAppendReq.
type ArtifactAppendRequest struct {
	ExecutionID  string `json:"execution_id"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	BlobRef      string `json:"blob_ref"`
	URL          string `json:"url"`
	MetadataJSON string `json:"metadata_json"`
}

// ArtifactAppendResponse mirrors the success projection.
type ArtifactAppendResponse struct {
	ArtifactID string `json:"artifact_id"`
}

// ExecReportProgressRequest mirrors api reportProgressReq.
type ExecReportProgressRequest struct {
	ExecutionID string `json:"execution_id"`
	Kind        string `json:"kind"`
	Content     string `json:"content"`
}

// ExecReportProgressResponse mirrors the success projection.
type ExecReportProgressResponse struct {
	Status string `json:"status"`
}

// ExecReportFailureRequest mirrors api reportFailureReq.
type ExecReportFailureRequest struct {
	ExecutionID string `json:"execution_id"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
}

// ExecReportFailureResponse mirrors the success projection.
type ExecReportFailureResponse struct {
	Status string `json:"status"`
}

// ExecNotifyWorkingRequest mirrors api notifyWorkingReq.
type ExecNotifyWorkingRequest struct {
	ExecutionID string `json:"execution_id"`
	CWD         string `json:"cwd"`
	BranchName  string `json:"branch_name"`
}

// ExecNotifyWorkingResponse mirrors the success projection.
type ExecNotifyWorkingResponse struct {
	Status string `json:"status"`
}

// ExecConcludeRequest mirrors api concludeReq.
type ExecConcludeRequest struct {
	ExecutionID string `json:"execution_id"`
	Message     string `json:"message"`
}

// ExecConcludeResponse mirrors the success projection.
type ExecConcludeResponse struct {
	Status string `json:"status"`
}

// DispatchRequest mirrors api dispatchReq.
type DispatchRequest struct {
	TaskID               string `json:"task_id"`
	WorkerID             string `json:"worker_id"`
	AgentCLI             string `json:"agent_cli"`
	AgentInstanceID      string `json:"agent_instance_id"`
	BaseBranch           string `json:"base_branch"`
	ExecutionTimeoutSecs *int64 `json:"execution_timeout_seconds"`
}

// DispatchResponse mirrors the success projection.
type DispatchResponse struct {
	ExecutionID string `json:"execution_id"`
}

// KillExecutionRequest mirrors api killExecReq.
type KillExecutionRequest struct {
	ExecutionID string `json:"execution_id"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
}

// KillExecutionResponse mirrors the success projection.
type KillExecutionResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
}

// =============================================================================
// Task — Create / BindConversation + read repo methods
// =============================================================================

// TaskCreate POSTs /admin/taskruntime/task/create.
func (c *Client) TaskCreate(ctx context.Context, req TaskCreateRequest) (TaskCreateResponse, error) {
	var res TaskCreateResponse
	err := c.postJSON(ctx, "/admin/taskruntime/task/create", req, &res)
	return res, err
}

// TaskBindConversation POSTs /admin/taskruntime/task/bind-conversation.
func (c *Client) TaskBindConversation(ctx context.Context, req TaskBindConversationRequest) (TaskBindConversationResponse, error) {
	var res TaskBindConversationResponse
	err := c.postJSON(ctx, "/admin/taskruntime/task/bind-conversation", req, &res)
	return res, err
}

// TaskReadContext GETs /admin/taskruntime/task/read-context?task_id=…&recent_messages_n=…
// (v2.3-1). Returns the raw JSON body verbatim so the caller can stream
// it to stdout (the handler historically just json.Marshal'd the service
// result). This keeps the wire shape stable without forcing the cli
// package to take a compile dependency on taskruntime/service for the
// TaskContext struct.
func (c *Client) TaskReadContext(ctx context.Context, taskID string, recentN int) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.getJSON(ctx, "/admin/taskruntime/task/read-context"+
		buildQuery("task_id", taskID, "recent_messages_n", strconv.Itoa(recentN)), &out)
	return out, err
}

// TaskFindByID GETs /admin/taskruntime/task/find-by-id?id=…
func (c *Client) TaskFindByID(ctx context.Context, id string) (TaskDTO, error) {
	var out TaskDTO
	err := c.getJSON(ctx, "/admin/taskruntime/task/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// TaskFindByStatus GETs /admin/taskruntime/task/find-by-status?status=…
func (c *Client) TaskFindByStatus(ctx context.Context, status string) ([]TaskDTO, error) {
	var out []TaskDTO
	err := c.getJSON(ctx, "/admin/taskruntime/task/find-by-status"+buildQuery("status", status), &out)
	return out, err
}

// =============================================================================
// Execution — read repo methods
// =============================================================================

// ExecFindByID GETs /admin/taskruntime/exec/find-by-id?id=…
func (c *Client) ExecFindByID(ctx context.Context, id string) (ExecutionDTO, error) {
	var out ExecutionDTO
	err := c.getJSON(ctx, "/admin/taskruntime/exec/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// ExecFindByTaskID GETs /admin/taskruntime/exec/find-by-task-id?task_id=…
func (c *Client) ExecFindByTaskID(ctx context.Context, taskID string) ([]ExecutionDTO, error) {
	var out []ExecutionDTO
	err := c.getJSON(ctx, "/admin/taskruntime/exec/find-by-task-id"+buildQuery("task_id", taskID), &out)
	return out, err
}

// ExecFindByStatus GETs /admin/taskruntime/exec/find-by-status?status=…
func (c *Client) ExecFindByStatus(ctx context.Context, status string) ([]ExecutionDTO, error) {
	var out []ExecutionDTO
	err := c.getJSON(ctx, "/admin/taskruntime/exec/find-by-status"+buildQuery("status", status), &out)
	return out, err
}

// =============================================================================
// InputRequest — Create / Respond / Cancel + read repo methods
// =============================================================================

// IRCreate POSTs /admin/taskruntime/ir/create.
func (c *Client) IRCreate(ctx context.Context, req IRCreateRequest) (IRCreateResponse, error) {
	var res IRCreateResponse
	err := c.postJSON(ctx, "/admin/taskruntime/ir/create", req, &res)
	return res, err
}

// IRRespond POSTs /admin/taskruntime/ir/respond.
func (c *Client) IRRespond(ctx context.Context, req IRRespondRequest) (IRRespondResponse, error) {
	var res IRRespondResponse
	err := c.postJSON(ctx, "/admin/taskruntime/ir/respond", req, &res)
	return res, err
}

// IRCancel POSTs /admin/taskruntime/ir/cancel.
func (c *Client) IRCancel(ctx context.Context, req IRCancelRequest) (IRCancelResponse, error) {
	var res IRCancelResponse
	err := c.postJSON(ctx, "/admin/taskruntime/ir/cancel", req, &res)
	return res, err
}

// IRFindByID GETs /admin/taskruntime/ir/find-by-id?id=…
func (c *Client) IRFindByID(ctx context.Context, id string) (InputRequestDTO, error) {
	var out InputRequestDTO
	err := c.getJSON(ctx, "/admin/taskruntime/ir/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// IRFindByExecutionID GETs /admin/taskruntime/ir/find-by-execution-id?execution_id=…
func (c *Client) IRFindByExecutionID(ctx context.Context, execID string) (InputRequestDTO, error) {
	var out InputRequestDTO
	err := c.getJSON(ctx, "/admin/taskruntime/ir/find-by-execution-id"+buildQuery("execution_id", execID), &out)
	return out, err
}

// IRFindPending GETs /admin/taskruntime/ir/find-pending.
func (c *Client) IRFindPending(ctx context.Context) ([]InputRequestDTO, error) {
	var out []InputRequestDTO
	err := c.getJSON(ctx, "/admin/taskruntime/ir/find-pending", &out)
	return out, err
}

// =============================================================================
// Artifact — Append + read repo methods
// =============================================================================

// ArtifactAppend POSTs /admin/taskruntime/artifact/append.
func (c *Client) ArtifactAppend(ctx context.Context, req ArtifactAppendRequest) (ArtifactAppendResponse, error) {
	var res ArtifactAppendResponse
	err := c.postJSON(ctx, "/admin/taskruntime/artifact/append", req, &res)
	return res, err
}

// ArtifactFindByID GETs /admin/taskruntime/artifact/find-by-id?id=…
func (c *Client) ArtifactFindByID(ctx context.Context, id string) (ArtifactDTO, error) {
	var out ArtifactDTO
	err := c.getJSON(ctx, "/admin/taskruntime/artifact/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// ArtifactFindByExecutionID GETs /admin/taskruntime/artifact/find-by-execution-id?execution_id=…
func (c *Client) ArtifactFindByExecutionID(ctx context.Context, execID string) ([]ArtifactDTO, error) {
	var out []ArtifactDTO
	err := c.getJSON(ctx, "/admin/taskruntime/artifact/find-by-execution-id"+buildQuery("execution_id", execID), &out)
	return out, err
}

// =============================================================================
// Execution lifecycle — ReportProgress / ReportFailure / NotifyWorking /
// Conclude. Drives the state machine end-to-end.
// =============================================================================

// ExecReportProgress POSTs /admin/taskruntime/exec/report-progress.
func (c *Client) ExecReportProgress(ctx context.Context, req ExecReportProgressRequest) (ExecReportProgressResponse, error) {
	var res ExecReportProgressResponse
	err := c.postJSON(ctx, "/admin/taskruntime/exec/report-progress", req, &res)
	return res, err
}

// ExecReportFailure POSTs /admin/taskruntime/exec/report-failure.
func (c *Client) ExecReportFailure(ctx context.Context, req ExecReportFailureRequest) (ExecReportFailureResponse, error) {
	var res ExecReportFailureResponse
	err := c.postJSON(ctx, "/admin/taskruntime/exec/report-failure", req, &res)
	return res, err
}

// ExecNotifyWorking POSTs /admin/taskruntime/exec/notify-working.
func (c *Client) ExecNotifyWorking(ctx context.Context, req ExecNotifyWorkingRequest) (ExecNotifyWorkingResponse, error) {
	var res ExecNotifyWorkingResponse
	err := c.postJSON(ctx, "/admin/taskruntime/exec/notify-working", req, &res)
	return res, err
}

// ExecConclude POSTs /admin/taskruntime/exec/conclude.
func (c *Client) ExecConclude(ctx context.Context, req ExecConcludeRequest) (ExecConcludeResponse, error) {
	var res ExecConcludeResponse
	err := c.postJSON(ctx, "/admin/taskruntime/exec/conclude", req, &res)
	return res, err
}

// =============================================================================
// Dispatch — Dispatch
// =============================================================================

// Dispatch POSTs /admin/taskruntime/dispatch/dispatch.
func (c *Client) Dispatch(ctx context.Context, req DispatchRequest) (DispatchResponse, error) {
	var res DispatchResponse
	err := c.postJSON(ctx, "/admin/taskruntime/dispatch/dispatch", req, &res)
	return res, err
}

// DispatchWithDecisionRequest bundles Dispatch + DecisionRecord. v2.3-2:
// supervisor-driven CLI MUST use this when AGENT_CENTER_INVOCATION_ID
// is set to satisfy ADR-0014 § 2 atomicity (state + audit in one tx).
type DispatchWithDecisionRequest struct {
	Dispatch DispatchRequest        `json:"dispatch"`
	Decision DecisionRecordRequest  `json:"decision"`
}

// DispatchWithDecisionResponse mirrors the bundled success projection.
type DispatchWithDecisionResponse struct {
	ExecutionID string `json:"execution_id"`
	DecisionID  string `json:"decision_id"`
}

// DispatchWithDecision POSTs /admin/taskruntime/dispatch/dispatch-with-decision.
// Both side-effects commit or roll back as one unit.
func (c *Client) DispatchWithDecision(ctx context.Context, req DispatchWithDecisionRequest) (DispatchWithDecisionResponse, error) {
	var res DispatchWithDecisionResponse
	err := c.postJSON(ctx, "/admin/taskruntime/dispatch/dispatch-with-decision", req, &res)
	return res, err
}

// =============================================================================
// Kill — RequestKill (kill-execution)
// =============================================================================

// KillRequest POSTs /admin/taskruntime/kill/request.
func (c *Client) KillRequest(ctx context.Context, req KillExecutionRequest) (KillExecutionResponse, error) {
	var res KillExecutionResponse
	err := c.postJSON(ctx, "/admin/taskruntime/kill/request", req, &res)
	return res, err
}

// KillWithDecisionRequest bundles RequestKill + DecisionRecord (v2.3-2).
type KillWithDecisionRequest struct {
	Kill     KillExecutionRequest   `json:"kill"`
	Decision DecisionRecordRequest  `json:"decision"`
}

// KillWithDecisionResponse mirrors the bundled success projection.
type KillWithDecisionResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
	DecisionID  string `json:"decision_id"`
}

// KillWithDecision POSTs /admin/taskruntime/kill/request-with-decision.
// Both side-effects commit or roll back as one unit.
func (c *Client) KillWithDecision(ctx context.Context, req KillWithDecisionRequest) (KillWithDecisionResponse, error) {
	var res KillWithDecisionResponse
	err := c.postJSON(ctx, "/admin/taskruntime/kill/request-with-decision", req, &res)
	return res, err
}
