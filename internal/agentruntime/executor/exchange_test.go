package executor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
)

func newFx(t *testing.T) (*FileExchange, string) {
	t.Helper()
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := NewFileExchange(layout, clock.NewFakeClock(testNow))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	return fx, root
}

func TestNewLayout_Errors(t *testing.T) {
	if _, err := NewLayout("  "); err == nil {
		t.Fatal("expected error for empty agent root")
	}
}

func TestLayout_Paths(t *testing.T) {
	l, _ := NewLayout("/agent")
	if got := l.ExecutorsDir(); got != "/agent/executors" {
		t.Errorf("ExecutorsDir=%q", got)
	}
	checks := []struct {
		name string
		fn   func(string) (string, error)
		want string
	}{
		{"Dir", l.Dir, "/agent/executors/e1"},
		{"WorkspaceDir", l.WorkspaceDir, "/agent/executors/e1/workspace"},
		{"InputPath", l.InputPath, "/agent/executors/e1/input.json"},
		{"OutputPath", l.OutputPath, "/agent/executors/e1/output.json"},
		{"StatusPath", l.StatusPath, "/agent/executors/e1/status"},
		{"ProgressPath", l.ProgressPath, "/agent/executors/e1/progress.jsonl"},
	}
	for _, c := range checks {
		got, err := c.fn("e1")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s=%q want %q", c.name, got, c.want)
		}
		if _, err := c.fn("../escape"); err == nil {
			t.Errorf("%s should reject traversal id", c.name)
		}
	}
}

func TestNewFileExchange_Errors(t *testing.T) {
	if _, err := NewFileExchange(nil, nil); err == nil {
		t.Fatal("expected error for nil layout")
	}
	l, _ := NewLayout("/agent")
	fx, err := NewFileExchange(l, nil) // nil clock defaults
	if err != nil {
		t.Fatalf("NewFileExchange nil clock: %v", err)
	}
	if fx.Layout() != l {
		t.Fatal("Layout() should return the underlying layout")
	}
}

// TestFullChain exercises the complete acceptance path: orchestrator writes input
// → executor reads it → streams progress + status → writes output + done status →
// orchestrator reads output/status/progress back (design §7 lifecycle).
func TestFullChain(t *testing.T) {
	fx, _ := newFx(t)
	const id = "exec-chain1"

	dir, err := fx.Provision(id)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("provisioned dir missing: %v", err)
	}

	// Orchestrator writes input.
	in := validInput()
	in.ExecutorID = id
	in.Context = "aggregated problem context"
	in.Source = SourceRefs{ChatIDs: []string{"chan-1"}, IssueRef: "issue-9"}
	if err := fx.WriteInput(in); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}

	// Executor reads input.
	got, err := fx.ReadInput(id)
	if err != nil {
		t.Fatalf("ReadInput: %v", err)
	}
	if got.Goal.Title != in.Goal.Title || got.Model != in.Model || got.Source.IssueRef != "issue-9" {
		t.Fatalf("ReadInput mismatch: %+v", got)
	}

	// Executor streams progress + a running status.
	if err := fx.WriteStatus(Status{ExecutorID: id, State: StateRunning, Model: in.Model}); err != nil {
		t.Fatalf("WriteStatus running: %v", err)
	}
	if err := fx.AppendProgress(id, ProgressEntry{Phase: "plan", Message: "planning"}); err != nil {
		t.Fatalf("AppendProgress 1: %v", err)
	}
	if err := fx.AppendProgress(id, ProgressEntry{Phase: "run", Message: "executing"}); err != nil {
		t.Fatalf("AppendProgress 2: %v", err)
	}

	// Executor writes output + done status.
	if err := fx.WriteOutput(Output{ExecutorID: id, Success: true, Result: "all done"}); err != nil {
		t.Fatalf("WriteOutput: %v", err)
	}
	if err := fx.WriteStatus(Status{ExecutorID: id, State: StateDone, Model: in.Model, Summary: "done"}); err != nil {
		t.Fatalf("WriteStatus done: %v", err)
	}

	// Orchestrator reads everything back.
	out, err := fx.ReadOutput(id)
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	if !out.Success || out.Result != "all done" {
		t.Fatalf("ReadOutput mismatch: %+v", out)
	}
	st, err := fx.ReadStatus(id)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if st.State != StateDone || st.Summary != "done" {
		t.Fatalf("ReadStatus mismatch: %+v", st)
	}
	prog, err := fx.ReadProgress(id)
	if err != nil {
		t.Fatalf("ReadProgress: %v", err)
	}
	if len(prog) != 2 || prog[0].Message != "planning" || prog[1].Phase != "run" {
		t.Fatalf("ReadProgress mismatch: %+v", prog)
	}
	// Defaulted timestamps come from the injected clock.
	if !prog[0].At.Equal(testNow) {
		t.Errorf("progress At=%v want %v", prog[0].At, testNow)
	}
	if !out.FinishedAt.Equal(testNow) {
		t.Errorf("output FinishedAt=%v want %v", out.FinishedAt, testNow)
	}
}

func TestReadInput_TaskInputCompletePackageReusable(t *testing.T) {
	fx, root := newFx(t)
	in := validInput()
	in.ExecutorID = "exec-task-input-ok"
	in.TaskInput = &TaskInputRef{Dir: "task-input/v1", ManifestPath: "task-input/v1/manifest.json"}
	mustWriteTaskInputPackage(t, root, in.ExecutorID, "mockup.png", []byte("image bytes"))
	mustWriteInput(t, fx, in)

	first, err := fx.ReadInput(in.ExecutorID)
	if err != nil {
		t.Fatalf("ReadInput first: %v", err)
	}
	second, err := fx.ReadInput(in.ExecutorID)
	if err != nil {
		t.Fatalf("ReadInput second: %v", err)
	}
	if first.TaskInput == nil || second.TaskInput == nil || second.TaskInput.ManifestPath != "task-input/v1/manifest.json" {
		t.Fatalf("task_input ref not preserved across reads: first=%+v second=%+v", first.TaskInput, second.TaskInput)
	}
	if got, err := os.ReadFile(filepath.Join(root, "executors", in.ExecutorID, "workspace", "task-input", "v1", "attachments", "mockup.png")); err != nil || string(got) != "image bytes" {
		t.Fatalf("complete package bytes not reusable: got=%q err=%v", got, err)
	}
}

func TestReadInput_TaskInputRejectsStaleAndIncompletePackages(t *testing.T) {
	tests := []struct {
		name       string
		ref        *TaskInputRef
		packageFn  func(t *testing.T, root, execID string)
		wantErrSub string
	}{
		{
			name:       "stale unversioned ref",
			ref:        &TaskInputRef{Dir: "task-input", ManifestPath: "task-input/manifest.json"},
			packageFn:  func(t *testing.T, root, execID string) { mustWriteLegacyTaskInputPackage(t, root, execID) },
			wantErrSub: "task_input.dir must be task-input/v1",
		},
		{
			name:       "missing manifest",
			ref:        &TaskInputRef{Dir: "task-input/v1", ManifestPath: "task-input/v1/manifest.json"},
			packageFn:  func(t *testing.T, root, execID string) { mustMkdirWorkspace(t, root, execID) },
			wantErrSub: "manifest unreadable",
		},
		{
			name: "missing attachment",
			ref:  &TaskInputRef{Dir: "task-input/v1", ManifestPath: "task-input/v1/manifest.json"},
			packageFn: func(t *testing.T, root, execID string) {
				mustWriteTaskInputManifest(t, root, execID, "attachments/missing.png")
			},
			wantErrSub: "attachments/missing.png",
		},
		{
			name: "manifest path escapes workspace",
			ref:  &TaskInputRef{Dir: "task-input/v1", ManifestPath: "task-input/v1/manifest.json"},
			packageFn: func(t *testing.T, root, execID string) {
				mustWriteTaskInputManifest(t, root, execID, "../escape.png")
			},
			wantErrSub: "must stay under task-input/v1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx, root := newFx(t)
			in := validInput()
			in.ExecutorID = "exec-task-input-bad"
			in.TaskInput = tc.ref
			tc.packageFn(t, root, in.ExecutorID)
			mustWriteInput(t, fx, in)
			_, err := fx.ReadInput(in.ExecutorID)
			if err == nil || !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("ReadInput err=%v want substring %q", err, tc.wantErrSub)
			}
		})
	}
}

func TestWriteStatus_DefaultsTimestamps(t *testing.T) {
	fx, _ := newFx(t)
	if err := fx.WriteStatus(Status{ExecutorID: "e1", State: StateRunning}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	st, err := fx.ReadStatus("e1")
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if !st.StartedAt.Equal(testNow) || !st.LastProgressAt.Equal(testNow) {
		t.Fatalf("defaults not applied: %+v", st)
	}
}

func TestWrite_InvalidRejected(t *testing.T) {
	fx, _ := newFx(t)
	if err := fx.WriteInput(Input{ExecutorID: "e1"}); err == nil {
		t.Fatal("WriteInput should reject invalid input")
	}
	if err := fx.WriteStatus(Status{ExecutorID: "e1", State: "bad"}); err == nil {
		t.Fatal("WriteStatus should reject invalid state")
	}
	if err := fx.WriteOutput(Output{ExecutorID: "e1", Success: false}); err == nil {
		t.Fatal("WriteOutput should reject failure without error")
	}
	if err := fx.AppendProgress("e1", ProgressEntry{}); err == nil {
		t.Fatal("AppendProgress should reject empty message")
	}
}

func TestRead_MissingReturnsNotExist(t *testing.T) {
	fx, _ := newFx(t)
	if _, err := fx.ReadOutput("nope"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadOutput missing err=%v want ErrNotExist", err)
	}
	if _, err := fx.ReadStatus("nope"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadStatus missing err=%v want ErrNotExist", err)
	}
	if _, err := fx.ReadInput("nope"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadInput missing err=%v want ErrNotExist", err)
	}
	prog, err := fx.ReadProgress("nope")
	if err != nil || prog != nil {
		t.Errorf("ReadProgress missing = %v,%v want nil,nil", prog, err)
	}
}

func TestRead_CorruptIsError(t *testing.T) {
	fx, root := newFx(t)
	const id = "exec-corrupt"
	if _, err := fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	write := func(leaf, body string) {
		if err := os.WriteFile(filepath.Join(root, "executors", id, leaf), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("output.json", "{not json")
	if _, err := fx.ReadOutput(id); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("corrupt output should be a non-NotExist error, got %v", err)
	}
	write("status", "{not json")
	if _, err := fx.ReadStatus(id); err == nil {
		t.Errorf("corrupt status should error")
	}
	write("progress.jsonl", `{"at":"2026-06-28T10:00:00Z","message":"ok"}`+"\n{bad line}\n")
	if _, err := fx.ReadProgress(id); err == nil {
		t.Errorf("corrupt progress line should error")
	}
	write("input.json", "{nope")
	if _, err := fx.ReadInput(id); err == nil {
		t.Errorf("corrupt input should error")
	}
}

func TestRead_ValidationFailure(t *testing.T) {
	fx, root := newFx(t)
	const id = "exec-invalid"
	if _, err := fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	// Well-formed JSON but violates protocol invariants (failed status, no error).
	body := `{"executor_id":"exec-invalid","state":"failed","started_at":"2026-06-28T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(root, "executors", id, "status"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.ReadStatus(id); err == nil {
		t.Fatal("ReadStatus should reject a status that fails Validate")
	}
}

func TestScan(t *testing.T) {
	fx, root := newFx(t)

	// e1: full lifecycle (input + done status + output).
	in := validInput()
	in.ExecutorID = "exec-done"
	mustWriteInput(t, fx, in)
	must(t, fx.WriteStatus(Status{ExecutorID: "exec-done", State: StateDone, Model: in.Model}))
	must(t, fx.WriteOutput(Output{ExecutorID: "exec-done", Success: true, Result: "r"}))

	// e2: running, input + status, no output yet.
	in2 := validInput()
	in2.ExecutorID = "exec-running"
	mustWriteInput(t, fx, in2)
	must(t, fx.WriteStatus(Status{ExecutorID: "exec-running", State: StateRunning, Model: in2.Model}))

	// Noise that must be ignored: a non-dir, a dotfile dir, and a junk-named dir.
	if err := os.WriteFile(filepath.Join(root, "executors", "loose.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "executors", ".hidden"), 0o700); err != nil {
		t.Fatal(err)
	}

	snaps, err := fx.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	byID := map[string]Snapshot{}
	for _, s := range snaps {
		byID[s.ExecutorID] = s
	}
	if len(byID) != 2 {
		t.Fatalf("Scan returned %d snapshots, want 2: %+v", len(snaps), snaps)
	}
	done := byID["exec-done"]
	if done.Input == nil || done.Status == nil || !done.HasOutput || done.Output == nil {
		t.Fatalf("exec-done snapshot incomplete: %+v", done)
	}
	if done.Status.State != StateDone {
		t.Errorf("exec-done state=%v", done.Status.State)
	}
	run := byID["exec-running"]
	if run.Input == nil || run.Status == nil || run.HasOutput {
		t.Fatalf("exec-running snapshot wrong: %+v", run)
	}
}

func TestScan_MissingDirIsEmpty(t *testing.T) {
	fx, _ := newFx(t) // executors/ never created
	snaps, err := fx.Scan()
	if err != nil || snaps != nil {
		t.Fatalf("Scan empty = %v,%v want nil,nil", snaps, err)
	}
}

func TestScan_CorruptInputStillReported(t *testing.T) {
	fx, root := newFx(t)
	const id = "exec-partial"
	if _, err := fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "executors", id, "input.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	snaps, err := fx.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(snaps) != 1 || snaps[0].ExecutorID != id || snaps[0].Input != nil {
		t.Fatalf("partial dir should be reported with nil Input: %+v", snaps)
	}
}

func TestRemove(t *testing.T) {
	fx, root := newFx(t)
	const id = "exec-rm"
	if _, err := fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	if err := fx.Remove(id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "executors", id)); !os.IsNotExist(err) {
		t.Fatalf("dir still exists after Remove: %v", err)
	}
	// Idempotent: removing a non-existent dir is fine.
	if err := fx.Remove(id); err != nil {
		t.Fatalf("Remove idempotent: %v", err)
	}
	// Bad id is rejected.
	if err := fx.Remove("../escape"); err == nil {
		t.Fatal("Remove should reject traversal id")
	}
}

func TestContainedPath(t *testing.T) {
	fx, _ := newFx(t)
	const id = "exec-contain"
	ws, _ := fx.layout.WorkspaceDir(id)
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	// Existing file inside workspace (mustExist=true) resolves.
	inside := filepath.Join(ws, "file.txt")
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := fx.ContainedPath(id, "file.txt", true)
	if err != nil {
		t.Fatalf("ContainedPath inside: %v", err)
	}
	if filepath.Base(got) != "file.txt" {
		t.Errorf("resolved=%q", got)
	}
	// New file (mustExist=false) under workspace is allowed.
	if _, err := fx.ContainedPath(id, "subdir/new.txt", false); err == nil {
		// parent subdir doesn't exist → error expected for mustExist=false (parent must exist)
		t.Errorf("expected error: parent subdir missing")
	}
	if _, err := fx.ContainedPath(id, "new.txt", false); err != nil {
		t.Errorf("ContainedPath new leaf: %v", err)
	}
	// Traversal to an existing dir outside the workspace is refused with the sentinel.
	if _, err := fx.ContainedPath(id, "..", false); !errors.Is(err, ErrPathEscapesWorkspace) {
		t.Errorf("traversal err=%v want ErrPathEscapesWorkspace", err)
	}
	// Deeper traversal into a non-existent outside tree is still refused (any error).
	if _, err := fx.ContainedPath(id, "../../etc/passwd", false); err == nil {
		t.Errorf("deep traversal should be refused")
	}
	// Absolute path outside is refused.
	if _, err := fx.ContainedPath(id, "/etc/passwd", true); err == nil {
		t.Errorf("absolute outside should be refused")
	}
	// Empty path is refused.
	if _, err := fx.ContainedPath(id, "  ", true); err == nil {
		t.Errorf("empty path should be refused")
	}
}

func TestContainedPath_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	fx, _ := newFx(t)
	const id = "exec-symlink"
	ws, _ := fx.layout.WorkspaceDir(id)
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.ContainedPath(id, "escape", true); !errors.Is(err, ErrPathEscapesWorkspace) {
		t.Errorf("symlink escape err=%v want ErrPathEscapesWorkspace", err)
	}
}

func TestResolveContained_RootMissing(t *testing.T) {
	// A workspace root that doesn't exist yet fails on EvalSymlinks(root).
	if _, err := resolveContained(filepath.Join(t.TempDir(), "nope"), "x", true); err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestResolveWithin(t *testing.T) {
	root := t.TempDir()
	// Child under an as-yet-nonexistent subtree is still judged within.
	child := filepath.Join(root, "executors", "e1")
	got, err := resolveWithin(filepath.Join(root, "executors"), child)
	if err != nil {
		t.Fatalf("resolveWithin: %v", err)
	}
	// Returned path has symlinked ancestors resolved (e.g. macOS /var → /private/var).
	if want := evalExistingPrefix(filepath.Clean(child)); got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// Escape is refused.
	if _, err := resolveWithin(filepath.Join(root, "executors"), filepath.Join(root, "other")); !errors.Is(err, ErrPathEscapesWorkspace) {
		t.Errorf("escape err=%v want ErrPathEscapesWorkspace", err)
	}
	// Empty root refused.
	if _, err := resolveWithin("  ", child); err == nil {
		t.Error("empty root should be refused")
	}
	// Root not yet created is tolerated (compared as cleaned).
	missingRoot := filepath.Join(t.TempDir(), "notcreated")
	if _, err := resolveWithin(missingRoot, filepath.Join(missingRoot, "x")); err != nil {
		t.Errorf("missing root within: %v", err)
	}
}

func mustWriteInput(t *testing.T, fx *FileExchange, in Input) {
	t.Helper()
	if _, err := fx.Provision(in.ExecutorID); err != nil {
		t.Fatal(err)
	}
	must(t, fx.WriteInput(in))
}

func mustMkdirWorkspace(t *testing.T, root, execID string) string {
	t.Helper()
	workspace := filepath.Join(root, "executors", execID, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func mustWriteTaskInputPackage(t *testing.T, root, execID, name string, body []byte) {
	t.Helper()
	workspace := mustMkdirWorkspace(t, root, execID)
	attDir := filepath.Join(workspace, "task-input", "v1", "attachments")
	if err := os.MkdirAll(attDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attDir, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
	mustWriteTaskInputManifest(t, root, execID, "attachments/"+name)
}

func mustWriteTaskInputManifest(t *testing.T, root, execID, attachmentRel string) {
	t.Helper()
	workspace := mustMkdirWorkspace(t, root, execID)
	versionDir := filepath.Join(workspace, "task-input", "v1")
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := "task-input/v1/" + attachmentRel
	doc := map[string]any{
		"version": 1,
		"task_id": "task-1",
		"files": []map[string]any{{
			"uri": "ac://files/1", "name": filepath.Base(attachmentRel), "path": path,
			"source_scope": "task", "source_id": "task-1", "canonical": true, "required": true,
		}},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "manifest.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustWriteLegacyTaskInputPackage(t *testing.T, root, execID string) {
	t.Helper()
	workspace := mustMkdirWorkspace(t, root, execID)
	legacyDir := filepath.Join(workspace, "task-input")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "manifest.json"), []byte(`{"version":1,"files":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
