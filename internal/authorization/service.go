package authorization

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

type Service struct {
	db                       *sql.DB
	store                    *Store
	gen                      idgen.Generator
	clock                    clock.Clock
	sink                     *observability.EventSink
	effectiveCache           effectiveCache
	mode                     EnforcementMode
	metrics                  shadowMetricCounters
	requiredShadowTransports []Transport
	minShadowChecks          int64
	minShadowWindow          time.Duration
}

type Deps struct {
	DB                       *sql.DB
	Store                    *Store
	IDGen                    idgen.Generator
	Clock                    clock.Clock
	EventSink                *observability.EventSink
	Mode                     EnforcementMode
	RequiredShadowTransports []Transport
	MinShadowChecks          int64
	MinShadowWindow          time.Duration
}

const (
	defaultEnforceReadinessMaxAge = 24 * time.Hour
	defaultEnforceShadowWindow    = time.Minute
)

var defaultReadinessCoveragePairs = []string{
	"org:org.read",
	"project:project.read",
	"project:project.write",
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
	mode := NormalizeEnforcementMode(deps.Mode)
	s := &Service{
		db: deps.DB, store: store, gen: gen, clock: clk, sink: deps.EventSink, mode: mode,
		requiredShadowTransports: deps.RequiredShadowTransports, minShadowChecks: deps.MinShadowChecks, minShadowWindow: deps.MinShadowWindow,
	}
	return s
}

type shadowMetricCounters struct {
	checks         atomic.Int64
	mismatches     atomic.Int64
	legacyOnly     atomic.Int64
	equivalentOnly atomic.Int64
}

func NormalizeEnforcementMode(mode EnforcementMode) EnforcementMode {
	switch mode {
	case EnforcementLegacy, EnforcementShadow, EnforcementEnforce:
		return mode
	default:
		return EnforcementShadow
	}
}

func ParseEnforcementMode(raw string) (EnforcementMode, error) {
	mode := EnforcementMode(strings.TrimSpace(strings.ToLower(raw)))
	if mode == "" {
		return EnforcementShadow, nil
	}
	switch mode {
	case EnforcementLegacy, EnforcementShadow, EnforcementEnforce:
		return mode, nil
	case "or", "dual_allow", "dual-allow", "fallback":
		return "", fmt.Errorf("%w: authorization mode %q is forbidden; use legacy, shadow, or enforce", ErrInvalid, raw)
	default:
		return "", fmt.Errorf("%w: unknown authorization mode %q", ErrInvalid, raw)
	}
}

func (s *Service) EnforcementMode() EnforcementMode {
	if s == nil {
		return EnforcementLegacy
	}
	return s.mode
}

func (s *Service) ShadowMetrics() ShadowMetrics {
	if s == nil {
		return ShadowMetrics{}
	}
	return ShadowMetrics{
		Checks:         s.metrics.checks.Load(),
		Mismatches:     s.metrics.mismatches.Load(),
		LegacyOnly:     s.metrics.legacyOnly.Load(),
		EquivalentOnly: s.metrics.equivalentOnly.Load(),
	}
}

func (s *Service) ShadowReadyToEnforce() bool {
	if s == nil {
		return false
	}
	if s.store != nil && len(s.requiredShadowTransports) > 0 {
		return s.ValidateEnforceReadiness(context.Background(), s.requiredShadowTransports, defaultEnforceReadinessMaxAge) == nil
	}
	return s.metrics.checks.Load() > 0 && s.metrics.mismatches.Load() == 0
}

func (s *Service) ShadowReadiness(ctx context.Context) (ShadowReadiness, error) {
	if s == nil || s.store == nil {
		return ShadowReadiness{}, fmt.Errorf("%w: shadow readiness store is not wired", ErrDenied)
	}
	rec, err := s.store.getShadowReadiness(ctx)
	if err != nil {
		return ShadowReadiness{}, err
	}
	return ShadowReadiness{
		Mode:            rec.Mode,
		WindowStartedAt: rec.WindowStartedAt.UTC().Format(time.RFC3339Nano),
		WindowEndedAt:   rec.WindowEndedAt.UTC().Format(time.RFC3339Nano),
		Transports:      append([]string(nil), rec.Transports...),
		Checks:          rec.Checks,
		Mismatches:      rec.Mismatches,
		LegacyOnly:      rec.LegacyOnly,
		EquivalentOnly:  rec.EquivalentOnly,
		Ready:           rec.Ready,
		Reason:          rec.Reason,
	}, nil
}

func (s *Service) ValidateEnforceReadiness(ctx context.Context, required []Transport, maxAge time.Duration) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: shadow readiness store is not wired", ErrDenied)
	}
	rec, err := s.store.getShadowReadiness(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: shadow readiness evidence missing", ErrDenied)
		}
		return err
	}
	if rec.Checks <= 0 {
		return fmt.Errorf("%w: shadow readiness has no checks", ErrDenied)
	}
	minChecks := s.minShadowChecks
	if minChecks <= 0 && len(required) > 0 {
		minChecks = int64(len(required))
	}
	if minChecks > 0 && rec.Checks < minChecks {
		return fmt.Errorf("%w: shadow readiness has too few checks", ErrDenied)
	}
	if rec.Mismatches != 0 || !rec.Ready {
		return fmt.Errorf("%w: shadow readiness has mismatches", ErrDenied)
	}
	if rec.WindowStartedAt.IsZero() || rec.WindowEndedAt.IsZero() || rec.WindowEndedAt.Before(rec.WindowStartedAt) {
		return fmt.Errorf("%w: shadow readiness window is incomplete", ErrDenied)
	}
	if s.minShadowWindow > 0 && rec.WindowEndedAt.Sub(rec.WindowStartedAt) < s.minShadowWindow {
		return fmt.Errorf("%w: shadow readiness window is too short", ErrDenied)
	}
	if maxAge > 0 && s.clock.Now().UTC().Sub(rec.WindowEndedAt.UTC()) > maxAge {
		return fmt.Errorf("%w: shadow readiness evidence is stale", ErrDenied)
	}
	auditCoverage, err := s.store.shadowAuditCoverage(ctx, rec.WindowStartedAt, rec.WindowEndedAt)
	if err != nil {
		return err
	}
	if auditCoverage.Checks < rec.Checks || auditCoverage.Checks < minChecks {
		return fmt.Errorf("%w: shadow readiness audit evidence is incomplete", ErrDenied)
	}
	if auditCoverage.Mismatches != 0 {
		return fmt.Errorf("%w: shadow readiness audit evidence has mismatches", ErrDenied)
	}
	have := map[string]bool{}
	for _, transport := range rec.Transports {
		have[transport] = true
	}
	for _, transport := range required {
		raw := string(transport)
		if !have[raw] || !auditCoverage.Transports[raw] {
			return fmt.Errorf("%w: shadow readiness missing %s coverage", ErrDenied, transport)
		}
	}
	for _, pair := range defaultReadinessCoveragePairs {
		if !auditCoverage.CoveragePairs[pair] {
			return fmt.Errorf("%w: shadow readiness missing %s coverage", ErrDenied, pair)
		}
	}
	return nil
}

func (s *Service) AuditEnforcementModeSelected(ctx context.Context, actor SubjectRef, reason string) error {
	if s == nil || s.db == nil || s.store == nil {
		return errors.New("authorization service: nil db")
	}
	if actor == "" {
		actor = "system"
	}
	return s.audit(ctx, auditEvent{
		EventType:    "authorization.enforcement_mode.selected",
		ActorRef:     actor,
		ResourceKind: "authorization",
		ResourceID:   string(s.mode),
		Payload: map[string]any{
			"mode":    string(s.mode),
			"reason":  strings.TrimSpace(reason),
			"message": strings.TrimSpace(reason),
		},
	})
}

type effectiveCache struct {
	mu      sync.RWMutex
	entries map[string]effectiveCacheEntry
}

type effectiveCacheEntry struct {
	version     string
	effective   []EffectivePermission
	denied      []string
	resolvedOrg string
}

func effectiveCacheKey(req CheckRequest) string {
	r := req.Resource
	return strings.Join([]string{
		string(req.SubjectRef),
		string(req.Transport),
		req.BearerScope,
		string(req.Permission),
		r.Kind,
		r.ID,
		r.OrgID,
		r.ProjectID,
		r.URI,
		r.OwnerRef,
		r.IdentityMemberID,
	}, "\x00")
}

func cloneEffectivePermissions(in []EffectivePermission) []EffectivePermission {
	if len(in) == 0 {
		return nil
	}
	out := make([]EffectivePermission, len(in))
	copy(out, in)
	return out
}

func cloneDenied(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
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
	exp, err := s.ResolveEffective(ctx, req)
	if err != nil {
		return exp.Decision, err
	}
	if !exp.Decision.Allowed {
		return exp.Decision, ErrDenied
	}
	return exp.Decision, nil
}

func (s *Service) Explain(ctx context.Context, req CheckRequest) (ExplainResult, error) {
	return s.ResolveEffective(ctx, req)
}

func (s *Service) ResolveEffective(ctx context.Context, req CheckRequest) (ExplainResult, error) {
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
		if strings.HasPrefix(e.EventType, "authorization.shadow.") {
			continue
		}
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
			result, err := s.runOperation(txCtx, req.ActorRef, req.OrgID, req.IdempotencyKey, op)
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
	if err == nil {
		s.invalidateEffectiveCache()
	}
	return out, err
}

func (s *Service) RevokeBatch(ctx context.Context, req BatchRequest) (BatchResult, error) {
	for i := range req.Operations {
		req.Operations[i].Type = "revoke_assignment"
	}
	return s.ApplyBatch(ctx, req)
}

func (s *Service) PreviewRevoke(ctx context.Context, req RevokePreviewRequest) (RevokePreview, error) {
	if s == nil || s.db == nil || s.store == nil {
		return RevokePreview{}, errors.New("authorization service: nil db")
	}
	req.ActorRef = SubjectRef(strings.TrimSpace(string(req.ActorRef)))
	req.OrgID = strings.TrimSpace(req.OrgID)
	if req.ActorRef == "" || req.OrgID == "" {
		return RevokePreview{}, fmt.Errorf("%w: actor_ref and org_id required", ErrInvalid)
	}
	if err := req.ActorRef.Validate(); err != nil {
		return RevokePreview{}, err
	}
	if req.TTL <= 0 {
		req.TTL = 5 * time.Minute
	}
	now := s.clock.Now().UTC()
	token, err := randomToken()
	if err != nil {
		return RevokePreview{}, err
	}
	var out RevokePreview
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		spec, operations, err := s.normalizedRevokeSpec(txCtx, req.ActorRef, req.OrgID, req.Operations)
		if err != nil {
			return err
		}
		body, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		requestHash := hashBytes(body)
		subjectHash := hashSubjects(spec.Targets)
		previewID := "rvp-" + accessSafeHash(string(body)+"|"+now.Format(time.RFC3339Nano))
		rec := revokePreviewRecord{
			PreviewID: previewID, TokenHash: hashString(token), ActorRef: req.ActorRef, OrgID: req.OrgID,
			SubjectHash: subjectHash, RequestHash: requestHash, RequestJSON: string(body), CreatedAt: now, ExpiresAt: now.Add(req.TTL),
		}
		if err := s.store.saveRevokePreview(txCtx, rec); err != nil {
			return err
		}
		out = RevokePreview{
			PreviewID: previewID, Token: token, ActorRef: req.ActorRef, OrgID: req.OrgID,
			ExpiresAt: rec.ExpiresAt, Operations: operations, Targets: spec.Targets, RequestHash: requestHash,
		}
		return nil
	})
	return out, err
}

func (s *Service) ConfirmRevoke(ctx context.Context, req RevokeConfirmRequest) (BatchResult, error) {
	if s == nil || s.db == nil || s.store == nil {
		return BatchResult{}, errors.New("authorization service: nil db")
	}
	req.ActorRef = SubjectRef(strings.TrimSpace(string(req.ActorRef)))
	req.OrgID = strings.TrimSpace(req.OrgID)
	if req.PreviewID == "" || req.Token == "" || req.ActorRef == "" || req.OrgID == "" {
		return BatchResult{}, fmt.Errorf("%w: preview_id, token, actor_ref and org_id required", ErrInvalid)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		req.IdempotencyKey = "revoke-confirm-" + shortHash(req.PreviewID)
	}
	var out BatchResult
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		rec, err := s.store.getRevokePreview(txCtx, req.PreviewID)
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		switch {
		case rec.ActorRef != req.ActorRef || rec.OrgID != req.OrgID:
			return ErrPreviewRejected
		case rec.TokenHash != hashString(req.Token):
			return ErrPreviewRejected
		}
		batch, err := revokeConfirmBatchFromPreview(rec, req)
		if err != nil {
			return err
		}
		digest, err := batchDigest(batch)
		if err != nil {
			return err
		}
		if prevJSON, replay, err := s.store.beginIdempotency(txCtx, req.IdempotencyKey, string(req.ActorRef), "revoke_confirm", digest, now); err != nil || replay {
			if err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(prevJSON), &out); err != nil {
				return err
			}
			out.Replayed = true
			return nil
		}
		switch {
		case rec.Status != "pending":
			return ErrPreviewRejected
		case !rec.ExpiresAt.After(now):
			return ErrPreviewExpired
		}
		if err := s.verifyLiveRevokePreview(txCtx, rec, req); err != nil {
			return err
		}
		if err := s.store.consumeRevokePreview(txCtx, req.PreviewID, now); err != nil {
			return err
		}
		out, err = s.runBatchInTx(txCtx, batch)
		if err != nil {
			return err
		}
		body, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return s.store.completeIdempotency(txCtx, req.IdempotencyKey, body, now)
	})
	if err == nil {
		s.invalidateEffectiveCache()
	}
	return out, err
}

func (s *Service) verifyLiveRevokePreview(ctx context.Context, rec revokePreviewRecord, req RevokeConfirmRequest) error {
	spec, _, err := s.normalizedRevokeSpec(ctx, req.ActorRef, req.OrgID, req.Operations)
	if err != nil {
		return err
	}
	body, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if rec.RequestHash != hashBytes(body) || rec.SubjectHash != hashSubjects(spec.Targets) || rec.RequestJSON != string(body) {
		return ErrPreviewRejected
	}
	return nil
}

func revokeConfirmBatchFromPreview(rec revokePreviewRecord, req RevokeConfirmRequest) (BatchRequest, error) {
	var spec normalizedRevokeRequest
	if err := json.Unmarshal([]byte(rec.RequestJSON), &spec); err != nil {
		return BatchRequest{}, err
	}
	body, err := json.Marshal(spec)
	if err != nil {
		return BatchRequest{}, err
	}
	if rec.RequestHash != hashBytes(body) || rec.SubjectHash != hashSubjects(spec.Targets) || spec.ActorRef != req.ActorRef || spec.OrgID != req.OrgID {
		return BatchRequest{}, ErrPreviewRejected
	}
	if len(req.Operations) != len(spec.Targets) {
		return BatchRequest{}, ErrPreviewRejected
	}
	batch := BatchRequest{IdempotencyKey: req.IdempotencyKey, ActorRef: req.ActorRef, OrgID: req.OrgID}
	for i, op := range req.Operations {
		target := spec.Targets[i]
		opID := strings.TrimSpace(op.ID)
		if opID == "" {
			opID = fmt.Sprintf("revoke-%d", i+1)
		}
		if opID != target.OperationID {
			return BatchRequest{}, ErrPreviewRejected
		}
		if strings.TrimSpace(op.Revoke.AssignmentID) != "" {
			if strings.TrimSpace(op.Revoke.AssignmentID) != target.AssignmentID {
				return BatchRequest{}, ErrPreviewRejected
			}
		} else {
			kind, id := op.Revoke.Resource.Key()
			targetKind, targetID := target.Resource.Key()
			if op.Revoke.SubjectRef != target.SubjectRef || strings.TrimSpace(op.Revoke.RoleID) != target.RoleID || kind != targetKind || id != targetID {
				return BatchRequest{}, ErrPreviewRejected
			}
		}
		if strings.TrimSpace(op.Revoke.Reason) != target.Reason || strings.TrimSpace(op.Revoke.Message) != target.Message {
			return BatchRequest{}, ErrPreviewRejected
		}
		op.Type = "revoke_assignment"
		op.ID = opID
		op.Revoke.AssignmentID = target.AssignmentID
		op.Revoke.ExpectedVersion = target.Version
		op.Revoke.Reason = target.Reason
		op.Revoke.Message = target.Message
		batch.Operations = append(batch.Operations, op)
	}
	return batch, nil
}

type normalizedRevokeRequest struct {
	ActorRef SubjectRef         `json:"actor_ref"`
	OrgID    string             `json:"org_id"`
	Targets  []RevokeTargetSpec `json:"targets"`
}

func (s *Service) normalizedRevokeSpec(ctx context.Context, actor SubjectRef, orgID string, operations []BatchOperation) (normalizedRevokeRequest, []OperationResult, error) {
	spec := normalizedRevokeRequest{ActorRef: actor, OrgID: orgID}
	results := make([]OperationResult, 0, len(operations))
	for i, op := range operations {
		op.Type = "revoke_assignment"
		if strings.TrimSpace(op.ID) == "" {
			op.ID = fmt.Sprintf("revoke-%d", i+1)
		}
		if err := s.requireRevokeAllowed(ctx, actor, orgID, op.Revoke); err != nil {
			return spec, results, err
		}
		target, err := s.resolveRevokeTarget(ctx, orgID, op.Revoke)
		if err != nil {
			return spec, results, err
		}
		spec.Targets = append(spec.Targets, RevokeTargetSpec{
			OperationID: op.ID, AssignmentID: target.ID, SubjectRef: target.SubjectRef, RoleID: target.RoleID,
			Resource: ResourceScope{Kind: target.ResourceKind, ID: target.ResourceID, OrgID: target.OrgID},
			Version:  target.Version, Reason: strings.TrimSpace(op.Revoke.Reason), Message: strings.TrimSpace(op.Revoke.Message),
		})
		results = append(results, OperationResult{ID: op.ID, Type: op.Type, Status: previewStatus("revoked"), RoleID: target.RoleID, AssignmentID: target.ID})
	}
	return spec, results, nil
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
		or, err := s.runOperation(ctx, req.ActorRef, req.OrgID, req.IdempotencyKey, op)
		if err != nil {
			return BatchResult{}, err
		}
		res.Operations = append(res.Operations, or)
	}
	return res, nil
}

func (s *Service) runOperation(ctx context.Context, actor SubjectRef, orgID string, requestID string, op BatchOperation) (OperationResult, error) {
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
		if role.Kind != "custom" {
			return OperationResult{}, ErrSystemRoleImmutable
		}
		if role.OrgID != orgID {
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
		reason := strings.TrimSpace(op.Revoke.Reason)
		message := strings.TrimSpace(op.Revoke.Message)
		if message == "" {
			message = reason
		}
		payload := map[string]any{"status": status}
		if reason != "" {
			payload["reason"] = reason
			payload["message"] = message
		}
		if err := s.audit(ctx, auditEvent{EventType: "authorization.assignment.revoked", ActorRef: actor, SubjectRef: a.SubjectRef, RoleID: a.RoleID, AssignmentID: a.ID, ResourceKind: a.ResourceKind, ResourceID: a.ResourceID, RequestID: requestID, Payload: payload}); err != nil {
			return OperationResult{}, err
		}
		return OperationResult{ID: op.ID, Type: op.Type, Status: status, RoleID: a.RoleID, AssignmentID: a.ID}, nil

	case "grant_direct_permission":
		kind, resourceID := op.DirectGrant.Resource.Key()
		if kind == "" || resourceID == "" {
			return OperationResult{}, fmt.Errorf("%w: direct grant resource required", ErrInvalid)
		}
		if err := op.DirectGrant.SubjectRef.Validate(); err != nil {
			return OperationResult{}, err
		}
		if !PermissionDefinedForResource(op.DirectGrant.PermissionKey, kind) {
			return OperationResult{}, fmt.Errorf("%w: %s for %s", ErrPermissionUndefined, op.DirectGrant.PermissionKey, kind)
		}
		resolved, _, err := s.resolveResource(ctx, op.DirectGrant.Resource)
		if err != nil {
			return OperationResult{}, err
		}
		if resolved.OrgID == "" || resolved.OrgID != orgID {
			return OperationResult{}, fmt.Errorf("%w: direct grant resource belongs to another org", ErrNotFound)
		}
		if err := s.requireDelegatablePermission(ctx, actor, op.DirectGrant.PermissionKey, resolved); err != nil {
			return OperationResult{}, err
		}
		role, roleStatus, err := s.store.upsertManagedInternalRole(ctx, orgID, op.DirectGrant.PermissionKey, kind, s.clock.Now())
		if err != nil {
			return OperationResult{}, err
		}
		if err := s.requireAssignmentSubjectApplicable(ctx, op.DirectGrant.SubjectRef, role.ID, resolved); err != nil {
			return OperationResult{}, err
		}
		assignID := strings.TrimSpace(op.DirectGrant.ID)
		if assignID == "" {
			assignID = "asgn-" + shortHash(orgID+"|"+string(op.DirectGrant.SubjectRef)+"|"+role.ID+"|"+kind+"|"+resourceID)
		}
		a, status, err := s.store.assignRole(ctx, RoleAssignment{
			ID:           assignID,
			OrgID:        orgID,
			SubjectRef:   op.DirectGrant.SubjectRef,
			RoleID:       role.ID,
			ResourceKind: kind,
			ResourceID:   resourceID,
			CreatedBy:    string(actor),
			ExpiresAt:    op.DirectGrant.ExpiresAt,
		}, s.clock.Now())
		if err != nil {
			return OperationResult{}, err
		}
		if err := s.audit(ctx, auditEvent{EventType: "authorization.direct_grant.created", ActorRef: actor, SubjectRef: a.SubjectRef, PermissionKey: op.DirectGrant.PermissionKey, RoleID: role.ID, AssignmentID: a.ID, ResourceKind: kind, ResourceID: resourceID, Payload: map[string]any{"status": status, "role_status": roleStatus}}); err != nil {
			return OperationResult{}, err
		}
		return OperationResult{ID: op.ID, Type: op.Type, Status: status, RoleID: role.ID, AssignmentID: a.ID}, nil
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

func (s *Service) requireDelegatablePermission(ctx context.Context, actor SubjectRef, permission PermissionKey, resource ResourceScope) error {
	if actor == "system" {
		return nil
	}
	exp, err := s.Explain(ctx, CheckRequest{
		SubjectRef: actor,
		Transport:  TransportSystem,
		Permission: permission,
		Resource:   resource,
	})
	if err != nil && !errors.Is(err, ErrDenied) {
		return err
	}
	if !exp.Decision.Allowed {
		return fmt.Errorf("%w: %s", ErrNotDelegatable, permission)
	}
	for _, eff := range exp.Effective {
		if eff.Key == permission && eff.Delegatable {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotDelegatable, permission)
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
	version, err := s.effectiveVersion(ctx)
	if err != nil {
		return nil, []string{"effective permission version unavailable"}, err
	}
	key := effectiveCacheKey(req)
	if cached, ok := s.getCachedEffective(key, version); ok {
		return cached.effective, cached.denied, nil
	}
	effective, denied, err := s.deriveEffectiveUncached(ctx, req)
	if err == nil {
		s.putCachedEffective(key, effectiveCacheEntry{
			version:     version,
			effective:   cloneEffectivePermissions(effective),
			denied:      cloneDenied(denied),
			resolvedOrg: req.Resource.OrgID,
		})
	}
	return effective, denied, err
}

func (s *Service) deriveEffectiveUncached(ctx context.Context, req CheckRequest) ([]EffectivePermission, []string, error) {
	legacy, legacyDenied, legacyErr := s.deriveLegacyEffective(ctx, req)
	if s.mode == EnforcementLegacy {
		return legacy, legacyDenied, legacyErr
	}
	equivalent, equivalentDenied, equivalentErr := s.deriveEquivalentEffective(ctx, req)
	comparison := compareEffective(req, legacy, equivalent)
	s.recordShadowComparison(ctx, comparison)
	if s.mode == EnforcementEnforce {
		if equivalentErr != nil {
			return equivalent, equivalentDenied, equivalentErr
		}
		return equivalent, equivalentDenied, nil
	}
	return legacy, legacyDenied, legacyErr
}

func (s *Service) deriveLegacyEffective(ctx context.Context, req CheckRequest) ([]EffectivePermission, []string, error) {
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
	if err := s.addTeamRAMEffective(ctx, req, &out); err != nil {
		return out, denied, err
	}
	if err := s.addCustomEffective(ctx, req, &out); err != nil {
		return out, denied, err
	}
	return out, denied, nil
}

func (s *Service) getCachedEffective(key, version string) (effectiveCacheEntry, bool) {
	s.effectiveCache.mu.RLock()
	defer s.effectiveCache.mu.RUnlock()
	if s.effectiveCache.entries == nil {
		return effectiveCacheEntry{}, false
	}
	entry, ok := s.effectiveCache.entries[key]
	if !ok || entry.version != version {
		return effectiveCacheEntry{}, false
	}
	entry.effective = cloneEffectivePermissions(entry.effective)
	entry.denied = cloneDenied(entry.denied)
	return entry, true
}

func (s *Service) putCachedEffective(key string, entry effectiveCacheEntry) {
	s.effectiveCache.mu.Lock()
	defer s.effectiveCache.mu.Unlock()
	if s.effectiveCache.entries == nil {
		s.effectiveCache.entries = map[string]effectiveCacheEntry{}
	}
	entry.effective = cloneEffectivePermissions(entry.effective)
	entry.denied = cloneDenied(entry.denied)
	s.effectiveCache.entries[key] = entry
}

func (s *Service) invalidateEffectiveCache() {
	s.effectiveCache.mu.Lock()
	defer s.effectiveCache.mu.Unlock()
	s.effectiveCache.entries = nil
}

func (s *Service) effectiveVersion(ctx context.Context) (string, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, 8)
	queries := []string{
		`SELECT COALESCE(GROUP_CONCAT(v, '|'), '') FROM (SELECT id || ':' || org_id || ':' || subject_ref || ':' || role_id || ':' || resource_kind || ':' || resource_id || ':' || version || ':' || COALESCE(revoked_at, '') || ':' || COALESCE(expires_at, '') AS v FROM authorization_role_assignments ORDER BY id)`,
		`SELECT COALESCE(GROUP_CONCAT(v, '|'), '') FROM (SELECT id || ':' || org_id || ':' || kind || ':' || COALESCE(managed, 0) || ':' || COALESCE(visibility, 'visible') || ':' || name || ':' || version || ':' || COALESCE(revoked_at, '') AS v FROM authorization_roles ORDER BY id)`,
		`SELECT COALESCE(GROUP_CONCAT(v, '|'), '') FROM (SELECT role_id || ':' || permission_key || ':' || resource_kind || ':' || delegatable AS v FROM authorization_role_permissions ORDER BY role_id, permission_key, resource_kind)`,
		`SELECT COALESCE(GROUP_CONCAT(v, '|'), '') FROM (SELECT team_id || ':' || member_ref || ':' || role || ':' || created_at AS v FROM team_members ORDER BY team_id, member_ref)`,
		`SELECT COALESCE(GROUP_CONCAT(v, '|'), '') FROM (SELECT team_id || ':' || project_id || ':' || created_at AS v FROM team_projects ORDER BY team_id, project_id)`,
		`SELECT COALESCE(GROUP_CONCAT(v, '|'), '') FROM (SELECT team_id || ':' || team_role || ':' || ram_role_id || ':' || created_at AS v FROM team_role_ram_role_mappings ORDER BY team_id, team_role, ram_role_id)`,
		`SELECT COALESCE(GROUP_CONCAT(v, '|'), '') FROM (SELECT team_id || ':' || team_role || ':' || version || ':' || updated_at AS v FROM team_role_ram_role_versions ORDER BY team_id, team_role)`,
		`SELECT COALESCE(GROUP_CONCAT(v, '|'), '') FROM (SELECT team_id || ':' || agent_ref || ':' || created_at AS v FROM team_memory_policy_curators ORDER BY team_id, agent_ref)`,
	}
	for _, query := range queries {
		var part string
		if err := exec.QueryRowContext(ctx, query).Scan(&part); err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "|"), nil
}

func (s *Service) deriveEquivalentEffective(ctx context.Context, req CheckRequest) ([]EffectivePermission, []string, error) {
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
	if err := s.addBuiltinEquivalentEffective(ctx, req, add, &denied); err != nil {
		return out, denied, err
	}
	// Team Role -> RAM Role mappings are the unified model introduced by the
	// resolver. They must participate in both shadow sides so enforce mode does
	// not revoke a valid mapped grant merely because the legacy-equivalence
	// migration only materialized membership-era built-ins.
	if err := s.addTeamRAMEffective(ctx, req, &out); err != nil {
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

func (s *Service) addBuiltinEquivalentEffective(ctx context.Context, req CheckRequest, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
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
				return s.expandBuiltinRole(ctx, orgBuiltinRoleID(m.Role), SourceOrgRole, m.EvidenceRef, r.Kind, add)
			case "team":
				if err := s.expandBuiltinRole(ctx, teamWebBuiltinRoleID(m.Role), SourceOrgRole, m.EvidenceRef, r.Kind, add); err != nil {
					return err
				}
			}
		} else if req.SubjectRef.IsUser() || req.SubjectRef.IsAgent() {
			*denied = append(*denied, "subject is not a joined org member")
		}
	}
	switch r.Kind {
	case "project":
		return s.addProjectEquivalent(ctx, req.SubjectRef, r.ID, add, denied)
	case "task":
		return s.addTaskEquivalent(ctx, req.SubjectRef, r.ID, add, denied)
	case "issue":
		return s.addChildProjectEquivalent(ctx, req.SubjectRef, "issue", r.ID, add, denied)
	case "plan":
		return s.addChildProjectEquivalent(ctx, req.SubjectRef, "plan", r.ID, add, denied)
	case "team":
		return s.addTeamEquivalent(ctx, req.SubjectRef, r.ID, add, denied)
	case "conversation":
		return s.addConversationEquivalent(ctx, req.SubjectRef, r.ID, add, denied)
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

func orgBuiltinRoleID(role string) string {
	switch role {
	case "owner":
		return "sys-org-owner"
	case "admin":
		return "sys-org-admin"
	default:
		return "sys-org-member"
	}
}

func projectBuiltinRoleID(role string) string {
	if role == "owner" {
		return "sys-project-owner"
	}
	return "sys-project-member"
}

func teamWebBuiltinRoleID(role string) string {
	switch role {
	case "owner":
		return "sys-team-web-owner"
	case "admin":
		return "sys-team-web-admin"
	default:
		return "sys-team-web-member"
	}
}

func (s *Service) expandBuiltinRole(ctx context.Context, roleID string, source DecisionSource, evidence, resourceKind string, add func(PermissionKey, DecisionSource, string, bool)) error {
	if roleID == "" {
		return nil
	}
	perms, err := s.store.rolePermissions(ctx, roleID)
	if errors.Is(err, ErrRoleNotFound) {
		perms = builtinRolePermissionsFallback(roleID)
		err = nil
	}
	if err != nil {
		return err
	}
	for _, p := range perms {
		if p.ResourceKind == resourceKind {
			add(p.PermissionKey, source, evidence, p.Delegatable)
		}
	}
	return nil
}

func builtinRolePermissionsFallback(roleID string) []RolePermission {
	add := func(key PermissionKey, kind string, delegatable bool) RolePermission {
		return RolePermission{RoleID: roleID, PermissionKey: key, ResourceKind: kind, Delegatable: delegatable}
	}
	switch roleID {
	case "sys-org-owner":
		return []RolePermission{
			add("org.read", "org", true), add("org.settings.manage", "org", true), add("org.lifecycle.manage", "org", true),
			add("org.member.list", "org", true), add("org.member.create.human", "org", true), add("org.member.create.agent", "org", true),
			add("org.member.role.manage", "org", true), add("org.member.disable", "org", true), add("org.invitation.manage", "org", true),
			add("org.analytics.read", "org", true), add("org.work_items.read", "org", true), add("team.create", "org", true), add("template.read", "org", true), add("template.write", "org", true),
			add("coderepo.workspace.read", "org", true), add("coderepo.workspace.manage", "org", true),
			add("ai_runtime.catalog.read", "org", true), add("ai_runtime.catalog.export", "org", true), add("ai_runtime.catalog.manage", "org", true),
		}
	case "sys-org-admin":
		return []RolePermission{
			add("org.read", "org", false), add("org.member.list", "org", false), add("org.member.create.human", "org", true),
			add("org.member.create.agent", "org", true), add("org.invitation.manage", "org", true), add("org.analytics.read", "org", false),
			add("org.work_items.read", "org", false), add("team.create", "org", false), add("template.read", "org", false), add("template.write", "org", false), add("coderepo.workspace.read", "org", false),
			add("coderepo.workspace.manage", "org", false), add("ai_runtime.catalog.read", "org", false),
			add("ai_runtime.catalog.export", "org", false), add("ai_runtime.catalog.manage", "org", false),
		}
	case "sys-org-member":
		return []RolePermission{
			add("org.read", "org", false), add("org.member.list", "org", false), add("org.work_items.read", "org", false),
			add("team.create", "org", false), add("template.read", "org", false), add("template.write", "org", false), add("coderepo.workspace.read", "org", false),
			add("ai_runtime.catalog.read", "org", false), add("ai_runtime.catalog.export", "org", false),
		}
	case "sys-team-web-owner":
		return []RolePermission{
			add("team.read", "team", true), add("team.write", "team", true), add("team.member.manage", "team", true),
			add("team.project.link.manage", "team", true), add("team.runtime_config.manage", "team", true),
			add("team.memory.read", "team", true), add("team.memory.propose", "team", true), add("team.memory.review", "team", true),
		}
	case "sys-team-web-admin":
		return []RolePermission{
			add("team.read", "team", false), add("team.write", "team", false), add("team.member.manage", "team", false),
			add("team.project.link.manage", "team", false), add("team.runtime_config.manage", "team", false),
			add("team.memory.read", "team", false), add("team.memory.propose", "team", false), add("team.memory.review", "team", false),
		}
	case "sys-team-web-member":
		return []RolePermission{
			add("team.read", "team", false), add("team.write", "team", false), add("team.member.manage", "team", false),
			add("team.project.link.manage", "team", false), add("team.runtime_config.manage", "team", false), add("team.memory.read", "team", false),
		}
	}
	return nil
}

func (s *Service) addProjectEquivalent(ctx context.Context, subject SubjectRef, projectID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	pm, ok, err := s.projectMember(ctx, projectID, subject)
	if err != nil {
		return err
	}
	if !ok {
		*denied = append(*denied, "subject is not a project member")
		return nil
	}
	return s.expandBuiltinRole(ctx, projectBuiltinRoleID(pm.Role), SourceProjectMember, pm.EvidenceRef, "project", add)
}

func (s *Service) addTaskEquivalent(ctx context.Context, subject SubjectRef, taskID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
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
		add("task.start.self", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
		add("task.heartbeat.self", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
		add("task.complete.self", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
		add("task.block.self", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
	}
	if createdBy == string(subject) {
		add("task.read", SourceProjectMember, "pm_tasks:"+taskID+"/created_by", false)
	}
	return s.addProjectEquivalent(ctx, subject, projectID, func(key PermissionKey, source DecisionSource, evidence string, delegatable bool) {
		switch key {
		case "project.read":
			add("task.read", source, evidence, delegatable)
		case "project.write":
			add("task.write", source, evidence, delegatable)
		}
	}, denied)
}

func (s *Service) addChildProjectEquivalent(ctx context.Context, subject SubjectRef, kind, id string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	projectID, err := s.parentProject(ctx, kind, id)
	if err != nil {
		return err
	}
	return s.addProjectEquivalent(ctx, subject, projectID, func(key PermissionKey, source DecisionSource, evidence string, delegatable bool) {
		switch {
		case kind == "issue" && key == "project.read":
			add("issue.read", source, evidence, delegatable)
		case kind == "issue" && key == "project.write":
			add("issue.write", source, evidence, delegatable)
		case kind == "plan" && key == "project.read":
			add("plan.read", source, evidence, delegatable)
		case kind == "plan" && key == "project.write":
			add("plan.write", source, evidence, delegatable)
		}
	}, denied)
}

func (s *Service) addTeamEquivalent(ctx context.Context, subject SubjectRef, teamID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	if !subject.IsAgent() {
		return nil
	}
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
		if err := s.expandBuiltinRole(ctx, "sys-team-member", SourceTeamMember, evidence, "team", add); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !inTeam {
		*denied = append(*denied, "agent is not a current team member")
		return nil
	}
	var exists int
	if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_memory_policy_curators WHERE team_id = ? AND agent_ref = ?`, teamID, subject).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		add("team.memory.review", SourceTeamMemoryPolicy, "team_memory_policy_curators:"+teamID+"/"+string(subject), false)
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
	add("team.create", SourceOrgRole, evidence, role == "owner")
	add("template.read", SourceOrgRole, evidence, role == "owner")
	add("template.write", SourceOrgRole, evidence, role == "owner")
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
		add("task.start.self", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
		add("task.heartbeat.self", SourceProjectMember, "pm_tasks:"+taskID+"/assignee", false)
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

func (s *Service) addTeamRAMEffective(ctx context.Context, req CheckRequest, out *[]EffectivePermission) error {
	if !(req.SubjectRef.IsUser() || req.SubjectRef.IsAgent()) || req.Resource.OrgID == "" {
		return nil
	}
	exec, err := s.store.exec(ctx)
	if err != nil {
		return err
	}
	rows, err := exec.QueryContext(ctx, `SELECT tm.team_id, tm.role, trm.ram_role_id, arp.permission_key, arp.resource_kind, arp.delegatable, COALESCE(tp.project_id, '')
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		JOIN team_role_ram_role_mappings trm ON trm.team_id = tm.team_id AND trm.team_role = tm.role
		JOIN authorization_roles ar ON ar.id = trm.ram_role_id AND ar.revoked_at IS NULL AND ar.kind IN ('system', 'custom') AND (ar.org_id = '' OR ar.org_id = t.org_id)
		JOIN authorization_role_permissions arp ON arp.role_id = ar.id
		LEFT JOIN team_projects tp ON tp.team_id = tm.team_id
		WHERE tm.member_ref = ? AND t.org_id = ?
		ORDER BY tm.team_id, tm.role, trm.ram_role_id, arp.permission_key, tp.project_id`, req.SubjectRef, req.Resource.OrgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var teamID, teamRole, roleID, resourceKind, linkedProjectID string
		var key PermissionKey
		var delegatable int
		if err := rows.Scan(&teamID, &teamRole, &roleID, &key, &resourceKind, &delegatable, &linkedProjectID); err != nil {
			return err
		}
		if !teamRAMScopeMatches(req.Resource, teamID, resourceKind, func(projectID string) bool { return linkedProjectID == projectID }) {
			continue
		}
		if req.Resource.Kind == "conversation" {
			if key == "project.read" || key == "task.read" || key == "issue.read" || key == "plan.read" {
				addEffectivePermission(out, EffectivePermission{
					Key:         "conversation.read",
					Source:      SourceTeamRoleRAM,
					EvidenceRef: "team_role_ram_role_mappings:" + teamID + "/" + teamRole + "/" + roleID,
					Delegatable: delegatable == 1,
					RoleID:      roleID,
				})
			}
			continue
		}
		if !PermissionDefinedForResource(key, req.Resource.Kind) {
			continue
		}
		addEffectivePermission(out, EffectivePermission{
			Key:         key,
			Source:      SourceTeamRoleRAM,
			EvidenceRef: "team_role_ram_role_mappings:" + teamID + "/" + teamRole + "/" + roleID,
			Delegatable: delegatable == 1,
			RoleID:      roleID,
		})
	}
	return rows.Err()
}

func teamRAMScopeMatches(r ResourceScope, teamID, permissionResourceKind string, linkedProject func(string) bool) bool {
	if r.Kind != "conversation" && permissionResourceKind != r.Kind {
		return false
	}
	switch r.Kind {
	case "team":
		return r.ID == teamID
	case "project":
		return r.ID != "" && linkedProject(r.ID)
	case "task", "issue", "plan":
		return r.ProjectID != "" && linkedProject(r.ProjectID)
	case "conversation":
		kind, _, ok := pmOwnerRef(r.OwnerRef)
		if !ok || r.ProjectID == "" || !linkedProject(r.ProjectID) {
			return false
		}
		return permissionResourceKind == "project" || permissionResourceKind == kind
	default:
		return false
	}
}

func addEffectivePermission(out *[]EffectivePermission, next EffectivePermission) {
	if next.Key == "" {
		return
	}
	for _, p := range *out {
		if p.Key == next.Key && p.Source == next.Source && p.EvidenceRef == next.EvidenceRef {
			return
		}
	}
	*out = append(*out, next)
}

func (s *Service) addConversationEffective(ctx context.Context, subject SubjectRef, convID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return err
	}
	var participantsJSON, ownerRef string
	if err := exec.QueryRowContext(ctx, `SELECT participants, COALESCE(owner_ref, '') FROM conversations WHERE id = ?`, convID).Scan(&participantsJSON, &ownerRef); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := s.addOwnedConversationEffective(ctx, subject, convID, ownerRef, add, denied); err != nil {
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

func (s *Service) addConversationEquivalent(ctx context.Context, subject SubjectRef, convID string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return err
	}
	var participantsJSON, ownerRef string
	if err := exec.QueryRowContext(ctx, `SELECT participants, COALESCE(owner_ref, '') FROM conversations WHERE id = ?`, convID).Scan(&participantsJSON, &ownerRef); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := s.addOwnedConversationEquivalent(ctx, subject, convID, ownerRef, add, denied); err != nil {
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

func (s *Service) addOwnedConversationEffective(ctx context.Context, subject SubjectRef, convID, ownerRef string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	kind, id, ok := pmOwnerRef(ownerRef)
	if !ok {
		return nil
	}
	bridge := func(key PermissionKey, source DecisionSource, evidence string, delegatable bool) {
		switch key {
		case "task.read", "issue.read", "plan.read":
			add("conversation.read", source, evidence+"/conversation:"+convID, delegatable)
		case "task.write", "issue.write", "plan.write":
			add("conversation.post", source, evidence+"/conversation:"+convID, delegatable)
		}
	}
	switch kind {
	case "task":
		return s.addTaskEffective(ctx, subject, id, bridge, denied)
	case "issue":
		return s.addIssueEffective(ctx, subject, id, bridge, denied)
	case "plan":
		return s.addPlanEffective(ctx, subject, id, bridge, denied)
	default:
		return nil
	}
}

func (s *Service) addOwnedConversationEquivalent(ctx context.Context, subject SubjectRef, convID, ownerRef string, add func(PermissionKey, DecisionSource, string, bool), denied *[]string) error {
	kind, id, ok := pmOwnerRef(ownerRef)
	if !ok {
		return nil
	}
	bridge := func(key PermissionKey, source DecisionSource, evidence string, delegatable bool) {
		switch key {
		case "task.read", "issue.read", "plan.read":
			add("conversation.read", source, evidence+"/conversation:"+convID, delegatable)
		case "task.write", "issue.write", "plan.write":
			add("conversation.post", source, evidence+"/conversation:"+convID, delegatable)
		}
	}
	switch kind {
	case "task":
		return s.addTaskEquivalent(ctx, subject, id, bridge, denied)
	case "issue":
		return s.addChildProjectEquivalent(ctx, subject, "issue", id, bridge, denied)
	case "plan":
		return s.addChildProjectEquivalent(ctx, subject, "plan", id, bridge, denied)
	default:
		return nil
	}
}

func pmOwnerRef(ownerRef string) (kind, id string, ok bool) {
	ownerRef = strings.TrimSpace(ownerRef)
	for _, p := range []struct {
		prefix string
		kind   string
	}{
		{"pm://tasks/", "task"},
		{"pm://issues/", "issue"},
		{"pm://plans/", "plan"},
	} {
		if strings.HasPrefix(ownerRef, p.prefix) {
			id = strings.TrimSpace(strings.TrimPrefix(ownerRef, p.prefix))
			return p.kind, id, id != ""
		}
	}
	return "", "", false
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
		if ref.Scope == "agent" && (ref.ScopeID == subject.BareID() || ref.ScopeID == r.OwnerRef || ref.ScopeID == r.IdentityMemberID) {
			evidence := "file_references:" + ref.Scope + "/" + ref.ScopeID
			add("file.download", SourceFileScope, evidence, false)
			add("file.attach", SourceFileScope, evidence, false)
			add("file.upload", SourceFileScope, evidence, false)
			return nil
		}
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
		role, err := s.store.getRole(ctx, a.RoleID)
		if err != nil {
			return err
		}
		source := SourceCustomRole
		if role.Kind == "managed" && role.Managed && role.Visibility == "internal" {
			source = SourceDirectBinding
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
				Source:       source,
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

func compareEffective(req CheckRequest, legacy, equivalent []EffectivePermission) ShadowComparison {
	legacySet := permissionSet(legacy)
	equivalentSet := permissionSet(equivalent)
	cmp := ShadowComparison{
		Mode:              EnforcementShadow,
		SubjectRef:        req.SubjectRef,
		Transport:         req.Transport,
		Permission:        req.Permission,
		Resource:          req.Resource,
		LegacyAllowed:     hasPermissionKey(legacySet, req.Permission),
		EquivalentAllowed: hasPermissionKey(equivalentSet, req.Permission),
	}
	for key := range legacySet {
		if _, ok := equivalentSet[key]; !ok {
			cmp.LegacyOnly = append(cmp.LegacyOnly, key)
		}
	}
	for key := range equivalentSet {
		if _, ok := legacySet[key]; !ok {
			cmp.EquivalentOnly = append(cmp.EquivalentOnly, key)
		}
	}
	sort.Slice(cmp.LegacyOnly, func(i, j int) bool { return cmp.LegacyOnly[i] < cmp.LegacyOnly[j] })
	sort.Slice(cmp.EquivalentOnly, func(i, j int) bool { return cmp.EquivalentOnly[i] < cmp.EquivalentOnly[j] })
	cmp.Mismatch = cmp.LegacyAllowed != cmp.EquivalentAllowed || len(cmp.LegacyOnly) > 0 || len(cmp.EquivalentOnly) > 0
	return cmp
}

func permissionSet(perms []EffectivePermission) map[PermissionKey]struct{} {
	out := make(map[PermissionKey]struct{}, len(perms))
	for _, p := range perms {
		out[p.Key] = struct{}{}
	}
	return out
}

func hasPermissionKey(set map[PermissionKey]struct{}, key PermissionKey) bool {
	if key == "*" {
		return len(set) > 0
	}
	_, ok := set[key]
	return ok
}

func (s *Service) recordShadowComparison(ctx context.Context, cmp ShadowComparison) {
	if s == nil || s.mode == EnforcementLegacy {
		return
	}
	cmp.Mode = s.mode
	s.metrics.checks.Add(1)
	if cmp.Mismatch {
		s.metrics.mismatches.Add(1)
		s.metrics.legacyOnly.Add(int64(len(cmp.LegacyOnly)))
		s.metrics.equivalentOnly.Add(int64(len(cmp.EquivalentOnly)))
	}
	s.recordShadowComparisonAudit(ctx, cmp)
	s.persistShadowReadiness(ctx, cmp)
	if cmp.Mismatch && s.sink != nil {
		_, _ = s.sink.Emit(ctx, observability.EmitCommand{
			EventType: observability.EventType("authorization.shadow.diff"),
			Refs:      observability.EventRefs{OrganizationID: cmp.Resource.OrgID, ProjectID: cmp.Resource.ProjectID},
			Actor:     observability.Actor(cmp.SubjectRef),
			Payload: map[string]any{
				"mode":               string(cmp.Mode),
				"subject_ref":        string(cmp.SubjectRef),
				"permission":         string(cmp.Permission),
				"resource_kind":      cmp.Resource.Kind,
				"resource_id":        cmp.Resource.ID,
				"transport":          string(cmp.Transport),
				"legacy_allowed":     cmp.LegacyAllowed,
				"equivalent_allowed": cmp.EquivalentAllowed,
				"legacy_only":        permissionKeysToStrings(cmp.LegacyOnly),
				"equivalent_only":    permissionKeysToStrings(cmp.EquivalentOnly),
			},
		})
	}
}

func (s *Service) recordShadowComparisonAudit(ctx context.Context, cmp ShadowComparison) {
	if s == nil || s.store == nil {
		return
	}
	now := s.clock.Now().UTC()
	_ = s.audit(ctx, auditEvent{
		ID:            "audit-" + shortHash("authorization.shadow.compare|"+string(cmp.SubjectRef)+"|"+string(cmp.Transport)+"|"+string(cmp.Permission)+"|"+cmp.Resource.Kind+"|"+cmp.Resource.ID+"|"+now.Format(time.RFC3339Nano)),
		EventType:     "authorization.shadow.compare",
		ActorRef:      cmp.SubjectRef,
		SubjectRef:    cmp.SubjectRef,
		PermissionKey: cmp.Permission,
		ResourceKind:  cmp.Resource.Kind,
		ResourceID:    cmp.Resource.ID,
		Payload: map[string]any{
			"mode":               string(cmp.Mode),
			"subject_ref":        string(cmp.SubjectRef),
			"transport":          string(cmp.Transport),
			"permission":         string(cmp.Permission),
			"resource_kind":      cmp.Resource.Kind,
			"resource_id":        cmp.Resource.ID,
			"project_id":         cmp.Resource.ProjectID,
			"org_id":             cmp.Resource.OrgID,
			"legacy_allowed":     cmp.LegacyAllowed,
			"equivalent_allowed": cmp.EquivalentAllowed,
			"mismatch":           cmp.Mismatch,
			"legacy_only":        permissionKeysToStrings(cmp.LegacyOnly),
			"equivalent_only":    permissionKeysToStrings(cmp.EquivalentOnly),
		},
		CreatedAt: now,
	})
}

func (s *Service) persistShadowReadiness(ctx context.Context, cmp ShadowComparison) {
	if s == nil || s.store == nil {
		return
	}
	now := s.clock.Now().UTC()
	transports := []string{string(cmp.Transport)}
	if existing, err := s.store.getShadowReadiness(ctx); err == nil {
		transports = append(transports, existing.Transports...)
		if !existing.WindowStartedAt.IsZero() {
			now = existing.WindowStartedAt.UTC()
		}
	}
	checks := s.metrics.checks.Load()
	mismatches := s.metrics.mismatches.Load()
	_ = s.store.persistShadowReadiness(ctx, shadowReadinessRecord{
		Mode:            s.mode,
		WindowStartedAt: now,
		WindowEndedAt:   s.clock.Now().UTC(),
		Transports:      dedupeStrings(transports),
		Checks:          checks,
		Mismatches:      mismatches,
		LegacyOnly:      s.metrics.legacyOnly.Load(),
		EquivalentOnly:  s.metrics.equivalentOnly.Load(),
		Ready:           checks > 0 && mismatches == 0,
		Reason:          "shadow comparison persisted",
	})
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func permissionKeysToStrings(keys []PermissionKey) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, string(key))
	}
	return out
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
		orgID, ownerRef, err := s.conversationScope(ctx, r.ID)
		if err != nil {
			return r, []string{"conversation not found"}, err
		}
		if r.OrgID != "" && r.OrgID != orgID {
			return r, []string{"conversation belongs to another org"}, ErrNotFound
		}
		r.OrgID = orgID
		r.OwnerRef = ownerRef
		if kind, id, ok := pmOwnerRef(ownerRef); ok {
			if projectID, err := s.parentProject(ctx, kind, id); err == nil {
				r.ProjectID = projectID
			}
		}
	case "file":
		if r.URI == "" {
			r.URI = r.ID
		}
		// A new upload has no file URI until its transfer session is created.
		// In that pre-creation check, the requested live placement refs are the
		// resource being authorized. Existing-file operations still require a URI.
		if r.URI == "" && len(r.Refs) == 0 {
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

func (s *Service) conversationScope(ctx context.Context, convID string) (string, string, error) {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return "", "", err
	}
	var orgID, ownerRef string
	if err := exec.QueryRowContext(ctx, `SELECT organization_id, COALESCE(owner_ref, '') FROM conversations WHERE id = ?`, convID).Scan(&orgID, &ownerRef); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return orgID, ownerRef, nil
}

func (s *Service) conversationOrg(ctx context.Context, convID string) (string, error) {
	orgID, _, err := s.conversationScope(ctx, convID)
	return orgID, err
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

func randomToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func hashString(v string) string {
	return hashBytes([]byte(v))
}

func hashBytes(v []byte) string {
	sum := sha256.Sum256(v)
	return hex.EncodeToString(sum[:])
}

func accessSafeHash(v string) string {
	return hashString(v)[:16]
}

func hashSubjects(targets []RevokeTargetSpec) string {
	subjects := make([]string, 0, len(targets))
	for _, target := range targets {
		subjects = append(subjects, string(target.SubjectRef))
	}
	sort.Strings(subjects)
	return hashString(strings.Join(subjects, "\x00"))
}
