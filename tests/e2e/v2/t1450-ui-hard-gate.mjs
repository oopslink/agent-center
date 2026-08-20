import { chromium } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const base = process.env.AC_BASE;
const slug = process.env.AC_SLUG;
const email = process.env.AC_EMAIL;
const passcode = process.env.AC_PASS;
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../../..');
const out = process.env.AC_OUT || join(repoRoot, 'docs/acceptance/t1450-ui-hard-gate');
if (!base || !slug || !email || !passcode) {
  throw new Error('AC_BASE, AC_SLUG, AC_EMAIL and AC_PASS are required');
}

const shots = join(out, 'screenshots');
const logs = join(out, 'logs');
mkdirSync(shots, { recursive: true });
mkdirSync(logs, { recursive: true });

const events = [];
const apiEvents = [];
const consoleEvents = [];
const runId = Date.now().toString(36);
const step = (name, detail = {}) => {
  const row = { at: new Date().toISOString(), name, ...detail };
  events.push(row);
  console.log('STEP', JSON.stringify(row));
};

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({
  viewport: { width: 1440, height: 950 },
  recordHar: { path: join(logs, '12-browser.har'), content: 'omit' },
});
const page = await context.newPage();

page.on('console', (msg) => consoleEvents.push({ type: msg.type(), text: msg.text(), url: page.url() }));
page.on('pageerror', (err) => consoleEvents.push({ type: 'pageerror', text: err.message, url: page.url() }));
page.on('response', async (resp) => {
  if (!resp.url().includes('/api/')) return;
  const rec = { status: resp.status(), method: resp.request().method(), url: resp.url() };
  if (resp.status() >= 400) {
    rec.body = (await resp.text().catch(() => '')).slice(0, 800);
  }
  apiEvents.push(rec);
});

async function shot(name, selector) {
  await page.waitForTimeout(350);
  const full = join(shots, `${name}-full.png`);
  await page.screenshot({ path: full, fullPage: true });
  let detail = null;
  if (selector) {
    const loc = page.locator(selector).first();
    if (await loc.count()) {
      detail = join(shots, `${name}-detail.png`);
      await loc.screenshot({ path: detail }).catch(() => { detail = null; });
    }
  }
  step(`screenshot:${name}`, { full, detail, url: page.url() });
}

async function api(path, options = {}) {
  const resp = await page.request.fetch(`${base}${path}`, {
    ...options,
    headers: { 'content-type': 'application/json', ...(options.headers || {}) },
  });
  const text = await resp.text();
  apiEvents.push({ status: resp.status(), method: options.method || 'GET', url: `${base}${path}`, body: resp.status() >= 400 ? text.slice(0, 800) : undefined });
  let json = null;
  try { json = text ? JSON.parse(text) : null; } catch {}
  return { status: resp.status(), text, json };
}

await page.goto(`${base}/signin`, { waitUntil: 'domcontentloaded' });
await shot('01-signin-default', 'form');
await page.fill('#login', email);
await page.fill('#passcode', passcode);
await page.click('button[type=submit]');
await page.waitForURL(/organizations|signin|signup/, { timeout: 15000 }).catch(() => {});
await shot('02-signed-in', 'body');

const me = await api('/api/auth/me');
step('auth-me', { status: me.status, body: me.json });

await page.route(`**/api/orgs/${slug}/access/overview**`, async (route) => {
  await new Promise((resolve) => setTimeout(resolve, 1300));
  await route.continue();
}, { times: 1 });
await page.goto(`${base}/organizations/${slug}/access`, { waitUntil: 'domcontentloaded' });
await shot('03-access-loading', '[data-testid="page-Access"]');
await page.waitForSelector('[data-testid="access-roles-view"]', { timeout: 15000 });
await shot('04-access-default-roles-mappings', '[data-testid="page-Access"]');
const accessText = await page.locator('[data-testid="page-Access"]').innerText();
step('access-entry-contract', {
  hasRolesMappings: accessText.includes('Roles & mappings'),
  hasSubjectAccess: accessText.includes('Subject access'),
  hasProfiles: /Profiles/i.test(accessText),
});

await page.goto(`${base}/organizations/${slug}/ai-runtime`, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('[data-testid="page-AiRuntime"]');
await shot('05-ai-runtime-empty-models', '[data-testid="page-AiRuntime"]');
if ((await page.locator('[data-testid="ai-runtime-model-row"]').count()) === 0) {
  await page.click('[data-testid="ai-runtime-create-model"]');
  await page.fill('[data-testid="ai-runtime-model-key"]', 'claude-opus-4-8');
  await page.fill('[data-testid="ai-runtime-model-model-key"]', 'claude-opus-4-8');
  await page.fill('[data-testid="ai-runtime-model-display-name"]', 'Claude Opus 4.8');
  await page.fill('[data-testid="ai-runtime-model-compatible-clis"]', 'claude-code');
  await shot('06-ai-runtime-create-model-editing', '[data-testid="ai-runtime-model-form"]');
  await page.click('[data-testid="ai-runtime-form-save"]');
  await page.waitForSelector('[data-testid="ai-runtime-model-row"]', { timeout: 15000 });
} else {
  step('ai-runtime-model-create-skip', { reason: 'model already present from earlier retry' });
}
await shot('07-ai-runtime-model-saved', '[data-testid="ai-runtime-catalog"]');

await page.click('[data-testid="ai-runtime-edit-model"]');
await page.fill('[data-testid="ai-runtime-model-display-name"]', 'Claude Opus 4.8 UI Edited');
await shot('08-ai-runtime-edit-model', '[data-testid="ai-runtime-model-form"]');
await page.click('[data-testid="ai-runtime-form-save"]');
await page.waitForTimeout(800);
await shot('09-ai-runtime-edit-saved', '[data-testid="ai-runtime-catalog"]');

const workers = (await api(`/api/orgs/${slug}/workers`)).json.workers || [];
step('workers-visible', { count: workers.length, workers: workers.map((w) => ({ id: w.worker_id, status: w.status, version: w.worker_version })) });
if (workers.length === 0) throw new Error('no org-bound workers visible');

await page.goto(`${base}/organizations/${slug}/teams/agents`, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('[data-testid="page-TeamsDirectoryAgents"], [data-testid="agents-add-btn"]');
await shot('10-agents-before-create', '[data-testid="page-Agents"]');
let agents = (await api(`/api/orgs/${slug}/agents`)).json.agents || [];
if (!agents.some((a) => a.name === 'UI Bound Agent')) {
  await page.click('[data-testid="agents-add-btn"]');
  await page.fill('[data-testid="agent-create-name"]', 'UI Bound Agent');
  await page.fill('[data-testid="agent-create-description"]', 'created by t1450 UI hard gate');
  await page.click('[data-testid="agent-create-worker-trigger"]');
  await page.locator('[data-testid="agent-create-worker-option"]').filter({ hasText: workers[0].name || workers[0].worker_id }).first().click();
  await shot('11-agent-create-bound-worker', '[data-testid="agent-create-modal"]');
  await page.click('[data-testid="agent-create-submit"]');
  await page.waitForSelector('[data-testid="agent-row-UI Bound Agent"]', { timeout: 15000 });
} else {
  step('agent-create-skip', { reason: 'UI Bound Agent already present from earlier retry' });
}
await shot('12-agent-created-direct-binding', '[data-testid="agents-table"]');
agents = (await api(`/api/orgs/${slug}/agents`)).json.agents || [];
step('agents-after-ui-create', { count: agents.length, agents: agents.map((a) => ({ id: a.id, worker_id: a.worker_id, cli: a.cli, model: a.model })) });

const project2 = await api(`/api/orgs/${slug}/projects`, {
  method: 'POST',
  data: JSON.stringify({ name: `Beta UI Gate ${runId}`, description: 'second project for multi-project evidence' }),
});
step('multi-project-created', { status: project2.status, body: project2.json });

const team = await api(`/api/orgs/${slug}/teams`, {
  method: 'POST',
  data: JSON.stringify({
    name: `UI Hard Gate Team ${runId}`,
    description: 'team role mapping evidence',
    visibility: 'org-private',
    roles: [
      { role: 'planner', count: 1, cli: 'claude-code', model: 'claude-opus-4-8', max_concurrency: 1, ram_role_keys: ['Team basic'], access_requirements: ['team.read', 'team.memory.read'] },
      { role: 'reviewer', count: 1, cli: 'claude-code', model: 'claude-opus-4-8', max_concurrency: 1, ram_role_keys: ['Team contributor'], access_requirements: ['team.read', 'team.write', 'team.memory.read', 'team.memory.propose'] },
    ],
  }),
});
step('team-created-for-mappings', { status: team.status, id: team.json?.id, roles: team.json?.roles?.map((r) => ({ role: r.role, ram_role_keys: r.ram_role_keys })) });

await page.goto(`${base}/organizations/${slug}/access?view=team-role-mappings`, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('[data-testid="access-team-role-mappings-view"]', { timeout: 15000 });
await shot('13-team-role-mappings-nonempty', '[data-testid="access-team-role-mappings-view"]');
const mappingRows = await page.locator('[data-testid^="access-mapping-"]').count();
step('team-role-mappings-visible', { mappingRows });

const row = page.locator('[data-testid^="access-mapping-"]').filter({ hasText: 'planner' }).first();
const roleTrigger = row.locator('[data-testid*="-trigger"]').first();
if (await roleTrigger.count()) {
  await roleTrigger.click();
  await page.getByRole('option', { name: /Team curator/ }).first().click();
}
await row.getByText('Preview impact').click().catch(async () => {
  await row.locator('button', { hasText: 'Preview impact' }).click();
});
await page.waitForSelector('[data-testid="access-mapping-preview"]', { timeout: 10000 }).catch(() => {});
await shot('14-team-role-mapping-preview', '[data-testid="access-team-role-mappings-view"]');
await row.getByText('Save mapping').click().catch(async () => {
  await row.locator('button', { hasText: 'Save mapping' }).click();
});
if (await page.locator('[data-testid="confirm-modal-confirm"]').count()) {
  await page.click('[data-testid="confirm-modal-confirm"]');
}
await page.waitForTimeout(1200);
await shot('15-team-role-mapping-save-refresh', '[data-testid="access-team-role-mappings-view"]');

await page.goto(`${base}/organizations/${slug}/access`, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('[data-testid="access-roles-view"]', { timeout: 15000 });
const roleName = `UI release operator ${runId}`;
const createRole = page.locator('[data-testid="access-role-create"]');
await createRole.locator('[data-testid="access-role-name"]').fill(roleName);
await createRole.locator('[data-testid="access-role-description"]').fill('created from UI');
await createRole.locator('[data-testid="access-role-permissions"] button').filter({ hasText: 'org.read' }).click();
await createRole.locator('[data-testid="access-role-permissions"] button').filter({ hasText: 'project.write' }).click();
await shot('16-ram-role-create-editing', '[data-testid="access-role-create"]');
await page.click('[data-testid="access-role-create-submit"]');
await page.waitForSelector('[data-testid="access-role-detail"]', { timeout: 10000 });
await shot('17-ram-role-created', '[data-testid="access-role-detail"]');

const roleDetailText = await page.locator('[data-testid="access-role-detail"]').innerText();
const rolesNow = (await api(`/api/orgs/${slug}/access/ram-roles`)).json.roles || [];
const customRole = rolesNow.find((r) => r.name === roleName);
step('ram-role-created-api-readback', { customRole, roleDetailText: roleDetailText.slice(0, 300) });
if (!customRole) throw new Error('custom role not found after UI create');

const customDetail = page.locator('[data-testid="access-role-detail"]');
await customDetail.locator('[data-testid="access-role-permissions"] button').filter({ hasText: 'team.memory.review' }).click();
await shot('18-ram-role-publish-editing', '[data-testid="access-role-detail"]');
await customDetail.locator('[data-testid="access-role-new-version-submit"]').click();
await page.waitForTimeout(1000);
await shot('19-ram-role-published-v2', '[data-testid="access-role-detail"]');

await page.click('[data-testid="access-role-disable-submit"]');
await page.waitForTimeout(1000);
await shot('20-ram-role-revoked-refresh', '[data-testid="access-roles-view"]');

const casRole = await api(`/api/orgs/${slug}/access/ram-roles`, {
  method: 'POST',
  data: JSON.stringify({ name: `UI CAS role ${runId}`, description: '409 proof', permissions: ['org.read'] }),
});
const casId = casRole.json?.id;
await page.goto(`${base}/organizations/${slug}/access`, { waitUntil: 'domcontentloaded' });
await page.waitForSelector(`[data-testid="access-role-row-${casId}"]`, { timeout: 10000 });
await page.click(`[data-testid="access-role-row-${casId}"]`);
await page.waitForSelector('[data-testid="access-role-detail"]');
await api(`/api/orgs/${slug}/access/ram-roles/${casId}/versions`, {
  method: 'POST',
  data: JSON.stringify({ expected_latest_version: 1, permissions: ['org.read', 'project.write'] }),
});
const casDetail = page.locator('[data-testid="access-role-detail"]');
await casDetail.locator('[data-testid="access-role-permissions"] button').filter({ hasText: 'team.memory.review' }).click();
await casDetail.locator('[data-testid="access-role-new-version-submit"]').click();
await page.waitForTimeout(1000);
await shot('21-ui-triggered-409-cas', '[data-testid="access-role-detail"]');

const memberDisplayName = `Member 403 UI ${runId}`;
const member = await api(`/api/orgs/${slug}/members`, {
  method: 'POST',
  data: JSON.stringify({ display_name: memberDisplayName, role: 'member' }),
});
step('member-created-for-403', { status: member.status, identity_id: member.json?.identity_id, hasTempPasscode: !!member.json?.temp_passcode });
if (!member.json?.temp_passcode) throw new Error(`member create did not return temp passcode, status=${member.status}`);
const memberCtx = await browser.newContext({ viewport: { width: 1440, height: 950 } });
const memberPage = await memberCtx.newPage();
memberPage.on('response', async (resp) => {
  if (resp.url().includes('/api/') && resp.status() >= 400) {
    apiEvents.push({ status: resp.status(), method: resp.request().method(), url: resp.url(), body: (await resp.text().catch(() => '')).slice(0, 800), actor: 'member' });
  }
});
await memberPage.goto(`${base}/signin`, { waitUntil: 'domcontentloaded' });
await memberPage.fill('#login', memberDisplayName);
await memberPage.fill('#passcode', member.json.temp_passcode);
await memberPage.click('button[type=submit]');
await memberPage.waitForTimeout(1500);
await memberPage.goto(`${base}/organizations/${slug}/access`, { waitUntil: 'domcontentloaded' });
await memberPage.waitForSelector('[data-testid="access-forbidden"]', { timeout: 15000 });
await memberPage.screenshot({ path: join(shots, '22-ui-403-forbidden-full.png'), fullPage: true });
await memberPage.locator('[data-testid="access-forbidden"]').screenshot({ path: join(shots, '22-ui-403-forbidden-detail.png') });
step('ui-403-forbidden', { text: (await memberPage.locator('[data-testid="access-forbidden"]').innerText()).slice(0, 500) });
const memberEffectiveProbe = await memberPage.evaluate(async ({ orgSlug, identityId, orgId }) => {
  const params = new URLSearchParams({
    subject_ref: `user:${identityId}`,
    resource_kind: 'org',
    resource_id: orgId,
  });
  const response = await fetch(`/api/orgs/${orgSlug}/permissions/effective?${params.toString()}`);
  return { status: response.status, body: await response.text() };
}, { orgSlug: slug, identityId: member.json.identity_id, orgId: project2.json.organization_id });
apiEvents.push({
  status: memberEffectiveProbe.status,
  method: 'GET',
  url: `${base}/api/orgs/${slug}/permissions/effective?subject_ref=user:${member.json.identity_id}&resource_kind=org&resource_id=${project2.json.organization_id}`,
  body: memberEffectiveProbe.body.slice(0, 800),
  actor: 'member-ui-session',
});
let memberEffectiveJson = null;
try { memberEffectiveJson = JSON.parse(memberEffectiveProbe.body); } catch {}
step('member-ui-session-effective-access-probe', {
  status: memberEffectiveProbe.status,
  hasManagePermission: !!memberEffectiveJson?.permissions?.some((p) => p.key === 'org.member.role.manage'),
  body: memberEffectiveProbe.body.slice(0, 300),
});
await memberCtx.close();

await page.goto(`${base}/organizations/${slug}/access?view=subject-access`, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('[data-testid="access-subject-view"]', { timeout: 15000 });
await shot('23-subject-access-direct-and-inherited', '[data-testid="access-subject-view"]');

writeFileSync(join(logs, '12-browser-api-events.json'), JSON.stringify(apiEvents, null, 2));
writeFileSync(join(logs, '12-browser-console-events.json'), JSON.stringify(consoleEvents, null, 2));
writeFileSync(join(logs, '12-browser-steps.json'), JSON.stringify(events, null, 2));

await context.close();
await browser.close();
