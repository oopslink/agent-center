// TRUE web-console-driven end-to-end: sign in, open a channel, TYPE a message in
// the UI composer, SEND via the UI, wait for the agent's reply to RENDER in the UI.
import { chromium } from '@playwright/test';
import { mkdirSync } from 'node:fs';

const BASE = 'http://127.0.0.1:7180';
const SLUG = 'e2e';
const CHAN = process.env.CHAN || 'channel-5e0b7ff1';
const OUT = '/tmp/e2e-ui-e2e';
mkdirSync(OUT, { recursive: true });

const errs = [];
const browser = await chromium.launch({ headless: true });
const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
page.on('console', (m) => m.type() === 'error' && errs.push(m.text().slice(0, 200)));
page.on('pageerror', (e) => errs.push('PAGEERR ' + e.message.slice(0, 200)));
const shot = async (n) => { await page.waitForTimeout(800); await page.screenshot({ path: `${OUT}/${n}.png`, fullPage: true }); console.log('shot', n); };

// 1. sign in THROUGH the UI
await page.goto(`${BASE}/signin`, { waitUntil: 'domcontentloaded' });
await page.fill('#display_name', 'E2E Owner');
await page.fill('#passcode', 'E2EPass1!');
await page.click('button[type=submit]');
await page.waitForTimeout(2500);

// 2. open the channel THROUGH the UI
await page.goto(`${BASE}/organizations/${SLUG}/channels/${CHAN}`, { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(1500);
const composer = page.locator('[data-testid="composer-textarea"]');
await composer.waitFor({ state: 'visible', timeout: 15000 });

// count agent message bubbles before (to detect a NEW reply)
const agentReplyText = (s) =>
  s.includes('42') || /carol/i.test(s); // 6*7=42, or self-identification
const beforeCount = await page.locator('text=/42/').count();
await shot('1-before-send');

// 3. TYPE the message in the UI composer + SEND via the UI button
const msg = '@Carol quick check: reply in ONE short sentence stating your own name and the result of 6 times 7.';
await composer.click();
await composer.fill(msg);
await page.waitForTimeout(400);
await page.keyboard.press('Escape'); // dismiss any @mention autocomplete popup
await page.waitForTimeout(200);
await page.locator('[data-testid="composer-send"]').click();
console.log('sent via UI composer:', msg);
await page.waitForTimeout(1500);
await shot('2-after-send');

// 4. wait for Carol's reply (containing 42) to RENDER in the UI — poll the DOM
let replied = false;
for (let i = 0; i < 45; i++) {
  await page.waitForTimeout(3000);
  const c = await page.locator('text=/42/').count();
  if (c > beforeCount) { replied = true; break; }
  if (i % 5 === 0) console.log(`waiting for reply… ${i * 3}s (matches=${c})`);
}
await shot('3-after-reply');

// 5. dump the rendered conversation text for proof
const bubbles = await page.locator('[data-testid="message-composer"]').count();
const bodyText = await page.locator('main, body').first().innerText().catch(() => '');
const tail = bodyText.split('\n').filter((l) => l.trim()).slice(-25).join('\n');
console.log('\n=== rendered conversation tail (UI) ===\n' + tail);
console.log('\nreplied(rendered in UI):', replied, '| composer present:', bubbles > 0);
console.log('CONSOLE/PAGE ERRORS:', errs.length, errs.slice(0, 8));
await browser.close();
