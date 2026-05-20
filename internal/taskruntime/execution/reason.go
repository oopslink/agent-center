package execution

import (
	"errors"
	"fmt"
	"strings"
)

// CompletedReason is the closed-enum for `task_execution.completed`. v1
// allows the canonical "agent_reported_success" plus empty (no extra
// reason); other strings are rejected.
type CompletedReason string

const (
	CompletedAgentReportedSuccess CompletedReason = "agent_reported_success"
)

// Validate enforces the closed enum (empty is allowed — "no specific
// reason").
func (r CompletedReason) Validate() error {
	switch r {
	case "", CompletedAgentReportedSuccess:
		return nil
	}
	return fmt.Errorf("%w: completed reason %q", ErrUnknownReason, r)
}

// String returns the enum value.
func (r CompletedReason) String() string { return string(r) }

// FailedReason is the closed-enum for `task_execution.failed`. 13 known
// values per 02-task-execution § 7.1 + ADR-0011 / ADR-0018 + 05-agent-
// adapters § 7. The `dispatch_nack:<sub>` family is namespaced.
type FailedReason string

const (
	FailedAgentExitNonzero      FailedReason = "agent_exit_nonzero"
	FailedAgentReported         FailedReason = "agent_reported_failure"
	FailedAgentCrashed          FailedReason = "agent_crashed"
	FailedWorktreeSetup         FailedReason = "worktree_setup_failed"
	FailedSubmittedTimeout      FailedReason = "submitted_timeout"
	FailedExecutionTimeout      FailedReason = "execution_timeout"
	FailedInputTimeout          FailedReason = "input_timeout"
	FailedWorkerLost            FailedReason = "worker_lost"
	FailedDispatchNoAck         FailedReason = "dispatch_no_ack"
	FailedNoInputChannel        FailedReason = "no_input_channel"
	FailedShimNoHello           FailedReason = "shim_no_hello"
	FailedShimCrashed           FailedReason = "shim_crashed"
	FailedJsonlParseError       FailedReason = "jsonl_parse_error"
	FailedAdapterInternalError  FailedReason = "adapter_internal_error"

	// dispatchNackPrefix is the namespace for NACK sub-reasons (ADR-0011).
	dispatchNackPrefix = "dispatch_nack:"
)

// Validate enforces the closed enum + `dispatch_nack:<sub>` namespacing.
func (r FailedReason) Validate() error {
	if r == "" {
		return errors.New("failed_reason: required")
	}
	switch r {
	case FailedAgentExitNonzero, FailedAgentReported, FailedAgentCrashed,
		FailedWorktreeSetup, FailedSubmittedTimeout, FailedExecutionTimeout,
		FailedInputTimeout, FailedWorkerLost, FailedDispatchNoAck,
		FailedNoInputChannel, FailedShimNoHello, FailedShimCrashed,
		FailedJsonlParseError, FailedAdapterInternalError:
		return nil
	}
	if strings.HasPrefix(string(r), dispatchNackPrefix) {
		sub := strings.TrimPrefix(string(r), dispatchNackPrefix)
		if !knownDispatchNackSub(sub) {
			return fmt.Errorf("%w: dispatch_nack sub-reason %q", ErrUnknownReason, sub)
		}
		return nil
	}
	return fmt.Errorf("%w: failed reason %q", ErrUnknownReason, r)
}

// String returns the enum value.
func (r FailedReason) String() string { return string(r) }

// DispatchNack creates a `dispatch_nack:<sub>` failed reason.
func DispatchNack(sub NackSubReason) FailedReason {
	return FailedReason(dispatchNackPrefix + string(sub))
}

// NackSubReason is the closed enum of dispatch NACK sub-reasons (ADR-0011
// § Decision 2).
type NackSubReason string

const (
	NackWorkerAtCapacity         NackSubReason = "worker_at_capacity"
	NackMappingMissing           NackSubReason = "mapping_missing"
	NackAgentCliUnsupported      NackSubReason = "agent_cli_unsupported"
	NackWorktreePathBusy         NackSubReason = "worktree_path_busy"
	NackBaseBranchMissing        NackSubReason = "base_branch_missing"
	NackEnvelopeVersionUnsupported NackSubReason = "envelope_version_unsupported"
)

// Validate enforces the closed enum.
func (s NackSubReason) Validate() error {
	if !knownDispatchNackSub(string(s)) {
		return fmt.Errorf("%w: nack sub-reason %q", ErrUnknownReason, s)
	}
	return nil
}

// String returns the enum value.
func (s NackSubReason) String() string { return string(s) }

func knownDispatchNackSub(s string) bool {
	switch NackSubReason(s) {
	case NackWorkerAtCapacity, NackMappingMissing, NackAgentCliUnsupported,
		NackWorktreePathBusy, NackBaseBranchMissing, NackEnvelopeVersionUnsupported:
		return true
	}
	return false
}

// KilledReason is the closed-enum for `task_execution.killed`. 7 known
// values per 02-task-execution § 7.2.
type KilledReason string

const (
	KilledUserRequest         KilledReason = "user_request"
	KilledSupervisorRequest   KilledReason = "supervisor_request"
	KilledAbandonPrecondition KilledReason = "abandon_precondition"
	KilledSuspendPrecondition KilledReason = "suspend_precondition"
	KilledReconcileStale      KilledReason = "reconcile_stale"
	KilledReconcileUnknown    KilledReason = "reconcile_unknown"
	KilledTimeoutKill         KilledReason = "timeout_kill"
)

// Validate enforces the closed enum.
func (r KilledReason) Validate() error {
	if r == "" {
		return errors.New("killed_reason: required")
	}
	switch r {
	case KilledUserRequest, KilledSupervisorRequest, KilledAbandonPrecondition,
		KilledSuspendPrecondition, KilledReconcileStale, KilledReconcileUnknown,
		KilledTimeoutKill:
		return nil
	}
	return fmt.Errorf("%w: killed reason %q", ErrUnknownReason, r)
}

// String returns the enum value.
func (r KilledReason) String() string { return string(r) }
