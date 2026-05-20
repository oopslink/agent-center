package workerdaemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/shim"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// GCSweeper removes per-execution directories older than RetentionHours
// and the corresponding worktree (when applicable). ADR-0018 § 9.
type GCSweeper struct {
	execBaseDir    string
	retention      time.Duration
	clk            clock.Clock
	workspace      *WorkspaceManager
	mappingResolver MappingResolver
	workerID        string
	onReleased      func(executionID string, mode execution.WorkspaceMode)
}

// NewGCSweeper constructs a sweeper.
func NewGCSweeper(execBaseDir string, retention time.Duration, clk clock.Clock, ws *WorkspaceManager, resolver MappingResolver, workerID string, onReleased func(string, execution.WorkspaceMode)) *GCSweeper {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if retention == 0 {
		retention = 24 * time.Hour
	}
	return &GCSweeper{
		execBaseDir: execBaseDir, retention: retention, clk: clk,
		workspace: ws, mappingResolver: resolver, workerID: workerID,
		onReleased: onReleased,
	}
}

// Sweep iterates per-execution dirs and deletes those past retention.
// Returns the list of execution_ids that were cleaned.
func (g *GCSweeper) Sweep(ctx context.Context) ([]string, error) {
	if g.execBaseDir == "" {
		return nil, errors.New("gc: exec_base_dir not set")
	}
	entries, err := os.ReadDir(g.execBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := g.clk.Now().Add(-g.retention)
	var cleaned []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(g.execBaseDir, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		// Try to find the workspace mode from envelope.json
		dir, err := shim.NewDir(g.execBaseDir, entry.Name())
		if err != nil {
			continue
		}
		mode := execution.WorkspaceDirect
		if envelopeBytes, err := dir.ReadEnvelope(); err == nil {
			if m, ok := parseWorkspaceModeFromEnvelope(envelopeBytes); ok {
				mode = m
			}
		}
		// Release worktree if applicable. We swallow the error because
		// `git worktree remove` may fail (e.g. base repo gone); GC keeps
		// going.
		if g.workspace != nil && g.mappingResolver != nil {
			if proj, ok := parseProjectIDFromEnvelope(envelopeBytesOrEmpty(dir)); ok {
				if bp, err := g.mappingResolver.ResolveBasePath(ctx, g.workerID, proj); err == nil {
					_ = g.workspace.Release(ctx, bp, mode, entry.Name())
				}
			}
		}
		if err := dir.Remove(); err != nil {
			continue
		}
		cleaned = append(cleaned, entry.Name())
		if g.onReleased != nil {
			g.onReleased(entry.Name(), mode)
		}
	}
	return cleaned, nil
}

func envelopeBytesOrEmpty(d *shim.Dir) []byte {
	b, _ := d.ReadEnvelope()
	return b
}
