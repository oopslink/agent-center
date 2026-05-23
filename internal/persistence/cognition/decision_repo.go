package cognition

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/cognition"
	
	"github.com/oopslink/agent-center/internal/persistence"
)

// DecisionRepo is the SQLite DecisionRecordRepository impl.
//
// Append-only by design: the interface declares only Append + read methods.
// No Update / Delete exists on either the AR or this repo — that's the
// invariant cognition/01 § 4.5 enforces at compile time.
type DecisionRepo struct {
	db *sql.DB
}

// NewDecisionRepo wires the repo against the given *sql.DB.
func NewDecisionRepo(db *sql.DB) *DecisionRepo {
	return &DecisionRepo{db: db}
}

// Append inserts a decision record. Returns ErrDecisionImmutable when a row
// with the same id already exists.
func (r *DecisionRepo) Append(ctx context.Context, d *cognition.DecisionRecord) error {
	if d == nil {
		return errors.New("decision_repo: nil decision")
	}
	if d.Rationale() == "" {
		return cognition.ErrRationaleRequired
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `
		INSERT INTO decision_records
		(id, invocation_id, kind, target_refs, rationale, outcome, outcome_message, created_at)
		VALUES (?,?,?,?,?,?,?,?)
	`,
		string(d.ID()),
		string(d.InvocationID()),
		string(d.Kind()),
		d.TargetRefsJSON(),
		d.Rationale(),
		string(d.Outcome()),
		d.OutcomeMessage(),
		formatTime(d.CreatedAt()),
	)
	if err != nil {
		if isUniqueViolation(err, "decision_records") {
			return cognition.ErrDecisionImmutable
		}
		return fmt.Errorf("decision_repo: insert: %w", err)
	}
	return nil
}

// FindByID loads a single decision row.
func (r *DecisionRepo) FindByID(ctx context.Context, id cognition.DecisionID) (*cognition.DecisionRecord, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, `
		SELECT id, invocation_id, kind, target_refs, rationale, outcome, outcome_message, created_at
		FROM decision_records WHERE id = ?
	`, string(id))
	d, err := scanDecisionRow(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, cognition.ErrDecisionNotFound
		}
		return nil, err
	}
	return d, nil
}

// FindByInvocationID lists all decisions for an invocation.
func (r *DecisionRepo) FindByInvocationID(ctx context.Context, id cognition.InvocationID) ([]*cognition.DecisionRecord, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, `
		SELECT id, invocation_id, kind, target_refs, rationale, outcome, outcome_message, created_at
		FROM decision_records WHERE invocation_id = ?
		ORDER BY created_at ASC, id ASC
	`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*cognition.DecisionRecord
	for rows.Next() {
		d, err := scanDecisionRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Find returns rows matching filter.
func (r *DecisionRepo) Find(ctx context.Context, filter cognition.DecisionFilter) ([]*cognition.DecisionRecord, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	if filter.Limit > cognition.MaxDecisionLimit {
		return nil, cognition.ErrDecisionLimitTooLarge
	}
	var sb strings.Builder
	sb.WriteString(`
		SELECT id, invocation_id, kind, target_refs, rationale, outcome, outcome_message, created_at
		FROM decision_records WHERE 1=1`)
	var args []any
	if filter.InvocationID != nil {
		sb.WriteString(" AND invocation_id = ?")
		args = append(args, string(*filter.InvocationID))
	}
	if filter.Kind != nil {
		sb.WriteString(" AND kind = ?")
		args = append(args, string(*filter.Kind))
	}
	if filter.Cursor != nil {
		sb.WriteString(" AND id > ?")
		args = append(args, string(*filter.Cursor))
	}
	sb.WriteString(" ORDER BY id ASC")
	limit := filter.Limit
	if limit <= 0 {
		limit = cognition.DefaultDecisionLimit
	}
	sb.WriteString(" LIMIT ?")
	args = append(args, limit)

	rows, err := exec.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*cognition.DecisionRecord
	for rows.Next() {
		d, err := scanDecisionRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanDecisionRow(scan func(...any) error) (*cognition.DecisionRecord, error) {
	var (
		id, invocationID, kind, targetRefs, rationale, outcome string
		outcomeMessage                                          sql.NullString
		createdAtS                                              string
	)
	if err := scan(&id, &invocationID, &kind, &targetRefs, &rationale,
		&outcome, &outcomeMessage, &createdAtS); err != nil {
		return nil, err
	}
	createdAt, err := parseTime(createdAtS)
	if err != nil {
		return nil, fmt.Errorf("scan decision: created_at: %w", err)
	}
	return cognition.RehydrateDecision(cognition.RehydrateDecisionInput{
		ID:             cognition.DecisionID(id),
		InvocationID:   cognition.InvocationID(invocationID),
		Kind:           cognition.DecisionKind(kind),
		TargetRefsJSON: targetRefs,
		Rationale:      rationale,
		Outcome:        cognition.DecisionOutcome(outcome),
		OutcomeMessage: outcomeMessage.String,
		CreatedAt:      createdAt,
	})
}
