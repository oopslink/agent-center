import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { join } from 'node:path';

const root = new URL('.', import.meta.url).pathname;
const rawDir = join(root, 'raw');
const screenshotDir = join(root, 'screenshots');
const harDir = join(root, 'har');
const { chromium } = await import('/tmp/t2169-candidate/tests/e2e/v2/node_modules/@playwright/test/index.mjs');

async function main() {
  await mkdir(screenshotDir, { recursive: true });
  await mkdir(harDir, { recursive: true });
  const pack = JSON.parse(await readFile(join(rawDir, '01-install-with-seed.json'), 'utf8'));
  const signinBody = {
    display_name: pack.signin.display_name,
    passcode: pack.signin.passcode,
    org_slug: pack.signin.org_slug,
  };
  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 1440, height: 980 },
    deviceScaleFactor: 1,
    recordHar: { path: join(harDir, 'insight-hard-gates.har'), content: 'omit' },
  });
  const signin = await context.request.post(`${pack.web_url}/api/auth/signin`, { data: signinBody });
  if (!signin.ok()) {
    throw new Error(`signin failed ${signin.status()} ${await signin.text()}`);
  }
  const cookie = /ac_session=([^;]+)/.exec(signin.headers()['set-cookie'] || '')?.[1];
  if (!cookie) throw new Error('signin did not return ac_session');
  await context.addCookies([{ name: 'ac_session', value: cookie, domain: '127.0.0.1', path: '/', httpOnly: true, sameSite: 'Lax' }]);

  const page = await context.newPage();
  const consoleEvents = [];
  page.on('console', (msg) => consoleEvents.push({ type: msg.type(), text: msg.text() }));
  page.on('pageerror', (err) => consoleEvents.push({ type: 'pageerror', text: err.message }));
  await page.addInitScript(() => localStorage.setItem('ac.theme', 'light'));

  const slug = encodeURIComponent(pack.signin.org_slug);
  const routes = [
    ['01-overview', `/organizations/${slug}/insights/overview`, 'page-InsightOverview'],
    ['02-agents', `/organizations/${slug}/insights/agents`, 'page-InsightAgents'],
    ['03-projects', `/organizations/${slug}/insights/projects`, 'page-InsightProjects'],
    ['04-executions', `/organizations/${slug}/insights/executions?window=24h`, 'page-InsightExecutions'],
  ];
  const shots = [];
  for (const [name, path, testId] of routes) {
    await page.goto(`${pack.web_url}${path}`, { waitUntil: 'domcontentloaded' });
    await page.getByTestId(testId).waitFor({ timeout: 12000 });
    await page.waitForTimeout(800);
    const file = join(screenshotDir, `${name}.png`);
    await page.screenshot({ path: file, fullPage: true });
    shots.push({ name, url: page.url(), file });
  }
  await writeFile(join(rawDir, '23-ui-capture-summary.json'), JSON.stringify({ shots, consoleEvents }, null, 2));
  await context.close();
  await browser.close();
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
