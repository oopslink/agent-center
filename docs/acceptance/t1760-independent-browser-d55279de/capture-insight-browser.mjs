import { createRequire } from "node:module";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmod, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const evidenceRepo = process.cwd();
const candidateRepo = process.env.CANDIDATE_ROOT ? resolve(process.env.CANDIDATE_ROOT) : evidenceRepo;
const require = createRequire(join(candidateRepo, "tests/e2e/v2/package.json"));
const { chromium, request: pwRequest } = require("@playwright/test");
const out = resolve(evidenceRepo, "docs/acceptance/t1760-independent-browser-d55279de");
const screenshots = join(out, "screenshots");
const apiDir = join(out, "api");
const rawDir = join(out, "raw");
await mkdir(screenshots, { recursive: true });
await mkdir(apiDir, { recursive: true });
await mkdir(rawDir, { recursive: true });

const candidate = "d55279debfa874ecaeff90eaac020aa62d8a7a2e";
const bin = resolve(candidateRepo, "bin/agent-center");
const tempDir = await import("node:fs/promises").then((fs) => fs.mkdtemp(join(tmpdir(), "t1760-insight-browser-")));
const webPort = 18000 + Math.floor(Math.random() * 20000);
const grpcPort = webPort + 1;
const configPath = join(tempDir, "config.yaml");
const dbPath = join(tempDir, "agent-center.db");
const sockPath = join(tempDir, "admin.sock");
const masterKeyPath = join(tempDir, "master.key");
await writeFile(masterKeyPath, randomBytes(32).toString("base64") + "\n", "utf8");
await chmod(masterKeyPath, 0o600);
await writeFile(configPath, `
server:
  listen_addr: ":${grpcPort}"
  sqlite_path: "${dbPath}"
  admin_socket_path: "${sockPath}"
web_console:
  enabled: true
  listen_addr: "127.0.0.1:${webPort}"
secret_management:
  master_key_file: "${masterKeyPath}"
`, "utf8");

const proc = spawn(bin, ["server", "--config", configPath], {
  stdio: ["ignore", "pipe", "pipe"],
  env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
});
const stdout = [];
const stderr = [];
proc.stdout.on("data", (c) => stdout.push(c));
proc.stderr.on("data", (c) => stderr.push(c));

const baseURL = `http://127.0.0.1:${webPort}`;
async function waitReady() {
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    try {
      const r = await fetch(baseURL + "/api/health");
      if (r.ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("server did not become ready");
}

const raw = [];
async function saveAPI(name, response, bodyText) {
  const entry = {
    name,
    url: response.url(),
    status: response.status(),
    headers: response.headers(),
    body: tryJSON(bodyText),
  };
  raw.push(entry);
  await writeFile(join(apiDir, `${name}.json`), JSON.stringify(entry, null, 2));
  return entry;
}
function tryJSON(text) {
  try { return JSON.parse(text); } catch { return text; }
}
function emptySummary() {
  return {
    completed_executions: 0,
    failed_executions: 0,
    failure_rate: null,
    slot_utilization: null,
    slot_coverage_ratio: null,
    queue_wait_ms: { p50: null, p95: null, samples: 0 },
    execution_duration_ms: { p50: null, p95: null, samples: 0 },
  };
}
const envelope = {
  window: { kind: "rolling", duration: "24h", start: "2026-08-28T00:00:00Z", end: "2026-08-29T00:00:00Z" },
  as_of: "2026-08-29T00:00:00Z",
  refreshed_at: "2026-08-29T00:00:01Z",
  freshness: { state: "fresh", age_ms: 1200, threshold_ms: 30000 },
};
const summary = {
  completed_executions: 3,
  failed_executions: 1,
  failure_rate: 0.3333333333,
  slot_utilization: 0.5,
  slot_coverage_ratio: 0.75,
  queue_wait_ms: { p50: 250, p95: 900, samples: 3 },
  execution_duration_ms: { p50: 4000, p95: 10000, samples: 3 },
};
const execution = {
  execution_id: "exec-browser-1",
  command_id: "cmd-browser-1",
  task_id: "task-browser-1",
  task_ref: "task-browser-1",
  task_title: "Browser acceptance task",
  agent_ref: "agent:browser-agent",
  agent_name: "Browser Agent",
  project_id: "proj-browser-1",
  project_name: "Browser Project",
  worker_id: "worker-browser-1",
  outcome: "failed",
  failure_reason: "exit 1",
  queued_at: "2026-08-28T23:59:00Z",
  started_at: "2026-08-28T23:59:01Z",
  finished_at: "2026-08-28T23:59:06Z",
  queue_wait_ms: 1000,
  duration_ms: 5000,
  recovered: false,
  quality: "valid",
};
const overviewFull = {
  ...envelope,
  summary,
  agents: [{ agent_ref: "agent:browser-agent", display_name: "Browser Agent", summary }],
  projects: [{ project_id: "proj-browser-1", name: "Browser Project", summary }],
  diagnostics: { invalid_facts: 0, late_events: 0 },
};

const checks = [];
function pass(name, extra = {}) { checks.push({ name, status: "pass", ...extra }); }
function fail(name, extra = {}) { checks.push({ name, status: "fail", ...extra }); }
async function settle(page) {
  await page.waitForLoadState("domcontentloaded").catch(() => {});
  await page.waitForTimeout(500);
}
async function waitForAnyTestId(page, ids, timeout = 10000) {
  const selectors = ids.map((id) => `[data-testid="${id}"]`).join(",");
  await page.locator(selectors).first().waitFor({ state: "visible", timeout });
}

try {
  await waitReady();
  const req = await pwRequest.newContext();
  const version = await req.get(baseURL + "/api/system/version");
  const versionText = await version.text();
  await writeFile(join(apiDir, "system-version.json"), JSON.stringify({ status: version.status(), body: tryJSON(versionText) }, null, 2));
  const versionJSON = JSON.parse(versionText);
  if (versionJSON.commit !== candidate.slice(0, 8)) throw new Error(`running commit ${versionJSON.commit} != ${candidate.slice(0, 8)}`);
  pass("running binary commit matches candidate", { commit: versionJSON.commit });

  const suffix = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  const signup = await req.post(baseURL + "/api/auth/signup", {
    data: {
      display_name: `browser-${suffix}`,
      passcode: "E2ePass1!",
      email: `browser-${suffix}@example.test`,
      organization_name: `Browser Org ${suffix}`,
    },
  });
  const signupText = await signup.text();
  await writeFile(join(apiDir, "signup-redacted.json"), JSON.stringify({ status: signup.status(), body: tryJSON(signupText) }, null, 2));
  if (signup.status() !== 201) throw new Error(`signup failed ${signup.status()} ${signupText}`);
  const signed = JSON.parse(signupText);
  const cookie = /ac_session=([^;]+)/.exec(signup.headers()["set-cookie"] || "")?.[1];
  if (!cookie) throw new Error("missing ac_session");
  const slug = signed.organization_slug;
  const orgURL = `${baseURL}/organizations/${slug}/insights/overview`;

  const freshResp = await req.get(`${baseURL}/api/orgs/${slug}/insights/overview?window=24h`, {
    headers: { Cookie: `ac_session=${cookie}` },
  });
  const freshText = await freshResp.text();
  await writeFile(join(apiDir, "fresh-org-real-overview.json"), JSON.stringify({ status: freshResp.status(), body: tryJSON(freshText) }, null, 2));
  if (freshResp.status() === 200) pass("fresh org real API returns overview without crashing backend");
  else fail("fresh org real API", { status: freshResp.status(), body: tryJSON(freshText) });

  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, baseURL });
  await context.addCookies([{ name: "ac_session", value: cookie, url: baseURL, httpOnly: true, sameSite: "Lax" }]);
  const page = await context.newPage();
  const browserErrors = [];
  page.on("pageerror", (e) => browserErrors.push(String(e)));
  page.on("console", (m) => {
    if (m.type() !== "error") return;
    const text = m.text();
    if (text.includes("Failed to load resource: the server responded with a status of 403")) return;
    if (text.includes("Failed to load resource: the server responded with a status of 503")) return;
    browserErrors.push(text);
  });

  await page.goto(orgURL);
  await waitForAnyTestId(page, ["insight-empty", "insight-unavailable", "insight-stale"]);
  await page.screenshot({ path: join(screenshots, "01-fresh-org-real-empty.png"), fullPage: true });
  if (await page.getByTestId("insight-empty").isVisible()) pass("fresh org UI shows user empty state");
  else fail("fresh org UI empty state missing", { text: await page.locator("body").innerText() });

  await page.route("**/api/orgs/*/insights/overview**", async (route) => {
    const body = { ...envelope, summary: { ...emptySummary(), queue_wait_ms: null, execution_duration_ms: undefined }, agents: null, projects: null, diagnostics: null };
    await writeFile(join(apiDir, "mock-null-collections-overview.json"), JSON.stringify({ status: 200, body }, null, 2));
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  await page.route("**/api/orgs/*/insights/executions**", async (route) => {
    const body = { ...envelope, executions: null, next_cursor: null };
    await writeFile(join(apiDir, "mock-null-collections-executions.json"), JSON.stringify({ status: 200, body }, null, 2));
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  await page.goto(orgURL + "?case=null-collections");
  await waitForAnyTestId(page, ["insight-empty"]);
  await page.screenshot({ path: join(screenshots, "02-null-collections-empty.png"), fullPage: true });
  const nullText = await page.locator("body").innerText();
  if (nullText.includes("No executions") && nullText.includes("No agent executions") && nullText.includes("No project executions")) pass("null collections render empty states");
  else fail("null collections empty state", { text: nullText });
  await page.getByRole("button", { name: "Execution details" }).click();
  await waitForAnyTestId(page, ["insight-drilldown-empty"]);
  await page.screenshot({ path: join(screenshots, "03-null-drilldown-empty.png"), fullPage: true });
  if ((await page.locator("body").innerText()).includes("No matching execution attempts")) pass("null drilldown collection renders empty state");
  else fail("null drilldown empty state missing");
  await page.unroute("**/api/orgs/*/insights/overview**");
  await page.unroute("**/api/orgs/*/insights/executions**");

  await page.route("**/api/orgs/*/insights/overview**", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(overviewFull) }));
  const executionRequests = [];
  await page.route("**/api/orgs/*/insights/executions?**", async (route) => {
    const url = new URL(route.request().url());
    const body = { ...envelope, executions: [execution], next_cursor: null };
    executionRequests.push({ url: route.request().url(), params: Object.fromEntries(url.searchParams.entries()), response: body });
    await writeFile(join(apiDir, `mock-executions-${executionRequests.length}.json`), JSON.stringify(executionRequests.at(-1), null, 2));
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  await page.route("**/api/orgs/*/insights/executions/exec-browser-1**", async (route) => {
    const body = { ...envelope, execution };
    await writeFile(join(apiDir, "mock-execution-detail.json"), JSON.stringify({ status: 200, url: route.request().url(), body }, null, 2));
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  await page.goto(orgURL + "?case=full");
  await waitForAnyTestId(page, ["insight-summary"]);
  await page.screenshot({ path: join(screenshots, "04-global-overview.png"), fullPage: true });
  if ((await page.getByTestId("insight-summary").innerText()).includes("3")) pass("global overview summary rendered");
  await page.getByRole("button", { name: "Browser Agent" }).click();
  await waitForAnyTestId(page, ["insight-execution-row"]);
  await page.screenshot({ path: join(screenshots, "05-agent-drilldown.png"), fullPage: true });
  await page.getByRole("button", { name: "Browser Project" }).click();
  await waitForAnyTestId(page, ["insight-execution-row"]);
  await page.screenshot({ path: join(screenshots, "06-project-drilldown.png"), fullPage: true });
  const agentReq = executionRequests.find((r) => r.params.agent_ref === "agent:browser-agent");
  const projectReq = executionRequests.find((r) => r.params.project_id === "proj-browser-1");
  if (agentReq?.params.window === "24h") pass("agent scope/window API-UI reconciliation", { params: agentReq.params });
  else fail("agent scope/window reconciliation", { requests: executionRequests });
  if (projectReq?.params.window === "24h" && !("agent_ref" in projectReq.params)) pass("project scope/window API-UI reconciliation", { params: projectReq.params });
  else fail("project scope/window reconciliation", { requests: executionRequests });
  await page.getByRole("link", { name: "exec-browser-1" }).click();
  await waitForAnyTestId(page, ["insight-execution-detail"]);
  await page.screenshot({ path: join(screenshots, "07-execution-detail-identity-status-time.png"), fullPage: true });
  const detailText = await page.locator("body").innerText();
  if (detailText.includes("exec-browser-1") && detailText.includes("failed") && detailText.includes("Browser Agent") && detailText.includes("Browser Project") && detailText.includes("2026")) {
    pass("execution detail identity/status/time rendered");
  } else {
    fail("execution detail identity/status/time", { text: detailText });
  }
  await page.unroute("**/api/orgs/*/insights/overview**");
  await page.unroute("**/api/orgs/*/insights/executions?**");
  await page.unroute("**/api/orgs/*/insights/executions/exec-browser-1**");

  await page.route("**/api/orgs/*/insights/overview**", (route) => route.fulfill({ status: 200, contentType: "application/json", body: "null" }));
  await writeFile(join(apiDir, "mock-null-overview-response.json"), JSON.stringify({ status: 200, body: null }, null, 2));
  await page.goto(orgURL + "?case=null-overview");
  await waitForAnyTestId(page, ["insight-empty"]);
  await page.screenshot({ path: join(screenshots, "08-null-overview-empty.png"), fullPage: true });
  const nullOverviewText = await page.locator("body").innerText();
  if (nullOverviewText.includes("Past 24 hours") && nullOverviewText.includes("unknown") && nullOverviewText.includes("No executions")) pass("200 null overview response renders unknown empty window");
  else fail("200 null overview response", { text: nullOverviewText });
  await page.unroute("**/api/orgs/*/insights/overview**");

  await page.route("**/api/orgs/*/insights/overview**", (route) => route.fulfill({ status: 403, contentType: "application/json", body: JSON.stringify({ error: "forbidden", message: "no insight permission" }) }));
  await writeFile(join(apiDir, "mock-403-overview.json"), JSON.stringify({ status: 403, body: { error: "forbidden", message: "no insight permission" } }, null, 2));
  await page.goto(orgURL + "?case=403");
  await waitForAnyTestId(page, ["insight-auth-error"], 15000);
  await page.screenshot({ path: join(screenshots, "09-forbidden-403.png"), fullPage: true });
  if ((await page.locator("body").innerText()).includes("not authorized")) pass("403 renders authorization empty/error state");
  else fail("403 state missing");
  await page.unroute("**/api/orgs/*/insights/overview**");

  await page.route("**/api/orgs/*/insights/overview**", (route) => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "insight_rebuilding", message: "rebuilding", ...envelope, freshness: { state: "rebuilding", age_ms: 0, threshold_ms: 30000 } }) }));
  await writeFile(join(apiDir, "mock-503-rebuilding-overview.json"), JSON.stringify({ status: 503, body: { error: "insight_rebuilding", message: "rebuilding", ...envelope, freshness: { state: "rebuilding", age_ms: 0, threshold_ms: 30000 } } }, null, 2));
  await page.goto(orgURL + "?case=503");
  await waitForAnyTestId(page, ["insight-rebuilding"], 15000);
  await page.screenshot({ path: join(screenshots, "10-rebuilding-503.png"), fullPage: true });
  if ((await page.locator("body").innerText()).includes("rebuilding")) pass("503 rebuilding envelope renders dedicated state");
  else fail("503 rebuilding state missing");

  if (browserErrors.length === 0) pass("browser console/page errors clean");
  else fail("browser console/page errors", { browserErrors });

  await browser.close();
  await req.dispose();
} finally {
  await writeFile(join(rawDir, "server-stdout.log"), Buffer.concat(stdout).toString("utf8"));
  await writeFile(join(rawDir, "server-stderr.log"), Buffer.concat(stderr).toString("utf8"));
  if (proc.exitCode === null) {
    proc.kill("SIGTERM");
    await new Promise((resolve) => setTimeout(resolve, 1000));
    if (proc.exitCode === null) proc.kill("SIGKILL");
  }
}

const verdict = {
  candidate,
  checked_at_utc: new Date().toISOString(),
  verdict: checks.every((c) => c.status === "pass") ? "PASS" : "REJECT",
  checks,
};
await writeFile(join(out, "verdict.json"), JSON.stringify(verdict, null, 2));
if (verdict.verdict !== "PASS") {
  console.error(JSON.stringify(verdict, null, 2));
  process.exit(1);
}
console.log(JSON.stringify(verdict, null, 2));
