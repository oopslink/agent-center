package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

const (
	taskInputDirName          = "task-input"
	taskInputVersion          = "v1"
	taskInputAttachmentsDir   = "attachments"
	taskInputManifestFileName = "manifest.json"
	taskInputReadmeFileName   = "README.md"
	taskInputMetadataFileName = "context.json"
	maxTaskInputFileBytes     = 100 << 20
)

type fileDownloader interface {
	DownloadFile(ctx context.Context, agentID, fileURI, destPath string) error
}

type taskInputFile struct {
	URI       string `json:"uri"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type taskInputManifest struct {
	Version     int                     `json:"version"`
	TaskID      string                  `json:"task_id"`
	GeneratedAt string                  `json:"generated_at"`
	Source      string                  `json:"source"`
	Files       []taskInputManifestFile `json:"files"`
}

type taskInputManifestFile struct {
	SourceScope string `json:"source_scope"`
	SourceID    string `json:"source_id"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	MimeType    string `json:"mime_type"`
	SizeBytes   int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Path        string `json:"path"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Canonical   bool   `json:"canonical"`
	Required    bool   `json:"required"`
}

func (r *LocalRuntime) materializeTaskInputPackage(ctx context.Context, agentID, taskID, execID, workspaceDir string, task *centerTaskDetail) (*executor.TaskInputRef, error) {
	files, err := r.listTaskInputFiles(ctx, agentID, taskID)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = []taskInputFile{}
	}
	var dl fileDownloader
	if len(files) > 0 {
		caller := r.toolCaller()
		var ok bool
		dl, ok = caller.(fileDownloader)
		if !ok || dl == nil {
			return nil, fmt.Errorf("task-input materialization: center transport cannot download files")
		}
	}
	root := filepath.Join(workspaceDir, taskInputDirName)
	stage := filepath.Join(workspaceDir, "."+taskInputDirName+".tmp")
	if err := os.RemoveAll(stage); err != nil {
		return nil, fmt.Errorf("task-input materialization: clean staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	versionStage := filepath.Join(stage, taskInputVersion)
	if err := os.MkdirAll(filepath.Join(versionStage, taskInputAttachmentsDir), 0o700); err != nil {
		return nil, fmt.Errorf("task-input materialization: mkdir staging: %w", err)
	}
	manifest := taskInputManifest{
		Version:     1,
		TaskID:      taskID,
		GeneratedAt: r.now().UTC().Format(time.RFC3339Nano),
		Source:      "task-scope list_files",
		Files:       make([]taskInputManifestFile, 0, len(files)),
	}
	usedNames := map[string]int{}
	for _, f := range files {
		if strings.TrimSpace(f.URI) == "" {
			return nil, fmt.Errorf("task-input materialization: task %s listed file with empty uri", taskID)
		}
		if f.SizeBytes < 0 || f.SizeBytes > maxTaskInputFileBytes {
			return nil, fmt.Errorf("task-input materialization: task %s file %s size %d exceeds limit %d", taskID, f.URI, f.SizeBytes, maxTaskInputFileBytes)
		}
		name := uniqueSafeFilename(f.Filename, usedNames)
		rel := filepath.Join(taskInputDirName, taskInputVersion, taskInputAttachmentsDir, name)
		stagePath := filepath.Join(versionStage, taskInputAttachmentsDir, name)
		tmpPath := stagePath + ".download"
		if err := dl.DownloadFile(ctx, agentID, f.URI, tmpPath); err != nil {
			return nil, fmt.Errorf("task-input materialization: download %s (%s): %w", f.URI, f.Filename, err)
		}
		gotSize, gotSHA, err := fileSizeSHA256(tmpPath)
		if err != nil {
			return nil, fmt.Errorf("task-input materialization: verify %s: %w", f.URI, err)
		}
		if gotSize != f.SizeBytes {
			return nil, fmt.Errorf("task-input materialization: size mismatch for %s: listed=%d downloaded=%d", f.URI, f.SizeBytes, gotSize)
		}
		if err := os.Rename(tmpPath, stagePath); err != nil {
			return nil, fmt.Errorf("task-input materialization: place %s: %w", f.URI, err)
		}
		width, height := imageDimensions(stagePath)
		manifest.Files = append(manifest.Files, taskInputManifestFile{
			SourceScope: "task",
			SourceID:    taskID,
			URI:         f.URI,
			Name:        f.Filename,
			MimeType:    f.MimeType,
			SizeBytes:   gotSize,
			SHA256:      gotSHA,
			Path:        filepath.ToSlash(rel),
			Width:       width,
			Height:      height,
			Canonical:   true,
			Required:    true,
		})
	}
	sort.SliceStable(manifest.Files, func(i, j int) bool {
		return manifest.Files[i].Path < manifest.Files[j].Path
	})
	if err := writeJSONFileAtomic(filepath.Join(versionStage, taskInputManifestFileName), manifest); err != nil {
		return nil, err
	}
	if err := writeJSONFileAtomic(filepath.Join(versionStage, taskInputMetadataFileName), map[string]any{
		"task_id":      taskID,
		"executor_id":  execID,
		"title":        task.goalTitle(taskID),
		"description":  task.Description,
		"project_id":   task.ProjectID,
		"stage_id":     task.StageID,
		"generated_at": manifest.GeneratedAt,
	}); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(versionStage, taskInputReadmeFileName), []byte(taskInputReadme(taskID, task, len(manifest.Files))), 0o600); err != nil {
		return nil, fmt.Errorf("task-input materialization: write README: %w", err)
	}
	if err := os.RemoveAll(root); err != nil {
		return nil, fmt.Errorf("task-input materialization: replace stale package: %w", err)
	}
	if err := os.Rename(stage, root); err != nil {
		return nil, fmt.Errorf("task-input materialization: publish package: %w", err)
	}
	return &executor.TaskInputRef{
		Dir:          filepath.ToSlash(filepath.Join(taskInputDirName, taskInputVersion)),
		ManifestPath: filepath.ToSlash(filepath.Join(taskInputDirName, taskInputVersion, taskInputManifestFileName)),
	}, nil
}

func imageDimensions(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func (r *LocalRuntime) listTaskInputFiles(ctx context.Context, agentID, taskID string) ([]taskInputFile, error) {
	caller := r.toolCaller()
	if caller == nil {
		return nil, fmt.Errorf("task-input materialization: no center transport")
	}
	var raw json.RawMessage
	if err := caller.CallAgentTool(ctx, "list_files", map[string]any{
		"agent_id": agentID,
		"scope":    "task",
		"scope_id": taskID,
	}, &raw); err != nil {
		return nil, fmt.Errorf("task-input materialization: list_files task %s: %w", taskID, err)
	}
	var resp struct {
		Files []taskInputFile `json:"files"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("task-input materialization: decode list_files: %w", err)
		}
	}
	return resp.Files, nil
}

func taskInputReadme(taskID string, task *centerTaskDetail, count int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task Input\n\n")
	fmt.Fprintf(&b, "Task: %s\n\n", taskID)
	fmt.Fprintf(&b, "This directory was materialized by the Supervisor before the executor was forked. It is self-contained for task metadata and task-scoped attachments.\n\n")
	fmt.Fprintf(&b, "Attachments: %d\n\n", count)
	fmt.Fprintf(&b, "Read `manifest.json` for canonical source URI, original name, MIME type, size, SHA256, and local path for every file. Attachment bytes are under `attachments/`.\n\n")
	if title := strings.TrimSpace(task.goalTitle(taskID)); title != "" {
		fmt.Fprintf(&b, "## Task\n\n%s\n", title)
	}
	return b.String()
}

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func uniqueSafeFilename(name string, used map[string]int) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == "/" || base == "" {
		base = "attachment"
	}
	base = unsafeFilenameChars.ReplaceAllString(base, "_")
	base = strings.Trim(base, "._")
	if base == "" {
		base = "attachment"
	}
	n := used[base]
	used[base] = n + 1
	if n == 0 {
		return base
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "attachment"
	}
	return fmt.Sprintf("%s-%d%s", stem, n+1, ext)
}

func fileSizeSHA256(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func writeJSONFileAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("task-input materialization: marshal %s: %w", filepath.Base(path), err)
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("task-input materialization: write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("task-input materialization: publish %s: %w", filepath.Base(path), err)
	}
	return nil
}
