import playwright from '../../../tests/e2e/v2/node_modules/@playwright/test/index.js';
import { spawn, execFileSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { chmod, cp, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const repoRoot = resolve(new URL('../../..', import.meta.url).pathname);
const outDir = resolve(repoRoot, 'docs/acceptance/t1457');
const canonical = '/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png';
const binary = resolve(repoRoot, 'bin/agent-center');
const { chromium, request: playwrightRequest, expect } = playwright;
const candidateSHA = process.env.CANDIDATE_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { cwd: repoRoot }).toString().trim();
const canonicalSHA = '80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56';

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

async function api(ctx, method, url, data, okStatuses = []) {
  const response = await ctx[method](url, data === undefined ? undefined : { data });
  if (!response.ok() && !okStatuses.includes(response.status())) {
    throw new Error(`${method.toUpperCase()} ${url} -> ${response.status()} ${await response.text()}`);
  }
  if (response.status() === 204) return null;
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

async function screenshotState(page, name, checks) {
  await checks();
  await assertNoHorizontalOverflow(page, name);
  const path = join(outDir, `${name}-candidate-1672x941.png`);
  await page.screenshot({ path, fullPage: false });
  await diffAgainstCanonical(name, path);
  return path;
}

async function assertNoHorizontalOverflow(page, name) {
  const metrics = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    bodyScrollWidth: document.body.scrollWidth,
    url: location.href,
  }));
  if (metrics.scrollWidth > metrics.clientWidth || metrics.bodyScrollWidth > metrics.clientWidth) {
    throw new Error(`${name} horizontal overflow: ${JSON.stringify(metrics)}`);
  }
  return metrics;
}

async function diffAgainstCanonical(name, candidate) {
  const script = `
import json, math, sys
from pathlib import Path
from PIL import Image, ImageChops
canonical, candidate, overlay, diff, stats = map(Path, sys.argv[1:])
a = Image.open(canonical).convert('RGB')
b = Image.open(candidate).convert('RGB')
if a.size != b.size:
    raise SystemExit(f'size mismatch: canonical={a.size} candidate={b.size}')
d = ImageChops.difference(a, b)
changed = 0
sq = 0
abs_sum = 0
max_delta = 0
for px in d.getdata():
    if px != (0, 0, 0):
        changed += 1
    abs_sum += px[0] + px[1] + px[2]
    sq += px[0] * px[0] + px[1] * px[1] + px[2] * px[2]
    max_delta = max(max_delta, px[0], px[1], px[2])
pixel_count = a.size[0] * a.size[1]
Image.blend(a, b, 0.5).save(overlay)
d.save(diff)
stats.write_text(json.dumps({
    'canonical': str(canonical),
    'candidate': str(candidate),
    'canonical_sha256': '${canonicalSHA}',
    'width': a.size[0],
    'height': a.size[1],
    'pixel_count': pixel_count,
    'changed_pixels': changed,
    'changed_ratio': changed / pixel_count,
    'mae_rgb_channel': abs_sum / (pixel_count * 3),
    'rmse_rgb_channel': math.sqrt(sq / (pixel_count * 3)),
    'max_abs_channel_delta': max_delta,
}, indent=2) + '\\n')
`;
  execFileSync('python3', [
    '-c',
    script,
    canonical,
    candidate,
    join(outDir, `${name}-canonical-overlay.png`),
    join(outDir, `${name}-canonical-pixel-diff.png`),
    join(outDir, `${name}-canonical-diff-stats.json`),
  ], { cwd: repoRoot, stdio: 'inherit' });
}

async function clickPermission(page, permissionKey) {
  await page.getByTestId('team-ram-role-permissions').getByText(permissionKey, { exact: true }).click();
}

async function dismissNotice(page) {
  const notice = page.getByTestId('team-role-notice');
  if (await notice.isVisible().catch(() => false)) {
    await page.getByLabel('Dismiss notification').click();
    await expect(notice).toHaveCount(0);
  }
}

async function main() {
  await mkdir(outDir, { recursive: true });
  const actualCanonicalSHA = execFileSync('shasum', ['-a', '256', canonical]).toString().trim().split(/\s+/)[0];
  if (actualCanonicalSHA !== canonicalSHA) throw new Error(`canonical sha mismatch: ${actualCanonicalSHA}`);
  await cp(canonical, join(outDir, 't1457-canonical.png'));

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
  const stdout = [];
  const stderr = [];
  proc.stdout.on('data', (chunk) => stdout.push(chunk));
  proc.stderr.on('data', (chunk) => stderr.push(chunk));
  const baseURL = `http://127.0.0.1:${webPort}`;
  let browser;
  try {
    const deadline = Date.now() + 12000;
    while (Date.now() < deadline) {
      try {
        const response = await fetch(`${baseURL}/api/system/version`);
        if (response.ok) break;
      } catch {
        await new Promise((resolveWait) => setTimeout(resolveWait, 100));
      }
    }

    const systemVersion = await (await fetch(`${baseURL}/api/system/version`)).json();
    if (systemVersion.commit !== candidateSHA) {
      throw new Error(`/api/system/version.commit=${systemVersion.commit} does not equal candidate ${candidateSHA}`);
    }

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
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, { ram_role_ids: [developerRole.id, reviewerRole.id], expected_version: 1 });
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/reviewer/ram-roles`, { ram_role_ids: [reviewerRole.id], expected_version: 1 });
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/ops/ram-roles`, { ram_role_ids: [opsRole.id], expected_version: 1 });

    const behavior = {
      runtime_sha_exact: systemVersion.commit === candidateSHA,
      crud: {},
      mapping: {},
      cas: {},
      error: {},
    };
    const staleMapping = await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, { ram_role_ids: [developerRole.id], expected_version: 1 }, [409]);
    behavior.cas.api_stale_mapping_409 = staleMapping?.error === 'version_conflict';
    const badPreview = await api(authed, 'post', `${orgApi}/teams/${team.id}/roles/developer/ram-roles/preview`, { ram_role_ids: ['role-does-not-exist'] }, [422]);
    behavior.error.api_bad_mapping_422 = badPreview?.error === 'invalid_ram_role';

    browser = await chromium.launch();
    const context = await browser.newContext({ viewport: { width: 1672, height: 941 }, deviceScaleFactor: 1, colorScheme: 'light' });
    await context.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const consoleEvents = [];
    const networkFailures = [];
    const page = await context.newPage();
    page.on('console', (msg) => {
      if (['error', 'warning'].includes(msg.type())) consoleEvents.push({ type: msg.type(), text: msg.text() });
    });
    page.on('requestfailed', (request) => networkFailures.push({ url: request.url(), failure: request.failure()?.errorText ?? 'unknown' }));
    page.on('response', (response) => {
      if (response.status() >= 500) networkFailures.push({ url: response.url(), status: response.status() });
    });

    await page.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    await expect(page.getByTestId('team-role-list')).toContainText('Platform Team');
    await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Developer Work Executor');

    const states = [];
    states.push(await screenshotState(page, '01-role-list', async () => {
      await expect(page.getByTestId('team-role-list')).toContainText('developer');
      await expect(page.getByTestId('team-role-list')).toContainText('reviewer');
    }));
    await page.getByTestId(`team-role-detail-${team.id}-reviewer`).click();
    states.push(await screenshotState(page, '02-role-detail', async () => {
      await expect(page.getByTestId('team-role-selected-detail')).toContainText('Platform Team / reviewer');
    }));
    states.push(await screenshotState(page, '03-work-config', async () => {
      await expect(page.getByTestId('team-role-selected-work-config')).toContainText('codex');
      await expect(page.getByTestId('team-role-selected-work-config')).toContainText('gpt-5');
      await expect(page.getByTestId('team-role-selected-work-config')).toContainText('2');
    }));
    states.push(await screenshotState(page, '04-ram-mapping-table', async () => {
      await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Platform Ops Steward');
    }));

    await page.getByTestId('team-role-selected-edit-mapping').click();
    states.push(await screenshotState(page, '05-edit-mapping-drawer', async () => {
      await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
      await expect(page.getByTestId('team-role-immediate-impact')).toContainText('CAS v2');
    }));
    await page.getByTestId('team-role-drawer-ram-roles-trigger').click();
    await page.locator('[data-testid="team-role-drawer-ram-roles-option"]').filter({ hasText: 'Platform Ops Steward' }).click();
    await page.mouse.click(100, 100);
    await expect(page.locator('[data-testid="team-role-drawer-ram-roles-option"]')).toHaveCount(0);
    await page.getByRole('button', { name: 'Preview impact' }).click();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Added / removed');
    await page.getByRole('button', { name: 'Apply mapping' }).click();
    await expect(page.getByTestId('team-role-mapping-confirm')).toBeVisible();
    await page.getByRole('button', { name: 'Apply now' }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Applied');
    behavior.mapping.browser_preview_apply = true;
    await dismissNotice(page);

    await page.getByTestId('team-roles-create-ram-role').click();
    states.push(await screenshotState(page, '06-create-ram-role-drawer', async () => {
      await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
      await expect(page.getByTestId('team-ram-role-audit')).toContainText('Versioned writes');
    }));
    await page.getByTestId('team-ram-role-name').fill('Browser Created Role');
    await page.getByTestId('team-ram-role-stable-key').fill(`browser.created.${suffix}`);
    await page.getByTestId('team-ram-role-description').fill('Created by T1457 browser CRUD verification.');
    await clickPermission(page, 'team.read');
    await page.getByRole('button', { name: 'Create', exact: true }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Created RAM Role Browser Created Role');
    behavior.crud.browser_create = true;
    await dismissNotice(page);

    await page.locator('[data-testid^="team-ram-role-"]').filter({ hasText: 'Browser Created Role' }).getByRole('button', { name: 'Edit' }).click();
    states.push(await screenshotState(page, '07-edit-version-drawer', async () => {
      await expect(page.getByTestId('team-ram-role-drawer')).toContainText('Edit RAM Role');
      await expect(page.getByRole('button', { name: 'Create version' })).toBeVisible();
    }));
    await clickPermission(page, 'project.read');
    await page.getByRole('button', { name: 'Create version' }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Created v2');
    behavior.crud.browser_new_version = true;
    await dismissNotice(page);

    await page.locator('[data-testid^="team-ram-role-"]').filter({ hasText: 'Browser Created Role' }).getByRole('button', { name: 'Duplicate' }).click();
    states.push(await screenshotState(page, '08-duplicate-delete-safeguard', async () => {
      await expect(page.getByTestId('team-ram-role-drawer')).toContainText('Duplicate RAM Role');
      await expect(page.getByTestId('team-ram-role-audit')).toContainText('No Team Role references');
      await expect(page.getByTestId(`team-ram-role-delete-blocked-${reviewerRole.id}`)).toBeVisible();
    }));
    await page.getByLabel('Close drawer').click();
    await page.locator('[data-testid^="team-ram-role-"]').filter({ hasText: 'Browser Created Role' }).getByRole('button', { name: 'Delete' }).click();
    await page.getByTestId('confirm-modal-confirm').click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Deleted RAM Role Browser Created Role');
    behavior.crud.browser_delete = true;
    await dismissNotice(page);
    await page.getByTestId(`team-ram-role-${reviewerRole.id}`).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByTestId('confirm-modal-message')).toContainText('Delete is blocked');
    await page.getByRole('button', { name: 'Blocked' }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('still referenced');
    behavior.crud.browser_delete_safeguard = true;

    await page.getByTestId(`team-role-edit-mapping-${team.id}-developer`).click();
    await page.getByTestId('team-role-drawer-ram-roles-trigger').click();
    const selectedOption = page.locator('[data-testid="team-role-drawer-ram-roles-option"][aria-selected="true"]').first();
    await selectedOption.click();
    await page.mouse.click(100, 100);
    await expect(page.locator('[data-testid="team-role-drawer-ram-roles-option"]')).toHaveCount(0);
    await page.getByRole('button', { name: 'Preview impact' }).click();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Members');
    const developerMappingBeforeConflict = await api(authed, 'get', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`);
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, {
      ram_role_ids: [developerRole.id, reviewerRole.id],
      expected_version: developerMappingBeforeConflict.version,
    });
    await page.getByRole('button', { name: 'Apply mapping' }).click();
    await page.getByRole('button', { name: 'Apply now' }).click();
    states.push(await screenshotState(page, '09-cas-error', async () => {
      await expect(page.getByTestId('team-role-mapping-error')).toContainText('version');
      await expect(page.getByRole('button', { name: 'Refresh server version' })).toBeVisible();
    }));
    behavior.cas.browser_conflict_error = true;

    const page1280 = await context.newPage();
    await page1280.setViewportSize({ width: 1280, height: 941 });
    await page1280.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
    await expect(page1280.getByTestId('page-TeamsRoles')).toBeVisible();
    const overflow1280 = await assertNoHorizontalOverflow(page1280, '1280-overflow');
    await page1280.screenshot({ path: join(outDir, '1280-overflow-candidate.png'), fullPage: false });

    const audit = {
      candidate_sha: candidateSHA,
      runtime_version: systemVersion,
      runtime_sha_exact: systemVersion.commit === candidateSHA,
      canonical_sha256: canonicalSHA,
      baseURL,
      orgSlug,
      finalURL: page.url(),
      state_count: states.length,
      states: [
        '01-role-list',
        '02-role-detail',
        '03-work-config',
        '04-ram-mapping-table',
        '05-edit-mapping-drawer',
        '06-create-ram-role-drawer',
        '07-edit-version-drawer',
        '08-duplicate-delete-safeguard',
        '09-cas-error',
      ],
      screenshots: states.map((path) => path.replace(`${repoRoot}/`, '')),
      overflow1280,
      behavior,
      console_events: consoleEvents,
      network_failures: networkFailures,
      teamId: team.id,
      ramRoleIds: [developerRole.id, reviewerRole.id, opsRole.id],
    };
    await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify(audit, null, 2)}\n`, 'utf8');
    await writeFile(join(outDir, 'browser-audit.json'), `${JSON.stringify({ console_events: consoleEvents, network_failures: networkFailures }, null, 2)}\n`, 'utf8');
    await authed.dispose();
    await setup.dispose();
  } finally {
    if (browser) await browser.close();
    proc.kill('SIGTERM');
    await new Promise((resolveWait) => setTimeout(resolveWait, 500));
    if (!proc.killed) proc.kill('SIGKILL');
    await writeFile(join(outDir, 'server-stdout.log'), Buffer.concat(stdout).toString('utf8'), 'utf8');
    await writeFile(join(outDir, 'server-stderr.log'), Buffer.concat(stderr).toString('utf8'), 'utf8');
    await rm(tempDir, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
