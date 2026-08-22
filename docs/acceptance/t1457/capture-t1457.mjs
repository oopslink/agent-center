import playwright from '../../../tests/e2e/v2/node_modules/@playwright/test/index.js';
import { spawn, execFileSync } from 'node:child_process';
import { copyFile, chmod, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createHash, randomBytes } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const repoRoot = resolve(new URL('../../..', import.meta.url).pathname);
const outDir = resolve(repoRoot, 'docs/acceptance/t1457');
const binary = resolve(repoRoot, 'bin/agent-center');
const canonical = join(outDir, 'canonical-t1457-1672x941.png');
const { chromium, request: playwrightRequest, expect } = playwright;

const states = [
  ['01-role-list', 'Role list'],
  ['02-role-detail', 'Role detail'],
  ['03-create-drawer', 'Create RAM Role drawer'],
  ['04-edit-drawer', 'Edit RAM Role drawer'],
  ['05-work-config', 'Work config'],
  ['06-ram-mapping', 'RAM mapping'],
  ['07-version-duplicate-delete-safeguard', 'Version / duplicate / delete safeguard'],
  ['08-cas-error', 'CAS error'],
  ['09-api-error', 'API error'],
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

async function api(ctx, method, url, data, okStatuses = [200, 201, 204]) {
  const response = await ctx[method](url, data === undefined ? undefined : { data });
  if (!okStatuses.includes(response.status())) {
    throw new Error(`${method.toUpperCase()} ${url} -> ${response.status()} ${await response.text()}`);
  }
  return response.status() === 204 ? null : response.json();
}

async function waitForServer(baseURL) {
  const deadline = Date.now() + 12000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseURL}/api/health`);
      if (response.ok) return;
    } catch {
      await new Promise((resolveWait) => setTimeout(resolveWait, 100));
    }
  }
  throw new Error(`server did not become ready: ${baseURL}`);
}

async function sha256(path) {
  return createHash('sha256').update(await readFile(path)).digest('hex');
}

async function capture(page, slug) {
  const candidate = join(outDir, `${slug}-candidate-1672x941.png`);
  await page.screenshot({ path: candidate, fullPage: false });
  await diffAgainstCanonical(candidate, slug);
}

async function diffAgainstCanonical(candidate, slug) {
  const overlay = join(outDir, `${slug}-canonical-overlay-1672x941.png`);
  const diff = join(outDir, `${slug}-canonical-diff-1672x941.png`);
  const stats = join(outDir, `${slug}-canonical-diff-stats.json`);
  const py = String.raw`
from PIL import Image
import json, math, sys
canonical, candidate, overlay, diff, stats = sys.argv[1:]
ca = Image.open(canonical).convert("RGBA")
co = Image.open(candidate).convert("RGBA")
if ca.size != co.size:
    raise SystemExit(f"size mismatch: canonical={ca.size} candidate={co.size}")
w, h = ca.size
changed = 0
abs_sum = 0
sq_sum = 0
max_delta = 0
pix = Image.new("RGBA", ca.size)
for y in range(h):
    for x in range(w):
        c = ca.getpixel((x, y))
        p = co.getpixel((x, y))
        deltas = [abs(c[i] - p[i]) for i in range(3)]
        if any(deltas):
            changed += 1
            pix.putpixel((x, y), (255, 0, 0, 255))
        else:
            pix.putpixel((x, y), (255, 255, 255, 255))
        abs_sum += sum(deltas)
        sq_sum += sum(d * d for d in deltas)
        max_delta = max(max_delta, *deltas)
Image.blend(ca, co, 0.5).save(overlay)
pix.save(diff)
channels = w * h * 3
doc = {
    "canonical": canonical,
    "candidate": candidate,
    "overlay": overlay,
    "pixel_diff": diff,
    "width": w,
    "height": h,
    "total_pixels": w * h,
    "changed_pixels": changed,
    "changed_ratio": changed / (w * h),
    "mae_rgb": abs_sum / channels,
    "rmse_rgb": math.sqrt(sq_sum / channels),
    "max_abs_channel_delta": max_delta,
}
open(stats, "w", encoding="utf-8").write(json.dumps(doc, indent=2) + "\n")
`;
  execFileSync('python3', ['-c', py, canonical, candidate, overlay, diff, stats], { stdio: 'inherit' });
}

async function main() {
  await mkdir(outDir, { recursive: true });
  if (await sha256(canonical) !== '80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56') {
    throw new Error(`canonical SHA mismatch for ${canonical}`);
  }

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
  const consoleEvents = [];
  const networkEvents = [];
  const actions = [];
  let browser;

  try {
    await waitForServer(baseURL);
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
    const versionCommit = (await (await fetch(`${baseURL}/version.commit`)).text()).trim();
    const head = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: repoRoot }).toString().trim();
    if (version.commit !== versionCommit) throw new Error(`/version.commit ${versionCommit} != /api/system/version ${version.commit}`);

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
    const unreferencedRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'Temporary CRUD Role',
      stable_key: `team.temp.crud.${suffix}`,
      description: 'Unreferenced role used by browser CRUD verification.',
      scope: 'team',
      permissions: ['team.read'],
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

    browser = await chromium.launch();
    const context = await browser.newContext({
      viewport: { width: 1672, height: 941 },
      deviceScaleFactor: 1,
      colorScheme: 'light',
    });
    await context.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    page.on('console', (msg) => consoleEvents.push({ type: msg.type(), text: msg.text() }));
    page.on('requestfailed', (request) => networkEvents.push({ kind: 'requestfailed', method: request.method(), url: request.url(), failure: request.failure()?.errorText }));
    page.on('response', (response) => {
      if (response.status() >= 400) networkEvents.push({ kind: 'response', status: response.status(), url: response.url() });
    });

    await page.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    await expect(page.getByTestId('team-role-list')).toContainText('Platform Team');
    await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Developer Work Executor');
    await expect(page.getByTestId(`team-ram-role-delete-blocked-${developerRole.id}`)).toBeVisible();
    const mainMetrics = await page.evaluate(() => ({ clientWidth: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth, url: location.href }));
    if (mainMetrics.clientWidth !== mainMetrics.scrollWidth) throw new Error(`1672 horizontal overflow: ${JSON.stringify(mainMetrics)}`);
    await capture(page, '01-role-list');
    actions.push('Verified role list, RAM mapping table, delete safeguard, and 1672 overflow guard.');

    await page.getByTestId(`team-role-detail-${team.id}-developer`).click();
    await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
    await capture(page, '02-role-detail');
    await page.getByLabel('Close drawer').click();

    await page.getByTestId('team-roles-create-ram-role').click({ force: true });
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await expect(page.getByTestId('team-ram-role-audit')).toContainText('Versioned writes');
    await capture(page, '03-create-drawer');
    await page.getByTestId('team-ram-role-name').fill(`Browser Created ${suffix}`);
    await page.getByTestId('team-ram-role-stable-key').fill(`browser.created.${suffix}`);
    await page.getByTestId('team-ram-role-description').fill('Created through the browser evidence flow.');
    await page.getByTestId('team-ram-role-permissions').getByText('team.read').click();
    await page.getByTestId('team-ram-role-drawer').getByRole('button', { name: 'Create', exact: true }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Created RAM Role');
    actions.push('Browser CRUD create completed through the Team Roles drawer.');

    await page.getByTestId(`team-ram-role-${unreferencedRole.id}`).getByRole('button', { name: 'Edit' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await capture(page, '04-edit-drawer');
    await page.getByTestId('team-ram-role-description').fill('Edited through the browser evidence flow.');
    await page.getByRole('button', { name: 'Save metadata' }).click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Saved RAM Role');
    actions.push('Browser CRUD edit completed through the Team Roles drawer.');

    await page.getByTestId(`team-role-edit-mapping-${team.id}-developer`).click();
    await expect(page.getByTestId('team-role-work-config')).toContainText('codex');
    await capture(page, '05-work-config');
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('CAS v2');
    await capture(page, '06-ram-mapping');
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Duplicate' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toContainText('Duplicate RAM Role');
    await capture(page, '07-version-duplicate-delete-safeguard');
    await page.getByLabel('Close drawer').click();
    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByTestId('confirm-modal-message')).toContainText('Delete is blocked');
    await page.getByRole('button', { name: 'Cancel' }).click();
    actions.push('Browser duplicate drawer and referenced delete safeguard verified.');

    await page.getByTestId(`team-role-edit-mapping-${team.id}-reviewer`).click();
    await page.getByTestId('team-role-drawer-ram-roles-chip-remove').click();
    await page.getByRole('button', { name: 'Preview impact' }).click();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Added / removed');
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/reviewer/ram-roles`, { ram_role_ids: [reviewerRole.id, developerRole.id], expected_version: 2 });
    await page.getByRole('button', { name: 'Apply mapping' }).click();
    await expect(page.getByTestId('team-role-mapping-confirm')).toBeVisible();
    await page.getByRole('button', { name: 'Apply now' }).click();
    await expect(page.getByTestId('team-role-mapping-error')).toContainText('version');
    await capture(page, '08-cas-error');
    actions.push('Browser mapping CAS stale write produced a real 409 error state.');
    await page.getByLabel('Close drawer').click();
    await expect(page.getByTestId('team-role-mapping-drawer')).toBeHidden();
    if (await page.getByLabel('Dismiss notification').isVisible().catch(() => false)) {
      await page.getByLabel('Dismiss notification').click();
    }

    await page.getByTestId('team-roles-create-ram-role').click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await page.getByTestId('team-ram-role-name').fill('Duplicate Stable Key Error');
    await page.getByTestId('team-ram-role-stable-key').fill(`team.developer.executor.${suffix}`);
    await page.getByTestId('team-ram-role-description').fill('This intentionally conflicts with an existing stable key.');
    await page.getByTestId('team-ram-role-permissions').getByText('team.read').click();
    await page.getByTestId('team-ram-role-drawer').getByRole('button', { name: 'Create', exact: true }).click();
    await expect(page.getByTestId('team-ram-role-error')).toBeVisible();
    await capture(page, '09-api-error');
    actions.push('Browser API error state verified with a duplicate stable key create attempt.');
    await page.getByLabel('Close drawer').click();

    const deleteCard = page.getByTestId(`team-ram-role-${unreferencedRole.id}`);
    await deleteCard.getByRole('button', { name: 'Delete' }).click();
    await page.getByTestId('confirm-modal-confirm').click();
    await expect(page.getByTestId('team-role-notice')).toContainText('Deleted RAM Role');
    actions.push('Browser CRUD delete completed for an unreferenced RAM Role.');

    await page.setViewportSize({ width: 1280, height: 941 });
    await page.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    const overflow1280 = await page.evaluate(() => ({ clientWidth: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth, url: location.href }));
    if (overflow1280.clientWidth !== overflow1280.scrollWidth) throw new Error(`1280 horizontal overflow: ${JSON.stringify(overflow1280)}`);
    await page.screenshot({ path: join(outDir, '10-overflow-1280-candidate.png'), fullPage: false });

    const evidence = {
      baseURL,
      orgSlug,
      head,
      version,
      version_commit_endpoint: versionCommit,
      canonical: {
        path: canonical,
        sha256: await sha256(canonical),
      },
      viewport_1672: mainMetrics,
      viewport_1280: overflow1280,
      teamId: team.id,
      ramRoleIds: [developerRole.id, reviewerRole.id, opsRole.id, unreferencedRole.id],
      states: states.map(([slug, label]) => ({
        slug,
        label,
        candidate: `docs/acceptance/t1457/${slug}-candidate-1672x941.png`,
        overlay: `docs/acceptance/t1457/${slug}-canonical-overlay-1672x941.png`,
        pixel_diff: `docs/acceptance/t1457/${slug}-canonical-diff-1672x941.png`,
        stats: `docs/acceptance/t1457/${slug}-canonical-diff-stats.json`,
      })),
      browser_actions: actions,
      console_events: consoleEvents,
      network_events: networkEvents,
    };
    await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify(evidence, null, 2)}\n`, 'utf8');
    await writeFile(join(outDir, 'browser-verification.json'), `${JSON.stringify({ actions, consoleEvents, networkEvents }, null, 2)}\n`, 'utf8');
    await authed.dispose();
    await setup.dispose();
  } finally {
    if (browser) await browser.close();
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
