package authorization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrEnforceNotReady = errors.New("authorization: enforce readiness check failed")

// ValidateEnforceReady proves the persisted authorization registry is complete
// enough to run the unified resolver as the sole enforcement path. It is
// intentionally stricter than shadow mode: missing tables, stale permission
// registry rows, missing system roles, or incomplete built-in role permissions
// must refuse process startup instead of silently falling back.
func (s *Service) ValidateEnforceReady(ctx context.Context) error {
	if s == nil || s.db == nil || s.store == nil {
		return fmt.Errorf("%w: authorization service is not wired", ErrEnforceNotReady)
	}
	if err := s.requireReadinessTables(ctx); err != nil {
		return err
	}
	if err := s.requirePermissionRegistryReady(ctx); err != nil {
		return err
	}
	if err := s.requireSystemRolesReady(ctx); err != nil {
		return err
	}
	if _, err := s.effectiveVersion(ctx); err != nil {
		return fmt.Errorf("%w: effective permission version unavailable: %v", ErrEnforceNotReady, err)
	}
	return nil
}

func (s *Service) requireReadinessTables(ctx context.Context) error {
	exec, err := s.store.exec(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEnforceNotReady, err)
	}
	for _, table := range []string{
		"permission_definitions",
		"authorization_roles",
		"authorization_role_permissions",
		"authorization_role_assignments",
		"authorization_audit_events",
		"team_members",
		"team_projects",
		"team_role_ram_role_mappings",
		"team_role_ram_role_versions",
	} {
		if _, err := exec.QueryContext(ctx, "SELECT 1 FROM "+table+" LIMIT 0"); err != nil {
			return fmt.Errorf("%w: required table %s unavailable: %v", ErrEnforceNotReady, table, err)
		}
	}
	return nil
}

func (s *Service) requirePermissionRegistryReady(ctx context.Context) error {
	defs, err := s.store.ListDefinitions(ctx)
	if err != nil {
		return fmt.Errorf("%w: permission registry unreadable: %v", ErrEnforceNotReady, err)
	}
	have := make(map[PermissionKey]PermissionDefinition, len(defs))
	for _, def := range defs {
		have[def.Key] = def
	}
	for _, want := range Definitions() {
		got, ok := have[want.Key]
		if !ok {
			return fmt.Errorf("%w: missing permission definition %s", ErrEnforceNotReady, want.Key)
		}
		if got.Category != want.Category || !sameStringSet(got.ResourceKinds, want.ResourceKinds) {
			return fmt.Errorf("%w: stale permission definition %s", ErrEnforceNotReady, want.Key)
		}
	}
	return nil
}

func (s *Service) requireSystemRolesReady(ctx context.Context) error {
	for _, roleID := range []string{
		"sys-org-owner",
		"sys-org-admin",
		"sys-org-member",
		"sys-project-owner",
		"sys-project-member",
		"sys-team-web-owner",
		"sys-team-web-admin",
		"sys-team-web-member",
		"sys-team-member",
		"sys-admin-token",
	} {
		role, err := s.store.getRole(ctx, roleID)
		if err != nil {
			if errors.Is(err, ErrRoleNotFound) || errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: missing system role %s", ErrEnforceNotReady, roleID)
			}
			return fmt.Errorf("%w: system role %s unreadable: %v", ErrEnforceNotReady, roleID, err)
		}
		if role.Kind != "system" {
			return fmt.Errorf("%w: role %s is %s, want system", ErrEnforceNotReady, roleID, role.Kind)
		}
		wantPerms := readinessSystemRolePermissions(roleID)
		if len(wantPerms) == 0 {
			continue
		}
		gotPerms, err := s.store.rolePermissions(ctx, roleID)
		if err != nil {
			return fmt.Errorf("%w: role %s permissions unreadable: %v", ErrEnforceNotReady, roleID, err)
		}
		got := map[string]bool{}
		for _, p := range gotPerms {
			got[string(p.PermissionKey)+"\x00"+p.ResourceKind] = true
		}
		for _, p := range wantPerms {
			key := string(p.PermissionKey) + "\x00" + p.ResourceKind
			if !got[key] {
				return fmt.Errorf("%w: role %s missing permission %s/%s", ErrEnforceNotReady, roleID, p.PermissionKey, p.ResourceKind)
			}
		}
	}
	return nil
}

func readinessSystemRolePermissions(roleID string) []RolePermission {
	if perms := builtinRolePermissionsFallback(roleID); len(perms) > 0 {
		return perms
	}
	add := func(key PermissionKey, kind string) RolePermission {
		return RolePermission{RoleID: roleID, PermissionKey: key, ResourceKind: kind}
	}
	switch roleID {
	case "sys-project-owner":
		return []RolePermission{
			add("project.read", "project"), add("project.write", "project"), add("project.member.add", "project"),
			add("project.member.remove", "project"), add("project.repo_ref.manage", "project"), add("project.stage.manage", "project"),
		}
	case "sys-project-member":
		return []RolePermission{
			add("project.read", "project"), add("project.write", "project"), add("project.member.add", "project"),
			add("project.repo_ref.manage", "project"),
		}
	case "sys-admin-token":
		return []RolePermission{
			add("admin_token.manage", "admin_token"), add("secret.resolve", "secret"), add("blob.put", "blob"),
			add("dispatch.pull", "worker"), add("task.internal.report", "task"), add("worker.enroll", "worker"),
			add("worker.heartbeat", "worker"), add("worker.capability.report", "worker"),
			add("runtime.status.read", "worker"), add("runtime.deploy", "worker"),
		}
	}
	return nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[strings.TrimSpace(v)]++
	}
	for _, v := range b {
		key := strings.TrimSpace(v)
		if seen[key] == 0 {
			return false
		}
		seen[key]--
	}
	return true
}
