package projectmanager

import "encoding/json"

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
