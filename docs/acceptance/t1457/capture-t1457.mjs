import playwright from '../../../tests/e2e/v2/node_modules/@playwright/test/index.js';
import { spawn, execFileSync } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { existsSync, readFileSync } from 'node:fs';
import { chmod, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const repoRoot = resolve(new URL('../../..', import.meta.url).pathname);
const outDir = process.env.T1457_OUTDIR ? resolve(process.env.T1457_OUTDIR) : resolve(repoRoot, 'docs/acceptance/t1457');
const binary = resolve(repoRoot, 'bin/agent-center');
const canonicalPath = process.env.T1457_CANONICAL
  || '/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png';
const canonicalSha = '80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56';
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

function git(args) {
  return execFileSync('git', args, { cwd: repoRoot, encoding: 'utf8' }).trim();
}

function sha256(path) {
  return createHash('sha256').update(readFileSync(path)).digest('hex');
}

async function api(ctx, method, url, data, ok = [200, 201, 204]) {
  const response = await ctx[method](url, data === undefined ? undefined : { data });
  const text = await response.text();
  if (!ok.includes(response.status())) throw new Error(`${method.toUpperCase()} ${url} -> ${response.status()} ${text}`);
  if (response.status() === 204 || text === '') return null;
  return JSON.parse(text);
}

function makeDiff(candidate, state) {
  const statsPath = join(outDir, `${state}-canonical-diff-stats.json`);
  const overlayPath = join(outDir, `${state}-canonical-overlay.png`);
  const diffPath = join(outDir, `${state}-canonical-pixel-diff.png`);
  const py = `
import json, math, sys
from PIL import Image, ImageChops
canonical, candidate, overlay, diff, stats = sys.argv[1:]
c = Image.open(canonical).convert('RGB')
i = Image.open(candidate).convert('RGB')
if c.size != i.size:
    i = i.resize(c.size)
o = Image.blend(c, i, 0.5)
o.save(overlay)
d = ImageChops.difference(c, i)
d.save(diff)
width, height = c.size
changed = 0
abs_sum = 0
sq_sum = 0
max_delta = 0
for pr, pg, pb in d.getdata():
    if pr or pg or pb:
        changed += 1
    for v in (pr, pg, pb):
        abs_sum += v
        sq_sum += v * v
        if v > max_delta:
            max_delta = v
pixels = width * height
channels = pixels * 3
payload = {
    "state": ${JSON.stringify(state)},
    "canonical": canonical,
    "candidate": candidate,
    "overlay": overlay,
    "diff": diff,
    "width": width,
    "height": height,
    "pixels": pixels,
    "changed_pixels": changed,
    "changed_ratio": changed / pixels,
    "mae_rgb": abs_sum / channels,
    "rmse_rgb": math.sqrt(sq_sum / channels),
    "max_channel_delta": max_delta,
}
open(stats, 'w').write(json.dumps(payload, indent=2) + '\\n')
`;
  execFileSync('python3', ['-c', py, canonicalPath, candidate, overlayPath, diffPath, statsPath], { cwd: repoRoot, stdio: 'inherit' });
  return JSON.parse(readFileSync(statsPath, 'utf8'));
}

async function startServer() {
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
  proc.stdout.on('data', () => {});
  const baseURL = `http://127.0.0.1:${webPort}`;
  const deadline = Date.now() + 12000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseURL}/api/health`);
      if (response.ok) return { proc, tempDir, baseURL, stderr };
    } catch {
      await new Promise((resolveWait) => setTimeout(resolveWait, 100));
    }
  }
  throw new Error(`server did not start; stderr=${Buffer.concat(stderr).toString('utf8')}`);
}

async function seed(baseURL) {
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
  await api(authed, 'post', `${orgApi}/ai-runtime/models`, {
    expected_revision: runtimeRevision,
    value: { key: 'gpt-5', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], default_parameters: {}, enabled: true },
  });

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

  return { setup, authed, orgSlug, orgApi, sessionCookie, team, roles: { developerRole, reviewerRole, opsRole } };
}

async function main() {
  await mkdir(outDir, { recursive: true });
  if (!existsSync(binary)) throw new Error(`missing binary ${binary}; run make build-backend first`);
  if (!existsSync(canonicalPath)) throw new Error(`missing canonical ${canonicalPath}`);
  const actualCanonicalSha = sha256(canonicalPath);
  if (actualCanonicalSha !== canonicalSha) throw new Error(`canonical sha mismatch: ${actualCanonicalSha}`);

  const currentMain = git(['rev-parse', 'origin/main']);
  const head = git(['rev-parse', 'HEAD']);
  const branch = git(['rev-parse', '--abbrev-ref', 'HEAD']);
  const mergeBaseMain = git(['merge-base', 'HEAD', currentMain]);
  const mergeBaseRejected = git(['merge-base', 'HEAD', 'ddba9b10816b803b0563e97de574ebe7378c8ef2']);
  const server = await startServer();
  const seedState = await seed(server.baseURL);
  const stateStats = [];
  const browser = await chromium.launch();
  const consoleEntries = [];
  const failedRequests = [];
  const responses = [];

  async function newAuthedContext(width, height) {
    const context = await browser.newContext({ viewport: { width, height }, deviceScaleFactor: 1, colorScheme: 'light' });
    await context.addCookies([{ name: 'ac_session', value: seedState.sessionCookie, url: server.baseURL, httpOnly: true, sameSite: 'Lax' }]);
    return context;
  }

  async function capture(page, state, testId) {
    if (testId) await expect(page.getByTestId(testId)).toBeVisible();
    await page.waitForTimeout(250);
    const metrics = await page.evaluate(() => ({
      url: location.href,
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
      bodyText: document.body.innerText.slice(0, 2000),
    }));
    if (metrics.clientWidth !== metrics.scrollWidth) {
      throw new Error(`${state} horizontal overflow: clientWidth=${metrics.clientWidth} scrollWidth=${metrics.scrollWidth}`);
    }
    const screenshot = join(outDir, `${state}-candidate-1672x941.png`);
    await page.screenshot({ path: screenshot, fullPage: false });
    const stats = makeDiff(screenshot, state);
    stateStats.push({ ...stats, url: metrics.url, clientWidth: metrics.clientWidth, scrollWidth: metrics.scrollWidth });
  }

  const context = await newAuthedContext(1672, 941);
  context.on('page', (page) => {
    page.on('console', (msg) => consoleEntries.push({ type: msg.type(), text: msg.text(), url: page.url() }));
    page.on('requestfailed', (request) => failedRequests.push({ url: request.url(), method: request.method(), failure: request.failure()?.errorText }));
    page.on('response', (response) => {
      if (response.status() >= 400) responses.push({ url: response.url(), status: response.status(), method: response.request().method() });
    });
  });
  const page = await context.newPage();
  await page.goto(`${server.baseURL}/organizations/${seedState.orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
  await expect(page.getByTestId('team-role-list')).toContainText('Platform Team');
  await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Developer Work Executor');
  await capture(page, '01-role-list-detail-work-config-ram-mapping', 'team-role-list');

  await page.getByTestId(`team-role-edit-mapping-${seedState.team.id}-developer`).click();
  await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
  await expect(page.getByTestId('team-role-work-config')).toContainText('gpt-5');
  await expect(page.getByTestId('team-role-immediate-impact')).toContainText('CAS v2');
  await capture(page, '02-mapping-edit-drawer-cas', 'team-role-mapping-drawer');
  await page.getByLabel('Close drawer').click();

  await page.getByTestId('team-roles-create-ram-role').click();
  await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
  await expect(page.getByTestId('team-ram-role-audit')).toContainText('Versioned writes');
  await capture(page, '03-create-ram-role-drawer', 'team-ram-role-drawer');
  await page.getByLabel('Close drawer').click();

  await page.getByTestId(`team-ram-role-${seedState.roles.developerRole.id}`).getByRole('button', { name: 'Edit' }).click();
  await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
  await expect(page.getByTestId('team-ram-role-audit')).toContainText('Referenced by Platform Team/developer');
  await capture(page, '04-edit-ram-role-drawer-versioned', 'team-ram-role-drawer');
  await page.getByLabel('Close drawer').click();

  await page.getByTestId(`team-ram-role-${seedState.roles.developerRole.id}`).getByRole('button', { name: 'Duplicate' }).click();
  await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
  await expect(page.getByTestId('team-ram-role-name')).toHaveValue(/copy/);
  await capture(page, '05-duplicate-ram-role-drawer', 'team-ram-role-drawer');
  await page.getByLabel('Close drawer').click();

  await page.getByTestId(`team-ram-role-${seedState.roles.developerRole.id}`).getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByText('Delete RAM Role')).toBeVisible();
  await expect(page.getByText('This RAM Role is referenced. Delete is blocked until mappings are removed or migrated.')).toBeVisible();
  await capture(page, '06-delete-safeguard-modal', null);
  await page.getByRole('button', { name: 'Blocked' }).click();
  await expect(page.getByTestId('team-role-notice')).toContainText('still referenced');
  await capture(page, '07-delete-safeguard-notice', 'team-role-notice');

  await page.getByTestId(`team-role-edit-mapping-${seedState.team.id}-developer`).click();
  await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
  await api(seedState.authed, 'put', `${seedState.orgApi}/teams/${seedState.team.id}/roles/developer/ram-roles`, {
    ram_role_ids: [seedState.roles.developerRole.id],
    expected_version: 2,
  });
  await page.getByTestId('team-role-drawer-ram-roles-chip-remove').first().click();
  await page.getByRole('button', { name: 'Preview impact' }).click();
  await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Added / removed');
  await page.getByRole('button', { name: 'Apply mapping' }).click();
  await expect(page.getByTestId('team-role-mapping-confirm')).toBeVisible();
  await page.getByRole('button', { name: 'Apply now' }).click();
  await expect(page.getByTestId('team-role-mapping-error')).toContainText(/409|conflict|version/i);
  await capture(page, '08-cas-error-mapping', 'team-role-mapping-error');
  await page.getByLabel('Close drawer').click();

  const staleDelete = await seedState.authed.delete(`${seedState.orgApi}/access/ram-roles/${seedState.roles.developerRole.id}`, {
    data: { expected_latest_version: 0, confirm_unreferenced: true, reason: 'stale delete probe' },
  });
  const staleDeleteBody = await staleDelete.text();
  if (staleDelete.status() < 400) throw new Error(`expected stale delete error, got ${staleDelete.status()}`);
  await capture(page, '09-error-state-after-real-api-409', 'page-TeamsRoles');

  const overflowContext = await newAuthedContext(1280, 941);
  const overflowPage = await overflowContext.newPage();
  await overflowPage.goto(`${server.baseURL}/organizations/${seedState.orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
  await expect(overflowPage.getByTestId('page-TeamsRoles')).toBeVisible();
  const overflowMetrics = await overflowPage.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    url: location.href,
  }));
  if (overflowMetrics.clientWidth !== overflowMetrics.scrollWidth) {
    throw new Error(`1280 horizontal overflow: clientWidth=${overflowMetrics.clientWidth} scrollWidth=${overflowMetrics.scrollWidth}`);
  }
  await overflowPage.screenshot({ path: join(outDir, 'fresh-1280-overflow-candidate.png'), fullPage: false });
  await overflowContext.close();

  const versionCommit = (await (await fetch(`${server.baseURL}/version.commit`)).text()).trim();
  const systemVersion = await (await fetch(`${server.baseURL}/api/system/version`)).json();
  const state = {
    generated_at: new Date().toISOString(),
    current_main: currentMain,
    head,
    branch,
    merge_base_head_current_main: mergeBaseMain,
    merge_base_head_rejected_ddba9b10: mergeBaseRejected,
    baseURL: server.baseURL,
    orgSlug: seedState.orgSlug,
    version_commit: versionCommit,
    system_version: systemVersion,
    canonical: { path: canonicalPath, sha256: actualCanonicalSha },
    teamId: seedState.team.id,
    ramRoleIds: Object.fromEntries(Object.entries(seedState.roles).map(([key, role]) => [key, role.id])),
    overflow_1280: overflowMetrics,
    stale_delete_probe: { status: staleDelete.status(), body: staleDeleteBody.slice(0, 500) },
    states: stateStats,
    console: consoleEntries,
    failed_requests: failedRequests,
    error_responses: responses,
  };
  await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify(state, null, 2)}\n`, 'utf8');
  await writeFile(join(outDir, 'console-network.json'), `${JSON.stringify({ console: consoleEntries, failed_requests: failedRequests, error_responses: responses }, null, 2)}\n`, 'utf8');
  await browser.close();
  await context.close();
  await seedState.authed.dispose();
  await seedState.setup.dispose();

  if (process.env.T1457_KEEPALIVE === '1') {
    await writeFile(join(outDir, 'stable-instance.json'), `${JSON.stringify({ baseURL: server.baseURL, orgSlug: seedState.orgSlug, version_commit: versionCommit }, null, 2)}\n`, 'utf8');
    console.log(`T1457_KEEPALIVE baseURL=${server.baseURL} orgSlug=${seedState.orgSlug} version_commit=${versionCommit}`);
    await new Promise(() => {});
  }

  server.proc.kill('SIGTERM');
  await new Promise((resolveWait) => setTimeout(resolveWait, 500));
  await rm(server.tempDir, { recursive: true, force: true });
  if (server.stderr.length) process.stderr.write(Buffer.concat(server.stderr).toString('utf8'));
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
