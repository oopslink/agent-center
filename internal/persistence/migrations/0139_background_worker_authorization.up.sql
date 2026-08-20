INSERT OR IGNORE INTO authorization_roles
    (id, org_id, kind, name, description, created_by, created_at, updated_at)
VALUES
    ('sys-background-worker', '', 'system', 'background.worker', 'Fixed production background worker operation grants', 'system', datetime('now'), datetime('now'));

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
VALUES
    ('sys-background-worker', 'worker.capability.report', 'worker', 0, datetime('now'));

INSERT OR IGNORE INTO authorization_role_assignments
    (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
VALUES
    ('asgn-background-worker-auto-assign-reconciler', 'system', 'agent:background', 'sys-background-worker', 'worker', 'background:auto_assign_reconciler', 'system', datetime('now'), 1),
    ('asgn-background-worker-lease-checker', 'system', 'agent:background', 'sys-background-worker', 'worker', 'background:lease_checker', 'system', datetime('now'), 1),
    ('asgn-background-worker-overdue-block-reminder', 'system', 'agent:background', 'sys-background-worker', 'worker', 'background:overdue_block_reminder', 'system', datetime('now'), 1),
    ('asgn-background-worker-plan-reconcile', 'system', 'agent:background', 'sys-background-worker', 'worker', 'background:plan_reconcile', 'system', datetime('now'), 1),
    ('asgn-background-worker-resolved-issue-closer', 'system', 'agent:background', 'sys-background-worker', 'worker', 'background:resolved_issue_closer', 'system', datetime('now'), 1);

INSERT OR IGNORE INTO authorization_audit_events
    (id, event_type, actor_ref, subject_ref, permission_key, resource_kind, resource_id, role_id, assignment_id, request_id, payload_json, created_at)
VALUES
    ('audit-background-worker-auto-assign-reconciler', 'authorization.assignment.created', 'system', 'agent:background', 'worker.capability.report', 'worker', 'background:auto_assign_reconciler', 'sys-background-worker', 'asgn-background-worker-auto-assign-reconciler', 'migration:0139', '{"source":"migration:0139_background_worker_authorization"}', datetime('now')),
    ('audit-background-worker-lease-checker', 'authorization.assignment.created', 'system', 'agent:background', 'worker.capability.report', 'worker', 'background:lease_checker', 'sys-background-worker', 'asgn-background-worker-lease-checker', 'migration:0139', '{"source":"migration:0139_background_worker_authorization"}', datetime('now')),
    ('audit-background-worker-overdue-block-reminder', 'authorization.assignment.created', 'system', 'agent:background', 'worker.capability.report', 'worker', 'background:overdue_block_reminder', 'sys-background-worker', 'asgn-background-worker-overdue-block-reminder', 'migration:0139', '{"source":"migration:0139_background_worker_authorization"}', datetime('now')),
    ('audit-background-worker-plan-reconcile', 'authorization.assignment.created', 'system', 'agent:background', 'worker.capability.report', 'worker', 'background:plan_reconcile', 'sys-background-worker', 'asgn-background-worker-plan-reconcile', 'migration:0139', '{"source":"migration:0139_background_worker_authorization"}', datetime('now')),
    ('audit-background-worker-resolved-issue-closer', 'authorization.assignment.created', 'system', 'agent:background', 'worker.capability.report', 'worker', 'background:resolved_issue_closer', 'sys-background-worker', 'asgn-background-worker-resolved-issue-closer', 'migration:0139', '{"source":"migration:0139_background_worker_authorization"}', datetime('now'));
