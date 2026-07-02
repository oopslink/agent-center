package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

type TemplateRepo struct{ db *sql.DB }

func NewTemplateRepo(db *sql.DB) *TemplateRepo { return &TemplateRepo{db: db} }

const templateSelect = `SELECT id, org_id, name, description, content, is_builtin, created_by, created_at, updated_at, version FROM pm_templates`

func (r *TemplateRepo) Save(ctx context.Context, t *pm.Template) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_templates (id, org_id, name, description, content, is_builtin, created_by, created_at, updated_at, version)
         VALUES (?,?,?,?,?,?,?,?,?,?)`,
		string(t.ID()), t.OrgID(), t.Name(), t.Description(), t.Content(),
		boolToInt(t.IsBuiltin()), string(t.CreatedBy()),
		ts(t.CreatedAt()), ts(t.UpdatedAt()), t.Version())
	if isUnique(err) {
		return pm.ErrTemplateExists
	}
	return err
}

func (r *TemplateRepo) Update(ctx context.Context, t *pm.Template) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_templates SET name=?, description=?, content=?, updated_at=?, version=? WHERE id=?`,
		t.Name(), t.Description(), t.Content(), ts(t.UpdatedAt()), t.Version(), string(t.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrTemplateNotFound
	}
	return nil
}

func (r *TemplateRepo) FindByID(ctx context.Context, id pm.TemplateID) (*pm.Template, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, templateSelect+` WHERE id = ?`, string(id))
	t, err := scanTemplate(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrTemplateNotFound
	}
	return t, err
}

func (r *TemplateRepo) ListByOrg(ctx context.Context, orgID string) ([]*pm.Template, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, templateSelect+` WHERE org_id = ? OR is_builtin = 1 ORDER BY is_builtin DESC, name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Template
	for rows.Next() {
		t, err := scanTemplate(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TemplateRepo) Delete(ctx context.Context, id pm.TemplateID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM pm_templates WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrTemplateNotFound
	}
	return nil
}

func scanTemplate(scan func(...any) error) (*pm.Template, error) {
	var id, orgID, name, description, content, createdBy, createdAt, updatedAt string
	var isBuiltin int
	var version int
	if err := scan(&id, &orgID, &name, &description, &content, &isBuiltin, &createdBy, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return pm.RehydrateTemplate(pm.RehydrateTemplateInput{
		ID:          pm.TemplateID(id),
		OrgID:       orgID,
		Name:        name,
		Description: description,
		Content:     content,
		Builtin:     isBuiltin != 0,
		CreatedBy:   pm.IdentityRef(createdBy),
		CreatedAt:   parseTime(createdAt),
		UpdatedAt:   parseTime(updatedAt),
		Version:     version,
	})
}

var _ pm.TemplateRepository = (*TemplateRepo)(nil)
