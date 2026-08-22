package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

type fakeTaskInputDownloader struct {
	root  string
	bytes map[string][]byte
	err   error
}

func (f fakeTaskInputDownloader) DownloadTaskInputAttachment(_ context.Context, att TaskInputAttachment, destPath string) error {
	if f.err != nil {
		return f.err
	}
	b, ok := f.bytes[att.URI]
	if !ok {
		return os.ErrNotExist
	}
	abs := filepath.Join(f.root, filepath.FromSlash(destPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	return os.WriteFile(abs, b, 0o600)
}

func TestMaterializeTaskInput_PNGJPEGPlainManifestSHAAndSafeNames(t *testing.T) {
	root := t.TempDir()
	layout, _ := NewLayout(root)
	fx, _ := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	execID := "exec-task-input"
	ws, _ := layout.WorkspaceDir(execID)
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	pngBytes := imageBytes(t, "png", 2, 3)
	jpgBytes := imageBytes(t, "jpeg", 4, 5)
	txtBytes := []byte("plain file\n")
	atts := []TaskInputAttachment{
		{SourceScope: "task", SourceID: "task-1", URI: "ac://files/png", Name: "../mockup.png", MIME: "image/png", Size: int64(len(pngBytes)), Required: true, Canonical: true},
		{SourceScope: "task", SourceID: "task-1", URI: "ac://files/jpg", Name: "mockup.png", MIME: "image/jpeg", Size: int64(len(jpgBytes)), Required: true, Canonical: true},
		{SourceScope: "conversation", SourceID: "conv-1", URI: "ac://files/txt", Name: "notes.txt", MIME: "text/plain", Size: int64(len(txtBytes)), Required: true, Canonical: true},
	}
	dir, err := fx.MaterializeTaskInput(context.Background(), execID, ws, TaskInputSpec{
		TaskID:      "task-1",
		ExecutorID:  execID,
		Goal:        Goal{Title: "use mockups"},
		CreatedAt:   time.Unix(1700000000, 0),
		Attachments: atts,
		Downloader: fakeTaskInputDownloader{root: ws, bytes: map[string][]byte{
			"ac://files/png": pngBytes,
			"ac://files/jpg": jpgBytes,
			"ac://files/txt": txtBytes,
		}},
	})
	if err != nil {
		t.Fatalf("MaterializeTaskInput: %v", err)
	}
	if filepath.Base(dir) != taskInputVersion {
		t.Fatalf("dir = %q, want version leaf %q", dir, taskInputVersion)
	}
	var mf TaskInputManifest
	readJSONFile(t, filepath.Join(dir, "manifest.json"), &mf)
	if len(mf.Attachments) != 3 {
		t.Fatalf("manifest attachments = %d, want 3", len(mf.Attachments))
	}
	wantBytes := map[string][]byte{
		"ac://files/png": pngBytes,
		"ac://files/jpg": jpgBytes,
		"ac://files/txt": txtBytes,
	}
	seen := map[string]bool{}
	for _, got := range mf.Attachments {
		if strings.Contains(got.Path, "..") || strings.HasPrefix(got.Path, "/") {
			t.Fatalf("unsafe manifest path %q", got.Path)
		}
		seen[got.Path] = true
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(got.Path)))
		if err != nil {
			t.Fatalf("read attachment %s: %v", got.Path, err)
		}
		if !bytes.Equal(b, wantBytes[got.URI]) {
			t.Fatalf("%s bytes changed", got.URI)
		}
		if got.SHA256 != shaHex(wantBytes[got.URI]) {
			t.Fatalf("%s sha = %s want %s", got.URI, got.SHA256, shaHex(wantBytes[got.URI]))
		}
		if got.Size != int64(len(wantBytes[got.URI])) {
			t.Fatalf("%s size = %d", got.URI, got.Size)
		}
	}
	if !seen["attachments/mockup.png"] || !seen["attachments/mockup-2.png"] || !seen["attachments/notes.txt"] {
		t.Fatalf("duplicate/path-safe names not assigned as expected: %+v", mf.Attachments)
	}
}

func TestMaterializeTaskInput_DownloadFailureLeavesNoPackage(t *testing.T) {
	root := t.TempDir()
	layout, _ := NewLayout(root)
	fx, _ := NewFileExchange(layout, nil)
	execID := "exec-fail"
	ws, _ := layout.WorkspaceDir(execID)
	_ = os.MkdirAll(ws, 0o700)
	_, err := fx.MaterializeTaskInput(context.Background(), execID, ws, TaskInputSpec{
		TaskID: "task-1", Goal: Goal{Title: "x"},
		Attachments: []TaskInputAttachment{{SourceScope: "task", SourceID: "task-1", URI: "ac://files/missing", Name: "x.png", MIME: "image/png", Size: 1, Required: true, Canonical: true}},
		Downloader:  fakeTaskInputDownloader{root: ws, err: errors.New("download boom")},
	})
	if err == nil || !strings.Contains(err.Error(), "download boom") {
		t.Fatalf("err = %v, want diagnostic download error", err)
	}
	if _, statErr := os.Stat(filepath.Join(ws, "task-input", "v1")); !os.IsNotExist(statErr) {
		t.Fatalf("failed package must not be published, stat err=%v", statErr)
	}
}

func TestPool_TaskInputFailureDoesNotSpawn(t *testing.T) {
	pool, _ := newTestPool(t, 1, nil)
	var started bool
	pool.spawner = &Spawner{
		start: func(cmd *exec.Cmd) error {
			started = true
			cmd.Process = &fakeProcess
			return nil
		},
	}
	_, err := pool.Launch(context.Background(), LaunchSpec{
		Input:     validPoolInput("exec-no-spawn"),
		RunnerCmd: []string{"true"},
		TaskInput: &TaskInputSpec{
			TaskID: "task-1", Goal: Goal{Title: "x"},
			Attachments: []TaskInputAttachment{{SourceScope: "task", SourceID: "task-1", URI: "ac://files/a", Name: "a.txt", MIME: "text/plain", Size: 1, Required: true, Canonical: true}},
			Downloader:  fakeTaskInputDownloader{root: filepath.Join(t.TempDir(), "missing"), err: errors.New("no bytes")},
		},
	})
	if err == nil {
		t.Fatal("expected task-input failure")
	}
	if started {
		t.Fatal("spawner must not start when task-input materialization fails")
	}
	if pool.Active() != 0 {
		t.Fatalf("failed launch must release slot, active=%d", pool.Active())
	}
}

func imageBytes(t *testing.T, kind string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x + 10), G: uint8(y + 20), B: 30, A: 255})
		}
	}
	var b bytes.Buffer
	switch kind {
	case "png":
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
	case "jpeg":
		if err := jpeg.Encode(&b, img, nil); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown image kind %q", kind)
	}
	return b.Bytes()
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readJSONFile(t *testing.T, path string, out any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}
