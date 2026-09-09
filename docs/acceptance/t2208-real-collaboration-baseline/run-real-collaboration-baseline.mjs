import { createRequire } from "node:module";
import { createServer } from "node:net";
import { spawn, execFile } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, cp, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir, cpus, totalmem, freemem, platform, arch } from "node:os";
import { join, resolve } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const REPO = resolve(new URL("../../..", import.meta.url).pathname);
const requireFromE2E = createRequire(resolve(REPO, "tests/e2e/v2/package.json"));
const { chromium, request: playwrightRequest } = requireFromE2E("@playwright/test");
const OUT = resolve(REPO, "docs/acceptance/t2208-real-collaboration-baseline/evidence");
const RAW = join(OUT, "raw");
const PROTO = join(OUT, "frozen-prototype");
const BIN = resolve(REPO, "bin/agent-center");
const DESIGN_SHA = "d851459ad5913c07c47b82335f6f37b15aba3503";
const DESIGN_REF = "ac-exec/task-3283cdb8/exec-5f769ab7";
const MAIN_BASE = "512d1181264a706155266fa658566146b2ae58c6";
const VIEWPORT = { width: 1440, height: 920 };
const TIERS = [100, 500, 2200];
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let serverLog = "";
const seedProgress = [];

async function main() {
  await rm(OUT, { recursive: true, force: true });
  await mkdir(RAW, { recursive: true });
  await extractFrozenPrototype();
  if (!existsSync(BIN)) throw new Error(`missing ${BIN}; run make build`);

  const git = await gitProvenance();
  const installRoot = await mkdtemp(join(tmpdir(), "t2208-real-collab-"));
  const ports = { web: await freePort(), grpc: await freePort(), admin: await freePort(), proto: await freePort() };
  const baseURL = `http://127.0.0.1:${ports.web}`;
  const protoURL = `http://127.0.0.1:${ports.proto}`;
  const configPath = await writeConfig(installRoot, ports);
  const server = spawn(BIN, ["server", "--config", configPath], {
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "t2208-real-collab-baseline" },
  });
  server.stdout.on("data", (c) => { serverLog += c.toString("utf8"); });
  server.stderr.on("data", (c) => { serverLog += c.toString("utf8"); });
  const protoServer = spawn(process.execPath, ["-e", staticServerSource(PROTO, ports.proto)], { stdio: ["ignore", "pipe", "pipe"] });

  const provenance = {
    task: "T2208 independent real Collaboration baseline and assertion audit",
    build_sha: git.head,
    origin_main_sha_after_fresh_fetch: git.originMain,
    recorded_main_base: MAIN_BASE,
    design_asset_ref: DESIGN_REF,
    design_asset_sha: DESIGN_SHA,
    instance_root: installRoot,
    config_path: configPath,
    real_origin: baseURL,
    prototype_origin: protoURL,
    ports,
    viewport: VIEWPORT,
    runtime: {
      node: process.version,
      platform: platform(),
      arch: arch(),
      cpu_model: cpus()[0]?.model,
      cpu_count: cpus().length,
      total_memory_mb: Math.round(totalmem() / 1048576),
      free_memory_mb_at_start: Math.round(freemem() / 1048576),
    },
    auth_data_provenance: "Fresh isolated binary instance; owner signup plus all projects/plans/tasks/agents seeded through authenticated public Web Console HTTP APIs.",
    no_bypass: "No SQLite/admin socket/admin HTTP/worker token/MCP/DB path used by this harness.",
  };

  try {
    await waitHealthy(`${baseURL}/api/health`);
    await waitHealthy(protoURL);
    const seeded = await seedData(`${baseURL}/api`);
    provenance.organization_slug = seeded.slug;
    provenance.organization_id = seeded.organization_id;
    provenance.owner_login = seeded.ownerName;
    provenance.seeded_projects = Object.fromEntries(Object.entries(seeded.projects).map(([k, v]) => [k, { project_id: v.projectID, plan_id: v.planID, requested_tasks: v.taskCount, agents: seeded.agentRefs }]));

    const browser = await chromium.launch();
    provenance.browser_version = browser.version();

    const realResults = [];
    const protoResults = [];
    for (const tier of TIERS) {
      realResults.push(await measureRealTier(browser, baseURL, seeded, tier));
      protoResults.push(await measurePrototypeTier(browser, protoURL, tier));
    }
    await browser.close();

    const audit = assertionAudit(realResults, protoResults);
    const verdict = audit.every((row) => row.verdict === "PASS" || row.verdict === "N/A") ? "PASS" : audit.some((row) => row.verdict === "BLOCKED") ? "BLOCKED" : "REJECT";
    const summary = { verdict, provenance, realResults, protoResults, audit };
    await writeFile(join(OUT, "verdict.json"), JSON.stringify(summary, null, 2));
    await writeFile(resolve(REPO, "docs/acceptance/t2208-real-collaboration-baseline/REPORT.md"), report(summary));
    await writeFile(join(RAW, "server.log"), serverLog);
    await writeFile(join(RAW, "environment.json"), JSON.stringify(provenance, null, 2));
    if (verdict !== "PASS") process.exitCode = 2;
  } finally {
    await writeFile(join(RAW, "server.log"), serverLog).catch(() => {});
    server.kill("SIGTERM");
    protoServer.kill("SIGTERM");
    await sleep(500);
    if (server.exitCode === null) server.kill("SIGKILL");
    if (protoServer.exitCode === null) protoServer.kill("SIGKILL");
  }
}

async function extractFrozenPrototype() {
  await mkdir(PROTO, { recursive: true });
  const files = ["index.html", "prototype.css", "prototype.js"];
  for (const file of files) {
    const gitPath = `docs/design/features/collaboration-insight-redesign/prototype/${file}`;
    const { stdout } = await execFileAsync("git", ["show", `${DESIGN_SHA}:${gitPath}`], { cwd: REPO, maxBuffer: 20 * 1024 * 1024 });
    await writeFile(join(PROTO, file), stdout);
  }
  const evidenceFiles = [
    "docs/design/features/collaboration-insight-redesign/evidence/raw/benchmark-summary.json",
    "docs/design/features/collaboration-insight-redesign/evidence/raw/benchmark-summary.csv",
  ];
  for (const gitPath of evidenceFiles) {
    try {
      const { stdout } = await execFileAsync("git", ["show", `${DESIGN_SHA}:${gitPath}`], { cwd: REPO, maxBuffer: 50 * 1024 * 1024 });
      await writeFile(join(RAW, `frozen-${gitPath.split("/").pop()}`), stdout);
    } catch {}
  }
}

async function gitProvenance() {
  const run = async (...args) => (await execFileAsync("git", args, { cwd: REPO })).stdout.trim();
  await run("fetch", "origin", "main", "--prune");
  return {
    head: await run("rev-parse", "HEAD"),
    originMain: await run("rev-parse", "origin/main"),
    branch: await run("branch", "--show-current"),
    status: await run("status", "--short", "--branch"),
  };
}

async function freePort() {
  return new Promise((resolvePort, reject) => {
    const srv = createServer();
    srv.listen(0, "127.0.0.1", () => {
      const port = srv.address().port;
      srv.close(() => resolvePort(port));
    });
    srv.on("error", reject);
  });
}

async function writeConfig(root, ports) {
  const masterKeyPath = join(root, "master.key");
  await writeFile(masterKeyPath, randomBytes(32).toString("base64") + "\n", "utf8");
  await chmod(masterKeyPath, 0o600);
  const configPath = join(root, "config.yaml");
  await writeFile(configPath, `server:
  listen_addr: ":${ports.grpc}"
  sqlite_path: "${join(root, "agent-center.db")}"
  admin_socket_path: "${join(root, "admin.sock")}"
  admin_tcp_listen: "127.0.0.1:${ports.admin}"
  admin_tls_cert_path: "${join(root, "admin.crt")}"
  admin_tls_key_path: "${join(root, "admin.key")}"
  bootstrap_public_url: "127.0.0.1:${ports.admin}"
web_console:
  enabled: true
  listen_addr: "127.0.0.1:${ports.web}"
secret_management:
  master_key_file: "${masterKeyPath}"
blob_store:
  root: "${join(root, "blobs")}"
`, "utf8");
  return configPath;
}

function staticServerSource(dir, port) {
  return `
const http = require("http"), fs = require("fs"), path = require("path");
const root = ${JSON.stringify(dir)};
const types = { ".html": "text/html", ".css": "text/css", ".js": "text/javascript", ".json": "application/json" };
http.createServer((req, res) => {
  const url = new URL(req.url, "http://127.0.0.1");
  let file = path.join(root, url.pathname === "/" ? "index.html" : url.pathname);
  if (!file.startsWith(root)) { res.writeHead(403).end("forbidden"); return; }
  fs.readFile(file, (err, body) => {
    if (err) { res.writeHead(404).end("not found"); return; }
    res.writeHead(200, { "content-type": types[path.extname(file)] || "application/octet-stream" });
    res.end(body);
  });
}).listen(${port}, "127.0.0.1");
`;
}

async function waitHealthy(url) {
  for (let i = 0; i < 180; i++) {
    try {
      const r = await fetch(url);
      if (r.ok) return;
    } catch {}
    await sleep(250);
  }
  throw new Error(`not healthy: ${url}`);
}

async function seedData(apiURL) {
  const req = await playwrightRequest.newContext();
  const ownerName = `t2208-owner-${Date.now().toString(36)}`;
  const passcode = "T2208Pass1!";
  const signup = await json(req.post(`${apiURL}/auth/signup`, { data: { display_name: ownerName, passcode, organization_name: "T2208 Real Collaboration Org", email: `${ownerName}@example.test` } }), "signup");
  const org = `${apiURL}/orgs/${signup.organization_slug}`;
  const worker = await json(req.post(`${org}/admintoken/mint-enroll`, { data: { name: "t2208-worker" } }), "mint worker");
  const runtime = await json(req.get(`${org}/ai-runtime`), "runtime");
  let revision = runtime.revision || 0;
  if (!(runtime.clis || []).some((c) => c.key === "codex" && c.enabled)) {
    const cli = await json(req.post(`${org}/ai-runtime/clis`, { data: { expected_revision: revision, value: { key: "codex", display_name: "Codex", executable: "codex", required_features: [], enabled: true } } }), "create cli");
    revision = cli.revision;
  }
  const model = await json(req.post(`${org}/ai-runtime/models`, { data: { expected_revision: revision, value: { key: "gpt-t2208", model_key: "gpt-t2208", display_name: "GPT T2208", compatible_cli_keys: ["codex"], default_parameters: {}, enabled: true, context_window: 128000, input_cost_per_mtok: 0.1, output_cost_per_mtok: 0.4, tier: "acceptance" } } }), "create model");
  const agents = [];
  for (const name of ["Atlas", "Forge", "Signal", "Delta", "Nova", "Orbit"]) {
    const agent = await json(req.post(`${org}/members/agent`, { data: { display_name: `${name} agent`, cli: "codex", model: "gpt-t2208", worker_id: worker.worker_id } }), `agent ${name}`);
    agents.push(`agent:${agent.agent_id || agent.identity_id}`);
  }
  const projects = {};
  for (const tier of TIERS) projects[tier] = await seedProject(req, org, tier, agents);
  await req.dispose();
  return { slug: signup.organization_slug, organization_id: signup.organization_id, ownerName, passcode, agentRefs: agents, runtimeRevision: model.revision, projects };
}

async function seedProject(req, org, taskCount, agents) {
  const project = await json(req.post(`${org}/projects`, { data: { name: `T2208 scale ${taskCount}`, description: `Public API scale fixture ${taskCount}` } }), `project ${taskCount}`);
  const plan = await json(req.post(`${org}/projects/${project.id}/plans`, { data: { name: `T2208 plan ${taskCount}`, description: "Plan graph scale fixture" } }), `plan ${taskCount}`);
  await Promise.all(agents.map((identity_id) => ok(req.post(`${org}/projects/${project.id}/members`, {
    data: { identity_id, role: "member" },
  }), `project member ${taskCount}.${identity_id}`)));
  const tasks = [];
  const started = Date.now();
  const batchSize = 8;
  for (let i = 0; i < taskCount; i += batchSize) {
    const batch = Array.from({ length: Math.min(batchSize, taskCount - i) }, (_, j) => i + j);
    const created = await Promise.all(batch.map((n) => json(req.post(`${org}/projects/${project.id}/tasks`, {
      data: { title: `Scale ${taskCount} task ${String(n + 1).padStart(4, "0")}`, description: "real Collaboration baseline scale seed", assignee: agents[n % agents.length] },
    }), `task ${taskCount}.${n}`)));
    tasks.push(...created.map((t) => t.id));
    await recordSeedProgress({ tier: taskCount, phase: "tasks", completed: tasks.length, total: taskCount, elapsed_ms: Date.now() - started });
  }
  for (let i = 0; i < tasks.length; i += batchSize) {
    const batch = tasks.slice(i, i + batchSize);
    await Promise.all(batch.map((task_id, j) => ok(req.post(`${org}/projects/${project.id}/plans/${plan.id}/tasks`, { data: { task_id } }), `plan add ${taskCount}.${i + j}`)));
    await recordSeedProgress({ tier: taskCount, phase: "plan-membership", completed: Math.min(i + batch.length, tasks.length), total: tasks.length, elapsed_ms: Date.now() - started });
  }
  for (let i = 0; i < Math.min(12, tasks.length); i++) {
    await ok(req.post(`${org}/projects/${project.id}/tasks/${tasks[i]}/assign`, { data: { assignee: agents[(i + 1) % agents.length] } }), `reassign ${taskCount}.${i}`);
  }
  if (tasks.length > 2) {
    await ok(req.post(`${org}/projects/${project.id}/plans/${plan.id}/start`, { data: {} }), `start plan ${taskCount}`).catch(() => ({}));
    await ok(req.post(`${org}/projects/${project.id}/tasks/${tasks[0]}/start`, { data: {} }), `start first ${taskCount}`).catch(() => ({}));
    await ok(req.post(`${org}/projects/${project.id}/tasks/${tasks[0]}/complete`, { data: {} }), `complete first ${taskCount}`).catch(() => ({}));
  }
  return { projectID: project.id, planID: plan.id, taskCount, tasks: tasks.slice(0, 8), seed_ms: Date.now() - started };
}

async function recordSeedProgress(entry) {
  const last = seedProgress[seedProgress.length - 1];
  seedProgress.push({ at: new Date().toISOString(), ...entry });
  if (!last || last.tier !== entry.tier || last.phase !== entry.phase || entry.completed === entry.total || entry.completed - last.completed >= 80) {
    console.error(`[seed] tier=${entry.tier} phase=${entry.phase} ${entry.completed}/${entry.total} elapsed=${entry.elapsed_ms}ms`);
  }
  await writeFile(join(RAW, "seed-progress.json"), JSON.stringify(seedProgress, null, 2));
}

async function measureRealTier(browser, baseURL, seeded, tier) {
  const outDir = join(RAW, `real-${tier}`);
  await mkdir(outDir, { recursive: true });
  const projectID = seeded.projects[tier].projectID;
  const consoleEvents = [], pageErrors = [], network = [], longTasks = [];
  const context = await browser.newContext({
    viewport: VIEWPORT,
    deviceScaleFactor: 1,
    recordVideo: { dir: outDir, size: VIEWPORT },
    recordHar: { path: join(outDir, "network.har"), content: "embed" },
  });
  await context.tracing.start({ screenshots: true, snapshots: true, sources: false });
  const page = await context.newPage();
  page.on("console", (msg) => consoleEvents.push({ type: msg.type(), text: msg.text(), location: msg.location() }));
  page.on("pageerror", (err) => pageErrors.push(String(err.stack || err.message || err)));
  page.on("response", async (resp) => {
    if (resp.url().startsWith(baseURL)) network.push({ url: resp.url(), method: resp.request().method(), type: resp.request().resourceType(), status: resp.status() });
  });
  await installLongTaskObserver(page);
  await signin(context, page, baseURL, seeded);
  const api = await fetchRealAPI(page, baseURL, seeded.slug, projectID, tier, outDir);
  const url = `${baseURL}/organizations/${seeded.slug}/insights/collaboration?project_id=${encodeURIComponent(projectID)}&lod=full`;
  const t0 = Date.now();
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.getByTestId("collaboration-graph-svg").waitFor({ timeout: 90000 });
  const tti = Date.now() - t0;
  await page.waitForTimeout(600);
  const render = await measureQuietRender(page);
  const interactions = await exerciseRealInteractions(page);
  const filters = await assertRealFilters(page, baseURL, seeded.slug, projectID, seeded.agentRefs[0]);
  if (filters.verdict !== "PASS") {
    await page.goto(url, { waitUntil: "domcontentloaded", timeout: 90000 }).catch(() => {});
    await page.getByTestId("collaboration-graph-svg").waitFor({ timeout: 90000 }).catch(() => {});
  }
  const contextState = await assertRealContext(page);
  longTasks.push(...await page.evaluate(() => window.__longTasks || []));
  const memory = await page.evaluate(() => performance.memory ? { usedJSHeapSize: performance.memory.usedJSHeapSize, totalJSHeapSize: performance.memory.totalJSHeapSize, js_heap_mb: Math.round(performance.memory.usedJSHeapSize / 1048576) } : null);
  await page.screenshot({ path: join(outDir, "page.png"), fullPage: true });
  await context.tracing.stop({ path: join(outDir, "trace.zip") });
  await context.close();
  await writeFile(join(outDir, "console.json"), JSON.stringify({ consoleEvents, pageErrors }, null, 2));
  await writeFile(join(outDir, "network.json"), JSON.stringify(network, null, 2));
  const result = { tier, kind: "real", url, api, tti_ms: tti, quiet_render: render, interactions, filters, contextState, memory, longTasks, evidence_dir: relative(outDir) };
  await writeFile(join(outDir, "result.json"), JSON.stringify(result, null, 2));
  return result;
}

async function signin(context, page, baseURL, seeded) {
  const auth = await playwrightRequest.newContext({ baseURL });
  await json(auth.post("/api/auth/signin", { data: { login: seeded.ownerName, passcode: seeded.passcode } }), "browser auth signin");
  const storage = await auth.storageState();
  await auth.dispose();
  await context.addCookies(storage.cookies);
  await page.goto(`${baseURL}/organizations/${seeded.slug}/projects`, { waitUntil: "domcontentloaded", timeout: 60000 });
  const me = await page.evaluate(async () => {
    const r = await fetch("/api/auth/me");
    return { status: r.status, text: await r.text() };
  });
  if (me.status !== 200) throw new Error(`browser auth did not stick: HTTP ${me.status} ${me.text.slice(0, 200)}`);
}

async function fetchRealAPI(page, baseURL, slug, projectID, tier, outDir) {
  const queries = {
    visible_full: `/api/orgs/${slug}/insights/collaboration-effects?project_id=${encodeURIComponent(projectID)}&lod=full&limit=500`,
    product_default: `/api/orgs/${slug}/insights/collaboration-effects?project_id=${encodeURIComponent(projectID)}&limit=100`,
    clustered_90: `/api/orgs/${slug}/insights/collaboration-effects?project_id=${encodeURIComponent(projectID)}&lod=cluster&max_nodes=90&limit=100`,
  };
  const out = {};
  for (const [name, path] of Object.entries(queries)) {
    const body = await page.evaluate(async (url) => {
      const r = await fetch(url);
      return { status: r.status, text: await r.text() };
    }, path);
    out[name] = { status: body.status, body: JSON.parse(body.text) };
    await writeFile(join(outDir, `api-${name}.json`), JSON.stringify(out[name], null, 2));
  }
  return {
    requested_scale: tier,
    visible_full: graphCounts(out.visible_full.body),
    product_default: graphCounts(out.product_default.body),
    clustered_90: graphCounts(out.clustered_90.body),
    next_cursor: out.visible_full.body.next_cursor || "",
    truncated: Boolean(out.visible_full.body.truncated || out.visible_full.body.graph?.truncated),
  };
}

async function measureQuietRender(page) {
  return page.evaluate(async () => {
    const samples = [];
    for (let i = 0; i < 12; i++) {
      const t0 = performance.now();
      await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      samples.push(performance.now() - t0);
    }
    samples.sort((a, b) => a - b);
    return { p50_ms: samples[Math.floor(samples.length * 0.5)], p95_ms: samples[Math.floor(samples.length * 0.95)] || samples.at(-1), samples };
  });
}

async function exerciseRealInteractions(page) {
  const svg = page.getByTestId("collaboration-graph-svg");
  const box = await svg.boundingBox();
  const state = async () => page.evaluate(() => {
    const svg = document.querySelector("[data-testid='collaboration-graph-svg']");
    const first = svg?.querySelector("g[role='button'] circle, g[role='button'] rect");
    return { viewBox: svg?.getAttribute("viewBox"), nodes: svg?.querySelectorAll("g[role='button']").length || 0, edges: svg?.querySelectorAll("line.collaboration-edge").length || 0, first: first ? { cx: first.getAttribute("cx"), cy: first.getAttribute("cy"), x: first.getAttribute("x"), y: first.getAttribute("y") } : null };
  });
  const before = await state();
  const tPan = Date.now();
  await page.mouse.move(box.x + 600, box.y + 260);
  await page.mouse.down();
  await page.mouse.move(box.x + 720, box.y + 330, { steps: 12 });
  await page.mouse.up();
  const pan = await state();
  const pan_ms = Date.now() - tPan;
  const tWheel = Date.now();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Input.dispatchMouseEvent", { type: "mouseWheel", x: box.x + box.width / 2, y: box.y + box.height / 2, deltaX: 0, deltaY: -360 });
  await page.waitForFunction(
    (beforeViewBox) => document.querySelector("[data-testid='collaboration-graph-svg']")?.getAttribute("viewBox") !== beforeViewBox,
    pan.viewBox,
    { timeout: 1500 },
  ).catch(async () => {
    await svg.dispatchEvent("wheel", { deltaY: -360, clientX: box.x + box.width / 2, clientY: box.y + box.height / 2, bubbles: true, cancelable: true });
    await page.waitForTimeout(120);
  });
  const wheel = await state();
  const wheel_ms = Date.now() - tWheel;
  await page.getByRole("button", { name: /^reset$/i }).click();
  await page.waitForTimeout(80);
  const node = page.locator("[data-testid='collaboration-graph-svg'] g[role='button']").first();
  const nb = await node.boundingBox();
  const dragBefore = await state();
  const tDrag = Date.now();
  await page.mouse.move(nb.x + nb.width / 2, nb.y + nb.height / 2);
  await page.mouse.down();
  await page.mouse.move(nb.x + nb.width / 2 + 90, nb.y + nb.height / 2 + 30, { steps: 10 });
  await page.mouse.up();
  const drag = await state();
  const drag_ms = Date.now() - tDrag;
  await node.focus();
  await page.keyboard.press("Enter");
  await page.waitForTimeout(80);
  const focus = await state();
  return {
    pan: { verdict: before.viewBox !== pan.viewBox ? "PASS" : "REJECT", ms: pan_ms, before: before.viewBox, after: pan.viewBox },
    wheel_zoom: { verdict: pan.viewBox !== wheel.viewBox ? "PASS" : "REJECT", ms: wheel_ms, before: pan.viewBox, after: wheel.viewBox },
    node_drag: { verdict: JSON.stringify(dragBefore.first) !== JSON.stringify(drag.first) ? "PASS" : "REJECT", ms: drag_ms, before: dragBefore.first, after: drag.first },
    neighborhood_focus: { verdict: focus.viewBox !== drag.viewBox ? "PASS" : "REJECT", before: drag.viewBox, after: focus.viewBox },
    visible_counts: { nodes: before.nodes, edges: before.edges },
  };
}

async function assertRealFilters(page, baseURL, slug, projectID, agentRef) {
  const before = await graphDom(page);
  const t0 = Date.now();
  await page.goto(`${baseURL}/organizations/${slug}/insights/collaboration?project_id=${encodeURIComponent(projectID)}&agent_ref=${encodeURIComponent(agentRef)}&lod=full`, { waitUntil: "domcontentloaded" });
  const visible = await page.getByTestId("collaboration-graph-svg").waitFor({ timeout: 180000 }).then(() => true).catch(() => false);
  if (!visible) {
    return {
      response_ms: Date.now() - t0,
      agent_ref: agentRef,
      before,
      after: await graphDom(page),
      url: page.url(),
      body_excerpt: await page.locator("body").innerText().then((text) => text.slice(0, 1000)).catch((err) => String(err)),
      verdict: "BLOCKED",
      reason: "filtered real Collaboration page did not expose collaboration-graph-svg before timeout; preserved as evidence instead of aborting",
    };
  }
  const after = await graphDom(page);
  const api = await page.evaluate(async (url) => {
    const r = await fetch(url);
    return JSON.parse(await r.text());
  }, `/api/orgs/${slug}/insights/collaboration-effects?project_id=${encodeURIComponent(projectID)}&agent_ref=${encodeURIComponent(agentRef)}&lod=full&limit=500`);
  const ids = new Set(api.graph.nodes.map((n) => n.id));
  return {
    response_ms: Date.now() - t0,
    agent_ref: agentRef,
    before,
    after,
    expected_id_set_sample: [...ids].slice(0, 30),
    verdict: after.nodes > 0 && after.nodes < before.nodes && [...ids].some((id) => id === agentRef) && api.graph.edges.every((e) => e.source === agentRef || e.target === agentRef || e.relation_type === "plan_task" || e.relation_type === "project_plan") ? "PASS" : "REJECT",
  };
}

async function assertRealContext(page) {
  const url = page.url();
  const selected = await page.locator("section[data-testid='collaboration-graph'] button").filter({ hasText: /evidence [1-9]/ }).first();
  const hasEdge = await selected.count();
  if (!hasEdge) return { verdict: "BLOCKED", reason: "no effect edge with evidence visible" };
  await selected.click();
  await page.getByTestId("collaboration-evidence-drawer").waitFor({ timeout: 15000 });
  const drawer = await page.getByTestId("collaboration-evidence-drawer").innerText();
  return {
    verdict: /before|after/i.test(drawer) && /pm\./.test(drawer) ? "PASS" : "REJECT",
    selected_source: "edge button with evidence > 0",
    filter_values: Object.fromEntries(new URL(url).searchParams.entries()),
    viewport: await graphDom(page),
    drawer_excerpt: drawer.slice(0, 500),
  };
}

async function measurePrototypeTier(browser, protoURL, tier) {
  const outDir = join(RAW, `prototype-${tier}`);
  await mkdir(outDir, { recursive: true });
  const consoleEvents = [], pageErrors = [], longTasks = [];
  const context = await browser.newContext({
    viewport: VIEWPORT,
    deviceScaleFactor: 1,
    recordVideo: { dir: outDir, size: VIEWPORT },
    recordHar: { path: join(outDir, "network.har"), content: "embed" },
  });
  await context.tracing.start({ screenshots: true, snapshots: true, sources: false });
  const page = await context.newPage();
  page.on("console", (msg) => consoleEvents.push({ type: msg.type(), text: msg.text(), location: msg.location() }));
  page.on("pageerror", (err) => pageErrors.push(String(err.stack || err.message || err)));
  await installLongTaskObserver(page);
  const url = `${protoURL}/index.html?scale=${tier}`;
  const t0 = Date.now();
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => window.collaborationPrototype?.metrics()?.rendered_nodes > 0, null, { timeout: 30000 });
  const tti = Date.now() - t0;
  await page.waitForTimeout(500);
  const render = await measureQuietRender(page);
  const strict = await strictPrototypeAssertions(page);
  const interactions = await exercisePrototypeInteractions(page);
  longTasks.push(...await page.evaluate(() => window.__longTasks || []));
  const memory = await page.evaluate(() => performance.memory ? { usedJSHeapSize: performance.memory.usedJSHeapSize, totalJSHeapSize: performance.memory.totalJSHeapSize, js_heap_mb: Math.round(performance.memory.usedJSHeapSize / 1048576) } : null);
  await page.screenshot({ path: join(outDir, "page.png"), fullPage: true });
  await context.tracing.stop({ path: join(outDir, "trace.zip") });
  await context.close();
  await writeFile(join(outDir, "console.json"), JSON.stringify({ consoleEvents, pageErrors }, null, 2));
  const result = { tier, kind: "prototype", url, tti_ms: tti, quiet_render: render, strict, interactions, memory, longTasks, evidence_dir: relative(outDir) };
  await writeFile(join(outDir, "result.json"), JSON.stringify(result, null, 2));
  return result;
}

async function strictPrototypeAssertions(page) {
  return page.evaluate(async () => {
    const api = window.collaborationPrototype;
    const graphKey = (g) => `${g.nodes.map((n) => n.id).sort().join("|")}::${g.edges.map((e) => e.id).sort().join("|")}`;
    api.state.filters = { window: "30d", project: "", agent: "" };
    api.state.view = "network"; api.state.layout = "community"; api.state.focus = null; api.state.selected = null; api.state.depth = 0;
    const start = api.state.current = graphForState();
    api.setFilter("project", "Alpha");
    await new Promise((r) => setTimeout(r, 30));
    const project = graphForState();
    api.setFilter("agent", "agent:atlas");
    await new Promise((r) => setTimeout(r, 30));
    const agent = graphForState();
    api.setFilter("window", "24h");
    await new Promise((r) => setTimeout(r, 30));
    const win = graphForState();
    let selectedNode = agent.nodes.find((candidate) => {
      api.state.selected = { type: "node", id: candidate.id };
      api.state.focus = candidate.id;
      api.state.depth = 1;
      const one = graphForState();
      api.state.depth = 2;
      const two = graphForState();
      const oneIds = new Set(one.nodes.map((n) => n.id));
      return two.nodes.some((n) => !oneIds.has(n.id));
    }) || agent.nodes.find((n) => n.agent === "agent:atlas" || n.id.includes("atlas")) || agent.nodes[0];
    api.state.selected = { type: "node", id: selectedNode.id };
    api.state.focus = selectedNode.id;
    api.state.depth = 1;
    const oneHop = graphForState();
    api.state.depth = 2;
    const twoHop = graphForState();
    api.state.depth = 0;
    const restored = graphForState();
    api.state.selected = { type: "edge", id: restored.edges[0].id };
    const edge = selectedEdge();
    preserveSelectionForView("impact");
    const impactSelected = api.state.selected;
    const expectedProjectEdges = project.edges.map((e) => e.id).sort();
    const expectedAgentEdges = agent.edges.map((e) => e.id).sort();
    const expectedWindowEdges = win.edges.map((e) => e.id).sort();
    const oneNodes = new Set(oneHop.nodes.map((n) => n.id));
    const twoNodes = new Set(twoHop.nodes.map((n) => n.id));
    const added = [...twoNodes].filter((id) => !oneNodes.has(id));
    return {
      project_filter: { verdict: graphKey(project) !== graphKey(start) && project.edges.every((e) => e.project === "Alpha") ? "PASS" : "REJECT", expected_edge_ids: expectedProjectEdges.slice(0, 50), count: expectedProjectEdges.length },
      agent_filter: { verdict: graphKey(agent) !== graphKey(project) && agent.edges.every((e) => e.agent === "agent:atlas" || e.source.includes("atlas")) ? "PASS" : "REJECT", expected_edge_ids: expectedAgentEdges.slice(0, 50), count: expectedAgentEdges.length },
      window_filter: { verdict: win.edges.length > 0 && win.edges.every((e) => windowRank(e.window) <= windowRank("24h")) ? "PASS" : "REJECT", expected_edge_ids: expectedWindowEdges.slice(0, 50), count: expectedWindowEdges.length, changed_from_prior_filter: graphKey(win) !== graphKey(agent) },
      neighborhood_expand: { verdict: added.length > 0 && twoHop.nodes.length > oneHop.nodes.length ? "PASS" : "REJECT", selected_id: selectedNode.id, one_hop_nodes: oneHop.nodes.map((n) => n.id).sort().slice(0, 50), two_hop_added_nodes: added.sort().slice(0, 50), one_count: oneHop.nodes.length, two_count: twoHop.nodes.length },
      selected_context: { verdict: edge?.id === restored.edges[0].id && edge?.effect_id === restored.edges[0].effect_id ? "PASS" : "REJECT", selected_id: edge?.id, filter_values: { ...api.state.filters }, viewport: { ...api.state.camera } },
      view_switch_context: { verdict: api.state.notice.includes("Preserved") || api.state.notice.includes("cleared explicitly") ? "PASS" : "REJECT", prior_edge_id: edge?.id, after_selection: impactSelected, notice: api.state.notice },
    };
  });
}

async function exercisePrototypeInteractions(page) {
  const canvas = page.locator("#graph");
  const box = await canvas.boundingBox();
  const state = () => page.evaluate(() => ({ metrics: window.collaborationPrototype.metrics(), camera: { ...window.collaborationPrototype.state.camera }, selected: window.collaborationPrototype.state.selected, pinned: [...window.collaborationPrototype.state.pinned] }));
  const before = await state();
  const tWheel = Date.now();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -300);
  await page.waitForTimeout(80);
  const wheel = await state();
  const tPan = Date.now();
  await page.mouse.move(box.x + 900, box.y + 520);
  await page.mouse.down();
  await page.mouse.move(box.x + 960, box.y + 570, { steps: 8 });
  await page.mouse.up();
  await page.waitForTimeout(80);
  const pan = await state();
  await page.evaluate(() => {
    window.collaborationPrototype.state.camera = { x: 0, y: 0, zoom: 1 };
    window.collaborationPrototype.state.drag = null;
    window.collaborationPrototype.redraw();
  });
  await page.waitForTimeout(80);
  const preDrag = await state();
  const target = await page.evaluate(() => window.collaborationPrototype.samplePointerTarget("node"));
  const tDrag = Date.now();
  await page.mouse.move(box.x + target.x, box.y + target.y);
  await page.mouse.down();
  await page.mouse.move(box.x + target.x + 60, box.y + target.y + 20, { steps: 8 });
  await page.mouse.up();
  await page.waitForTimeout(80);
  const drag = await state();
  return {
    wheel_zoom: { verdict: before.camera.zoom !== wheel.camera.zoom ? "PASS" : "REJECT", ms: Date.now() - tWheel, before: before.camera, after: wheel.camera },
    pan: { verdict: wheel.camera.x !== pan.camera.x || wheel.camera.y !== pan.camera.y ? "PASS" : "REJECT", ms: Date.now() - tPan, before: wheel.camera, after: pan.camera },
    node_drag: { verdict: drag.pinned.length > preDrag.pinned.length ? "PASS" : "REJECT", ms: Date.now() - tDrag, pinned_before: preDrag.pinned.slice(0, 10), pinned_after: drag.pinned.slice(0, 10) },
  };
}

async function installLongTaskObserver(page) {
  await page.addInitScript(() => {
    window.__longTasks = [];
    try {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) window.__longTasks.push({ name: entry.name, startTime: entry.startTime, duration: entry.duration });
      }).observe({ entryTypes: ["longtask"] });
    } catch {}
  });
}

async function graphDom(page) {
  return page.evaluate(() => {
    const svg = document.querySelector("[data-testid='collaboration-graph-svg']");
    const nodeEls = [...(svg?.querySelectorAll("g[role='button']") || [])];
    const edgeEls = [...(svg?.querySelectorAll("line.collaboration-edge") || [])];
    const idFrom = (el) => el.getAttribute("data-node-id") || el.getAttribute("data-edge-id") || el.getAttribute("aria-label") || el.id || "";
    return {
      viewBox: svg?.getAttribute("viewBox") || "",
      nodes: nodeEls.length,
      edges: edgeEls.length,
      node_ids: nodeEls.map(idFrom).filter(Boolean).sort(),
      edge_ids: edgeEls.map(idFrom).filter(Boolean).sort(),
      id_mapping_contract: "Future harness captures DOM data-node-id/data-edge-id when exposed; legacy frozen T2208 artifacts predate this field and are checked by public API set plus DOM count parity in verify-frozen-evidence.mjs.",
    };
  });
}

function graphCounts(body) {
  return {
    status: body ? 200 : null,
    nodes: body?.graph?.nodes?.length || 0,
    edges: body?.graph?.edges?.length || 0,
    effects: body?.effects?.length || 0,
    lod: body?.graph?.lod,
    graph_truncated: Boolean(body?.graph?.truncated),
    response_truncated: Boolean(body?.truncated),
    next_cursor: body?.next_cursor || "",
  };
}

function assertionAudit(realResults, protoResults) {
  const rows = [];
  for (const tier of TIERS) {
    const real = realResults.find((r) => r.tier === tier);
    const proto = protoResults.find((r) => r.tier === tier);
    const realScaleBlocked = real.filters.verdict === "BLOCKED" || real.contextState.verdict === "BLOCKED";
    rows.push({ gate: `scale-${tier}-real-page`, verdict: realScaleBlocked ? "BLOCKED" : real.api.visible_full.nodes >= tier && real.api.visible_full.edges >= tier && real.interactions.pan.verdict === "PASS" && real.interactions.wheel_zoom.verdict === "PASS" ? "PASS" : "REJECT", evidence: `${real.evidence_dir}/result.json`, detail: real.api });
    rows.push({ gate: `scale-${tier}-prototype`, verdict: proto.strict.project_filter.verdict === "PASS" && proto.strict.neighborhood_expand.verdict === "PASS" && proto.interactions.wheel_zoom.verdict === "PASS" ? "PASS" : "REJECT", evidence: `${proto.evidence_dir}/result.json`, detail: proto.strict });
  }
  rows.push({ gate: "T2208-neighborhood-expand", verdict: protoResults.every((r) => r.strict.neighborhood_expand.verdict === "PASS") ? "PASS" : "REJECT", evidence: "prototype-*/result.json", detail: "Strict audit requires two-hop target set to add IDs and grow; equality is REJECT." });
  rows.push({ gate: "T2208-window-project-agent-filters", verdict: realResults.some((r) => r.filters.verdict === "BLOCKED") ? "BLOCKED" : protoResults.every((r) => r.strict.window_filter.verdict === "PASS" && r.strict.project_filter.verdict === "PASS" && r.strict.agent_filter.verdict === "PASS") && realResults.every((r) => r.filters.verdict === "PASS") ? "PASS" : "REJECT", evidence: "real-*/result.json and prototype-*/result.json", detail: "Prototype validates expected edge ID sets; real page validates filtered API ID/edge set for agent_ref plus project_id." });
  rows.push({ gate: "T2208-selected-context", verdict: realResults.some((r) => r.contextState.verdict === "BLOCKED") ? "BLOCKED" : protoResults.every((r) => r.strict.selected_context.verdict === "PASS") && realResults.every((r) => r.contextState.verdict === "PASS") ? "PASS" : "REJECT", evidence: "real-*/result.json and prototype-*/result.json", detail: "Selected IDs, filter values, viewport/camera, and drawer state captured." });
  rows.push({ gate: "T2208-view-switch", verdict: protoResults.every((r) => r.strict.view_switch_context.verdict === "PASS") ? "PASS" : "REJECT", evidence: "prototype-*/result.json", detail: "Real page has no dimensional view switch control, so product verdict is N/A and not counted as failure." });
  rows.push({ gate: "T2208-real-view-switch", verdict: "N/A", evidence: "web/src/pages/InsightCollaboration.tsx real DOM captures", detail: "Current production page exposes one Collaboration graph view plus filters; no Network/Impact/Lineage switch exists." });
  rows.push({ gate: "performance-evidence-complete", verdict: realResults.every(hasPerfFiles) && protoResults.every(hasPerfFiles) ? "PASS" : "REJECT", evidence: "raw/*/{trace.zip,network.har,page.webm,console.json,result.json}", detail: "TTI, quiet render, pan, wheel zoom, drag, filter response, memory, long tasks captured per tier." });
  return rows;
}

function hasPerfFiles(r) {
  const dir = resolve(REPO, r.evidence_dir);
  return existsSync(join(dir, "trace.zip")) && existsSync(join(dir, "network.har")) && existsSync(join(dir, "console.json")) && existsSync(join(dir, "result.json"));
}

function report(summary) {
  const lines = [];
  lines.push("# T2208 Real Collaboration Baseline Evidence");
  lines.push("");
  lines.push(`Verdict: **${summary.verdict}**`);
  lines.push("");
  lines.push("## Provenance");
  for (const [key, value] of Object.entries(summary.provenance)) lines.push(`- ${key}: ${typeof value === "object" ? JSON.stringify(value) : value}`);
  lines.push("");
  lines.push("## T2208 Hard-Gate Audit");
  lines.push("| Gate | Verdict | Evidence | Detail |");
  lines.push("|---|---|---|---|");
  for (const row of summary.audit) lines.push(`| ${row.gate} | ${row.verdict} | \`${row.evidence}\` | ${JSON.stringify(row.detail).replaceAll("|", "\\|")} |`);
  lines.push("");
  lines.push("## Performance Summary");
  lines.push("| Kind | Scale | TTI ms | Quiet p95 ms | Memory MB | Long tasks | Evidence |");
  lines.push("|---|---:|---:|---:|---:|---:|---|");
  for (const row of [...summary.realResults, ...summary.protoResults]) lines.push(`| ${row.kind} | ${row.tier} | ${row.tti_ms} | ${Math.round(row.quiet_render.p95_ms)} | ${row.memory?.js_heap_mb ?? "n/a"} | ${row.longTasks.length} | \`${row.evidence_dir}\` |`);
  lines.push("");
  lines.push("## Data Provenance");
  lines.push("- Real page data was created through owner-authenticated public Web Console HTTP API calls against the isolated binary instance.");
  lines.push("- The real page loaded `/organizations/{org}/insights/collaboration` and fetched production `/api/orgs/{slug}/insights/collaboration-effects` responses; no Canvas or component props were fed directly.");
  lines.push("- Frozen prototype files were extracted from the committed design asset SHA into `evidence/frozen-prototype` for same-browser comparison.");
  lines.push("");
  lines.push("## Raw Evidence");
  lines.push("- `evidence/verdict.json`");
  lines.push("- `evidence/raw/environment.json`");
  lines.push("- `evidence/raw/server.log`");
  lines.push("- `evidence/raw/real-{100,500,2200}/trace.zip`, `network.har`, `page.webm`, `console.json`, `network.json`, `result.json`, API JSON");
  lines.push("- `evidence/raw/prototype-{100,500,2200}/trace.zip`, `network.har`, `page.webm`, `console.json`, `result.json`");
  return lines.join("\n") + "\n";
}

async function json(respPromise, label) {
  const resp = await respPromise;
  const text = await resp.text();
  if (!resp.ok()) throw new Error(`${label}: HTTP ${resp.status()} ${text.slice(0, 500)}`);
  return text ? JSON.parse(text) : {};
}

async function ok(respPromise, label) {
  await json(respPromise, label);
}

function relative(path) {
  return path.replace(REPO + "/", "");
}

main().catch(async (err) => {
  await mkdir(RAW, { recursive: true });
  await writeFile(join(RAW, "infrastructure-error.log"), `${err.stack || err.message || err}\n\n--- server log ---\n${serverLog}`).catch(() => {});
  console.error(err);
  process.exit(1);
});
