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
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
)

type accessGraphCandidate struct {
	Resource ResourceScope
}

type accessGraphTokenGrant struct {
	TokenID     string
	BearerScope string
	Permission  PermissionKey
	Resource    ResourceScope
}

// ListAccessGraph builds the read-only governance projection described by
// ADR-0058: subject -> relationship/source -> scope -> effective permission.
// It never writes authorization state and never treats runtime capabilities as
// grants. The graph is a projection over existing authority tables, with an
// optional shadow pass back through Explain so callers can observe parity drift.
func (s *Service) ListAccessGraph(ctx context.Context, req AccessGraphRequest) (AccessGraphPage, error) {
	if s == nil || s.db == nil || s.store == nil {
		return AccessGraphPage{}, errors.New("authorization service: nil db")
	}
	req.SubjectRef = SubjectRef(strings.TrimSpace(string(req.SubjectRef)))
	req.OrgID = strings.TrimSpace(req.OrgID)
	if req.OrgID == "" {
		return AccessGraphPage{}, fmt.Errorf("%w: org_id required", ErrInvalid)
	}
	if err := req.SubjectRef.Validate(); err != nil {
		return AccessGraphPage{}, err
	}
	if err := s.ensureOrg(ctx, req.OrgID); err != nil {
		return AccessGraphPage{}, err
	}
	visible, err := s.SubjectVisibleInOrg(ctx, req.SubjectRef, req.OrgID)
	if err != nil {
		return AccessGraphPage{}, err
	}
	if !visible {
		return AccessGraphPage{}, ErrNotFound
	}
	layer := normalizeAccessGraphLayer(req.Layer)
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := 0
	if strings.TrimSpace(req.Cursor) != "" {
		n, err := strconv.Atoi(req.Cursor)
		if err != nil || n < 0 {
			return AccessGraphPage{}, fmt.Errorf("%w: invalid cursor", ErrInvalid)
		}
		offset = n
	}

	candidates, err := s.accessGraphCandidates(ctx, req.SubjectRef, req.OrgID)
	if err != nil {
		return AccessGraphPage{}, err
	}
	builder := newAccessGraphBuilder(req.SubjectRef, req.OrgID, req.RedactEvidence)
	for _, c := range candidates {
		eff, err := s.ListEffective(ctx, req.SubjectRef, c.Resource)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return AccessGraphPage{}, err
		}
		for _, p := range eff.Permissions {
			builder.addPermission(eff.Resource, p)
		}
	}

	tokenGrants, risk, err := s.accessGraphTokenGrantsAndRisk(ctx, req.SubjectRef, req.OrgID, req.RedactEvidence)
	if err != nil {
		return AccessGraphPage{}, err
	}
	for _, g := range tokenGrants {
		builder.addPermission(g.Resource, EffectivePermission{
			Key:         g.Permission,
			Source:      SourceAdminTokenScope,
			EvidenceRef: "admin_tokens:" + g.TokenID + "/scope:" + g.BearerScope,
		})
	}
	risk.SubjectRef = req.SubjectRef
	relationships, scopes, permissions := builder.finish()

	parity := AccessGraphParityShadow{Complete: true}
	if req.ShadowParity {
		parity = s.accessGraphParityShadow(ctx, req, permissions)
	}

	page := AccessGraphPage{
		SubjectRef:   req.SubjectRef,
		OrgID:        req.OrgID,
		Layer:        layer,
		Limit:        limit,
		Cursor:       req.Cursor,
		Complete:     true,
		RiskSummary:  risk,
		ParityShadow: parity,
	}
	page.Relationships, page.Scopes, page.Permissions, page.Completeness, page.NextCursor = paginateAccessGraph(layer, relationships, scopes, permissions, offset, limit)
	page.Complete = !page.Completeness.HasMore
	return page, nil
}

func normalizeAccessGraphLayer(layer string) string {
	switch strings.TrimSpace(strings.ToLower(layer)) {
	case "relationship", "relationships", "source", "sources":
		return "relationships"
	case "scope", "scopes":
		return "scopes"
	case "permission", "permissions", "":
		return "permissions"
	default:
		return "permissions"
	}
}

func paginateAccessGraph(layer string, relationships []AccessGraphRelationship, scopes []AccessGraphScope, permissions []AccessGraphPermission, offset, limit int) ([]AccessGraphRelationship, []AccessGraphScope, []AccessGraphPermission, AccessGraphCompleteness, string) {
	switch layer {
	case "relationships":
		total := len(relationships)
		end := minInt(offset+limit, total)
		if offset > total {
			offset, end = total, total
		}
		pageRelationships := relationships[offset:end]
		next := ""
		if end < total {
			next = strconv.Itoa(end)
		}
		return pageRelationships, nil, nil, AccessGraphCompleteness{
			RequestedLayer: layer,
			Returned:       len(pageRelationships),
			Total:          total,
			HasMore:        end < total,
		}, next
	case "scopes":
		total := len(scopes)
		end := minInt(offset+limit, total)
		if offset > total {
			offset, end = total, total
		}
		pageScopes := scopes[offset:end]
		relIDs := map[string]struct{}{}
		for _, sc := range pageScopes {
			relIDs[sc.RelationshipID] = struct{}{}
		}
		pageRelationships := filterRelationships(relationships, relIDs)
		next := ""
		if end < total {
			next = strconv.Itoa(end)
		}
		return pageRelationships, pageScopes, nil, AccessGraphCompleteness{
			RequestedLayer: layer,
			Returned:       len(pageScopes),
			Total:          total,
			HasMore:        end < total,
		}, next
	default:
		total := len(permissions)
		end := minInt(offset+limit, total)
		if offset > total {
			offset, end = total, total
		}
		pagePermissions := permissions[offset:end]
		relIDs := map[string]struct{}{}
		scopeIDs := map[string]struct{}{}
		for _, p := range pagePermissions {
			relIDs[p.RelationshipID] = struct{}{}
			scopeIDs[p.ScopeID] = struct{}{}
		}
		pageRelationships := filterRelationships(relationships, relIDs)
		pageScopes := filterScopes(scopes, scopeIDs)
		next := ""
		if end < total {
			next = strconv.Itoa(end)
		}
		return pageRelationships, pageScopes, pagePermissions, AccessGraphCompleteness{
			RequestedLayer: layer,
			Returned:       len(pagePermissions),
			Total:          total,
			HasMore:        end < total,
		}, next
	}
}

func filterRelationships(in []AccessGraphRelationship, ids map[string]struct{}) []AccessGraphRelationship {
	out := make([]AccessGraphRelationship, 0, len(ids))
	for _, r := range in {
		if _, ok := ids[r.ID]; ok {
			out = append(out, r)
		}
	}
	return out
}

func filterScopes(in []AccessGraphScope, ids map[string]struct{}) []AccessGraphScope {
	out := make([]AccessGraphScope, 0, len(ids))
	for _, s := range in {
		if _, ok := ids[s.ID]; ok {
			out = append(out, s)
		}
	}
	return out
}

type accessGraphBuilder struct {
	subject       SubjectRef
	orgID         string
	redact        bool
	relationships map[string]*AccessGraphRelationship
	scopes        map[string]*AccessGraphScope
	permissions   map[string]*AccessGraphPermission
}

func newAccessGraphBuilder(subject SubjectRef, orgID string, redact bool) *accessGraphBuilder {
	return &accessGraphBuilder{
		subject:       subject,
		orgID:         orgID,
		redact:        redact,
		relationships: map[string]*AccessGraphRelationship{},
		scopes:        map[string]*AccessGraphScope{},
		permissions:   map[string]*AccessGraphPermission{},
	}
}

func (b *accessGraphBuilder) addPermission(resource ResourceScope, p EffectivePermission) {
	if p.Key == "" || p.Source == "" {
		return
	}
	ev := accessGraphEvidence(p.Source, p.EvidenceRef, b.redact)
	relID := "rel:" + accessGraphHash(string(b.subject)+"|"+string(p.Source)+"|"+p.EvidenceRef)
	scopeKind, scopeID := resource.Key()
	scopeKey := scopeKind + ":" + scopeID
	scopeGraphID := "scope:" + accessGraphHash(relID+"|"+scopeKey)
	permID := "perm:" + accessGraphHash(scopeGraphID+"|"+string(p.Key)+"|"+string(p.Source)+"|"+p.EvidenceRef)
	if _, ok := b.relationships[relID]; !ok {
		b.relationships[relID] = &AccessGraphRelationship{
			ID:         relID,
			SubjectRef: b.subject,
			Source:     p.Source,
			Evidence:   ev,
		}
	}
	if _, ok := b.scopes[scopeGraphID]; !ok {
		b.scopes[scopeGraphID] = &AccessGraphScope{
			ID:             scopeGraphID,
			RelationshipID: relID,
			Resource:       resource,
			Source:         p.Source,
			Evidence:       ev,
		}
		b.relationships[relID].ScopeCount++
	}
	if _, ok := b.permissions[permID]; ok {
		return
	}
	b.permissions[permID] = &AccessGraphPermission{
		ID:             permID,
		RelationshipID: relID,
		ScopeID:        scopeGraphID,
		Resource:       resource,
		Key:            p.Key,
		Source:         p.Source,
		Evidence:       ev,
		Delegatable:    p.Delegatable,
		RoleID:         p.RoleID,
		AssignmentID:   p.AssignmentID,
	}
	b.relationships[relID].PermissionCount++
	b.scopes[scopeGraphID].PermissionCount++
}

func (b *accessGraphBuilder) finish() ([]AccessGraphRelationship, []AccessGraphScope, []AccessGraphPermission) {
	relationships := make([]AccessGraphRelationship, 0, len(b.relationships))
	for _, r := range b.relationships {
		relationships = append(relationships, *r)
	}
	sort.Slice(relationships, func(i, j int) bool {
		if relationships[i].Source == relationships[j].Source {
			return relationships[i].ID < relationships[j].ID
		}
		return relationships[i].Source < relationships[j].Source
	})

	scopes := make([]AccessGraphScope, 0, len(b.scopes))
	for _, s := range b.scopes {
		scopes = append(scopes, *s)
	}
	sort.Slice(scopes, func(i, j int) bool {
		ki, ii := scopes[i].Resource.Key()
		kj, ij := scopes[j].Resource.Key()
		if ki == kj {
			if ii == ij {
				return scopes[i].RelationshipID < scopes[j].RelationshipID
			}
			return ii < ij
		}
		return ki < kj
	})

	permissions := make([]AccessGraphPermission, 0, len(b.permissions))
	for _, p := range b.permissions {
		permissions = append(permissions, *p)
	}
	sort.Slice(permissions, func(i, j int) bool {
		ki, ii := permissions[i].Resource.Key()
		kj, ij := permissions[j].Resource.Key()
		if ki != kj {
			return ki < kj
		}
		if ii != ij {
			return ii < ij
		}
		if permissions[i].Key != permissions[j].Key {
			return permissions[i].Key < permissions[j].Key
		}
		return permissions[i].ID < permissions[j].ID
	})
	return relationships, scopes, permissions
}

func (s *Service) accessGraphCandidates(ctx context.Context, subject SubjectRef, orgID string) ([]accessGraphCandidate, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := []accessGraphCandidate{}
	add := func(r ResourceScope) {
		if r.Kind == "" {
			return
		}
		if r.OrgID == "" && r.Kind != "worker" && r.Kind != "admin_token" {
			r.OrgID = orgID
		}
		kind, id := r.Key()
		if id == "" {
			return
		}
		key := kind + "\x00" + id + "\x00" + r.OrgID + "\x00" + r.ProjectID
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, accessGraphCandidate{Resource: r})
	}

	if _, ok, err := s.orgMember(ctx, orgID, subject); err != nil {
		return nil, err
	} else if ok {
		add(ResourceScope{Kind: "org", ID: orgID, OrgID: orgID})
	}

	projectRows, err := exec.QueryContext(ctx, `SELECT p.id
		FROM pm_projects p
		JOIN pm_project_members m ON m.project_id = p.id
		WHERE p.organization_id = ? AND m.identity_id = ?`,
		orgID, string(subject))
	if err != nil {
		return nil, err
	}
	projectIDs := []string{}
	for projectRows.Next() {
		var id string
		if err := projectRows.Scan(&id); err != nil {
			_ = projectRows.Close()
			return nil, err
		}
		projectIDs = append(projectIDs, id)
		add(ResourceScope{Kind: "project", ID: id, OrgID: orgID})
	}
	if err := projectRows.Close(); err != nil {
		return nil, err
	}
	if err := projectRows.Err(); err != nil {
		return nil, err
	}
	for _, projectID := range projectIDs {
		if err := s.addProjectChildCandidates(ctx, exec, add, orgID, projectID); err != nil {
			return nil, err
		}
	}

	ownTaskRows, err := exec.QueryContext(ctx, `SELECT t.id, t.project_id
		FROM pm_tasks t
		JOIN pm_projects p ON p.id = t.project_id
		WHERE p.organization_id = ? AND (t.assignee = ? OR t.created_by = ?)`,
		orgID, string(subject), string(subject))
	if err != nil {
		return nil, err
	}
	for ownTaskRows.Next() {
		var id, projectID string
		if err := ownTaskRows.Scan(&id, &projectID); err != nil {
			_ = ownTaskRows.Close()
			return nil, err
		}
		add(ResourceScope{Kind: "task", ID: id, ProjectID: projectID, OrgID: orgID})
	}
	if err := ownTaskRows.Close(); err != nil {
		return nil, err
	}
	if err := ownTaskRows.Err(); err != nil {
		return nil, err
	}

	teamRows, err := exec.QueryContext(ctx, `SELECT id FROM teams WHERE org_id = ?`, orgID)
	if err != nil {
		return nil, err
	}
	for teamRows.Next() {
		var id string
		if err := teamRows.Scan(&id); err != nil {
			_ = teamRows.Close()
			return nil, err
		}
		add(ResourceScope{Kind: "team", ID: id, OrgID: orgID})
	}
	if err := teamRows.Close(); err != nil {
		return nil, err
	}
	if err := teamRows.Err(); err != nil {
		return nil, err
	}

	convRows, err := exec.QueryContext(ctx, `SELECT id, participants FROM conversations WHERE organization_id = ?`, orgID)
	if err != nil {
		return nil, err
	}
	for convRows.Next() {
		var id, participantsJSON string
		if err := convRows.Scan(&id, &participantsJSON); err != nil {
			_ = convRows.Close()
			return nil, err
		}
		if conversationHasActiveParticipant(participantsJSON, subject) {
			add(ResourceScope{Kind: "conversation", ID: id, OrgID: orgID})
		}
	}
	if err := convRows.Close(); err != nil {
		return nil, err
	}
	if err := convRows.Err(); err != nil {
		return nil, err
	}

	if subject.IsWorker() {
		if ok, err := s.workerInOrg(ctx, subject.BareID(), orgID); err != nil {
			return nil, err
		} else if ok {
			add(ResourceScope{Kind: "worker", ID: subject.BareID(), OrgID: orgID})
		}
		agentRows, err := exec.QueryContext(ctx, `SELECT id, COALESCE(identity_member_id, '') FROM agents WHERE organization_id = ? AND worker_id = ?`,
			orgID, subject.BareID())
		if err != nil {
			return nil, err
		}
		for agentRows.Next() {
			var id, memberID string
			if err := agentRows.Scan(&id, &memberID); err != nil {
				_ = agentRows.Close()
				return nil, err
			}
			add(ResourceScope{Kind: "agent", ID: id, OrgID: orgID, IdentityMemberID: memberID})
		}
		if err := agentRows.Close(); err != nil {
			return nil, err
		}
		if err := agentRows.Err(); err != nil {
			return nil, err
		}
	}
	if subject.IsAgent() {
		agentRows, err := exec.QueryContext(ctx, `SELECT id, COALESCE(identity_member_id, '') FROM agents WHERE organization_id = ? AND identity_member_id = ?`,
			orgID, subject.BareID())
		if err != nil {
			return nil, err
		}
		for agentRows.Next() {
			var id, memberID string
			if err := agentRows.Scan(&id, &memberID); err != nil {
				_ = agentRows.Close()
				return nil, err
			}
			add(ResourceScope{Kind: "agent", ID: id, OrgID: orgID, IdentityMemberID: memberID})
		}
		if err := agentRows.Close(); err != nil {
			return nil, err
		}
		if err := agentRows.Err(); err != nil {
			return nil, err
		}
	}

	assignmentRows, err := exec.QueryContext(ctx, `SELECT resource_kind, resource_id
		FROM authorization_role_assignments
		WHERE org_id = ? AND subject_ref = ? AND revoked_at IS NULL`,
		orgID, string(subject))
	if err != nil {
		return nil, err
	}
	for assignmentRows.Next() {
		var kind, id string
		if err := assignmentRows.Scan(&kind, &id); err != nil {
			_ = assignmentRows.Close()
			return nil, err
		}
		add(ResourceScope{Kind: kind, ID: id, OrgID: orgID})
	}
	if err := assignmentRows.Close(); err != nil {
		return nil, err
	}
	if err := assignmentRows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (s *Service) addProjectChildCandidates(ctx context.Context, exec interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, add func(ResourceScope), orgID, projectID string) error {
	for _, spec := range []struct {
		Kind  string
		Query string
	}{
		{"task", `SELECT id FROM pm_tasks WHERE project_id = ?`},
		{"issue", `SELECT id FROM pm_issues WHERE project_id = ?`},
		{"plan", `SELECT id FROM pm_plans WHERE project_id = ?`},
	} {
		rows, err := exec.QueryContext(ctx, spec.Query, projectID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			add(ResourceScope{Kind: spec.Kind, ID: id, ProjectID: projectID, OrgID: orgID})
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SubjectVisibleInOrg(ctx context.Context, subject SubjectRef, orgID string) (bool, error) {
	subject = SubjectRef(strings.TrimSpace(string(subject)))
	orgID = strings.TrimSpace(orgID)
	if subject == "" || orgID == "" {
		return false, fmt.Errorf("%w: subject_ref and org_id required", ErrInvalid)
	}
	if err := subject.Validate(); err != nil {
		return false, err
	}
	if subject == "system" {
		return true, nil
	}
	if subject.IsUser() || subject.IsAgent() {
		_, ok, err := s.orgMember(ctx, orgID, subject)
		return ok, err
	}
	if subject.IsWorker() {
		return s.workerInOrg(ctx, subject.BareID(), orgID)
	}
	return false, nil
}

func (s *Service) ResourceVisibleInOrg(ctx context.Context, resource ResourceScope, orgID string) (bool, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return false, fmt.Errorf("%w: org_id required", ErrInvalid)
	}
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.ID = strings.TrimSpace(resource.ID)
	resource.OrgID = strings.TrimSpace(resource.OrgID)
	if resource.Kind == "" {
		return false, fmt.Errorf("%w: resource.kind required", ErrInvalid)
	}
	switch resource.Kind {
	case "org":
		id := resource.ID
		if id == "" {
			id = resource.OrgID
		}
		return id == "" || id == orgID, nil
	case "worker":
		if resource.ID == "" || resource.ID == "*" {
			return true, nil
		}
		return s.workerInOrg(ctx, resource.ID, orgID)
	case "admin_token", "secret", "blob", "git":
		return true, nil
	default:
		if resource.OrgID != "" && resource.OrgID != orgID {
			return false, nil
		}
		resource.OrgID = orgID
		resolved, _, err := s.resolveResource(ctx, resource)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		return resolved.OrgID == "" || resolved.OrgID == orgID, nil
	}
}

func (s *Service) workerInOrg(ctx context.Context, workerID, orgID string) (bool, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return false, err
	}
	var found int
	if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers WHERE id = ? AND organization_id = ?`, workerID, orgID).Scan(&found); err != nil {
		return false, err
	}
	return found > 0, nil
}

func conversationHasActiveParticipant(participantsJSON string, subject SubjectRef) bool {
	var participants []struct {
		IdentityID string `json:"identity_id"`
		LeftAt     string `json:"left_at"`
	}
	if err := json.Unmarshal([]byte(participantsJSON), &participants); err != nil {
		return false
	}
	for _, p := range participants {
		if p.IdentityID == string(subject) && strings.TrimSpace(p.LeftAt) == "" {
			return true
		}
	}
	return false
}

func (s *Service) accessGraphTokenGrantsAndRisk(ctx context.Context, subject SubjectRef, orgID string, redact bool) ([]accessGraphTokenGrant, AccessGraphRiskSummary, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return nil, AccessGraphRiskSummary{}, err
	}
	owners := accessGraphTokenOwners(subject)
	workerID := ""
	if subject.IsWorker() {
		workerID = subject.BareID()
		owners = append(owners, "enroll:worker:"+workerID)
	}
	if len(owners) == 0 {
		return nil, AccessGraphRiskSummary{}, nil
	}
	if subject.IsWorker() {
		ok, err := s.workerInOrg(ctx, subject.BareID(), orgID)
		if err != nil {
			return nil, AccessGraphRiskSummary{}, err
		}
		if !ok {
			return nil, AccessGraphRiskSummary{}, nil
		}
	}
	args := make([]any, 0, len(owners)+1)
	placeholders := make([]string, 0, len(owners))
	for _, owner := range owners {
		if strings.TrimSpace(owner) == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, owner)
	}
	if len(placeholders) == 0 {
		return nil, AccessGraphRiskSummary{}, nil
	}
	query := `SELECT id, owner, scopes_json, COALESCE(last_used_at, ''), COALESCE(expires_at, ''),
		is_enroll, COALESCE(worker_id, ''), COALESCE(used_at, '')
		FROM admin_tokens
		WHERE revoked_at IS NULL AND owner IN (` + strings.Join(placeholders, ",") + `)`
	if workerID != "" {
		query += ` OR (revoked_at IS NULL AND worker_id = ?)`
		args = append(args, workerID)
	}
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, AccessGraphRiskSummary{}, err
	}
	defer rows.Close()
	var grants []accessGraphTokenGrant
	risk := AccessGraphRiskSummary{SubjectRef: subject}
	now := s.clock.Now().UTC()
	for rows.Next() {
		var id, owner, scopesJSON, lastUsed, expiresAt, tokenWorkerID, usedAt string
		var isEnroll int
		if err := rows.Scan(&id, &owner, &scopesJSON, &lastUsed, &expiresAt, &isEnroll, &tokenWorkerID, &usedAt); err != nil {
			return nil, AccessGraphRiskSummary{}, err
		}
		var scopes []string
		_ = json.Unmarshal([]byte(scopesJSON), &scopes)
		active := strings.TrimSpace(usedAt) == ""
		if expiresAt != "" {
			if exp, err := time.Parse(time.RFC3339Nano, expiresAt); err == nil && exp.Before(now) {
				active = false
			}
		}
		if !active {
			risk.InactiveAdminTokens++
			continue
		}
		risk.ActiveAdminTokens++
		if isEnroll == 1 || strings.HasPrefix(owner, "enroll:worker:") {
			risk.ActiveEnrollTokens++
		}
		if strings.HasPrefix(owner, "worker:") {
			risk.ActiveWorkerTokens++
		}
		ev := accessGraphEvidence(SourceAdminTokenScope, "admin_tokens:"+id, redact)
		for _, scope := range scopes {
			scope = strings.TrimSpace(scope)
			if scope == "" {
				continue
			}
			if scope == "*" || strings.HasSuffix(scope, ":*") {
				risk.WildcardAdminTokens++
				risk.Findings = append(risk.Findings, AccessGraphRiskFinding{
					Severity: "high",
					Kind:     "broad_admin_token_scope",
					Message:  "active admin token carries a wildcard bearer scope",
					Evidence: ev,
				})
			}
			if scope == "*" {
				for _, def := range Definitions() {
					if !accessGraphBearerDefinition(def) {
						continue
					}
					if len(def.ResourceKinds) == 0 {
						continue
					}
					resource := accessGraphBearerResource(def.Key, def.ResourceKinds[0], subject, tokenWorkerID)
					grants = append(grants, accessGraphTokenGrant{TokenID: id, BearerScope: scope, Permission: def.Key, Resource: resource})
				}
				continue
			}
			permission, ok := PermissionForBearerScope(scope)
			if !ok {
				risk.Findings = append(risk.Findings, AccessGraphRiskFinding{
					Severity: "medium",
					Kind:     "unmapped_admin_token_scope",
					Message:  "active admin token carries a bearer scope without a PermissionKey mapping",
					Evidence: ev,
				})
				continue
			}
			def, ok := Definition(permission)
			resourceKind := "admin_token"
			if ok && len(def.ResourceKinds) > 0 {
				resourceKind = def.ResourceKinds[0]
			}
			grants = append(grants, accessGraphTokenGrant{
				TokenID:     id,
				BearerScope: scope,
				Permission:  permission,
				Resource:    accessGraphBearerResource(permission, resourceKind, subject, tokenWorkerID),
			})
		}
		if strings.TrimSpace(lastUsed) == "" && isEnroll == 0 {
			risk.Findings = append(risk.Findings, AccessGraphRiskFinding{
				Severity: "medium",
				Kind:     "unused_admin_token",
				Message:  "active long-term admin token has no last_used_at",
				Evidence: ev,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, AccessGraphRiskSummary{}, err
	}
	if subject.IsWorker() {
		if err := s.addWorkerRisk(ctx, exec, subject.BareID(), orgID, redact, &risk); err != nil {
			return nil, AccessGraphRiskSummary{}, err
		}
	}
	return grants, risk, nil
}

func accessGraphBearerDefinition(def PermissionDefinition) bool {
	for _, source := range def.LegacySources {
		if strings.HasPrefix(source, "admin_tokens.") {
			return true
		}
	}
	return false
}

func accessGraphTokenOwners(subject SubjectRef) []string {
	switch {
	case subject.IsWorker():
		return []string{string(subject)}
	case subject.IsUser():
		return []string{string(subject), subject.BareID()}
	case subject.IsAgent():
		return []string{string(subject), subject.BareID()}
	default:
		return []string{string(subject)}
	}
}

func accessGraphBearerResource(permission PermissionKey, resourceKind string, subject SubjectRef, tokenWorkerID string) ResourceScope {
	r := ResourceScope{Kind: resourceKind, ID: "*"}
	if resourceKind == "worker" {
		workerID := tokenWorkerID
		if workerID == "" && subject.IsWorker() {
			workerID = subject.BareID()
		}
		if workerID != "" {
			r.ID = workerID
		}
	}
	if permission == "admin_token.manage" {
		r.Kind, r.ID = "admin_token", "*"
	}
	return r
}

func (s *Service) addWorkerRisk(ctx context.Context, exec interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, workerID, orgID string, redact bool, risk *AccessGraphRiskSummary) error {
	var status, lastHeartbeat, capsJSON string
	if err := exec.QueryRowContext(ctx, `SELECT status, COALESCE(last_heartbeat_at, ''), capabilities_json FROM workers WHERE id = ? AND organization_id = ?`,
		workerID, orgID).Scan(&status, &lastHeartbeat, &capsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	risk.WorkerStatus = status
	risk.WorkerLastHeartbeatAt = lastHeartbeat
	if status != "online" {
		risk.Findings = append(risk.Findings, AccessGraphRiskFinding{
			Severity: "medium",
			Kind:     "worker_offline",
			Message:  "worker is not online",
			Evidence: accessGraphEvidence(SourceWorkerOwner, "workers:"+workerID, redact),
		})
	}
	var caps []struct {
		AgentCLI string `json:"agent_cli"`
		Detected bool   `json:"detected"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(capsJSON), &caps); err == nil {
		for _, cap := range caps {
			if cap.Detected && !cap.Enabled {
				risk.WorkerDisabledCapabilities++
			}
		}
	}
	if risk.WorkerDisabledCapabilities > 0 {
		risk.Findings = append(risk.Findings, AccessGraphRiskFinding{
			Severity: "low",
			Kind:     "worker_disabled_capabilities",
			Message:  "worker has detected but disabled runtime capabilities",
			Evidence: accessGraphEvidence(SourceWorkerOwner, "workers:"+workerID+"/capabilities", redact),
		})
	}
	return nil
}

func (s *Service) accessGraphParityShadow(ctx context.Context, req AccessGraphRequest, permissions []AccessGraphPermission) AccessGraphParityShadow {
	out := AccessGraphParityShadow{Complete: true}
	for _, p := range permissions {
		check := CheckRequest{
			SubjectRef: req.SubjectRef,
			Transport:  TransportSystem,
			Permission: p.Key,
			Resource:   p.Resource,
			RequestID:  req.RequestID,
		}
		if p.Source == SourceAdminTokenScope {
			check.Transport = TransportAdminHTTP
			check.BearerScope = bearerScopeFromEvidence(p.Evidence)
		}
		exp, err := s.Explain(ctx, check)
		out.Checked++
		actual := err == nil && exp.Decision.Allowed
		if !actual {
			out.Mismatches++
			finding := AccessGraphParityFinding{
				Permission: p.Key,
				Resource:   p.Resource,
				Source:     p.Source,
				Evidence:   p.Evidence,
				Expected:   true,
				Actual:     actual,
			}
			if exp.Decision.Reason != "" {
				finding.Reason = exp.Decision.Reason
			} else if err != nil {
				finding.Reason = err.Error()
			}
			out.Findings = append(out.Findings, finding)
		}
	}
	if out.Mismatches > 0 {
		out.Complete = false
	}
	s.emitAccessGraphParity(ctx, req, out)
	return out
}

func bearerScopeFromEvidence(e AccessGraphEvidence) string {
	ref := e.Ref
	if ref == "" {
		return ""
	}
	const marker = "/scope:"
	i := strings.Index(ref, marker)
	if i < 0 {
		return ""
	}
	return ref[i+len(marker):]
}

func (s *Service) emitAccessGraphParity(ctx context.Context, req AccessGraphRequest, shadow AccessGraphParityShadow) {
	if s == nil || s.sink == nil {
		return
	}
	actor := req.ActorRef
	if actor == "" {
		actor = req.SubjectRef
	}
	if actor == "" {
		actor = "system"
	}
	payload := map[string]any{
		"subject_ref": string(req.SubjectRef),
		"layer":       normalizeAccessGraphLayer(req.Layer),
		"checked":     shadow.Checked,
		"mismatches":  shadow.Mismatches,
		"complete":    shadow.Complete,
	}
	if req.RequestID != "" {
		payload["request_id"] = req.RequestID
	}
	_, _ = s.sink.Emit(ctx, observability.EmitCommand{
		EventType: "authorization.access_graph.parity_shadow",
		Refs:      observability.EventRefs{OrganizationID: req.OrgID},
		Actor:     observability.Actor(actor),
		Payload:   payload,
	})
}

func accessGraphEvidence(source DecisionSource, ref string, redact bool) AccessGraphEvidence {
	e := AccessGraphEvidence{Source: source}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return e
	}
	if redact {
		e.Ref = redactEvidenceRef(ref)
		e.Hash = accessGraphHash(ref)
		e.Redacted = true
		return e
	}
	e.Ref = ref
	e.Hash = accessGraphHash(ref)
	return e
}

func RedactEvidenceRef(ref string) string {
	return redactEvidenceRef(ref)
}

func redactEvidenceRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "admin_tokens:") {
		if i := strings.Index(ref, "/scope:"); i >= 0 {
			return "admin_tokens:redacted" + ref[i:]
		}
		return "admin_tokens:redacted"
	}
	if i := strings.IndexAny(ref, ":/"); i >= 0 {
		return ref[:i+1] + "redacted"
	}
	return "redacted"
}

func accessGraphHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])[:16]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
