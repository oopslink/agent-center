package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// TaskExecutionRepo implements execution.Repository.
type TaskExecutionRepo struct {
	db *sql.DB
}

// NewTaskExecutionRepo constructs the repo.
func NewTaskExecutionRepo(db *sql.DB) *TaskExecutionRepo { return &TaskExecutionRepo{db: db} }

const taskExecSelect = `SELECT
	id, task_id, worker_id, agent_cli, workspace_mode, cwd, branch_name, base_branch,
	priority, eta_at, execution_timeout_override, working_seconds_accumulated,
	status, dispatch_state, pending_input_request_id,
	started_at, working_started_at, cancel_requested_at, cancel_reason, cancel_message,
	ended_at, completed_reason, completed_message, failed_reason, failed_message,
	killed_reason, killed_message, created_at, updated_at, version
FROM task_executions`

// Save inserts a new TaskExecution row.
func (r *TaskExecutionRepo) Save(ctx context.Context, e *execution.TaskExecution) error {
	if e == nil {
		return errors.New("execution repo: nil execution")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	timeoutOverride := nullDuration(e.ExecutionTimeoutOverride())
	const stmt = `INSERT INTO task_executions (
		id, task_id, worker_id, agent_cli, workspace_mode, cwd, branch_name, base_branch,
		priority, eta_at, execution_timeout_override, working_seconds_accumulated,
		status, dispatch_state, pending_input_request_id,
		started_at, working_started_at, cancel_requested_at, cancel_reason, cancel_message,
		ended_at, completed_reason, completed_message, failed_reason, failed_message,
		killed_reason, killed_message, created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		string(e.ID()),
		string(e.TaskID()),
		e.WorkerID(),
		e.AgentCLI(),
		string(e.WorkspaceMode()),
		nullString(e.CWD()),
		nullString(e.BranchName()),
		nullString(e.BaseBranch()),
		e.Priority(),
		nullTimePtrStr(e.EtaAt()),
		timeoutOverride,
		e.WorkingSecondsAccumulated(),
		string(e.Status()),
		string(e.DispatchState()),
		nullString(string(e.PendingInputRequestID())),
		e.StartedAt().Format(timeFormat),
		nullTimePtrStr(e.WorkingStartedAt()),
		nullTimePtrStr(e.CancelRequestedAt()),
		nullString(e.CancelReason()),
		nullString(e.CancelMessage()),
		nullTimePtrStr(e.EndedAt()),
		nullString(string(e.CompletedReason())),
		nullString(e.CompletedMessage()),
		nullString(string(e.FailedReason())),
		nullString(e.FailedMessage()),
		nullString(string(e.KilledReason())),
		nullString(e.KilledMessage()),
		e.CreatedAt().Format(timeFormat),
		e.UpdatedAt().Format(timeFormat),
		e.Version(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			return fmt.Errorf("execution: id %s already exists", e.ID())
		}
		return err
	}
	return nil
}

// Update is the CAS UPDATE path.
func (r *TaskExecutionRepo) Update(ctx context.Context, e *execution.TaskExecution) error {
	if e == nil {
		return errors.New("execution repo: nil execution")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	timeoutOverride := nullDuration(e.ExecutionTimeoutOverride())
	const stmt = `UPDATE task_executions SET
		cwd = ?, branch_name = ?, base_branch = ?, priority = ?, eta_at = ?,
		execution_timeout_override = ?, working_seconds_accumulated = ?,
		status = ?, dispatch_state = ?, pending_input_request_id = ?,
		working_started_at = ?, cancel_requested_at = ?, cancel_reason = ?, cancel_message = ?,
		ended_at = ?, completed_reason = ?, completed_message = ?, failed_reason = ?, failed_message = ?,
		killed_reason = ?, killed_message = ?, updated_at = ?, version = ?
	WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		nullString(e.CWD()),
		nullString(e.BranchName()),
		nullString(e.BaseBranch()),
		e.Priority(),
		nullTimePtrStr(e.EtaAt()),
		timeoutOverride,
		e.WorkingSecondsAccumulated(),
		string(e.Status()),
		string(e.DispatchState()),
		nullString(string(e.PendingInputRequestID())),
		nullTimePtrStr(e.WorkingStartedAt()),
		nullTimePtrStr(e.CancelRequestedAt()),
		nullString(e.CancelReason()),
		nullString(e.CancelMessage()),
		nullTimePtrStr(e.EndedAt()),
		nullString(string(e.CompletedReason())),
		nullString(e.CompletedMessage()),
		nullString(string(e.FailedReason())),
		nullString(e.FailedMessage()),
		nullString(string(e.KilledReason())),
		nullString(e.KilledMessage()),
		e.UpdatedAt().Format(timeFormat),
		e.Version(),
		string(e.ID()),
		e.Version()-1,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var existing int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_executions WHERE id = ?`, string(e.ID()))
		if scanErr := row.Scan(&existing); scanErr == nil {
			if existing == 0 {
				return execution.ErrTaskExecutionNotFound
			}
		}
		return execution.ErrTaskExecutionVersionConflict
	}
	return nil
}

// FindByID returns an execution by id.
func (r *TaskExecutionRepo) FindByID(ctx context.Context, id taskruntime.TaskExecutionID) (*execution.TaskExecution, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, taskExecSelect+` WHERE id = ?`, string(id))
	e, err := scanTaskExecution(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, execution.ErrTaskExecutionNotFound
	}
	return e, err
}

// FindByTaskID returns executions for a task ordered by created_at DESC.
func (r *TaskExecutionRepo) FindByTaskID(ctx context.Context, taskID taskruntime.TaskID) ([]*execution.TaskExecution, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		taskExecSelect+` WHERE task_id = ? ORDER BY created_at DESC`, string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskExecutions(rows)
}

// FindByWorkerID returns executions for a worker, optionally filtered by
// status(es).
func (r *TaskExecutionRepo) FindByWorkerID(ctx context.Context, workerID string, statuses ...execution.Status) ([]*execution.TaskExecution, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	if len(statuses) == 0 {
		rows, err := exec.QueryContext(ctx,
			taskExecSelect+` WHERE worker_id = ? ORDER BY created_at DESC`, workerID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanTaskExecutions(rows)
	}
	placeholders := "?"
	args := []any{workerID, string(statuses[0])}
	for i := 1; i < len(statuses); i++ {
		placeholders += ", ?"
		args = append(args, string(statuses[i]))
	}
	q := taskExecSelect + ` WHERE worker_id = ? AND status IN (` + placeholders + `) ORDER BY created_at DESC`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskExecutions(rows)
}

// FindActive returns all executions in submitted / working / input_required.
func (r *TaskExecutionRepo) FindActive(ctx context.Context) ([]*execution.TaskExecution, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		taskExecSelect+` WHERE status IN ('submitted','working','input_required') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskExecutions(rows)
}

// FindPendingAckOlderThan returns executions with dispatch_state=pending_ack
// created_at < cutoff (formatted timestamp).
func (r *TaskExecutionRepo) FindPendingAckOlderThan(ctx context.Context, cutoff string) ([]*execution.TaskExecution, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		taskExecSelect+` WHERE dispatch_state = 'pending_ack' AND created_at < ?`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskExecutions(rows)
}

// FindSubmittedOlderThan returns executions stuck in submitted status with
// created_at < cutoff.
func (r *TaskExecutionRepo) FindSubmittedOlderThan(ctx context.Context, cutoff string) ([]*execution.TaskExecution, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		taskExecSelect+` WHERE status = 'submitted' AND created_at < ?`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskExecutions(rows)
}

func scanTaskExecutions(rows *sql.Rows) ([]*execution.TaskExecution, error) {
	var out []*execution.TaskExecution
	for rows.Next() {
		e, err := scanTaskExecution(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanTaskExecution(scan func(...any) error) (*execution.TaskExecution, error) {
	var (
		id                  string
		taskID              string
		workerID            string
		agentCLI            string
		workspaceMode       string
		cwd                 sql.NullString
		branchName          sql.NullString
		baseBranch          sql.NullString
		priority            string
		etaAtRaw            sql.NullString
		timeoutOverride     sql.NullInt64
		workingSecondsAccum int64
		status              string
		dispatchState       string
		pendingIR           sql.NullString
		startedAtRaw        string
		workingStartedRaw   sql.NullString
		cancelRequestedRaw  sql.NullString
		cancelReason        sql.NullString
		cancelMessage       sql.NullString
		endedAtRaw          sql.NullString
		completedReason     sql.NullString
		completedMessage    sql.NullString
		failedReason        sql.NullString
		failedMessage       sql.NullString
		killedReason        sql.NullString
		killedMessage       sql.NullString
		createdAtRaw        string
		updatedAtRaw        string
		version             int
	)
	if err := scan(&id, &taskID, &workerID, &agentCLI, &workspaceMode, &cwd, &branchName, &baseBranch,
		&priority, &etaAtRaw, &timeoutOverride, &workingSecondsAccum,
		&status, &dispatchState, &pendingIR,
		&startedAtRaw, &workingStartedRaw, &cancelRequestedRaw, &cancelReason, &cancelMessage,
		&endedAtRaw, &completedReason, &completedMessage, &failedReason, &failedMessage,
		&killedReason, &killedMessage, &createdAtRaw, &updatedAtRaw, &version); err != nil {
		return nil, err
	}
	startedAt, err := parseTimeStr(sql.NullString{String: startedAtRaw, Valid: true})
	if err != nil {
		return nil, err
	}
	workingStarted, err := parseTimePtrStr(workingStartedRaw)
	if err != nil {
		return nil, err
	}
	cancelRequested, err := parseTimePtrStr(cancelRequestedRaw)
	if err != nil {
		return nil, err
	}
	endedAt, err := parseTimePtrStr(endedAtRaw)
	if err != nil {
		return nil, err
	}
	eta, err := parseTimePtrStr(etaAtRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := parseTimeStr(sql.NullString{String: createdAtRaw, Valid: true})
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTimeStr(sql.NullString{String: updatedAtRaw, Valid: true})
	if err != nil {
		return nil, err
	}
	return execution.Rehydrate(execution.RehydrateInput{
		ID:                        taskruntime.TaskExecutionID(id),
		TaskID:                    taskruntime.TaskID(taskID),
		WorkerID:                  workerID,
		AgentCLI:                  agentCLI,
		WorkspaceMode:             execution.WorkspaceMode(workspaceMode),
		CWD:                       cwd.String,
		BranchName:                branchName.String,
		BaseBranch:                baseBranch.String,
		Priority:                  priority,
		EtaAt:                     eta,
		ExecutionTimeoutOverride:  parseDurationFromInt(timeoutOverride),
		WorkingSecondsAccumulated: workingSecondsAccum,
		Status:                    execution.Status(status),
		DispatchState:             execution.DispatchState(dispatchState),
		PendingInputRequestID:     taskruntime.InputRequestID(pendingIR.String),
		StartedAt:                 startedAt,
		WorkingStartedAt:          workingStarted,
		CancelRequestedAt:         cancelRequested,
		CancelReason:              cancelReason.String,
		CancelMessage:             cancelMessage.String,
		EndedAt:                   endedAt,
		CompletedReason:           execution.CompletedReason(completedReason.String),
		CompletedMessage:          completedMessage.String,
		FailedReason:              execution.FailedReason(failedReason.String),
		FailedMessage:             failedMessage.String,
		KilledReason:              execution.KilledReason(killedReason.String),
		KilledMessage:             killedMessage.String,
		CreatedAt:                 createdAt,
		UpdatedAt:                 updatedAt,
		Version:                   version,
	})
}
