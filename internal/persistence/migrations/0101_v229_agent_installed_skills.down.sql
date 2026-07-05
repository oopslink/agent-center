-- 0101_v229_agent_installed_skills.down.sql — reverse the installed-skills table.
DROP INDEX IF EXISTS idx_agent_installed_skills_agent;
DROP TABLE IF EXISTS agent_installed_skills;
