package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

// InstalledSkillRepo implements agent.InstalledSkillRepository over
// agent_installed_skills (migration 0101). The set is REPLACED wholesale per agent on
// each runtime report — the runtime is the single source of truth for its current
// on-disk skill state, so there is no incremental upsert, only delete-by-agent +
// insert-all inside one transaction.
type InstalledSkillRepo struct{ db *sql.DB }

// NewInstalledSkillRepo constructs the repo.
func NewInstalledSkillRepo(db *sql.DB) *InstalledSkillRepo { return &InstalledSkillRepo{db: db} }

// compile-time check.
var _ agent.InstalledSkillRepository = (*InstalledSkillRepo)(nil)

// ReplaceForAgent atomically swaps the agent's whole installed-skill set. It runs the
// delete + inserts inside a tx (composing with any ambient tx via ExecutorFromCtx, or
// opening its own) so a reader never observes a half-replaced set. The row PK is a
// deterministic composite (agent_ref\x1flayer\x1fname): rows are never referenced by id
// externally, and (agent_ref, layer, name) is unique after normalization — the same
// name may recur across layers (the shadow case), so layer is part of the key.
func (r *InstalledSkillRepo) ReplaceForAgent(ctx context.Context, agentRef agent.AgentID, skills []agent.InstalledSkill) error {
	return persistence.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		exec, _ := persistence.ExecutorFromCtx(txCtx, r.db)
		if _, err := exec.ExecContext(txCtx,
			`DELETE FROM agent_installed_skills WHERE agent_ref = ?`, string(agentRef)); err != nil {
			return err
		}
		for _, s := range skills {
			id := string(agentRef) + "\x1f" + string(s.Layer) + "\x1f" + s.Name
			if _, err := exec.ExecContext(txCtx,
				`INSERT INTO agent_installed_skills (id, agent_ref, layer, name, description, shadowed, collected_at)
				 VALUES (?,?,?,?,?,?,?)`,
				id, string(agentRef), string(s.Layer), s.Name, s.Description,
				boolToInt(s.Shadowed), ts(s.CollectedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

const installedSkillSelect = `SELECT agent_ref, layer, name, description, shadowed, collected_at
	FROM agent_installed_skills`

// ListByAgent returns the agent's installed skills ordered by layer precedence
// (built-in→plugin→user→project) then name — the same stable order the API groups on.
// The CASE keeps the SQL ordering identical to agent.SkillLayer.Rank() so the read
// path and the domain agree without post-sorting.
func (r *InstalledSkillRepo) ListByAgent(ctx context.Context, agentRef agent.AgentID) ([]agent.InstalledSkill, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		installedSkillSelect+` WHERE agent_ref = ?
			ORDER BY CASE layer
				WHEN 'built-in' THEN 0 WHEN 'plugin' THEN 1 WHEN 'user' THEN 2 WHEN 'project' THEN 3 ELSE 4 END,
				name`,
		string(agentRef))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]agent.InstalledSkill, 0)
	for rows.Next() {
		var (
			ref, layer, name, desc, collectedAt string
			shadowed                            int
		)
		if err := rows.Scan(&ref, &layer, &name, &desc, &shadowed, &collectedAt); err != nil {
			return nil, err
		}
		ca, _ := time.Parse(time.RFC3339Nano, collectedAt)
		out = append(out, agent.InstalledSkill{
			AgentRef:    agent.AgentID(ref),
			Layer:       agent.SkillLayer(layer),
			Name:        name,
			Description: desc,
			Shadowed:    shadowed != 0,
			CollectedAt: ca,
		})
	}
	return out, rows.Err()
}
