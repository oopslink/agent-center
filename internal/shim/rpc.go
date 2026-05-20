package shim

import (
	"encoding/json"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// ProtocolVersion identifies the shim ↔ daemon RPC schema version.
const ProtocolVersion = 1

// MessageType is the discriminator on RPC payloads.
type MessageType string

const (
	MsgShimHello     MessageType = "shim_hello"
	MsgShimGoodbye   MessageType = "shim_goodbye"
	MsgPhaseChanged  MessageType = "phase_changed"
	MsgEvent         MessageType = "event"
	MsgRPCForward    MessageType = "rpc_forward"
	MsgKill          MessageType = "kill"
	MsgGoodbyeAck    MessageType = "goodbye_ack"
	MsgReconcile     MessageType = "reconcile"
)

// ShimHello is the first message a shim sends after spawn (ADR-0018 § 4).
type ShimHello struct {
	ProtocolVersion int       `json:"protocol_version"`
	ExecutionID     string    `json:"execution_id"`
	ShimToken       string    `json:"shim_token"`
	ShimPID         int       `json:"shim_pid"`
	ShimStartTime   time.Time `json:"shim_start_time"`
	AgentPID        int       `json:"agent_pid"`
	AgentStartTime  time.Time `json:"agent_start_time"`
	LastAckedSeq    int64     `json:"last_acked_seq,omitempty"`
}

// PhaseChanged signals a phase transition (starting/running/done).
type PhaseChanged struct {
	ExecutionID string `json:"execution_id"`
	Phase       Phase  `json:"phase"`
	ExitCode    int    `json:"exit_code,omitempty"`
}

// EventMsg wraps one AgentTraceEvent or BC-level event the shim pushes to
// daemon.
type EventMsg struct {
	ExecutionID string          `json:"execution_id"`
	Seq         int64           `json:"seq"`
	Payload     json.RawMessage `json:"payload"`
}

// RPCForward is the body of a daemon-side RPC that the shim got from the
// agent (request-input / report-progress / etc.).
type RPCForward struct {
	ExecutionID string          `json:"execution_id"`
	Method      string          `json:"method"`
	Body        json.RawMessage `json:"body"`
}

// KillMsg is the daemon → shim kill RPC.
type KillMsg struct {
	ExecutionID string                  `json:"execution_id"`
	Reason      execution.KilledReason  `json:"reason"`
	Message     string                  `json:"message"`
}

// ShimGoodbye signals agent exit; daemon must ACK.
type ShimGoodbye struct {
	ExecutionID string `json:"execution_id"`
	ExitCode    int    `json:"exit_code"`
	Reason      string `json:"reason,omitempty"`
	Message     string `json:"message,omitempty"`
}

// GoodbyeAck is daemon → shim acknowledgement that close-up persisted.
type GoodbyeAck struct {
	ExecutionID string `json:"execution_id"`
}

// Envelope is a generic typed-tagged envelope for line-based RPC over the
// shim ↔ daemon socket.
type Envelope struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// EncodeEnvelope marshals an Envelope with the given type and payload.
func EncodeEnvelope(t MessageType, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: t, Payload: body})
}

// DecodeEnvelope unmarshals.
func DecodeEnvelope(data []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return Envelope{}, err
	}
	return e, nil
}
