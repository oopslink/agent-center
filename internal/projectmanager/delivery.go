package projectmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Delivery is the last forked executor's terminal git status — the center-side mirror
// of agentruntime executor.FinalizedGitStatus. It answers the
// one question the delivery-flow fixes (issue-f30b7e7b) need: did this executor
// produce a DURABLE, pushed delivery, or a zero-delivery run (committed-but-not-pushed
// / dirty / no-commit) that must be auto-blocked rather than re-nudged/re-dispatched?
//
// It is populated by the report_delivery agent-tool from the worker's CenterWriteback
// (the worker probes git at finalize and reports it). A nil *Delivery means the
// executor never reported one (e.g. it never forked — the fork-fail loop — or the
// best-effort send failed): treated as "no valid delivery", the safe side.
type Delivery struct {
	Probed      bool     `json:"probed"`
	Pushed      bool     `json:"pushed"`
	Branch      string   `json:"branch,omitempty"`
	HeadSHA     string   `json:"head_sha,omitempty"`
	Dirty       bool     `json:"dirty"`
	DirtyPaths  []string `json:"dirty_paths,omitempty"`
	BaseRef     string   `json:"base_ref,omitempty"`
	BaseKnown   bool     `json:"base_known"`
	AheadOfBase int      `json:"ahead_of_base"`
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

type DeliverySubjectType string

const (
	DeliverySubjectCommit DeliverySubjectType = "commit"
)

type AcceptanceVerdict string

const (
	AcceptancePassed            AcceptanceVerdict = "passed"
	AcceptanceRejected          AcceptanceVerdict = "rejected"
	AcceptanceWaivedByAuthority AcceptanceVerdict = "waived_by_authority"
)

type DeliverySubject struct {
	ID                     string              `json:"id"`
	SubjectType            DeliverySubjectType `json:"subject_type"`
	PlanID                 PlanID              `json:"plan_id"`
	TaskID                 TaskID              `json:"task_id"`
	NodeID                 string              `json:"node_id,omitempty"`
	ExecutionID            string              `json:"execution_id,omitempty"`
	RepoID                 string              `json:"repo_id,omitempty"`
	Remote                 string              `json:"remote"`
	Branch                 string              `json:"branch"`
	BaseSHA                string              `json:"base_sha"`
	CandidateSHA           string              `json:"candidate_sha"`
	CandidateRef           string              `json:"candidate_ref"`
	PushedRemote           string              `json:"pushed_remote"`
	DeliveryContractHash   string              `json:"delivery_contract_hash"`
	AcceptanceContractHash string              `json:"acceptance_contract_hash"`
	CreatedAt              time.Time           `json:"created_at"`
}

type Acceptance struct {
	ID              string            `json:"id"`
	SubjectID       string            `json:"subject_id"`
	SubjectDigest   string            `json:"subject_digest"`
	PlanID          PlanID            `json:"plan_id"`
	TaskID          TaskID            `json:"task_id"`
	GateTaskID      TaskID            `json:"gate_task_id,omitempty"`
	ContractHash    string            `json:"contract_hash"`
	Verdict         AcceptanceVerdict `json:"verdict"`
	ActorRef        IdentityRef       `json:"actor_ref"`
	AuthorityRank   int               `json:"authority_rank"`
	AuthoritySource string            `json:"authority_source"`
	EvidenceRef     string            `json:"evidence_ref"`
	EvidenceSHA     string            `json:"evidence_sha"`
	FindingsJSON    string            `json:"findings_json"`
	CreatedAt       time.Time         `json:"created_at"`
}

func NewDeliverySubject(s DeliverySubject) (DeliverySubject, error) {
	if strings.TrimSpace(s.ID) == "" || s.PlanID == "" || s.TaskID == "" {
		return DeliverySubject{}, errors.New("projectmanager: delivery subject scope required")
	}
	if s.SubjectType == "" {
		s.SubjectType = DeliverySubjectCommit
	}
	if s.SubjectType != DeliverySubjectCommit {
		return DeliverySubject{}, errors.New("projectmanager: unsupported delivery subject type")
	}
	s.Remote = strings.TrimSpace(s.Remote)
	s.Branch = strings.TrimSpace(s.Branch)
	s.BaseSHA = normalizeDigest(s.BaseSHA)
	s.CandidateSHA = normalizeDigest(s.CandidateSHA)
	s.CandidateRef = strings.TrimSpace(s.CandidateRef)
	s.PushedRemote = strings.TrimSpace(s.PushedRemote)
	if s.Remote == "" || s.Branch == "" || s.BaseSHA == "" || s.CandidateSHA == "" || s.CandidateRef == "" || s.PushedRemote == "" {
		return DeliverySubject{}, errors.New("projectmanager: immutable delivery subject requires remote, branch, base_sha, candidate_sha, candidate_ref and pushed_remote")
	}
	if !isImmutableVersion(s.BaseSHA) || !isImmutableVersion(s.CandidateSHA) {
		return DeliverySubject{}, errors.New("projectmanager: delivery subject requires immutable commit/digest shas")
	}
	if s.DeliveryContractHash == "" || s.AcceptanceContractHash == "" {
		return DeliverySubject{}, errors.New("projectmanager: delivery and acceptance contract hashes required")
	}
	if s.CreatedAt.IsZero() {
		return DeliverySubject{}, errors.New("projectmanager: delivery subject created_at required")
	}
	s.CreatedAt = s.CreatedAt.UTC()
	return s, nil
}

func NewAcceptance(a Acceptance, subject DeliverySubject) (Acceptance, error) {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.SubjectID) == "" || a.PlanID == "" || a.TaskID == "" {
		return Acceptance{}, errors.New("projectmanager: acceptance scope required")
	}
	if a.SubjectID != subject.ID || a.PlanID != subject.PlanID || a.TaskID != subject.TaskID {
		return Acceptance{}, errors.New("projectmanager: acceptance scope must match delivery subject")
	}
	if a.SubjectDigest != subject.Digest() {
		return Acceptance{}, errors.New("projectmanager: acceptance subject digest mismatch")
	}
	if a.ContractHash == "" || a.ContractHash != subject.AcceptanceContractHash {
		return Acceptance{}, errors.New("projectmanager: acceptance contract hash mismatch")
	}
	if a.Verdict != AcceptancePassed && a.Verdict != AcceptanceRejected && a.Verdict != AcceptanceWaivedByAuthority {
		return Acceptance{}, errors.New("projectmanager: acceptance verdict invalid")
	}
	if err := a.ActorRef.Validate(); err != nil {
		return Acceptance{}, err
	}
	if a.AuthorityRank <= 0 || strings.TrimSpace(a.AuthoritySource) == "" {
		return Acceptance{}, errors.New("projectmanager: trusted acceptance authority required")
	}
	a.EvidenceSHA = normalizeDigest(a.EvidenceSHA)
	if a.EvidenceSHA != "" && a.EvidenceSHA != subject.CandidateSHA {
		return Acceptance{}, errors.New("projectmanager: acceptance evidence sha must match delivery subject candidate")
	}
	if a.CreatedAt.IsZero() {
		return Acceptance{}, errors.New("projectmanager: acceptance created_at required")
	}
	a.CreatedAt = a.CreatedAt.UTC()
	return a, nil
}

func (s DeliverySubject) Digest() string {
	canonical := strings.Join([]string{
		string(s.SubjectType), string(s.PlanID), string(s.TaskID), s.NodeID, s.ExecutionID, s.RepoID,
		s.Remote, s.Branch, s.BaseSHA, s.CandidateSHA, s.CandidateRef, s.PushedRemote,
		s.DeliveryContractHash, s.AcceptanceContractHash, s.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, "\n")
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ContractHash(contract string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(contract)), " ")
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeDigest(v string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "sha256:")
}

func isImmutableVersion(v string) bool {
	v = normalizeDigest(v)
	if len(v) != 40 && len(v) != 64 {
		return false
	}
	for _, r := range v {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
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
		msg := "worktree still has uncommitted changes"
		if len(d.DirtyPaths) > 0 {
			msg += ": " + strings.Join(d.DirtyPaths, ", ")
		}
		reasons = append(reasons, DeliveryReason{Code: "worktree_dirty", Message: msg})
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
	return fmt.Sprintf("%s: %s; block/retry it or register manual_recovery delivery with pushed SHA + test evidence (report_manual_recovery_delivery MCP or worker recover-delivery CLI)",
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
