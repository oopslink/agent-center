// Package projection implements Observability BC projection read models.
//
// Per observability/00-overview § 1.4:
//   - AgentWorkItemProjection — independent table agent_work_item_projections
//     (read model over agent BC work items; BC-owned per conventions § 9.z).
package projection

import (
	"errors"
)

// Sentinel errors for the projection repository / service.
var (
	// ErrProjectionNotFound — FindByID with no matching row.
	ErrProjectionNotFound = errors.New("observability/projection: not found")
	// ErrProjectionStale — UPSERT request's LastPushAt is older than the
	// stored row's LastPushAt; service drops the write and emits an event
	// instead.
	ErrProjectionStale = errors.New("observability/projection: stale push (out of order)")
)
