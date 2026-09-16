import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const require = createRequire(import.meta.url);
const { chromium } = require("../../tests/e2e/v2/node_modules/@playwright/test");
const repo = resolve(new URL("../..", import.meta.url).pathname);
const bin = resolve(repo, "bin/agent-center");
const outDir = resolve(repo, "docs/acceptance/i166-evidence");
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function freePort() {
  const net = await import("node:net");
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close(() => resolve(address.port));
    });
  });
}

async function json(resp, label) {
  const text = await resp.text();
  if (!resp.ok()) throw new Error(`${label}: HTTP ${resp.status()} ${text.slice(0, 300)}`);
  return text ? JSON.parse(text) : {};
}

async function waitForServer(baseURL) {
  let last = "";
  for (let i = 0; i < 100; i++) {
    try {
      const resp = await fetch(`${baseURL}/`);
      if (resp.ok) return;
      last = `HTTP ${resp.status()}`;
    } catch (err) {
      last = String(err);
    }
    await sleep(100);
  }
  throw new Error(`server did not become ready: ${last}`);
}

async function main() {
  await mkdir(outDir, { recursive: true });
  const tempDir = await mkdtemp(join(tmpdir(), "i166-dag-"));
  const webPort = await freePort();
  const grpcPort = await freePort();
  const dbPath = join(tempDir, "agent-center-i166.db");
  const sockPath = join(tempDir, "admin.sock");
  const masterKeyPath = join(tempDir, "master.key");
  const blobRoot = join(tempDir, "blobs");
  await writeFile(masterKeyPath, `${randomBytes(32).toString("base64")}\n`, "utf8");
  await chmod(masterKeyPath, 0o600);
  const configPath = join(tempDir, "config.yaml");
  await writeFile(configPath, `server:
  listen_addr: ":${grpcPort}"
  sqlite_path: "${dbPath}"
  admin_socket_path: "${sockPath}"
web_console:
  enabled: true
  listen_addr: "127.0.0.1:${webPort}"
secret_management:
  master_key_file: "${masterKeyPath}"
blob_store:
  root: "${blobRoot}"
`, "utf8");

  const baseURL = `http://127.0.0.1:${webPort}`;
  const apiURL = `${baseURL}/api`;
  const proc = spawn(bin, ["server", "--config", configPath], {
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "i166-dag-acceptance" },
  });
  const logs = [];
  proc.stdout.on("data", (chunk) => logs.push(chunk));
  proc.stderr.on("data", (chunk) => logs.push(chunk));

  const evidence = {
    provenance: {
      candidate_sha: "c24623332d4c4ab0edafce5d12021db532672bda",
      base_sha: "ee9eab20c82083ae526f89ecf3907846a03c5dd7",
      started_at: new Date().toISOString(),
      base_url: baseURL,
      config_path: configPath,
      db_path: dbPath,
      temp_dir: tempDir,
      binary: bin,
      web_port: webPort,
      grpc_port: grpcPort,
      session_cookie_namespace: "ac_session on 127.0.0.1 isolated browser context",
    },
    plans: {},
    checks: [],
    screenshots: [],
    console_errors: [],
  };

  const record = (name, ok, data = {}) => evidence.checks.push({ name, ok, ...data });
  let browser;
  try {
    await waitForServer(baseURL);
    browser = await chromium.launch();
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
    const req = context.request;
    const suffix = Date.now().toString(36);
    const requestedSlug = `i166-${suffix}`;
    const passcode = "I166Pass1!";
    const signup = await json(await req.post(`${apiURL}/auth/signup`, {
      data: {
        display_name: `i166-owner-${suffix}`,
        email: `i166-${suffix}@example.test`,
        passcode,
        organization_name: `I166 ${suffix}`,
        organization_slug: requestedSlug,
      },
    }), "signup");
    const slug = signup.organization_slug || requestedSlug;
    const signin = await req.post(`${apiURL}/auth/signin`, {
      data: { display_name: `i166-owner-${suffix}`, passcode },
    });
    const cookie = /ac_session=([^;]+)/.exec(signin.headers()["set-cookie"] || "")?.[1];
    if (!cookie) throw new Error("signin did not set ac_session");
    await context.addCookies([{ name: "ac_session", value: cookie, domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
    const ownerRef = `user:${signup.identity_id}`;
    const org = `${apiURL}/orgs/${slug}`;
    const project = await json(await req.post(`${org}/projects`, {
      data: { name: `I166 DAG ${suffix}`, description: "isolated Plan DAG acceptance data" },
    }), "create project");
    const projectId = project.id || project.project_id;

    async function createTask(title) {
      const task = await json(await req.post(`${org}/projects/${projectId}/tasks`, {
        data: { title, description: title },
      }), `create task ${title}`);
      return task.id || task.task_id;
    }

    const longTitle = "A very long DAG node title that should wrap or truncate without resizing the ELK layout or overlapping neighbouring nodes";
    const a = await createTask("ordinary root A");
    const b = await createTask("ordinary root B");
    const c = await createTask("branch join C");
    const d = await createTask(longTitle);
    const e = await createTask("tail E");
    for (const taskId of [a, b, c, d, e]) {
      await json(await req.post(`${org}/projects/${projectId}/tasks/${taskId}/assign`, { data: { assignee: ownerRef } }), `assign ${taskId}`);
    }
    const plan = await json(await req.post(`${org}/projects/${projectId}/plans`, {
      data: { name: "I166 graph-backed branch join", description: "cross branch, join, long title" },
    }), "create graph plan");
    const planId = plan.id || plan.plan_id;
    for (const taskId of [a, b, c, d, e]) {
      await json(await req.post(`${org}/projects/${projectId}/plans/${planId}/tasks`, { data: { task_id: taskId } }), `add ${taskId}`);
    }
    await json(await req.post(`${org}/projects/${projectId}/plans/${planId}/dependencies`, { data: { from_task_id: c, to_task_id: a } }), "dep c<-a");
    await json(await req.post(`${org}/projects/${projectId}/plans/${planId}/dependencies`, { data: { from_task_id: c, to_task_id: b } }), "dep c<-b");
    await json(await req.post(`${org}/projects/${projectId}/plans/${planId}/dependencies`, { data: { from_task_id: d, to_task_id: c } }), "dep d<-c");
    await json(await req.post(`${org}/projects/${projectId}/plans/${planId}/dependencies`, { data: { from_task_id: e, to_task_id: d } }), "dep e<-d");

    const singleTask = await createTask("single node task");
    const singlePlan = await json(await req.post(`${org}/projects/${projectId}/plans`, {
      data: { name: "I166 single node", description: "single node coverage" },
    }), "create single plan");
    const singlePlanId = singlePlan.id || singlePlan.plan_id;
    await json(await req.post(`${org}/projects/${projectId}/plans/${singlePlanId}/tasks`, { data: { task_id: singleTask } }), "add single task");

    const emptyPlan = await json(await req.post(`${org}/projects/${projectId}/plans`, {
      data: { name: "I166 empty plan", description: "empty graph coverage" },
    }), "create empty plan");
    const emptyPlanId = emptyPlan.id || emptyPlan.plan_id;

    await json(await req.post(`${org}/projects/${projectId}/plans/${planId}/start`, { data: {} }), "start graph plan");
    const graphRead = await json(await req.get(`${org}/projects/${projectId}/plans/${planId}/graph`), "graph read");
    const stagesRead = await json(await req.get(`${org}/projects/${projectId}/plans/${planId}/stages`), "stages read");
    evidence.plans.graph_backed = { id: planId, node_count: graphRead.nodes?.length ?? 0, edge_count: graphRead.edges?.length ?? 0, has_graph: graphRead.has_graph };
    evidence.plans.single = { id: singlePlanId };
    evidence.plans.empty = { id: emptyPlanId };
    evidence.plans.stages_read = { count: stagesRead.stages?.length ?? 0, note: "No Web API creates stages; create_stage is MCP/admin-tool only in this executor." };

    const page = await context.newPage();
    page.on("console", (msg) => {
      if (msg.type() === "error") evidence.console_errors.push(msg.text());
    });

    async function screenshot(name, fullPage = false) {
      const file = `${name}.png`;
      await page.screenshot({ path: join(outDir, file), fullPage });
      evidence.screenshots.push(file);
    }

    await page.goto(`${baseURL}/organizations/${slug}/projects/${projectId}/plans/${planId}`, { waitUntil: "domcontentloaded" });
    await page.getByTestId("plan-tab-dag").click();
    await page.getByTestId("plan-dag-reactflow").waitFor({ timeout: 15000 });
    await page.waitForTimeout(1200);
    await screenshot("graph-backed-desktop-light");
    const nodeCount = await page.locator(".react-flow__node").count();
    const edgeCount = await page.locator(".react-flow__edges path").count();
    const boxes = await page.locator(".react-flow__node").evaluateAll((els) => els.map((el) => {
      const r = el.getBoundingClientRect();
      return { x: r.x, y: r.y, w: r.width, h: r.height, text: el.textContent };
    }));
    const overlaps = [];
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const p = boxes[i], q = boxes[j];
        if (p.w <= 1 || p.h <= 1 || q.w <= 1 || q.h <= 1) continue;
        if (p.x < q.x + q.w && p.x + p.w > q.x && p.y < q.y + q.h && p.y + p.h > q.y) {
          overlaps.push([i, j, p.text?.slice(0, 30), q.text?.slice(0, 30)]);
        }
      }
    }
    record("graph-backed React Flow rendered with no node overlap", overlaps.length === 0, { nodeCount, edgeCount, overlaps });
    record("graph read uses orchestration graph", graphRead.has_graph === true, { apiNodeCount: graphRead.nodes?.length ?? 0, apiEdgeCount: graphRead.edges?.length ?? 0 });

    await page.mouse.wheel(0, 500);
    await page.locator(".react-flow__controls-fitview").click();
    record("fit view control available", await page.locator(".react-flow__controls-fitview").count() === 1);
    await page.emulateMedia({ colorScheme: "dark" });
    await screenshot("graph-backed-desktop-dark");

    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByTestId("plan-tab-dag").click();
    await page.getByTestId("plan-graph-stepper").waitFor({ timeout: 15000 });
    await screenshot("graph-backed-mobile");
    record("mobile graph stepper rendered", await page.getByTestId("plan-graph-stepper").count() === 1);

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(`${baseURL}/organizations/${slug}/projects/${projectId}/plans/${singlePlanId}`, { waitUntil: "domcontentloaded" });
    await page.getByTestId("plan-tab-dag").click();
    await page.getByTestId("plan-dag-reactflow").waitFor({ timeout: 15000 });
    await page.waitForTimeout(800);
    await screenshot("legacy-single-node");
    record("legacy single node rendered", await page.locator(".react-flow__node").count().then((count) => count >= 3));
    record("pending legacy plan shows dependency edit controls", await page.getByTestId("plan-node-connect").count().then((count) => count >= 1));

    await page.goto(`${baseURL}/organizations/${slug}/projects/${projectId}/plans/${emptyPlanId}`, { waitUntil: "domcontentloaded" });
    await page.getByTestId("plan-tab-dag").click();
    await page.getByTestId("plan-dag-empty").waitFor({ timeout: 15000 });
    await screenshot("empty-plan");
    record("empty plan state rendered", await page.getByTestId("plan-dag-empty").count().then((count) => count === 1));

    evidence.provenance.finished_at = new Date().toISOString();
    await writeFile(join(outDir, "real-chain-results.json"), JSON.stringify(evidence, null, 2), "utf8");
  } finally {
    if (browser) await browser.close();
    if (proc.exitCode == null) {
      proc.kill("SIGTERM");
      await sleep(500);
      if (proc.exitCode == null) proc.kill("SIGKILL");
    }
    await writeFile(join(outDir, "server.log"), Buffer.concat(logs).toString("utf8"), "utf8");
  }
}

main().catch(async (err) => {
  await mkdir(outDir, { recursive: true });
  await writeFile(join(outDir, "real-chain-error.txt"), `${err.stack || err}\n`, "utf8");
  console.error(err);
  process.exit(1);
});
