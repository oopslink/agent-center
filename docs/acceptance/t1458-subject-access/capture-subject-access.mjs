import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';

const outDir = new URL('.', import.meta.url).pathname;
const baseURL = process.env.AC_WEB_URL || 'http://localhost:5173';
const require = createRequire(new URL('../../../tests/e2e/v2/package.json', import.meta.url));
const { chromium } = require('@playwright/test');

const subjects = [
  { ref: 'user:hayang', kind: 'human', name: 'Hayang', role: 'owner', status: 'joined', team_names: ['agent-center core'] },
  { ref: 'agent:builder', kind: 'agent', name: 'Builder', role: 'agent', status: 'joined', team_names: ['agent-center core'] },
  { ref: 'agent:external', kind: 'agent', name: 'External Bot', role: 'agent', status: 'unavailable', team_names: [] },
  { ref: 'user:ops', kind: 'human', name: 'Ops Lead', role: 'admin', status: 'joined', team_names: [] },
];

const catalog = [
  { key: 'org.read', label: 'Read organization', description: 'Open organization resources.', resource_kinds: ['org'], actions: ['read'], risk: 'low', category: 'access', legacy_sources: ['org_role'] },
  { key: 'org.member.role.manage', label: 'Manage org roles', description: 'Change organization role assignments.', resource_kinds: ['org'], actions: ['manage'], risk: 'high', high_risk: true, category: 'access', legacy_sources: ['org_role'] },
  { key: 'project.write', label: 'Write project', description: 'Create and update project work.', resource_kinds: ['project'], actions: ['write'], risk: 'medium', category: 'access', legacy_sources: ['project_member', 'custom_role'] },
  { key: 'team.memory.review', label: 'Review team memory', description: 'Promote or reject team memory.', resource_kinds: ['team'], actions: ['review'], risk: 'high', high_risk: true, category: 'access', legacy_sources: ['team_role_ram', 'team_memory_policy'] },
  { key: 'file.download', label: 'Download files', description: 'Download reachable files.', resource_kinds: ['file', 'task', 'issue', 'plan', 'conversation'], actions: ['download'], risk: 'medium', category: 'access', legacy_sources: ['file_scope'] },
];

const resources = {
  org: { kind: 'org', id: 'org-test', org_id: 'org-test', label: 'Test Org' },
  project: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' },
  team: { kind: 'team', id: 'team-7c19b0', org_id: 'org-test', label: 'agent-center core' },
};

const decisions = [
  allowed('user:hayang', 'org.read', resources.org, 'org_role', 'owner role derives org.read', 'members:mem-1', 'low'),
  allowed('user:hayang', 'org.member.role.manage', resources.org, 'org_role', 'owner role derives org.member.role.manage', 'members:mem-1', 'high'),
  allowed('agent:builder', 'project.write', resources.project, 'project_member', 'project membership derives project.write', 'pm_project_members:pmem-1', 'medium'),
  allowed('agent:builder', 'project.write', resources.project, 'custom_role', 'matched unified authorization service', 'authorization_role_assignments:grant-custom-1', 'medium', 'grant-custom-1', 'role-access-project-write', '2026-09-21T12:30:00Z'),
  allowed('agent:builder', 'team.memory.review', resources.team, 'team_role_ram', 'matched team_role_ram', 'team_role_ram_role_mappings:team-7c19b0/reviewer/team-curator', 'high', 'grant-team-ram', 'team-curator'),
  denied('agent:builder', 'team.memory.review', resources.team, 'team_memory_policy', 'not a curator for this team policy', 'team_memory_policy:team-7c19b0', 'denied', 'high'),
  denied('agent:external', 'project.write', resources.project, 'project_member', 'subject is not a joined organization member', 'members:missing', 'unauthorized', 'medium'),
  denied('user:ops', 'file.download', resources.team, 'file_scope', 'file.download does not apply to team resources', 'permission_registry:file.download', 'not_applicable', 'medium'),
];

const grants = [
  { id: 'grant-custom-1', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'project.write', resource: resources.project, source: 'custom_role', status: 'expires_soon', expires_at: '2026-09-21T12:30:00Z', created_by: 'user:hayang', created_at: '2026-08-14T08:00:00Z', role_id: 'role-access-project-write', risk: 'medium' },
  { id: 'grant-team-ram', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'team.memory.review', resource: resources.team, source: 'team_role_ram', status: 'active', created_by: 'system', created_at: '2026-08-14T08:00:00Z', role_id: 'team-curator', risk: 'high' },
  { id: 'grant-owner', subject_ref: 'user:hayang', subject_name: 'Hayang', permission: 'org.member.role.manage', resource: resources.org, source: 'org_role', status: 'active', created_by: 'system', created_at: '2026-08-14T08:00:00Z', risk: 'high' },
];

function allowed(subject_ref, permission, resource, source, reason, evidence_ref, risk, grant_id, role_id, expires_at) {
  return { allowed: true, subject_ref, permission, resource, source, reason, evidence_ref, status: 'allowed', risk, grant_id, role_id, expires_at };
}

function denied(subject_ref, permission, resource, source, reason, evidence_ref, status, risk) {
  return { allowed: false, subject_ref, permission, resource, source, reason, evidence_ref, status, risk };
}

function overviewFor(url) {
  const u = new URL(url);
  const q = (u.searchParams.get('q') || '').toLowerCase();
  const subjectKind = u.searchParams.get('subject_kind');
  const status = u.searchParams.get('status');
  const filtered = decisions.filter((d) => {
    const subject = subjects.find((s) => s.ref === d.subject_ref);
    const haystack = `${subject?.name || ''} ${d.subject_ref} ${d.permission} ${d.reason} ${d.resource.label || ''}`.toLowerCase();
    if (q && !haystack.includes(q)) return false;
    if (subjectKind && subjectKind !== 'all' && subject?.kind !== subjectKind) return false;
    if (status && status !== 'all' && d.status !== status) return false;
    return true;
  });
  return {
    generated_at: '2026-08-22T00:00:00Z',
    subjects,
    roles: [],
    catalog,
    decisions: filtered,
    grants,
    summary: {
      allowed: filtered.filter((d) => d.status === 'allowed').length,
      high_risk: filtered.filter((d) => d.risk === 'high').length,
      expiring: grants.filter((g) => g.status === 'expires_soon').length,
      denied: filtered.filter((d) => d.status === 'denied' || d.status === 'unauthorized').length,
      not_applicable: filtered.filter((d) => d.status === 'not_applicable').length,
    },
  };
}

async function routeJSON(page) {
  await page.route('**/*', (route) => {
    const pathname = new URL(route.request().url()).pathname;
    if (pathname.startsWith('/api/')) return route.fulfill({ json: [] });
    return route.continue();
  });
  await page.route('**/api/orgs', (route) => route.fulfill({ json: [{ id: 'org-test', slug: 'test', name: 'Test Org', role: 'owner' }] }));
  await page.route('**/api/auth/me', (route) => route.fulfill({ json: { identity_id: 'hayang', display_name: 'Hayang', kind: 'user' } }));
  await page.route('**/api/orgs/test/permissions/effective**', (route) => {
    const forbidden = route.request().url().includes('scenario=403');
    route.fulfill({ json: { subject_ref: 'user:hayang', resource: resources.org, permissions: forbidden ? [{ key: 'org.read', source: 'org_role', evidence_ref: 'members:mem-1' }] : [{ key: 'org.member.role.manage', source: 'org_role', evidence_ref: 'members:mem-1' }] } });
  });
  await page.route('**/api/permissions/effective**', (route) => {
    route.fulfill({ json: { subject_ref: 'user:hayang', resource: resources.org, permissions: [{ key: 'org.member.role.manage', source: 'org_role', evidence_ref: 'members:mem-1' }] } });
  });
  await page.route('**/api/orgs/test/permissions/explain', (route) => route.fulfill({ json: { decision: denied('user:hayang', 'org.member.role.manage', resources.org, 'org_role', 'current subject lacks org.member.role.manage', 'members:mem-1', 'unauthorized', 'high') } }));
  await page.route('**/api/orgs/test/access/overview**', (route) => route.fulfill({ json: overviewFor(route.request().url()) }));
  await page.route('**/api/access/overview**', (route) => route.fulfill({ json: overviewFor(route.request().url()) }));
  await page.route('**/api/orgs/test/access/ram-roles**', (route) => route.fulfill({ json: { roles: [{ id: 'team-curator', stable_key: 'team-curator', name: 'Team curator', version: 2, kind: 'system', description: 'Review team memory.', permissions: ['team.memory.review'], risk: 'high', scope: 'team' }] } }));
  await page.route('**/api/orgs/test/access/batch/preview', (route) => route.fulfill({ json: { request_id: 'preview-1', items: [{ id: 'item-1', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'project.write', resource: resources.project, status: 'allowed', risk: 'medium', high_risk: false, reason: 'grant can be applied', grant_id: 'grant-new' }], summary: { total: 1, grantable: 1, high_risk: 0, unauthorized: 0, not_applicable: 0 } } }));
  await page.route('**/api/orgs/test/access/batch/apply', (route) => {
    if (route.request().postData()?.includes('conflict')) {
      return route.fulfill({ status: 409, json: { error: 'conflict', message: 'stale preview conflict' } });
    }
    return route.fulfill({ json: { operation_id: 'op-1', applied_at: '2026-08-22T00:00:00Z', items: [{ id: 'item-1', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'project.write', resource: resources.project, status: 'allowed', risk: 'medium', high_risk: false, reason: 'grant applied', grant_id: 'grant-new' }], summary: { total: 1, succeeded: 1, failed: 0, unauthorized: 0, not_applicable: 0, partial_failure: false } } });
  });
  await page.route('**/api/orgs/test/access/grants/revoke/preview', (route) => route.fulfill({ json: { preview_id: 'revoke-preview-1', token: 'revoke-token-1', expires_at: '2026-08-22T00:05:00Z', items: [{ id: 'revoke-1', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'project.write', resource: resources.project, status: 'allowed', risk: 'medium', high_risk: false, reason: 'grant can be revoked', grant_id: 'grant-custom-1' }], summary: { total: 1, grantable: 1, high_risk: 0, unauthorized: 0, not_applicable: 0 } } }));
  await page.route('**/api/orgs/test/access/grants/revoke/confirm', (route) => route.fulfill({ json: { operation_id: 'revoke-op-1', applied_at: '2026-08-22T00:01:00Z', items: [{ id: 'revoke-1', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'project.write', resource: resources.project, status: 'allowed', risk: 'medium', high_risk: false, reason: 'grant revoked', grant_id: 'grant-custom-1' }], summary: { total: 1, succeeded: 1, failed: 0, unauthorized: 0, not_applicable: 0, partial_failure: false } } }));
  await page.route('**/api/orgs/test/permissions/audit**', (route) => route.fulfill({ json: { events: [{ id: 'audit-1', event_type: 'authorization.assignment.created', actor_ref: 'user:hayang', subject_ref: 'agent:builder', assignment_id: 'grant-custom-1', created_at: '2026-08-14T08:00:00Z' }] } }));
  await page.route('**/api/orgs/test/teams', (route) => route.fulfill({ json: [{ id: 'team-7c19b0', name: 'agent-center core', roles: [{ role: 'reviewer' }] }] }));
  await page.route('**/api/orgs/test/teams/team-7c19b0/members', (route) => route.fulfill({ json: [{ member_ref: 'agent:builder', role: 'reviewer', roles: ['reviewer'] }] }));
  await page.route('**/api/orgs/test/teams/team-7c19b0/roles/reviewer/ram-roles', (route) => route.fulfill({ json: { team_id: 'team-7c19b0', team_role: 'reviewer', ram_role_ids: ['team-curator'], version: 1 } }));
  await page.route('**/api/orgs/test/projects**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/orgs/test/members**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/orgs/test/conversations**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/orgs/test/channels**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/orgs/test/dms**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/orgs/test/unread-conversations**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/orgs/test/attention**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/orgs/test/reminders**', (route) => route.fulfill({ json: [] }));
}

async function shot(page, name) {
  await page.screenshot({ path: path.join(outDir, `${name}.png`), fullPage: true });
}

async function main() {
  await fs.mkdir(outDir, { recursive: true });
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1672, height: 941 }, deviceScaleFactor: 1 });
  page.on('requestfailed', (request) => console.log(`requestfailed: ${request.url()} ${request.failure()?.errorText || ''}`));
  await routeJSON(page);

  await page.goto(`${baseURL}/organizations/test/access/subject-access`);
  try {
    await page.getByTestId('access-subject-view').waitFor({ timeout: 10_000 });
  } catch (error) {
    await page.screenshot({ path: path.join(outDir, 'debug-access-timeout.png'), fullPage: true });
    console.log(await page.locator('body').innerText().catch(() => 'body unavailable'));
    throw error;
  }
  await shot(page, 's5-01-ready-1672-light');
  await page.evaluate(() => document.documentElement.classList.add('dark'));
  await shot(page, 's5-02-ready-1672-dark');
  await page.setViewportSize({ width: 1280, height: 900 });
  await shot(page, 's5-03-ready-1280-dark');
  await page.evaluate(() => document.documentElement.classList.remove('dark'));
  await shot(page, 's5-04-ready-1280-light');
  await page.setViewportSize({ width: 1672, height: 941 });

  await page.getByTestId('access-filter-type').selectOption('agent');
  await page.getByTestId('access-subject-row-agent:builder').click();
  await shot(page, 's5-05-filter-agent-detail');

  await page.getByTestId('access-open-direct-binding').click();
  await page.getByTestId('access-batch-drawer').waitFor();
  await shot(page, 's5-06-add-binding-context');

  const drawer = page.getByTestId('access-batch-drawer');
  await drawer.getByRole('button', { name: /project\.write/ }).click();
  await drawer.getByRole('button', { name: /Project Alpha/ }).click();
  await drawer.getByTestId('access-batch-reason').fill('temporary direct binding');
  await drawer.getByTestId('access-run-preview').click();
  await drawer.getByTestId('access-preview-summary').waitFor();
  await shot(page, 's5-07-grant-preview');

  await drawer.getByTestId('access-preview-continue').click();
  await drawer.getByTestId('access-apply-batch').click();
  await page.getByTestId('access-result').waitFor();
  await page.getByTestId('access-toast').waitFor();
  await drawer.getByRole('button', { name: 'Done' }).click();
  await shot(page, 's5-08-grant-success-toast');

  await page.getByTestId('access-grant-select').first().check();
  await page.getByTestId('access-revoke-preview').click();
  await page.getByTestId('access-revoke-preview-panel').waitFor();
  await shot(page, 's5-09-revoke-preview');
  await page.getByTestId('access-revoke-confirm').click();
  await page.getByText('Revoke result').waitFor();
  await shot(page, 's5-10-revoke-success-toast');
  await page.getByLabel('Dismiss notification').click().catch(() => {});

  const forbidden = await browser.newPage({ viewport: { width: 1672, height: 941 }, deviceScaleFactor: 1 });
  await routeJSON(forbidden);
  await forbidden.route('**/api/orgs/test/permissions/effective**', (route) => route.fulfill({ json: { subject_ref: 'user:ops', resource: resources.org, permissions: [{ key: 'org.read', source: 'org_role', evidence_ref: 'members:mem-ops' }] } }));
  await forbidden.goto(`${baseURL}/organizations/test/access/subject-access`);
  await forbidden.getByTestId('access-subject-access-forbidden').waitFor();
  await shot(forbidden, 's5-11-forbidden-403');
  await forbidden.close();

  await page.getByTestId('access-open-direct-binding').click();
  const conflictDrawer = page.getByTestId('access-batch-drawer');
  await conflictDrawer.getByRole('button', { name: /project\.write/ }).click();
  await conflictDrawer.getByRole('button', { name: /Project Alpha/ }).click();
  await conflictDrawer.getByTestId('access-batch-reason').fill('conflict direct binding');
  await conflictDrawer.getByTestId('access-run-preview').click();
  await conflictDrawer.getByTestId('access-preview-summary').waitFor();
  await conflictDrawer.getByTestId('access-preview-continue').click();
  await conflictDrawer.getByTestId('access-apply-batch').click();
  await page.getByTestId('access-toast').waitFor();
  await conflictDrawer.getByLabel('Close').click();
  await shot(page, 's5-12-conflict-409-toast');
  await page.getByLabel('Dismiss notification').click();

  await page.getByTestId('access-filter-type').selectOption('all');
  await page.getByTestId('access-filter-status').selectOption('not_applicable');
  await page.getByTestId('access-explicit-deny').waitFor();
  await shot(page, 's5-13-not-applicable');

  const empty = await browser.newPage({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 1 });
  await routeJSON(empty);
  await empty.route('**/api/orgs/test/access/overview**', (route) => route.fulfill({ json: { generated_at: '2026-08-24T00:00:00Z', subjects: [], roles: [], catalog: [], decisions: [], grants: [], summary: { allowed: 0, high_risk: 0, expiring: 0, denied: 0, not_applicable: 0 } } }));
  await empty.goto(`${baseURL}/organizations/test/access/subject-access`);
  await empty.getByTestId('access-empty').waitFor();
  await shot(empty, 's5-14-empty-1280');
  await empty.close();

  const loading = await browser.newPage({ viewport: { width: 1672, height: 941 }, deviceScaleFactor: 1 });
  await routeJSON(loading);
  await loading.route('**/api/orgs/test/access/overview**', () => new Promise(() => {}));
  await loading.goto(`${baseURL}/organizations/test/access/subject-access`);
  await loading.getByTestId('access-subject-access-loading').waitFor();
  await shot(loading, 's5-15-loading-1672');
  await loading.close();

  await browser.close();
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
