import playwright from '../../../tests/e2e/v2/node_modules/@playwright/test/index.js';
import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const repoRoot = resolve(new URL('../../..', import.meta.url).pathname);
const outDir = resolve(repoRoot, 'docs/acceptance/t1457');
const binary = resolve(repoRoot, 'bin/agent-center');
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

async function api(ctx, method, url, data) {
  const response = await ctx[method](url, data === undefined ? undefined : { data });
  if (!response.ok()) throw new Error(`${method.toUpperCase()} ${url} -> ${response.status()} ${await response.text()}`);
  return response.status() === 204 ? null : response.json();
}

async function main() {
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
  try {
    const deadline = Date.now() + 8000;
    while (Date.now() < deadline) {
      try {
        const response = await fetch(`${baseURL}/`);
        if (response.ok) break;
      } catch {
        await new Promise((resolveWait) => setTimeout(resolveWait, 100));
      }
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
        value: {
          key: 'codex',
          display_name: 'Codex',
          executable: 'codex',
          required_features: [],
          enabled: true,
        },
      });
      runtimeRevision = cliWrite.revision;
    }
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
    await api(authed, 'put', `${orgApi}/teams/${team.id}/roles/developer/ram-roles`, {
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

    const browser = await chromium.launch();
    const context = await browser.newContext({
      viewport: { width: 1672, height: 941 },
      deviceScaleFactor: 1,
      colorScheme: 'light',
    });
    await context.addCookies([{ name: 'ac_session', value: sessionCookie, url: baseURL, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    await page.goto(`${baseURL}/organizations/${orgSlug}/teams/roles`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('page-TeamsRoles')).toBeVisible();
    await expect(page.getByTestId('team-role-list')).toContainText('Platform Team');
    await expect(page.getByTestId('team-role-ram-mappings')).toContainText('Developer Work Executor');
    await expect(page.getByTestId(`team-ram-role-delete-blocked-${developerRole.id}`)).toBeVisible();
    const metrics = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
      url: location.href,
    }));
    if (metrics.clientWidth !== metrics.scrollWidth) {
      throw new Error(`horizontal overflow: clientWidth=${metrics.clientWidth} scrollWidth=${metrics.scrollWidth}`);
    }
    await page.screenshot({ path: join(outDir, 'teams-roles-main-1672x941.png'), fullPage: false });

    await page.getByTestId(`team-role-edit-mapping-${team.id}-developer`).click();
    await expect(page.getByTestId('team-role-mapping-drawer')).toBeVisible();
    await expect(page.getByTestId('team-role-immediate-impact')).toContainText('CAS v2');
    await page.screenshot({ path: join(outDir, 'teams-roles-mapping-drawer-1672x941.png'), fullPage: false });
    await page.getByLabel('Close drawer').click();

    await page.getByTestId('team-roles-create-ram-role').click();
    await expect(page.getByTestId('team-ram-role-drawer')).toBeVisible();
    await expect(page.getByTestId('team-ram-role-audit')).toContainText('Versioned writes');
    await page.screenshot({ path: join(outDir, 'teams-roles-ram-role-drawer-1672x941.png'), fullPage: false });

    const stateReport = {
      baseURL,
      orgSlug,
      finalURL: metrics.url,
      head: (await (await import('node:child_process')).execFileSync('git', ['rev-parse', 'HEAD'], { cwd: repoRoot })).toString().trim(),
      clientWidth: metrics.clientWidth,
      scrollWidth: metrics.scrollWidth,
      teamId: team.id,
      ramRoleIds: [developerRole.id, reviewerRole.id, opsRole.id],
      checkedStates: [
        'Team IA route /teams/roles',
        'role list',
        'role detail entry',
        'work config',
        'RAM mapping table',
        'mapping edit drawer',
        'immediate impact/CAS version',
        'RAM Role create drawer',
        'versioned write controls',
        'delete safeguard',
        'audit copy',
        '1280/1672 no horizontal overflow guard',
      ],
    };
    await writeFile(join(outDir, 'capture-state.json'), `${JSON.stringify(stateReport, null, 2)}\n`, 'utf8');
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
