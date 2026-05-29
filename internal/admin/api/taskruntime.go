package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// =============================================================================
// TaskRepo — FindByID / FindByStatus / FindByProject
// =============================================================================

func (s *Server) taskFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskRepo == nil {
		writeError(w, http.StatusNotImplemented, "task_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	t, err := d.TaskRepo.FindByID(r.Context(), taskruntime.TaskID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskMap(t))
}

func (s *Server) taskFindByStatusHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskRepo == nil {
		writeError(w, http.StatusNotImplemented, "task_repo_not_wired", "")
		return
	}
	st := r.URL.Query().Get("status")
	if st == "" {
		writeError(w, http.StatusBadRequest, "missing_status", "")
		return
	}
	list, err := d.TaskRepo.FindByStatus(r.Context(), task.Status(st), task.Filter{Limit: 200})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, t := range list {
		out[i] = taskMap(t)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// ExecRepo — FindByID / FindByTaskID / FindByStatus
// =============================================================================

func (s *Server) execFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ExecRepo == nil {
		writeError(w, http.StatusNotImplemented, "exec_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	e, err := d.ExecRepo.FindByID(r.Context(), taskruntime.TaskExecutionID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, executionMap(e))
}

func (s *Server) execFindByTaskIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ExecRepo == nil {
		writeError(w, http.StatusNotImplemented, "exec_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("task_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	list, err := d.ExecRepo.FindByTaskID(r.Context(), taskruntime.TaskID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, e := range list {
		out[i] = executionMap(e)
	}
	writeJSON(w, http.StatusOK, out)
}

// execFindByStatusHandler uses FindActive (no terminal-status repo method
// exists — query side filters in-memory). Status filter is in-memory.
func (s *Server) execFindByStatusHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ExecRepo == nil {
		writeError(w, http.StatusNotImplemented, "exec_repo_not_wired", "")
		return
	}
	list, err := d.ExecRepo.FindActive(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	want := r.URL.Query().Get("status")
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		if want != "" && string(e.Status()) != want {
			continue
		}
		out = append(out, executionMap(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// IRRepo — FindByID / FindByTaskExecutionID / FindPending
// =============================================================================

func (s *Server) irFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IRRepo == nil {
		writeError(w, http.StatusNotImplemented, "ir_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	ir, err := d.IRRepo.FindByID(r.Context(), taskruntime.InputRequestID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, irMap(ir))
}

func (s *Server) irFindByExecutionIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IRRepo == nil {
		writeError(w, http.StatusNotImplemented, "ir_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("execution_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_execution_id", "")
		return
	}
	ir, err := d.IRRepo.FindByTaskExecutionID(r.Context(), taskruntime.TaskExecutionID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, irMap(ir))
}

func (s *Server) irFindPendingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IRRepo == nil {
		writeError(w, http.StatusNotImplemented, "ir_repo_not_wired", "")
		return
	}
	list, err := d.IRRepo.FindPending(r.Context(), time.Now().UTC().Add(24*365*time.Hour))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, ir := range list {
		out[i] = irMap(ir)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// ArtifactRepo — FindByID / FindByExecutionID / FindByTaskID
// =============================================================================

func (s *Server) artifactFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ArtifactRepo == nil {
		writeError(w, http.StatusNotImplemented, "artifact_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	a, err := d.ArtifactRepo.FindByID(r.Context(), taskruntime.ArtifactID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactMap(a))
}

func (s *Server) artifactFindByExecutionIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ArtifactRepo == nil {
		writeError(w, http.StatusNotImplemented, "artifact_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("execution_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_execution_id", "")
		return
	}
	list, err := d.ArtifactRepo.FindByExecutionID(r.Context(), taskruntime.TaskExecutionID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, a := range list {
		out[i] = artifactMap(a)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// TaskSvc — Create / BindConversation / ReadContext
// =============================================================================

type taskCreateReq struct {
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

func (s *Server) taskCreateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskSvc == nil {
		writeError(w, http.StatusNotImplemented, "task_svc_not_wired", "")
		return
	}
	var req taskCreateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	deps := make([]taskruntime.TaskID, len(req.DependsOnTaskIDs))
	for i, t := range req.DependsOnTaskIDs {
		deps[i] = taskruntime.TaskID(t)
	}
	pr := task.Priority(req.Priority)
	res, err := d.TaskSvc.Create(r.Context(), trservice.TaskCreateInput{
		ProjectID:         req.ProjectID,
		Title:             req.Title,
		Description:       req.Description,
		ParentTaskID:      taskruntime.TaskID(req.ParentTaskID),
		FromIssueID:       req.FromIssueID,
		Priority:          pr,
		RequiresWorktree:  req.RequiresWorktree,
		DependsOnTaskIDs:  deps,
		WithConversation:  req.WithConversation,
		ConversationTitle: req.ConversationTitle,
		Actor:             d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":         string(res.TaskID),
		"conversation_id": string(res.ConversationID),
	})
}

type taskBindConvReq struct {
	TaskID         string `json:"task_id"`
	Mode           string `json:"mode"`
	ExistingConvID string `json:"existing_conversation_id"`
	Title          string `json:"title"`
	ChannelHint    string `json:"channel_hint"`
}

func (s *Server) taskBindConversationHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskSvc == nil {
		writeError(w, http.StatusNotImplemented, "task_svc_not_wired", "")
		return
	}
	var req taskBindConvReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	convID, err := d.TaskSvc.BindConversation(r.Context(), trservice.BindConversationInput{
		TaskID:         taskruntime.TaskID(req.TaskID),
		Mode:           req.Mode,
		ExistingConvID: conversation.ConversationID(req.ExistingConvID),
		Title:          req.Title,
		ChannelHint:    req.ChannelHint,
		Actor:          d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":         req.TaskID,
		"conversation_id": string(convID),
	})
}

// v2.3-1 (task #24): agent-facing read-context proxy. Without this the
// `read-task-context` CLI returns ExitNotImplemented in Client mode
// (worker daemon / agent dispatch context). Returns the TaskContext
// struct verbatim; its JSON tags already match the legacy direct-write
// output so consumers see no shape change.
func (s *Server) taskReadContextHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TaskSvc == nil {
		writeError(w, http.StatusNotImplemented, "task_svc_not_wired", "")
		return
	}
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	recentN := 0
	if v := r.URL.Query().Get("recent_messages_n"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			recentN = parsed
		}
	}
	tctx, err := d.TaskSvc.ReadContext(r.Context(), taskruntime.TaskID(taskID), recentN)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tctx)
}

// =============================================================================
// IRSvc — Create (request-input) / Respond / Cancel
// =============================================================================

type irCreateReq struct {
	ExecutionID string   `json:"execution_id"`
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Urgency     string   `json:"urgency"`
}

func (s *Server) irCreateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IRSvc == nil {
		writeError(w, http.StatusNotImplemented, "ir_svc_not_wired", "")
		return
	}
	var req irCreateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	urg := inputrequest.Urgency(req.Urgency)
	res, err := d.IRSvc.Create(r.Context(), trservice.CreateInput{
		ExecutionID: taskruntime.TaskExecutionID(req.ExecutionID),
		Question:    req.Question,
		Options:     req.Options,
		Urgency:     urg,
		Actor:       observability.Actor("agent:" + req.ExecutionID),
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"input_request_id": string(res.InputRequestID),
		"conversation_id":  string(res.ConversationID),
	})
}

type irRespondReq struct {
	InputRequestID string `json:"input_request_id"`
	Answer         string `json:"answer"`
	DecidedBy      string `json:"decided_by"`
}

func (s *Server) irRespondHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IRSvc == nil {
		writeError(w, http.StatusNotImplemented, "ir_svc_not_wired", "")
		return
	}
	var req irRespondReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	who := req.DecidedBy
	if who == "" {
		who = string(d.Actor)
	}
	if err := d.IRSvc.Respond(r.Context(), trservice.RespondInput{
		InputRequestID: taskruntime.InputRequestID(req.InputRequestID),
		Answer:         req.Answer,
		DecidedBy:      who,
		Actor:          d.Actor,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"answered": true})
}

type irCancelReq struct {
	InputRequestID string `json:"input_request_id"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
}

func (s *Server) irCancelHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IRSvc == nil {
		writeError(w, http.StatusNotImplemented, "ir_svc_not_wired", "")
		return
	}
	var req irCancelReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.IRSvc.Cancel(r.Context(), trservice.CancelInput{
		InputRequestID: taskruntime.InputRequestID(req.InputRequestID),
		Reason:         req.Reason,
		Message:        req.Message,
		Actor:          d.Actor,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
}

// =============================================================================
// ArtifactSvc — Append (report-artifact)
// =============================================================================

type artifactAppendReq struct {
	ExecutionID  string `json:"execution_id"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	BlobRef      string `json:"blob_ref"`
	URL          string `json:"url"`
	MetadataJSON string `json:"metadata_json"`
}

func (s *Server) artifactAppendHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ArtifactSvc == nil {
		writeError(w, http.StatusNotImplemented, "artifact_svc_not_wired", "")
		return
	}
	var req artifactAppendReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := d.ArtifactSvc.Append(r.Context(), trservice.AppendInput{
		ExecutionID:  taskruntime.TaskExecutionID(req.ExecutionID),
		Kind:         req.Kind,
		Title:        req.Title,
		BlobRef:      req.BlobRef,
		URL:          req.URL,
		MetadataJSON: req.MetadataJSON,
		Actor:        observability.Actor("agent:" + req.ExecutionID),
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact_id": string(res.ArtifactID)})
}

// =============================================================================
// ExecSvc — ReportProgress / ReportFailure
// =============================================================================

type reportProgressReq struct {
	ExecutionID string `json:"execution_id"`
	Kind        string `json:"kind"`
	Content     string `json:"content"`
}

func (s *Server) execReportProgressHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ExecSvc == nil {
		writeError(w, http.StatusNotImplemented, "exec_svc_not_wired", "")
		return
	}
	var req reportProgressReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.ExecSvc.ReportProgress(r.Context(), trservice.ReportProgressInput{
		ExecutionID: taskruntime.TaskExecutionID(req.ExecutionID),
		Kind:        req.Kind,
		Content:     req.Content,
		Actor:       observability.Actor("agent:" + req.ExecutionID),
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// notifyWorkingReq drives the v2.2 Phase D state machine fix
// (submitted → working). The worker daemon calls this after the agent
// subprocess is up; the center is authoritative on execution state.
type notifyWorkingReq struct {
	ExecutionID string `json:"execution_id"`
	CWD         string `json:"cwd"`
	BranchName  string `json:"branch_name"`
}

func (s *Server) execNotifyWorkingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ExecSvc == nil {
		writeError(w, http.StatusNotImplemented, "exec_svc_not_wired", "")
		return
	}
	var req notifyWorkingReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.ExecSvc.NotifyWorking(r.Context(), trservice.NotifyWorkingInput{
		ExecutionID: taskruntime.TaskExecutionID(req.ExecutionID),
		CWD:         req.CWD,
		BranchName:  req.BranchName,
		Actor:       observability.Actor("worker:" + req.ExecutionID),
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "working"})
}

// concludeReq closes out the state machine (working → completed +
// task → done). Companion to /admin/taskruntime/exec/notify-working.
type concludeReq struct {
	ExecutionID string `json:"execution_id"`
	Message     string `json:"message"`
}

func (s *Server) execConcludeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ExecSvc == nil {
		writeError(w, http.StatusNotImplemented, "exec_svc_not_wired", "")
		return
	}
	var req concludeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.ExecSvc.ConcludeSuccess(r.Context(), trservice.ConcludeSuccessInput{
		ExecutionID: taskruntime.TaskExecutionID(req.ExecutionID),
		Message:     req.Message,
		Actor:       observability.Actor("worker:" + req.ExecutionID),
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed"})
}

type reportFailureReq struct {
	ExecutionID string `json:"execution_id"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
}

func (s *Server) execReportFailureHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ExecSvc == nil {
		writeError(w, http.StatusNotImplemented, "exec_svc_not_wired", "")
		return
	}
	var req reportFailureReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.ExecSvc.ReportFailure(r.Context(), trservice.ReportFailureInput{
		ExecutionID: taskruntime.TaskExecutionID(req.ExecutionID),
		Reason:      req.Reason,
		Message:     req.Message,
		Actor:       observability.Actor("agent:" + req.ExecutionID),
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "failed"})
}

// =============================================================================
// DispatchSvc — Dispatch
// =============================================================================

type dispatchReq struct {
	TaskID               string `json:"task_id"`
	WorkerID             string `json:"worker_id"`
	AgentCLI             string `json:"agent_cli"`
	AgentInstanceID      string `json:"agent_instance_id"`
	BaseBranch           string `json:"base_branch"`
	ExecutionTimeoutSecs *int64 `json:"execution_timeout_seconds"`
}

func (s *Server) dispatchHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.DispatchSvc == nil {
		writeError(w, http.StatusNotImplemented, "dispatch_svc_not_wired", "")
		return
	}
	var req dispatchReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	in := dispatch.DispatchInput{
		TaskID:          taskruntime.TaskID(req.TaskID),
		WorkerID:        req.WorkerID,
		AgentCLI:        req.AgentCLI,
		AgentInstanceID: req.AgentInstanceID,
		BaseBranch:      req.BaseBranch,
		Actor:           d.Actor,
	}
	if req.ExecutionTimeoutSecs != nil {
		dur := time.Duration(*req.ExecutionTimeoutSecs) * time.Second
		in.ExecutionTimeoutOverride = &dur
	}
	res, err := d.DispatchSvc.Dispatch(r.Context(), in)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id": string(res.ExecutionID),
	})
}

// =============================================================================
// KillCoordinator — RequestKill (kill-execution)
// =============================================================================

type killExecReq struct {
	ExecutionID string `json:"execution_id"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
}

func (s *Server) killExecutionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.KillCoordinator == nil {
		writeError(w, http.StatusNotImplemented, "kill_coordinator_not_wired", "")
		return
	}
	var req killExecReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	reason := execution.KilledReason(req.Reason)
	if err := d.KillCoordinator.RequestKill(r.Context(),
		taskruntime.TaskExecutionID(req.ExecutionID),
		reason, req.Message, d.Actor); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id": req.ExecutionID,
		"status":       "kill_requested",
	})
}

// =============================================================================
// Projection helpers
// =============================================================================

func taskMap(t *task.Task) map[string]any {
	deps := t.DependsOnTaskIDs()
	depStrs := make([]string, len(deps))
	for i, d := range deps {
		depStrs[i] = string(d)
	}
	m := map[string]any{
		"id":                   string(t.ID()),
		"project_id":           t.ProjectID(),
		"title":                t.Title(),
		"description":          t.Description(),
		"status":               string(t.Status()),
		"priority":             string(t.Priority()),
		"requires_worktree":    t.RequiresWorktree(),
		"parent_task_id":       string(t.ParentTaskID()),
		"from_issue_id":        t.FromIssueID(),
		"conversation_id":      t.ConversationID(),
		"current_execution_id": string(t.CurrentExecutionID()),
		"depends_on_task_ids":  depStrs,
		"created_by":           t.CreatedBy(),
		"created_at":           t.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":           t.UpdatedAt().Format(time.RFC3339Nano),
		"version":              t.Version(),
	}
	return m
}

func executionMap(e *execution.TaskExecution) map[string]any {
	return map[string]any{
		"id":                          string(e.ID()),
		"task_id":                     string(e.TaskID()),
		"worker_id":                   e.WorkerID(),
		"agent_cli":                   e.AgentCLI(),
		"workspace_mode":              string(e.WorkspaceMode()),
		"status":                      string(e.Status()),
		"failed_reason":               string(e.FailedReason()),
		"failed_message":              e.FailedMessage(),
		"base_branch":                 e.BaseBranch(),
		"cwd":                         e.CWD(),
		"branch_name":                 e.BranchName(),
		"started_at":                  e.StartedAt().Format(time.RFC3339Nano),
		"working_seconds_accumulated": e.WorkingSecondsAccumulated(),
		"version":                     e.Version(),
	}
}

func irMap(ir *inputrequest.InputRequest) map[string]any {
	m := map[string]any{
		"id":           string(ir.ID()),
		"status":       string(ir.Status()),
		"execution_id": string(ir.TaskExecutionID()),
		"question":     ir.Question(),
		"options":      ir.Options(),
		"urgency":      string(ir.Urgency()),
		"created_at":   ir.CreatedAt().Format(time.RFC3339Nano),
	}
	if ra := ir.RespondedAt(); ra != nil {
		m["answer"] = ir.ResponseText()
		m["decided_by"] = ir.RespondedBy()
		m["decided_at"] = ra.Format(time.RFC3339Nano)
	}
	return m
}

func artifactMap(a *execution.Artifact) map[string]any {
	return map[string]any{
		"id":            string(a.ID()),
		"task_id":       string(a.TaskID()),
		"execution_id":  string(a.ExecutionID()),
		"kind":          a.Kind(),
		"title":         a.Title(),
		"blob_ref":      a.BlobRef(),
		"url":           a.URL(),
		"metadata_json": a.MetadataJSON(),
		"created_at":    a.CreatedAt().Format(time.RFC3339Nano),
		"created_by":    a.CreatedBy(),
	}
}
