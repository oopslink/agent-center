package sqlite

import (
	"context"
	"database/sql"
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
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO projects (
		id, name, kind, default_agent_cli, description,
		created_by_identity_id, created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(p.ID()),
		p.Name(),
		nullString(string(p.Kind())),
		nullString(p.DefaultAgentCLI()),
		nullString(p.Description()),
		p.CreatedByIdentityID(),
		p.CreatedAt().Format(time.RFC3339Nano),
		p.UpdatedAt().Format(time.RFC3339Nano),
		p.Version(),
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
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, projectSelect+` WHERE id = ?`, string(id))
	p, err := scanProject(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrProjectNotFound
	}
	return p, err
}

// FindAll lists all projects (optionally filtered by kind).
func (r *ProjectRepo) FindAll(ctx context.Context, filter workforce.ProjectFilter) ([]*workforce.Project, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	q := projectSelect
	var args []any
	if filter.Kind != nil {
		q += ` WHERE kind = ?`
		args = append(args, string(*filter.Kind))
	}
	q += ` ORDER BY id`
	rows, err := exec.QueryContext(ctx, q, args...)
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
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	// Load existing → apply via domain method → CAS UPDATE.
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
	const stmt = `UPDATE projects
		SET name = ?, kind = ?, default_agent_cli = ?, description = ?,
		    updated_at = ?, version = ?
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		cur.Name(), nullString(string(cur.Kind())), nullString(cur.DefaultAgentCLI()),
		nullString(cur.Description()), cur.UpdatedAt().Format(time.RFC3339Nano), cur.Version(),
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
// using MappingRepository.CountActiveByProjectID. Implementation simply
// deletes the row.
//
// We return ErrProjectHasActiveDeps if FK referential integrity blocks the
// delete (mappings reference the project) — the precondition check is
// belt-and-braces here.
func (r *ProjectRepo) Delete(ctx context.Context, id workforce.ProjectID) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	res, err := exec.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, string(id))
	if err != nil {
		// Foreign-key violation → translate to domain error.
		if isForeignKeyViolation(err) {
			return workforce.ErrProjectHasActiveDeps
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return workforce.ErrProjectNotFound
	}
	return nil
}

func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "FOREIGN KEY constraint failed") ||
		contains(msg, "constraint failed: FOREIGN KEY")
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		// avoid pulling in strings just for this; tiny helper
		(len(haystack) > len(needle) && indexOf(haystack, needle) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

const projectSelect = `SELECT id, name, kind, default_agent_cli, description,
	created_by_identity_id, created_at, updated_at, version
	FROM projects`

func scanProject(scan func(...any) error) (*workforce.Project, error) {
	var (
		id, name                           string
		kind                               sql.NullString
		defaultAgentCLI, description       sql.NullString
		createdByIdentityID                string
		createdAt, updatedAt               string
		version                            int
	)
	if err := scan(&id, &name, &kind, &defaultAgentCLI, &description,
		&createdByIdentityID, &createdAt, &updatedAt, &version); err != nil {
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
		Kind:                workforce.ProjectKind(kind.String),
		DefaultAgentCLI:     defaultAgentCLI.String,
		Description:         description.String,
		CreatedByIdentityID: createdByIdentityID,
		CreatedAt:           created,
		UpdatedAt:           updated,
		Version:             version,
	})
}
