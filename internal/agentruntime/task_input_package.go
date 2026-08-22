package agentruntime

import (
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
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	taskInputDirName         = "task-input"
	taskInputVersion         = "v1"
	taskInputManifestName    = "manifest.json"
	taskInputCompleteName    = ".complete"
	taskInputIncompleteName  = ".incomplete"
	taskInputAttachmentsName = "attachments"
)

// FileDownloader is the raw-byte counterpart to ToolCaller. *workerdaemon.FileTransferClient
// satisfies this shape; tests use a fake.
type FileDownloader interface {
	DownloadFile(ctx context.Context, agentRoot, agentID, ulidOrURI, destPath string) error
}

type centerTaskFile struct {
	URI       string `json:"uri"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type taskInputPackage struct {
	Version      string                 `json:"version"`
	TaskID       string                 `json:"task_id"`
	AgentID      string                 `json:"agent_id"`
	PackageDir   string                 `json:"package_dir"`
	Materialized time.Time              `json:"materialized_at"`
	Files        []taskInputPackageFile `json:"files"`
}

type taskInputPackageFile struct {
	URI       string `json:"uri"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	LocalPath string `json:"local_path"`
}

func (r *LocalRuntime) materializeTaskInputPackage(ctx context.Context, agentID, taskID, execID string, fx *executorFileLayout) (*taskInputPackage, error) {
	files, err := r.listTaskFiles(ctx, agentID, taskID)
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].URI == files[j].URI {
			return files[i].Filename < files[j].Filename
		}
		return files[i].URI < files[j].URI
	})

	pkgDir, err := fx.taskInputDir(execID)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(pkgDir, taskInputManifestName)
	if pkg, ok, err := readCompleteTaskInputPackage(manifestPath, taskID, pkgDir, files); err != nil {
		return nil, err
	} else if ok {
		return pkg, nil
	}
	if err := rejectIncompleteOrStalePackage(pkgDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(pkgDir, taskInputAttachmentsName), 0o700); err != nil {
		return nil, fmt.Errorf("task-input: mkdir package: %w", err)
	}
	incomplete := filepath.Join(pkgDir, taskInputIncompleteName)
	if err := os.WriteFile(incomplete, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("task-input: mark incomplete: %w", err)
	}

	dl := r.fileDownloader()
	out := &taskInputPackage{
		Version:      taskInputVersion,
		TaskID:       taskID,
		AgentID:      agentID,
		PackageDir:   pkgDir,
		Materialized: r.now().UTC(),
		Files:        make([]taskInputPackageFile, 0, len(files)),
	}
	used := map[string]int{}
	for _, f := range files {
		if strings.TrimSpace(f.URI) == "" {
			return nil, fmt.Errorf("task-input: list_files returned file with empty uri")
		}
		name, err := uniquePackageFilename(f.Filename, f.URI, used)
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(pkgDir, taskInputAttachmentsName, name)
		if dl == nil {
			return nil, fmt.Errorf("task-input: file downloader unavailable for %s", f.URI)
		}
		if err := dl.DownloadFile(ctx, fx.agentRoot, agentID, f.URI, dst); err != nil {
			return nil, fmt.Errorf("task-input: download %s: %w", f.URI, err)
		}
		got, err := inspectMaterializedFile(dst)
		if err != nil {
			return nil, fmt.Errorf("task-input: inspect %s: %w", f.URI, err)
		}
		if f.Size >= 0 && got.Size != f.Size {
			return nil, fmt.Errorf("task-input: size mismatch for %s: got %d want %d", f.URI, got.Size, f.Size)
		}
		if want := strings.ToLower(strings.TrimSpace(f.SHA256)); want != "" && got.SHA256 != want {
			return nil, fmt.Errorf("task-input: sha256 mismatch for %s: got %s want %s", f.URI, got.SHA256, want)
		}
		if f.Width > 0 && got.Width != f.Width {
			return nil, fmt.Errorf("task-input: width mismatch for %s: got %d want %d", f.URI, got.Width, f.Width)
		}
		if f.Height > 0 && got.Height != f.Height {
			return nil, fmt.Errorf("task-input: height mismatch for %s: got %d want %d", f.URI, got.Height, f.Height)
		}
		out.Files = append(out.Files, taskInputPackageFile{
			URI:       f.URI,
			Filename:  name,
			MimeType:  f.MimeType,
			Size:      got.Size,
			SHA256:    got.SHA256,
			Width:     got.Width,
			Height:    got.Height,
			LocalPath: filepath.Join(taskInputAttachmentsName, name),
		})
	}
	if err := writeTaskInputManifestAtomic(manifestPath, out); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(pkgDir, taskInputCompleteName), []byte("ok\n"), 0o600); err != nil {
		return nil, fmt.Errorf("task-input: mark complete: %w", err)
	}
	if err := os.Remove(incomplete); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("task-input: clear incomplete: %w", err)
	}
	return out, nil
}

func (r *LocalRuntime) listTaskFiles(ctx context.Context, agentID, taskID string) ([]centerTaskFile, error) {
	caller := r.toolCaller()
	if caller == nil {
		return nil, fmt.Errorf("task-input: no center transport")
	}
	var raw json.RawMessage
	body := map[string]any{"agent_id": agentID, "scope": "task", "scope_id": taskID}
	if err := caller.CallAgentTool(ctx, "list_files", body, &raw); err != nil {
		return nil, fmt.Errorf("task-input: list_files task=%s: %w", taskID, err)
	}
	var resp struct {
		Files []centerTaskFile `json:"files"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("task-input: decode list_files: %w", err)
		}
	}
	if resp.Files == nil {
		resp.Files = []centerTaskFile{}
	}
	return resp.Files, nil
}

func (r *LocalRuntime) fileDownloader() FileDownloader {
	if r.cfg.FileDownloader == nil {
		return nil
	}
	return r.cfg.FileDownloader()
}

type executorFileLayout struct {
	agentRoot string
	execDir   func(string) (string, error)
}

func (l *executorFileLayout) taskInputDir(execID string) (string, error) {
	dir, err := l.execDir(execID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, taskInputDirName, taskInputVersion), nil
}

func readCompleteTaskInputPackage(manifestPath, taskID, pkgDir string, files []centerTaskFile) (*taskInputPackage, bool, error) {
	if _, err := os.Stat(filepath.Join(pkgDir, taskInputCompleteName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, false, fmt.Errorf("task-input: complete package missing manifest: %w", err)
	}
	var pkg taskInputPackage
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil, false, fmt.Errorf("task-input: decode complete manifest: %w", err)
	}
	if pkg.Version != taskInputVersion || pkg.TaskID != taskID || filepath.Clean(pkg.PackageDir) != filepath.Clean(pkgDir) {
		return nil, false, fmt.Errorf("task-input: stale complete package for task=%s dir=%s", pkg.TaskID, pkg.PackageDir)
	}
	if len(pkg.Files) != len(files) {
		return nil, false, fmt.Errorf("task-input: stale complete package file count=%d want=%d", len(pkg.Files), len(files))
	}
	byURI := map[string]taskInputPackageFile{}
	for _, f := range pkg.Files {
		byURI[f.URI] = f
	}
	for _, want := range files {
		got, ok := byURI[want.URI]
		if !ok {
			return nil, false, fmt.Errorf("task-input: stale complete package missing %s", want.URI)
		}
		if want.Size >= 0 && got.Size != want.Size {
			return nil, false, fmt.Errorf("task-input: stale complete package size mismatch for %s", want.URI)
		}
		if sha := strings.ToLower(strings.TrimSpace(want.SHA256)); sha != "" && got.SHA256 != sha {
			return nil, false, fmt.Errorf("task-input: stale complete package sha mismatch for %s", want.URI)
		}
	}
	for _, f := range pkg.Files {
		if strings.Contains(f.LocalPath, "..") || filepath.IsAbs(f.LocalPath) {
			return nil, false, fmt.Errorf("task-input: complete manifest contains unsafe path %q", f.LocalPath)
		}
		if _, err := os.Stat(filepath.Join(pkgDir, f.LocalPath)); err != nil {
			return nil, false, fmt.Errorf("task-input: complete package unreadable file %s: %w", f.LocalPath, err)
		}
	}
	return &pkg, true, nil
}

func rejectIncompleteOrStalePackage(pkgDir string) error {
	if _, err := os.Stat(filepath.Join(pkgDir, taskInputIncompleteName)); err == nil {
		return fmt.Errorf("task-input: incomplete package exists at %s", pkgDir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(pkgDir); err == nil {
		entries, rerr := os.ReadDir(pkgDir)
		if rerr != nil {
			return rerr
		}
		if len(entries) > 0 {
			return fmt.Errorf("task-input: stale unversioned/incomplete package exists at %s", pkgDir)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func inspectMaterializedFile(path string) (taskInputPackageFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return taskInputPackageFile{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return taskInputPackageFile{}, err
	}
	out := taskInputPackageFile{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}
	if _, err := f.Seek(0, 0); err == nil {
		if cfg, _, derr := image.DecodeConfig(f); derr == nil {
			out.Width = cfg.Width
			out.Height = cfg.Height
		}
	}
	return out, nil
}

func writeTaskInputManifestAtomic(path string, pkg *taskInputPackage) error {
	b, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("task-input: marshal manifest: %w", err)
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("task-input: write manifest temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("task-input: commit manifest: %w", err)
	}
	return nil
}

var safeFilenameRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func uniquePackageFilename(filename, uri string, used map[string]int) (string, error) {
	base := strings.TrimSpace(filename)
	if base == "" {
		base = strings.TrimPrefix(strings.TrimSpace(uri), "ac://files/")
	}
	base = filepath.Base(base)
	base = safeFilenameRE.ReplaceAllString(base, "_")
	base = strings.Trim(base, "._")
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("task-input: unsafe filename %q for %s", filename, uri)
	}
	if strings.Contains(base, "..") || strings.ContainsAny(base, `/\:`) {
		return "", fmt.Errorf("task-input: unsafe filename %q for %s", filename, uri)
	}
	name := base
	if n := used[base]; n > 0 {
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		name = fmt.Sprintf("%s-%d%s", stem, n+1, ext)
	}
	used[base]++
	return name, nil
}
