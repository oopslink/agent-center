// Standalone UI walkthrough for E2E bug/UX hunting.
// Drives the real Web Console (127.0.0.1:7180) with the installed chromium,
// captures a screenshot + console/page errors per step.
import { chromium } from '@playwright/test';
import { mkdirSync } from 'node:fs';

const BASE = process.env.AC_BASE || 'http://127.0.0.1:7180';
const SLUG = process.env.AC_SLUG || 'e2e';
const NAME = process.env.AC_NAME || 'E2E Owner';
const PASS = process.env.AC_PASS || 'E2EPass1!';
const OUT = '/tmp/e2e-shots';
mkdirSync(OUT, { recursive: true });

const consoleErrors = [];
const pageErrors = [];
const reqFailures = [];

function attach(page) {
  page.on('console', (m) => {
    if (m.type() === 'error') consoleErrors.push(`[console] ${m.text()}`.slice(0, 300));
  });
  page.on('pageerror', (e) => pageErrors.push(`[pageerror] ${e.message}`.slice(0, 300)));
  page.on('requestfailed', (r) => {
    const u = r.url();
    if (u.includes('/api/')) reqFailures.push(`[reqfail] ${r.failure()?.errorText} ${u}`.slice(0, 200));
  });
  page.on('response', (r) => {
    const u = r.url();
    if (u.includes('/api/') && r.status() >= 400) reqFailures.push(`[http ${r.status()}] ${u}`.slice(0, 200));
  });
}

async function shot(page, name) {
  await page.waitForTimeout(900);
  await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: true }).catch(() => {});
  console.log(`shot: ${name}  url=${page.url()}`);
}

const browser = await chromium.launch({ headless: true });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
attach(page);

try {
  // 1. signin
  await page.goto(`${BASE}/signin`, { waitUntil: 'domcontentloaded' });
  await shot(page, '01-signin');
  await page.fill('#display_name', NAME);
  await page.fill('#passcode', PASS);
  await page.click('button[type=submit]');
  await page.waitForTimeout(2000);
  await shot(page, '02-after-signin');

  const steps = [
    ['03-org-home', `${BASE}/organizations/${SLUG}`],
    ['04-fleet', `${BASE}/organizations/${SLUG}/fleet`],
    ['05-agents', `${BASE}/organizations/${SLUG}/members/agents`],
    ['06-projects', `${BASE}/organizations/${SLUG}/projects`],
    ['07-tasks', `${BASE}/organizations/${SLUG}/tasks`],
    ['08-channels', `${BASE}/organizations/${SLUG}/channels`],
    ['09-environment', `${BASE}/organizations/${SLUG}/environment`],
    ['10-issues', `${BASE}/organizations/${SLUG}/issues`],
  ];
  for (const [name, url] of steps) {
    await page.goto(url, { waitUntil: 'domcontentloaded' }).catch((e) => console.log(`nav fail ${url}: ${e.message}`));
    await shot(page, name);
  }

  // Deep links: click into first agent, first project, the channel.
  await page.goto(`${BASE}/organizations/${SLUG}/members/agents`, { waitUntil: 'domcontentloaded' });
  const agentLink = page.locator('a[href*="/agents/agent-"]').first();
  if (await agentLink.count()) {
    await agentLink.click();
    await page.waitForTimeout(1500);
    await shot(page, '11-agent-detail');
  }
  await page.goto(`${BASE}/organizations/${SLUG}/projects`, { waitUntil: 'domcontentloaded' });
  const projLink = page.locator('a[href*="/projects/project-"]').first();
  if (await projLink.count()) {
    await projLink.click();
    await page.waitForTimeout(1500);
    await shot(page, '12-project-detail');
  }
  await page.goto(`${BASE}/organizations/${SLUG}/channels`, { waitUntil: 'domcontentloaded' });
  const chanLink = page.locator('a[href*="/channels/channel-"]').first();
  if (await chanLink.count()) {
    await chanLink.click();
    await page.waitForTimeout(1500);
    await shot(page, '13-channel-detail');
  }
} catch (e) {
  console.log('WALKTHROUGH ERROR:', e.message);
} finally {
  console.log('\n=== CONSOLE ERRORS (' + consoleErrors.length + ') ===');
  [...new Set(consoleErrors)].slice(0, 40).forEach((e) => console.log(e));
  console.log('\n=== PAGE ERRORS (' + pageErrors.length + ') ===');
  [...new Set(pageErrors)].slice(0, 40).forEach((e) => console.log(e));
  console.log('\n=== API FAILURES (' + reqFailures.length + ') ===');
  [...new Set(reqFailures)].slice(0, 40).forEach((e) => console.log(e));
  await browser.close();
}
