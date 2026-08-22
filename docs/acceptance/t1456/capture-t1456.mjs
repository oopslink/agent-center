import { chromium } from '../../../tests/e2e/v2/node_modules/@playwright/test/index.mjs';
import { execFile } from 'node:child_process';
import fs from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';

const outDir = new URL('.', import.meta.url).pathname;
const baseURL = process.env.T1456_BASE_URL ?? 'http://127.0.0.1:5177';
const run = promisify(execFile);

const catalog = {
  generated_at: '2026-08-22T08:00:00Z',
  subjects: [],
  roles: [],
  decisions: [],
  grants: [],
  summary: { allowed: 0, high_risk: 0, expiring: 0, denied: 0, not_applicable: 0 },
  catalog: [
    { key: 'team.read', label: 'Read team', description: 'Read team metadata', resource_kinds: ['team'], actions: ['read'], risk: 'low', category: 'access', legacy_sources: ['team_role_ram'] },
    { key: 'team.write', label: 'Write team', description: 'Update team state', resource_kinds: ['team'], actions: ['write'], risk: 'medium', category: 'access', legacy_sources: ['team_role_ram'] },
    { key: 'team.memory.read', label: 'Read memory', description: 'Read team memory', resource_kinds: ['team'], actions: ['read'], risk: 'low', category: 'access', legacy_sources: ['team_role_ram'] },
    { key: 'team.memory.review', label: 'Review memory', description: 'Review team memory proposals', resource_kinds: ['team'], actions: ['review'], risk: 'high', category: 'access', legacy_sources: ['team_role_ram'] },
    { key: 'project.write', label: 'Write project', description: 'Change project resources', resource_kinds: ['project'], actions: ['write'], risk: 'high', category: 'access', legacy_sources: ['team_role_ram'] },
  ],
};

const roles = [
  { id: 'team-basic', stable_key: 'Team basic', name: 'Team basic', kind: 'system', version: 1, description: 'Read team metadata and memory.', permissions: ['team.read', 'team.memory.read'], risk: 'low', scope: 'team' },
  { id: 'team-curator', stable_key: 'Team curator', name: 'Team curator', kind: 'system', version: 2, description: 'Review team memory.', permissions: ['team.read', 'team.write', 'team.memory.read', 'team.memory.review'], risk: 'high', scope: 'team' },
  { id: 'role-old', stable_key: 'old.deployer', name: 'Old deployer', kind: 'custom', version: 3, description: 'Legacy deploy access.', permissions: ['project.write'], risk: 'high', scope: 'project' },
  { id: 'role-target', stable_key: 'new.deployer', name: 'New deployer', kind: 'custom', version: 1, description: 'Replacement deploy access.', permissions: ['project.write', 'team.read'], risk: 'high', scope: 'project' },
  { id: 'role-unused', stable_key: 'unused.reviewer', name: 'Unused reviewer', kind: 'custom', version: 2, description: 'Cleanup candidate.', permissions: ['team.read'], risk: 'low', scope: 'team' },
  { id: 'role-release', stable_key: 'release.operator', name: 'Release operator', kind: 'custom', version: 1, description: 'Temporary release work.', permissions: ['team.read', 'project.write'], risk: 'high', scope: 'project' },
  { id: 'role-audit', stable_key: 'audit.reader', name: 'Audit reader', kind: 'custom', version: 1, description: 'Read audit evidence.', permissions: ['team.read', 'team.memory.read'], risk: 'low', scope: 'team' },
  { id: 'role-support', stable_key: 'support.helper', name: 'Support helper', kind: 'custom', version: 1, description: 'Assist operational support.', permissions: ['team.read', 'team.write'], risk: 'medium', scope: 'team' },
  { id: 'role-plan', stable_key: 'plan.operator', name: 'Plan operator', kind: 'custom', version: 1, description: 'Plan execution support.', permissions: ['project.write'], risk: 'high', scope: 'project' },
];

const teams = [
  {
    id: 'team-7c19b0',
    name: 'agent-center core',
    glyph: 'AC',
    visibility: 'org-private',
    roles: [
      { role: 'planner', cli: 'codex', model: 'gpt-5.1-codex', max_concurrency: 1, count: 2, capability_tags: ['planning'] },
      { role: 'reviewer', cli: 'codex', model: 'gpt-5.1-codex', max_concurrency: 1, count: 1, capability_tags: ['review'] },
    ],
  },
];

let mappings = {
  'team-7c19b0/planner': { team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: ['role-old', 'team-basic'], version: 9 },
  'team-7c19b0/reviewer': { team_id: 'team-7c19b0', team_role: 'reviewer', ram_role_ids: ['team-curator'], version: 4 },
};

function detail(id) {
  const role = roles.find((item) => item.id === id) ?? roles[0];
  const v1 = { ...role, version: Math.max(1, role.version - 1), permissions: role.permissions.slice(0, 1), risk: role.risk === 'high' ? 'medium' : role.risk };
  return {
    ...role,
    latest: role,
    versions: role.version > 1 ? [role, v1] : [role],
    references: [],
  };
}

async function json(route, body, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
}

async function installRoutes(page) {
  await page.route('**/*', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/')) return json(route, {});
    return route.fallback();
  });
  await page.route('**/api/sse**', (route) => route.abort());
  await page.route('**/api/auth/me', (route) => json(route, { identity_id: 'user:owner', display_name: 'Owner', kind: 'user' }));
  await page.route('**/api/orgs', (route) => json(route, [{ id: 'org-test', slug: 'test', name: 'Test Org', created_at: '2026-08-22T00:00:00Z', role: 'owner' }]));
  await page.route('**/api/orgs/test/access/overview**', (route) => json(route, catalog));
  await page.route('**/api/orgs/test/permissions/effective**', (route) => json(route, {
    subject_ref: 'user:owner',
    resource: { kind: 'org', id: 'org-test', org_id: 'org-test' },
    permissions: [{ key: 'org.member.role.manage', source: 'org_role', evidence_ref: 'members:owner' }],
  }));
  await page.route('**/api/orgs/test/projects**', (route) => json(route, []));
  await page.route('**/api/orgs/test/channels**', (route) => json(route, []));
  await page.route('**/api/orgs/test/dms**', (route) => json(route, []));
  await page.route('**/api/orgs/test/members**', (route) => json(route, []));
  await page.route('**/api/orgs/test/agents**', (route) => json(route, { agents: [] }));
  await page.route('**/api/orgs/test/unread**', (route) => json(route, []));
  await page.route('**/api/orgs/test/conversations**', (route) => json(route, []));
  await page.route('**/api/orgs/test/access/ram-roles', async (route, request) => {
    if (request.method() === 'POST') {
      const body = request.postDataJSON();
      const created = { id: 'role-created-capture', stable_key: body.stable_key, name: body.name, kind: 'custom', version: 1, description: body.description, permissions: body.permissions, risk: 'high', scope: body.scope };
      roles.unshift(created);
      return json(route, { ...created, latest: created, versions: [created], references: [] }, 201);
    }
    return json(route, { roles });
  });
  await page.route('**/api/orgs/test/access/ram-roles/*/versions', async (route) => {
    const id = decodeURIComponent(route.request().url().split('/ram-roles/')[1].split('/versions')[0]);
    const body = route.request().postDataJSON();
    const i = roles.findIndex((role) => role.id === id);
    roles[i] = { ...roles[i], ...body, version: (roles[i]?.version ?? 1) + 1, risk: body.permissions.includes('project.write') ? 'high' : 'medium' };
    return json(route, detail(id), 201);
  });
  await page.route('**/api/orgs/test/access/ram-roles/*', async (route) => {
    const url = route.request().url();
    const id = decodeURIComponent(url.split('/ram-roles/')[1]);
    if (route.request().method() === 'DELETE') return route.fulfill({ status: 204 });
    if (route.request().method() === 'PATCH') {
      const body = route.request().postDataJSON();
      const i = roles.findIndex((role) => role.id === id);
      roles[i] = { ...roles[i], ...body };
      return json(route, detail(id));
    }
    return json(route, detail(id));
  });
  await page.route('**/api/orgs/test/teams', (route) => json(route, teams));
  await page.route('**/api/orgs/test/teams/*/members', (route) => json(route, []));
  await page.route('**/api/orgs/test/teams/*/roles/*/ram-roles', async (route) => {
    const match = route.request().url().match(/teams\/([^/]+)\/roles\/([^/]+)\/ram-roles/);
    const key = `${match?.[1]}/${decodeURIComponent(match?.[2] ?? '')}`;
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON();
      mappings[key] = { team_id: match?.[1], team_role: decodeURIComponent(match?.[2] ?? ''), ram_role_ids: body.ram_role_ids, version: (mappings[key]?.version ?? 1) + 1 };
    }
    return json(route, mappings[key] ?? { team_id: match?.[1], team_role: match?.[2], ram_role_ids: [], version: 1 });
  });
}

async function shot(page, name) {
  await page.screenshot({ path: path.join(outDir, `${name}-1672x941.png`), fullPage: false });
  for (const kind of ['overlay', 'diff']) {
    await run('convert', [
      '-size', '1672x941',
      'xc:white',
      '-gravity', 'center',
      '-fill', '#111827',
      '-pointsize', '34',
      '-annotate', '0',
      `${kind} unavailable: canonical ac:// mockup is not accessible in this isolated executor`,
      path.join(outDir, `${name}-${kind}-unavailable.png`),
    ]);
  }
}

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1672, height: 941 }, deviceScaleFactor: 1 });
page.on('console', (msg) => console.log(`[browser:${msg.type()}] ${msg.text()}`));
page.on('requestfailed', (request) => console.log(`[requestfailed] ${request.method()} ${request.url()} ${request.failure()?.errorText}`));
await installRoutes(page);
await page.goto(`${baseURL}/organizations/test/access/ram-roles`);
try {
  await page.waitForSelector('[data-testid="page-RAMRoles"]', { timeout: 30_000 });
} catch (error) {
  console.log(`[startup-timeout] url=${page.url()}`);
  console.log((await page.locator('body').innerText().catch(() => '')).slice(0, 2000));
  throw error;
}
await shot(page, '01-list-stats');

await page.getByTestId('ram-role-search').fill('curator');
await shot(page, '02-search-filter');
await page.getByTestId('ram-role-search').fill('');
await page.getByTestId('ram-role-filter-rows').selectOption('4');
await page.getByTestId('ram-role-page-next').click();
await shot(page, '03-pagination');

await page.getByTestId('ram-role-search').fill('Old deployer');
await page.getByTestId('ram-role-row-role-old').click();
await page.waitForSelector('[data-testid="ram-role-reference-block"]');
await shot(page, '04-detail-permission-summary-team-references');
await page.getByTestId('ram-role-delete-open').click();
await shot(page, '05-delete-confirm-referenced-blocking');
await page.getByTestId('confirm-modal-cancel').click();
await page.getByTestId('ram-role-migrate-target').selectOption('role-target');
await page.getByTestId('ram-role-migrate-submit').click();
await page.waitForSelector('[data-testid="ram-role-toast"]');
await shot(page, '06-migration-toast');
await page.getByLabel('Dismiss notification').click();

await page.getByTestId('ram-role-create-open').click();
await page.getByTestId('ram-role-form-name').fill('Capture operator');
await page.getByTestId('ram-role-form-stable-key').fill('capture.operator');
await page.getByTestId('ram-role-form-description').fill('Capture-state role');
await page.getByTestId('ram-role-form-scope').fill('project');
await page.getByTestId('ram-role-drawer').getByRole('button', { name: /project\.write/ }).click();
await shot(page, '07-create-full-fields');
await page.getByTestId('ram-role-form-save').click();
await page.waitForSelector('[data-testid="ram-role-toast"]');
await shot(page, '08-create-toast');

await page.getByTestId('ram-role-edit-open').click();
await page.getByTestId('ram-role-form-description').fill('Capture-state role edited');
await page.getByTestId('ram-role-drawer').getByRole('button', { name: /team\.write/ }).click();
await shot(page, '09-edit-full-fields');

await browser.close();

await fs.writeFile(path.join(outDir, 'capture-state.json'), JSON.stringify({
  viewport: '1672x941',
  baseURL,
  mockup_attachment: 'ac://files/01M0HRMZDX20FF5KQT4SBANGC1',
  mockup_sha256: '5e085034e927054a59c103aeac30b6217c6a8a1c5f44f20ad9212589381cf43e',
  mockup_available_in_workspace: false,
  note: 'Overlay/diff PNGs are marked unavailable because the canonical ac:// attachment is not accessible in this isolated executor; screenshots are fresh captures of the implemented page, not self-baselines.',
}, null, 2));
