-- 0101_v229_agent_installed_skills.up.sql — skill observability (issue-4a45e9cc).
--
-- The declared `agents.skills` list was a static, never-verified WISH; it told you
-- nothing about which skills the runtime ACTUALLY resolved. This table replaces that
-- wish with the OBSERVED reality: the effective skill set the agent-runtime resolved
-- on disk, grouped into the four claude-code precedence LAYERS (built-in / plugin /
-- user / project), each skill carrying its name + description + whether it is SHADOWED
-- (a higher-precedence layer defines the same name, so this copy is inert).
--
-- Flow: the agent-runtime collects the resolved set on boot + on heartbeat, fingerprints
-- it, and re-uploads through the EXISTING agent→center channel only when the fingerprint
-- changes. The center replaces the agent's whole row set on each upload (delete-by-agent
-- + insert), so this table always mirrors the last reported reality.
--
-- Shape mirrors the append-style child tables (0100 pm_audit_log / 0070): TEXT PK,
-- collected_at TEXT (RFC3339), NO version / NO updated_at / NO FK REFERENCES (the schema
-- is uniformly FK-free / app-managed; agent_ref is a logical reference to agents.id).
-- shadowed is an INTEGER 0/1 boolean (SQLite + PG common subset, mirrors other bool
-- columns bound via boolToInt). collected_at is the batch collection time so an offline
-- agent's panel can show "last collected X ago".
--
-- UPGRADE SAFETY: purely additive — a new empty table + one lookup index, nothing else
-- touched. Empty table + no write point exercised until a runtime uploads = 零回归.
--
-- DIALECT NOTE: CREATE TABLE + CREATE INDEX in the SQLite + PG common subset (mirrors 0100).
CREATE TABLE agent_installed_skills (
    id           TEXT PRIMARY KEY,
    agent_ref    TEXT NOT NULL,
    layer        TEXT NOT NULL,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    shadowed     INTEGER NOT NULL DEFAULT 0,
    collected_at TEXT NOT NULL
);

-- Primary read path: an agent's full installed-skill set (the §query API reads this,
-- grouping by layer in the app layer). Also the delete-by-agent boundary on re-upload.
CREATE INDEX idx_agent_installed_skills_agent ON agent_installed_skills (agent_ref, layer, name);
