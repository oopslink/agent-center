package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// Layout file / dir names — the file-exchange wire contract (design §7). These
// strings are part of the protocol both processes agree on; do not rename.
const (
	executorsDirName = "executors"
	workspaceDirName = "workspace" // the executor's git worktree (see package doc, conventions §12)
	inputFileName    = "input.json"
	outputFileName   = "output.json"
	statusFileName   = "status"
	progressFileName = "progress.jsonl"
	// finalizedFileName marks a TERMINAL executor whose teardown is DEFERRED (the
	// k8s TTL-after-finished analog): Finalize retains the dir/worktree and stamps
	// this file; the periodic reaper removes it after the TTL / when the retained
	// count exceeds the cap. Its presence = "this terminal executor is retained".
	finalizedFileName = "finalized"
)

// ErrPathEscapesWorkspace is returned when a path resolves outside the executor's
// workspace worktree (".." traversal, an absolute path outside, or a symlink that
// points out). Callers errors.Is against it to refuse the access (the design §6.D
// containment guard: an executor must never escape its own worktree). Mirrors the
// daemon file_transfer guard's sentinel.
var ErrPathEscapesWorkspace = errors.New("executor: path escapes workspace worktree")

// validateExecutorID rejects any id that could escape the executors/ parent when
// joined into a path. Mirrors taskexec.validatePathComponent — an executor id is
// a single path segment, never a traversal.
func validateExecutorID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("executor: executor_id required")
	}
	if strings.ContainsAny(id, "/\\:") {
		return fmt.Errorf("executor: executor_id %q contains path separator", id)
	}
	if strings.Contains(id, "\x00") {
		return fmt.Errorf("executor: executor_id %q contains null byte", id)
	}
	if id == "." || id == ".." || strings.Contains(id, "..") {
		return fmt.Errorf("executor: executor_id %q contains path traversal", id)
	}
	return nil
}

// Layout resolves the per-executor directory tree under an agent root. It is a
// pure path resolver (no IO) so it stays trivially testable; the FileExchange
// builds on it for the actual reads/writes.
type Layout struct {
	agentRoot string
}

// NewLayout anchors a Layout at agentRoot (the per-agent home, e.g.
// <AgentHomeBase>/agents/<agent_id>). The root must be non-empty; it is NOT
// required to exist yet (Provision creates the subtree).
func NewLayout(agentRoot string) (*Layout, error) {
	if strings.TrimSpace(agentRoot) == "" {
		return nil, errors.New("executor: agent_root required")
	}
	return &Layout{agentRoot: agentRoot}, nil
}

// ExecutorsDir is <agent_root>/executors — the parent of every executor dir and
// the directory the orchestrator scans on crash recovery (design §12).
func (l *Layout) ExecutorsDir() string {
	return filepath.Join(l.agentRoot, executorsDirName)
}

// Dir is <agent_root>/executors/<executor_id>.
func (l *Layout) Dir(executorID string) (string, error) {
	if err := validateExecutorID(executorID); err != nil {
		return "", err
	}
	return filepath.Join(l.ExecutorsDir(), executorID), nil
}

// WorkspaceDir is the executor's git worktree (<dir>/workspace).
func (l *Layout) WorkspaceDir(executorID string) (string, error) {
	dir, err := l.Dir(executorID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, workspaceDirName), nil
}

// InputPath is <dir>/input.json.
func (l *Layout) InputPath(executorID string) (string, error) {
	return l.join(executorID, inputFileName)
}

// OutputPath is <dir>/output.json.
func (l *Layout) OutputPath(executorID string) (string, error) {
	return l.join(executorID, outputFileName)
}

// StatusPath is <dir>/status.
func (l *Layout) StatusPath(executorID string) (string, error) {
	return l.join(executorID, statusFileName)
}

// ProgressPath is <dir>/progress.jsonl.
func (l *Layout) ProgressPath(executorID string) (string, error) {
	return l.join(executorID, progressFileName)
}

func (l *Layout) join(executorID, leaf string) (string, error) {
	dir, err := l.Dir(executorID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, leaf), nil
}

// FileExchange performs the orchestrator↔executor file protocol over a Layout.
// One instance is shared by both sides conceptually; in practice the orchestrator
// process calls the orchestrator-side methods and the (separately spawned)
// executor process calls the executor-side methods on its own instance. The
// injected clock stamps protocol timestamps so tests stay deterministic
// (conventions §14.x).
type FileExchange struct {
	layout *Layout
	clk    clock.Clock
}

// NewFileExchange builds a FileExchange over layout. A nil clock defaults to the
// system clock (UTC).
func NewFileExchange(layout *Layout, clk clock.Clock) (*FileExchange, error) {
	if layout == nil {
		return nil, errors.New("executor: layout required")
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &FileExchange{layout: layout, clk: clk}, nil
}

// Layout exposes the underlying path resolver (for callers that need raw paths,
// e.g. to spawn the executor pointed at its dir).
func (fx *FileExchange) Layout() *Layout { return fx.layout }

// -----------------------------------------------------------------------------
// Orchestrator side
// -----------------------------------------------------------------------------

// Provision creates <executors>/<id>/ (0700) so the protocol files (input.json,
// output.json, status, progress.jsonl) have a home. It is idempotent (MkdirAll).
// It deliberately does NOT create workspace/: the git worktree there is
// materialised by a WorktreeProvisioner (worktree.go), and `git worktree add`
// wants to create that leaf itself.
func (fx *FileExchange) Provision(executorID string) (dir string, err error) {
	dir, err = fx.layout.Dir(executorID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("executor: mkdir %q: %w", dir, err)
	}
	return dir, nil
}

// WriteInput validates and atomically writes input.json (orchestrator → executor).
func (fx *FileExchange) WriteInput(in Input) error {
	if err := in.Validate(); err != nil {
		return err
	}
	path, err := fx.layout.InputPath(in.ExecutorID)
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, in)
}

// ReadOutput reads + validates output.json (executor → orchestrator). A missing
// file returns os.ErrNotExist unwrapped-matchable (errors.Is) so the orchestrator
// can distinguish "not done yet" from a corrupt/invalid output (design §9).
func (fx *FileExchange) ReadOutput(executorID string) (Output, error) {
	path, err := fx.layout.OutputPath(executorID)
	if err != nil {
		return Output{}, err
	}
	var out Output
	if err := readJSON(path, &out); err != nil {
		return Output{}, mapReadErr("output.json", executorID, err)
	}
	if err := out.Validate(); err != nil {
		return Output{}, fmt.Errorf("executor: output.json for %s invalid: %w", executorID, err)
	}
	return out, nil
}

// ReadStatus reads + validates the status file. A missing file returns
// os.ErrNotExist (errors.Is-matchable).
func (fx *FileExchange) ReadStatus(executorID string) (Status, error) {
	path, err := fx.layout.StatusPath(executorID)
	if err != nil {
		return Status{}, err
	}
	var st Status
	if err := readJSON(path, &st); err != nil {
		return Status{}, mapReadErr("status", executorID, err)
	}
	if err := st.Validate(); err != nil {
		return Status{}, fmt.Errorf("executor: status for %s invalid: %w", executorID, err)
	}
	return st, nil
}

// ReadProgress parses progress.jsonl into entries (executor → orchestrator). A
// missing file yields an empty slice + nil error (no progress streamed yet). A
// malformed line is surfaced as an error (conventions §17: never silently skip an
// unparseable protocol record) carrying the 1-based line number.
func (fx *FileExchange) ReadProgress(executorID string) ([]ProgressEntry, error) {
	path, err := fx.layout.ProgressPath(executorID)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("executor: read progress.jsonl for %s: %w", executorID, err)
	}
	var entries []ProgressEntry
	for i, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e ProgressEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("executor: progress.jsonl for %s line %d corrupt: %w", executorID, i+1, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Snapshot is the orchestrator's reconstructed view of one executor dir, built by
// Scan for crash recovery (design §12: files are the durable state). Input is nil
// if input.json is missing/corrupt; Status/Output likewise. HasOutput reports
// whether output.json was present + valid.
type Snapshot struct {
	ExecutorID string
	Input      *Input
	Status     *Status
	Output     *Output
	HasOutput  bool
}

// Scan rebuilds the orchestrator's in-flight state by reading every executor dir
// under executors/ (design §12). It is best-effort per dir: a dir whose input is
// unreadable is still reported (with nil Input) so the orchestrator can decide to
// clean it up rather than have it vanish — unknown/partial dirs surface, never
// silently dropped (conventions §17). A missing executors/ dir yields nil, nil.
func (fx *FileExchange) Scan() ([]Snapshot, error) {
	root := fx.layout.ExecutorsDir()
	ents, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("executor: scan %q: %w", root, err)
	}
	var snaps []Snapshot
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if validateExecutorID(id) != nil || strings.HasPrefix(id, ".") {
			continue // not a protocol executor dir
		}
		snap := Snapshot{ExecutorID: id}
		if in, err := fx.readInputBestEffort(id); err == nil {
			snap.Input = &in
		}
		if st, err := fx.ReadStatus(id); err == nil {
			snap.Status = &st
		}
		if out, err := fx.ReadOutput(id); err == nil {
			snap.Output = &out
			snap.HasOutput = true
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}

func (fx *FileExchange) readInputBestEffort(executorID string) (Input, error) {
	path, err := fx.layout.InputPath(executorID)
	if err != nil {
		return Input{}, err
	}
	var in Input
	if err := readJSON(path, &in); err != nil {
		return Input{}, err
	}
	if err := in.Validate(); err != nil {
		return Input{}, err
	}
	return in, nil
}

// Remove deletes the entire executor dir (orchestrator cleanup after a finished
// executor, design §7 step h). Containment-guarded: the resolved dir MUST sit
// inside executors/ — a corrupt id that escaped would be refused before any
// RemoveAll. Removing a non-existent dir is a no-op (idempotent).
func (fx *FileExchange) Remove(executorID string) error {
	dir, err := fx.layout.Dir(executorID)
	if err != nil {
		return err
	}
	contained, err := resolveWithin(fx.layout.ExecutorsDir(), dir)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(contained); err != nil {
		return fmt.Errorf("executor: remove %q: %w", contained, err)
	}
	return nil
}

// FinalizedRef is a terminal executor whose teardown was deferred: its id + the
// time Finalize stamped it (the reaper's TTL/cap key).
type FinalizedRef struct {
	ExecutorID string
	At         time.Time
}

type finalizedMarker struct {
	FinalizedAt string `json:"finalized_at"`
}

// FinalizedPath is <dir>/finalized.
func (l *Layout) FinalizedPath(executorID string) (string, error) {
	return l.join(executorID, finalizedFileName)
}

// MarkFinalized stamps the executor dir as terminal-retained at `at` (delayed
// teardown). Written atomically so a concurrent reaper never reads a torn marker.
func (fx *FileExchange) MarkFinalized(executorID string, at time.Time) error {
	path, err := fx.layout.FinalizedPath(executorID)
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, finalizedMarker{FinalizedAt: at.UTC().Format(time.RFC3339Nano)})
}

// ListFinalized returns every retained-terminal executor (those carrying a
// `finalized` marker) with its stamp — the reaper's input. A dir without the marker
// (live / retryable-crash-retained) is skipped; an unreadable/corrupt marker is
// treated as finalized at time zero so it reaps promptly (never leaks).
func (fx *FileExchange) ListFinalized() ([]FinalizedRef, error) {
	root := fx.layout.ExecutorsDir()
	ents, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("executor: list finalized %q: %w", root, err)
	}
	var out []FinalizedRef
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if validateExecutorID(id) != nil || strings.HasPrefix(id, ".") {
			continue
		}
		path, err := fx.layout.FinalizedPath(id)
		if err != nil {
			continue
		}
		var m finalizedMarker
		if err := readJSON(path, &m); err != nil {
			continue // no marker (or unreadable) → not a retained-terminal dir
		}
		at, perr := time.Parse(time.RFC3339Nano, m.FinalizedAt)
		if perr != nil {
			at = time.Time{} // corrupt stamp → reap promptly
		}
		out = append(out, FinalizedRef{ExecutorID: id, At: at})
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Executor side
// -----------------------------------------------------------------------------

// ReadInput reads + validates input.json (the executor's first act).
func (fx *FileExchange) ReadInput(executorID string) (Input, error) {
	in, err := fx.readInputBestEffort(executorID)
	if err != nil {
		return Input{}, mapReadErr("input.json", executorID, err)
	}
	return in, nil
}

// AppendProgress appends one entry to progress.jsonl (append-only, design §7). If
// the entry's At is zero it is stamped from the injected clock so callers can omit
// it. The write is a single O_APPEND line so concurrent-safe enough for the one
// writer the protocol assumes (the executor itself).
func (fx *FileExchange) AppendProgress(executorID string, e ProgressEntry) error {
	if e.At.IsZero() {
		e.At = fx.clk.Now().UTC()
	}
	if err := e.Validate(); err != nil {
		return err
	}
	path, err := fx.layout.ProgressPath(executorID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("executor: mkdir for progress %q: %w", path, err)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("executor: marshal progress: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("executor: open progress %q: %w", path, err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("executor: append progress %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("executor: close progress %q: %w", path, err)
	}
	return nil
}

// WriteStatus validates and atomically writes the status file. If StartedAt is
// zero it is stamped now; LastProgressAt defaults to StartedAt when zero, so a
// freshly-started executor always has a coherent watchdog timestamp.
func (fx *FileExchange) WriteStatus(st Status) error {
	now := fx.clk.Now().UTC()
	if st.StartedAt.IsZero() {
		st.StartedAt = now
	}
	if st.LastProgressAt.IsZero() {
		st.LastProgressAt = st.StartedAt
	}
	if err := st.Validate(); err != nil {
		return err
	}
	path, err := fx.layout.StatusPath(st.ExecutorID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("executor: mkdir for status %q: %w", path, err)
	}
	return writeJSONAtomic(path, st)
}

// WriteOutput validates and atomically writes output.json (the executor's last
// act on success/failure). FinishedAt defaults to now when zero.
func (fx *FileExchange) WriteOutput(out Output) error {
	if out.FinishedAt.IsZero() {
		out.FinishedAt = fx.clk.Now().UTC()
	}
	if err := out.Validate(); err != nil {
		return err
	}
	path, err := fx.layout.OutputPath(out.ExecutorID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("executor: mkdir for output %q: %w", path, err)
	}
	return writeJSONAtomic(path, out)
}

// ContainedPath resolves userPath against the executor's workspace worktree and
// guarantees the result stays inside it (design §6.D containment guard). It is
// the seam the executor uses for EVERY file access so it can never escape its
// worktree. Defends against ".." traversal, absolute paths outside the worktree,
// and symlink escape — identical strategy to the daemon file_transfer guard.
//
// mustExist selects the eval strategy: true derefs the whole path (the leaf must
// exist) so an in-worktree symlink to /etc/passwd is dereferenced then rejected;
// false derefs the parent (the leaf may be a not-yet-created file) so a symlinked
// parent escape is still caught while a new file is allowed.
func (fx *FileExchange) ContainedPath(executorID, userPath string, mustExist bool) (string, error) {
	ws, err := fx.layout.WorkspaceDir(executorID)
	if err != nil {
		return "", err
	}
	return resolveContained(ws, userPath, mustExist)
}

// -----------------------------------------------------------------------------
// shared filesystem helpers
// -----------------------------------------------------------------------------

// writeJSONAtomic marshals v (indented) and writes path via temp-file + rename so
// a crash mid-write never leaves a torn protocol file (mirrors taskexec /
// sessioninstance). The O_NOFOLLOW open is a race-free backstop against the final
// path being swapped to a symlink between rename target resolution and write.
func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("executor: mkdir %q: %w", filepath.Dir(path), err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("executor: marshal %q: %w", path, err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("executor: open tmp %q: %w", tmp, err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("executor: write tmp %q: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("executor: close tmp %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("executor: rename %q: %w", path, err)
	}
	return nil
}

// readJSON reads + unmarshals path. A missing file returns os.ErrNotExist
// unwrapped (errors.Is-matchable); a present-but-corrupt file is an error, never
// silently zeroed (mirrors sessioninstance.ReadInstance).
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err // os.ErrNotExist stays errors.Is-matchable
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("corrupt %s: %w", path, err)
	}
	return nil
}

// mapReadErr keeps a missing file as os.ErrNotExist (so the orchestrator can test
// "not written yet"), and wraps anything else with protocol context.
func mapReadErr(what, executorID string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fmt.Errorf("executor: read %s for %s: %w", what, executorID, err)
}
