package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/memory"
)

const codexMemoryCommitTimeout = 10 * time.Second

func (r *LocalRuntime) commitDirtyMemory(agentID string) {
	home, _, _, err := r.agentPaths(agentID)
	if err != nil {
		r.log("codex agent=%s: memory sync resolve home: %v", agentID, err)
		return
	}
	r.mu.Lock()
	displayName := r.state.DisplayName
	r.mu.Unlock()
	authorName, authorEmail := memorySyncIdentity(displayName, agentID)

	ctx, cancel := context.WithTimeout(context.Background(), codexMemoryCommitTimeout)
	defer cancel()
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
