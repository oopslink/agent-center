// Package authorization implements the frozen unified access contract.
//
// The service derives legacy access from authoritative membership tables and
// layers explicit custom-role assignments on top. Runtime capabilities are not
// read as grants.
package authorization

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type PermissionKey string
type SubjectRef string
type Transport string
type DecisionSource string

const (
	TransportWeb       Transport = "web"
	TransportMCP       Transport = "mcp"
	TransportAdminHTTP Transport = "admin_http"
	TransportGitHTTP   Transport = "git_http"
	TransportSystem    Transport = "system"

	SourceOrgRole                 DecisionSource = "org_role"
	SourceProjectMember           DecisionSource = "project_member"
	SourceTeamMember              DecisionSource = "team_member"
	SourceTeamMemoryPolicy        DecisionSource = "team_memory_policy"
	SourceConversationParticipant DecisionSource = "conversation_participant"
	SourceFileScope               DecisionSource = "file_scope"
	SourceAdminTokenScope         DecisionSource = "admin_token_scope"
	SourceWorkerOwner             DecisionSource = "worker_owner"
	SourceAgentWorkerBinding      DecisionSource = "agent_worker_binding"
	SourceSystem                  DecisionSource = "system"
	SourceCustomRole              DecisionSource = "custom_role"
)

var (
	ErrDenied              = errors.New("authorization: permission denied")
	ErrUnauthenticated     = errors.New("authorization: unauthenticated")
	ErrNotFound            = errors.New("authorization: resource not found")
	ErrInvalid             = errors.New("authorization: invalid request")
	ErrConflict            = errors.New("authorization: conflict")
	ErrNotDelegatable      = errors.New("authorization: permission is not delegatable by actor")
	ErrPermissionUndefined = errors.New("authorization: permission is not defined")
	ErrRoleNotFound        = errors.New("authorization: role not found")
	ErrAssignmentNotFound  = errors.New("authorization: role assignment not found")
	ErrSystemRoleImmutable = errors.New("authorization: system role is immutable")
	ErrIdempotencyRequired = errors.New("authorization: idempotency key required")
	ErrIdempotencyConflict = errors.New("authorization: idempotency key reused with different request")
	ErrPreviewRequired     = errors.New("authorization: preview id required")
	ErrPreviewNotFound     = errors.New("authorization: preview not found")
	ErrPreviewExpired      = errors.New("authorization: preview expired")
	ErrPreviewStale        = errors.New("authorization: preview stale")
	ErrPreviewConsumed     = errors.New("authorization: preview already applied")
)

type ResourceScope struct {
	Kind             string    `json:"kind"`
	ID               string    `json:"id,omitempty"`
	OrgID            string    `json:"org_id,omitempty"`
	ProjectID        string    `json:"project_id,omitempty"`
	URI              string    `json:"uri,omitempty"`
	OwnerRef         string    `json:"owner_ref,omitempty"`
	IdentityMemberID string    `json:"identity_member_id,omitempty"`
	Refs             []FileRef `json:"refs,omitempty"`
}

type FileRef struct {
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id"`
}

type CheckRequest struct {
	SubjectRef  SubjectRef    `json:"subject_ref"`
	Transport   Transport     `json:"transport"`
	BearerScope string        `json:"bearer_scope,omitempty"`
	Permission  PermissionKey `json:"permission"`
	Resource    ResourceScope `json:"resource"`
	RequestID   string        `json:"request_id,omitempty"`
}

type AccessDecision struct {
	Allowed     bool           `json:"allowed"`
	SubjectRef  SubjectRef     `json:"subject_ref"`
	Permission  PermissionKey  `json:"permission"`
	Resource    ResourceScope  `json:"resource"`
	Source      DecisionSource `json:"source,omitempty"`
	Reason      string         `json:"reason"`
	EvidenceRef string         `json:"evidence_ref,omitempty"`
}

type EffectivePermission struct {
	Key          PermissionKey  `json:"key"`
	Source       DecisionSource `json:"source"`
	EvidenceRef  string         `json:"evidence_ref"`
	Delegatable  bool           `json:"delegatable,omitempty"`
	RoleID       string         `json:"role_id,omitempty"`
	AssignmentID string         `json:"assignment_id,omitempty"`
	ExpiresAt    *time.Time     `json:"expires_at,omitempty"`
}

type EffectivePermissions struct {
	SubjectRef  SubjectRef            `json:"subject_ref"`
	Resource    ResourceScope         `json:"resource"`
	Permissions []EffectivePermission `json:"permissions"`
}

type ExplainResult struct {
	Decision    AccessDecision        `json:"decision"`
	Effective   []EffectivePermission `json:"effective"`
	DeniedBy    []string              `json:"denied_by,omitempty"`
	ResolvedOrg string                `json:"resolved_org,omitempty"`
}

type AccessGraphRequest struct {
	SubjectRef     SubjectRef `json:"subject_ref"`
	OrgID          string     `json:"org_id"`
	Layer          string     `json:"layer,omitempty"`
	Cursor         string     `json:"cursor,omitempty"`
	Limit          int        `json:"limit,omitempty"`
	RedactEvidence bool       `json:"redact_evidence,omitempty"`
	ShadowParity   bool       `json:"shadow_parity,omitempty"`
	RequestID      string     `json:"request_id,omitempty"`
	ActorRef       SubjectRef `json:"actor_ref,omitempty"`
}

type AccessGraphPage struct {
	SubjectRef    SubjectRef                `json:"subject_ref"`
	OrgID         string                    `json:"org_id"`
	Layer         string                    `json:"layer"`
	Limit         int                       `json:"limit"`
	Cursor        string                    `json:"cursor,omitempty"`
	NextCursor    string                    `json:"next_cursor,omitempty"`
	Complete      bool                      `json:"complete"`
	Completeness  AccessGraphCompleteness   `json:"completeness"`
	Relationships []AccessGraphRelationship `json:"relationships"`
	Scopes        []AccessGraphScope        `json:"scopes"`
	Permissions   []AccessGraphPermission   `json:"permissions"`
	RiskSummary   AccessGraphRiskSummary    `json:"risk_summary"`
	ParityShadow  AccessGraphParityShadow   `json:"parity_shadow"`
}

type AccessGraphCompleteness struct {
	RequestedLayer string `json:"requested_layer"`
	Returned       int    `json:"returned"`
	Total          int    `json:"total"`
	HasMore        bool   `json:"has_more"`
	Reason         string `json:"reason,omitempty"`
}

type AccessGraphEvidence struct {
	Source   DecisionSource `json:"source"`
	Ref      string         `json:"ref,omitempty"`
	Hash     string         `json:"hash,omitempty"`
	Redacted bool           `json:"redacted,omitempty"`
}

type AccessGraphRelationship struct {
	ID              string              `json:"id"`
	SubjectRef      SubjectRef          `json:"subject_ref"`
	Source          DecisionSource      `json:"source"`
	Role            string              `json:"role,omitempty"`
	Evidence        AccessGraphEvidence `json:"evidence"`
	ScopeCount      int                 `json:"scope_count"`
	PermissionCount int                 `json:"permission_count"`
}

type AccessGraphScope struct {
	ID              string              `json:"id"`
	RelationshipID  string              `json:"relationship_id"`
	Resource        ResourceScope       `json:"resource"`
	Source          DecisionSource      `json:"source"`
	Evidence        AccessGraphEvidence `json:"evidence"`
	PermissionCount int                 `json:"permission_count"`
}

type AccessGraphPermission struct {
	ID             string              `json:"id"`
	RelationshipID string              `json:"relationship_id"`
	ScopeID        string              `json:"scope_id"`
	Resource       ResourceScope       `json:"resource"`
	Key            PermissionKey       `json:"key"`
	Source         DecisionSource      `json:"source"`
	Evidence       AccessGraphEvidence `json:"evidence"`
	Delegatable    bool                `json:"delegatable,omitempty"`
	RoleID         string              `json:"role_id,omitempty"`
	AssignmentID   string              `json:"assignment_id,omitempty"`
}

type AccessGraphRiskSummary struct {
	SubjectRef                 SubjectRef               `json:"subject_ref"`
	ActiveAdminTokens          int                      `json:"active_admin_tokens"`
	ActiveWorkerTokens         int                      `json:"active_worker_tokens"`
	ActiveEnrollTokens         int                      `json:"active_enroll_tokens"`
	WildcardAdminTokens        int                      `json:"wildcard_admin_tokens"`
	InactiveAdminTokens        int                      `json:"inactive_admin_tokens"`
	WorkerStatus               string                   `json:"worker_status,omitempty"`
	WorkerLastHeartbeatAt      string                   `json:"worker_last_heartbeat_at,omitempty"`
	WorkerDisabledCapabilities int                      `json:"worker_disabled_capabilities,omitempty"`
	Findings                   []AccessGraphRiskFinding `json:"findings,omitempty"`
}

type AccessGraphRiskFinding struct {
	Severity string              `json:"severity"`
	Kind     string              `json:"kind"`
	Message  string              `json:"message"`
	Evidence AccessGraphEvidence `json:"evidence"`
}

type AccessGraphParityShadow struct {
	Checked    int                        `json:"checked"`
	Mismatches int                        `json:"mismatches"`
	Complete   bool                       `json:"complete"`
	Findings   []AccessGraphParityFinding `json:"findings,omitempty"`
}

type AccessGraphParityFinding struct {
	Permission PermissionKey       `json:"permission"`
	Resource   ResourceScope       `json:"resource"`
	Source     DecisionSource      `json:"source"`
	Evidence   AccessGraphEvidence `json:"evidence"`
	Expected   bool                `json:"expected"`
	Actual     bool                `json:"actual"`
	Reason     string              `json:"reason,omitempty"`
}

type PermissionDefinition struct {
	Key           PermissionKey `json:"key"`
	Category      string        `json:"category"`
	ResourceKinds []string      `json:"resource_kinds"`
	Actions       []string      `json:"actions"`
	LegacySources []string      `json:"legacy_sources"`
}

type Role struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id,omitempty"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int       `json:"version"`
}

type RolePermission struct {
	RoleID        string        `json:"role_id"`
	PermissionKey PermissionKey `json:"permission_key"`
	ResourceKind  string        `json:"resource_kind"`
	Delegatable   bool          `json:"delegatable,omitempty"`
}

type RoleAssignment struct {
	ID            string     `json:"id"`
	OrgID         string     `json:"org_id"`
	SubjectRef    SubjectRef `json:"subject_ref"`
	RoleID        string     `json:"role_id"`
	ResourceKind  string     `json:"resource_kind"`
	ResourceID    string     `json:"resource_id"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedBy     string     `json:"revoked_by,omitempty"`
	RevokedReason string     `json:"revoked_reason,omitempty"`
	Version       int        `json:"version"`
}

type BatchRequest struct {
	IdempotencyKey string           `json:"idempotency_key,omitempty"`
	PreviewID      string           `json:"preview_id,omitempty"`
	ActorRef       SubjectRef       `json:"actor_ref"`
	OrgID          string           `json:"org_id"`
	Operations     []BatchOperation `json:"operations"`
}

type BatchOperation struct {
	ID          string                `json:"id,omitempty"`
	Type        string                `json:"type"`
	Role        RoleInput             `json:"role,omitempty"`
	Permissions []RolePermissionInput `json:"permissions,omitempty"`
	Assignment  AssignmentInput       `json:"assignment,omitempty"`
	Revoke      RevokeInput           `json:"revoke,omitempty"`
}

type RoleInput struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type RolePermissionInput struct {
	PermissionKey PermissionKey `json:"permission_key"`
	ResourceKind  string        `json:"resource_kind"`
	Delegatable   bool          `json:"delegatable,omitempty"`
}

type AssignmentInput struct {
	ID         string        `json:"id,omitempty"`
	SubjectRef SubjectRef    `json:"subject_ref"`
	RoleID     string        `json:"role_id"`
	Resource   ResourceScope `json:"resource"`
	ExpiresAt  *time.Time    `json:"expires_at,omitempty"`
}

type RevokeInput struct {
	AssignmentID string        `json:"assignment_id,omitempty"`
	SubjectRef   SubjectRef    `json:"subject_ref,omitempty"`
	RoleID       string        `json:"role_id,omitempty"`
	Resource     ResourceScope `json:"resource,omitempty"`
	Reason       string        `json:"reason,omitempty"`
}

type BatchResult struct {
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	PreviewID      string            `json:"preview_id,omitempty"`
	ExpiresAt      *time.Time        `json:"expires_at,omitempty"`
	Replayed       bool              `json:"replayed,omitempty"`
	Preview        bool              `json:"preview"`
	Operations     []OperationResult `json:"operations"`
}

type AuditEvent struct {
	ID            string         `json:"id"`
	EventType     string         `json:"event_type"`
	ActorRef      SubjectRef     `json:"actor_ref"`
	SubjectRef    SubjectRef     `json:"subject_ref,omitempty"`
	PermissionKey PermissionKey  `json:"permission_key,omitempty"`
	ResourceKind  string         `json:"resource_kind,omitempty"`
	ResourceID    string         `json:"resource_id,omitempty"`
	RoleID        string         `json:"role_id,omitempty"`
	AssignmentID  string         `json:"assignment_id,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	Payload       map[string]any `json:"payload,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

type OperationResult struct {
	ID           string `json:"id,omitempty"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	RoleID       string `json:"role_id,omitempty"`
	AssignmentID string `json:"assignment_id,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

func (s SubjectRef) Validate() error {
	v := strings.TrimSpace(string(s))
	if v == "system" {
		return nil
	}
	for _, p := range []string{"user:", "agent:", "worker:"} {
		if strings.HasPrefix(v, p) && len(v) > len(p) {
			return nil
		}
	}
	return fmt.Errorf("%w: invalid subject_ref %q", ErrInvalid, s)
}

func (s SubjectRef) BareID() string {
	v := string(s)
	if i := strings.IndexByte(v, ':'); i >= 0 {
		return v[i+1:]
	}
	return v
}

func (s SubjectRef) IsUser() bool   { return strings.HasPrefix(string(s), "user:") }
func (s SubjectRef) IsAgent() bool  { return strings.HasPrefix(string(s), "agent:") }
func (s SubjectRef) IsWorker() bool { return strings.HasPrefix(string(s), "worker:") }
func UserSubject(identityID string) SubjectRef {
	return SubjectRef("user:" + strings.TrimSpace(identityID))
}
func AgentSubject(identityMemberID string) SubjectRef {
	return SubjectRef("agent:" + strings.TrimSpace(identityMemberID))
}
func WorkerSubject(workerID string) SubjectRef {
	return SubjectRef("worker:" + strings.TrimSpace(workerID))
}

func (r ResourceScope) Key() (string, string) {
	id := r.ID
	if r.Kind == "file" {
		id = r.URI
	}
	return strings.TrimSpace(r.Kind), strings.TrimSpace(id)
}
