// Package cognition hosts the Cognition BC (Supervisor + Memory) tactical
// types. Per plan-6 § 1 and cognition/00-overview / 01-supervisor-invocation
// / 02-memory.
//
// Customer role in the Context Map: writes through CLI handlers, reads via
// EventRepository (no inbound event subscription — see ADR-0013).
package cognition

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
)

// InvocationID is the ULID identifying a single SupervisorInvocation.
type InvocationID string

// String returns the underlying string.
func (id InvocationID) String() string { return string(id) }

// DecisionID is the ULID identifying a single DecisionRecord row.
type DecisionID string

// String returns the underlying string.
func (id DecisionID) String() string { return string(id) }

// ScopeKind enumerates the 5 scope_kind values that may appear on an
// Invocation row (cognition/01-supervisor-invocation § 3.2). Memory adds 2
// more (project / supervisor) — see MemoryScopeKind.
type ScopeKind string

// Closed enum (Invocation scope_kind).
const (
	ScopeTask         ScopeKind = "task"
	ScopeIssue        ScopeKind = "issue"
	ScopeConversation ScopeKind = "conversation"
	ScopeWorker       ScopeKind = "worker"
	ScopeGlobal       ScopeKind = "global"
)

// GlobalScopeKey is the canonical scope_key for ScopeGlobal — invariant.
const GlobalScopeKey = "_global_"

// IsValid reports whether sk is one of the 5 Invocation scope_kinds.
func (sk ScopeKind) IsValid() bool {
	switch sk {
	case ScopeTask, ScopeIssue, ScopeConversation, ScopeWorker, ScopeGlobal:
		return true
	}
	return false
}

// String returns the underlying name.
func (sk ScopeKind) String() string { return string(sk) }

// ParseScopeKind parses the textual representation; returns
// ErrUnknownScopeKind on unknown input (no fallback).
func ParseScopeKind(s string) (ScopeKind, error) {
	sk := ScopeKind(s)
	if !sk.IsValid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownScopeKind, s)
	}
	return sk, nil
}

// ErrUnknownScopeKind is returned by ParseScopeKind.
var ErrUnknownScopeKind = errors.New("cognition: unknown scope_kind")

// InvocationScope is the immutable VO pairing scope_kind + scope_key.
//
// Construct via NewInvocationScope — direct field assignment is impossible
// (fields unexported).
type InvocationScope struct {
	kind ScopeKind
	key  string
}

// NewInvocationScope constructs a scope. For ScopeGlobal the key is forced
// to GlobalScopeKey ("_global_"); any other key is rejected.
func NewInvocationScope(kind ScopeKind, key string) (InvocationScope, error) {
	if !kind.IsValid() {
		return InvocationScope{}, fmt.Errorf("%w: %q", ErrUnknownScopeKind, kind)
	}
	if kind == ScopeGlobal {
		if key != "" && key != GlobalScopeKey {
			return InvocationScope{}, fmt.Errorf("cognition: scope_kind=global requires key=%q, got %q", GlobalScopeKey, key)
		}
		return InvocationScope{kind: ScopeGlobal, key: GlobalScopeKey}, nil
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return InvocationScope{}, fmt.Errorf("cognition: scope_kind=%s requires non-empty key", kind)
	}
	// Path-traversal guard at construction time so all downstream consumers
	// can trust the scope (cognition/02-memory § 3).
	if strings.ContainsAny(k, "/\\:") || strings.Contains(k, "..") || strings.Contains(k, "\x00") {
		return InvocationScope{}, fmt.Errorf("cognition: scope key %q contains forbidden characters", k)
	}
	return InvocationScope{kind: kind, key: k}, nil
}

// MustNewInvocationScope is a test convenience; panics on error.
func MustNewInvocationScope(kind ScopeKind, key string) InvocationScope {
	s, err := NewInvocationScope(kind, key)
	if err != nil {
		panic(err)
	}
	return s
}

// Kind returns the scope kind.
func (s InvocationScope) Kind() ScopeKind { return s.kind }

// Key returns the scope key.
func (s InvocationScope) Key() string { return s.key }

// IsZero reports whether s is the zero value (no kind set).
func (s InvocationScope) IsZero() bool { return s.kind == "" }

// String returns "kind:key" (or "global" for ScopeGlobal). Useful for
// logging and map keys.
func (s InvocationScope) String() string {
	if s.kind == ScopeGlobal {
		return string(ScopeGlobal)
	}
	return string(s.kind) + ":" + s.key
}

// Equal reports value equality.
func (s InvocationScope) Equal(o InvocationScope) bool {
	return s.kind == o.kind && s.key == o.key
}

// MarshalJSON serialises to {"kind":"...","key":"..."}.
func (s InvocationScope) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"kind": string(s.kind), "key": s.key})
}

// UnmarshalJSON parses {"kind":"...","key":"..."} via NewInvocationScope.
func (s *InvocationScope) UnmarshalJSON(b []byte) error {
	var raw struct {
		Kind string `json:"kind"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	v, err := NewInvocationScope(ScopeKind(raw.Kind), raw.Key)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// TriggerEventSet is the immutable, deduplicated, sorted set of event IDs
// that woke an Invocation. ≥ 1 (cognition/01 § 5).
type TriggerEventSet struct {
	ids []observability.EventID
}

// NewTriggerEventSet constructs a set; ids must be non-empty. Duplicates
// are dropped (set semantics) and ordering canonicalised by id ASC.
func NewTriggerEventSet(ids []observability.EventID) (TriggerEventSet, error) {
	if len(ids) == 0 {
		return TriggerEventSet{}, errors.New("cognition: TriggerEventSet requires ≥ 1 event_id")
	}
	seen := map[observability.EventID]struct{}{}
	out := make([]observability.EventID, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			return TriggerEventSet{}, errors.New("cognition: TriggerEventSet contains empty event_id")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return TriggerEventSet{ids: out}, nil
}

// IDs returns a copy of the underlying id slice (sorted ASC).
func (s TriggerEventSet) IDs() []observability.EventID {
	out := make([]observability.EventID, len(s.ids))
	copy(out, s.ids)
	return out
}

// Len returns the count.
func (s TriggerEventSet) Len() int { return len(s.ids) }

// Contains reports whether id is present.
func (s TriggerEventSet) Contains(id observability.EventID) bool {
	for _, x := range s.ids {
		if x == id {
			return true
		}
	}
	return false
}

// MarshalJSON serialises to ["id1","id2",...].
func (s TriggerEventSet) MarshalJSON() ([]byte, error) {
	if s.ids == nil {
		return []byte("[]"), nil
	}
	out := make([]string, len(s.ids))
	for i, id := range s.ids {
		out[i] = string(id)
	}
	return json.Marshal(out)
}

// UnmarshalJSON parses an array; routes through NewTriggerEventSet for
// validation.
func (s *TriggerEventSet) UnmarshalJSON(b []byte) error {
	var raw []string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	ids := make([]observability.EventID, len(raw))
	for i, r := range raw {
		ids[i] = observability.EventID(r)
	}
	v, err := NewTriggerEventSet(ids)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// InvocationStatus is the 4-state enum (cognition/01 § 2).
type InvocationStatus string

// Closed enum.
const (
	StatusRunning   InvocationStatus = "running"
	StatusSucceeded InvocationStatus = "succeeded"
	StatusFailed    InvocationStatus = "failed"
	StatusTimedOut  InvocationStatus = "timed_out"
)

// IsValid reports whether s is one of the 4 statuses.
func (s InvocationStatus) IsValid() bool {
	switch s {
	case StatusRunning, StatusSucceeded, StatusFailed, StatusTimedOut:
		return true
	}
	return false
}

// IsTerminal reports whether s is a terminal status.
func (s InvocationStatus) IsTerminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusTimedOut
}

// String returns the underlying name.
func (s InvocationStatus) String() string { return string(s) }

// ParseInvocationStatus is the typed parse (returns ErrUnknownStatus).
func ParseInvocationStatus(s string) (InvocationStatus, error) {
	v := InvocationStatus(s)
	if !v.IsValid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownStatus, s)
	}
	return v, nil
}

// ErrUnknownStatus is returned by ParseInvocationStatus.
var ErrUnknownStatus = errors.New("cognition: unknown invocation status")

// InvocationFailedReason is the closed enum that pairs with FailedMessage
// (conventions § 16).
type InvocationFailedReason string

// Closed enum (plan-6 § 1.3).
const (
	FailedReasonClaudeNonZero        InvocationFailedReason = "claude_nonzero"
	FailedReasonCLICommandError      InvocationFailedReason = "cli_command_error"
	FailedReasonOOM                  InvocationFailedReason = "oom"
	FailedReasonCenterRestartOrphan  InvocationFailedReason = "center_restart_orphan"
	FailedReasonKilledByAdmin        InvocationFailedReason = "killed_by_admin"
	FailedReasonUnknown              InvocationFailedReason = "unknown"
)

// IsValid reports membership in the closed enum.
func (r InvocationFailedReason) IsValid() bool {
	switch r {
	case FailedReasonClaudeNonZero, FailedReasonCLICommandError,
		FailedReasonOOM, FailedReasonCenterRestartOrphan,
		FailedReasonKilledByAdmin, FailedReasonUnknown:
		return true
	}
	return false
}

// String returns the underlying name.
func (r InvocationFailedReason) String() string { return string(r) }

// TokenUsage is the per-Invocation accumulation reported by the supervisor
// subprocess (cognition/01 § 3).
type TokenUsage struct {
	Input       int `json:"input"`
	Output      int `json:"output"`
	CacheRead   int `json:"cache_read"`
	CacheCreate int `json:"cache_create"`
}

// Add returns the pointwise sum.
func (t TokenUsage) Add(o TokenUsage) TokenUsage {
	return TokenUsage{
		Input:       t.Input + o.Input,
		Output:      t.Output + o.Output,
		CacheRead:   t.CacheRead + o.CacheRead,
		CacheCreate: t.CacheCreate + o.CacheCreate,
	}
}

// IsZero reports whether the receiver is the zero value.
func (t TokenUsage) IsZero() bool { return t == TokenUsage{} }

// HardTimeout enumerates the per-scope timeout durations (plan-6 § 1.3).
//
// task/issue/conversation/worker = 180s; global = 600s.
type HardTimeout time.Duration

// Duration returns the wrapped time.Duration.
func (h HardTimeout) Duration() time.Duration { return time.Duration(h) }

// Seconds returns the timeout in seconds (integer).
func (h HardTimeout) Seconds() int { return int(time.Duration(h).Seconds()) }

// HardTimeoutFor returns the timeout for a given scope_kind.
func HardTimeoutFor(kind ScopeKind) HardTimeout {
	if kind == ScopeGlobal {
		return HardTimeout(600 * time.Second)
	}
	return HardTimeout(180 * time.Second)
}

// DecisionKind is the 12-value closed enum (cognition/01 § 4.4).
type DecisionKind string

// Closed enum.
const (
	DecisionDispatch              DecisionKind = "dispatch"
	DecisionKillExecution         DecisionKind = "kill_execution"
	DecisionAbandonTask           DecisionKind = "abandon_task"
	DecisionSuspendTask           DecisionKind = "suspend_task"
	DecisionResumeTask            DecisionKind = "resume_task"
	DecisionOpenIssue             DecisionKind = "open_issue"
	DecisionIssueComment          DecisionKind = "issue_comment"
	DecisionConcludeIssue         DecisionKind = "conclude_issue"
	DecisionCloseIssue            DecisionKind = "close_issue"
	DecisionConversationMessage   DecisionKind = "conversation_message"
	DecisionEscalateInputRequest  DecisionKind = "escalate_input_request"
	DecisionNoOp                  DecisionKind = "no_op"
)

// IsValid reports membership in the closed enum.
func (k DecisionKind) IsValid() bool {
	switch k {
	case DecisionDispatch, DecisionKillExecution, DecisionAbandonTask,
		DecisionSuspendTask, DecisionResumeTask, DecisionOpenIssue,
		DecisionIssueComment, DecisionConcludeIssue, DecisionCloseIssue,
		DecisionConversationMessage, DecisionEscalateInputRequest,
		DecisionNoOp:
		return true
	}
	return false
}

// String returns the underlying name.
func (k DecisionKind) String() string { return string(k) }

// ParseDecisionKind parses the textual representation; returns
// ErrUnknownDecisionKind on unknown input (no silent fallback,
// conventions § 17).
func ParseDecisionKind(s string) (DecisionKind, error) {
	k := DecisionKind(s)
	if !k.IsValid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownDecisionKind, s)
	}
	return k, nil
}

// ErrUnknownDecisionKind is returned by ParseDecisionKind.
var ErrUnknownDecisionKind = errors.New("cognition: unknown decision_kind")

// AllDecisionKinds returns the full 12-element closed set in declaration
// order (test + skill-doc parity).
func AllDecisionKinds() []DecisionKind {
	return []DecisionKind{
		DecisionDispatch, DecisionKillExecution, DecisionAbandonTask,
		DecisionSuspendTask, DecisionResumeTask, DecisionOpenIssue,
		DecisionIssueComment, DecisionConcludeIssue, DecisionCloseIssue,
		DecisionConversationMessage, DecisionEscalateInputRequest,
		DecisionNoOp,
	}
}

// DecisionOutcome is the binary outcome for a DecisionRecord row
// (cognition/01 § 4.6).
type DecisionOutcome string

// Closed enum.
const (
	OutcomeSucceeded DecisionOutcome = "succeeded"
	OutcomeFailed    DecisionOutcome = "failed"
)

// IsValid reports membership.
func (o DecisionOutcome) IsValid() bool {
	return o == OutcomeSucceeded || o == OutcomeFailed
}

// String returns the underlying name.
func (o DecisionOutcome) String() string { return string(o) }
