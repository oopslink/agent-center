package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// ProjectRepo implements workforce.ProjectRepository.
type ProjectRepo struct {
	db *sql.DB
}

// NewProjectRepo constructs the repository.
func NewProjectRepo(db *sql.DB) *ProjectRepo {
	return &ProjectRepo{db: db}
}

// Save inserts a new Project. id-clash → ErrProjectAlreadyExists.
func (r *ProjectRepo) Save(ctx context.Context, p *workforce.Project) error {
	if p == nil {
		return errors.New("project repo: nil project")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO projects (
		id, name, description, tags,
		created_by_identity_id, created_at, updated_at, version, organization_id
	) VALUES (?,?,?,?,?,?,?,?,?)`
	tagsJSON, err := marshalTags(p.Tags())
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, stmt,
		string(p.ID()),
		p.Name(),
		p.Description(),
		tagsJSON,
		p.CreatedByIdentityID(),
		p.CreatedAt().Format(time.RFC3339Nano),
		p.UpdatedAt().Format(time.RFC3339Nano),
		p.Version(),
		p.OrganizationID(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			return workforce.ErrProjectAlreadyExists
		}
		return err
	}
	return nil
}

// FindByID returns a Project; ErrProjectNotFound if absent.
func (r *ProjectRepo) FindByID(ctx context.Context, id workforce.ProjectID) (*workforce.Project, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, projectSelect+` WHERE id = ?`, string(id))
	p, err := scanProject(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrProjectNotFound
	}
	return p, err
}

// FindAll lists all projects.
//
// v2.5.5 dropped the by-kind filter alongside ProjectKind; tag-based
// filtering, when introduced, will read the JSON column at the
// service layer or via a future projection table.
func (r *ProjectRepo) FindAll(ctx context.Context, _ workforce.ProjectFilter) ([]*workforce.Project, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, projectSelect+` ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*workforce.Project
	for rows.Next() {
		p, err := scanProject(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Update applies fields via CAS; returns updated Project on success.
func (r *ProjectRepo) Update(ctx context.Context, id workforce.ProjectID, fields workforce.ProjectUpdateFields, version int, at time.Time) (*workforce.Project, error) {
	if fields.IsEmpty() {
		return nil, errors.New("project repo: update has no changes")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	cur, err := r.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.Version() != version {
		return nil, workforce.ErrProjectVersionConflict
	}
	if err := cur.ApplyAndBumpVersion(fields, at); err != nil {
		return nil, err
	}
	tagsJSON, err := marshalTags(cur.Tags())
	if err != nil {
		return nil, err
	}
	const stmt = `UPDATE projects
		SET name = ?, description = ?, tags = ?,
		    updated_at = ?, version = ?
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		cur.Name(), cur.Description(), tagsJSON,
		cur.UpdatedAt().Format(time.RFC3339Nano), cur.Version(),
		string(id), version,
	)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, workforce.ErrProjectVersionConflict
	}
	return cur, nil
}

// Delete removes a Project. Strict: row presence is required; the active-
// mapping precondition is enforced by the caller (Application Service)
// using MappingRepository.CountActiveByProjectID.
//
// conventions § 9.w: schema declares no FOREIGN KEY; referential integrity
// is enforced at the application layer (ProjectCRUDService.Remove).
func (r *ProjectRepo) Delete(ctx context.Context, id workforce.ProjectID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return workforce.ErrProjectNotFound
	}
	return nil
}

const projectSelect = `SELECT id, name, description, tags,
	created_by_identity_id, created_at, updated_at, version, organization_id
	FROM projects`

func scanProject(scan func(...any) error) (*workforce.Project, error) {
	var (
		id, name, description string
		tagsJSON              string
		createdByIdentityID   string
		createdAt, updatedAt  string
		version               int
		organizationID        string
	)
	if err := scan(&id, &name, &description, &tagsJSON,
		&createdByIdentityID, &createdAt, &updatedAt, &version, &organizationID); err != nil {
		return nil, err
	}
	tags, err := unmarshalTags(tagsJSON)
	if err != nil {
		return nil, err
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, err
	}
	return workforce.RehydrateProject(workforce.RehydrateProjectInput{
		ID:                  workforce.ProjectID(id),
		Name:                name,
		Description:         description,
		Tags:                tags,
		CreatedByIdentityID: createdByIdentityID,
		CreatedAt:           created,
		UpdatedAt:           updated,
		Version:             version,
		OrganizationID:      organizationID,
	})
}

func marshalTags(tags []string) (string, error) {
	if len(tags) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "", fmt.Errorf("marshal tags: %w", err)
	}
	return string(b), nil
}

func unmarshalTags(s string) ([]string, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("unmarshal tags: %w", err)
	}
	return out, nil
}
