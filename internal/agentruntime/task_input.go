package agentruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

const taskInputVersion = "v1"

var sha256HexRE = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

type FileDownloader interface {
	DownloadCenterFile(ctx context.Context, agentID, fileURI string) ([]byte, error)
}

type taskInputFile struct {
	URI      string `json:"uri"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Path     string `json:"path"`
}

type taskInputManifest struct {
	Version        string          `json:"version"`
	TaskID         string          `json:"task_id"`
	SourceTool     string          `json:"source_tool"`
	MaterializedAt time.Time       `json:"materialized_at"`
	Files          []taskInputFile `json:"files"`
}

type listTaskFilesResponse struct {
	Files []taskInputFile `json:"files"`
}

func (r *LocalRuntime) materializeTaskInputPackage(ctx context.Context, agentID, taskID, wsPath string) (*executor.TaskInputPackage, error) {
	caller := r.toolCaller()
	if caller == nil {
		return nil, errors.New("task-input: center transport unavailable")
	}
	if strings.TrimSpace(wsPath) == "" {
		return nil, errors.New("task-input: workspace path required")
	}
	var raw json.RawMessage
	if err := caller.CallAgentTool(ctx, "list_files", map[string]any{
		"agent_id": agentID,
		"scope":    "task",
		"scope_id": taskID,
	}, &raw); err != nil {
		return nil, fmt.Errorf("task-input: list_files task=%s: %w", taskID, err)
	}
	var resp listTaskFilesResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("task-input: decode list_files: %w", err)
		}
	}
	if len(resp.Files) == 0 {
		return nil, nil
	}
	files := append([]taskInputFile(nil), resp.Files...)
	if err := validateListedTaskFiles(files); err != nil {
		return nil, err
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].URI < files[j].URI
	})
	dir := filepath.Join(wsPath, "task-input", taskInputVersion)
	if ok, err := reusableTaskInputPackage(dir, taskID, files); err != nil {
		return nil, err
	} else if ok {
		return &executor.TaskInputPackage{Version: taskInputVersion, RelativeDir: "task-input/" + taskInputVersion, Manifest: "task-input/" + taskInputVersion + "/manifest.json"}, nil
	}
	downloader := r.cfg.FileDownloader
	if downloader == nil {
		if c, err := NewCenterHTTPClient(r.cfg.AdminURL, r.cfg.ServerFingerprint, r.cfg.WorkerToken, 0); err == nil {
			downloader = c
		}
	}
	if downloader == nil {
		return nil, errors.New("task-input: file downloader unavailable")
	}
	stage := filepath.Join(wsPath, "task-input", "."+taskInputVersion+".tmp")
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return nil, fmt.Errorf("task-input: create staging: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := os.MkdirAll(filepath.Join(stage, "files"), 0o700); err != nil {
		return nil, fmt.Errorf("task-input: create files dir: %w", err)
	}
	for i := range files {
		raw, err := downloader.DownloadCenterFile(ctx, agentID, files[i].URI)
		if err != nil {
			return nil, fmt.Errorf("task-input: download %s: %w", files[i].URI, err)
		}
		if err := verifyTaskInputBytes(&files[i], raw); err != nil {
			return nil, err
		}
		files[i].Path = "files/" + files[i].Filename
		if err := os.WriteFile(filepath.Join(stage, files[i].Path), raw, 0o600); err != nil {
			return nil, fmt.Errorf("task-input: write %s: %w", files[i].Filename, err)
		}
	}
	manifest := taskInputManifest{Version: taskInputVersion, TaskID: taskID, SourceTool: "list_files", MaterializedAt: r.now().UTC(), Files: files}
	mb, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	mb = append(mb, '\n')
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), mb, 0o600); err != nil {
		return nil, fmt.Errorf("task-input: write manifest: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("task-input: remove stale package: %w", err)
	}
	if err := os.Rename(stage, dir); err != nil {
		return nil, fmt.Errorf("task-input: publish package: %w", err)
	}
	return &executor.TaskInputPackage{Version: taskInputVersion, RelativeDir: "task-input/" + taskInputVersion, Manifest: "task-input/" + taskInputVersion + "/manifest.json"}, nil
}

func validateListedTaskFiles(files []taskInputFile) error {
	seen := map[string]struct{}{}
	for _, f := range files {
		if strings.TrimSpace(f.URI) == "" {
			return errors.New("task-input: file uri required")
		}
		if strings.TrimSpace(f.Filename) == "" {
			return fmt.Errorf("task-input: filename required for %s", f.URI)
		}
		if f.Filename != filepath.Base(f.Filename) || strings.Contains(f.Filename, "..") {
			return fmt.Errorf("task-input: invalid filename %q", f.Filename)
		}
		if _, ok := seen[f.Filename]; ok {
			return fmt.Errorf("task-input: duplicate filename %q", f.Filename)
		}
		seen[f.Filename] = struct{}{}
		if strings.TrimSpace(f.MimeType) == "" {
			return fmt.Errorf("task-input: mime_type required for %s", f.URI)
		}
		if f.Size <= 0 {
			return fmt.Errorf("task-input: positive size required for %s", f.URI)
		}
		if !sha256HexRE.MatchString(strings.TrimSpace(f.SHA256)) {
			return fmt.Errorf("task-input: valid sha256 required for %s", f.URI)
		}
	}
	return nil
}

func verifyTaskInputBytes(f *taskInputFile, raw []byte) error {
	if int64(len(raw)) != f.Size {
		return fmt.Errorf("task-input: size mismatch for %s: got %d want %d", f.URI, len(raw), f.Size)
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, f.SHA256) {
		return fmt.Errorf("task-input: sha256 mismatch for %s: got %s want %s", f.URI, got, f.SHA256)
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.MimeType)), "image/") {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("task-input: decode image dimensions for %s: %w", f.URI, err)
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			return fmt.Errorf("task-input: invalid image dimensions for %s", f.URI)
		}
		if f.Width > 0 && f.Width != cfg.Width {
			return fmt.Errorf("task-input: image width mismatch for %s: got %d want %d", f.URI, cfg.Width, f.Width)
		}
		if f.Height > 0 && f.Height != cfg.Height {
			return fmt.Errorf("task-input: image height mismatch for %s: got %d want %d", f.URI, cfg.Height, f.Height)
		}
		f.Width, f.Height = cfg.Width, cfg.Height
	}
	return nil
}

func reusableTaskInputPackage(dir, taskID string, files []taskInputFile) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("task-input: read existing manifest: %w", err)
	}
	var m taskInputManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, fmt.Errorf("task-input: reject incomplete package: %w", err)
	}
	if m.Version != taskInputVersion || m.TaskID != taskID || len(m.Files) != len(files) {
		return false, fmt.Errorf("task-input: stale package rejected")
	}
	for i := range files {
		if m.Files[i].URI != files[i].URI || m.Files[i].Filename != files[i].Filename || !strings.EqualFold(m.Files[i].SHA256, files[i].SHA256) {
			return false, fmt.Errorf("task-input: stale package rejected")
		}
		if _, err := os.Stat(filepath.Join(dir, m.Files[i].Path)); err != nil {
			return false, fmt.Errorf("task-input: incomplete package rejected: %w", err)
		}
	}
	return true, nil
}

func taskInputFileULID(uri string) (string, error) {
	s := strings.TrimSpace(uri)
	if strings.HasPrefix(s, "ac://files/") {
		s = strings.TrimPrefix(s, "ac://files/")
	}
	if s == "" || strings.ContainsAny(s, `/\`) || strings.Contains(s, "..") {
		return "", fmt.Errorf("task-input: invalid file uri %q", uri)
	}
	return s, nil
}
