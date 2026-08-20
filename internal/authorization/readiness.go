package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const readinessWindow = 5 * time.Minute

var ErrReadinessRejected = errors.New("authorization: readiness gate rejected")

type ReadinessSnapshot struct {
	ID          string          `json:"id"`
	Mode        EnforcementMode `json:"mode"`
	Transports  []Transport     `json:"transports"`
	Permissions []PermissionKey `json:"permissions"`
	Resources   []string        `json:"resources"`
	Checks      int64           `json:"checks"`
	Mismatches  int64           `json:"mismatches"`
	StartedAt   time.Time       `json:"started_at"`
	ObservedAt  time.Time       `json:"observed_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

func RequiredReadinessTransports() []Transport {
	return []Transport{TransportWeb, TransportMCP, TransportSystem}
}

func RequiredReadinessPermissions() []PermissionKey {
	return []PermissionKey{"org.read", "project.read", "project.write", "task.write", "issue.write", "team.read"}
}

func RequiredReadinessResources() []string {
	return []string{"org", "project", "task", "issue", "team"}
}

func (s *Service) RecordReadiness(ctx context.Context, snap ReadinessSnapshot) error {
	if s == nil || s.store == nil {
		return ErrDenied
	}
	now := s.clock.Now().UTC()
	if snap.ID == "" {
		snap.ID = s.gen.NewULID()
	}
	if snap.StartedAt.IsZero() {
		snap.StartedAt = now.Add(-readinessWindow)
	}
	if snap.ObservedAt.IsZero() {
		snap.ObservedAt = now
	}
	if snap.ExpiresAt.IsZero() {
		snap.ExpiresAt = now.Add(readinessWindow)
	}
	snap.Transports = normalizeTransports(snap.Transports)
	snap.Permissions = normalizePermissions(snap.Permissions)
	snap.Resources = normalizeStrings(snap.Resources)
	if err := validateReadiness(snap, now); err != nil {
		_ = s.audit(ctx, auditEvent{EventType: "authorization.readiness.rejected", ActorRef: "system", ResourceKind: "authorization", ResourceID: "readiness", Payload: map[string]any{"reason": err.Error()}})
		return err
	}
	if err := s.store.saveReadiness(ctx, snap); err != nil {
		return err
	}
	return s.audit(ctx, auditEvent{EventType: "authorization.readiness.accepted", ActorRef: "system", ResourceKind: "authorization", ResourceID: "readiness", Payload: map[string]any{"checks": snap.Checks}})
}

func (s *Service) RequireEnforceReadiness(ctx context.Context) error {
	if s == nil || s.store == nil {
		return ErrDenied
	}
	if s.mode != EnforcementEnforce {
		return nil
	}
	snap, err := s.store.latestReadiness(ctx)
	if err != nil {
		_ = s.audit(ctx, auditEvent{EventType: "authorization.enforce.rejected", ActorRef: "system", ResourceKind: "authorization", ResourceID: "readiness", Payload: map[string]any{"reason": err.Error()}})
		return ErrReadinessRejected
	}
	if err := validateReadiness(snap, s.clock.Now().UTC()); err != nil {
		_ = s.audit(ctx, auditEvent{EventType: "authorization.enforce.rejected", ActorRef: "system", ResourceKind: "authorization", ResourceID: "readiness", Payload: map[string]any{"reason": err.Error()}})
		return err
	}
	return nil
}

func (s *Service) RollbackEnforcement(ctx context.Context, reason string) error {
	if s == nil || s.store == nil {
		return ErrDenied
	}
	s.mode = EnforcementLegacy
	return s.audit(ctx, auditEvent{EventType: "authorization.enforce.rollback", ActorRef: "system", ResourceKind: "authorization", ResourceID: "mode", Payload: map[string]any{"mode": string(s.mode), "reason": strings.TrimSpace(reason)}})
}

func validateReadiness(snap ReadinessSnapshot, now time.Time) error {
	if snap.Mode != EnforcementEnforce {
		return ErrReadinessRejected
	}
	if snap.Checks < 25 || snap.Mismatches != 0 {
		return ErrReadinessRejected
	}
	if snap.StartedAt.IsZero() || snap.ObservedAt.Sub(snap.StartedAt) < readinessWindow {
		return ErrReadinessRejected
	}
	if snap.ExpiresAt.IsZero() || !snap.ExpiresAt.After(now) {
		return ErrReadinessRejected
	}
	if !containsAllTransports(snap.Transports, RequiredReadinessTransports()) ||
		!containsAllPermissions(snap.Permissions, RequiredReadinessPermissions()) ||
		!containsAllStrings(snap.Resources, RequiredReadinessResources()) {
		return ErrReadinessRejected
	}
	return nil
}

func normalizeTransports(in []Transport) []Transport {
	out := make([]Transport, 0, len(in))
	seen := map[Transport]struct{}{}
	for _, v := range in {
		v = Transport(strings.TrimSpace(string(v)))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizePermissions(in []PermissionKey) []PermissionKey {
	out := make([]PermissionKey, 0, len(in))
	seen := map[PermissionKey]struct{}{}
	for _, v := range in {
		v = PermissionKey(strings.TrimSpace(string(v)))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func containsAllTransports(have, want []Transport) bool {
	set := map[Transport]struct{}{}
	for _, v := range have {
		set[v] = struct{}{}
	}
	for _, v := range want {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

func containsAllPermissions(have, want []PermissionKey) bool {
	set := map[PermissionKey]struct{}{}
	for _, v := range have {
		set[v] = struct{}{}
	}
	for _, v := range want {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

func containsAllStrings(have, want []string) bool {
	set := map[string]struct{}{}
	for _, v := range have {
		set[v] = struct{}{}
	}
	for _, v := range want {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

func readinessJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
