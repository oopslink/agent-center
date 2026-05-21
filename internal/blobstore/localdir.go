package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// LocalDir is the v1 default BlobStore implementation backed by a local
// directory (01-blob-store.md "实现"). Concurrency-safe at the FS level
// (rename / O_TRUNC writes); no cross-process locking — single-server v1.
type LocalDir struct {
	root         string
	maxBytes     int64
}

// NewLocalDir returns a LocalDir rooted at root. The directory is created
// if absent.
func NewLocalDir(root string) (*LocalDir, error) {
	if root == "" {
		return nil, errors.New("blobstore: empty root path")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("blobstore: mkdir root: %w", err)
	}
	return &LocalDir{root: root, maxBytes: MaxBlobBytes}, nil
}

// WithMaxBytes lets callers override the per-blob size cap (plan-4 § 6.7).
func (l *LocalDir) WithMaxBytes(n int64) *LocalDir {
	if n > 0 {
		l.maxBytes = n
	}
	return l
}

// Root returns the local root directory (for diagnostics / URL).
func (l *LocalDir) Root() string { return l.root }

// Put writes content to root/relPath. Refuses uploads larger than maxBytes.
func (l *LocalDir) Put(_ context.Context, relPath string, content io.Reader, size int64) error {
	if err := validateRel(relPath); err != nil {
		return err
	}
	if size > l.maxBytes {
		return fmt.Errorf("%w: size=%d max=%d", ErrPayloadTooLarge, size, l.maxBytes)
	}
	abs := filepath.Join(l.root, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("blobstore: mkdir: %w", err)
	}
	// Atomic write: write to .tmp then rename.
	tmp := abs + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("blobstore: open tmp: %w", err)
	}
	// Cap the reader explicitly to guard against size=-1 paths.
	written, err := io.Copy(f, &limitedReader{R: content, N: l.maxBytes})
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		if errors.Is(err, errLimitedReader) {
			return fmt.Errorf("%w: exceeded max=%d", ErrPayloadTooLarge, l.maxBytes)
		}
		return fmt.Errorf("blobstore: copy: %w", err)
	}
	if size >= 0 && written != size {
		_ = os.Remove(tmp)
		return fmt.Errorf("blobstore: size mismatch: declared=%d written=%d", size, written)
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("blobstore: rename: %w", err)
	}
	return nil
}

// Get returns a ReadCloser for the blob.
func (l *LocalDir) Get(_ context.Context, relPath string) (io.ReadCloser, error) {
	if err := validateRel(relPath); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(l.root, relPath))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrBlobNotFound
		}
		return nil, fmt.Errorf("blobstore: open: %w", err)
	}
	return f, nil
}

// Delete removes the blob.
func (l *LocalDir) Delete(_ context.Context, relPath string) error {
	if err := validateRel(relPath); err != nil {
		return err
	}
	abs := filepath.Join(l.root, relPath)
	if err := os.Remove(abs); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrBlobNotFound
		}
		return fmt.Errorf("blobstore: delete: %w", err)
	}
	return nil
}

// Exists reports whether the blob is present.
func (l *LocalDir) Exists(_ context.Context, relPath string) (bool, error) {
	if err := validateRel(relPath); err != nil {
		return false, err
	}
	_, err := os.Stat(filepath.Join(l.root, relPath))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// List walks the prefix and returns relative paths.
func (l *LocalDir) List(_ context.Context, prefix string) ([]string, error) {
	abs := filepath.Join(l.root, prefix)
	var out []string
	err := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip half-written .tmp files (Put atomic-write residue).
		if strings.HasSuffix(p, ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(l.root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// URL returns a file:// URL.
func (l *LocalDir) URL(relPath string) string {
	abs, _ := filepath.Abs(filepath.Join(l.root, relPath))
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

func validateRel(rel string) error {
	if rel == "" {
		return errors.New("blobstore: empty relPath")
	}
	if strings.Contains(rel, "..") {
		return errors.New("blobstore: relPath contains '..'")
	}
	if filepath.IsAbs(rel) {
		return errors.New("blobstore: relPath must be relative")
	}
	return nil
}

// errLimitedReader is the sentinel limitedReader produces when N runs out.
var errLimitedReader = errors.New("blobstore: limit exceeded")

type limitedReader struct {
	R io.Reader
	N int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.N <= 0 {
		return 0, errLimitedReader
	}
	if int64(len(p)) > l.N {
		p = p[:l.N]
	}
	n, err := l.R.Read(p)
	l.N -= int64(n)
	return n, err
}
