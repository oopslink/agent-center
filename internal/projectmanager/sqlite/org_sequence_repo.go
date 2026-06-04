package sqlite

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// OrgSequenceRepo implements pm.OrgSequenceRepository over pm_org_sequence
// (v2.7.1 #245).
type OrgSequenceRepo struct{ db *sql.DB }

func NewOrgSequenceRepo(db *sql.DB) *OrgSequenceRepo { return &OrgSequenceRepo{db: db} }

// Allocate atomically returns + advances the per-(org, type) counter. The single
// UPSERT...RETURNING is race-safe: a missing row inserts next_value=1 (allocates
// 1); an existing row sets next_value=next_value+1 RETURNING the new value.
// next_value is thus the most-recently-allocated number (high-water), matching
// the migration's backfill seed (next_value = MAX(org_number)). Runs in the
// caller's tx via ExecutorFromCtx so allocation commits atomically with the
// issue/task insert.
func (r *OrgSequenceRepo) Allocate(ctx context.Context, orgID, entityType string) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var n int
	err := exec.QueryRowContext(ctx,
		`INSERT INTO pm_org_sequence (organization_id, entity_type, next_value)
		 VALUES (?, ?, 1)
		 ON CONFLICT(organization_id, entity_type)
		 DO UPDATE SET next_value = next_value + 1
		 RETURNING next_value`,
		orgID, entityType).Scan(&n)
	return n, err
}

var _ pm.OrgSequenceRepository = (*OrgSequenceRepo)(nil)
