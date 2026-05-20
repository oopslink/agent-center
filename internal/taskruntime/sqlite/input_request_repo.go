package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

// InputRequestRepo implements inputrequest.Repository.
type InputRequestRepo struct {
	db *sql.DB
}

// NewInputRequestRepo constructs the repo.
func NewInputRequestRepo(db *sql.DB) *InputRequestRepo { return &InputRequestRepo{db: db} }

const irSelect = `SELECT
	id, task_execution_id, status, question, options, urgency,
	requested_at, responded_at, responded_by, response_text,
	ended_reason, ended_message, created_at, updated_at, version
FROM input_requests`

// Save inserts a new InputRequest row.
func (r *InputRequestRepo) Save(ctx context.Context, ir *inputrequest.InputRequest) error {
	if ir == nil {
		return errors.New("input_request repo: nil request")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	optionsJSON, err := ir.OptionsJSON()
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO input_requests (
		id, task_execution_id, status, question, options, urgency,
		requested_at, responded_at, responded_by, response_text,
		ended_reason, ended_message, created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(ir.ID()),
		string(ir.TaskExecutionID()),
		string(ir.Status()),
		ir.Question(),
		nullString(optionsJSON),
		string(ir.Urgency()),
		ir.RequestedAt().Format(timeFormat),
		nullTimePtrStr(ir.RespondedAt()),
		nullString(ir.RespondedBy()),
		nullString(ir.ResponseText()),
		nullString(ir.EndedReason()),
		nullString(ir.EndedMessage()),
		ir.CreatedAt().Format(timeFormat),
		ir.UpdatedAt().Format(timeFormat),
		ir.Version(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			return errors.New("input_request: id duplicate")
		}
		return err
	}
	return nil
}

// Update is the CAS UPDATE path.
func (r *InputRequestRepo) Update(ctx context.Context, ir *inputrequest.InputRequest) error {
	if ir == nil {
		return errors.New("input_request repo: nil request")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	optionsJSON, err := ir.OptionsJSON()
	if err != nil {
		return err
	}
	const stmt = `UPDATE input_requests SET
		status = ?, question = ?, options = ?, urgency = ?,
		responded_at = ?, responded_by = ?, response_text = ?,
		ended_reason = ?, ended_message = ?, updated_at = ?, version = ?
	WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(ir.Status()),
		ir.Question(),
		nullString(optionsJSON),
		string(ir.Urgency()),
		nullTimePtrStr(ir.RespondedAt()),
		nullString(ir.RespondedBy()),
		nullString(ir.ResponseText()),
		nullString(ir.EndedReason()),
		nullString(ir.EndedMessage()),
		ir.UpdatedAt().Format(timeFormat),
		ir.Version(),
		string(ir.ID()),
		ir.Version()-1,
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
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM input_requests WHERE id = ?`, string(ir.ID()))
		if scanErr := row.Scan(&existing); scanErr == nil {
			if existing == 0 {
				return inputrequest.ErrInputRequestNotFound
			}
		}
		return inputrequest.ErrInputRequestVersionConflict
	}
	return nil
}

// FindByID returns an InputRequest by id.
func (r *InputRequestRepo) FindByID(ctx context.Context, id taskruntime.InputRequestID) (*inputrequest.InputRequest, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, irSelect+` WHERE id = ?`, string(id))
	ir, err := scanInputRequest(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, inputrequest.ErrInputRequestNotFound
	}
	return ir, err
}

// FindByTaskExecutionID returns the (most recent) InputRequest for an
// execution. v1 invariant: only one IR per execution at a time, but
// historical resolved IRs may persist.
func (r *InputRequestRepo) FindByTaskExecutionID(ctx context.Context, executionID taskruntime.TaskExecutionID) (*inputrequest.InputRequest, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx,
		irSelect+` WHERE task_execution_id = ? ORDER BY created_at DESC LIMIT 1`,
		string(executionID))
	ir, err := scanInputRequest(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, inputrequest.ErrInputRequestNotFound
	}
	return ir, err
}

// FindPending returns InputRequests pending older than threshold.
func (r *InputRequestRepo) FindPending(ctx context.Context, olderThan time.Time) ([]*inputrequest.InputRequest, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx,
		irSelect+` WHERE status = 'pending' AND requested_at < ? ORDER BY requested_at`,
		olderThan.UTC().Format(timeFormat))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*inputrequest.InputRequest
	for rows.Next() {
		ir, err := scanInputRequest(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, ir)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanInputRequest(scan func(...any) error) (*inputrequest.InputRequest, error) {
	var (
		id           string
		executionID  string
		status       string
		question     string
		optionsRaw   sql.NullString
		urgency      string
		requestedRaw string
		respondedRaw sql.NullString
		respondedBy  sql.NullString
		responseText sql.NullString
		endedReason  sql.NullString
		endedMessage sql.NullString
		createdRaw   string
		updatedRaw   string
		version      int
	)
	if err := scan(&id, &executionID, &status, &question, &optionsRaw, &urgency,
		&requestedRaw, &respondedRaw, &respondedBy, &responseText,
		&endedReason, &endedMessage, &createdRaw, &updatedRaw, &version); err != nil {
		return nil, err
	}
	var options []string
	if optionsRaw.Valid && optionsRaw.String != "" {
		var err error
		options, err = unmarshalStringList(optionsRaw.String)
		if err != nil {
			return nil, err
		}
	}
	requestedAt, err := parseTimeStr(sql.NullString{String: requestedRaw, Valid: true})
	if err != nil {
		return nil, err
	}
	respondedAt, err := parseTimePtrStr(respondedRaw)
	if err != nil {
		return nil, err
	}
	createdAt, err := parseTimeStr(sql.NullString{String: createdRaw, Valid: true})
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTimeStr(sql.NullString{String: updatedRaw, Valid: true})
	if err != nil {
		return nil, err
	}
	return inputrequest.Rehydrate(inputrequest.RehydrateInput{
		ID:              taskruntime.InputRequestID(id),
		TaskExecutionID: taskruntime.TaskExecutionID(executionID),
		Status:          inputrequest.Status(status),
		Question:        question,
		Options:         options,
		Urgency:         inputrequest.Urgency(urgency),
		RequestedAt:     requestedAt,
		RespondedAt:     respondedAt,
		RespondedBy:     respondedBy.String,
		ResponseText:    responseText.String,
		EndedReason:     endedReason.String,
		EndedMessage:    endedMessage.String,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		Version:         version,
	})
}
