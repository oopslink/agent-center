package ledger

import "context"

// Repository is the Bridge BC ACL audit repository.
//
// State machine:
//
//	pending → delivered  (Mark with vendor_msg_ref + card_message_id)
//	pending → failed     (with last_error + retry_count++)
//
// Any other transition returns ErrLedgerInvalidTransition.
type Repository interface {
	Append(ctx context.Context, l *FeishuDeliveryLedger) error
	FindByMessageID(ctx context.Context, messageID string) (*FeishuDeliveryLedger, error)
	FindByID(ctx context.Context, id string) (*FeishuDeliveryLedger, error)
	// MarkDelivered transitions pending → delivered.
	MarkDelivered(ctx context.Context, id string, expectedVersion int, vendorMsgRef, cardMessageID, threadKey string) error
	// MarkFailed transitions pending → failed (retry_count is incremented).
	MarkFailed(ctx context.Context, id string, expectedVersion int, lastError string) error
}
