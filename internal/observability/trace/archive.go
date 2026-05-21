// Package trace implements Observability BC's TraceArchiveService — packs
// per-execution {events.jsonl, agent.log} into trace.jsonl.gz and uploads
// to the BlobStore on execution terminal hook (plan-4 § 3.4 +
// ADR-0015 § 4 + 01-blob-store.md "路径约定").
package trace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

// FailureReason is the closed-enum set of TraceArchive failure causes
// (plan-4 § 3.4 + conventions § 16).
type FailureReason string

const (
	ReasonBlobStoreUnavailable FailureReason = "blob_store_unavailable"
	ReasonPayloadTooLarge      FailureReason = "payload_too_large"
	ReasonChecksumMismatch     FailureReason = "checksum_mismatch"
	ReasonSourceMissing        FailureReason = "source_missing"
)

// TraceArchiveResult summarises a successful upload.
type TraceArchiveResult struct {
	BlobRef string
	Bytes   int64
}

// SourceFileSpec is one logical file packed into the tarball (events.jsonl,
// agent.log, ...). Path is absolute on disk; NameInTar is the in-archive
// path (defaults to filepath.Base(Path)).
type SourceFileSpec struct {
	Path      string
	NameInTar string
}

// CenterCallback is invoked once an upload succeeds. The caller (worker
// daemon → center RPC layer) persists the blob ref into tasks.trace_blob_path
// (plan-4 § 3.4 step 4 + 01-blob-store.md "DB 字段").
type CenterCallback func(ctx context.Context, info TraceArchiveResult) error

// Service is the worker-daemon side TraceArchiveService.
type Service struct {
	store        blobstore.BlobStore
	sink         *observability.EventSink
	clk          clock.Clock
	maxBytes     int64
	defaultActor observability.Actor
}

// NewService wires a TraceArchiveService. sink must be non-nil so failure /
// success can emit observability events (§ 17 errors never silently
// swallowed). maxBytes defaults to blobstore.MaxBlobBytes.
func NewService(store blobstore.BlobStore, sink *observability.EventSink, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Service{store: store, sink: sink, clk: clk, maxBytes: blobstore.MaxBlobBytes, defaultActor: observability.Actor("system")}
}

// WithMaxBytes overrides the size cap (plan-4 § 6.7 hard guard).
func (s *Service) WithMaxBytes(n int64) *Service {
	if n > 0 {
		s.maxBytes = n
	}
	return s
}

// WithActor sets the actor used when emitting events (defaults to "system").
func (s *Service) WithActor(a observability.Actor) *Service {
	if a != "" {
		s.defaultActor = a
	}
	return s
}

// ArchiveRequest captures the per-execution archive job.
type ArchiveRequest struct {
	TaskID      string
	ExecutionID taskruntime.TaskExecutionID
	SourceFiles []SourceFileSpec
	BlobRef     string // optional — defaults to tasks/<task_id>/<execution_id>/trace.jsonl.gz
}

// Archive packs the request's source files, uploads them to the BlobStore,
// and on success emits observability.trace_archive_uploaded. Failures emit
// observability.trace_archive_failed with the reason closed-enum (§ 17).
//
// If center != nil it is invoked synchronously on success, allowing the
// caller (worker daemon → center RPC) to persist tasks.trace_blob_path
// in the same logical hop.
func (s *Service) Archive(ctx context.Context, req ArchiveRequest, center CenterCallback) (TraceArchiveResult, error) {
	if s == nil || s.store == nil {
		return TraceArchiveResult{}, errors.New("trace archive: nil receiver / store")
	}
	if req.ExecutionID == "" {
		return TraceArchiveResult{}, errors.New("trace archive: execution_id required")
	}
	if req.TaskID == "" {
		return TraceArchiveResult{}, errors.New("trace archive: task_id required")
	}
	blobRef := req.BlobRef
	if blobRef == "" {
		blobRef = fmt.Sprintf("tasks/%s/%s/trace.jsonl.gz", req.TaskID, req.ExecutionID)
	}
	// Build the tarball into a buffer (small enough — plan-4 § 6.7 enforced
	// via blobstore-side max guard too).
	buf, err := s.packFiles(req.SourceFiles)
	if err != nil {
		s.emitFailure(ctx, req, blobRef, classifyPackError(err), 0, err)
		return TraceArchiveResult{}, err
	}
	if int64(buf.Len()) > s.maxBytes {
		err := fmt.Errorf("%w: packed=%d max=%d", blobstore.ErrPayloadTooLarge, buf.Len(), s.maxBytes)
		s.emitFailure(ctx, req, blobRef, ReasonPayloadTooLarge, 1, err)
		return TraceArchiveResult{}, err
	}
	size := int64(buf.Len())
	if err := s.store.Put(ctx, blobRef, bytes.NewReader(buf.Bytes()), size); err != nil {
		reason := ReasonBlobStoreUnavailable
		if errors.Is(err, blobstore.ErrPayloadTooLarge) {
			reason = ReasonPayloadTooLarge
		}
		s.emitFailure(ctx, req, blobRef, reason, 1, err)
		return TraceArchiveResult{}, err
	}
	// Optionally verify by re-reading (light sanity — full checksum left
	// to future work).
	ok, exErr := s.store.Exists(ctx, blobRef)
	if exErr != nil || !ok {
		s.emitFailure(ctx, req, blobRef, ReasonChecksumMismatch, 1, fmt.Errorf("post-upload Exists returned ok=%v err=%v", ok, exErr))
		return TraceArchiveResult{}, fmt.Errorf("trace archive: post-upload existence check failed: %v", exErr)
	}
	result := TraceArchiveResult{BlobRef: blobRef, Bytes: size}
	if center != nil {
		if err := center(ctx, result); err != nil {
			s.emitFailure(ctx, req, blobRef, ReasonBlobStoreUnavailable, 1, fmt.Errorf("center callback: %w", err))
			return result, err
		}
	}
	if s.sink != nil {
		_, _ = s.sink.Emit(ctx, observability.EmitCommand{
			EventType: "observability.trace_archive_uploaded",
			Refs:      observability.EventRefs{ExecutionID: string(req.ExecutionID), TaskID: req.TaskID},
			Actor:     s.defaultActor,
			Payload: map[string]any{
				"execution_id": string(req.ExecutionID),
				"task_id":      req.TaskID,
				"blob_ref":     blobRef,
				"bytes":        size,
				"reason":       "execution_terminal",
				"message":      fmt.Sprintf("uploaded %d bytes to %s", size, blobRef),
			},
		})
	}
	return result, nil
}

func (s *Service) packFiles(files []SourceFileSpec) (*bytes.Buffer, error) {
	if len(files) == 0 {
		return nil, errors.New("trace archive: no source files")
	}
	buf := &bytes.Buffer{}
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		st, err := os.Stat(f.Path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("source_missing: %s", f.Path)
			}
			return nil, fmt.Errorf("stat %s: %w", f.Path, err)
		}
		hdr := &tar.Header{
			Name:    nameInTar(f),
			Mode:    int64(st.Mode().Perm()),
			Size:    st.Size(),
			ModTime: st.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("tar header %s: %w", f.Path, err)
		}
		src, err := os.Open(f.Path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", f.Path, err)
		}
		_, err = io.Copy(tw, src)
		_ = src.Close()
		if err != nil {
			return nil, fmt.Errorf("copy %s: %w", f.Path, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("tar close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return buf, nil
}

func nameInTar(f SourceFileSpec) string {
	if f.NameInTar != "" {
		return f.NameInTar
	}
	return filepath.Base(f.Path)
}

func classifyPackError(err error) FailureReason {
	if err == nil {
		return ReasonBlobStoreUnavailable
	}
	if strings.HasPrefix(err.Error(), "source_missing") {
		return ReasonSourceMissing
	}
	return ReasonBlobStoreUnavailable
}

func (s *Service) emitFailure(ctx context.Context, req ArchiveRequest, blobRef string, reason FailureReason, attempt int, cause error) {
	if s.sink == nil {
		return
	}
	_, _ = s.sink.Emit(ctx, observability.EmitCommand{
		EventType: "observability.trace_archive_failed",
		Refs:      observability.EventRefs{ExecutionID: string(req.ExecutionID), TaskID: req.TaskID},
		Actor:     s.defaultActor,
		Payload: map[string]any{
			"execution_id": string(req.ExecutionID),
			"task_id":      req.TaskID,
			"blob_ref":     blobRef,
			"attempt":      attempt,
			"reason":       string(reason),
			"message":      cause.Error(),
		},
	})
}

// PendingScanner scans a worker-daemon executions root for finished
// executions that lack a trace.uploaded marker — used at daemon startup to
// retry archives that failed previously (plan-4 § 3.4 step 6).
type PendingScanner struct {
	executionsRoot string
	svc            *Service
	clk            clock.Clock
}

// NewPendingScanner constructs a scanner.
func NewPendingScanner(executionsRoot string, svc *Service, clk clock.Clock) *PendingScanner {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &PendingScanner{executionsRoot: executionsRoot, svc: svc, clk: clk}
}

// ScanResult summarises one pending scan pass.
type ScanResult struct {
	Retried   int
	Succeeded int
	Failed    int
}

// Scan walks the executions root. Per ADR-0018 § 4.1: per-execution dir
// holds {events.jsonl, agent.log, terminal.json}. terminal.json is the
// marker — its presence + absence of `uploaded.json` means archive failed
// previously; we retry. Each dir whose archive succeeds gets `uploaded.json`
// written next to it so subsequent scans skip it.
func (s *PendingScanner) Scan(ctx context.Context, deriveRequest func(execDir string) (ArchiveRequest, bool, error), center CenterCallback) (ScanResult, error) {
	var res ScanResult
	entries, err := os.ReadDir(s.executionsRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil
		}
		return res, fmt.Errorf("pending scan: read root: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(s.executionsRoot, e.Name())
		terminalPath := filepath.Join(dir, "terminal.json")
		if _, err := os.Stat(terminalPath); err != nil {
			continue // not terminal yet
		}
		uploadedPath := filepath.Join(dir, "uploaded.json")
		if _, err := os.Stat(uploadedPath); err == nil {
			continue // already uploaded
		}
		req, ok, derr := deriveRequest(dir)
		if derr != nil {
			res.Failed++
			continue
		}
		if !ok {
			continue
		}
		res.Retried++
		_, err := s.svc.Archive(ctx, req, center)
		if err != nil {
			res.Failed++
			continue
		}
		res.Succeeded++
		_ = os.WriteFile(uploadedPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o644)
	}
	return res, nil
}
