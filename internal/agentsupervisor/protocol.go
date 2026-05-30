package agentsupervisor

// Reconnectable unix-socket RPC protocol for the persistent agent-supervisor
// (slice D2-f s2). This file defines the wire CONTRACT only: the protocol
// version + compatibility rule, the length-framed transport, and the typed
// request/response messages for the four ops {hello,inject,read,ack}. The
// server (server.go) and client (client.go) both speak this; the daemon-side
// attach/re-attach + version-mismatch ACTION is s3 and is NOT here.
//
// WHY A SOCKET. The supervisor survives a daemon crash/restart (s1). When the
// daemon comes back it must RE-ATTACH to the still-running supervisor: stream
// claude's output events from wherever it left off (a byte offset into
// events.jsonl), inject new input into claude's held-open stdin, and ack
// consumed bytes so events.jsonl does not grow unbounded. The socket is the
// reconnectable channel for exactly that; it lives at <home>/supervisor.sock so
// a returning daemon finds it by path.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the supervisor RPC wire version this build SPEAKS (the
// newest it understands). MinSupportedProtocol is the OLDEST supervisor version
// this build can still talk to.
//
// 🔴 COMPATIBILITY POLICY (v2.7 D2-f 4a) — this is the whole point of process
// SURVIVAL across deploys, so it must NOT be exact-match. Deploy == daemon
// restart, but the supervisor OUTLIVES the daemon, so an upgraded daemon will
// re-attach to a supervisor speaking an OLDER version. If every version drift
// were "incompatible", every deploy would force ALL agents to mode-B relaunch
// and "survive across deploys" would never actually happen. So:
//   - ADDITIVE / backward-compatible change (new optional field, new op the old
//     side ignores): bump ProtocolVersion ONLY (leave MinSupportedProtocol). The
//     new daemon still speaks the old supervisor's version → re-attach SURVIVES.
//   - BREAKING change (reframing, removed/repurposed field, changed semantics):
//     bump MinSupportedProtocol to drop the old versions → those supervisors are
//     incompatible → s3 takes the controlled mode-B degrade.
//
// IsCompatible is therefore a RANGE check, not an equality.
const (
	ProtocolVersion      = 1
	MinSupportedProtocol = 1
)

// IsCompatible reports whether THIS build can talk to a supervisor advertising
// `remote`: compatible iff `remote` is within [MinSupportedProtocol, ProtocolVersion].
// An additive bump of ProtocolVersion keeps older `remote`s compatible (survival
// preserved); only crossing the MinSupportedProtocol breaking boundary makes a
// `remote` incompatible.
//
// This is the (4a) PRIMITIVE that s3 builds its mode-B degrade on: when a
// returning daemon attaches and finds an INCOMPATIBLE supervisor (outside the
// range), s3 must NOT silent-fail — it stops the old supervisor and relaunches a
// fresh one it can speak to. A COMPATIBLE supervisor (additive drift) is
// re-attached and SURVIVES. Here we only provide the clean boolean; the ACTION
// is s3.
func IsCompatible(remote int) bool {
	return compatibleInRange(remote, MinSupportedProtocol, ProtocolVersion)
}

// compatibleInRange is the pure range predicate IsCompatible delegates to; kept
// separate so tests can exercise additive-compatible vs breaking-incompatible
// semantics over a SYNTHETIC range (today the real range is the single point
// [1,1], so range behavior can only be tested via this helper).
func compatibleInRange(remote, min, max int) bool { return remote >= min && remote <= max }

// maxFrameSize caps a single length-framed message payload (anti-abuse: a
// bogus/hostile length prefix must not make us allocate gigabytes). 16 MiB is
// far above any legitimate read chunk or inject line.
const maxFrameSize = 16 << 20 // 16 MiB

// ErrFrameTooLarge is returned by readFrame when the length prefix exceeds
// maxFrameSize, and by writeFrame when asked to write an oversize payload.
var ErrFrameTooLarge = errors.New("agentsupervisor: frame exceeds max size")

// writeFrame writes b as a single length-framed message: a 4-byte big-endian
// length prefix followed by the payload. One frame == one JSON message.
func writeFrame(w io.Writer, b []byte) error {
	if len(b) > maxFrameSize {
		return ErrFrameTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	return nil
}

// readFrame reads one length-framed message: a 4-byte big-endian length then
// exactly that many payload bytes. A length above maxFrameSize is rejected
// (ErrFrameTooLarge) WITHOUT reading the body, so an oversize/garbage prefix
// cannot drive an unbounded allocation. io.EOF at a frame boundary is returned
// verbatim so callers can detect a clean connection close.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrameSize {
		return nil, ErrFrameTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("agentsupervisor: read frame body: %w", err)
	}
	return buf, nil
}

// --- Message types (one JSON object per frame) ---------------------------
//
// A request is {op, ...op-specific fields}; a response is {ok, error,
// ...op-specific fields}. ok=false carries a non-empty error string and the
// op-specific fields are zero. The server NEVER crashes on a bad request — it
// replies ok=false.

// Op enumerates the request operations.
const (
	OpHello  = "hello"
	OpInject = "inject"
	OpRead   = "read"
	OpAck    = "ack"
)

// Error codes carried in Response.Error for machine-distinguishable failures
// the daemon must branch on. Other errors are free-form strings.
const (
	// ErrCodeOffsetTruncated is returned by read when the requested absolute
	// offset is below baseOffset (already acked + truncated). On re-attach the
	// daemon must read from its last-acked offset, which == baseOffset.
	ErrCodeOffsetTruncated = "offset_truncated"
	// ErrCodeUnknownOp is returned for an unrecognized op.
	ErrCodeUnknownOp = "unknown_op"
)

// Request is the union of all op requests; only the fields relevant to Op are
// populated. Kept as one struct (not an interface) so the server decodes once
// and switches on Op — minimal + explicit.
type Request struct {
	Op string `json:"op"`

	// inject
	Message string `json:"message,omitempty"`

	// read
	Offset   int64 `json:"offset,omitempty"`
	MaxBytes int   `json:"max_bytes,omitempty"`

	// ack
	AckOffset int64 `json:"ack_offset,omitempty"`
}

// Response is the union of all op responses. Ok=false ⇒ Error set, all
// op-specific fields zero.
type Response struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	// hello
	ProtocolVersion int    `json:"protocol_version,omitempty"`
	InstanceID      string `json:"instance_id,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	ChildPID        int    `json:"child_pid,omitempty"`
	StartedAt       string `json:"started_at,omitempty"` // RFC3339Nano
	BaseOffset      int64  `json:"base_offset,omitempty"`
	CurrentOffset   int64  `json:"current_offset,omitempty"`

	// read
	Data       []byte `json:"data,omitempty"` // base64 in JSON (Go marshals []byte so)
	NextOffset int64  `json:"next_offset,omitempty"`
	EOF        bool   `json:"eof,omitempty"`

	// ack (also reuses BaseOffset above)
}

// HelloResp is the decoded, typed view the client returns to its caller (s3).
type HelloResp struct {
	ProtocolVersion int
	InstanceID      string
	AgentID         string
	ChildPID        int
	StartedAt       string
	BaseOffset      int64
	CurrentOffset   int64
}
