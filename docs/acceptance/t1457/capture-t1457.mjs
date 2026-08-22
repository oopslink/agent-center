import playwright from '../../../tests/e2e/v2/node_modules/@playwright/test/index.js';
import { execFileSync, spawn } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { chmod, mkdir, mkdtemp, readFile, readdir, rm, unlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, join, resolve } from 'node:path';

const repoRoot = resolve(new URL('../../..', import.meta.url).pathname);
const outDir = resolve(repoRoot, 'docs/acceptance/t1457');
const binary = resolve(repoRoot, 'bin/agent-center');
const canonical = process.env.T1457_CANONICAL ||
  '/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png';
const canonicalSHA256 = '80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56';
const { chromium, request: playwrightRequest, expect } = playwright;

const states = [
  ['roles-list', 'Team Role list, detail entry, work config, RAM mapping table'],
  ['ram-role-detail', 'RAM Role versions, duplicate/delete controls, delete safeguard'],
  ['mapping-drawer', 'Team Role RAM mapping edit drawer'],
  ['mapping-preview', 'Mapping preview with affected members/projects and CAS source version'],
  ['mapping-cas-error', 'Mapping CAS conflict and inline refresh error state'],
  ['ram-role-create-drawer', 'Create RAM Role drawer with permission picker and audit guard'],
  ['ram-role-edit-drawer', 'Edit RAM Role drawer with versioned write controls'],
  ['ram-role-duplicate-drawer', 'Duplicate RAM Role drawer'],
  ['ram-role-delete-safeguard', 'Referenced RAM Role delete safeguard confirmation'],
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
  const text = await response.text();
  if (!okStatuses.includes(response.status())) {
    throw new Error(`${method.toUpperCase()} ${url} -> ${response.status()} ${text}`);
  }
  return text ? JSON.parse(text) : null;
}

async function cleanGeneratedEvidence() {
  await mkdir(outDir, { recursive: true });
  for (const file of await readdir(outDir)) {
    if (/^t1457-.*\.(png|json|log)$/.test(file) || ['capture-state.json', 'report.md'].includes(file)) {
      await unlink(join(outDir, file)).catch(() => undefined);
    }
  }
}

async function sha256(path) {
  return createHash('sha256').update(await readFile(path)).digest('hex');
}

function git(args) {
  return execFileSync('git', args, { cwd: repoRoot }).toString().trim();
}

function diffFor(stateName, candidatePath) {
  const overlayPath = join(outDir, `t1457-${stateName}-canonical-overlay.png`);
  const diffPath = join(outDir, `t1457-${stateName}-canonical-pixel-diff.png`);
  const stats = JSON.parse(execFileSync('python3', [
    join(outDir, 'image_diff.py'),
    canonical,
    candidatePath,
    overlayPath,
    diffPath,
  ], { cwd: repoRoot }).toString());
  return {
    ...stats,
    candidate: `docs/acceptance/t1457/${basename(candidatePath)}`,
    overlay: `docs/acceptance/t1457/${basename(overlayPath)}`,
    pixel_diff: `docs/acceptance/t1457/${basename(diffPath)}`,
  };
}

async function capture(page, stateName, description) {
  await page.waitForLoadState('networkidle').catch(() => undefined);
  const metrics = await page.evaluate(() => ({
    url: location.href,
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    clientHeight: document.documentElement.clientHeight,
    scrollHeight: document.documentElement.scrollHeight,
  }));
  if (metrics.clientWidth !== metrics.scrollWidth) {
    throw new Error(`${stateName} horizontal overflow: clientWidth=${metrics.clientWidth} scrollWidth=${metrics.scrollWidth}`);
  }
  const candidate = join(outDir, `t1457-${stateName}-candidate-1672x941.png`);
  await page.screenshot({ path: candidate, fullPage: false });
  return { state: stateName, description, metrics, candidate: `docs/acceptance/t1457/${basename(candidate)}`, diff: diffFor(stateName, candidate) };
}

async function writeReport(report) {
  const rows = report.states.map((state) =>
    `| ${state.state} | ${state.candidate} | ${state.diff.overlay} | ${state.diff.pixel_diff} | ${state.diff.changed_pixels} | ${state.diff.changed_ratio.toFixed(6)} |`,
  ).join('\n');
  const text = `# T1457 Team Roles / RAM Role Mapping Evidence

- Git parent at task start: \`${report.git.parent}\`.
- Final candidate HEAD at capture time: \`${report.git.head}\`.
- Runtime /api/system/version.commit: \`${report.version.commit}\`.
- Canonical attachment SHA256: \`${canonicalSHA256}\`.
- Canonical file used: \`${canonical}\`.
- Canonical file verified SHA256: \`${report.canonical.sha256}\`.
- Fresh browser instance: \`${report.baseURL}\` (isolated local server, torn down after capture).
- Stable external preview: \`${report.stableURL ?? 'BLOCKED: no deploy/Sites/shared-preview credential or mechanism is available in this isolated executor'}\`.
- 1280 overflow check: clientWidth=\`${report.overflow1280.clientWidth}\`, scrollWidth=\`${report.overflow1280.scrollWidth}\`, url=\`${report.overflow1280.url}\`.
- Console events captured: \`${report.console.count}\`; network events captured: \`${report.network.count}\`.

## State Matrix

| State | Candidate | Overlay | Pixel diff | Changed px | Changed ratio |
| --- | --- | --- | --- | ---: | ---: |
${rows}

## Browser/API Coverage

- Public signup API created a fresh owner session and organization.
- Public org APIs seeded AI runtime, three custom RAM Roles, Team Roles, and Team Role RAM mappings.
- Chromium verified role list, detail, create/edit/duplicate drawers, mapping preview/save path, CAS conflict, delete safeguard, version/error surfaces, console/network capture, and exact runtime SHA.
- Mapping CAS raw result: \`${report.api.mappingConflict.status}\` \`${report.api.mappingConflict.body}\`.
- RAM Role CAS raw result: \`${report.api.ramRoleConflict.status}\` \`${report.api.ramRoleConflict.body}\`.
- CRUD raw result: created \`${report.api.createdRoleId}\`, edited to version \`${report.api.editedRoleVersion}\`, deleted status \`${report.api.deleteStatus}\`.

## Raw Evidence Files

- \`docs/acceptance/t1457/capture-state.json\`
- \`docs/acceptance/t1457/t1457-console.log\`
- \`docs/acceptance/t1457/t1457-network.log\`
`;
  await writeFile(join(outDir, 'report.md'), text, 'utf8');
}

async function main() {
  await cleanGeneratedEvidence();
  const canonicalActual = await sha256(canonical);
  if (canonicalActual !== canonicalSHA256) {
    throw new Error(`canonical SHA mismatch: expected ${canonicalSHA256} got ${canonicalActual}`);
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
  const stdout = [];
  proc.stderr.on('data', (chunk) => stderr.push(chunk));
  proc.stdout.on('data', (chunk) => stdout.push(chunk));
  const baseURL = `http://127.0.0.1:${webPort}`;
  let browser;
  const consoleEvents = [];
  const networkEvents = [];
  try {
    const deadline = Date.now() + 10000;
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

    const head = git(['rev-parse', 'HEAD']);
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
    if (version.commit !== head) {
      throw new Error(`runtime commit mismatch: version.commit=${version.commit} head=${head}`);
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
    const crudRole = await api(authed, 'post', `${orgApi}/access/ram-roles`, {
      name: 'T1457 CRUD Role',
      stable_key: `t1457.crud.${suffix}`,
      description: 'Browser CRUD proof role.',
      scope: 'team',
      permissions: ['team.read'],
    });
    const crudEdited = await api(authed, 'post', `${orgApi}/access/ram-roles/${crudRole.id}/versions`, {
      name: 'T1457 CRUD Role',
      stable_key: crudRole.stable_key,
      description: 'Browser CRUD proof role edited.',
      scope: 'team',
      permissions: ['team.read', 'team.memory.review'],
      expected_latest_version: crudRole.latest.version,
    });
    const crudDeleteResp = await authed.delete(`${orgApi}/access/ram-roles/${crudRole.id}`, {
      data: { expected_latest_version: crudEdited.latest.version, confirm_unreferenced: true, reason: 'T1457 browser CRUD proof' },
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

    const mappingConflict = await authed.put(`${orgApi}/teams/${team.id}/roles/ops/ram-roles`, {
      data: { ram_role_ids: [opsRole.id], expected_version: 1 },
    });
    const ramRoleConflict = await authed.post(`${orgApi}/access/ram-roles/${opsRole.id}/versions`, {
      data: {
        name: opsRole.name,
        stable_key: opsRole.stable_key,
        description: 'Stale version write proof.',
        scope: opsRole.scope,
        permissions: opsRole.latest.permissions,
        expected_latest_version: 0,
      },
    });

    browser = await chromium.launch();
    const context = await browser.newContext({
      viewport: { width: 1672, height: 941 },
      deviceScaleFactor: 1,
      colorScheme: 'light',
    });
    await context.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    page.on('console', (msg) => consoleEvents.push({ type: msg.type(), text: msg.text(), location: msg.location() }));
    page.on('requestfinished', async (request) => {
      const response = await request.response();
      networkEvents.push({ method: request.method(), url: request.url(), status: response?.status() ?? null });
    });
    page.on('requestfailed', (request) => networkEvents.push({ method: request.method(), url: request.url(), failure: request.failure()?.errorText ?? 'failed' }));

    const captures = [];
    await page.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    await expect(page.getByTestId('team-role-list')).toContainText('Platform Team');
    await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Developer Work Executor');
    captures.push(await capture(page, states[0][0], states[0][1]));

    await expect(page.getByTestId(`team-ram-role-delete-blocked-${developerRole.id}`)).toBeVisible();
    captures.push(await capture(page, states[1][0], states[1][1]));

    await page.getByTestId(`team-role-edit-mapping-${team.id}-developer`).click();
    await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('CAS v2');
    captures.push(await capture(page, states[2][0], states[2][1]));
    await page.getByTestId('team-role-drawer-ram-roles-trigger').click();
    await page.locator(`[data-testid="team-role-drawer-ram-roles-option"][data-value="${opsRole.id}"]`).click();
    await page.getByTestId('team-role-drawer-ram-roles-search').press('Escape');
    await page.getByTestId('team-role-drawer-ram-roles-options').waitFor({ state: 'hidden', timeout: 3000 });
    await page.getByRole('button', { name: 'Preview impact' }).click();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('Added / removed');
    captures.push(await capture(page, states[3][0], states[3][1]));
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, {
      ram_role_ids: [developerRole.id, reviewerRole.id],
      expected_version: 2,
    });
    await page.getByRole('button', { name: 'Apply mapping' }).click();
    await expect(page.getByTestId('team-role-mapping-confirm')).toBeVisible();
    await page.getByRole('button', { name: 'Apply now' }).click();
    await expect(page.getByTestId('team-role-mapping-error')).toContainText('409');
    captures.push(await capture(page, states[4][0], states[4][1]));
    await page.getByLabel('Close drawer').click();

    await page.getByTestId('team-roles-create-ram-role').click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await expect(page.getByTestId('team-ram-role-audit')).toContainText('Versioned writes');
    captures.push(await capture(page, states[5][0], states[5][1]));
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${opsRole.id}`).getByRole('button', { name: 'Edit' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toContainText('Edit RAM Role');
    await expect(page.getByRole('button', { name: 'Create version' })).toBeVisible();
    captures.push(await capture(page, states[6][0], states[6][1]));
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${opsRole.id}`).getByRole('button', { name: 'Duplicate' }).click();
    await expect(page.getByTestId('team-ram-role-drawer')).toContainText('Duplicate RAM Role');
    captures.push(await capture(page, states[7][0], states[7][1]));
    await page.getByLabel('Close drawer').click();

    await page.getByTestId(`team-ram-role-${developerRole.id}`).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByTestId('confirm-modal')).toContainText('Delete is blocked');
    captures.push(await capture(page, states[8][0], states[8][1]));

    const context1280 = await browser.newContext({
      viewport: { width: 1280, height: 720 },
      deviceScaleFactor: 1,
      colorScheme: 'light',
    });
    await context1280.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const page1280 = await context1280.newPage();
    await page1280.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'networkidle' });
    const overflow1280 = await page1280.evaluate(() => ({
      url: location.href,
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
    }));
    if (overflow1280.clientWidth !== overflow1280.scrollWidth) {
      throw new Error(`1280 horizontal overflow: clientWidth=${overflow1280.clientWidth} scrollWidth=${overflow1280.scrollWidth}`);
    }
    await page1280.screenshot({ path: join(outDir, 't1457-1280-overflow-candidate.png'), fullPage: false });
    await context1280.close();

    const report = {
      git: { parent: git(['rev-parse', 'origin/main']), head, branch: git(['branch', '--show-current']) },
      baseURL,
      stableURL: process.env.T1457_STABLE_URL || null,
      canonical: { path: canonical, sha256: canonicalActual },
      version,
      orgSlug,
      teamId: team.id,
      ramRoleIds: [developerRole.id, reviewerRole.id, opsRole.id],
      states: captures,
      overflow1280,
      console: { count: consoleEvents.length, events: consoleEvents },
      network: { count: networkEvents.length, events: networkEvents },
      api: {
        createdRoleId: crudRole.id,
        editedRoleVersion: crudEdited.latest.version,
        deleteStatus: crudDeleteResp.status(),
        mappingConflict: { status: mappingConflict.status(), body: await mappingConflict.text() },
        ramRoleConflict: { status: ramRoleConflict.status(), body: await ramRoleConflict.text() },
      },
    };
    await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify(report, null, 2)}\n`, 'utf8');
    await writeFile(join(outDir, 't1457-console.log'), consoleEvents.map((event) => JSON.stringify(event)).join('\n') + '\n', 'utf8');
    await writeFile(join(outDir, 't1457-network.log'), networkEvents.map((event) => JSON.stringify(event)).join('\n') + '\n', 'utf8');
    await writeReport(report);
    await browser.close();
    browser = null;
    await authed.dispose();
    await setup.dispose();
  } finally {
    if (browser) await browser.close().catch(() => undefined);
    proc.kill('SIGTERM');
    await new Promise((resolveWait) => setTimeout(resolveWait, 500));
    if (!proc.killed) proc.kill('SIGKILL');
    await writeFile(join(outDir, 't1457-server-stdout.log'), Buffer.concat(stdout).toString('utf8'), 'utf8').catch(() => undefined);
    await writeFile(join(outDir, 't1457-server-stderr.log'), Buffer.concat(stderr).toString('utf8'), 'utf8').catch(() => undefined);
    await rm(tempDir, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
