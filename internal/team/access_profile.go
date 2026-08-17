package team

import (
	"fmt"
	"strings"
)

type AccessRequirement struct {
	PermissionKey string `json:"permission_key"`
	ResourceKind  string `json:"resource_kind"`
	Required      bool   `json:"required,omitempty"`
}

type AccessProfileMode string

const (
	AccessProfileDefault    AccessProfileMode = "default"
	AccessProfileAdditional AccessProfileMode = "additional"
	AccessProfileOverride   AccessProfileMode = "override"
)

type AccessProfileRef struct {
	ProfileID string            `json:"profile_id"`
	Version   int               `json:"version"`
	Mode      AccessProfileMode `json:"mode"`
}

type RoleAccessLint struct {
	Role     string `json:"role"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

func normalizeRoleAccess(role string, reqs []AccessRequirement, refs []AccessProfileRef) ([]AccessRequirement, []AccessProfileRef, []RoleAccessLint, error) {
	var lints []RoleAccessLint
	seenReq := map[string]struct{}{}
	outReqs := make([]AccessRequirement, 0, len(reqs))
	for _, req := range reqs {
		key := strings.TrimSpace(req.PermissionKey)
		kind := strings.TrimSpace(req.ResourceKind)
		if key == "" || kind == "" {
			return nil, nil, nil, ErrInvalidAccessRequirements
		}
		if strings.HasPrefix(kind, "membership") || strings.HasPrefix(key, "membership.") {
			lints = append(lints, RoleAccessLint{Role: role, Code: "membership-derived", Severity: "error", Message: "membership-derived permissions must stay derived and cannot become custom assignments"})
			return nil, nil, lints, ErrInvalidAccessRequirements
		}
		if req.Required {
			lints = append(lints, RoleAccessLint{Role: role, Code: "required-profile", Severity: "info", Message: fmt.Sprintf("%s on %s must be satisfied by an access profile preview", key, kind)})
		}
		dedupe := key + "\x00" + kind
		if _, ok := seenReq[dedupe]; ok {
			continue
		}
		seenReq[dedupe] = struct{}{}
		outReqs = append(outReqs, AccessRequirement{PermissionKey: key, ResourceKind: kind, Required: req.Required})
	}
	outRefs := make([]AccessProfileRef, 0, len(refs))
	seenRef := map[string]struct{}{}
	overrideSeen := false
	for _, ref := range refs {
		ref.ProfileID = strings.TrimSpace(ref.ProfileID)
		if ref.ProfileID == "" || ref.Version <= 0 {
			return nil, nil, lints, ErrInvalidAccessProfileRef
		}
		switch ref.Mode {
		case "":
			ref.Mode = AccessProfileDefault
		case AccessProfileDefault, AccessProfileAdditional:
		case AccessProfileOverride:
			if overrideSeen {
				lints = append(lints, RoleAccessLint{Role: role, Code: "multiple-overrides", Severity: "error", Message: "only one override access profile may be attached to a role"})
				return nil, nil, lints, ErrInvalidAccessProfileRef
			}
			overrideSeen = true
		default:
			return nil, nil, lints, ErrInvalidAccessProfileRef
		}
		dedupe := ref.ProfileID + "\x00" + fmt.Sprint(ref.Version) + "\x00" + string(ref.Mode)
		if _, ok := seenRef[dedupe]; ok {
			continue
		}
		seenRef[dedupe] = struct{}{}
		outRefs = append(outRefs, ref)
	}
	return outReqs, outRefs, lints, nil
}

func LintRoleAccessRequirements(roles []RoleConfig) []RoleAccessLint {
	var out []RoleAccessLint
	for _, rc := range roles {
		_, _, lints, err := normalizeRoleAccess(strings.TrimSpace(rc.Role), rc.AccessRequirements, rc.AccessProfiles)
		out = append(out, lints...)
		if err != nil && len(lints) == 0 {
			out = append(out, RoleAccessLint{Role: strings.TrimSpace(rc.Role), Code: "invalid-access-config", Severity: "error", Message: err.Error()})
		}
	}
	return out
}
