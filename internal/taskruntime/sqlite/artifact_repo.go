package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// ArtifactRepo implements execution.ArtifactRepository (append-only).
type ArtifactRepo struct {
	db *sql.DB
}

// NewArtifactRepo constructs the repo.
func NewArtifactRepo(db *sql.DB) *ArtifactRepo { return &ArtifactRepo{db: db} }

const artifactSelect = `SELECT
	id, task_id, execution_id, kind, title, blob_ref, url, metadata_json,
	created_at, created_by
FROM artifacts`

// Append inserts a new Artifact row. UPDATE / DELETE intentionally NOT
// implemented (interface contract: append-only).
func (r *ArtifactRepo) Append(ctx context.Context, a *execution.Artifact) error {
	if a == nil {
		return errors.New("artifact repo: nil artifact")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO artifacts (
		id, task_id, execution_id, kind, title, blob_ref, url, metadata_json,
		created_at, created_by
	) VALUES (?,?,?,?,?,?,?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		string(a.ID()),
		string(a.TaskID()),
		string(a.ExecutionID()),
		a.Kind(),
		a.Title(),
		nullString(a.BlobRef()),
		nullString(a.URL()),
		a.MetadataJSON(),
		a.CreatedAt().Format(timeFormat),
		a.CreatedBy(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			return errors.New("artifact: id duplicate")
		}
		return err
	}
	return nil
}

// FindByID returns an Artifact by id.
func (r *ArtifactRepo) FindByID(ctx context.Context, id taskruntime.ArtifactID) (*execution.Artifact, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, artifactSelect+` WHERE id = ?`, string(id))
	a, err := scanArtifact(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, execution.ErrArtifactNotFound
	}
	return a, err
}

// FindByExecutionID returns all artifacts for an execution.
func (r *ArtifactRepo) FindByExecutionID(ctx context.Context, executionID taskruntime.TaskExecutionID) ([]*execution.Artifact, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		artifactSelect+` WHERE execution_id = ? ORDER BY created_at DESC`,
		string(executionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

// FindByTaskID returns all artifacts for a task (across executions).
func (r *ArtifactRepo) FindByTaskID(ctx context.Context, taskID taskruntime.TaskID) ([]*execution.Artifact, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		artifactSelect+` WHERE task_id = ? ORDER BY created_at DESC`,
		string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func scanArtifacts(rows *sql.Rows) ([]*execution.Artifact, error) {
	var out []*execution.Artifact
	for rows.Next() {
		a, err := scanArtifact(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanArtifact(scan func(...any) error) (*execution.Artifact, error) {
	var (
		id           string
		taskID       string
		executionID  string
		kind         string
		title        string
		blobRef      sql.NullString
		url          sql.NullString
		metadataJSON string
		createdRaw   string
		createdBy    string
	)
	if err := scan(&id, &taskID, &executionID, &kind, &title, &blobRef, &url, &metadataJSON,
		&createdRaw, &createdBy); err != nil {
		return nil, err
	}
	createdAt, err := parseTimeStr(sql.NullString{String: createdRaw, Valid: true})
	if err != nil {
		return nil, err
	}
	return execution.RehydrateArtifact(execution.RehydrateArtifactInput{
		ID:           taskruntime.ArtifactID(id),
		TaskID:       taskruntime.TaskID(taskID),
		ExecutionID:  taskruntime.TaskExecutionID(executionID),
		Kind:         kind,
		Title:        title,
		BlobRef:      blobRef.String,
		URL:          url.String,
		MetadataJSON: metadataJSON,
		CreatedAt:    createdAt,
		CreatedBy:    createdBy,
	})
}
