package query

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

// LogsKind enumerates `logs <kind>` values.
type LogsKind string

const (
	LogsTask      LogsKind = "task"
	LogsExecution LogsKind = "execution"
)

// AllLogsKinds is the closed enum.
var AllLogsKinds = []LogsKind{LogsTask, LogsExecution}

// ValidLogsKind reports recognition.
func ValidLogsKind(s string) bool {
	for _, k := range AllLogsKinds {
		if string(k) == s {
			return true
		}
	}
	return false
}

// Sentinel errors.
var (
	ErrLogsKindUnknown    = errors.New("logs: unknown kind")
	ErrLogsTargetMissing  = errors.New("logs: target id has no associated blob")
	ErrLogsArchivedFollow = errors.New("logs: --follow not supported on archived blobs")
)

// LogsRequest is the verb's input.
type LogsRequest struct {
	Kind   LogsKind
	ID     string
	Follow bool
}

// LogsService surfaces the archived `tasks/<id>/log.log.gz` /
// `tasks/<id>/<exec_id>/trace.jsonl.gz` blobs. Live `--follow` should be
// routed to peek-trace (plan-4 § 3.6 step 6: `logs --follow on archived`
// returns explicit error).
type LogsService struct {
	deps  Deps
	store blobstore.BlobStore
}

// NewLogsService wires.
func NewLogsService(deps Deps, store blobstore.BlobStore) *LogsService {
	return &LogsService{deps: deps, store: store}
}

// Open opens the blob for streaming to stdout. Returned ReadCloser MUST be
// closed by the caller.
func (s *LogsService) Open(ctx context.Context, req LogsRequest) (io.ReadCloser, string, error) {
	if !ValidLogsKind(string(req.Kind)) {
		return nil, "", fmt.Errorf("%w: %q", ErrLogsKindUnknown, req.Kind)
	}
	if req.ID == "" {
		return nil, "", errors.New("logs: id required")
	}
	if req.Follow {
		return nil, "", ErrLogsArchivedFollow
	}
	if s.store == nil {
		return nil, "", errors.New("logs: blob store not wired")
	}
	var blobRef string
	switch req.Kind {
	case LogsTask:
		if s.deps.Tasks == nil {
			return nil, "", errors.New("logs: tasks repo not wired")
		}
		t, err := s.deps.Tasks.FindByID(ctx, taskruntime.TaskID(req.ID))
		if err != nil {
			return nil, "", err
		}
		// Tasks don't have a blob_path column in v1 schema; we default
		// to the canonical archive path on disk: tasks/<id>/log.log.gz.
		_ = t
		blobRef = fmt.Sprintf("tasks/%s/log.log.gz", req.ID)
	case LogsExecution:
		if s.deps.Executions == nil {
			return nil, "", errors.New("logs: executions repo not wired")
		}
		e, err := s.deps.Executions.FindByID(ctx, taskruntime.TaskExecutionID(req.ID))
		if err != nil {
			return nil, "", err
		}
		blobRef = fmt.Sprintf("tasks/%s/%s/trace.jsonl.gz", e.TaskID(), e.ID())
	}
	rc, err := s.store.Get(ctx, blobRef)
	if err != nil {
		if errors.Is(err, blobstore.ErrBlobNotFound) {
			return nil, blobRef, fmt.Errorf("%w: %s", ErrLogsTargetMissing, blobRef)
		}
		return nil, blobRef, err
	}
	return rc, blobRef, nil
}
