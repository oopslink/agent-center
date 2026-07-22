package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ModelCatalogRepo persists the org model catalog over pm_model_catalog (0104).
type ModelCatalogRepo struct{ db *sql.DB }

func NewModelCatalogRepo(db *sql.DB) *ModelCatalogRepo { return &ModelCatalogRepo{db: db} }

const modelCatalogSelect = `SELECT id, org_id, model_id, display_name, input_cost, output_cost, context_window, tier, created_by, created_at, updated_at, version FROM pm_model_catalog`

func (r *ModelCatalogRepo) Save(ctx context.Context, e *pm.ModelCatalogEntry) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	return insertModelCatalogEntry(ctx, exec, e)
}

// insertModelCatalogEntry inserts one row, mapping a (org_id, model_id) unique
// collision to ErrModelCatalogEntryExists.
func insertModelCatalogEntry(ctx context.Context, exec persistence.SQLExecutor, e *pm.ModelCatalogEntry) error {
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_model_catalog (id, org_id, model_id, display_name, input_cost, output_cost, context_window, tier, created_by, created_at, updated_at, version, runtime_key)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(e.ID()), e.OrgID(), e.ModelID(), e.DisplayName(), e.InputCost(), e.OutputCost(),
		e.ContextWindow(), e.Tier(), string(e.CreatedBy()), ts(e.CreatedAt()), ts(e.UpdatedAt()), e.Version(), e.ModelID())
	if isUnique(err) {
		return pm.ErrModelCatalogEntryExists
	}
	return err
}

func (r *ModelCatalogRepo) Update(ctx context.Context, e *pm.ModelCatalogEntry) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_model_catalog SET model_id=?, display_name=?, input_cost=?, output_cost=?, context_window=?, tier=?, updated_at=?, version=? WHERE id=?`,
		e.ModelID(), e.DisplayName(), e.InputCost(), e.OutputCost(), e.ContextWindow(), e.Tier(),
		ts(e.UpdatedAt()), e.Version(), string(e.ID()))
	if isUnique(err) {
		return pm.ErrModelCatalogEntryExists
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrModelCatalogEntryNotFound
	}
	return nil
}

func (r *ModelCatalogRepo) FindByID(ctx context.Context, id pm.ModelCatalogEntryID) (*pm.ModelCatalogEntry, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, modelCatalogSelect+` WHERE id = ?`, string(id))
	e, err := scanModelCatalogEntry(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrModelCatalogEntryNotFound
	}
	return e, err
}

func (r *ModelCatalogRepo) FindByModelID(ctx context.Context, orgID, modelID string) (*pm.ModelCatalogEntry, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, modelCatalogSelect+` WHERE org_id = ? AND model_id = ?`, orgID, modelID)
	e, err := scanModelCatalogEntry(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrModelCatalogEntryNotFound
	}
	return e, err
}

func (r *ModelCatalogRepo) ListByOrg(ctx context.Context, orgID string) ([]*pm.ModelCatalogEntry, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, modelCatalogSelect+` WHERE org_id = ? ORDER BY model_id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.ModelCatalogEntry
	for rows.Next() {
		e, err := scanModelCatalogEntry(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *ModelCatalogRepo) Delete(ctx context.Context, id pm.ModelCatalogEntryID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM pm_model_catalog WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrModelCatalogEntryNotFound
	}
	return nil
}

// ReplaceForOrg atomically clears the org's catalog and inserts the given set
// (import mode=replace). All-or-nothing: any insert error rolls the whole tx back.
func (r *ModelCatalogRepo) ReplaceForOrg(ctx context.Context, orgID string, entries []*pm.ModelCatalogEntry) error {
	return persistence.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		exec, _ := persistence.ExecutorFromCtx(txCtx, r.db)
		if _, err := exec.ExecContext(txCtx, `DELETE FROM pm_model_catalog WHERE org_id = ?`, orgID); err != nil {
			return err
		}
		for _, e := range entries {
			if err := insertModelCatalogEntry(txCtx, exec, e); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpsertForOrg atomically inserts-or-updates each entry by (org_id, model_id)
// (import mode=upsert): an existing model_id is updated in place (version bumped),
// a new one is inserted. All-or-nothing.
func (r *ModelCatalogRepo) UpsertForOrg(ctx context.Context, orgID string, entries []*pm.ModelCatalogEntry) error {
	return persistence.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		exec, _ := persistence.ExecutorFromCtx(txCtx, r.db)
		for _, e := range entries {
			res, err := exec.ExecContext(txCtx,
				`UPDATE pm_model_catalog SET display_name=?, input_cost=?, output_cost=?, context_window=?, tier=?, updated_at=?, version=version+1 WHERE org_id=? AND model_id=?`,
				e.DisplayName(), e.InputCost(), e.OutputCost(), e.ContextWindow(), e.Tier(), ts(e.UpdatedAt()), orgID, e.ModelID())
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				continue // updated an existing row
			}
			if err := insertModelCatalogEntry(txCtx, exec, e); err != nil {
				return err
			}
		}
		return nil
	})
}

func scanModelCatalogEntry(scan func(...any) error) (*pm.ModelCatalogEntry, error) {
	var id, orgID, modelID, displayName, tier, createdBy, createdAt, updatedAt string
	var inputCost, outputCost float64
	var contextWindow, version int
	if err := scan(&id, &orgID, &modelID, &displayName, &inputCost, &outputCost, &contextWindow, &tier, &createdBy, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return pm.RehydrateModelCatalogEntry(pm.RehydrateModelCatalogEntryInput{
		ID:            pm.ModelCatalogEntryID(id),
		OrgID:         orgID,
		ModelID:       modelID,
		DisplayName:   displayName,
		InputCost:     inputCost,
		OutputCost:    outputCost,
		ContextWindow: contextWindow,
		Tier:          tier,
		CreatedBy:     pm.IdentityRef(createdBy),
		CreatedAt:     parseTime(createdAt),
		UpdatedAt:     parseTime(updatedAt),
		Version:       version,
	})
}

var _ pm.ModelCatalogRepository = (*ModelCatalogRepo)(nil)
