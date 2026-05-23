// Package cognition (persistence) implements the SQLite-backed repositories
// for the Cognition BC (SupervisorInvocation + DecisionRecord).
//
// Per 02-persistence-schema § 8.3: ULID PKs, ISO8601 timestamps, JSON-as-
// TEXT for trigger_event_ids / target_refs, partial unique index for the
// running-per-scope invariant, CAS UPDATE via version column.
package cognition

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// InvocationRepo is the SQLite SupervisorInvocationRepository impl.
type InvocationRepo struct {
	db *sql.DB
}

// NewInvocationRepo wires the repo against the given *sql.DB.
func NewInvocationRepo(db *sql.DB) *InvocationRepo {
	return &InvocationRepo{db: db}
}

// Save inserts a freshly-spawned (status=running) invocation.
func (r *InvocationRepo) Save(ctx context.Context, inv *cognition.SupervisorInvocation) error {
	if inv == nil {
		return errors.New("invocation_repo: nil invocation")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	triggerJSON, err := marshalTriggerEvents(inv.TriggerEvents())
	if err != nil {
		return err
	}
	usageJSON, err := json.Marshal(inv.TokenUsage())
	if err != nil {
		return fmt.Errorf("invocation_repo: marshal token_usage: %w", err)
	}
	var agentInstanceID any
	if inv.AgentInstanceID() != "" {
		agentInstanceID = inv.AgentInstanceID()
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO supervisor_invocations
		(id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		 started_at, ended_at, failed_reason, failed_message, timed_out_at,
		 token_usage, decisions_made, prompt_blob_ref, created_at, updated_at, version,
		 agent_instance_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		string(inv.ID()),
		string(inv.Scope().Kind()),
		inv.Scope().Key(),
		string(triggerJSON),
		string(inv.Status()),
		inv.HardTimeoutSeconds(),
		formatTime(inv.StartedAt()),
		formatNullableTime(inv.EndedAt()),
		string(inv.FailedReason()),
		inv.FailedMessage(),
		formatNullableTime(inv.TimedOutAt()),
		string(usageJSON),
		inv.DecisionsMade(),
		inv.PromptBlobRef(),
		formatTime(inv.CreatedAt()),
		formatTime(inv.UpdatedAt()),
		inv.Version(),
		agentInstanceID,
	)
	if err != nil {
		if isUniqueViolation(err, "uniq_invocations_running_per_scope") {
			return cognition.ErrScopeKeyRunningExists
		}
		return fmt.Errorf("invocation_repo: insert: %w", err)
	}
	return nil
}

// UpdateStatusToTerminal CAS-updates a running row to a terminal state.
func (r *InvocationRepo) UpdateStatusToTerminal(ctx context.Context, inv *cognition.SupervisorInvocation) error {
	if inv == nil {
		return errors.New("invocation_repo: nil invocation")
	}
	if !inv.Status().IsTerminal() {
		return fmt.Errorf("invocation_repo: UpdateStatusToTerminal requires terminal status, got %s", inv.Status())
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	usageJSON, err := json.Marshal(inv.TokenUsage())
	if err != nil {
		return fmt.Errorf("invocation_repo: marshal token_usage: %w", err)
	}
	res, err := exec.ExecContext(ctx, `
		UPDATE supervisor_invocations
		SET status=?, ended_at=?, failed_reason=?, failed_message=?, timed_out_at=?,
		    token_usage=?, decisions_made=?, updated_at=?, version=version+1
		WHERE id=? AND version=?
	`,
		string(inv.Status()),
		formatNullableTime(inv.EndedAt()),
		string(inv.FailedReason()),
		inv.FailedMessage(),
		formatNullableTime(inv.TimedOutAt()),
		string(usageJSON),
		inv.DecisionsMade(),
		formatTime(inv.UpdatedAt()),
		string(inv.ID()),
		inv.Version()-1, // we incremented version on the in-memory AR
	)
	if err != nil {
		return fmt.Errorf("invocation_repo: update: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return cognition.ErrInvocationVersionConflict
	}
	return nil
}

// FindByID loads a single invocation.
func (r *InvocationRepo) FindByID(ctx context.Context, id cognition.InvocationID) (*cognition.SupervisorInvocation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, `
		SELECT id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		       started_at, ended_at, failed_reason, failed_message, timed_out_at,
		       token_usage, decisions_made, prompt_blob_ref, created_at, updated_at, version, agent_instance_id
		FROM supervisor_invocations WHERE id = ?
	`, string(id))
	inv, err := scanInvocationRow(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, cognition.ErrInvocationNotFound
		}
		return nil, err
	}
	return inv, nil
}

// FindRunningByScope returns the unique running invocation for a scope.
func (r *InvocationRepo) FindRunningByScope(ctx context.Context, scope cognition.InvocationScope) (*cognition.SupervisorInvocation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, `
		SELECT id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		       started_at, ended_at, failed_reason, failed_message, timed_out_at,
		       token_usage, decisions_made, prompt_blob_ref, created_at, updated_at, version, agent_instance_id
		FROM supervisor_invocations
		WHERE scope_kind = ? AND scope_key = ? AND status = 'running'
		LIMIT 1
	`, string(scope.Kind()), scope.Key())
	inv, err := scanInvocationRow(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, cognition.ErrInvocationNotFound
		}
		return nil, err
	}
	return inv, nil
}

// FindRunning returns all running invocations (crash recovery).
func (r *InvocationRepo) FindRunning(ctx context.Context) ([]*cognition.SupervisorInvocation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, `
		SELECT id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		       started_at, ended_at, failed_reason, failed_message, timed_out_at,
		       token_usage, decisions_made, prompt_blob_ref, created_at, updated_at, version, agent_instance_id
		FROM supervisor_invocations WHERE status = 'running' ORDER BY started_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*cognition.SupervisorInvocation
	for rows.Next() {
		inv, err := scanInvocationRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// Find returns rows matching filter.
func (r *InvocationRepo) Find(ctx context.Context, filter cognition.InvocationFilter) ([]*cognition.SupervisorInvocation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	if filter.Limit > cognition.MaxInvocationLimit {
		return nil, cognition.ErrInvocationLimitTooLarge
	}
	var sb strings.Builder
	sb.WriteString(`
		SELECT id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		       started_at, ended_at, failed_reason, failed_message, timed_out_at,
		       token_usage, decisions_made, prompt_blob_ref, created_at, updated_at, version, agent_instance_id
		FROM supervisor_invocations WHERE 1=1`)
	var args []any
	if filter.Status != nil {
		sb.WriteString(" AND status = ?")
		args = append(args, string(*filter.Status))
	}
	if filter.ScopeKind != nil {
		sb.WriteString(" AND scope_kind = ?")
		args = append(args, string(*filter.ScopeKind))
	}
	if filter.ScopeKey != nil {
		sb.WriteString(" AND scope_key = ?")
		args = append(args, *filter.ScopeKey)
	}
	if filter.Since != nil {
		sb.WriteString(" AND started_at >= ?")
		args = append(args, formatTime(filter.Since.UTC()))
	}
	if filter.Until != nil {
		sb.WriteString(" AND started_at < ?")
		args = append(args, formatTime(filter.Until.UTC()))
	}
	if filter.Cursor != nil {
		sb.WriteString(" AND id > ?")
		args = append(args, string(*filter.Cursor))
	}
	sb.WriteString(" ORDER BY id ASC")
	limit := filter.Limit
	if limit <= 0 {
		limit = cognition.DefaultInvocationLimit
	}
	sb.WriteString(" LIMIT ?")
	args = append(args, limit)

	rows, err := exec.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*cognition.SupervisorInvocation
	for rows.Next() {
		inv, err := scanInvocationRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func scanInvocationRow(scan func(...any) error) (*cognition.SupervisorInvocation, error) {
	var (
		id, scopeKind, scopeKey, triggerJSON, status, usageJSON, blobRef string
		hardTimeoutSeconds, decisionsMade                                int
		startedAtS, createdAtS, updatedAtS                               string
		endedAtS, timedOutAtS                                            sql.NullString
		failedReason, failedMessage                                      sql.NullString
		version                                                          int64
		agentInstanceID                                                  sql.NullString
	)
	if err := scan(&id, &scopeKind, &scopeKey, &triggerJSON, &status, &hardTimeoutSeconds,
		&startedAtS, &endedAtS, &failedReason, &failedMessage, &timedOutAtS,
		&usageJSON, &decisionsMade, &blobRef, &createdAtS, &updatedAtS, &version, &agentInstanceID); err != nil {
		return nil, err
	}
	scope, err := cognition.NewInvocationScope(cognition.ScopeKind(scopeKind), scopeKey)
	if err != nil {
		return nil, fmt.Errorf("scan invocation: scope: %w", err)
	}
	triggerSet, err := unmarshalTriggerEvents(triggerJSON)
	if err != nil {
		return nil, err
	}
	var usage cognition.TokenUsage
	if usageJSON != "" {
		if err := json.Unmarshal([]byte(usageJSON), &usage); err != nil {
			return nil, fmt.Errorf("scan invocation: token_usage: %w", err)
		}
	}
	startedAt, err := parseTime(startedAtS)
	if err != nil {
		return nil, fmt.Errorf("scan invocation: started_at: %w", err)
	}
	createdAt, err := parseTime(createdAtS)
	if err != nil {
		return nil, fmt.Errorf("scan invocation: created_at: %w", err)
	}
	updatedAt, err := parseTime(updatedAtS)
	if err != nil {
		return nil, fmt.Errorf("scan invocation: updated_at: %w", err)
	}
	var endedAt, timedOutAt *time.Time
	if endedAtS.Valid && endedAtS.String != "" {
		t, err := parseTime(endedAtS.String)
		if err != nil {
			return nil, fmt.Errorf("scan invocation: ended_at: %w", err)
		}
		endedAt = &t
	}
	if timedOutAtS.Valid && timedOutAtS.String != "" {
		t, err := parseTime(timedOutAtS.String)
		if err != nil {
			return nil, fmt.Errorf("scan invocation: timed_out_at: %w", err)
		}
		timedOutAt = &t
	}
	return cognition.Rehydrate(cognition.RehydrateInput{
		ID:                 cognition.InvocationID(id),
		AgentInstanceID:    agentInstanceID.String,
		Scope:              scope,
		TriggerEvents:      triggerSet,
		Status:             cognition.InvocationStatus(status),
		HardTimeoutSeconds: hardTimeoutSeconds,
		StartedAt:          startedAt,
		EndedAt:            endedAt,
		FailedReason:       cognition.InvocationFailedReason(failedReason.String),
		FailedMessage:      failedMessage.String,
		TimedOutAt:         timedOutAt,
		TokenUsage:         usage,
		DecisionsMade:      decisionsMade,
		PromptBlobRef:      blobRef,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
		Version:            version,
	})
}

func marshalTriggerEvents(set cognition.TriggerEventSet) ([]byte, error) {
	ids := set.IDs()
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return json.Marshal(out)
}

func unmarshalTriggerEvents(s string) (cognition.TriggerEventSet, error) {
	var raw []string
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return cognition.TriggerEventSet{}, fmt.Errorf("trigger_event_ids: %w", err)
	}
	ids := make([]observability.EventID, len(raw))
	for i, r := range raw {
		ids[i] = observability.EventID(r)
	}
	return cognition.NewTriggerEventSet(ids)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func formatNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func isUniqueViolation(err error, indexHint string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// modernc.org/sqlite reports "UNIQUE constraint failed: <table>.<col>"
	// or "constraint failed: UNIQUE ..." depending on version. The partial
	// unique index name appears in some versions.
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return true
	}
	if indexHint != "" && strings.Contains(msg, indexHint) {
		return true
	}
	return false
}
