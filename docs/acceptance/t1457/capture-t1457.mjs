import playwright from '../../../tests/e2e/v2/node_modules/@playwright/test/index.js';
import { execFileSync, spawn } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const repoRoot = resolve(new URL('../../..', import.meta.url).pathname);
const outDir = resolve(repoRoot, 'docs/acceptance/t1457');
const binary = resolve(repoRoot, 'bin/agent-center');
const canonicalPath = '/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png';
const canonicalSHA256 = '80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56';
const { chromium, request: playwrightRequest, expect } = playwright;

const stateDefs = [
  ['01-role-list-detail', 'Role list, selected role detail entry, work config, RAM mapping table, safeguards'],
  ['02-ram-mapping-filter', 'RAM mapping table filter/search state'],
  ['03-mapping-edit-drawer', 'Team Role RAM mapping edit drawer with work config and CAS read'],
  ['04-mapping-preview', 'Mapping preview immediate impact state'],
  ['05-mapping-cas-error', 'Mapping stale CAS error and refresh affordance'],
  ['06-ram-role-create-drawer', 'RAM Role create drawer and permission picker'],
  ['07-ram-role-edit-version', 'RAM Role edit drawer and versioned write controls'],
  ['08-ram-role-duplicate', 'RAM Role duplicate drawer'],
  ['09-delete-safeguard', 'Referenced RAM Role delete safeguard confirmation'],
  ['10-ram-role-error', 'RAM Role stale version error state'],
  ['11-overflow-1280', 'Fresh 1280px overflow measurement state'],
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

async function sha256(path) {
  return createHash('sha256').update(await readFile(path)).digest('hex');
}

async function api(ctx, method, url, data, ok = [200, 201, 204]) {
  const response = await ctx[method](url, data === undefined ? undefined : { data });
  const bodyText = await response.text();
  if (!ok.includes(response.status())) {
    throw new Error(`${method.toUpperCase()} ${url} -> ${response.status()} ${bodyText}`);
  }
  if (response.status() === 204 || bodyText.trim() === '') return null;
  return JSON.parse(bodyText);
}

async function screenshot(page, name, description, evidence, width = 1672, height = 941) {
  await page.setViewportSize({ width, height });
  await page.waitForLoadState('networkidle');
  const metrics = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    clientHeight: document.documentElement.clientHeight,
    scrollHeight: document.documentElement.scrollHeight,
    url: location.href,
  }));
  if (metrics.scrollWidth > metrics.clientWidth) {
    throw new Error(`${name} horizontal overflow: clientWidth=${metrics.clientWidth} scrollWidth=${metrics.scrollWidth}`);
  }
  const path = join(outDir, `${name}-${width}x${height}.png`);
  await page.screenshot({ path, fullPage: false });
  evidence.screenshots.push({ name, description, viewport: `${width}x${height}`, path, metrics });
  return { path, metrics };
}

async function selectOption(page, value) {
  await page.getByTestId('team-role-drawer-ram-roles-trigger').click();
  await page.locator(`[data-testid="team-role-drawer-ram-roles-option"][data-value="${value}"]`).click();
  await page.keyboard.press('Escape');
}

async function removeChip(page, value) {
  const chip = page.locator(`[data-testid="team-role-drawer-ram-roles-chip"][data-value="${value}"]`);
  await chip.locator('[data-testid="team-role-drawer-ram-roles-chip-remove"]').click();
}

async function fillRAMRoleDrawer(page, suffix) {
  await page.getByTestId('team-ram-role-name').fill(`Evidence RAM Role ${suffix}`);
  await page.getByTestId('team-ram-role-stable-key').fill(`team.evidence.${suffix}`);
  await page.getByTestId('team-ram-role-description').fill(`Evidence role ${suffix} created by the T1457 browser harness.`);
  await page.getByTestId('team-ram-role-scope').fill('team');
  await page.getByTestId('team-ram-role-permissions').getByText('team.read').click();
}

async function diffCanonical() {
  const diffScript = join(outDir, 'diff-canonical.py');
  execFileSync('python3', [diffScript, canonicalPath, outDir, ...stateDefs.slice(0, 10).map(([name]) => name)], {
    cwd: repoRoot,
    stdio: 'inherit',
  });
}

async function main() {
  await mkdir(outDir, { recursive: true });
  if (!existsSync(binary)) throw new Error(`missing binary ${binary}; run make build-backend first`);
  if (!existsSync(canonicalPath)) throw new Error(`missing canonical ${canonicalPath}`);
  const canonicalActual = await sha256(canonicalPath);
  if (canonicalActual !== canonicalSHA256) {
    throw new Error(`canonical SHA256 ${canonicalActual}, want ${canonicalSHA256}`);
  }

  const head = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: repoRoot, encoding: 'utf8' }).trim();
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
  const stdout = [];
  proc.stderr.on('data', (chunk) => stderr.push(chunk));
  proc.stdout.on('data', (chunk) => stdout.push(chunk));
  const baseURL = `http://127.0.0.1:${webPort}`;
  const evidence = {
    head,
    canonical: { path: canonicalPath, sha256: canonicalActual },
    baseURL,
    screenshots: [],
    browser: { console: [], pageErrors: [], failedRequests: [], apiResponses: [] },
    apiChecks: {},
    overflow1280: null,
  };

  let browser;
  let setup;
  let authed;
  try {
    const deadline = Date.now() + 15000;
    let ready = false;
    while (Date.now() < deadline) {
      try {
        const response = await fetch(`${baseURL}/api/system/version`);
        if (response.ok) {
          ready = true;
          break;
        }
      } catch {
        await new Promise((resolveWait) => setTimeout(resolveWait, 100));
      }
    }
    if (!ready) throw new Error(`server did not become ready at ${baseURL}`);

    const version = await (await fetch(`${baseURL}/api/system/version`)).json();
    evidence.version = version;
    if (version.commit !== head) {
      throw new Error(`/api/system/version.commit=${version.commit}, want exact HEAD ${head}`);
    }

    setup = await playwrightRequest.newContext();
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
    authed = await playwrightRequest.newContext({ extraHTTPHeaders: { Cookie: `ac_session=${sessionCookie}` } });

    const initialRuntime = await api(authed, 'get', `${orgApi}/ai-runtime`);
    let runtimeRevision = initialRuntime.revision;
    if (!initialRuntime.clis?.some((cli) => cli.key === 'codex')) {
      const cliWrite = await api(authed, 'post', `${orgApi}/ai-runtime/clis`, {
        expected_revision: runtimeRevision,
        value: { key: 'codex', display_name: 'Codex', executable: 'codex', required_features: [], enabled: true },
      });
      runtimeRevision = cliWrite.revision;
    }
    if (!initialRuntime.models?.some((model) => model.key === 'gpt-5')) {
      await api(authed, 'post', `${orgApi}/ai-runtime/models`, {
        expected_revision: runtimeRevision,
        value: {
          key: 'gpt-5',
          model_key: 'gpt-5',
          display_name: 'GPT-5',
          compatible_cli_keys: ['codex'],
          default_parameters: {},
          enabled: true,
        },
      });
    }

    const reviewerRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'Review Gate Operator',
      stable_key: 'team.review.operator',
      description: 'Review, audit, and approve Team Role access changes.',
      scope: 'team',
      permissions: ['team.read', 'team.memory.review'],
    });
    const developerRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'Developer Work Executor',
      stable_key: 'team.developer.executor',
      description: 'Developer execution permissions for project and task work.',
      scope: 'team',
      permissions: ['team.read', 'project.read', 'project.write'],
    });
    const opsRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'Platform Ops Steward',
      stable_key: 'team.platform.ops',
      description: 'High risk operational permissions for deployment safeguards.',
      scope: 'team',
      permissions: ['team.read', 'team.write'],
    });
    const disposableRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'Disposable Evidence Role',
      stable_key: 'team.evidence.disposable',
      description: 'Unreferenced role used for delete CRUD verification.',
      scope: 'team',
      permissions: ['team.read'],
    });
    const team = await api(authed, 'post', `${orgApi}/teams`, {
      name: 'Platform Team',
      description: 'Canonical Team IA validation team for role configuration and RAM mapping.',
      visibility: 'private',
      roles: [
        {
          role: 'developer',
          cli: 'codex',
          model: 'gpt-5',
          max_concurrency: 4,
          count: 3,
          tags: 'frontend,backend,ram',
          ram_role_keys: ['Developer Work Executor'],
          access_requirements: ['project.read', 'project.write'],
        },
        {
          role: 'reviewer',
          cli: 'codex',
          model: 'gpt-5',
          max_concurrency: 2,
          count: 2,
          tags: 'review,audit,safeguard',
          ram_role_keys: ['Review Gate Operator'],
          access_requirements: ['team.memory.review'],
        },
        {
          role: 'ops',
          cli: 'codex',
          model: 'gpt-5',
          max_concurrency: 1,
          count: 1,
          tags: 'deploy,guarded',
          ram_role_keys: ['Platform Ops Steward'],
          access_requirements: ['team.write'],
        },
      ],
    });
    const mapping1 = await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, {
      ram_role_ids: [developerRole.id, reviewerRole.id],
      expected_version: 1,
    });
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/reviewer/ram-roles`, {
      ram_role_ids: [reviewerRole.id],
      expected_version: 1,
    });
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/ops/ram-roles`, {
      ram_role_ids: [opsRole.id],
      expected_version: 1,
    });

    const preview = await api(authed, 'post', `${orgApi}/teams/${team.id}/roles/developer/ram-roles/preview`, {
      ram_role_ids: [developerRole.id],
    });
    const staleMapping = await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, {
      ram_role_ids: [developerRole.id],
      expected_version: 1,
    }, [409]);
    const updatedRole = await api(authed, 'patch', `${orgApi}/access/ram-roles/${disposableRole.id}`, {
      name: 'Disposable Evidence Role Updated',
      stable_key: 'team.evidence.disposable',
      description: 'Updated through the CRUD verification path.',
      scope: 'team',
      permissions: ['team.read'],
      expected_latest_version: disposableRole.latest.version,
    });
    const versionedRole = await api(authed, 'post', `${orgApi}/access/ram-roles/${disposableRole.id}/versions`, {
      name: 'Disposable Evidence Role Updated',
      stable_key: 'team.evidence.disposable',
      description: 'Versioned through the CRUD verification path.',
      scope: 'team',
      permissions: ['team.read', 'project.read'],
      expected_latest_version: updatedRole.latest.version,
    });
    const staleRole = await api(authed, 'post', `${orgApi}/access/ram-roles/${disposableRole.id}/versions`, {
      name: 'Disposable Evidence Role Updated',
      stable_key: 'team.evidence.disposable',
      description: 'Expected stale version failure.',
      scope: 'team',
      permissions: ['team.read'],
      expected_latest_version: updatedRole.latest.version,
    }, [409]);
    await api(authed, 'delete', `${orgApi}/access/ram-roles/${disposableRole.id}`, {
      expected_latest_version: versionedRole.latest.version,
      confirm_unreferenced: true,
      reason: 'T1457 CRUD delete verification',
    });
    evidence.apiChecks = {
      createRAMRole: reviewerRole.id,
      updateRAMRole: updatedRole.latest.version,
      versionRAMRole: versionedRole.latest.version,
      deleteUnreferencedRAMRole: disposableRole.id,
      previewMapping: preview.version,
      staleMappingStatus: staleMapping.code ?? staleMapping.error ?? staleMapping.message,
      staleRoleStatus: staleRole.code ?? staleRole.error ?? staleRole.message,
    };

    browser = await chromium.launch();
    const context = await browser.newContext({
      viewport: { width: 1672, height: 941 },
      deviceScaleFactor: 1,
      colorScheme: 'light',
    });
    await context.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    page.on('console', (msg) => evidence.browser.console.push({ type: msg.type(), text: msg.text() }));
    page.on('pageerror', (error) => evidence.browser.pageErrors.push(error.message));
    page.on('response', (response) => {
      const url = response.url();
      if (url.includes('/api/')) evidence.browser.apiResponses.push({ status: response.status(), url });
    });
    page.on('requestfailed', (request) => evidence.browser.failedRequests.push({
      url: request.url(),
      failure: request.failure()?.errorText,
    }));

    await page.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    await expect(page.getByTestId('team-role-list')).toContainText('Platform Team');
    await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Developer Work Executor');
    await expect(page.getByTestId(`team-ram-role-delete-blocked-${developerRole.id}`)).toBeVisible();
    await screenshot(page, '01-role-list-detail', stateDefs[0][1], evidence);

    await page.getByTestId('team-role-mapping-search').fill('developer');
    await expect(page.getByTestId(`team-role-mapping-row-${team.id}-developer`)).toBeVisible();
    await screenshot(page, '02-ram-mapping-filter', stateDefs[1][1], evidence);
    await page.getByTestId('team-role-mapping-search').fill('');

    await page.getByTestId(`team-role-edit-mapping-${team.id}-developer`).click();
    await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
    await expect(page.getByTestId('team-role-work-config')).toContainText('codex');
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText(`CAS v${mapping1.version}`);
    await screenshot(page, '03-mapping-edit-drawer', stateDefs[2][1], evidence);

    await removeChip(page, reviewerRole.id);
    await page.getByRole('button', { name: 'Preview impact' }).click();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Added / removed');
    await screenshot(page, '04-mapping-preview', stateDefs[3][1], evidence);

    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, {
      ram_role_ids: [developerRole.id, reviewerRole.id],
      expected_version: mapping1.version,
    });
    await page.getByRole('button', { name: 'Apply mapping' }).click();
    await expect(page.getByTestId('team-role-mapping-confirm')).toBeVisible();
    await page.getByRole('button', { name: 'Apply now' }).click();
    await expect(page.getByTestId('team-role-mapping-error')).toBeVisible();
    await screenshot(page, '05-mapping-cas-error', stateDefs[4][1], evidence);
    await page.getByLabel('Close drawer').click();

    await page.getByTestId('team-roles-create-ram-role').click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await fillRAMRoleDrawer(page, suffix);
    await screenshot(page, '06-ram-role-create-drawer', stateDefs[5][1], evidence);
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Edit' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create version' })).toBeVisible();
    await screenshot(page, '07-ram-role-edit-version', stateDefs[6][1], evidence);
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Duplicate' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await expect(page.getByTestId('team-ram-role-name')).toHaveValue(/copy/);
    await screenshot(page, '08-ram-role-duplicate', stateDefs[7][1], evidence);
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByText('Delete is blocked until mappings are removed or migrated.')).toBeVisible();
    await screenshot(page, '09-delete-safeguard', stateDefs[8][1], evidence);
    await page.getByRole('button', { name: 'Blocked' }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('still referenced');

    await page.getByTestId(`team-ram-role-${opsRole.id}`).getByRole('button', { name: 'Edit' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    const opsDetail = await api(authed, 'get', `${orgApi}/access/ram-roles/${opsRole.id}`);
    await api(authed, 'post', `${orgApi}/access/ram-roles/${opsRole.id}/versions`, {
      name: opsDetail.name,
      stable_key: opsDetail.stable_key,
      description: `${opsDetail.description} Updated externally.`,
      scope: opsDetail.scope,
      permissions: ['team.read', 'team.write'],
      expected_latest_version: opsDetail.latest.version,
    });
    await page.getByRole('button', { name: 'Create version' }).click();
    await expect(page.getByTestId('team-ram-role-error')).toBeVisible();
    await screenshot(page, '10-ram-role-error', stateDefs[9][1], evidence);
    await page.getByLabel('Close drawer').click();

    await page.setViewportSize({ width: 1280, height: 941 });
    await page.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'networkidle' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    const overflow1280 = await screenshot(page, '11-overflow-1280', stateDefs[10][1], evidence, 1280, 941);
    evidence.overflow1280 = overflow1280.metrics;

    await context.close();
    await authed.dispose();
    await setup.dispose();
    await diffCanonical();

    const badConsole = evidence.browser.console.filter((entry) =>
      ['error', 'warning'].includes(entry.type) &&
      !/Download the React DevTools/.test(entry.text),
    );
    const failedApi = evidence.browser.apiResponses.filter((entry) => entry.status >= 500);
    if (badConsole.length > 0 || evidence.browser.pageErrors.length > 0 || evidence.browser.failedRequests.length > 0 || failedApi.length > 0) {
      evidence.browser.failures = { badConsole, failedApi };
      await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify(evidence, null, 2)}\n`, 'utf8');
      throw new Error(`browser evidence has console/network failures; see ${join(outDir, 'capture-state.json')}`);
    }

    await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify(evidence, null, 2)}\n`, 'utf8');
  } finally {
    if (browser) await browser.close().catch(() => {});
    proc.kill('SIGTERM');
    await new Promise((resolveWait) => setTimeout(resolveWait, 500));
    if (!proc.killed) proc.kill('SIGKILL');
    await rm(tempDir, { recursive: true, force: true });
  }
  if (stderr.length) process.stderr.write(Buffer.concat(stderr).toString('utf8'));
  if (stdout.length) process.stdout.write(Buffer.concat(stdout).toString('utf8'));
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
