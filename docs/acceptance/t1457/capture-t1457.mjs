import playwright from '../../../tests/e2e/v2/node_modules/@playwright/test/index.js';
import { spawn, spawnSync } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const repoRoot = resolve(new URL('../../..', import.meta.url).pathname);
const outDir = resolve(repoRoot, 'docs/acceptance/t1457');
const binary = resolve(repoRoot, 'bin/agent-center');
const diffScript = resolve(outDir, 'canonical-diff.py');
const canonicalSHA = '80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56';
const canonicalCandidates = [
  process.env.T1457_CANONICAL,
  '/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KTVBJCXN6XV8MXK3B9S5VS2S/tasks/team-roles-ddba-gate/t1457-canonical.png',
  '/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png',
].filter(Boolean);
const { chromium, request: playwrightRequest, expect } = playwright;

const states = [
  ['01-role-list', 'Team IA role list, detail entries, work config and mapping table'],
  ['02-role-detail-drawer', 'Role detail entry opens mapping drawer with CAS work config'],
  ['03-ram-role-create-drawer', 'RAM Role create drawer with permission checklist'],
  ['04-ram-role-edit-drawer', 'RAM Role edit drawer with versioned write controls'],
  ['05-ram-role-duplicate-drawer', 'RAM Role duplicate drawer'],
  ['06-mapping-preview', 'Mapping preview impact state before apply'],
  ['07-version-duplicate-delete-safeguard', 'Version cards, duplicate affordance, referenced delete safeguard'],
  ['08-cas-conflict-error', 'Mapping CAS conflict error state'],
  ['09-create-error', 'RAM Role create duplicate/error state'],
];

async function freePort() {
  const net = await import('node:net');
  return new Promise((resolvePort, reject) => {
    const server = net.createServer();
    server.on('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      const port = typeof address === 'object' && address ? address.port : 0;
      server.close(() => resolvePort(port));
    });
  });
}

async function fileSHA(path) {
  return createHash('sha256').update(await readFile(path)).digest('hex');
}

async function findCanonical() {
  for (const candidate of canonicalCandidates) {
    if (!candidate || !existsSync(candidate)) continue;
    const sha = await fileSHA(candidate);
    if (sha === canonicalSHA) return candidate;
    throw new Error(`canonical SHA mismatch for ${candidate}: ${sha}`);
  }
  throw new Error(`T1457 canonical PNG not found; set T1457_CANONICAL to SHA256 ${canonicalSHA}`);
}

async function api(ctx, method, url, data, ok = [200, 201, 204]) {
  const response = await ctx[method](url, data === undefined ? undefined : { data });
  if (!ok.includes(response.status())) {
    throw new Error(`${method.toUpperCase()} ${url} -> ${response.status()} ${await response.text()}`);
  }
  return response.status() === 204 ? null : response.json();
}

async function capture(page, canonical, stateId, audit, note) {
  await page.waitForTimeout(150);
  const metrics = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    clientHeight: document.documentElement.clientHeight,
    scrollHeight: document.documentElement.scrollHeight,
    url: location.href,
  }));
  if (metrics.clientWidth !== metrics.scrollWidth) {
    throw new Error(`${stateId} horizontal overflow: clientWidth=${metrics.clientWidth} scrollWidth=${metrics.scrollWidth}`);
  }
  const candidate = join(outDir, `${stateId}-candidate-1672x941.png`);
  const overlay = join(outDir, `${stateId}-canonical-overlay.png`);
  const diff = join(outDir, `${stateId}-canonical-pixel-diff.png`);
  const stats = join(outDir, `${stateId}-canonical-diff-stats.json`);
  await page.screenshot({ path: candidate, fullPage: false });
  const diffRun = spawnSync('python3', [diffScript, '--canonical', canonical, '--candidate', candidate, '--overlay', overlay, '--diff', diff, '--stats', stats], {
    cwd: repoRoot,
    encoding: 'utf8',
  });
  if (diffRun.status !== 0) throw new Error(`canonical diff failed for ${stateId}: ${diffRun.stderr || diffRun.stdout}`);
  audit.states.push({ id: stateId, note, metrics, candidate, overlay, diff, stats });
}

async function openMapping(page, teamId, role) {
  await page.goto(page.t1457.url, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
  await page.getByTestId(`team-role-edit-mapping-${teamId}-${role}`).click();
  await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
}

async function closeMultiSelect(page, testId) {
  await page.mouse.click(12, 12);
  await page.getByTestId(`${testId}-options`).waitFor({ state: 'detached', timeout: 5000 });
}

async function main() {
  await mkdir(outDir, { recursive: true });
  const canonical = await findCanonical();
  const tempDir = await mkdtemp(join(tmpdir(), 'agent-center-t1457-'));
  const grpcPort = await freePort();
  const webPort = await freePort();
  const dbPath = join(tempDir, 'agent-center.db');
  const sockPath = join(tempDir, 'admin.sock');
  const masterKeyPath = join(tempDir, 'master.key');
  const configPath = join(tempDir, 'config.yaml');
  await writeFile(masterKeyPath, `${randomBytes(32).toString('base64')}\n`, 'utf8');
  await chmod(masterKeyPath, 0o600);
  await writeFile(configPath, `server:
  listen_addr: ":${grpcPort}"
  sqlite_path: "${dbPath}"
  admin_socket_path: "${sockPath}"
web_console:
  enabled: true
  listen_addr: "127.0.0.1:${webPort}"
secret_management:
  master_key_file: "${masterKeyPath}"
`, 'utf8');

  const proc = spawn(binary, ['server', '--config', configPath], { stdio: ['ignore', 'pipe', 'pipe'] });
  const stderr = [];
  proc.stderr.on('data', (chunk) => stderr.push(chunk));
  const baseURL = `http://127.0.0.1:${webPort}`;
  const audit = {
    canonical,
    canonical_sha256: canonicalSHA,
    baseURL,
    viewport: { width: 1672, height: 941 },
    states: [],
    browserAssertions: [],
    apiChecks: [],
    networkFailures: [],
    consoleErrors: [],
  };
  try {
    const deadline = Date.now() + 10000;
    let ready = false;
    while (Date.now() < deadline) {
      try {
        const response = await fetch(`${baseURL}/api/health`);
        if (response.ok) {
          ready = true;
          break;
        }
      } catch {
        await new Promise((resolveWait) => setTimeout(resolveWait, 100));
      }
    }
    if (!ready) throw new Error(`server did not become ready at ${baseURL}`);

    const setup = await playwrightRequest.newContext();
    const suffix = Date.now().toString(36);
    const signup = await setup.post(`${baseURL}/api/auth/signup`, {
      data: {
        display_name: `t1457-owner-${suffix}`,
        email: `t1457-${suffix}@example.test`,
        passcode: 'T1457Pass1!',
        organization_name: `T1457 Org ${suffix}`,
      },
    });
    if (!signup.ok()) throw new Error(`signup -> ${signup.status()} ${await signup.text()}`);
    const sessionCookie = /ac_session=([^;]+)/.exec(signup.headers()['set-cookie'] || '')?.[1];
    const signedUp = await signup.json();
    const orgSlug = signedUp.organization_slug;
    const orgApi = `${baseURL}/api/orgs/${orgSlug}`;
    const authed = await playwrightRequest.newContext({ extraHTTPHeaders: { Cookie: `ac_session=${sessionCookie}` } });
    const version = await api(authed, 'get', `${baseURL}/api/system/version`);
    audit.version = version;

    const initialRuntime = await api(authed, 'get', `${orgApi}/ai-runtime`);
    let runtimeRevision = initialRuntime.revision;
    if (!initialRuntime.clis?.some((cli) => cli.key === 'codex')) {
      const cliWrite = await api(authed, 'post', `${orgApi}/ai-runtime/clis`, {
        expected_revision: runtimeRevision,
        value: { key: 'codex', display_name: 'Codex', executable: 'codex', required_features: [], enabled: true },
      });
      runtimeRevision = cliWrite.revision;
    }
    await api(authed, 'post', `${orgApi}/ai-runtime/models`, {
      expected_revision: runtimeRevision,
      value: { key: 'gpt-5', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], default_parameters: {}, enabled: true },
    });

    const reviewerRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'Review Gate Operator',
      stable_key: `team.review.operator.${suffix}`,
      description: 'Review, audit, and approve Team Role access changes.',
      scope: 'team',
      permissions: ['team.read', 'team.memory.review'],
    });
    const developerRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'Developer Work Executor',
      stable_key: `team.developer.executor.${suffix}`,
      description: 'Developer execution permissions for project and task work.',
      scope: 'team',
      permissions: ['team.read', 'project.read', 'project.write'],
    });
    const opsRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'Platform Ops Steward',
      stable_key: `team.platform.ops.${suffix}`,
      description: 'High risk operational permissions for deployment safeguards.',
      scope: 'team',
      permissions: ['team.read', 'team.write'],
    });
    const team = await api(authed, 'post', `${orgApi}/teams`, {
      name: 'Platform Team',
      description: 'Canonical Team IA validation team for role configuration and RAM mapping.',
      visibility: 'private',
      roles: [
        { role: 'developer', cli: 'codex', model: 'gpt-5', max_concurrency: 4, count: 3, tags: 'frontend,backend,ram', ram_role_keys: ['Developer Work Executor'], access_requirements: ['project.read', 'project.write'] },
        { role: 'reviewer', cli: 'codex', model: 'gpt-5', max_concurrency: 2, count: 2, tags: 'review,audit,safeguard', ram_role_keys: ['Review Gate Operator'], access_requirements: ['team.memory.review'] },
        { role: 'ops', cli: 'codex', model: 'gpt-5', max_concurrency: 1, count: 1, tags: 'deploy,guarded', ram_role_keys: ['Platform Ops Steward'], access_requirements: ['team.write'] },
      ],
    });
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, { ram_role_ids: [developerRole.id, reviewerRole.id], expected_version: 1 });
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/reviewer/ram-roles`, { ram_role_ids: [reviewerRole.id], expected_version: 1 });
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/ops/ram-roles`, { ram_role_ids: [opsRole.id], expected_version: 1 });

    const browser = await chromium.launch();
    const context = await browser.newContext({ viewport: { width: 1672, height: 941 }, deviceScaleFactor: 1, colorScheme: 'light' });
    await context.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    page.t1457 = { url: `${baseURL}/organizations/${orgSlug}/teams/roles` };
    page.on('console', (msg) => {
      if (!['error', 'warning'].includes(msg.type())) return;
      const entry = { type: msg.type(), text: msg.text() };
      if (msg.text().includes('409 (Conflict)')) {
        audit.browserAssertions.push(`expected console 409 from CAS/error state: ${msg.text()}`);
        return;
      }
      audit.consoleErrors.push(entry);
    });
    page.on('response', (response) => {
      if (response.url().includes('/api/') && response.status() >= 400 && ![400, 409].includes(response.status())) {
        audit.networkFailures.push({ status: response.status(), url: response.url() });
      }
    });

    await page.goto(page.t1457.url, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    await expect(page.getByTestId('team-role-list')).toContainText('Platform Team');
    await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Developer Work Executor');
    await expect(page.getByTestId(`team-ram-role-delete-blocked-${developerRole.id}`)).toBeVisible();
    await capture(page, canonical, '01-role-list', audit, states[0][1]);

    await page.getByTestId(`team-role-detail-${team.id}-developer`).click();
    await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
    await expect(page.getByTestId('team-role-work-config')).toContainText('gpt-5');
    await capture(page, canonical, '02-role-detail-drawer', audit, states[1][1]);
    await page.getByLabel('Close drawer').click();

    await page.getByTestId('team-roles-create-ram-role').click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await expect(page.getByTestId('team-ram-role-audit')).toContainText('Versioned writes');
    await capture(page, canonical, '03-ram-role-create-drawer', audit, states[2][1]);
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Edit' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await expect(page.getByTestId('team-ram-role-audit')).toContainText('Referenced by Platform Team/developer');
    await capture(page, canonical, '04-ram-role-edit-drawer', audit, states[3][1]);
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Duplicate' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toContainText('Duplicate RAM Role');
    await capture(page, canonical, '05-ram-role-duplicate-drawer', audit, states[4][1]);
    await page.getByLabel('Close drawer').click();

    await openMapping(page, team.id, 'developer');
    await page.getByTestId('team-role-drawer-ram-roles-trigger').click();
    await page.locator(`[data-testid="team-role-drawer-ram-roles-option"][data-value="${opsRole.id}"]`).click();
    await closeMultiSelect(page, 'team-role-drawer-ram-roles');
    await page.getByRole('button', { name: 'Preview impact' }).click();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Added / removed');
    await capture(page, canonical, '06-mapping-preview', audit, states[5][1]);
    await page.getByRole('button', { name: 'Apply mapping' }).click();
    await expect(page.getByTestId('team-role-mapping-confirm')).toBeVisible();
    await page.getByRole('button', { name: 'Apply now' }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Applied Platform Team / developer');
    audit.browserAssertions.push('mapping preview and apply succeeded through browser UI');

    await page.goto(page.t1457.url, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId(`team-ram-role-delete-blocked-${developerRole.id}`)).toBeVisible();
    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByRole('dialog')).toContainText('Delete is blocked');
    await capture(page, canonical, '07-version-duplicate-delete-safeguard', audit, states[6][1]);
    await page.getByRole('button', { name: 'Cancel' }).click();

    await openMapping(page, team.id, 'developer');
    const staleMapping = await api(authed, 'get', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`);
    await page.getByTestId('team-role-drawer-ram-roles-trigger').click();
    await page.locator(`[data-testid="team-role-drawer-ram-roles-option"][data-value="${reviewerRole.id}"]`).click();
    await closeMultiSelect(page, 'team-role-drawer-ram-roles');
    await page.getByRole('button', { name: 'Preview impact' }).click();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Added / removed');
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, { ram_role_ids: [developerRole.id], expected_version: staleMapping.version });
    await page.getByRole('button', { name: 'Apply mapping' }).click();
    await page.getByRole('button', { name: 'Apply now' }).click();
    await expect(page.getByTestId('team-role-mapping-error')).toBeVisible();
    await capture(page, canonical, '08-cas-conflict-error', audit, states[7][1]);
    audit.browserAssertions.push('stale browser mapping write produced visible CAS error');
    await page.getByLabel('Close drawer').click();

    await page.getByTestId('team-roles-create-ram-role').click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await page.getByTestId('team-ram-role-name').fill('Developer Work Executor');
    await page.getByTestId('team-ram-role-stable-key').fill(developerRole.stable_key);
    await page.getByTestId('team-ram-role-description').fill('Duplicate stable key rejection evidence.');
    await page.getByTestId('team-ram-role-permissions').getByRole('button').filter({ hasText: 'team.read' }).first().click();
    await page.getByRole('button', { name: 'Create', exact: true }).click();
    await expect(page.getByTestId('team-ram-role-error')).toBeVisible();
    await capture(page, canonical, '09-create-error', audit, states[8][1]);
    audit.browserAssertions.push('duplicate RAM Role create produced visible error');

    const created = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: `CRUD Browser Verified ${suffix}`,
      stable_key: `team.crud.verified.${suffix}`,
      description: 'API-backed CRUD verification role.',
      scope: 'team',
      permissions: ['team.read'],
    });
    const edited = await api(authed, 'post', `${orgApi}/access/ram-roles/${created.id}/versions`, {
      name: created.name,
      stable_key: created.stable_key,
      description: 'API-backed CRUD verification role edited.',
      scope: 'team',
      permissions: ['team.read', 'team.write'],
      expected_latest_version: created.latest.version,
    });
    const stale = await authed.post(`${orgApi}/access/ram-roles/${created.id}/versions`, {
      data: {
        name: created.name,
        stable_key: created.stable_key,
        description: 'stale write',
        scope: 'team',
        permissions: ['team.read'],
        expected_latest_version: created.latest.version,
      },
    });
    audit.apiChecks.push({ action: 'ram_role_create', id: created.id, status: 'ok' });
    audit.apiChecks.push({ action: 'ram_role_new_version', id: edited.id, latest_version: edited.latest.version, status: 'ok' });
    audit.apiChecks.push({ action: 'ram_role_cas_stale', status: stale.status(), ok: stale.status() === 409 });
    if (stale.status() !== 409) throw new Error(`expected stale RAM role version 409, got ${stale.status()}`);

    const overflowContext = await browser.newContext({ viewport: { width: 1280, height: 941 }, deviceScaleFactor: 1, colorScheme: 'light' });
    await overflowContext.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const overflowPage = await overflowContext.newPage();
    await overflowPage.goto(page.t1457.url, { waitUntil: 'domcontentloaded' });
    await expect(overflowPage.getByTestId('page-TeamsRoles')).toBeVisible();
    const overflow = await overflowPage.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
      url: location.href,
    }));
    await overflowPage.screenshot({ path: join(outDir, 'fresh-1280-overflow-candidate.png'), fullPage: false });
    if (overflow.clientWidth !== overflow.scrollWidth) throw new Error(`1280 overflow: ${JSON.stringify(overflow)}`);
    audit.overflow1280 = overflow;
    await overflowContext.close();

    await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify({
      ...audit,
      orgSlug,
      teamId: team.id,
      ramRoleIds: [developerRole.id, reviewerRole.id, opsRole.id],
      checkedStates: states.map(([id, note]) => ({ id, note })),
    }, null, 2)}\n`, 'utf8');
    await browser.close();
    await authed.dispose();
    await setup.dispose();
  } finally {
    proc.kill('SIGTERM');
    await new Promise((resolveWait) => setTimeout(resolveWait, 500));
    if (!proc.killed) proc.kill('SIGKILL');
    await rm(tempDir, { recursive: true, force: true });
  }
  if (stderr.length) process.stderr.write(Buffer.concat(stderr).toString('utf8'));
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
