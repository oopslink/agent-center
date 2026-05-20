package dispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// DispatchAck is Worker → Center ACK after accepting a DispatchEnvelope
// (ADR-0011 § 2).
type DispatchAck struct {
	ExecutionID taskruntime.TaskExecutionID `json:"execution_id"`
	Accepted    bool                        `json:"accepted"`
	Message     string                      `json:"message,omitempty"`
	AckedAt     time.Time                   `json:"acked_at"`
}

// Validate checks invariants.
func (a DispatchAck) Validate() error {
	if strings.TrimSpace(string(a.ExecutionID)) == "" {
		return errors.New("dispatch ack: execution_id required")
	}
	if !a.Accepted {
		return errors.New("dispatch ack: accepted must be true (use DispatchNack)")
	}
	if a.AckedAt.IsZero() {
		return errors.New("dispatch ack: acked_at required")
	}
	return nil
}

// MarshalJSON marshals the ACK.
func (a DispatchAck) MarshalJSON() ([]byte, error) {
	type alias DispatchAck
	return json.Marshal(alias(a))
}

// DispatchNack is Worker → Center NACK with reason + message (ADR-0011 §
// 2, 6 sub_reason values).
type DispatchNack struct {
	ExecutionID taskruntime.TaskExecutionID `json:"execution_id"`
	Accepted    bool                        `json:"accepted"`
	Reason      execution.NackSubReason     `json:"reason"`
	Message     string                      `json:"message"`
	AckedAt     time.Time                   `json:"acked_at"`
}

// Validate enforces reason enum + message non-empty.
func (n DispatchNack) Validate() error {
	if strings.TrimSpace(string(n.ExecutionID)) == "" {
		return errors.New("dispatch nack: execution_id required")
	}
	if n.Accepted {
		return errors.New("dispatch nack: accepted must be false")
	}
	if err := n.Reason.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(n.Message) == "" {
		return errors.New("dispatch nack: message required (conventions § 16)")
	}
	if n.AckedAt.IsZero() {
		return errors.New("dispatch nack: acked_at required")
	}
	return nil
}

// MarshalJSON marshals the NACK.
func (n DispatchNack) MarshalJSON() ([]byte, error) {
	type alias DispatchNack
	return json.Marshal(alias(n))
}

// FailedReason returns the FailedReason representation of this NACK
// (`dispatch_nack:<sub>`) suitable for execution.MarkFailed.
func (n DispatchNack) FailedReason() execution.FailedReason {
	return execution.DispatchNack(n.Reason)
}

// ParseAck parses a JSON ACK message.
func ParseAck(data []byte) (DispatchAck, error) {
	var a DispatchAck
	if err := json.Unmarshal(data, &a); err != nil {
		return DispatchAck{}, fmt.Errorf("dispatch ack: %w", err)
	}
	return a, nil
}

// ParseNack parses a JSON NACK message.
func ParseNack(data []byte) (DispatchNack, error) {
	var n DispatchNack
	if err := json.Unmarshal(data, &n); err != nil {
		return DispatchNack{}, fmt.Errorf("dispatch nack: %w", err)
	}
	return n, nil
}
