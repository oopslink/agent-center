-- 0087_v2184_workspace_code_repos.up.sql — v2.18.4 BE-1 (issue-f980c8de).
--
-- Promote code repositories to a WORKSPACE-level (org-scoped) entity, parallel to
-- Projects/Issues/Tasks/Plans. Credentials are configured ONLY here, encrypted at
-- rest (AES-GCM via the secretmgmt master key, like user_secrets). A project links
-- to a workspace Repo through the EXISTING pm_code_repo_refs (which was already a
-- reference, not a VCS integration) — so this is additive, NO data migration: a
-- legacy url-only ref keeps working and the F3 merge-check resolver falls back to it.
--
-- code_repos: the workspace Repo. credential_ciphertext/nonce are NULLABLE (a repo
-- may have no credential — public repo / ls-remote). The API masks the credential
-- and never returns plaintext.
--
-- pm_code_repo_refs gains:
--   repo_id     — nullable FK-by-convention to code_repos.id. NULL = a legacy
--                 url-only ref (pre-0087 rows, still valid). Non-NULL = a reference
--                 to a workspace Repo (project side stores no url/credential).
--   is_primary  — per-project primary selector (the merge-check primaryRepoURL reads
--                 the primary ref → its workspace Repo url). DEFAULT 0; at most one
--                 primary per project is an application-layer invariant (BE-1 service).
--
-- UPGRADE SAFETY: one CREATE TABLE + two additive ADD COLUMN. Existing refs read
-- repo_id NULL (url-only, unchanged) and is_primary 0. No row is rewritten.
--
-- DIALECT NOTE: CREATE TABLE, BLOB, ADD COLUMN, and DROP COLUMN (down) are in the
-- SQLite (>=3.35) + PG common subset.

CREATE TABLE code_repos (
    id                    TEXT PRIMARY KEY,            -- ULID
    organization_id       TEXT NOT NULL,              -- workspace/org scope
    label                 TEXT NOT NULL,
    description           TEXT,
    url                   TEXT NOT NULL,
    provider              TEXT NOT NULL,              -- github | gitlab | git
    default_branch        TEXT,                       -- "" / NULL = unset
    credential_ciphertext BLOB,                       -- AES-GCM ciphertext; NULL = no credential
    credential_nonce      BLOB,                       -- AES-GCM nonce (companion to ciphertext)
    created_by            TEXT NOT NULL,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    version               INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_code_repos_org ON code_repos (organization_id);

ALTER TABLE pm_code_repo_refs ADD COLUMN repo_id TEXT;
ALTER TABLE pm_code_repo_refs ADD COLUMN is_primary INTEGER NOT NULL DEFAULT 0;
