#!/usr/bin/env node
import http from 'node:http';
import { createReadStream, existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { extname, join, resolve } from 'node:path';
import { spawn } from 'node:child_process';

const root = resolve(new URL('../prototype', import.meta.url).pathname);
const evidenceRoot = resolve(new URL('.', import.meta.url).pathname);
const rawDir = join(evidenceRoot, 'raw');
const shotDir = join(evidenceRoot, 'screenshots');
mkdirSync(rawDir, { recursive: true });
mkdirSync(shotDir, { recursive: true });

const chrome = process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const port = Number(process.env.PORT || 4187);
const cdpPort = Number(process.env.CDP_PORT || 9227);
const scales = [100, 500, 2200];
const modes = ['baseline', 'prototype'];
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' };

const server = http.createServer((req, res) => {
  const url = new URL(req.url || '/', `http://127.0.0.1:${port}`);
  const file = join(root, url.pathname === '/' ? 'index.html' : url.pathname);
  if (!file.startsWith(root) || !existsSync(file)) {
    res.writeHead(404);
    res.end('not found');
    return;
  }
  res.writeHead(200, { 'content-type': mime[extname(file)] || 'application/octet-stream' });
  createReadStream(file).pipe(res);
});

class CDP {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      if (msg.id && this.pending.has(msg.id)) {
        const { resolve: ok, reject } = this.pending.get(msg.id);
        this.pending.delete(msg.id);
        msg.error ? reject(new Error(JSON.stringify(msg.error))) : ok(msg.result);
      }
    };
  }
  send(method, params = {}) {
    const id = ++this.id;
    this.ws.send(JSON.stringify({ id, method, params }));
    return new Promise((resolvePromise, reject) => this.pending.set(id, { resolve: resolvePromise, reject }));
  }
}

function wait(ms) {
  return new Promise((resolvePromise) => setTimeout(resolvePromise, ms));
}

async function connect() {
  for (let i = 0; i < 80; i++) {
    try {
      const tabs = await fetch(`http://127.0.0.1:${cdpPort}/json`).then((r) => r.json());
      const page = tabs.find((tab) => tab.type === 'page') || tabs[0];
      if (page?.webSocketDebuggerUrl) {
        const ws = new WebSocket(page.webSocketDebuggerUrl);
        await new Promise((resolvePromise, reject) => {
          ws.onopen = resolvePromise;
          ws.onerror = reject;
        });
        return new CDP(ws);
      }
    } catch {
      await wait(250);
    }
  }
  throw new Error('Chrome DevTools endpoint did not become ready');
}

async function evalJs(cdp, expression) {
  const result = await cdp.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
  if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
  return result.result.value;
}

async function measure(cdp, label, fn) {
  const start = performance.now();
  await fn();
  await wait(80);
  const metrics = await evalJs(cdp, 'window.collaborationPrototype.metrics()');
  return { label, wall_ms: Number((performance.now() - start).toFixed(2)), metrics };
}

async function runOne(cdp, mode, scale) {
  const url = `http://127.0.0.1:${port}/index.html?mode=${mode}&view=network&scale=${scale}`;
  await cdp.send('Page.navigate', { url });
  await cdp.send('Page.loadEventFired').catch(() => {});
  await wait(500);
  await evalJs(cdp, 'window.collaborationPrototype.redraw()');
  const timeline = [];
  timeline.push(await measure(cdp, 'tti', async () => {}));
  timeline.push(await measure(cdp, 'idle-render', async () => {
    await evalJs(cdp, 'Promise.all(Array.from({length: 12}, () => window.collaborationPrototype.redraw()))');
  }));
  timeline.push(await measure(cdp, 'continuous-pan', async () => {
    await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', x: 720, y: 420, button: 'left', buttons: 1 });
    for (let i = 0; i < 18; i++) await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: 720 + i * 6, y: 420 + i * 2, button: 'left', buttons: 1 });
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: 828, y: 456, button: 'left', buttons: 0 });
  }));
  timeline.push(await measure(cdp, 'wheel-zoom', async () => {
    for (let i = 0; i < 8; i++) await cdp.send('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 760, y: 390, deltaX: 0, deltaY: i % 2 ? 140 : -140 });
  }));
  const target = await evalJs(cdp, "window.collaborationPrototype.samplePointerTarget('node')");
  timeline.push(await measure(cdp, 'node-drag', async () => {
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: target.x, y: target.y });
    await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', x: target.x, y: target.y, button: 'left', buttons: 1 });
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: target.x + 70, y: target.y + 38, button: 'left', buttons: 1 });
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: target.x + 70, y: target.y + 38, button: 'left', buttons: 0 });
  }));
  timeline.push(await measure(cdp, 'view-switch', async () => {
    await evalJs(cdp, "window.collaborationPrototype.setView('impact')");
    await evalJs(cdp, "window.collaborationPrototype.setView('lineage')");
  }));
  timeline.push(await measure(cdp, 'filter-response', async () => {
    await evalJs(cdp, "window.collaborationPrototype.setFilter('project', 'Alpha')");
    await evalJs(cdp, "window.collaborationPrototype.setFilter('agent', 'agent:atlas')");
    await evalJs(cdp, "window.collaborationPrototype.setFilter('window', '24h')");
  }));
  const assertions = mode === 'prototype' ? await evalJs(cdp, 'window.collaborationPrototype.assertPrototype()') : { pass: true, results: { baselineMeasured: true } };
  const screenshot = await cdp.send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: false });
  writeFileSync(join(shotDir, `${mode}-${scale}.png`), Buffer.from(screenshot.data, 'base64'));
  const raw = { url, mode, scale, generated_at: new Date().toISOString(), timeline, assertions, exit_code: 0 };
  writeFileSync(join(rawDir, `${mode}-${scale}-timeline.json`), JSON.stringify(raw, null, 2));
  return raw;
}

await new Promise((resolvePromise) => server.listen(port, '127.0.0.1', resolvePromise));
const chromeProc = spawn(chrome, [
  '--headless=new',
  '--disable-gpu',
  '--no-first-run',
  '--no-default-browser-check',
  `--remote-debugging-port=${cdpPort}`,
  '--window-size=1440,900',
  'about:blank',
], { stdio: ['ignore', 'ignore', 'pipe'] });

let exitCode = 0;
const rows = ['mode,scale,operation,wall_ms,rendered_nodes,rendered_edges,draw_ms_p95,heap_mb,first_interactive_ms'];
const all = [];
try {
  const cdp = await connect();
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');
  for (const currentMode of modes) {
    for (const scale of scales) {
      const raw = await runOne(cdp, currentMode, scale);
      all.push(raw);
      for (const item of raw.timeline) {
        rows.push([
          currentMode,
          scale,
          item.label,
          item.wall_ms,
          item.metrics?.rendered_nodes ?? '',
          item.metrics?.rendered_edges ?? '',
          item.metrics?.draw_ms_p95 ?? '',
          item.metrics?.js_heap_mb ?? '',
          item.metrics?.first_interactive_ms ?? '',
        ].join(','));
      }
      if (!raw.assertions.pass) exitCode = 1;
    }
  }
  writeFileSync(join(rawDir, 'benchmark-summary.json'), JSON.stringify({ generated_at: new Date().toISOString(), all, exit_code: exitCode }, null, 2));
  writeFileSync(join(rawDir, 'benchmark-summary.csv'), rows.join('\n') + '\n');
} catch (error) {
  exitCode = 1;
  writeFileSync(join(rawDir, 'benchmark-error.txt'), `${error.stack || error}\n`);
} finally {
  chromeProc.kill('SIGTERM');
  server.close();
}
process.exit(exitCode);
