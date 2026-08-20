DELETE FROM authorization_audit_events
WHERE id IN (
    'audit-background-worker-auto-assign-reconciler',
    'audit-background-worker-lease-checker',
    'audit-background-worker-overdue-block-reminder',
    'audit-background-worker-plan-reconcile',
    'audit-background-worker-resolved-issue-closer'
);

DELETE FROM authorization_role_assignments
WHERE id IN (
    'asgn-background-worker-auto-assign-reconciler',
    'asgn-background-worker-lease-checker',
    'asgn-background-worker-overdue-block-reminder',
    'asgn-background-worker-plan-reconcile',
    'asgn-background-worker-resolved-issue-closer'
);

DELETE FROM authorization_role_permissions
WHERE role_id = 'sys-background-worker';

DELETE FROM authorization_roles
WHERE id = 'sys-background-worker';
