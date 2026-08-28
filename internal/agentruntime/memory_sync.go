package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/memory"
)

const codexMemoryCommitTimeout = 10 * time.Second

func (r *LocalRuntime) commitDirtyMemory(ctx context.Context, agentID string) {
	if err := ctx.Err(); err != nil {
		return
	}
	home, _, _, err := r.agentPaths(agentID)
	if err != nil {
		r.log("codex agent=%s: memory sync resolve home: %v", agentID, err)
		return
	}
	r.mu.Lock()
	displayName := r.state.DisplayName
	r.mu.Unlock()
	authorName, authorEmail := memorySyncIdentity(displayName, agentID)

	if err := ctx.Err(); err != nil {
		return
	}
	memEngine := memory.NewEngine(filepath.Join(home, "memory"), "")
	if err := memEngine.CommitDirty(ctx, authorName, authorEmail, ""); err != nil {
		r.log("codex agent=%s: memory sync: %v", agentID, err)
	}
}

func memorySyncIdentity(displayName, agentID string) (name, email string) {
	name = strings.TrimSpace(displayName)
	if name == "" {
		name = agentID
	}
	return name, agentID + "@agent-center"
}
