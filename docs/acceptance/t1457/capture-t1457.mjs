import playwright from '../../../tests/e2e/v2/node_modules/@playwright/test/index.js';
import { spawn, execFileSync } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { chmod, copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, join, resolve } from 'node:path';

const repoRoot = resolve(new URL('../../..', import.meta.url).pathname);
const outDir = resolve(repoRoot, process.env.T1457_OUT_DIR ?? 'docs/acceptance/t1457');
const binary = resolve(repoRoot, 'bin/agent-center');
const canonicalSHA = '80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56';
const canonicalCandidates = [
  process.env.T1457_CANONICAL,
  resolve(repoRoot, 'docs/acceptance/t1457/canonical-t1457-sha80e51bb4.png'),
  '/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png',
].filter(Boolean);
const { chromium, request: playwrightRequest, expect } = playwright;

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

async function canonicalPath() {
  for (const candidate of canonicalCandidates) {
    if (!existsSync(candidate)) continue;
    const actual = await sha256(candidate);
    if (actual !== canonicalSHA) {
      throw new Error(`canonical SHA mismatch for ${candidate}: ${actual}`);
    }
    const stable = join(outDir, 'canonical-t1457-sha80e51bb4.png');
    if (resolve(candidate) !== stable) await copyFile(candidate, stable);
    return stable;
  }
  throw new Error(`canonical T1457 PNG with SHA256 ${canonicalSHA} was not found`);
}

async function api(ctx, method, url, data, ok = [200, 201, 204]) {
  const response = await ctx[method](url, data === undefined ? undefined : { data });
  if (!ok.includes(response.status())) {
    throw new Error(`${method.toUpperCase()} ${url} -> ${response.status()} ${await response.text()}`);
  }
  return response.status() === 204 ? null : response.json();
}

function git(args) {
  return execFileSync('git', args, { cwd: repoRoot, encoding: 'utf8' }).trim();
}

function magick(args, options = {}) {
  return execFileSync('magick', args, { cwd: repoRoot, encoding: 'utf8', stdio: options.stdio ?? ['ignore', 'pipe', 'pipe'] });
}

function metric(metricName, canonical, candidate) {
  try {
    execFileSync('magick', ['compare', '-metric', metricName, canonical, candidate, 'null:'], { encoding: 'utf8', stdio: ['ignore', 'ignore', 'pipe'] });
    return '0';
  } catch (error) {
    return String(error.stderr ?? '').trim();
  }
}

function imageSize(path) {
  const raw = magick(['identify', '-format', '%w %h', path]);
  const [width, height] = raw.trim().split(/\s+/).map(Number);
  return { width, height };
}

async function diffAgainstCanonical(canonical, candidate) {
  const stem = basename(candidate, '.png');
  const overlay = join(outDir, `${stem}-canonical-overlay.png`);
  const pixelDiff = join(outDir, `${stem}-canonical-pixel-diff.png`);
  const statsPath = join(outDir, `${stem}-canonical-diff-stats.json`);
  const canonicalSize = imageSize(canonical);
  const candidateSize = imageSize(candidate);
  if (canonicalSize.width !== candidateSize.width || canonicalSize.height !== candidateSize.height) {
    throw new Error(`size mismatch for ${candidate}: canonical=${canonicalSize.width}x${canonicalSize.height} candidate=${candidateSize.width}x${candidateSize.height}`);
  }
  magick([canonical, candidate, '-alpha', 'set', '-compose', 'blend', '-define', 'compose:args=50,50', '-composite', overlay]);
  try {
    execFileSync('magick', ['compare', '-metric', 'AE', canonical, candidate, pixelDiff], { encoding: 'utf8', stdio: ['ignore', 'ignore', 'pipe'] });
  } catch {
    // compare exits 1 when images differ; the diff file is still the desired artifact.
  }
  const stats = {
    canonical,
    candidate,
    width: candidateSize.width,
    height: candidateSize.height,
    pixels: candidateSize.width * candidateSize.height,
    changed_pixels: Number(metric('AE', canonical, candidate).split(/\s+/)[0] || 0),
    mae: metric('MAE', canonical, candidate),
    rmse: metric('RMSE', canonical, candidate),
    overlay,
    pixel_diff: pixelDiff,
  };
  await writeFile(statsPath, `${JSON.stringify(stats, null, 2)}\n`, 'utf8');
  return stats;
}

async function screenshot(page, canonical, name, manifest) {
  const path = join(outDir, `${name}.png`);
  await page.screenshot({ path, fullPage: false });
  const stats = await diffAgainstCanonical(canonical, path);
  manifest.screenshots.push({ state: name, screenshot: path, overlay: stats.overlay, pixel_diff: stats.pixel_diff, stats: join(outDir, `${name}-canonical-diff-stats.json`) });
}

async function assertNoHorizontalOverflow(page, label, width, manifest) {
  const metrics = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    url: location.href,
  }));
  manifest.overflow.push({ label, width, ...metrics });
  if (metrics.clientWidth !== metrics.scrollWidth) {
    throw new Error(`${label} horizontal overflow at ${width}: clientWidth=${metrics.clientWidth} scrollWidth=${metrics.scrollWidth}`);
  }
  return metrics;
}

async function optionByValue(page, testId, value) {
  return page.locator(`[data-testid="${testId}-option"][data-value="${value}"]`);
}

async function selectRAMRole(page, roleId) {
  await page.getByTestId('team-role-drawer-ram-roles-trigger').click();
  await expect(await optionByValue(page, 'team-role-drawer-ram-roles', roleId)).toBeVisible();
  await (await optionByValue(page, 'team-role-drawer-ram-roles', roleId)).click();
  await page.mouse.click(24, 24);
  await expect(page.getByTestId('team-role-drawer-ram-roles-options')).toBeHidden();
}

async function selectPermission(page, permission) {
  await page.getByTestId('team-ram-role-permissions').getByRole('button', { name: new RegExp(permission) }).click();
}

async function dismissNotice(page) {
  const close = page.getByLabel('Dismiss notification');
  if (await close.isVisible().catch(() => false)) {
    await close.click();
    await expect(page.getByTestId('team-role-notice')).toBeHidden();
  }
}

async function main() {
  await mkdir(outDir, { recursive: true });
  const canonical = await canonicalPath();
  const tempDir = await mkdtemp(join(tmpdir(), 'agent-center-t1457-'));
  const grpcPort = await freePort();
  const webPort = await freePort();
  const dbPath = join(tempDir, 'agent-center.db');
  const sockPath = join(tempDir, 'admin.sock');
  const masterKeyPath = join(tempDir, 'master.key');
  const configPath = join(tempDir, 'config.yaml');
  const head = git(['rev-parse', 'HEAD']);
  const branch = git(['rev-parse', '--abbrev-ref', 'HEAD']);
  const manifest = {
    head,
    branch,
    canonical_sha256: canonicalSHA,
    canonical,
    baseURL: '',
    orgSlug: '',
    systemVersion: null,
    checkedStates: [],
    screenshots: [],
    browser: { console: [], failedRequests: [], badResponses: [] },
    apiChecks: [],
    overflow: [],
    seeded: {},
  };

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
  proc.stdout.on('data', () => {});
  const baseURL = `http://127.0.0.1:${webPort}`;
  manifest.baseURL = baseURL;
  try {
    const deadline = Date.now() + 15_000;
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
    manifest.orgSlug = orgSlug;
    const orgApi = `${baseURL}/api/orgs/${orgSlug}`;
    const authed = await playwrightRequest.newContext({ extraHTTPHeaders: { Cookie: `ac_session=${sessionCookie}` } });
    manifest.systemVersion = await api(authed, 'get', `${baseURL}/api/system/version`);
    if (manifest.systemVersion.commit !== head.slice(0, 8)) {
      throw new Error(`running binary commit ${manifest.systemVersion.commit} does not match HEAD ${head}`);
    }

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
        value: { key: 'gpt-5', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], default_parameters: {}, enabled: true },
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
    const developerMapping = await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, {
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
    manifest.seeded = {
      teamId: team.id,
      developerRoleId: developerRole.id,
      reviewerRoleId: reviewerRole.id,
      opsRoleId: opsRole.id,
      initialDeveloperMappingVersion: developerMapping.version,
    };

    const browser = await chromium.launch();
    const context = await browser.newContext({ viewport: { width: 1672, height: 941 }, deviceScaleFactor: 1, colorScheme: 'light' });
    await context.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    page.on('console', (msg) => {
      if (['error', 'warning'].includes(msg.type())) manifest.browser.console.push({ type: msg.type(), text: msg.text() });
    });
    page.on('requestfailed', (request) => manifest.browser.failedRequests.push({ url: request.url(), failure: request.failure()?.errorText ?? 'unknown' }));
    page.on('response', (response) => {
      if (response.status() >= 400) manifest.browser.badResponses.push({ url: response.url(), status: response.status() });
    });

    await page.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    await expect(page.getByTestId('team-role-list')).toContainText('Platform Team');
    await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Developer Work Executor');
    await expect(page.getByTestId(`team-ram-role-delete-blocked-${developerRole.id}`)).toBeVisible();
    await assertNoHorizontalOverflow(page, 'main-1672', 1672, manifest);
    await screenshot(page, canonical, '01-roles-list-detail-work-config-ram-mapping', manifest);
    manifest.checkedStates.push('role list', 'role detail entries', 'work config', 'RAM mapping table', 'delete safeguard');

    await page.setViewportSize({ width: 1280, height: 941 });
    await assertNoHorizontalOverflow(page, 'main-1280', 1280, manifest);
    await page.screenshot({ path: join(outDir, 'fresh-1280-overflow-main.png'), fullPage: false });
    await page.setViewportSize({ width: 1672, height: 941 });

    await page.getByTestId(`team-role-detail-${team.id}-developer`).click();
    await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
    await expect(page.getByTestId('team-role-work-config')).toContainText('gpt-5');
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('CAS v2');
    await screenshot(page, canonical, '02-role-detail-mapping-drawer-work-config-cas', manifest);
    manifest.checkedStates.push('role detail drawer', 'mapping drawer', 'CAS version read');

    await selectRAMRole(page, opsRole.id);
    await page.getByRole('button', { name: 'Preview impact' }).click();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Added / removed');
    await screenshot(page, canonical, '03-mapping-preview-immediate-impact', manifest);
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, {
      ram_role_ids: [developerRole.id],
      expected_version: 2,
    });
    await page.getByRole('button', { name: 'Apply mapping' }).click();
    await expect(page.getByTestId('team-role-mapping-confirm')).toBeVisible();
    await screenshot(page, canonical, '04-mapping-confirm-versioned-write', manifest);
    await page.getByRole('button', { name: 'Apply now' }).click();
    await expect(page.getByTestId('team-role-mapping-error')).toBeVisible();
    await expect(page.getByTestId('team-role-mapping-error')).toContainText(/version|conflict|changed/i);
    await screenshot(page, canonical, '05-mapping-cas-error', manifest);
    manifest.checkedStates.push('mapping preview', 'mapping confirm', 'mapping CAS error');

    await page.getByRole('button', { name: /Refresh server version/i }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('CAS conflict');
    await page.getByLabel('Close drawer').click();
    await expect(page.getByTestId('team-role-mapping-drawer')).toBeHidden();
    await dismissNotice(page);

    await page.getByTestId('team-roles-create-ram-role').click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await screenshot(page, canonical, '06-ram-role-create-drawer-permissions-audit', manifest);
    await page.getByTestId('team-ram-role-name').fill('Temporary Evidence Operator');
    await page.getByTestId('team-ram-role-stable-key').fill(`team.temp.operator.${suffix}`);
    await page.getByTestId('team-ram-role-description').fill('Temporary role created through the real browser CRUD path.');
    await selectPermission(page, 'team.read');
    await page.getByRole('button', { name: 'Create', exact: true }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Created RAM Role');
    await screenshot(page, canonical, '07-ram-role-created-crud-success', manifest);
    await dismissNotice(page);
    const rolesAfterCreate = await api(authed, 'get', `${orgApi}/access/ram-roles`);
    const tempRole = rolesAfterCreate.roles.find((role) =>
      role.stable_key === `team.temp.operator.${suffix}` || role.name === 'Temporary Evidence Operator');
    if (!tempRole) throw new Error(`created RAM Role was not readable from API: ${JSON.stringify(rolesAfterCreate.roles.map((role) => ({ id: role.id, name: role.name, stable_key: role.stable_key })), null, 2)}`);
    manifest.apiChecks.push({ check: 'create RAM Role visible after server readback', roleId: tempRole.id, version: tempRole.version });

    await page.getByTestId(`team-ram-role-${tempRole.id}`).getByRole('button', { name: 'Edit' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await page.getByTestId('team-ram-role-description').fill('Edited metadata through the real browser path.');
    await screenshot(page, canonical, '08-ram-role-edit-drawer-version-controls', manifest);
    await page.getByRole('button', { name: 'Save metadata' }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Saved RAM Role');
    await dismissNotice(page);
    const tempDetail = await api(authed, 'get', `${orgApi}/access/ram-roles/${tempRole.id}`);
    manifest.apiChecks.push({ check: 'edit RAM Role readback', roleId: tempRole.id, version: tempDetail.latest.version, description: tempDetail.latest.description });

    await page.getByTestId(`team-ram-role-${tempRole.id}`).getByRole('button', { name: 'Duplicate' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await screenshot(page, canonical, '09-ram-role-duplicate-drawer', manifest);
    await page.getByRole('button', { name: 'Create', exact: true }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Created RAM Role');
    await dismissNotice(page);
    manifest.checkedStates.push('RAM Role create', 'RAM Role edit', 'RAM Role duplicate');

    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByRole('dialog')).toContainText('Delete RAM Role');
    await screenshot(page, canonical, '10-delete-safeguard-confirm-blocked', manifest);
    await page.getByRole('button', { name: 'Blocked' }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('still referenced');
    await screenshot(page, canonical, '11-delete-safeguard-notice', manifest);
    await dismissNotice(page);
    manifest.checkedStates.push('delete safeguard blocked modal', 'delete safeguard notice');

    await page.getByTestId(`team-ram-role-${tempRole.id}`).getByRole('button', { name: 'Edit' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await api(authed, 'post', `${orgApi}/access/ram-roles/${tempRole.id}/versions`, {
      expected_latest_version: 2,
      name: 'Temporary Evidence Operator',
      stable_key: `team.temp.operator.${suffix}`,
      description: 'Server-side version bump to force browser CAS error.',
      scope: 'team',
      permissions: ['team.read', 'team.memory.review'],
    });
    await page.getByTestId('team-ram-role-description').fill('Stale browser edit should conflict.');
    await page.getByRole('button', { name: 'Save metadata' }).click();
    await expect(page.getByTestId('team-ram-role-error')).toBeVisible();
    await screenshot(page, canonical, '12-ram-role-cas-error', manifest);
    manifest.checkedStates.push('RAM Role CAS error');

    const staleResponses = manifest.browser.badResponses.filter((item) => item.status >= 500 || (item.status >= 400 && item.status !== 409));
    if (staleResponses.length > 0) {
      throw new Error(`unexpected browser HTTP errors: ${JSON.stringify(staleResponses, null, 2)}`);
    }
    const consoleErrors = manifest.browser.console.filter((item) =>
      item.type === 'error' && !/status of 409|409 \(Conflict\)/i.test(item.text));
    if (consoleErrors.length > 0) {
      throw new Error(`browser console errors: ${JSON.stringify(consoleErrors, null, 2)}`);
    }

    await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify(manifest, null, 2)}\n`, 'utf8');
    await browser.close();
    await authed.dispose();
    await setup.dispose();
  } finally {
    proc.kill('SIGTERM');
    await new Promise((resolveWait) => setTimeout(resolveWait, 500));
    await rm(tempDir, { recursive: true, force: true });
  }
  if (stderr.length) await writeFile(join(outDir, 'server-stderr.log'), Buffer.concat(stderr).toString('utf8'), 'utf8');
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
