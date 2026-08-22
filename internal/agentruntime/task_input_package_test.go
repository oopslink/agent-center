package agentruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

type fakeFileDownloader struct {
	mu       sync.Mutex
	contents map[string][]byte
	failOnce map[string]error
	calls    []string
}

func (f *fakeFileDownloader) DownloadFile(_ context.Context, _ string, _ string, uri, dest string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, uri)
	if err := f.failOnce[uri]; err != nil {
		delete(f.failOnce, uri)
		return err
	}
	b, ok := f.contents[uri]
	if !ok {
		return errors.New("missing fake file " + uri)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dest, b, 0o600)
}

func (f *fakeFileDownloader) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestSpawnExecutor_TaskInputDownloadFailurePreventsStartAndRetrySucceeds(t *testing.T) {
	uri := "ac://files/01M0HRMZDX20FF5KQT4SBANGC1"
	content := pngBytes(t, 11, 7)
	sum := sha(content)
	sc := &scriptedToolCaller{
		getTaskBody: map[string]any{"id": "task-input", "title": "uses files", "status": "open", "assignee": "agent:agent-files"},
		listFilesBody: map[string]any{"files": []map[string]any{{
			"uri": uri, "filename": "mockup.png", "mime_type": "image/png",
			"size": len(content), "sha256": sum, "width": 11, "height": 7,
		}}},
	}
	dl := &fakeFileDownloader{
		contents: map[string][]byte{uri: content},
		failOnce: map[string]error{uri: errors.New("transient 503")},
	}
	rt, ee, home := engineForAgent(t, "agent-files")
	attach(rt, ee)
	setToolCaller(rt, sc)
	setFileDownloader(rt, dl)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-input"})
	if err != nil || res == nil || res.CommandStatus != controlCommandStatusFailed || res.Reason != "task_input_materialization_failed" {
		t.Fatalf("first SpawnExecutor = (%+v, %v), want task_input_materialization_failed", res, err)
	}
	if _, ok := sc.callFor("start_task"); ok {
		t.Fatalf("download failure must prevent start_task: calls=%v", sc.toolsSeen())
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("download failure must not fork, problems=%+v", probs)
	}

	res, err = rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-input"})
	if err != nil || res == nil || res.ExecutorID == "" {
		t.Fatalf("retry SpawnExecutor = (%+v, %v), want executor", res, err)
	}
	if dl.callCount() != 2 {
		t.Fatalf("download calls = %d, want first failure + retry success", dl.callCount())
	}
	pkgDir := filepath.Join(home, "executors", res.ExecutorID, "task-input", "v1")
	assertTaskInputFile(t, pkgDir, "mockup.png", content, sum, 11, 7)
	assertPromptPointsAtTaskInput(t, home, res.ExecutorID, pkgDir)
}

func TestTaskInputPackage_RejectsStaleAndReusesCompletePackage(t *testing.T) {
	uri := "ac://files/01M0HRMZEV7XS8A3MNGG64ZZW1"
	content := []byte("canonical text")
	sum := sha(content)
	rt, ee, home := engineForAgent(t, "agent-reuse")
	attach(rt, ee)
	execID := ee.engine.NewExecutorID()
	dl := &fakeFileDownloader{contents: map[string][]byte{uri: content}}
	setToolCaller(rt, &scriptedToolCaller{
		listFilesBody: map[string]any{"files": []map[string]any{{
			"uri": uri, "filename": "notes.txt", "mime_type": "text/plain", "size": len(content), "sha256": sum,
		}}},
	})
	setFileDownloader(rt, dl)
	layout, _ := executor.NewLayout(home)
	fx := &executorFileLayout{agentRoot: home, execDir: layout.Dir}
	pkg, err := rt.materializeTaskInputPackage(context.Background(), "agent-reuse", "task-a", execID, fx)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	pkg2, err := rt.materializeTaskInputPackage(context.Background(), "agent-reuse", "task-a", execID, fx)
	if err != nil {
		t.Fatalf("reuse complete package: %v", err)
	}
	if pkg2.PackageDir != pkg.PackageDir || dl.callCount() != 1 {
		t.Fatalf("complete package should be reused without re-download: pkg=%+v pkg2=%+v calls=%d", pkg, pkg2, dl.callCount())
	}
	if _, err := rt.materializeTaskInputPackage(context.Background(), "agent-reuse", "task-b", execID, fx); err == nil || !strings.Contains(err.Error(), "stale complete package") {
		t.Fatalf("stale package reuse for different task must fail, got %v", err)
	}
}

func TestTaskInputPackage_RejectsIncompletePackage(t *testing.T) {
	rt, ee, home := engineForAgent(t, "agent-incomplete")
	attach(rt, ee)
	execID := ee.engine.NewExecutorID()
	setToolCaller(rt, &scriptedToolCaller{})
	layout, _ := executor.NewLayout(home)
	pkgDir := filepath.Join(home, "executors", execID, "task-input", "v1")
	if err := os.MkdirAll(pkgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, ".incomplete"), []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := rt.materializeTaskInputPackage(context.Background(), "agent-incomplete", "task-i", execID, &executorFileLayout{
		agentRoot: home,
		execDir:   layout.Dir,
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete package exists") {
		t.Fatalf("incomplete package must be rejected, got %v", err)
	}
}

func assertTaskInputFile(t *testing.T, pkgDir, filename string, want []byte, wantSHA string, width, height int) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(pkgDir, "attachments", filename))
	if err != nil {
		t.Fatalf("read materialized file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("materialized bytes mismatch")
	}
	raw, err := os.ReadFile(filepath.Join(pkgDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest taskInputPackage
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Version != "v1" || len(manifest.Files) != 1 {
		t.Fatalf("bad manifest: %+v", manifest)
	}
	f := manifest.Files[0]
	if f.Filename != filename || f.SHA256 != wantSHA || f.Size != int64(len(want)) || f.Width != width || f.Height != height {
		t.Fatalf("file manifest = %+v", f)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, ".complete")); err != nil {
		t.Fatalf("complete marker missing: %v", err)
	}
}

func assertPromptPointsAtTaskInput(t *testing.T, home, execID, pkgDir string) {
	t.Helper()
	layout, _ := executor.NewLayout(home)
	tracker, err := executor.NewTracker(layout)
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}
	rec, err := tracker.Read(execID)
	if err != nil {
		t.Fatalf("read tracker: %v", err)
	}
	found := false
	for _, arg := range rec.RunnerCmd {
		if strings.Contains(arg, "task-input/v1") && strings.Contains(arg, pkgDir) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("runner prompt does not point to %s: argv=%q", pkgDir, rec.RunnerCmd)
	}
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x99, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
