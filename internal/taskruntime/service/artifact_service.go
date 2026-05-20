package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// ArtifactService handles `report-artifact` writes.
type ArtifactService struct {
	db          *sql.DB
	artifactRepo execution.ArtifactRepository
	execRepo    execution.Repository
	sink        *observability.EventSink
	idgen       idgen.Generator
	clock       clock.Clock
}

// NewArtifactService constructs the service.
func NewArtifactService(db *sql.DB, artifactRepo execution.ArtifactRepository, execRepo execution.Repository, sink *observability.EventSink, gen idgen.Generator, clk clock.Clock) *ArtifactService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ArtifactService{
		db: db, artifactRepo: artifactRepo, execRepo: execRepo,
		sink: sink, idgen: gen, clock: clk,
	}
}

// AppendInput captures parameters for `report-artifact`.
type AppendInput struct {
	ExecutionID  taskruntime.TaskExecutionID
	Kind         string
	Title        string
	BlobRef      string
	URL          string
	MetadataJSON string
	Actor        observability.Actor
}

// AppendResult bundles ids.
type AppendResult struct {
	ArtifactID taskruntime.ArtifactID
}

// Append writes the Artifact row + emits `artifact.uploaded`.
func (s *ArtifactService) Append(ctx context.Context, in AppendInput) (*AppendResult, error) {
	if err := in.Actor.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(in.ExecutionID)) == "" {
		return nil, errors.New("artifact: execution_id required")
	}
	now := s.clock.Now()
	var res AppendResult
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		e, err := s.execRepo.FindByID(txCtx, in.ExecutionID)
		if err != nil {
			return err
		}
		a, err := execution.NewArtifact(execution.NewArtifactInput{
			ID:           taskruntime.ArtifactID(s.idgen.NewULID()),
			TaskID:       e.TaskID(),
			ExecutionID:  e.ID(),
			Kind:         in.Kind,
			Title:        in.Title,
			BlobRef:      in.BlobRef,
			URL:          in.URL,
			MetadataJSON: in.MetadataJSON,
			CreatedBy:    in.Actor.String(),
			Now:          now,
		})
		if err != nil {
			return err
		}
		if err := s.artifactRepo.Append(txCtx, a); err != nil {
			return err
		}
		res.ArtifactID = a.ID()
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "artifact.uploaded",
			Refs: observability.EventRefs{
				ExecutionID: string(e.ID()),
				TaskID:      string(e.TaskID()),
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"artifact_id":  string(a.ID()),
				"execution_id": string(e.ID()),
				"task_id":      string(e.TaskID()),
				"kind":         a.Kind(),
				"title":        a.Title(),
				"blob_ref":     a.BlobRef(),
				"url":          a.URL(),
			},
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &res, nil
}
