-- 0102_v229_drop_agent_declared_skills.down.sql — restore the declared skills
-- column as it was created by 0042_v27_agents (JSON array, default empty). Data is
-- NOT recoverable (the drop discarded it); rows come back with the '[]' default.
ALTER TABLE agents ADD COLUMN skills TEXT NOT NULL DEFAULT '[]';
