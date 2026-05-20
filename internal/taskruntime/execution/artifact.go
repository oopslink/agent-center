package execution

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// Artifact is the TaskExecution append-only child entity (02-task-execution
// § 12). Independent table `artifacts`; not part of TaskExecution row.
type Artifact struct {
	id           taskruntime.ArtifactID
	taskID       taskruntime.TaskID
	executionID  taskruntime.TaskExecutionID
	kind         string
	title        string
	blobRef      string
	url          string
	metadataJSON string
	createdAt    time.Time
	createdBy    string
}

// NewArtifactInput captures constructor args.
type NewArtifactInput struct {
	ID           taskruntime.ArtifactID
	TaskID       taskruntime.TaskID
	ExecutionID  taskruntime.TaskExecutionID
	Kind         string
	Title        string
	BlobRef      string
	URL          string
	MetadataJSON string
	CreatedBy    string
	Now          time.Time
}

// NewArtifact constructs a fresh Artifact.
func NewArtifact(in NewArtifactInput) (*Artifact, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("artifact: id required")
	}
	if strings.TrimSpace(string(in.TaskID)) == "" {
		return nil, errors.New("artifact: task_id required")
	}
	if strings.TrimSpace(string(in.ExecutionID)) == "" {
		return nil, errors.New("artifact: execution_id required")
	}
	if strings.TrimSpace(in.Kind) == "" {
		return nil, errors.New("artifact: kind required")
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("artifact: title required")
	}
	if strings.TrimSpace(in.CreatedBy) == "" {
		return nil, errors.New("artifact: created_by required")
	}
	if in.Now.IsZero() {
		return nil, errors.New("artifact: now required")
	}
	metaJSON := in.MetadataJSON
	if metaJSON == "" {
		metaJSON = "{}"
	}
	return &Artifact{
		id:           in.ID,
		taskID:       in.TaskID,
		executionID:  in.ExecutionID,
		kind:         in.Kind,
		title:        in.Title,
		blobRef:      in.BlobRef,
		url:          in.URL,
		metadataJSON: metaJSON,
		createdAt:    in.Now.UTC(),
		createdBy:    in.CreatedBy,
	}, nil
}

// RehydrateArtifactInput is for repository round-trip.
type RehydrateArtifactInput struct {
	ID           taskruntime.ArtifactID
	TaskID       taskruntime.TaskID
	ExecutionID  taskruntime.TaskExecutionID
	Kind         string
	Title        string
	BlobRef      string
	URL          string
	MetadataJSON string
	CreatedAt    time.Time
	CreatedBy    string
}

// RehydrateArtifact reconstructs.
func RehydrateArtifact(in RehydrateArtifactInput) (*Artifact, error) {
	return &Artifact{
		id:           in.ID,
		taskID:       in.TaskID,
		executionID:  in.ExecutionID,
		kind:         in.Kind,
		title:        in.Title,
		blobRef:      in.BlobRef,
		url:          in.URL,
		metadataJSON: in.MetadataJSON,
		createdAt:    in.CreatedAt.UTC(),
		createdBy:    in.CreatedBy,
	}, nil
}

// Getters.
func (a *Artifact) ID() taskruntime.ArtifactID                { return a.id }
func (a *Artifact) TaskID() taskruntime.TaskID                { return a.taskID }
func (a *Artifact) ExecutionID() taskruntime.TaskExecutionID  { return a.executionID }
func (a *Artifact) Kind() string                              { return a.kind }
func (a *Artifact) Title() string                             { return a.title }
func (a *Artifact) BlobRef() string                           { return a.blobRef }
func (a *Artifact) URL() string                               { return a.url }
func (a *Artifact) MetadataJSON() string                      { return a.metadataJSON }
func (a *Artifact) CreatedAt() time.Time                      { return a.createdAt }
func (a *Artifact) CreatedBy() string                         { return a.createdBy }

// ArtifactRepository per 00-overview § 5.4 (append-only).
type ArtifactRepository interface {
	FindByID(ctx context.Context, id taskruntime.ArtifactID) (*Artifact, error)
	FindByExecutionID(ctx context.Context, executionID taskruntime.TaskExecutionID) ([]*Artifact, error)
	FindByTaskID(ctx context.Context, taskID taskruntime.TaskID) ([]*Artifact, error)
	Append(ctx context.Context, a *Artifact) error
}
