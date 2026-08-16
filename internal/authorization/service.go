package authorization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

type Service struct {
	db    *sql.DB
	store *Store
	gen   idgen.Generator
	clock clock.Clock
	sink  *observability.EventSink
}

type Deps struct {
	DB        *sql.DB
	Store     *Store
	IDGen     idgen.Generator
	Clock     clock.Clock
	EventSink *observability.EventSink
}

func New(deps Deps) *Service {
	clk := deps.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	gen := deps.IDGen
	if gen == nil {
		gen = idgen.NewGenerator(clk)
	}
	store := deps.Store
	if store == nil && deps.DB != nil {
		store = NewStore(deps.DB)
	}
	return &Service{db: deps.DB, store: store, gen: gen, clock: clk, sink: deps.EventSink}
}

func (s *Service) ListDefinitions(ctx context.Context) ([]PermissionDefinition, error) {
	if s == nil || s.store == nil {
		return Definitions(), nil
	}
	defs, err := s.store.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	return defs, nil
}

func (s *Service) Check(ctx context.Context, req CheckRequest) (AccessDecision, error) {
	exp, err := s.Explain(ctx, req)
	if err != nil {
		return exp.Decision, err
	}
	if !exp.Decision.Allowed {
		return exp.Decision, ErrDenied
	}
	return exp.Decision, nil
}

func (s *Service) Explain(ctx context.Context, req CheckRequest) (ExplainResult, error) {
	decision := AccessDecision{
		SubjectRef: req.SubjectRef,
		Permission: req.Permission,
		Resource:   req.Resource,
		Reason:     "permission_denied",
	}
	if s == nil || s.db == nil || s.store == nil {
		decision.Reason = "authorization_not_wired"
		return ExplainResult{Decision: decision, DeniedBy: []string{"authorization service is not wired"}}, ErrDenied
	}
	req.SubjectRef = SubjectRef(strings.TrimSpace(string(req.SubjectRef)))
	if err := req.SubjectRef.Validate(); err != nil {
		decision.Reason = "invalid_subject"
		return ExplainResult{Decision: decision, DeniedBy: []string{err.Error()}}, err
	}
	if req.SubjectRef == "system" {
		decision.Allowed = true
		decision.Source = SourceSystem
		decision.Reason = "system actor"
		decision.EvidenceRef = "system"
		return ExplainResult{Decision: decision}, nil
	}
	if strings.TrimSpace(string(req.Permission)) == "" {
		decision.Reason = "permission_required"
		return ExplainResult{Decision: decision, DeniedBy: []string{"permission is required"}}, ErrInvalid
	}
	resolved, denied, err := s.resolveResource(ctx, req.Resource)
	if err != nil {
		decision.Resource = resolved
		decision.Reason = "resource_not_found"
		return ExplainResult{Decision: decision, DeniedBy: denied, ResolvedOrg: resolved.OrgID}, err
	}
	req.Resource = resolved
	decision.Resource = resolved
	if req.Permission != "*" && !PermissionDefinedForResource(req.Permission, resolved.Kind) {
		decision.Reason = "permission_undefined"
		return ExplainResult{Decision: decision, DeniedBy: []string{string(req.Permission) + " is not defined for " + resolved.Kind}, ResolvedOrg: resolved.OrgID}, ErrPermissionUndefined
	}
	effective, deniedBy, err := s.deriveEffective(ctx, req)
	if err != nil {
		decision.Reason = "derive_failed"
		return ExplainResult{Decision: decision, Effective: effective, DeniedBy: append(deniedBy, err.Error()), ResolvedOrg: resolved.OrgID}, err
	}
	for _, p := range effective {
		if p.Key == req.Permission || req.Permission == "*" {
			decision.Allowed = true
			decision.Source = p.Source
			decision.Reason = "matched " + string(p.Source)
			decision.EvidenceRef = p.EvidenceRef
			return ExplainResult{Decision: decision, Effective: effective, DeniedBy: deniedBy, ResolvedOrg: resolved.OrgID}, nil
		}
	}
	return ExplainResult{Decision: decision, Effective: effective, DeniedBy: deniedBy, ResolvedOrg: resolved.OrgID}, nil
}

func (s *Service) ListEffective(ctx context.Context, subject SubjectRef, resource ResourceScope) (EffectivePermissions, error) {
	req := CheckRequest{SubjectRef: subject, Transport: TransportWeb, Permission: "org.read", Resource: resource}
	resolved, _, err := s.resolveResource(ctx, resource)
	if err != nil {
		return EffectivePermissions{SubjectRef: subject, Resource: resolved}, err
	}
	req.Resource = resolved
	effective, _, err := s.deriveEffective(ctx, req)
	if err != nil {
		return EffectivePermissions{SubjectRef: subject, Resource: resolved, Permissions: effective}, err
	}
	sort.Slice(effective, func(i, j int) bool {
		if effective[i].Key == effective[j].Key {
			return effective[i].EvidenceRef < effective[j].EvidenceRef
		}
		return effective[i].Key < effective[j].Key
	})
	return EffectivePermissions{SubjectRef: subject, Resource: resolved, Permissions: effective}, nil
}

func (s *Service) ListSubjectAudit(ctx context.Context, subject SubjectRef, orgID string, limit int) ([]AuditEvent, error) {
	if s == nil || s.db == nil || s.store == nil {
		return nil, errors.New("authorization service: nil db")
	}
	subject = SubjectRef(strings.TrimSpace(string(subject)))
	orgID = strings.TrimSpace(orgID)
	if subject == "" || orgID == "" {
		return nil, fmt.Errorf("%w: subject_ref and org_id required", ErrInvalid)
	}
	if err := subject.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rawLimit := limit * 4
	if rawLimit < 100 {
		rawLimit = 100
	}
	events, err := s.store.listAuditEventsForSubject(ctx, subject, rawLimit)
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(events))
	for _, e := range events {
		if !s.auditEventInOrg(ctx, e, orgID) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Service) auditEventInOrg(ctx context.Context, e AuditEvent, orgID string) bool {
	kind := strings.TrimSpace(e.ResourceKind)
	id := strings.TrimSpace(e.ResourceID)
	if kind == "" || id == "" {
		return false
	}
	if kind == "org" {
		return id == orgID
	}
	resolved, _, err := s.resolveResource(ctx, ResourceScope{Kind: kind, ID: id, OrgID: orgID})
	return err == nil && resolved.OrgID == orgID
}

func (s *Service) PreviewBatch(ctx context.Context, req BatchRequest) (BatchResult, error) {
	if s == nil || s.db == nil {
		return BatchResult{}, errors.New("authorization service: nil db")
	}
	var out BatchResult
	rollbackPreview := errors.New("authorization: rollback preview")
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		req.ActorRef = SubjectRef(strings.TrimSpace(string(req.ActorRef)))
		req.OrgID = strings.TrimSpace(req.OrgID)
		if req.ActorRef == "" || req.OrgID == "" {
			return fmt.Errorf("%w: actor_ref and org_id required", ErrInvalid)
		}
		if err := req.ActorRef.Validate(); err != nil {
			return err
		}
		out = BatchResult{IdempotencyKey: req.IdempotencyKey, Preview: true}
		for _, op := range req.Operations {
			result, err := s.runOperation(txCtx, req.ActorRef, req.OrgID, op)
			if err != nil {
				out.Operations = append(out.Operations, OperationResult{ID: op.ID, Type: op.Type, Status: "denied", Reason: err.Error()})
				continue
			}
			result.Status = previewStatus(result.Status)
			out.Operations = append(out.Operations, result)
		}
		return rollbackPreview
	})
	if errors.Is(err, rollbackPreview) {
		return out, nil
	}
	return BatchResult{}, err
}

func previewStatus(status string) string {
	switch status {
	case "created":
		return "would_create"
	case "updated":
		return "would_update"
	case "set":
		return "would_set"
	case "revoked":
		return "would_revoke"
	case "unchanged":
		return "would_leave_unchanged"
	default:
		return "would_" + status
	}
}

func (s *Service) ApplyBatch(ctx context.Context, req BatchRequest) (BatchResult, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return BatchResult{}, ErrIdempotencyRequired
	}
	var out BatchResult
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		digest, err := batchDigest(req)
		if err != nil {
			return err
		}
		if prevJSON, replay, err := s.store.beginIdempotency(txCtx, req.IdempotencyKey, string(req.ActorRef), "apply", digest, s.clock.Now()); err != nil || replay {
			if err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(prevJSON), &out); err != nil {
				return err
			}
			out.Replayed = true
			return nil
		}
		res, err := s.runBatchInTx(txCtx, req)
		if err != nil {
			return err
		}
		body, err := json.Marshal(res)
		if err != nil {
			return err
		}
		if err := s.store.completeIdempotency(txCtx, req.IdempotencyKey, body, s.clock.Now()); err != nil {
			return err
		}
		out = res
		return nil
	})
	return out, err
}

func (s *Service) RevokeBatch(ctx context.Context, req BatchRequest) (BatchResult, error) {
	for i := range req.Operations {
		req.Operations[i].Type = "revoke_assignment"
	}
	return s.ApplyBatch(ctx, req)
}

func (s *Service) runBatchInTx(ctx context.Context, req BatchRequest) (BatchResult, error) {
	req.ActorRef = SubjectRef(strings.TrimSpace(string(req.ActorRef)))
	req.OrgID = strings.TrimSpace(req.OrgID)
	if req.ActorRef == "" || req.OrgID == "" {
		return BatchResult{}, fmt.Errorf("%w: actor_ref and org_id required", ErrInvalid)
	}
	if err := req.ActorRef.Validate(); err != nil {
		return BatchResult{}, err
	}
	res := BatchResult{IdempotencyKey: req.IdempotencyKey}
	for _, op := range req.Operations {
		or, err := s.runOperation(ctx, req.ActorRef, req.OrgID, op)
		if err != nil {
			return BatchResult{}, err
		}
		res.Operations = append(res.Operations, or)
	}
	return res, nil
}

func (s *Service) runOperation(ctx context.Context, actor SubjectRef, orgID string, op BatchOperation) (OperationResult, error) {
	switch strings.TrimSpace(op.Type) {
	case "upsert_role":
		if err := s.requireManageRBAC(ctx, actor, orgID); err != nil {
			return OperationResult{}, err
		}
		id := strings.TrimSpace(op.Role.ID)
		if id == "" {
			id = "role-" + shortHash(orgID+"|"+op.Role.Name)
		}
		role, status, err := s.store.upsertCustomRole(ctx, Role{
			ID:          id,
			OrgID:       orgID,
			Kind:        "custom",
			Name:        op.Role.Name,
			Description: op.Role.Description,
			CreatedBy:   string(actor),
		}, s.clock.Now())
		if err != nil {
			return OperationResult{}, err
		}
		if err := s.audit(ctx, auditEvent{EventType: "authorization.role.upserted", ActorRef: actor, RoleID: role.ID, ResourceKind: "org", ResourceID: orgID, Payload: map[string]any{"status": status, "name": role.Name}}); err != nil {
			return OperationResult{}, err
		}
		return OperationResult{ID: op.ID, Type: op.Type, Status: status, RoleID: role.ID}, nil

	case "set_role_permissions":
		if err := s.requireManageRBAC(ctx, actor, orgID); err != nil {
			return OperationResult{}, err
		}
		roleID := strings.TrimSpace(op.Role.ID)
		if roleID == "" {
			roleID = strings.TrimSpace(op.Assignment.RoleID)
		}
		if roleID == "" {
			return OperationResult{}, fmt.Errorf("%w: role id required", ErrInvalid)
		}
		role, err := s.store.getRole(ctx, roleID)
		if err != nil {
			return OperationResult{}, err
		}
		if role.Kind == "custom" && role.OrgID != orgID {
			return OperationResult{}, fmt.Errorf("%w: role belongs to another org", ErrNotFound)
		}
		for _, p := range op.Permissions {
			if !PermissionDefinedForResource(p.PermissionKey, p.ResourceKind) {
				return OperationResult{}, fmt.Errorf("%w: %s for %s", ErrPermissionUndefined, p.PermissionKey, p.ResourceKind)
			}
		}
		if err := s.store.replaceRolePermissions(ctx, roleID, op.Permissions, s.clock.Now()); err != nil {
			return OperationResult{}, err
		}
		if err := s.audit(ctx, auditEvent{EventType: "authorization.role_permissions.set", ActorRef: actor, RoleID: roleID, ResourceKind: "org", ResourceID: orgID, Payload: map[string]any{"count": len(op.Permissions)}}); err != nil {
			return OperationResult{}, err
		}
		return OperationResult{ID: op.ID, Type: op.Type, Status: "set", RoleID: roleID}, nil

	case "assign_role":
		roleID := strings.TrimSpace(op.Assignment.RoleID)
		role, err := s.store.getRole(ctx, roleID)
		if err != nil {
			return OperationResult{}, err
		}
		if role.Kind == "custom" && role.OrgID != orgID {
			return OperationResult{}, fmt.Errorf("%w: role belongs to another org", ErrInvalid)
		}
		kind, resourceID := op.Assignment.Resource.Key()
		if kind == "" || resourceID == "" {
			return OperationResult{}, fmt.Errorf("%w: assignment resource required", ErrInvalid)
		}
		if err := op.Assignment.SubjectRef.Validate(); err != nil {
			return OperationResult{}, err
		}
		resolved, _, err := s.resolveResource(ctx, op.Assignment.Resource)
		if err != nil {
			return OperationResult{}, err
		}
		if resolved.OrgID == "" || resolved.OrgID != orgID {
			return OperationResult{}, fmt.Errorf("%w: assignment resource belongs to another org", ErrNotFound)
		}
		op.Assignment.Resource = resolved
		if err := s.requireAssignmentSubjectApplicable(ctx, op.Assignment.SubjectRef, roleID, resolved); err != nil {
			return OperationResult{}, err
		}
		if err := s.requireDelegatableRole(ctx, actor, roleID, op.Assignment.Resource); err != nil {
			return OperationResult{}, err
		}
		assignID := strings.TrimSpace(op.Assignment.ID)
		if assignID == "" {
			assignID = "asgn-" + shortHash(orgID+"|"+string(op.Assignment.SubjectRef)+"|"+roleID+"|"+kind+"|"+resourceID)
		}
		a, status, err := s.store.assignRole(ctx, RoleAssignment{
			ID:           assignID,
			OrgID:        orgID,
			SubjectRef:   op.Assignment.SubjectRef,
			RoleID:       roleID,
			ResourceKind: kind,
			ResourceID:   resourceID,
			CreatedBy:    string(actor),
			ExpiresAt:    op.Assignment.ExpiresAt,
		}, s.clock.Now())
		if err != nil {
			return OperationResult{}, err
		}
		if err := s.audit(ctx, auditEvent{EventType: "authorization.assignment.created", ActorRef: actor, SubjectRef: a.SubjectRef, RoleID: roleID, AssignmentID: a.ID, ResourceKind: kind, ResourceID: resourceID, Payload: map[string]any{"status": status}}); err != nil {
			return OperationResult{}, err
		}
		return OperationResult{ID: op.ID, Type: op.Type, Status: status, RoleID: roleID, AssignmentID: a.ID}, nil

	case "revoke_assignment":
		if err := s.requireRevokeAllowed(ctx, actor, orgID, op.Revoke); err != nil {
			return OperationResult{}, err
		}
		a, status, err := s.store.revokeAssignment(ctx, op.Revoke, actor, orgID, s.clock.Now())
		if err != nil {
			return OperationResult{}, err
		}
		if err := s.audit(ctx, auditEvent{EventType: "authorization.assignment.revoked", ActorRef: actor, SubjectRef: a.SubjectRef, RoleID: a.RoleID, AssignmentID: a.ID, ResourceKind: a.ResourceKind, ResourceID: a.ResourceID, Payload: map[string]any{"status": status, "reason": op.Revoke.Reason}}); err != nil {
			return OperationResult{}, err
		}
		return OperationResult{ID: op.ID, Type: op.Type, Status: status, RoleID: a.RoleID, AssignmentID: a.ID}, nil
	default:
		return OperationResult{}, fmt.Errorf("%w: unknown operation type %q", ErrInvalid, op.Type)
	}
}

func (s *Service) requireManageRBAC(ctx context.Context, actor SubjectRef, orgID string) error {
	if actor == "system" {
		return nil
	}
	_, err := s.Check(ctx, CheckRequest{
		SubjectRef: actor,
		Transport:  TransportSystem,
		Permission: "org.member.role.manage",
		Resource:   ResourceScope{Kind: "org", ID: orgID},
	})
	return err
}

func (s *Service) requireDelegatableRole(ctx context.Context, actor SubjectRef, roleID string, resource ResourceScope) error {
	if actor == "system" {
		return nil
	}
	perms, err := s.store.rolePermissions(ctx, roleID)
	if err != nil {
		return err
	}
	if len(perms) == 0 {
		return fmt.Errorf("%w: role has no permissions", ErrInvalid)
	}
	for _, p := range perms {
		if p.ResourceKind != resource.Kind {
			return fmt.Errorf("%w: role permission %s is scoped to %s, assignment resource is %s", ErrInvalid, p.PermissionKey, p.ResourceKind, resource.Kind)
		}
		if !PermissionDefinedForResource(p.PermissionKey, p.ResourceKind) {
			return fmt.Errorf("%w: %s for %s", ErrPermissionUndefined, p.PermissionKey, p.ResourceKind)
		}
		target := resource
		target.Kind = p.ResourceKind
		exp, err := s.Explain(ctx, CheckRequest{
			SubjectRef: actor,
			Transport:  TransportSystem,
			Permission: p.PermissionKey,
			Resource:   target,
		})
		if err != nil && !errors.Is(err, ErrDenied) {
			return err
		}
		if !exp.Decision.Allowed {
			return fmt.Errorf("%w: %s", ErrNotDelegatable, p.PermissionKey)
		}
		var delegatable bool
		for _, eff := range exp.Effective {
			if eff.Key == p.PermissionKey && eff.Delegatable {
				delegatable = true
				break
			}
		}
		if !delegatable {
			return fmt.Errorf("%w: %s", ErrNotDelegatable, p.PermissionKey)
		}
	}
	return nil
}

var agentForbiddenPermissions = map[PermissionKey]struct{}{
	"org.settings.manage":    {},
	"org.lifecycle.manage":   {},
	"org.member.role.manage": {},
	"org.member.disable":     {},
	"admin_token.manage":     {},
	"secret.resolve":         {},
}

func (s *Service) requireAssignmentSubjectApplicable(ctx context.Context, subject SubjectRef, roleID string, resource ResourceScope) error {
	if !(subject.IsUser() || subject.IsAgent()) {
		return fmt.Errorf("%w: role assignments require a human or agent subject", ErrInvalid)
	}
	if _, ok, err := s.orgMember(ctx, resource.OrgID, subject); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: assignment subject is not a joined org member", ErrNotFound)
	}
	if !subject.IsAgent() {
		return nil
	}
	perms, err := s.store.rolePermissions(ctx, roleID)
	if err != nil {
		return err
	}
	for _, p := range perms {
		if _, forbidden := agentForbiddenPermissions[p.PermissionKey]; forbidden {
			return fmt.Errorf("%w: agents cannot receive high-risk permission %s", ErrInvalid, p.PermissionKey)
		}
	}
	return nil
}

func (s *Service) requireRevokeAllowed(ctx context.Context, actor SubjectRef, orgID string, in RevokeInput) error {
	target, err := s.resolveRevokeTarget(ctx, orgID, in)
	if err != nil {
		return err
	}
	if target.OrgID != strings.TrimSpace(orgID) {
		return ErrNotFound
	}
	if target.RoleID == "sys-org-owner" {
		remaining, err := s.remainingOrgOwners(ctx, orgID, target.ID)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return fmt.Errorf("%w: cannot revoke the last organization owner", ErrConflict)
		}
	}
	if actor == "system" {
		return nil
	}
	if _, err := s.Check(ctx, CheckRequest{SubjectRef: actor, Transport: TransportSystem, Permission: "org.member.role.manage", Resource: ResourceScope{Kind: "org", ID: orgID}}); err == nil {
		return nil
	}
	return s.requireDelegatableRole(ctx, actor, target.RoleID, ResourceScope{
		Kind:  target.ResourceKind,
		ID:    target.ResourceID,
		OrgID: target.OrgID,
	})
}

func (s *Service) resolveRevokeTarget(ctx context.Context, orgID string, in RevokeInput) (RoleAssignment, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return RoleAssignment{}, fmt.Errorf("%w: org_id required", ErrInvalid)
	}
	target, err := s.store.assignmentForRevoke(ctx, orgID, in)
	if err != nil {
		if errors.Is(err, ErrAssignmentNotFound) {
			return RoleAssignment{}, ErrNotFound
		}
		return RoleAssignment{}, err
	}
	if target.OrgID != orgID {
		return RoleAssignment{}, ErrNotFound
	}
	resolved, _, err := s.resolveResource(ctx, ResourceScope{
		Kind:  target.ResourceKind,
		ID:    target.ResourceID,
		OrgID: target.OrgID,
	})
	if err != nil {
		return RoleAssignment{}, err
	}
	if resolved.OrgID != orgID {
		return RoleAssignment{}, ErrNotFound
	}
	return target, nil
}

func (s *Service) remainingOrgOwners(ctx context.Context, orgID, excludingAssignmentID string) (int, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return 0, err
	}
	var legacy, assigned int
	if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE organization_id = ? AND role = 'owner' AND status = 'joined'`, orgID).Scan(&legacy); err != nil {
		return 0, err
	}
	if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_role_assignments
		WHERE org_id = ? AND role_id = 'sys-org-owner' AND id <> ? AND revoked_at IS NULL`, orgID, excludingAssignmentID).Scan(&assigned); err != nil {
		return 0, err
	}
	return legacy + assigned, nil
}

func (s *Service) deriveEffective(ctx context.Context, req CheckRequest) ([]EffectivePermission, []string, error) {
	var out []EffectivePermission
	var denied []string
	add := func(key PermissionKey, source DecisionSource, evidence string, delegatable bool) {
		if key == "" {
			return
		}
		for _, p := range out {
			if p.Key == key && p.Source == source && p.EvidenceRef == evidence {
				return
			}
		}
		out = append(out, EffectivePermission{Key: key, Source: source, EvidenceRef: evidence, Delegatable: delegatable})
	}
	if req.BearerScope != "" {
		if key, ok := PermissionForBearerScope(req.BearerScope); ok {
			if key == "*" {
				add(req.Permission, SourceAdminTokenScope, "admin_tokens:*", false)
			} else {
				add(key, SourceAdminTokenScope, "admin_tokens:"+req.BearerScope, false)
			}
		} else {
			denied = append(denied, "bearer scope has no permission mapping")
		}
	}
	if err := s.addLegacyEffective(ctx, req, add, &denied); err != nil {
		return out, denied, err
	}
	if err := s.addCustomEffective(ctx, req, &out); err != nil {
		return out, denied, err
	}
	return out, denied, nil
}

func (s *Service) addLegacyEffective(ctx context.Context, req CheckRequest, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	r := req.Resource
	if r.ID == "*" && req.BearerScope != "" {
		return nil
	}
	if r.OrgID != "" && r.Kind != "worker" && r.Kind != "admin_token" && r.Kind != "secret" && r.Kind != "blob" {
		m, ok, err := s.orgMember(ctx, r.OrgID, req.SubjectRef)
		if err != nil {
			return err
		}
		if ok {
			if disabled, err := s.orgDisabled(ctx, r.OrgID); err != nil {
				return err
			} else if disabled && m.Role != "owner" {
				*denied = append(*denied, "disabled org admits only owner members")
				return nil
			}
			switch r.Kind {
			case "org":
				addOrgRole(m.Role, m.EvidenceRef, add)
			case "team":
				addTeamHumanRole(m.Role, m.EvidenceRef, add)
			}
		} else if req.SubjectRef.IsUser() || req.SubjectRef.IsAgent() {
			*denied = append(*denied, "subject is not a joined org member")
		}
	}
	switch r.Kind {
	case "project":
		return s.addProjectEffective(ctx, req.SubjectRef, r.ID, add, denied)
	case "task":
		return s.addTaskEffective(ctx, req.SubjectRef, r.ID, add, denied)
	case "issue":
		return s.addIssueEffective(ctx, req.SubjectRef, r.ID, add, denied)
	case "plan":
		return s.addPlanEffective(ctx, req.SubjectRef, r.ID, add, denied)
	case "team":
		return s.addTeamEffective(ctx, req.SubjectRef, r.ID, add, denied)
	case "conversation":
		return s.addConversationEffective(ctx, req.SubjectRef, r.ID, add, denied)
	case "file":
		return s.addFileEffective(ctx, req.SubjectRef, r, add, denied)
	case "agent":
		return s.addAgentEffective(ctx, req, add, denied)
	case "worker":
		return s.addWorkerEffective(req, add, denied)
	case "git":
		add("git.global.read", SourceSystem, "system:global_git_read", false)
	}
	return nil
}

type orgMemberRecord struct {
	ID          string
	Role        string
	EvidenceRef string
}

func (s *Service) orgMember(ctx context.Context, orgID string, subject SubjectRef) (orgMemberRecord, bool, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return orgMemberRecord{}, false, err
	}
	var row *sql.Row
	switch {
	case subject.IsUser():
		row = exec.QueryRowContext(ctx, `SELECT id, role FROM members WHERE organization_id = ? AND identity_id = ? AND status = 'joined'`, orgID, subject.BareID())
	case subject.IsAgent():
		row = exec.QueryRowContext(ctx, `SELECT id, role FROM members WHERE organization_id = ? AND id = ? AND status = 'joined'`, orgID, subject.BareID())
	default:
		return orgMemberRecord{}, false, nil
	}
	var m orgMemberRecord
	if err := row.Scan(&m.ID, &m.Role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return orgMemberRecord{}, false, nil
		}
		return orgMemberRecord{}, false, err
	}
	m.EvidenceRef = "members:" + m.ID
	return m, true, nil
}

func (s *Service) orgDisabled(ctx context.Context, orgID string) (bool, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return false, err
	}
	var disabled sql.NullString
	if err := exec.QueryRowContext(ctx, `SELECT disabled_at FROM organizations WHERE id = ? AND deleted_at IS NULL`, orgID).Scan(&disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	return disabled.Valid && strings.TrimSpace(disabled.String) != "", nil
}

func addOrgRole(role, evidence string, add func(PermissionKey, DecisionSource, string, bool)) {
	add("org.read", SourceOrgRole, evidence, role == "owner")
	add("org.member.list", SourceOrgRole, evidence, role == "owner")
	add("org.work_items.read", SourceOrgRole, evidence, role == "owner")
	add("coderepo.workspace.read", SourceOrgRole, evidence, role == "owner")
	add("ai_runtime.catalog.read", SourceOrgRole, evidence, role == "owner")
	add("ai_runtime.catalog.export", SourceOrgRole, evidence, role == "owner")
	if role == "admin" || role == "owner" {
		add("org.member.create.human", SourceOrgRole, evidence, true)
		add("org.member.create.agent", SourceOrgRole, evidence, true)
		add("org.invitation.manage", SourceOrgRole, evidence, true)
		add("org.analytics.read", SourceOrgRole, evidence, role == "owner")
		add("coderepo.workspace.manage", SourceOrgRole, evidence, role == "owner")
		add("ai_runtime.catalog.manage", SourceOrgRole, evidence, role == "owner")
	}
	if role == "owner" {
		add("org.settings.manage", SourceOrgRole, evidence, true)
		add("org.lifecycle.manage", SourceOrgRole, evidence, true)
		add("org.member.role.manage", SourceOrgRole, evidence, true)
		add("org.member.disable", SourceOrgRole, evidence, true)
	}
}

func addTeamHumanRole(role, evidence string, add func(PermissionKey, DecisionSource, string, bool)) {
	add("team.read", SourceOrgRole, evidence, role == "owner")
	add("team.write", SourceOrgRole, evidence, role == "owner")
	add("team.member.manage", SourceOrgRole, evidence, role == "owner")
	add("team.project.link.manage", SourceOrgRole, evidence, role == "owner")
	add("team.runtime_config.manage", SourceOrgRole, evidence, role == "owner")
	add("team.memory.read", SourceOrgRole, evidence, role == "owner")
	if role == "admin" || role == "owner" {
		add("team.memory.propose", SourceOrgRole, evidence, role == "owner")
		add("team.memory.review", SourceOrgRole, evidence, role == "owner")
	}
}

func (s *Service) addProjectEffective(ctx context.Context, subject SubjectRef, projectID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	pm, ok, err := s.projectMember(ctx, projectID, subject)
	if err != nil {
		return err
	}
	if !ok {
		*denied = append(*denied, "subject is not a project member")
		return nil
	}
	add("project.read", SourceProjectMember, pm.EvidenceRef, pm.Role == "owner")
	add("project.write", SourceProjectMember, pm.EvidenceRef, pm.Role == "owner")
	add("project.member.add", SourceProjectMember, pm.EvidenceRef, pm.Role == "owner")
	add("project.repo_ref.manage", SourceProjectMember, pm.EvidenceRef, pm.Role == "owner")
	if pm.Role == "owner" {
		add("project.member.remove", SourceProjectMember, pm.EvidenceRef, true)
		add("project.stage.manage", SourceProjectMember, pm.EvidenceRef, true)
	}
	return nil
}

type projectMemberRecord struct {
	ID          string
	Role        string
	EvidenceRef string
}

func (s *Service) projectMember(ctx context.Context, projectID string, subject SubjectRef) (projectMemberRecord, bool, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return projectMemberRecord{}, false, err
	}
	if !(subject.IsUser() || subject.IsAgent()) {
		return projectMemberRecord{}, false, nil
	}
	row := exec.QueryRowContext(ctx, `SELECT id, role FROM pm_project_members WHERE project_id = ? AND identity_id = ?`, projectID, subject)
	var m projectMemberRecord
	if err := row.Scan(&m.ID, &m.Role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return projectMemberRecord{}, false, nil
		}
		return projectMemberRecord{}, false, err
	}
	m.EvidenceRef = "pm_project_members:" + m.ID
	return m, true, nil
}

func (s *Service) addTaskEffective(ctx context.Context, subject SubjectRef, taskID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return err
	}
	var projectID, assignee, createdBy string
	if err := exec.QueryRowContext(ctx, `SELECT project_id, COALESCE(assignee, ''), created_by FROM pm_tasks WHERE id = ?`, taskID).Scan(&projectID, &assignee, &createdBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if assignee == string(subject) {
		add("task.read", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
		add("task.complete.self", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
		add("task.block.self", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
	}
	if createdBy == string(subject) {
		add("task.read", SourceProjectMember, "pm_tasks:"+taskID+"/created_by", false)
	}
	return s.addProjectEffective(ctx, subject, projectID, func(key PermissionKey, source DecisionSource, evidence string, delegatable bool) {
		switch key {
		case "project.read":
			add("task.read", source, evidence, delegatable)
		case "project.write":
			add("task.write", source, evidence, delegatable)
		}
	}, denied)
}

func (s *Service) addIssueEffective(ctx context.Context, subject SubjectRef, issueID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	projectID, err := s.parentProject(ctx, "issue", issueID)
	if err != nil {
		return err
	}
	return s.addProjectEffective(ctx, subject, projectID, func(key PermissionKey, source DecisionSource, evidence string, delegatable bool) {
		switch key {
		case "project.read":
			add("issue.read", source, evidence, delegatable)
		case "project.write":
			add("issue.write", source, evidence, delegatable)
		}
	}, denied)
}

func (s *Service) addPlanEffective(ctx context.Context, subject SubjectRef, planID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	projectID, err := s.parentProject(ctx, "plan", planID)
	if err != nil {
		return err
	}
	return s.addProjectEffective(ctx, subject, projectID, func(key PermissionKey, source DecisionSource, evidence string, delegatable bool) {
		switch key {
		case "project.read":
			add("plan.read", source, evidence, delegatable)
		case "project.write":
			add("plan.write", source, evidence, delegatable)
		}
	}, denied)
}

func (s *Service) addTeamEffective(ctx context.Context, subject SubjectRef, teamID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	if subject.IsAgent() {
		exec, err := s.store.exec(ctx)
		if err != nil {
			return err
		}
		rows, err := exec.QueryContext(ctx, `SELECT role FROM team_members WHERE team_id = ? AND member_ref = ?`, teamID, subject)
		if err != nil {
			return err
		}
		defer rows.Close()
		var inTeam bool
		for rows.Next() {
			var role string
			if err := rows.Scan(&role); err != nil {
				return err
			}
			inTeam = true
			evidence := "team_members:" + teamID + "/" + string(subject) + "/" + role
			add("team.memory.read", SourceTeamMember, evidence, false)
			add("team.memory.propose", SourceTeamMember, evidence, false)
			add("team.git.read", SourceTeamMember, evidence, false)
			add("team.git.write", SourceTeamMember, evidence, false)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if inTeam {
			var exists int
			if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_memory_policy_curators WHERE team_id = ? AND agent_ref = ?`, teamID, subject).Scan(&exists); err != nil {
				return err
			}
			if exists > 0 {
				add("team.memory.review", SourceTeamMemoryPolicy, "team_memory_policy_curators:"+teamID+"/"+string(subject), false)
			}
		} else {
			*denied = append(*denied, "agent is not a current team member")
		}
	}
	return nil
}

func (s *Service) addConversationEffective(ctx context.Context, subject SubjectRef, convID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return err
	}
	var participantsJSON string
	if err := exec.QueryRowContext(ctx, `SELECT participants FROM conversations WHERE id = ?`, convID).Scan(&participantsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var participants []struct {
		IdentityID string `json:"identity_id"`
		LeftAt     string `json:"left_at"`
	}
	if err := json.Unmarshal([]byte(participantsJSON), &participants); err != nil {
		return err
	}
	for _, p := range participants {
		if p.IdentityID == string(subject) && p.LeftAt == "" {
			evidence := "conversations:" + convID + "/participants/" + string(subject)
			add("conversation.read", SourceConversationParticipant, evidence, false)
			add("conversation.post", SourceConversationParticipant, evidence, false)
			return nil
		}
	}
	*denied = append(*denied, "subject is not an active conversation participant")
	return nil
}

func (s *Service) addFileEffective(ctx context.Context, subject SubjectRef, r ResourceScope, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	refs := r.Refs
	if len(refs) == 0 && r.URI != "" {
		exec, err := s.store.exec(ctx)
		if err != nil {
			return err
		}
		rows, err := exec.QueryContext(ctx, `SELECT scope, scope_id FROM file_references WHERE file_uri = ? AND deleted_at IS NULL`, r.URI)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref FileRef
			if err := rows.Scan(&ref.Scope, &ref.ScopeID); err != nil {
				return err
			}
			refs = append(refs, ref)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for _, ref := range refs {
		if s.fileRefReachable(ctx, subject, ref) {
			evidence := "file_references:" + ref.Scope + "/" + ref.ScopeID
			add("file.download", SourceFileScope, evidence, false)
			add("file.attach", SourceFileScope, evidence, false)
			add("file.upload", SourceFileScope, evidence, false)
			return nil
		}
	}
	*denied = append(*denied, "no live reachable file reference for subject")
	return nil
}

func (s *Service) fileRefReachable(ctx context.Context, subject SubjectRef, ref FileRef) bool {
	switch ref.Scope {
	case "uploader":
		return ref.ScopeID == string(subject)
	case "conversation":
		exp, _ := s.Explain(ctx, CheckRequest{SubjectRef: subject, Transport: TransportSystem, Permission: "conversation.read", Resource: ResourceScope{Kind: "conversation", ID: ref.ScopeID}})
		return exp.Decision.Allowed
	case "project":
		exp, _ := s.Explain(ctx, CheckRequest{SubjectRef: subject, Transport: TransportSystem, Permission: "project.read", Resource: ResourceScope{Kind: "project", ID: ref.ScopeID}})
		return exp.Decision.Allowed
	case "task":
		exp, _ := s.Explain(ctx, CheckRequest{SubjectRef: subject, Transport: TransportSystem, Permission: "task.read", Resource: ResourceScope{Kind: "task", ID: ref.ScopeID}})
		return exp.Decision.Allowed
	case "issue":
		exp, _ := s.Explain(ctx, CheckRequest{SubjectRef: subject, Transport: TransportSystem, Permission: "issue.read", Resource: ResourceScope{Kind: "issue", ID: ref.ScopeID}})
		return exp.Decision.Allowed
	default:
		return false
	}
}

func (s *Service) addAgentEffective(ctx context.Context, req CheckRequest, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return err
	}
	var workerID, identityMemberID string
	if err := exec.QueryRowContext(ctx, `SELECT worker_id, COALESCE(identity_member_id, '') FROM agents WHERE id = ?`, req.Resource.ID).Scan(&workerID, &identityMemberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if req.SubjectRef.IsWorker() {
		if req.SubjectRef.BareID() == workerID {
			add("agent.operate.self", SourceAgentWorkerBinding, "agents:"+req.Resource.ID+"/worker_id", false)
		} else {
			*denied = append(*denied, "worker token owner does not match agent worker binding")
		}
	}
	if req.SubjectRef.IsAgent() && identityMemberID != "" && req.SubjectRef.BareID() == identityMemberID {
		add("git.agent.read.self", SourceAgentWorkerBinding, "agents:"+req.Resource.ID+"/identity_member_id", false)
		add("git.agent.write.self", SourceAgentWorkerBinding, "agents:"+req.Resource.ID+"/identity_member_id", false)
	}
	return nil
}

func (s *Service) addWorkerEffective(req CheckRequest, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	if !req.SubjectRef.IsWorker() {
		return nil
	}
	if req.Resource.ID == "" || req.SubjectRef.BareID() != req.Resource.ID {
		*denied = append(*denied, "worker token owner does not match worker resource")
		return nil
	}
	add("worker.heartbeat", SourceWorkerOwner, "admin_tokens.owner:"+string(req.SubjectRef), false)
	add("worker.capability.report", SourceWorkerOwner, "admin_tokens.owner:"+string(req.SubjectRef), false)
	return nil
}

func (s *Service) addCustomEffective(ctx context.Context, req CheckRequest, out *[]EffectivePermission) error {
	kind, id := req.Resource.Key()
	if kind == "" || id == "" || req.Resource.OrgID == "" {
		return nil
	}
	assignments, err := s.store.activeAssignmentsFor(ctx, req.Resource.OrgID, req.SubjectRef, kind, id)
	if err != nil {
		return err
	}
	for _, a := range assignments {
		if a.ExpiresAt != nil && !a.ExpiresAt.After(s.clock.Now()) {
			continue
		}
		perms, err := s.store.rolePermissions(ctx, a.RoleID)
		if err != nil {
			return err
		}
		for _, p := range perms {
			if p.ResourceKind != kind {
				continue
			}
			*out = append(*out, EffectivePermission{
				Key:          p.PermissionKey,
				Source:       SourceCustomRole,
				EvidenceRef:  "authorization_role_assignments:" + a.ID,
				Delegatable:  p.Delegatable,
				RoleID:       a.RoleID,
				AssignmentID: a.ID,
				ExpiresAt:    a.ExpiresAt,
			})
		}
	}
	return nil
}

func (s *Service) resolveResource(ctx context.Context, r ResourceScope) (ResourceScope, []string, error) {
	r.Kind = strings.TrimSpace(r.Kind)
	r.ID = strings.TrimSpace(r.ID)
	r.OrgID = strings.TrimSpace(r.OrgID)
	r.ProjectID = strings.TrimSpace(r.ProjectID)
	r.URI = strings.TrimSpace(r.URI)
	if r.Kind == "" {
		return r, []string{"resource.kind required"}, ErrInvalid
	}
	switch r.Kind {
	case "org":
		if r.ID == "" {
			r.ID = r.OrgID
		}
		if r.ID == "" {
			return r, []string{"org id required"}, ErrInvalid
		}
		if err := s.ensureOrg(ctx, r.ID); err != nil {
			return r, []string{"org not found"}, err
		}
		r.OrgID = r.ID
	case "project":
		orgID, err := s.projectOrg(ctx, r.ID)
		if err != nil {
			return r, []string{"project not found"}, err
		}
		if r.OrgID != "" && r.OrgID != orgID {
			return r, []string{"project belongs to another org"}, ErrNotFound
		}
		r.OrgID = orgID
	case "task", "issue", "plan":
		if r.ID == "*" {
			return r, nil, nil
		}
		projectID, err := s.parentProject(ctx, r.Kind, r.ID)
		if err != nil {
			return r, []string{r.Kind + " not found"}, err
		}
		orgID, err := s.projectOrg(ctx, projectID)
		if err != nil {
			return r, []string{"parent project not found"}, err
		}
		if r.OrgID != "" && r.OrgID != orgID {
			return r, []string{r.Kind + " belongs to another org"}, ErrNotFound
		}
		r.ProjectID, r.OrgID = projectID, orgID
	case "team":
		orgID, err := s.teamOrg(ctx, r.ID)
		if err != nil {
			return r, []string{"team not found"}, err
		}
		if r.OrgID != "" && r.OrgID != orgID {
			return r, []string{"team belongs to another org"}, ErrNotFound
		}
		r.OrgID = orgID
	case "conversation":
		orgID, err := s.conversationOrg(ctx, r.ID)
		if err != nil {
			return r, []string{"conversation not found"}, err
		}
		if r.OrgID != "" && r.OrgID != orgID {
			return r, []string{"conversation belongs to another org"}, ErrNotFound
		}
		r.OrgID = orgID
	case "file":
		if r.URI == "" {
			r.URI = r.ID
		}
		if r.URI == "" {
			return r, []string{"file uri required"}, ErrInvalid
		}
	case "agent":
		orgID, memberID, err := s.agentOrg(ctx, r.ID)
		if err != nil {
			return r, []string{"agent not found"}, err
		}
		if r.OrgID != "" && r.OrgID != orgID {
			return r, []string{"agent belongs to another org"}, ErrNotFound
		}
		r.OrgID = orgID
		r.IdentityMemberID = memberID
	case "worker", "admin_token", "secret", "blob", "git":
		if r.ID == "" {
			r.ID = "*"
		}
	default:
		return r, []string{"unsupported resource kind"}, ErrInvalid
	}
	return r, nil, nil
}

func (s *Service) ensureOrg(ctx context.Context, orgID string) error {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return err
	}
	var found int
	if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations WHERE id = ? AND deleted_at IS NULL`, orgID).Scan(&found); err != nil {
		return err
	}
	if found == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) projectOrg(ctx context.Context, projectID string) (string, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return "", err
	}
	var orgID string
	if err := exec.QueryRowContext(ctx, `SELECT organization_id FROM pm_projects WHERE id = ?`, projectID).Scan(&orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return orgID, nil
}

func (s *Service) parentProject(ctx context.Context, kind, id string) (string, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return "", err
	}
	table := map[string]string{"task": "pm_tasks", "issue": "pm_issues", "plan": "pm_plans"}[kind]
	if table == "" {
		return "", ErrInvalid
	}
	var projectID string
	if err := exec.QueryRowContext(ctx, fmt.Sprintf(`SELECT project_id FROM %s WHERE id = ?`, table), id).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return projectID, nil
}

func (s *Service) teamOrg(ctx context.Context, teamID string) (string, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return "", err
	}
	var orgID string
	if err := exec.QueryRowContext(ctx, `SELECT org_id FROM teams WHERE id = ?`, teamID).Scan(&orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return orgID, nil
}

func (s *Service) conversationOrg(ctx context.Context, convID string) (string, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return "", err
	}
	var orgID string
	if err := exec.QueryRowContext(ctx, `SELECT organization_id FROM conversations WHERE id = ?`, convID).Scan(&orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return orgID, nil
}

func (s *Service) agentOrg(ctx context.Context, agentID string) (string, string, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return "", "", err
	}
	var orgID, memberID string
	if err := exec.QueryRowContext(ctx, `SELECT organization_id, COALESCE(identity_member_id, '') FROM agents WHERE id = ?`, agentID).Scan(&orgID, &memberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return orgID, memberID, nil
}

func (s *Service) audit(ctx context.Context, e auditEvent) error {
	if e.ID == "" {
		e.ID = s.gen.NewULID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = s.clock.Now()
	}
	if err := s.store.appendAudit(ctx, e); err != nil {
		return err
	}
	if s.sink != nil {
		refs := observability.EventRefs{OrganizationID: e.ResourceID}
		if e.ResourceKind == "team" {
			refs.TeamID = e.ResourceID
		}
		if e.ResourceKind == "project" {
			refs.ProjectID = e.ResourceID
		}
		payload := e.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		payload["subject_ref"] = string(e.SubjectRef)
		payload["permission_key"] = string(e.PermissionKey)
		payload["resource_kind"] = e.ResourceKind
		payload["role_id"] = e.RoleID
		payload["assignment_id"] = e.AssignmentID
		_, err := s.sink.Emit(ctx, observability.EmitCommand{
			EventType: observability.EventType(e.EventType),
			Refs:      refs,
			Actor:     observability.Actor(e.ActorRef),
			Payload:   payload,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func batchDigest(req BatchRequest) (string, error) {
	cp := req
	cp.IdempotencyKey = ""
	b, err := json.Marshal(cp)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
