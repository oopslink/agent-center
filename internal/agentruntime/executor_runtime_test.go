package agentruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/modelrouter"
	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
	"github.com/oopslink/agent-center/internal/agentruntime/taskexec"
)

func TestBuildExecutorEngine_ErrorOnBadRoot(t *testing.T) {
	rt := newExecRuntime(t, t.TempDir(), "agent-bad", "true")
	if _, err := rt.BuildExecutorEngine("", ExecutorConfig{MaxConcurrentTasks: 1, AllowedExecutors: testExecs}); err == nil {
		t.Error("empty agent root must surface an error")
	}
}

// TestCodexAuthPreflight is the unit lock for the T962 HARD acceptance point (pd):
// a cli=codex executor with missing $CODEX_HOME/auth.json must fail LOUD (warn), never
// silently fork-fail — the same config-source-missing discipline as the T950 judge P1.
func TestCodexAuthPreflight(t *testing.T) {
	ok := func(string) error { return nil }
	missing := func(string) error { return os.ErrNotExist }

	// A non-codex cli never warns (claude auth is a different namespace).
	if msg, warn := codexAuthPreflight("claude-code", "", missing); warn {
		t.Errorf("claude cli must not codex-warn; got %q", msg)
	}
	// codex + CODEX_HOME unset → loud warn naming CODEX_HOME.
	if msg, warn := codexAuthPreflight(CLICodex, "", ok); !warn || !strings.Contains(msg, "CODEX_HOME") {
		t.Errorf("codex + unset CODEX_HOME must warn about CODEX_HOME; got warn=%v msg=%q", warn, msg)
	}
	// codex + CODEX_HOME set but auth.json missing → loud warn naming auth.json.
	if msg, warn := codexAuthPreflight(CLICodex, "/home/agent/.codex", missing); !warn || !strings.Contains(msg, "auth.json") {
		t.Errorf("codex + missing auth.json must warn about auth.json; got warn=%v msg=%q", warn, msg)
	}
	// codex + CODEX_HOME set + auth.json present → healthy, no warn.
	if msg, warn := codexAuthPreflight(CLICodex, "/home/agent/.codex", ok); warn {
		t.Errorf("healthy codex auth must not warn; got %q", msg)
	}
}

// TestBuildExecutorEngine_JudgeEnabledButNoModel_WarnsLoud is the FAIL-LOUD half of the
// tester3 P1 fix: when judge_enabled=true but no orchestrator_model is resolvable the
// judge can't build, and BuildExecutorEngine MUST emit a LOUD warning rather than
// silently fall back (a no-signal inert switch is what shipped the original bug). Note
// ClaudeBinary is left EMPTY here (the deployment default) — that alone no longer
// disables the judge, so the only disable reason left is the missing model.
func TestBuildExecutorEngine_JudgeEnabledButNoModel_WarnsLoud(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	agentID := "agent-judgewarn"
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID:       agentID,
		Reporter:      &nopReporter{},
		WorkerID:      "w-1",
		AgentHomeBase: t.TempDir(),
		BinaryPath:    lookTrue(t),
		// ClaudeBinary intentionally empty = the deployment default (unset env).
		Log: func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() },
	}, &SessionState{})
	home, _, _, err := rt.agentPaths(agentID)
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	if _, err := rt.BuildExecutorEngine(home, ExecutorConfig{
		AgentID: agentID, JudgeEnabled: true, MaxConcurrentTasks: 1, // no OrchestratorModel → judge nil
	}); err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	mu.Lock()
	joined := strings.Join(logs, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "judge_enabled=true but judge DISABLED") {
		t.Errorf("want loud warn for enabled-but-no-orchestrator-model judge; got logs:\n%s", joined)
	}
}

func TestDrainExecutor_NilGuards(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-nilguard")
	rt.drainExecutor(nil, "", nil) // must not panic
	rt.drainExecutor(ee, "", nil)  // nil handle → no-op
}

func TestBuildExecutorEngine_ForksAndDrainFreesSlot(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-x")
	if ee.engine.Pool().Max() != 2 {
		t.Fatalf("pool max = %d, want 2", ee.engine.Pool().Max())
	}
	launched, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
		TaskRef: "task-1", Goal: executor.Goal{Title: "do it"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	if launched.Model != "claude-default" {
		t.Errorf("model = %q, want claude-default (chain fallback)", launched.Model)
	}
	if ee.engine.Pool().Active() != 1 {
		t.Fatalf("active = %d, want 1 after fork", ee.engine.Pool().Active())
	}
	rt.drainExecutor(ee, "", launched.Handle)
	if ee.engine.Pool().Active() != 0 {
		t.Errorf("active = %d, want 0 after drain", ee.engine.Pool().Active())
	}
}

func TestWorkViaExecutor_ForksAndRegistersRouting(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-y")
	attach(rt, ee)

	err := rt.workViaExecutor(context.Background(), WorkRequest{
		AgentID: "agent-y", TaskID: "t-1", TaskRef: "task-1", Brief: "fix the thing\nmore detail",
	}, ee)
	if err != nil {
		t.Fatalf("workViaExecutor: %v", err)
	}
	// Routing.json persists past the (async) drain.
	if probs := loadRouting(t, home); len(probs) != 1 || len(probs[0].TaskRefs) == 0 || probs[0].TaskRefs[0] != "task-1" {
		t.Errorf("expected one problem bound to task-1, got %+v", probs)
	}
	if got := rt.CurrentTaskID(); got != "t-1" {
		t.Errorf("currentTaskID = %q, want t-1", got)
	}
}

// errRunnerWD makes the engine's runner build fail, exercising the non-capacity
// error branch.
type errRunnerWD struct{}

func (errRunnerWD) Build(string, string, string) ([]string, error) {
	return nil, errors.New("runner boom")
}

func TestWorkViaExecutor_NonCapacityErrorWraps(t *testing.T) {
	trueBin := lookTrue(t)
	home := t.TempDir()
	rt := newExecRuntime(t, t.TempDir(), "a", trueBin)
	layout, _ := executor.NewLayout(home)
	fx, _ := executor.NewFileExchange(layout, nil)
	pool, _ := executor.NewPool(executor.PoolConfig{Exchange: fx, Spawner: executor.NewSpawner(), AgentRoot: home, BinaryPath: trueBin, Max: 2})
	routing, _ := executor.NewRoutingStore(home, nil)
	eng, _ := orchestrator.NewEngine(orchestrator.EngineConfig{
		Pool: pool, Routing: routing, Router: modelrouter.NewRouter(nil),
		RouterConfig: modelrouter.Config{DefaultExecutorModel: "m"},
		Runners:      map[string]orchestrator.RunnerCmdBuilder{"claude-code": errRunnerWD{}},
		IDs:          orchestrator.NewULIDMinter(nil),
	})
	mon, _ := executor.NewMonitor(executor.MonitorConfig{Exchange: fx, Pool: pool})
	ee := &ExecutorEngine{engine: eng, monitor: mon}

	err := rt.workViaExecutor(context.Background(), WorkRequest{AgentID: "a", TaskID: "t", Brief: "do"}, ee)
	if err == nil || errors.Is(err, executor.ErrAtCapacity) {
		t.Fatalf("expected a non-capacity fork error, got %v", err)
	}
	if !strings.Contains(err.Error(), "fork executor") {
		t.Errorf("expected wrapped fork-executor error, got %v", err)
	}
}

func TestWorkViaExecutor_AtCapacityRetryable(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-z") // max 2
	for i := 0; i < 2; i++ {
		if _, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
			TaskRef: "task-" + string(rune('a'+i)), Goal: executor.Goal{Title: "g"},
		}); err != nil {
			t.Fatalf("saturating launch %d: %v", i, err)
		}
	}
	err := rt.workViaExecutor(context.Background(), WorkRequest{AgentID: "agent-z", TaskID: "t3", TaskRef: "task-c", Brief: "x"}, ee)
	if err == nil {
		t.Fatal("expected a retryable at-capacity error")
	}
	if !errors.Is(err, executor.ErrAtCapacity) {
		t.Errorf("expected at-capacity error, got %v", err)
	}
}

func TestSpawnExecutor_MaterializesTaskInputPlan569CanonicalMockups(t *testing.T) {
	rt, ee, home := spawnRuntime(t, "agent-att")
	pngBytes := testImageBytes(t, "png", 2, 3)
	jpegBytes := testImageBytes(t, "jpeg", 4, 5)
	mobileBytes := testImageBytes(t, "png", 6, 7)
	txtBytes := []byte("plain contract file")
	sc := &scriptedToolCaller{
		getTaskBody: map[string]any{"id": "task-att", "title": "plan-569ab651 canonical mockups", "status": "open"},
		listFilesBody: map[string]any{"files": []map[string]any{
			{"uri": "ac://files/png1", "filename": "mockup.png", "mime_type": "image/png", "size": len(pngBytes)},
			{"uri": "ac://files/jpg1", "filename": "mockup.png", "mime_type": "image/jpeg", "size": len(jpegBytes)},
			{"uri": "ac://files/png2", "filename": "mobile-mockup.png", "mime_type": "image/png", "size": len(mobileBytes)},
			{"uri": "ac://files/txt1", "filename": "../notes.txt", "mime_type": "text/plain", "size": len(txtBytes)},
		}},
		downloads: map[string][]byte{
			"ac://files/png1": pngBytes,
			"ac://files/jpg1": jpegBytes,
			"ac://files/png2": mobileBytes,
			"ac://files/txt1": txtBytes,
		},
	}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-att"})
	if err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}
	if res == nil || res.ExecutorID == "" {
		t.Fatalf("executor not launched: %+v", res)
	}
	assertAdmissionForked(t, sc, "materialized fork")

	in, err := ee.fx.ReadInput(res.ExecutorID)
	if err != nil {
		t.Fatalf("ReadInput: %v", err)
	}
	if in.TaskInput == nil || in.TaskInput.Dir != "task-input/v1" {
		t.Fatalf("input task_input not wired: %+v", in.TaskInput)
	}
	manifestPath := filepath.Join(home, "executors", res.ExecutorID, "workspace", "task-input", "v1", "manifest.json")
	var manifest taskInputManifest
	readJSONTest(t, manifestPath, &manifest)
	if len(manifest.Files) != 4 {
		t.Fatalf("manifest files=%d want 4: %+v", len(manifest.Files), manifest.Files)
	}
	byURI := map[string]taskInputManifestFile{}
	for _, f := range manifest.Files {
		byURI[f.URI] = f
		if !f.Canonical || !f.Required || f.SourceScope != "task" || f.SourceID != "task-att" {
			t.Fatalf("manifest source flags wrong: %+v", f)
		}
		if strings.Contains(f.Path, "..") {
			t.Fatalf("manifest path traverses: %q", f.Path)
		}
	}
	assertManifestFile(t, home, res.ExecutorID, byURI["ac://files/png1"], pngBytes, 2, 3)
	assertManifestFile(t, home, res.ExecutorID, byURI["ac://files/jpg1"], jpegBytes, 4, 5)
	assertManifestFile(t, home, res.ExecutorID, byURI["ac://files/png2"], mobileBytes, 6, 7)
	assertManifestFile(t, home, res.ExecutorID, byURI["ac://files/txt1"], txtBytes, 0, 0)
	if byURI["ac://files/png1"].Path == byURI["ac://files/jpg1"].Path {
		t.Fatalf("same-name attachments were not disambiguated: %q", byURI["ac://files/png1"].Path)
	}
	_, tracker := seedExchange(t, home)
	rec, err := tracker.Read(res.ExecutorID)
	if err != nil {
		t.Fatalf("tracker read: %v", err)
	}
	if got := strings.Join(rec.RunnerCmd, "\n"); !strings.Contains(got, "task-input/v1/README.md") || !strings.Contains(got, "task-input/v1/manifest.json") {
		t.Fatalf("runner prompt does not point at task-input package: %s", got)
	}
}

func TestSpawnExecutor_TaskInputDownloadFailureDoesNotStartOrFork(t *testing.T) {
	rt, _, home := spawnRuntime(t, "agent-att-fail")
	sc := &scriptedToolCaller{
		getTaskBody:   map[string]any{"id": "task-fail", "title": "Use mockups", "status": "open"},
		listFilesBody: map[string]any{"files": []map[string]any{{"uri": "ac://files/missing", "filename": "mock.png", "mime_type": "image/png", "size": 10}}},
		downloadErr:   errors.New("403 file_not_reachable"),
	}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-fail"})
	if err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}
	if res == nil || res.CommandStatus != controlCommandStatusFailed || res.Reason != "task_input_materialization_failed" {
		t.Fatalf("result = %+v, want task_input_materialization_failed", res)
	}
	if got := sc.toolsSeen(); strings.Join(got, ",") != "get_task,list_files" {
		t.Fatalf("tool calls = %v, want no start_task/block_task after materialization failure", got)
	}
	inputs, _ := filepath.Glob(filepath.Join(home, "executors", "*", "input.json"))
	if len(inputs) != 0 {
		t.Fatalf("input files = %v, want none (executor not forked)", inputs)
	}
}

func TestTaskInput_RetryReplacesLegacyAndPartialPackage(t *testing.T) {
	rt := newExecRuntime(t, t.TempDir(), "agent-retry", lookTrue(t))
	pngBytes := testImageBytes(t, "png", 3, 4)
	sc := &scriptedToolCaller{
		listFilesBody: map[string]any{"files": []map[string]any{{
			"uri": "ac://files/retry", "filename": "retry.png", "mime_type": "image/png", "size": len(pngBytes),
		}}},
		downloads:   map[string][]byte{"ac://files/retry": pngBytes},
		downloadErr: errors.New("transient download failure"),
	}
	setToolCaller(rt, sc)
	workspace := t.TempDir()
	task := &centerTaskDetail{ID: "task-retry", Title: "retry package"}
	if _, err := rt.materializeTaskInputPackage(context.Background(), "agent-retry", task.ID, "exec-first", workspace, task); err == nil {
		t.Fatal("first materialization must fail")
	}
	if _, err := os.Stat(filepath.Join(workspace, taskInputDirName)); !os.IsNotExist(err) {
		t.Fatalf("failed attempt published task-input: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "."+taskInputDirName+".tmp")); !os.IsNotExist(err) {
		t.Fatalf("failed attempt leaked staging package: err=%v", err)
	}

	legacy := filepath.Join(workspace, taskInputDirName)
	if err := os.MkdirAll(filepath.Join(legacy, "attachments"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "manifest.json"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(workspace, "."+taskInputDirName+".tmp", taskInputVersion)
	if err := os.MkdirAll(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "half.download"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	sc.downloadErr = nil
	ref, err := rt.materializeTaskInputPackage(context.Background(), "agent-retry", task.ID, "exec-second", workspace, task)
	if err != nil {
		t.Fatalf("retry materialization: %v", err)
	}
	if ref.Dir != "task-input/v1" || ref.ManifestPath != "task-input/v1/manifest.json" {
		t.Fatalf("versioned ref = %+v", ref)
	}
	if _, err := os.Stat(filepath.Join(legacy, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy package survived retry: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "."+taskInputDirName+".tmp")); !os.IsNotExist(err) {
		t.Fatalf("partial staging package survived retry: err=%v", err)
	}
	got, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(ref.Dir), "attachments", "retry.png"))
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("retry attachment mismatch: err=%v", err)
	}
}

func spawnRuntime(t *testing.T, agentID string) (*LocalRuntime, *ExecutorEngine, string) {
	t.Helper()
	rt, ee, home := engineForAgent(t, agentID)
	attach(rt, ee)
	return rt, ee, home
}

func testImageBytes(t *testing.T, kind string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(20 + x), G: uint8(40 + y), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if kind == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		t.Fatalf("encode %s: %v", kind, err)
	}
	return buf.Bytes()
}

func assertManifestFile(t *testing.T, home, execID string, f taskInputManifestFile, want []byte, width, height int) {
	t.Helper()
	if f.SizeBytes != int64(len(want)) {
		t.Fatalf("%s size=%d want %d", f.URI, f.SizeBytes, len(want))
	}
	sum := sha256.Sum256(want)
	if f.SHA256 != fmt.Sprintf("%x", sum[:]) {
		t.Fatalf("%s sha=%s want %x", f.URI, f.SHA256, sum[:])
	}
	if f.Width != width || f.Height != height {
		t.Fatalf("%s dimensions=%dx%d want %dx%d", f.URI, f.Width, f.Height, width, height)
	}
	got, err := os.ReadFile(filepath.Join(home, "executors", execID, "workspace", filepath.FromSlash(f.Path)))
	if err != nil {
		t.Fatalf("read materialized %s: %v", f.Path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("materialized bytes for %s differ", f.URI)
	}
}

func readJSONTest(t *testing.T, path string, out any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestBuildWorkItem(t *testing.T) {
	t.Run("full detail", func(t *testing.T) {
		got := buildWorkItem("task-9", &centerTaskDetail{
			ID: "task-9", Title: "Fix the bug", Description: "do the fix", Model: "claude-haiku",
			DeliveryContract: "evidence_only",
		}, "", nil, nil, "", "")
		want := orchestrator.WorkItem{
			TaskID: "task-9", TaskRef: "task-9", TaskModel: "claude-haiku",
			Goal:             executor.Goal{Title: "Fix the bug", Description: "do the fix"},
			DeliveryContract: "evidence_only",
		}
		if got != want {
			t.Errorf("buildWorkItem = %+v, want %+v", got, want)
		}
	})
	t.Run("title falls back to first description line", func(t *testing.T) {
		got := buildWorkItem("task-1", &centerTaskDetail{Description: "  \nfirst line\nrest"}, "", nil, nil, "", "")
		if got.Goal.Title != "first line" {
			t.Errorf("goal title = %q, want 'first line'", got.Goal.Title)
		}
	})
	t.Run("title falls back to task id", func(t *testing.T) {
		got := buildWorkItem("task-7", &centerTaskDetail{}, "", nil, nil, "", "")
		if got.Goal.Title != "task task-7" {
			t.Errorf("goal title = %q, want 'task task-7'", got.Goal.Title)
		}
	})
	t.Run("supervisor override supplies model and context", func(t *testing.T) {
		got := buildWorkItem("task-9", &centerTaskDetail{
			ID: "task-9", Title: "Fix the bug", Description: "do the fix", Model: "claude-haiku",
		}, "", nil, nil, " claude-opus ", " use this traceback ")
		if got.TaskModel != "claude-opus" {
			t.Errorf("TaskModel override = %q, want claude-opus", got.TaskModel)
		}
		if got.Context != "use this traceback" {
			t.Errorf("Context override = %q, want trimmed context", got.Context)
		}
	})
	t.Run("repo keeps default branch protection context", func(t *testing.T) {
		got := buildWorkItem("task-9", &centerTaskDetail{
			Title: "Fix", BaseRef: "release/v2",
			Repo: &centerTaskRepo{URL: "git@example/repo.git", DefaultBranch: "develop"},
		}, "", nil, nil, "", "")
		if got.Repo == nil || got.Repo.BaseRef != "release/v2" || got.Repo.DefaultBranch != "develop" {
			t.Fatalf("repo delivery policy context was dropped: %+v", got.Repo)
		}
	})
}

// TestBuildExecutorEngine_WiresWriteback covers the W2 branch that builds the center
// writeback when the runtime has an agent-tool caller.
func TestBuildExecutorEngine_WiresWriteback(t *testing.T) {
	trueBin := lookTrue(t)
	rt := newExecRuntime(t, t.TempDir(), "a-wb", trueBin)
	setToolCaller(rt, &fakeToolCaller{}) // W2: enables the real center Writeback path
	home, _, _, err := rt.agentPaths("a-wb")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{
		AgentID: "a-wb", MaxConcurrentTasks: 1, DefaultExecutorModel: "d",
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine with ToolCaller: %v", err)
	}
	if ee == nil || ee.monitor == nil || ee.engine == nil {
		t.Fatal("engine/monitor should be built")
	}
}

// ---- SpawnExecutor (explicit fork_executor primitive) ----

// spawn drives SpawnExecutor with the given scripted caller attached.
func spawn(t *testing.T, agentID, taskID string, sc ToolCaller) (*LocalRuntime, *ExecutorEngine, string) {
	t.Helper()
	rt, ee, home := engineForAgent(t, agentID)
	attach(rt, ee)
	if sc != nil {
		setToolCaller(rt, sc)
	}
	_, _ = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: taskID})
	return rt, ee, home
}

func TestSpawnExecutor_AdmitsThenForks(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-9", "title": "Fix the bug", "description": "do the fix", "status": "open", "model": "claude-haiku",
	}, teamRulesBody: map[string]any{
		"team_id": "team-1", "phase": "execute", "commit": "abc123",
		"refresh_semantics": "snapshot at fork",
		"rules": []map[string]any{{
			"slug": "prefer-tests", "description": "test first", "body": "preloaded body must not persist",
			"body_bytes": 26, "applies_to": []string{"execute"}, "source_path": "rules/prefer-tests.md",
		}},
	}}
	rt, _, home := spawn(t, "agent-fork", "task-9", sc)

	assertAdmissionForked(t, sc, "admission must run get_task→start_task before forking")
	if body, ok := sc.callFor("get_team_rule_index"); !ok || body["phase"] != "execute" ||
		body["agent_id"] != "agent-fork" || body["execution_id"] == "" {
		t.Errorf("get_team_rule_index body = %v", body)
	}
	if body, ok := sc.callFor("start_task"); !ok || body["task_id"] != "task-9" || body["agent_id"] != "agent-fork" {
		t.Errorf("start_task body = %v", body)
	}
	if probs := loadRouting(t, home); len(probs) != 1 || len(probs[0].TaskRefs) == 0 || probs[0].TaskRefs[0] != "task-9" {
		t.Fatalf("expected one problem bound to task-9, got %+v", probs)
	} else {
		layout, err := executor.NewLayout(home)
		if err != nil {
			t.Fatalf("layout: %v", err)
		}
		fx, err := executor.NewFileExchange(layout, nil)
		if err != nil {
			t.Fatalf("file exchange: %v", err)
		}
		in, err := fx.ReadInput(probs[0].ExecutorIDs[0])
		if err != nil {
			t.Fatalf("read input: %v", err)
		}
		if in.TeamRules == nil || in.TeamRules.Commit != "abc123" || len(in.TeamRules.Rules) != 1 ||
			in.TeamRules.Rules[0].Slug != "prefer-tests" || in.TeamRules.Rules[0].BodyBytes != 26 {
			t.Fatalf("input team_rules = %+v", in.TeamRules)
		}
		if body, ok := sc.callFor("get_team_rule_index"); ok && body["execution_id"] != in.ExecutorID {
			t.Fatalf("get_team_rule_index execution_id=%v, input executor_id=%s", body["execution_id"], in.ExecutorID)
		}
		inputPath, err := layout.InputPath(in.ExecutorID)
		if err != nil {
			t.Fatalf("input path: %v", err)
		}
		raw, err := os.ReadFile(inputPath)
		if err != nil {
			t.Fatalf("read raw input: %v", err)
		}
		if strings.Contains(string(raw), "preloaded body must not persist") {
			t.Fatalf("input.json leaked rule body: %s", raw)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("decode raw input: %v", err)
		}
		teamRules, _ := doc["team_rules"].(map[string]any)
		storedRules, _ := teamRules["rules"].([]any)
		storedRule, _ := storedRules[0].(map[string]any)
		if _, leaked := storedRule["body"]; leaked {
			t.Fatalf("input.json rule entry leaked body key: %v", storedRule)
		}
	}
	if got := rt.CurrentTaskID(); got != "task-9" {
		t.Errorf("currentTaskID = %q, want task-9", got)
	}
}

func TestSpawnExecutor_StartTaskDeclinedSkipsFork(t *testing.T) {
	sc := &scriptedToolCaller{
		getTaskBody: map[string]any{"id": "task-2", "title": "t", "status": "open"},
		startErr:    errors.New("409 agent_busy"),
	}
	rt, _, home := spawn(t, "agent-cap", "task-2", sc)

	if seen := sc.toolsSeen(); len(seen) != 3 || seen[1] != "list_files" || seen[2] != "start_task" {
		t.Fatalf("tool calls = %v, want get_task then list_files then start_task", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("declined admission must NOT fork, got problems %+v", probs)
	}
	if got := rt.CurrentTaskID(); got != "" {
		t.Errorf("currentTaskID = %q, want empty (no fork)", got)
	}
}

func TestSpawnExecutor_AlreadyRunningWithoutExecutorForksWithoutRestart(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-3", "title": "t", "status": "running", "assignee": "agent:agent-again",
	}}
	_, _, home := spawn(t, "agent-again", "task-3", sc)

	for _, tool := range sc.toolsSeen() {
		if tool == "start_task" {
			t.Fatalf("already-admitted task must not be started twice: calls=%v", sc.toolsSeen())
		}
	}
	if probs := loadRouting(t, home); len(probs) != 1 || len(probs[0].TaskRefs) != 1 || probs[0].TaskRefs[0] != "task-3" {
		t.Fatalf("running task without an executor must fork exactly once, got %+v", probs)
	}
}

func TestSpawnExecutor_NonDispatchableStatesSkip(t *testing.T) {
	for _, tc := range []struct {
		name, status, blockedReason string
	}{
		{name: "blocked", status: "blocked", blockedReason: "waiting"},
		{name: "legacy running blocked", status: "running", blockedReason: "waiting"},
		{name: "completed", status: "completed"},
		{name: "discarded", status: "discarded"},
		{name: "unknown", status: "future_state"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := &scriptedToolCaller{getTaskBody: map[string]any{
				"id": "task-stop", "title": "t", "status": tc.status,
				"blocked_reason": tc.blockedReason, "assignee": "agent:agent-stop",
			}}
			_, _, home := spawn(t, "agent-stop", "task-stop", sc)
			for _, tool := range sc.toolsSeen() {
				if tool == "start_task" || tool == "block_task" {
					t.Fatalf("state %q must stop after read, calls=%v", tc.status, sc.toolsSeen())
				}
			}
			if probs := loadRouting(t, home); len(probs) != 0 {
				t.Fatalf("state %q must not fork, got %+v", tc.status, probs)
			}
		})
	}
}

// TestSpawnExecutor_SkipsForeignAssignee_Guard locks the issue-d118b5dc ② FIX: SpawnExecutor
// (the explicit fork_executor path) now GUARDS on the assignee — if the fetched task is
// assigned to a DIFFERENT agent, it stops after get_task (no start_task, no fork), leaving the
// task queued for its real assignee. Here agent-self receives a fork request for task-x
// assigned to agent-OTHER: it must NOT force-start or fork a cross-namespace executor. The
// DISPATCH-CROSS-NAMESPACE decision log is still emitted (fail-loud keeper).
func TestSpawnExecutor_SkipsForeignAssignee_Guard(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-x", "title": "assigned to another agent", "status": "open",
		"assignee": "agent:agent-OTHER", "model": "claude-haiku",
	}}
	rt, _, home := spawn(t, "agent-self", "task-x", sc)

	// FIX (②): assignee=agent-OTHER → stop after get_task, no start_task, no fork.
	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v — foreign-assignee guard must stop after get_task (want [get_task])", seen)
	}
	if _, ok := sc.callFor("start_task"); ok {
		t.Error("start_task must NOT be called for a task assigned to another agent (cross-namespace)")
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("foreign task must NOT be forked (② guard): problems=%+v", probs)
	}
	if got := rt.CurrentTaskID(); got != "" {
		t.Errorf("currentTaskID = %q, want empty (foreign task skipped)", got)
	}
}

// TestSpawnExecutor_OwnAssignee_ForksNormally is the ② guard's LIVENESS lock: a task whose
// assignee IS this runtime must still fork — the guard only refuses a FOREIGN assignee, never
// the agent's own work. This is the critical false-positive check: if the identity compare
// were wrong (e.g. keyed on the ULID cfg.AgentID instead of identityRef), the guard would
// refuse EVERY dispatch and wedge all concurrency. identityRef() falls back to cfg.AgentID
// ("agent-self") when no AgentRef is set, so an assignee of "agent:agent-self" is own.
func TestSpawnExecutor_OwnAssignee_ForksNormally(t *testing.T) {
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-own", "title": "my own task", "status": "open",
		"assignee": "agent:agent-self", "model": "claude-haiku",
	}}
	rt, _, home := spawn(t, "agent-self", "task-own", sc)

	assertAdmissionForked(t, sc, "an own-assignee task must fork normally (② guard must NOT false-positive)")
	if probs := loadRouting(t, home); len(probs) != 1 {
		t.Fatalf("own-assignee task must fork (② guard must NOT false-positive): problems=%+v", probs)
	}
	if got := rt.CurrentTaskID(); got != "task-own" {
		t.Errorf("currentTaskID = %q, want task-own (own task forked)", got)
	}
}

func TestSpawnExecutor_GetTaskErrorSkips(t *testing.T) {
	sc := &scriptedToolCaller{getTaskErr: errors.New("403 not_agents_task")}
	_, _, home := spawn(t, "agent-gterr", "task-4", sc)

	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want get_task only", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("get_task failure must NOT fork, got %+v", probs)
	}
}

func TestSpawnExecutor_MalformedGetTaskSkips(t *testing.T) {
	sc := &scriptedToolCaller{getTaskRaw: []byte("{not json")}
	_, _, home := spawn(t, "agent-bad", "task-8", sc)

	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want get_task only", seen)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("malformed get_task must NOT fork, got %+v", probs)
	}
}

func TestSpawnExecutor_NoToolCallerLeavesQueued(t *testing.T) {
	_, _, home := spawn(t, "agent-noc", "task-5", nil) // no ToolCaller wired
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Errorf("no ToolCaller must NOT fork, got %+v", probs)
	}
}

func TestSpawnExecutor_EmptyTaskID(t *testing.T) {
	sc := &scriptedToolCaller{}
	spawn(t, "agent-empty", "  ", sc)
	if seen := sc.toolsSeen(); len(seen) != 0 {
		t.Errorf("empty task_id must short-circuit before any center call, got %v", seen)
	}
}

// TestSpawnExecutor_ForkFailsAfterAdmission covers the reap-skew branch: the center
// admits (start_task ok) but the local pool is saturated, so the fork returns
// ErrAtCapacity. It must be logged, not panic; no extra slot is taken.
func TestSpawnExecutor_ForkFailsAfterAdmission(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-skew") // pool max 2
	attach(rt, ee)
	for i := 0; i < 2; i++ {
		if _, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
			TaskRef: "sat-" + string(rune('a'+i)), Goal: executor.Goal{Title: "g"},
		}); err != nil {
			t.Fatalf("saturate %d: %v", i, err)
		}
	}
	sc := &scriptedToolCaller{getTaskBody: map[string]any{"id": "task-6", "title": "t", "status": "open"}}
	setToolCaller(rt, sc)

	_, _ = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-6"}) // must not panic

	if seen := sc.toolsSeen(); len(seen) != 5 || seen[1] != "list_files" || seen[2] != "start_task" || seen[3] != "get_team_rule_index" || seen[4] != "block_task" {
		t.Fatalf("capacity skew must be surfaced after admission: tool calls = %v", seen)
	}
	if act := ee.engine.Pool().Active(); act != 2 {
		t.Errorf("pool active = %d, want 2 (saturated; failed fork took no slot)", act)
	}
}

// blockingToolCaller is a channel/barrier collaborator: it holds start_task until the
// expected number of distinct fork sequences are concurrently inside admission. No
// sleep is involved, so a failure is an invariant failure rather than scheduler timing.
type blockingToolCaller struct {
	mu               sync.Mutex
	startInFlight    int
	maxStartInFlight int
	getTasks         int
	expectedStarts   int
	startsReady      chan struct{}
	releaseStarts    chan struct{}
	readyOnce        sync.Once
}

func (b *blockingToolCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	b.mu.Lock()
	if tool == "start_task" {
		b.startInFlight++
		if b.startInFlight > b.maxStartInFlight {
			b.maxStartInFlight = b.startInFlight
		}
		if b.startInFlight == b.expectedStarts && b.startsReady != nil {
			b.readyOnce.Do(func() { close(b.startsReady) })
		}
	}
	if tool == "get_task" {
		b.getTasks++
	}
	b.mu.Unlock()

	if tool == "start_task" && b.releaseStarts != nil {
		select {
		case <-b.releaseStarts:
		case <-time.After(5 * time.Second):
			return errors.New("test start_task barrier timed out")
		}
	}
	if tool == "get_task" && out != nil {
		rb, _ := json.Marshal(map[string]any{"id": "t", "title": "t", "status": "open"})
		*out = append((*out)[:0], rb...)
	}

	b.mu.Lock()
	if tool == "start_task" {
		b.startInFlight--
	}
	b.mu.Unlock()
	return nil
}

// TestSpawnExecutor_DistinctTasksProceedConcurrently locks the intended N-way model:
// per-task single-flight must not serialize two different tasks. Pool.Launch remains
// the sole atomic ≤N gate.
func TestSpawnExecutor_DistinctTasksProceedConcurrently(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-parallel") // pool max 2 (both forks fit)
	attach(rt, ee)
	btc := &blockingToolCaller{
		expectedStarts: 2,
		startsReady:    make(chan struct{}),
		releaseStarts:  make(chan struct{}),
	}
	setToolCaller(rt, btc)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-" + string(rune('a'+n))})
		}(i)
	}
	select {
	case <-btc.startsReady:
	case <-time.After(5 * time.Second):
		t.Fatal("distinct task forks did not overlap at start_task")
	}
	close(btc.releaseStarts)
	wg.Wait()

	btc.mu.Lock()
	maxIn, got := btc.maxStartInFlight, btc.getTasks
	btc.mu.Unlock()

	if got < 2 {
		t.Fatalf("both SpawnExecutor calls must reach get_task, got %d", got)
	}
	if maxIn != 2 {
		t.Fatalf("distinct fork sequences max concurrent start_task = %d, want 2", maxIn)
	}
}

type gatedGetTaskCaller struct {
	mu         sync.Mutex
	getCalls   int
	startCalls int
	getEntered chan struct{}
	releaseGet chan struct{}
	enterOnce  sync.Once
}

func (g *gatedGetTaskCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	switch tool {
	case "get_task":
		g.mu.Lock()
		g.getCalls++
		g.mu.Unlock()
		g.enterOnce.Do(func() { close(g.getEntered) })
		<-g.releaseGet
		if out != nil {
			raw, _ := json.Marshal(map[string]any{
				"id": "task-one", "title": "one", "status": "open", "assignee": "agent:agent-one",
			})
			*out = append((*out)[:0], raw...)
		}
	case "start_task":
		g.mu.Lock()
		g.startCalls++
		g.mu.Unlock()
	}
	return nil
}

func TestSpawnExecutor_SameTaskConcurrentCallsCoalesce(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-one")
	attach(rt, ee)
	caller := &gatedGetTaskCaller{getEntered: make(chan struct{}), releaseGet: make(chan struct{})}
	setToolCaller(rt, caller)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-one"})
	}()
	select {
	case <-caller.getEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first fork did not enter get_task")
	}

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-one"})
	}()
	select {
	case <-secondDone:
	case <-time.After(5 * time.Second):
		t.Fatal("duplicate fork waited instead of coalescing")
	}
	caller.mu.Lock()
	getCallsBeforeRelease := caller.getCalls
	caller.mu.Unlock()
	if getCallsBeforeRelease != 1 {
		t.Fatalf("duplicate fork reached center %d times before release, want 1", getCallsBeforeRelease)
	}

	close(caller.releaseGet)
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first fork did not finish")
	}
	caller.mu.Lock()
	startCalls := caller.startCalls
	caller.mu.Unlock()
	if startCalls != 1 {
		t.Fatalf("same task start_task calls = %d, want 1", startCalls)
	}
	if probs := loadRouting(t, home); len(probs) != 1 {
		t.Fatalf("same task must create one executor route, got %+v", probs)
	}
}

func TestSpawnExecutor_SameTaskWithLiveExecutorCoalescesBeforeCenterRead(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-live")
	attach(rt, ee)
	caller := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-live", "title": "live", "status": "running", "assignee": "agent:agent-live",
	}}
	setToolCaller(rt, caller)
	rt.trackTaskExecutor("task-live", "exec-live")
	defer rt.untrackTaskExecutor("task-live", "exec-live")

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-live"})
	if err != nil || res == nil || res.ExecutorID != "exec-live" {
		t.Fatalf("duplicate live spawn = (%+v, %v), want coalesced exec-live result", res, err)
	}
	if calls := caller.toolsSeen(); len(calls) != 0 {
		t.Fatalf("known live executor duplicate reached center: calls=%v", calls)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("known live executor duplicate created another route: %+v", probs)
	}
}

// TestSpawnExecutor_ModelNotAllowedBlocks: the task was admitted (start_task ok) but
// its task.model is not in allowed_executors → the fork returns ErrModelNotAllowed →
// the task is blocked (block_task), not forked.
func TestSpawnExecutor_ModelNotAllowedBlocks(t *testing.T) {
	trueBin := lookTrue(t)
	rt := newExecRuntime(t, t.TempDir(), "agent-block", trueBin)
	home, _, _, err := rt.agentPaths("agent-block")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{
		AgentID: "agent-block", MaxConcurrentTasks: 1, AllowedExecutors: testExecs, // only model "m"
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)
	sc := &scriptedToolCaller{getTaskBody: map[string]any{
		"id": "task-x", "title": "t", "status": "open", "model": "forbidden-model",
	}}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-x"})
	if err != nil || res == nil || res.CommandStatus != controlCommandStatusFailed || res.Reason != string(CauseModelNotAllowed) {
		t.Fatalf("SpawnExecutor (model blocked) = (%v, %v), want failed/model_not_allowed", res, err)
	}
	seen := sc.toolsSeen()
	if len(seen) != 5 || seen[0] != "get_task" || seen[1] != "list_files" || seen[2] != "start_task" || seen[3] != "get_team_rule_index" || seen[4] != "block_task" {
		t.Fatalf("tool calls = %v, want [get_task list_files start_task get_team_rule_index block_task]", seen)
	}
	if body, ok := sc.callFor("block_task"); !ok || body["reason_type"] != "obstacle" {
		t.Errorf("block_task body = %v", body)
	}
	// No executor was actually spawned (the fork was rejected before launch).
	if act := ee.engine.Pool().Active(); act != 0 {
		t.Errorf("pool active = %d, want 0 (model-not-allowed took no slot)", act)
	}
}

// TestNotifyWork_ExecutorBranchForks: a concurrency-enabled runtime with a live session
// forks via the executor branch of NotifyWork (createTaskDir + workViaExecutor) instead
// of injecting the brief.
func TestNotifyWork_ExecutorBranchForks(t *testing.T) {
	trueBin := lookTrue(t)
	rt := newExecRuntime(t, t.TempDir(), "agent-nw", trueBin)
	rt.cfg.TaskDirManager = taskexec.NewDirManager()
	home, _, _, err := rt.agentPaths("agent-nw")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{AgentID: "agent-nw", MaxConcurrentTasks: 2, DefaultExecutorModel: "d"})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs }) // NotifyWork requires a live session before branching
	sc := &scriptedToolCaller{teamRulesBody: map[string]any{
		"team_id": "team-1", "phase": "execute", "commit": "notify-c1",
		"rules": []map[string]any{{"slug": "notify-rule", "description": "read for notify", "body_bytes": 12}},
	}}
	setToolCaller(rt, sc)

	if err := rt.NotifyWork(context.Background(), WorkRequest{AgentID: "agent-nw", TaskID: "t-1", TaskRef: "task-1", Brief: "do it"}); err != nil {
		t.Fatalf("NotifyWork: %v", err)
	}
	// Executor branch: the brief was NOT injected into the resident session.
	if msgs := fs.msgs(); len(msgs) != 0 {
		t.Errorf("executor branch must NOT inject into the session, got %v", msgs)
	}
	if probs := loadRouting(t, home); len(probs) != 1 || len(probs[0].TaskRefs) == 0 || probs[0].TaskRefs[0] != "task-1" {
		t.Fatalf("expected routing bound to task-1, got %+v", probs)
	} else {
		executorID := probs[0].ExecutorIDs[0]
		if body, ok := sc.callFor("get_team_rule_index"); !ok || body["execution_id"] != executorID {
			t.Fatalf("NotifyWork get_team_rule_index body = %v, want execution_id %s", body, executorID)
		}
		layout, err := executor.NewLayout(home)
		if err != nil {
			t.Fatalf("layout: %v", err)
		}
		fx, err := executor.NewFileExchange(layout, nil)
		if err != nil {
			t.Fatalf("file exchange: %v", err)
		}
		in, err := fx.ReadInput(executorID)
		if err != nil {
			t.Fatalf("read input: %v", err)
		}
		if in.TeamRules == nil || in.TeamRules.Commit != "notify-c1" {
			t.Fatalf("NotifyWork input team rules = %+v", in.TeamRules)
		}
	}
	if got := rt.CurrentTaskID(); got != "t-1" {
		t.Errorf("currentTaskID = %q, want t-1", got)
	}
}

func TestRecover_PreservesRuleSnapshotWithoutRefreshingIndex(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-rule-rec")
	attach(rt, ee)
	sc := &scriptedToolCaller{teamRulesErr: errors.New("recovery must not refresh team rule index")}
	setToolCaller(rt, sc)

	fx, tr := seedExchange(t, home)
	_, alivePID := liveChild(t)
	now := time.Now()
	const execID = "exec-rules-001"
	if _, err := fx.Provision(execID); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	mustWriteRuntimeTaskInputPackage(t, home, execID)
	if err := fx.WriteInput(executor.Input{
		ExecutorID: execID,
		Goal:       executor.Goal{Title: "recover rules"},
		Model:      "m",
		Source:     executor.SourceRefs{TaskRef: "task-rules"},
		TeamRules: &executor.RuleSnapshot{
			TeamID: "team-1",
			Phase:  "execute",
			Commit: "old-commit",
			Rules:  []executor.RuleContext{{Slug: "old-rule", Description: "old description"}},
		},
		TaskInput: &executor.TaskInputRef{Dir: "task-input/v1", ManifestPath: "task-input/v1/manifest.json"},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	if err := fx.WriteStatus(executor.Status{ExecutorID: execID, State: executor.StateRunning, Model: "m", StartedAt: now, LastProgressAt: now}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	if err := tr.Write(executor.Record{ExecutorID: execID, PID: alivePID, SpawnedAt: now}); err != nil {
		t.Fatalf("Tracker.Write: %v", err)
	}

	if err := rt.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if seen := sc.toolsSeen(); len(seen) != 0 {
		t.Fatalf("recovery must reuse input.json rule snapshot without center refresh, calls=%v", seen)
	}
	in, err := fx.ReadInput(execID)
	if err != nil {
		t.Fatalf("ReadInput: %v", err)
	}
	if in.TeamRules == nil || in.TeamRules.Commit != "old-commit" || in.TeamRules.Rules[0].Slug != "old-rule" {
		t.Fatalf("recovery changed team rule snapshot: %+v", in.TeamRules)
	}
	if in.TaskInput == nil || in.TaskInput.Dir != "task-input/v1" || in.TaskInput.ManifestPath != "task-input/v1/manifest.json" {
		t.Fatalf("recovery changed task-input package ref: %+v", in.TaskInput)
	}
	if got, err := os.ReadFile(filepath.Join(home, "executors", execID, "workspace", "task-input", "v1", "attachments", "existing.txt")); err != nil || string(got) != "existing task input" {
		t.Fatalf("recovery did not reuse complete task-input package: got=%q err=%v", got, err)
	}
}

func mustWriteRuntimeTaskInputPackage(t *testing.T, home, execID string) {
	t.Helper()
	root := filepath.Join(home, "executors", execID, "workspace", "task-input", "v1")
	if err := os.MkdirAll(filepath.Join(root, "attachments"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "attachments", "existing.txt"), []byte("existing task input"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := taskInputManifest{
		Version: 1,
		TaskID:  "task-rules",
		Files: []taskInputManifestFile{{
			SourceScope: "task",
			SourceID:    "task-rules",
			URI:         "ac://files/existing",
			Name:        "existing.txt",
			Path:        "task-input/v1/attachments/existing.txt",
			SizeBytes:   int64(len("existing task input")),
			Canonical:   true,
			Required:    true,
		}},
	}
	if err := writeJSONFileAtomic(filepath.Join(root, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
}

// ---- Recover / RunWatchdog (crash-recovery + orphan poll) ----

// TestRecover_AdoptsRunningFinalizesTerminal: after a restart the orchestrator rebuilds
// in-flight executor state from durable files — a still-alive orphan is re-adopted
// (counts toward the cap + registered for watchdog polling), a terminal orphan is
// finalized (dir torn down). It re-spawns nothing.
func TestRecover_AdoptsRunningFinalizesTerminal(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-rec")
	attach(rt, ee)

	fx, tr := seedExchange(t, home)
	_, alivePID := liveChild(t)
	now := time.Now()

	seedOrphan(t, fx, tr, "exec-aaa111", alivePID,
		executor.Status{ExecutorID: "exec-aaa111", State: executor.StateRunning, Model: "m", StartedAt: now, LastProgressAt: now}, nil)
	seedOrphan(t, fx, tr, "exec-bbb222", deadPID(t),
		executor.Status{ExecutorID: "exec-bbb222", State: executor.StateDone, Model: "m", StartedAt: now, LastProgressAt: now},
		&executor.Output{ExecutorID: "exec-bbb222", Success: true, Result: "ok", FinishedAt: now})

	if err := rt.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	orphans := ee.snapshotOrphans()
	if pid, ok := orphans["exec-aaa111"]; !ok || pid != alivePID {
		t.Errorf("running orphan A not adopted for watchdog: %+v", orphans)
	}
	if _, ok := orphans["exec-bbb222"]; ok {
		t.Error("terminal orphan B must NOT be registered for polling")
	}
	if ee.engine.Pool().Active() != 1 {
		t.Errorf("pool active = %d, want 1 (A re-adopted toward the cap)", ee.engine.Pool().Active())
	}
	// Delayed teardown (issue-f30b7e7b): recovery RETAINS the terminal orphan (marks it
	// finalized) rather than tearing it down inline; a later ReapFinalized pass removes it.
	if dirGone(t, fx, "exec-bbb222") {
		t.Error("terminal orphan B dir must be RETAINED by recovery (delayed teardown)")
	}
	if dirGone(t, fx, "exec-aaa111") {
		t.Error("running orphan A dir must be retained")
	}
	// The reaper removes the finalized terminal (B), never the running orphan (A, no marker).
	if _, err := ee.monitor.ReapFinalized(context.Background(), 0, 0); err != nil {
		t.Fatalf("ReapFinalized: %v", err)
	}
	if !dirGone(t, fx, "exec-bbb222") {
		t.Error("reap must remove the finalized terminal orphan B dir")
	}
	if dirGone(t, fx, "exec-aaa111") {
		t.Error("running orphan A dir must survive the reap (no finalized marker)")
	}
}

// TestRunWatchdog_PollsAdoptedOrphanToCompletion: an adopted orphan (no reapable
// handle) is watched by the watchdog tick — when its process exits, the poll detects
// it, finalizes it, and stops polling.
func TestRunWatchdog_PollsAdoptedOrphanToCompletion(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-wd")
	attach(rt, ee)

	fx, tr := seedExchange(t, home)
	child, alivePID := liveChild(t)
	now := time.Now()
	seedOrphan(t, fx, tr, "exec-ccc333", alivePID,
		executor.Status{ExecutorID: "exec-ccc333", State: executor.StateRunning, Model: "m", StartedAt: now, LastProgressAt: now}, nil)

	if err := rt.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; !ok {
		t.Fatal("orphan C should be adopted for watchdog polling")
	}

	// Tick 1 (orphan alive): still polled, not finalized.
	rt.RunWatchdog(context.Background())
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; !ok {
		t.Fatal("orphan C must remain tracked while alive")
	}

	_ = child.Process.Kill()
	_, _ = child.Process.Wait()

	// Tick 2 (orphan gone): the poll finalizes it and drops it.
	rt.RunWatchdog(context.Background())
	if _, ok := ee.snapshotOrphans()["exec-ccc333"]; ok {
		t.Error("orphan C must be dropped after the watchdog observes its exit")
	}
	if ee.engine.Pool().Active() != 0 {
		t.Errorf("pool active = %d, want 0 after orphan finalized", ee.engine.Pool().Active())
	}
}

// TestRunWatchdog_NoEngineNoop: RunWatchdog on a runtime with no engine is a safe
// no-op (the daemon iterates ALL runtimes; non-concurrent ones must not panic).
func TestRunWatchdog_NoEngineNoop(t *testing.T) {
	rt := newExecRuntime(t, t.TempDir(), "agent-none", "true")
	rt.RunWatchdog(context.Background()) // must not panic
	if err := rt.Recover(context.Background()); err != nil {
		t.Errorf("Recover with no engine must be a no-op nil, got %v", err)
	}
	if snaps := rt.SnapshotConcurrency(); snaps != nil {
		t.Errorf("SnapshotConcurrency with no engine must be nil, got %+v", snaps)
	}
}
