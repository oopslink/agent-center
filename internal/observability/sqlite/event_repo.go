// Package sqlite implements the Observability BC SQLite repositories.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// EventRepo is the SQLite-backed implementation of
// observability.EventRepository. The events table is append-only (no UPDATE
// / DELETE paths exposed); Append writes a single row using the executor
// from ctx (so callers can carry a tx) or the underlying *sql.DB.
type EventRepo struct {
	db  *sql.DB
	seq *atomic.Int64
}

// NewEventRepo constructs a repo, initialising the in-memory seq counter
// from MAX(seq) in the events table.
//
// Per observability/00 § 1.1 + 02-persistence § 8.2.2: seq is monotonic per
// process; on cold start we read the last persisted value and continue.
// Multi-process / HA is out of scope for v1 (plan § 6 R2).
func NewEventRepo(ctx context.Context, db *sql.DB) (*EventRepo, error) {
	if db == nil {
		return nil, errors.New("sqlite event repo: nil db")
	}
	var maxSeq sql.NullInt64
	row := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events`)
	if err := row.Scan(&maxSeq); err != nil {
		return nil, fmt.Errorf("sqlite event repo: init seq: %w", err)
	}
	r := &EventRepo{db: db, seq: &atomic.Int64{}}
	r.seq.Store(maxSeq.Int64)
	return r, nil
}

// NextSeq returns the next monotonic sequence number and atomically
// increments the counter. Exported so EventSink can pre-allocate seq before
// it constructs an Event (the Event AR requires Seq at construction).
func (r *EventRepo) NextSeq() int64 {
	return r.seq.Add(1)
}

// Append inserts the event row.
func (r *EventRepo) Append(ctx context.Context, e *observability.Event) error {
	if e == nil {
		return errors.New("sqlite event repo: nil event")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	payload, err := e.PayloadJSON()
	if err != nil {
		return fmt.Errorf("event payload marshal: %w", err)
	}
	refs, err := e.RefsJSON()
	if err != nil {
		return fmt.Errorf("event refs marshal: %w", err)
	}
	var correlationID, decisionID any
	if e.CorrelationID() != "" {
		correlationID = e.CorrelationID()
	}
	if e.DecisionID() != "" {
		decisionID = e.DecisionID()
	}
	const stmt = `INSERT INTO events
	(id, occurred_at, seq, event_type, refs, actor, payload, correlation_id, decision_id, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(e.ID()),
		e.OccurredAt().UTC().Format(time.RFC3339Nano),
		e.Seq(),
		string(e.Type()),
		string(refs),
		string(e.Actor()),
		string(payload),
		correlationID,
		decisionID,
		e.CreatedAt().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return observability.ErrEventAlreadyExists
		}
		return err
	}
	return nil
}

// FindByID returns the event with the given id.
func (r *EventRepo) FindByID(ctx context.Context, id observability.EventID) (*observability.Event, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const stmt = `SELECT id, occurred_at, seq, event_type, refs, actor, payload, correlation_id, decision_id, created_at
		FROM events WHERE id = ?`
	row := exec.QueryRowContext(ctx, stmt, string(id))
	e, err := scanEvent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, observability.ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// Find returns events matching filter, ordered by id ASC.
func (r *EventRepo) Find(ctx context.Context, filter observability.EventQueryFilter) ([]*observability.Event, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	sb := strings.Builder{}
	sb.WriteString(`SELECT id, occurred_at, seq, event_type, refs, actor, payload, correlation_id, decision_id, created_at
		FROM events WHERE 1=1`)
	var args []any
	if filter.EventType != nil {
		sb.WriteString(` AND event_type = ?`)
		args = append(args, string(*filter.EventType))
	}
	if filter.CorrelationID != nil {
		sb.WriteString(` AND correlation_id = ?`)
		args = append(args, *filter.CorrelationID)
	}
	if filter.DecisionID != nil {
		sb.WriteString(` AND decision_id = ?`)
		args = append(args, *filter.DecisionID)
	}
	if filter.Since != nil {
		sb.WriteString(` AND occurred_at >= ?`)
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	if filter.Cursor != nil {
		sb.WriteString(` AND id > ?`)
		args = append(args, string(*filter.Cursor))
	}
	if !filter.Refs.IsEmpty() {
		// Refs are stored as a JSON string; we keep it dialect-agnostic by
		// using LIKE on serialised key/value pairs (conventions § 9.0
		// forbids json_extract). Each filter key becomes a LIKE clause.
		for col, val := range refsLikeMap(filter.Refs) {
			sb.WriteString(` AND refs LIKE ?`)
			args = append(args, fmt.Sprintf(`%%"%s":"%s"%%`, col, val))
		}
	}
	sb.WriteString(` ORDER BY id ASC`)
	limit := filter.Limit
	if limit <= 0 {
		limit = observability.DefaultEventQueryLimit
	}
	sb.WriteString(` LIMIT ?`)
	args = append(args, limit)

	rows, err := exec.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*observability.Event
	for rows.Next() {
		e, err := scanEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func refsLikeMap(f observability.EventRefsFilter) map[string]string {
	m := map[string]string{}
	if f.WorkerID != "" {
		m["worker_id"] = f.WorkerID
	}
	if f.ProjectID != "" {
		m["project_id"] = f.ProjectID
	}
	if f.ProposalID != "" {
		m["proposal_id"] = f.ProposalID
	}
	if f.MappingID != "" {
		m["mapping_id"] = f.MappingID
	}
	if f.ConversationID != "" {
		m["conversation_id"] = f.ConversationID
	}
	if f.MessageID != "" {
		m["message_id"] = f.MessageID
	}
	if f.TaskID != "" {
		m["task_id"] = f.TaskID
	}
	if f.ExecutionID != "" {
		m["execution_id"] = f.ExecutionID
	}
	if f.InputRequestID != "" {
		m["input_request_id"] = f.InputRequestID
	}
	if f.IssueID != "" {
		m["issue_id"] = f.IssueID
	}
	return m
}

type scanFn func(dest ...any) error

func scanEvent(scan scanFn) (*observability.Event, error) {
	var (
		id            string
		occurredAt    string
		seq           int64
		eventType     string
		refsJSON      string
		actor         string
		payloadJSON   string
		correlationID sql.NullString
		decisionID    sql.NullString
		createdAt     string
	)
	if err := scan(&id, &occurredAt, &seq, &eventType, &refsJSON, &actor, &payloadJSON, &correlationID, &decisionID, &createdAt); err != nil {
		return nil, err
	}
	occ, err := time.Parse(time.RFC3339Nano, occurredAt)
	if err != nil {
		return nil, fmt.Errorf("parse occurred_at: %w", err)
	}
	cre, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	refs, err := unmarshalRefs(refsJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal refs: %w", err)
	}
	payload, err := unmarshalPayload(payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	return observability.NewEvent(observability.NewEventInput{
		ID:            observability.EventID(id),
		OccurredAt:    occ,
		Seq:           seq,
		EventType:     observability.EventType(eventType),
		Refs:          refs,
		Actor:         observability.Actor(actor),
		Payload:       payload,
		CorrelationID: correlationID.String,
		DecisionID:    decisionID.String,
		CreatedAt:     cre,
	})
}

func unmarshalRefs(s string) (observability.EventRefs, error) {
	if s == "" || s == "{}" {
		return observability.EventRefs{}, nil
	}
	var r observability.EventRefs
	err := jsonUnmarshal(s, &r)
	return r, err
}

func unmarshalPayload(s string) (map[string]any, error) {
	if s == "" || s == "{}" {
		return map[string]any{}, nil
	}
	var p map[string]any
	err := jsonUnmarshal(s, &p)
	return p, err
}

func isUniqueConstraint(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "constraint failed: UNIQUE"))
}
