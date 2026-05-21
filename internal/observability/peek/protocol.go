// Package peek implements the peek-trace RPC channel: center process (CLI
// caller) ↔ worker daemon ↔ per-execution events.jsonl. See plan-4 § 3.7
// + ADR-0015 § 4.
//
// v1 transport: line-delimited JSON over a unix socket served by the
// worker daemon. Single-machine deployment (Phase 5+ extends to TCP/TLS
// when multi-machine is on the table).
//
// Wire format:
//   - Client → Server: 1 JSON request line `{"execution_id":"E-1","last":10,"kind":"tool_call","follow":true}\n`
//   - Server → Client: N JSON response lines, each is either:
//       * `{"line":"<raw events.jsonl row>"}` (a streamed trace event)
//       * `{"error":{"reason":"worker_offline","message":"..."}}`
//     terminated by `{"done":true}\n` or by the connection closing.
package peek

import "errors"

// Request is the JSON body of one peek-trace call.
type Request struct {
	ExecutionID string `json:"execution_id"`
	// Last is the tail-N count (0 = no tail, send all).
	Last int `json:"last,omitempty"`
	// Kind filters by AgentTraceEvent type — `all` or empty means no filter.
	Kind string `json:"kind,omitempty"`
	// Follow keeps streaming new lines as the shim appends them.
	Follow bool `json:"follow,omitempty"`
}

// Response is the JSON body of one server frame.
type Response struct {
	// Line is one raw events.jsonl line (without trailing newline).
	Line string `json:"line,omitempty"`
	// Error is set when the server cannot satisfy the request.
	Error *ErrorPayload `json:"error,omitempty"`
	// Done flags the final frame (closed-cleanly).
	Done bool `json:"done,omitempty"`
}

// ErrorPayload carries the closed-enum failure reason + human message
// (conventions § 16 + plan-4 § 3.7).
type ErrorPayload struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// Closed-enum failure reasons per plan-4 § 3.7.
const (
	ReasonExecutionNotFound = "execution_not_found"
	ReasonWorkerOffline     = "worker_offline"
	ReasonWorkerNotOwner    = "worker_not_owner"
	ReasonTraceFileMissing  = "trace_file_missing"
	ReasonStreamCanceled    = "stream_canceled"
	ReasonInvalidRequest    = "invalid_request"
)

// ErrInvalidRequest is the typed error wrapping ReasonInvalidRequest.
var ErrInvalidRequest = errors.New("peek-trace: invalid request")
