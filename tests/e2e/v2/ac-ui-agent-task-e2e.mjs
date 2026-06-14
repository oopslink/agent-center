// Pure UI-driven end-to-end: create an agent + dispatch a task, ALL through the
// Web Console (no API calls). Sign in → Add Agent (Eve) → Start her → New Task in
// the project → assign to Eve (this dispatches) → watch the task reach completed
// and Eve's reply render, all in the UI.
import { chromium } from '@playwright/test';
import { mkdirSync } from 'node:fs';

const BASE = 'http://127.0.0.1:7180';
const SLUG = 'e2e';
const PROJ = process.env.PROJ || 'project-77523a62';
const OUT = '/tmp/e2e-ui-agent-task';
mkdirSync(OUT, { recursive: true });
const AGENT = process.env.AGENT_NAME || 'Frank';

const errs = [];
const browser = await chromium.launch({ headless: true });
const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
page.on('console', (m) => m.type() === 'error' && errs.push(m.text().slice(0, 200)));
page.on('pageerror', (e) => errs.push('PAGEERR ' + e.message.slice(0, 200)));
const shot = async (n) => { await page.waitForTimeout(700); await page.screenshot({ path: `${OUT}/${n}.png`, fullPage: true }); console.log('shot', n, '@', page.url()); };
const tid = (t) => page.locator(`[data-testid="${t}"]`);

// 0. sign in
await page.goto(`${BASE}/signin`, { waitUntil: 'domcontentloaded' });
await page.fill('#display_name', 'E2E Owner');
await page.fill('#passcode', 'E2EPass1!');
await page.click('button[type=submit]');
await page.waitForTimeout(2500);

// 1. CREATE AGENT via UI
await page.goto(`${BASE}/organizations/${SLUG}/agents`, { waitUntil: 'domcontentloaded' });
await tid('agents-add-btn').waitFor({ state: 'visible', timeout: 15000 });
await tid('agents-add-btn').click();
await tid('agent-create-modal').waitFor({ state: 'visible' });
await tid('agent-create-name').fill(AGENT);
// worker EntitySelect: open → pick the (only) worker option
await tid('agent-create-worker-trigger').click();
await page.waitForTimeout(400);
await tid('agent-create-worker-option').first().click();
await shot('1-agent-create-filled');
await tid('agent-create-submit').click();
await page.waitForTimeout(1500);
// verify Eve row exists
const eveRow = page.locator('[data-testid="agent-row"]', { hasText: AGENT });
await eveRow.first().waitFor({ state: 'visible', timeout: 10000 });
console.log('agent created in UI:', AGENT, '| rows now:', await page.locator('[data-testid="agent-row"]').count());
await shot('2-agent-created');

// 2. START AGENT via UI (open Eve's detail → Start button)
await eveRow.first().locator('a:has-text("Open")').click();
await page.waitForTimeout(1500);
await tid('agent-start-btn').waitFor({ state: 'visible', timeout: 10000 });
await tid('agent-start-btn').click();
await page.waitForTimeout(3000);
const lcText = await tid('page-AgentDetail').innerText().catch(() => '');
console.log('after start, lifecycle visible RUNNING?', /running/i.test(lcText));
await shot('3-agent-started');

// 3. CREATE TASK via UI (project → Tasks tab → New Task)
await page.goto(`${BASE}/organizations/${SLUG}/projects/${PROJ}`, { waitUntil: 'domcontentloaded' });
await tid('project-tab-tasks').waitFor({ state: 'visible', timeout: 10000 });
await tid('project-tab-tasks').click();
await page.waitForTimeout(800);
await tid('project-task-create-btn').click();
await tid('task-create-modal').waitFor({ state: 'visible' });
await tid('task-create-title').fill(`UI E2E ${AGENT}: report hostname`);
await tid('task-create-description').fill('Run the shell command `hostname` and reply with its exact output in one short sentence.');
await shot('4-task-create-filled');
await tid('task-create-submit').click();
await page.waitForTimeout(1500);
const taskRow = page.locator('[data-testid="task-row"]', { hasText: `UI E2E ${AGENT}` });
await taskRow.first().waitFor({ state: 'visible', timeout: 10000 });
console.log('task created in UI; opening it');
await shot('5-task-created');

// 4. ASSIGN to Eve via UI (open task → Edit → assignee select → save) → dispatch
await taskRow.first().locator('a').first().click();
await page.waitForTimeout(1500);
await tid('task-edit-button').waitFor({ state: 'visible', timeout: 10000 });
await tid('task-edit-button').click();
await tid('task-edit-modal').waitFor({ state: 'visible' });
// pick the option whose text contains the agent name
const optValue = await page.locator('[data-testid="task-edit-assignee"] option', { hasText: AGENT }).first().getAttribute('value');
console.log('assignee option value for', AGENT, '=', optValue);
await tid('task-edit-assignee').selectOption(optValue);
await shot('6-task-assign-eve');
await tid('task-edit-submit').click();
await page.waitForTimeout(2000);
await shot('7-task-assigned');

// 5. WAIT for the task to reach completed + Eve's reply to render — in the UI
let done = false;
for (let i = 0; i < 50; i++) {
  await page.waitForTimeout(3000);
  await page.reload({ waitUntil: 'domcontentloaded' }).catch(() => {});
  await page.waitForTimeout(1200);
  const body = await page.locator('body').innerText().catch(() => '');
  if (/\b(completed|done)\b/i.test(body) && /hostname|orbstack|linux/i.test(body)) { done = true; break; }
  if (i % 4 === 0) console.log(`waiting for completion… ${i * 4}s`);
}
await shot('8-task-completed');
const finalText = await page.locator('body').innerText().catch(() => '');
const tail = finalText.split('\n').filter((l) => l.trim()).slice(-30).join('\n');
console.log('\n=== task detail tail (UI) ===\n' + tail);
console.log('\nTASK COMPLETED (rendered in UI):', done);
console.log('CONSOLE/PAGE ERRORS:', errs.length, errs.slice(0, 8));
await browser.close();
