// Package autoassign holds the auto-assign feature's shared config accessors
// (v2.18.3 BE-1, issue-577a7b0e). BE-1 ships ONLY the project-level master-switch
// accessor over the center settings store; the reconciler itself is BE-2.
package autoassign

import (
	"context"

	"github.com/oopslink/agent-center/internal/settings"
)

// keyPrefix namespaces the per-project master switch in the center settings store.
// The full key is keyPrefix + <project_id>. Following the wake.* convention, an
// ABSENT key means "use the default" — here ON (decision 1) — so a project that was
// never configured is auto-assign-enabled, and disabling is an explicit opt-out.
const keyPrefix = "auto_assign.enabled."

// EnabledKey returns the settings key for a project's auto-assign master switch.
func EnabledKey(projectID string) string { return keyPrefix + projectID }

// Enabled reports whether auto-assign is enabled for a project. Absent/empty/any
// non-"false" value ⇒ true (default ON, decision 1); only the literal "false"
// disables it. A nil store ⇒ the default (true) — a missing settings backend must
// not silently disable the feature.
func Enabled(ctx context.Context, store settings.Store, projectID string) (bool, error) {
	if store == nil {
		return true, nil
	}
	v, found, err := store.Get(ctx, EnabledKey(projectID))
	if err != nil {
		return true, err
	}
	if !found {
		return true, nil
	}
	return v != "false", nil
}

// SetEnabled persists a project's auto-assign master switch as the canonical
// "true"/"false" string.
func SetEnabled(ctx context.Context, store settings.Store, projectID string, enabled bool) error {
	val := "true"
	if !enabled {
		val = "false"
	}
	return store.Set(ctx, EnabledKey(projectID), val)
}
