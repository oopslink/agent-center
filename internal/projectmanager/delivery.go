package projectmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Delivery is the last forked executor's terminal git status — the center-side mirror
// of agentruntime executor.FinalizedGitStatus (the verbatim 8 fields). It answers the
// one question the delivery-flow fixes (issue-f30b7e7b) need: did this executor
// produce a DURABLE, pushed delivery, or a zero-delivery run (committed-but-not-pushed
// / dirty / no-commit) that must be auto-blocked rather than re-nudged/re-dispatched?
//
// It is populated by the report_delivery agent-tool from the worker's CenterWriteback
// (the worker probes git at finalize and reports it). A nil *Delivery means the
// executor never reported one (e.g. it never forked — the fork-fail loop — or the
// best-effort send failed): treated as "no valid delivery", the safe side.
type Delivery struct {
	Probed      bool   `json:"probed"`
	Pushed      bool   `json:"pushed"`
	Branch      string `json:"branch,omitempty"`
	HeadSHA     string `json:"head_sha,omitempty"`
	Dirty       bool   `json:"dirty"`
	BaseRef     string `json:"base_ref,omitempty"`
	BaseKnown   bool   `json:"base_known"`
	AheadOfBase int    `json:"ahead_of_base"`
	// PushError is set when the eager supervisor-push (issue-f30b7e7b) could not push the
	// committed feat branch to origin (guardrail refusal / auth / non-ff / network). "" =
	// no push failure. It is the DURABLE record of WHY a delivery was not durably pushed —
	// so audit / DB queries can see the cause, not just the live task conversation + logs.
	PushError string `json:"push_error,omitempty"`
	// Source names who produced this delivery signal. "" and "executor" are the normal
	// runtime path; "manual_recovery" is a worker/runtime-side operator command that
	// reuses report_delivery after a human has recovered, tested and pushed the work.
	Source string `json:"source,omitempty"`
	// ExecutorID is the runtime executor that produced the signal when known.
	ExecutorID string `json:"executor_id,omitempty"`
	// Worktree is the retained/recovered worktree path used to register a manual recovery.
	// It is operator evidence only; completion still keys off the git fields above.
	Worktree string `json:"worktree,omitempty"`
	// Evidence is a short human-authored test/evidence summary for manual recovery delivery.
	Evidence string `json:"evidence,omitempty"`
	// Reason explains why the delivery was reported manually (dead/exhausted/abandoned
	// executor, operator takeover, etc.).
	Reason string `json:"reason,omitempty"`
}

// HasValidDelivery reports whether the executor produced a durable, pushed delivery —
// the ONLY positive forward-progress signal. Everything else is "no valid delivery":
//   - nil            — never reported (fork-fail / send lost)
//   - !Probed        — non-git / plain-dir / probe-failed → UNKNOWN, not counted as a
//     delivery (never falsely blocks a non-git task), but also not a
//     positive signal
//   - Probed&&!Pushed — committed-but-not-pushed / dirty / no-commit → the teardown-bug
//     zero-delivery signature that mislabels as success today
func (d *Delivery) HasValidDelivery() bool {
	return d != nil &&
		d.Probed &&
		d.Pushed &&
		!d.Dirty &&
		d.BaseKnown &&
		d.AheadOfBase > 0 &&
		d.Branch != "" &&
		d.HeadSHA != ""
}

// DeliveryReason is one machine-readable reason a delivery is not acceptable for
// complete_task. Keep codes stable; they are returned to agents on task_non_delivery.
type DeliveryReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// InvalidReasons returns every missing/invalid condition that prevents this delivery
// from counting as a durable pushed delivery. It is the diagnostic twin of
// HasValidDelivery; do not loosen completion by editing this without editing the gate.
func (d *Delivery) InvalidReasons() []DeliveryReason {
	if d == nil {
		return []DeliveryReason{{Code: "delivery_missing", Message: "no delivery has been reported for this task"}}
	}
	reasons := []DeliveryReason{}
	if !d.Probed {
		reasons = append(reasons, DeliveryReason{Code: "git_not_probed", Message: "executor/worktree git state was not probed"})
	}
	if !d.Pushed {
		msg := "delivery HEAD is not verified on a remote ref"
		if strings.TrimSpace(d.PushError) != "" {
			msg += ": " + d.PushError
		}
		reasons = append(reasons, DeliveryReason{Code: "head_not_pushed", Message: msg})
	}
	if d.Dirty {
		reasons = append(reasons, DeliveryReason{Code: "worktree_dirty", Message: "worktree still has uncommitted changes"})
	}
	if !d.BaseKnown {
		reasons = append(reasons, DeliveryReason{Code: "base_unknown", Message: "spawn/recovery base ref is unknown or unresolved"})
	}
	if d.AheadOfBase <= 0 {
		reasons = append(reasons, DeliveryReason{Code: "no_commit_ahead_of_base", Message: "HEAD is not ahead of the recorded base ref"})
	}
	if strings.TrimSpace(d.Branch) == "" {
		reasons = append(reasons, DeliveryReason{Code: "branch_missing", Message: "delivery branch is missing"})
	}
	if strings.TrimSpace(d.HeadSHA) == "" {
		reasons = append(reasons, DeliveryReason{Code: "head_sha_missing", Message: "delivery HEAD SHA is missing"})
	}
	return reasons
}

// TaskNoValidDeliveryError carries the same sentinel semantics as
// ErrTaskNoValidDelivery while exposing exact missing delivery conditions.
type TaskNoValidDeliveryError struct {
	Delivery *Delivery
	Reasons  []DeliveryReason
}

func (e *TaskNoValidDeliveryError) Error() string {
	reasons := e.Reasons
	if len(reasons) == 0 {
		reasons = e.Delivery.InvalidReasons()
	}
	parts := make([]string, 0, len(reasons))
	for _, r := range reasons {
		if r.Message != "" {
			parts = append(parts, r.Message)
		} else {
			parts = append(parts, r.Code)
		}
	}
	return fmt.Sprintf("%s: %s; block/retry it or register manual_recovery delivery with pushed SHA + test evidence",
		ErrTaskNoValidDelivery.Error(), strings.Join(parts, "; "))
}

func (e *TaskNoValidDeliveryError) Is(target error) bool {
	return errors.Is(target, ErrTaskNoValidDelivery)
}

// NewTaskNoValidDeliveryError wraps a delivery diagnostic in the sentinel-compatible
// error complete_task surfaces as task_non_delivery.
func NewTaskNoValidDeliveryError(d *Delivery) error {
	return &TaskNoValidDeliveryError{Delivery: d, Reasons: d.InvalidReasons()}
}

// MarshalDelivery renders a *Delivery to its stored JSON (” for nil). Kept beside the
// type so the repo and the report_delivery tool serialize identically.
func MarshalDelivery(d *Delivery) (string, error) {
	if d == nil {
		return "", nil
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalDelivery parses stored JSON back to a *Delivery (” → nil). A malformed
// value is a data error the caller surfaces (it never silently drops the signal).
func UnmarshalDelivery(s string) (*Delivery, error) {
	if s == "" {
		return nil, nil
	}
	var d Delivery
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		return nil, err
	}
	return &d, nil
}
