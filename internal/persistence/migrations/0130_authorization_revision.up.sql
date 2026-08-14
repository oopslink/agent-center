-- 0130_authorization_revision.up.sql -- cache invalidation clock for unified authorization.

CREATE TABLE IF NOT EXISTS authorization_revision (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    revision   INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO authorization_revision (id, revision, updated_at)
VALUES (1, 0, datetime('now'));

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_permission_definitions_ai AFTER INSERT ON permission_definitions BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_permission_definitions_au AFTER UPDATE ON permission_definitions BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_permission_definitions_ad AFTER DELETE ON permission_definitions BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_roles_ai AFTER INSERT ON authorization_roles BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_roles_au AFTER UPDATE ON authorization_roles BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_roles_ad AFTER DELETE ON authorization_roles BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_role_permissions_ai AFTER INSERT ON authorization_role_permissions BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_role_permissions_au AFTER UPDATE ON authorization_role_permissions BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_role_permissions_ad AFTER DELETE ON authorization_role_permissions BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_role_assignments_ai AFTER INSERT ON authorization_role_assignments BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_role_assignments_au AFTER UPDATE ON authorization_role_assignments BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_authorization_role_assignments_ad AFTER DELETE ON authorization_role_assignments BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_admin_tokens_ai AFTER INSERT ON admin_tokens BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_admin_tokens_au AFTER UPDATE ON admin_tokens BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_admin_tokens_ad AFTER DELETE ON admin_tokens BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_members_ai AFTER INSERT ON members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_members_au AFTER UPDATE ON members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_members_ad AFTER DELETE ON members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_organizations_ai AFTER INSERT ON organizations BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_organizations_au AFTER UPDATE ON organizations BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_organizations_ad AFTER DELETE ON organizations BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_projects_ai AFTER INSERT ON pm_projects BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_projects_au AFTER UPDATE ON pm_projects BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_projects_ad AFTER DELETE ON pm_projects BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_project_members_ai AFTER INSERT ON pm_project_members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_project_members_au AFTER UPDATE ON pm_project_members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_project_members_ad AFTER DELETE ON pm_project_members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_tasks_ai AFTER INSERT ON pm_tasks BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_tasks_au AFTER UPDATE ON pm_tasks BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_tasks_ad AFTER DELETE ON pm_tasks BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_issues_ai AFTER INSERT ON pm_issues BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_issues_au AFTER UPDATE ON pm_issues BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_issues_ad AFTER DELETE ON pm_issues BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_plans_ai AFTER INSERT ON pm_plans BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_plans_au AFTER UPDATE ON pm_plans BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_pm_plans_ad AFTER DELETE ON pm_plans BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_teams_ai AFTER INSERT ON teams BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_teams_au AFTER UPDATE ON teams BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_teams_ad AFTER DELETE ON teams BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_team_members_ai AFTER INSERT ON team_members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_team_members_au AFTER UPDATE ON team_members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_team_members_ad AFTER DELETE ON team_members BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_team_memory_policy_curators_ai AFTER INSERT ON team_memory_policy_curators BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_team_memory_policy_curators_au AFTER UPDATE ON team_memory_policy_curators BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_team_memory_policy_curators_ad AFTER DELETE ON team_memory_policy_curators BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_conversations_ai AFTER INSERT ON conversations BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_conversations_au AFTER UPDATE ON conversations BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_conversations_ad AFTER DELETE ON conversations BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_file_references_ai AFTER INSERT ON file_references BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_file_references_au AFTER UPDATE ON file_references BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_file_references_ad AFTER DELETE ON file_references BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_agents_ai AFTER INSERT ON agents BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_agents_au AFTER UPDATE ON agents BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS trg_authorization_revision_agents_ad AFTER DELETE ON agents BEGIN
    UPDATE authorization_revision SET revision = revision + 1, updated_at = datetime('now') WHERE id = 1;
END;
