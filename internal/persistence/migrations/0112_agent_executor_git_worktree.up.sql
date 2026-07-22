-- Per-agent opt-in for isolated executor git worktrees. Default OFF preserves
-- the existing plain-workspace behavior until an operator enables it.
ALTER TABLE agents ADD COLUMN executor_git_worktree INTEGER NOT NULL DEFAULT 0;
