package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/persistence"
)

// SQLiteRepo implements Repository on SQLite.
type SQLiteRepo struct {
	db    *sql.DB
	clock clock.Clock
}

// NewSQLiteRepo constructs the repo. clk defaults to SystemClock when nil.
func NewSQLiteRepo(db *sql.DB, clk clock.Clock) *SQLiteRepo {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &SQLiteRepo{db: db, clock: clk}
}

// Append inserts a fresh pending ledger row.
func (r *SQLiteRepo) Append(ctx context.Context, l *FeishuDeliveryLedger) error {
	if l == nil {
		return errors.New("ledger repo: nil ledger")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO feishu_delivery_ledger (
		id, message_id, conversation_id, channel, thread_key,
		vendor_msg_ref, card_message_id, status, retry_count, last_error,
		delivered_at, updated_at, created_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		l.ID(), l.MessageID(), l.ConversationID(), l.Channel(),
		nullString(l.ThreadKey()),
		nullString(l.VendorMsgRef()), nullString(l.CardMessageID()),
		string(l.Status()), l.RetryCount(), nullString(l.LastError()),
		nullTimePtr(l.DeliveredAt()),
		l.UpdatedAt().Format(time.RFC3339Nano),
		l.CreatedAt().Format(time.RFC3339Nano),
		l.Version(),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrLedgerDuplicate
		}
		return err
	}
	return nil
}

// FindByMessageID looks up by Message.id.
func (r *SQLiteRepo) FindByMessageID(ctx context.Context, messageID string) (*FeishuDeliveryLedger, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, ledgerSelect+` WHERE message_id = ?`, messageID)
	return scanLedgerRow(row)
}

// FindByID looks up by ledger id.
func (r *SQLiteRepo) FindByID(ctx context.Context, id string) (*FeishuDeliveryLedger, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, ledgerSelect+` WHERE id = ?`, id)
	return scanLedgerRow(row)
}

// MarkDelivered CAS-updates pending → delivered.
func (r *SQLiteRepo) MarkDelivered(ctx context.Context, id string, expectedVersion int, vendorMsgRef, cardMessageID, threadKey string) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := r.clock.Now().UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE feishu_delivery_ledger
		SET status = 'delivered',
		    vendor_msg_ref = ?,
		    card_message_id = ?,
		    thread_key = COALESCE(NULLIF(?, ''), thread_key),
		    delivered_at = ?,
		    updated_at = ?,
		    version = version + 1
		WHERE id = ? AND status = 'pending' AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		nullString(vendorMsgRef), nullString(cardMessageID), threadKey,
		now, now, id, expectedVersion,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return r.diagnoseUpdateFailure(ctx, exec, id, expectedVersion)
	}
	return nil
}

// MarkFailed CAS-updates pending → failed and increments retry_count.
func (r *SQLiteRepo) MarkFailed(ctx context.Context, id string, expectedVersion int, lastError string) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := r.clock.Now().UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE feishu_delivery_ledger
		SET status = 'failed',
		    last_error = ?,
		    retry_count = retry_count + 1,
		    updated_at = ?,
		    version = version + 1
		WHERE id = ? AND status = 'pending' AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, nullString(lastError), now, id, expectedVersion)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return r.diagnoseUpdateFailure(ctx, exec, id, expectedVersion)
	}
	return nil
}

// diagnoseUpdateFailure differentiates NotFound / InvalidTransition / VersionConflict.
func (r *SQLiteRepo) diagnoseUpdateFailure(ctx context.Context, exec persistence.SQLExecutor, id string, expectedVersion int) error {
	row := exec.QueryRowContext(ctx,
		`SELECT version, status FROM feishu_delivery_ledger WHERE id = ?`, id)
	var v int
	var status string
	switch err := row.Scan(&v, &status); err {
	case sql.ErrNoRows:
		return ErrLedgerNotFound
	case nil:
		if v != expectedVersion {
			return ErrLedgerVersionConflict
		}
		// Same version, different status: status is no longer pending.
		return ErrLedgerInvalidTransition
	default:
		return err
	}
}

const ledgerSelect = `SELECT id, message_id, conversation_id, channel, thread_key,
	vendor_msg_ref, card_message_id, status, retry_count, last_error,
	delivered_at, updated_at, created_at, version FROM feishu_delivery_ledger`

func scanLedgerRow(row *sql.Row) (*FeishuDeliveryLedger, error) {
	var (
		id, messageID, conversationID, channel string
		threadKey, vendorMsgRef, cardMessageID sql.NullString
		status                                 string
		retryCount                             int
		lastError                              sql.NullString
		deliveredAt                            sql.NullString
		updatedAt, createdAt                   string
		version                                int
	)
	err := row.Scan(&id, &messageID, &conversationID, &channel, &threadKey,
		&vendorMsgRef, &cardMessageID, &status, &retryCount, &lastError,
		&deliveredAt, &updatedAt, &createdAt, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLedgerNotFound
	}
	if err != nil {
		return nil, err
	}
	ut, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	ct, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	var delivered *time.Time
	if deliveredAt.Valid && deliveredAt.String != "" {
		dt, err := time.Parse(time.RFC3339Nano, deliveredAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse delivered_at: %w", err)
		}
		delivered = &dt
	}
	st := DeliveryStatus(status)
	if !st.IsValid() {
		return nil, ErrLedgerInvalidStatus
	}
	return Rehydrate(RehydrateInput{
		ID:             id,
		MessageID:      messageID,
		ConversationID: conversationID,
		Channel:        channel,
		ThreadKey:      threadKey.String,
		VendorMsgRef:   vendorMsgRef.String,
		CardMessageID:  cardMessageID.String,
		Status:         st,
		RetryCount:     retryCount,
		LastError:      lastError.String,
		DeliveredAt:    delivered,
		UpdatedAt:      ut,
		CreatedAt:      ct,
		Version:        version,
	}), nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "constraint failed: UNIQUE")
}
