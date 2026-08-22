package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	taskInputDirName       = "task-input"
	taskInputVersion       = "v1"
	taskInputManifestName  = "manifest.json"
	taskInputContextName   = "context.json"
	taskInputReadmeName    = "README.md"
	taskInputAttachments   = "attachments"
	defaultMaxAttachment   = int64(100 << 20)
	defaultMaxTaskInputAll = int64(250 << 20)
)

var nonPortableNameChar = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// TaskInputDownloader materializes one attachment into workspace-relative
// destPath. Production supplies the same byte-mover used by download_file.
type TaskInputDownloader interface {
	DownloadTaskInputAttachment(ctx context.Context, attachment TaskInputAttachment, destPath string) error
}

// TaskInputSpec is the fork-time, self-contained task package contract.
type TaskInputSpec struct {
	TaskID       string                `json:"task_id,omitempty"`
	ExecutorID   string                `json:"executor_id,omitempty"`
	Goal         Goal                  `json:"goal"`
	Context      string                `json:"context,omitempty"`
	Source       SourceRefs            `json:"source"`
	CreatedAt    time.Time             `json:"created_at"`
	Attachments  []TaskInputAttachment `json:"attachments,omitempty"`
	Downloader   TaskInputDownloader   `json:"-"`
	MaxFileBytes int64                 `json:"max_file_bytes,omitempty"`
	MaxBytes     int64                 `json:"max_bytes,omitempty"`
}

// TaskInputAttachment is the manifest source contract for one required input file.
type TaskInputAttachment struct {
	SourceScope string `json:"source_scope"`
	SourceID    string `json:"source_id,omitempty"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	MIME        string `json:"mime"`
	Size        int64  `json:"size"`
	Required    bool   `json:"required"`
	Canonical   bool   `json:"canonical"`
}

// TaskInputManifest is written as task-input/v1/manifest.json.
type TaskInputManifest struct {
	Version     string                  `json:"version"`
	TaskID      string                  `json:"task_id,omitempty"`
	ExecutorID  string                  `json:"executor_id,omitempty"`
	CreatedAt   string                  `json:"created_at"`
	Attachments []TaskInputManifestFile `json:"attachments"`
}

type TaskInputManifestFile struct {
	SourceScope string `json:"source_scope"`
	SourceID    string `json:"source_id,omitempty"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	MIME        string `json:"mime"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Path        string `json:"path"`
	Required    bool   `json:"required"`
	Canonical   bool   `json:"canonical"`
}

func (fx *FileExchange) TaskInputDir(executorID string) (string, error) {
	ws, err := fx.layout.WorkspaceDir(executorID)
	if err != nil {
		return "", err
	}
	return filepath.Join(ws, taskInputDirName, taskInputVersion), nil
}

func (fx *FileExchange) MaterializeTaskInput(ctx context.Context, executorID, workspaceDir string, spec TaskInputSpec) (string, error) {
	if strings.TrimSpace(workspaceDir) == "" {
		return "", errors.New("executor: task-input workspace required")
	}
	if !filepath.IsAbs(workspaceDir) {
		return "", errors.New("executor: task-input workspace must be absolute")
	}
	ws := filepath.Clean(workspaceDir)
	if spec.ExecutorID == "" {
		spec.ExecutorID = executorID
	}
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = fx.clk.Now().UTC()
	}
	maxFile := spec.MaxFileBytes
	if maxFile <= 0 {
		maxFile = defaultMaxAttachment
	}
	maxAll := spec.MaxBytes
	if maxAll <= 0 {
		maxAll = defaultMaxTaskInputAll
	}
	if len(spec.Attachments) > 0 && spec.Downloader == nil {
		return "", errors.New("executor: task-input downloader required for attachments")
	}

	finalDir := filepath.Join(ws, taskInputDirName, taskInputVersion)
	parent := filepath.Dir(finalDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("mkdir task-input parent: %w", err)
	}
	tmp, err := os.MkdirTemp(parent, "."+taskInputVersion+".tmp-")
	if err != nil {
		return "", fmt.Errorf("create task-input temp dir: %w", err)
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := os.MkdirAll(filepath.Join(tmp, taskInputAttachments), 0o700); err != nil {
		return "", fmt.Errorf("mkdir task-input attachments: %w", err)
	}

	manifest := TaskInputManifest{
		Version:    taskInputVersion,
		TaskID:     spec.TaskID,
		ExecutorID: spec.ExecutorID,
		CreatedAt:  spec.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	var total int64
	used := map[string]int{}
	for i, att := range spec.Attachments {
		if err := validateTaskInputAttachment(att, maxFile); err != nil {
			return "", fmt.Errorf("attachment %d invalid: %w", i, err)
		}
		total += att.Size
		if total > maxAll {
			return "", fmt.Errorf("task-input attachments total %d exceeds limit %d", total, maxAll)
		}
		leaf := uniqueAttachmentLeaf(used, i, att.Name)
		rel := filepath.ToSlash(filepath.Join(taskInputAttachments, leaf))
		destAbs := filepath.Join(tmp, filepath.FromSlash(rel))
		destRel, err := filepath.Rel(ws, destAbs)
		if err != nil || strings.HasPrefix(destRel, "..") || filepath.IsAbs(destRel) {
			return "", fmt.Errorf("attachment %q staging path escapes workspace", rel)
		}
		if err := spec.Downloader.DownloadTaskInputAttachment(ctx, att, filepath.ToSlash(destRel)); err != nil {
			return "", fmt.Errorf("download required attachment %q (%s): %w", att.Name, att.URI, err)
		}
		sum, size, err := sha256AndSize(destAbs)
		if err != nil {
			return "", fmt.Errorf("verify attachment %q: %w", rel, err)
		}
		if size != att.Size {
			return "", fmt.Errorf("verify attachment %q: size %d != manifest source size %d", rel, size, att.Size)
		}
		manifest.Attachments = append(manifest.Attachments, TaskInputManifestFile{
			SourceScope: att.SourceScope,
			SourceID:    att.SourceID,
			URI:         att.URI,
			Name:        att.Name,
			MIME:        att.MIME,
			Size:        size,
			SHA256:      sum,
			Path:        rel,
			Required:    att.Required,
			Canonical:   att.Canonical,
		})
	}

	if err := writeTaskInputReadme(filepath.Join(tmp, taskInputReadmeName), spec, manifest); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(tmp, taskInputContextName), taskInputContext(spec)); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(tmp, taskInputManifestName), manifest); err != nil {
		return "", err
	}
	_ = os.RemoveAll(finalDir)
	if err := os.Rename(tmp, finalDir); err != nil {
		return "", fmt.Errorf("publish task-input package: %w", err)
	}
	cleanupTmp = false
	return finalDir, nil
}

func validateTaskInputAttachment(att TaskInputAttachment, maxFile int64) error {
	if strings.TrimSpace(att.URI) == "" {
		return errors.New("uri required")
	}
	if strings.TrimSpace(att.SourceScope) == "" {
		return errors.New("source_scope required")
	}
	if strings.TrimSpace(att.Name) == "" {
		return errors.New("name required")
	}
	if att.Size < 0 {
		return errors.New("size must be non-negative")
	}
	if att.Size > maxFile {
		return fmt.Errorf("size %d exceeds per-file limit %d", att.Size, maxFile)
	}
	if !att.Required && !att.Canonical {
		return errors.New("attachment must be required or canonical for fork-time materialization")
	}
	return nil
}

func uniqueAttachmentLeaf(used map[string]int, index int, name string) string {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	base = strings.TrimSpace(base)
	if base == "." || base == "" {
		base = fmt.Sprintf("attachment-%03d", index+1)
	}
	base = nonPortableNameChar.ReplaceAllString(base, "_")
	if base == "." || base == ".." {
		base = fmt.Sprintf("attachment-%03d", index+1)
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = fmt.Sprintf("attachment-%03d", index+1)
	}
	n := used[base]
	used[base] = n + 1
	if n == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d%s", stem, n+1, ext)
}

func sha256AndSize(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func writeTaskInputReadme(path string, spec TaskInputSpec, manifest TaskInputManifest) error {
	var b strings.Builder
	b.WriteString("# Task Input\n\n")
	b.WriteString("This directory was materialized by the supervisor/runtime before the executor process was forked.\n")
	b.WriteString("It is the executor's self-contained task contract and attachment package.\n\n")
	if spec.TaskID != "" {
		b.WriteString("- Task ID: `" + spec.TaskID + "`\n")
	}
	if spec.ExecutorID != "" {
		b.WriteString("- Executor ID: `" + spec.ExecutorID + "`\n")
	}
	if spec.Goal.Title != "" {
		b.WriteString("- Title: " + spec.Goal.Title + "\n")
	}
	b.WriteString("\nFiles:\n")
	b.WriteString("- `context.json`: frozen task metadata and prompt context.\n")
	b.WriteString("- `manifest.json`: attachment source URI/name/MIME/size/SHA256/path records.\n")
	b.WriteString("- `attachments/`: original attachment bytes.\n\n")
	if len(manifest.Attachments) == 0 {
		b.WriteString("No attachments were provided for this task.\n")
	} else {
		b.WriteString("Attachments:\n")
		for _, a := range manifest.Attachments {
			b.WriteString(fmt.Sprintf("- `%s` %s size=%d sha256=%s source=%s\n", a.Path, a.MIME, a.Size, a.SHA256, a.URI))
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func taskInputContext(spec TaskInputSpec) map[string]any {
	b, _ := json.Marshal(spec)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
