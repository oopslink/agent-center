// Focused capture for the F-3 / F-5 fix acceptance evidence.
import { chromium } from '@playwright/test';
import { mkdirSync } from 'node:fs';

const BASE = 'http://127.0.0.1:7180';
const SLUG = 'e2e';
const OUT = '/tmp/e2e-verify';
mkdirSync(OUT, { recursive: true });
const CHAN = process.env.IDCHAN || 'channel-5e0b7ff1';

const browser = await chromium.launch({ headless: true });
const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
const errs = [];
page.on('console', (m) => m.type() === 'error' && errs.push(m.text().slice(0, 200)));
page.on('pageerror', (e) => errs.push('PAGEERR ' + e.message.slice(0, 200)));

async function shot(n) { await page.waitForTimeout(1200); await page.screenshot({ path: `${OUT}/${n}.png`, fullPage: true }); console.log('shot', n); }

await page.goto(`${BASE}/signin`, { waitUntil: 'domcontentloaded' });
await page.fill('#display_name', 'E2E Owner');
await page.fill('#passcode', 'E2EPass1!');
await page.click('button[type=submit]');
await page.waitForTimeout(2500);

await page.goto(`${BASE}/organizations/${SLUG}/members/agents`, { waitUntil: 'domcontentloaded' });
await shot('A-agents-after-fix');

await page.goto(`${BASE}/organizations/${SLUG}/channels/${CHAN}`, { waitUntil: 'domcontentloaded' });
await shot('B-identity-check-channel');

console.log('CONSOLE/PAGE ERRORS:', errs.length, errs.slice(0, 10));
await browser.close();
