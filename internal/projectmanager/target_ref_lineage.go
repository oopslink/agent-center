package projectmanager

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TargetRefLineageProof is the final-delivery gate evidence that a reviewed
// candidate SHA is already contained by the remote target ref. It is intentionally
// about the release target (origin/main by default, or an explicitly declared
// target_ref), not the executor's feature branch.
type TargetRefLineageProof struct {
	TargetRef    string `json:"target_ref"`
	LSRemoteRef  string `json:"ls_remote_ref"`
	LSRemoteSHA  string `json:"ls_remote_sha"`
	CandidateSHA string `json:"candidate_sha"`
	Ancestor     bool   `json:"ancestor"`
}

type TargetRefLineageReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ValidateTargetRefLineageProof(proof *TargetRefLineageProof, reviewedSHA string, requireAncestor bool) (*TargetRefLineageProof, []TargetRefLineageReason) {
	if proof == nil {
		return nil, []TargetRefLineageReason{{
			Code:    "target_ref_lineage_missing",
			Message: "target-ref lineage proof is required before a stage/release gate may pass",
		}}
	}
	normalized := *proof
	normalized.TargetRef = strings.TrimSpace(normalized.TargetRef)
	normalized.LSRemoteRef = strings.TrimSpace(normalized.LSRemoteRef)
	normalized.LSRemoteSHA = strings.TrimSpace(normalized.LSRemoteSHA)
	normalized.CandidateSHA = strings.TrimSpace(normalized.CandidateSHA)

	var reasons []TargetRefLineageReason
	if normalized.TargetRef == "" {
		reasons = append(reasons, TargetRefLineageReason{Code: "target_ref_missing", Message: "target ref is missing; use origin/main or the declared release ref"})
	}
	if normalized.LSRemoteRef == "" {
		reasons = append(reasons, TargetRefLineageReason{Code: "ls_remote_ref_missing", Message: "ls-remote returned ref is missing"})
	}
	if normalized.LSRemoteSHA == "" {
		reasons = append(reasons, TargetRefLineageReason{Code: "ls_remote_sha_missing", Message: "ls-remote target SHA is missing"})
	}
	if normalized.CandidateSHA == "" {
		reasons = append(reasons, TargetRefLineageReason{Code: "candidate_sha_missing", Message: "candidate/reviewed SHA is missing"})
	}
	if normalized.TargetRef != "" && normalized.LSRemoteRef != "" &&
		canonicalTargetRef(normalized.TargetRef) != canonicalTargetRef(normalized.LSRemoteRef) {
		reasons = append(reasons, TargetRefLineageReason{
			Code: "ls_remote_ref_mismatch",
			Message: fmt.Sprintf("ls-remote ref %q does not match target_ref %q",
				normalized.LSRemoteRef, normalized.TargetRef),
		})
	}
	if reviewed := strings.TrimSpace(reviewedSHA); reviewed != "" && normalized.CandidateSHA != "" &&
		!shaPrefixMatch(normalized.CandidateSHA, reviewed) {
		reasons = append(reasons, TargetRefLineageReason{
			Code:    "candidate_sha_mismatch",
			Message: fmt.Sprintf("candidate SHA %s does not match reviewed SHA %s", normalized.CandidateSHA, reviewed),
		})
	}
	if requireAncestor && !normalized.Ancestor {
		reasons = append(reasons, TargetRefLineageReason{
			Code:    "candidate_not_ancestor_of_target",
			Message: "reviewed candidate SHA is not proven to be an ancestor of the remote target ref; add/complete a Ship node first",
		})
	}
	return &normalized, reasons
}

func TargetRefLineageReasonCodes(reasons []TargetRefLineageReason) []string {
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if reason.Code != "" {
			out = append(out, reason.Code)
		}
	}
	return out
}

func SameTargetRefLineage(a, b *TargetRefLineageProof) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aa, _ := ValidateTargetRefLineageProof(a, "", false)
	bb, _ := ValidateTargetRefLineageProof(b, "", false)
	if aa == nil || bb == nil {
		return aa == nil && bb == nil
	}
	return aa.TargetRef == bb.TargetRef &&
		aa.LSRemoteRef == bb.LSRemoteRef &&
		aa.LSRemoteSHA == bb.LSRemoteSHA &&
		aa.CandidateSHA == bb.CandidateSHA &&
		aa.Ancestor == bb.Ancestor
}

func MarshalTargetRefLineage(p *TargetRefLineageProof) (string, error) {
	if p == nil {
		return "", nil
	}
	normalized, reasons := ValidateTargetRefLineageProof(p, "", false)
	if len(reasons) != 0 {
		return "", fmt.Errorf("projectmanager: invalid target-ref lineage proof: %s", targetRefLineageReasonString(reasons))
	}
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func UnmarshalTargetRefLineage(raw string) (*TargetRefLineageProof, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var proof TargetRefLineageProof
	if err := json.Unmarshal([]byte(raw), &proof); err != nil {
		return nil, err
	}
	normalized, reasons := ValidateTargetRefLineageProof(&proof, "", false)
	if len(reasons) != 0 {
		return nil, fmt.Errorf("projectmanager: invalid target-ref lineage proof: %s", targetRefLineageReasonString(reasons))
	}
	return normalized, nil
}

func targetRefLineageReasonString(reasons []TargetRefLineageReason) string {
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if reason.Message != "" {
			parts = append(parts, reason.Message)
		} else {
			parts = append(parts, reason.Code)
		}
	}
	return strings.Join(parts, "; ")
}

func canonicalTargetRef(ref string) string {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "":
		return ""
	case strings.HasPrefix(ref, "refs/heads/"), strings.HasPrefix(ref, "refs/tags/"):
		return ref
	case strings.HasPrefix(ref, "heads/"), strings.HasPrefix(ref, "tags/"):
		return "refs/" + ref
	case strings.HasPrefix(ref, "origin/"):
		return "refs/heads/" + strings.TrimPrefix(ref, "origin/")
	case strings.HasPrefix(ref, "refs/"):
		return ref
	default:
		return "refs/heads/" + ref
	}
}

func shaPrefixMatch(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if len(a) < 7 || len(b) < 7 {
		return a == b
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}
