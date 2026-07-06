package taskexec

import (
	"os"
	"path/filepath"
	"time"
)

// GCConfig controls retention periods (design §11.3).
type GCConfig struct {
	AbortedRetention time.Duration // default 7d
	DoneRetention    time.Duration // default 3d
}

// DefaultGCConfig returns the design-specified defaults.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		AbortedRetention: 7 * 24 * time.Hour,
		DoneRetention:    3 * 24 * time.Hour,
	}
}

// GCResult reports what was cleaned.
type GCResult struct {
	AbortedCleaned  int
	DoneCleaned     int
	LeftoverCleaned int
	Errors          []error
}

// RunGC scans tasksDir and removes expired directories (design §11.3).
//
// Three categories:
// 1. __gc_deleting leftovers → always remove (previous GC crash recovery)
// 2. __aborted_ directories → remove if older than AbortedRetention
// 3. Standard task dirs with status=done → remove if UpdatedAt older than DoneRetention
func RunGC(tasksDir string, cfg GCConfig, now time.Time) GCResult {
	var result GCResult
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if !os.IsNotExist(err) {
			result.Errors = append(result.Errors, err)
		}
		return result
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()

		// 1. gc_deleting leftovers: always clean up
		_, _, gcDeleting, isAborted := ParseAbortedDir(name)
		if gcDeleting {
			if err := os.RemoveAll(filepath.Join(tasksDir, name)); err != nil {
				result.Errors = append(result.Errors, err)
			} else {
				result.LeftoverCleaned++
			}
			continue
		}

		// 2. Aborted directories: check retention
		if isAborted {
			_, abortTs, _, _ := ParseAbortedDir(name)
			if !abortTs.IsZero() && now.Sub(abortTs) > cfg.AbortedRetention {
				// Two-phase delete: rename to gc_deleting, then remove
				gcName := GCDeletingDirName(name)
				src := filepath.Join(tasksDir, name)
				dst := filepath.Join(tasksDir, gcName)
				if err := os.Rename(src, dst); err != nil {
					result.Errors = append(result.Errors, err)
					continue
				}
				if err := os.RemoveAll(dst); err != nil {
					result.Errors = append(result.Errors, err)
				} else {
					result.AbortedCleaned++
				}
			}
			continue
		}

		// 3. Standard task dirs: check if done + expired
		if IsStandardTaskDir(name) {
			metaPath := filepath.Join(tasksDir, name, taskMetaFile)
			var meta TaskExecutionMeta
			if err := readJSON(metaPath, &meta); err != nil {
				continue // skip unreadable
			}
			if meta.Status == StatusDone && !meta.UpdatedAt.IsZero() && now.Sub(meta.UpdatedAt) > cfg.DoneRetention {
				if err := os.RemoveAll(filepath.Join(tasksDir, name)); err != nil {
					result.Errors = append(result.Errors, err)
				} else {
					result.DoneCleaned++
				}
			}
		}
	}
	return result
}
