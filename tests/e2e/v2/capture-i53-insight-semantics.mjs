// I53 Insight semantics real-page capture.
// Starts an isolated test-instance through the production install path, signs in
// through HTTP, waits for the Insight projector, then captures production pages.
import { chromium } from "@playwright/test";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";

const REPO = resolve(new URL("../../..", import.meta.url).pathname);
const BIN = process.env.AC_BIN || resolve(REPO, "bin/agent-center");
const ID = process.env.AC_ID || `i53-${Date.now().toString(36)}`;
const OUT = process.env.AC_OUT || "/tmp/i53-insight-semantics";
const KEEP = process.env.AC_KEEP_INSTANCE === "1";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...args) => console.log("[i53]", ...args);

async function main() {
  await mkdir(OUT, { recursive: true });
  const pack = spawnInstance();
  const base = pack.web_url;
  const api = `${base}/api`;
  const org = `${api}/orgs/${pack.signin.org_slug}`;

  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 1440, height: 980 },
    deviceScaleFactor: 1,
    recordVideo: { dir: OUT, size: { width: 1440, height: 980 } },
  });
  const request = context.request;
  const signin = await request.post(`${api}/auth/signin`, {
    data: {
      display_name: pack.signin.display_name,
      passcode: pack.signin.passcode,
      org_slug: pack.signin.org_slug,
    },
  });
  if (!signin.ok()) throw new Error(`signin ${signin.status()} ${await signin.text()}`);
  const cookie = /ac_session=([^;]+)/.exec(signin.headers()["set-cookie"] || "")?.[1];
  if (!cookie) throw new Error("signin did not return ac_session");
  await context.addCookies([{ name: "ac_session", value: cookie, domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);

  const consoleErrors = [];
  const page = await context.newPage();
  page.on("console", (msg) => { if (msg.type() === "error") consoleErrors.push(msg.text()); });
  page.on("pageerror", (err) => consoleErrors.push(`[pageerror] ${err.message}`));
  await page.addInitScript(() => localStorage.setItem("ac.theme", "light"));

  const overview = await waitForInsight(request, `${org}/insights/overview?window=24h`, "overview");
  const executions = await getJSON(request, `${org}/insights/executions?window=24h&limit=50`, "executions");
  const agents = await getJSON(request, `${org}/insights/v2/agents?window=24h`, "v2 agents").catch((err) => ({ error: String(err), agents: [] }));
  const projects = await getJSON(request, `${org}/insights/v2/projects?window=24h`, "v2 projects").catch((err) => ({ error: String(err), projects: [] }));

  const shots = [];
  async function shot(name, url, testId) {
    await page.goto(url, { waitUntil: "domcontentloaded" });
    await page.getByTestId(testId).waitFor({ timeout: 12_000 });
    await page.waitForTimeout(800);
    const path = join(OUT, `${name}.png`);
    await page.screenshot({ path, fullPage: true });
    shots.push({ name, path, url: page.url() });
    log("shot", name, page.url());
  }

  const slug = encodeURIComponent(pack.signin.org_slug);
  await shot("01-overview", `${base}/organizations/${slug}/insights/overview`, "page-InsightOverview");
  await shot("02-executions", `${base}/organizations/${slug}/insights/executions?window=24h`, "page-InsightExecutions");
  const firstExec = executions.executions?.[0]?.execution_id;
  if (firstExec) {
    await shot("03-execution-detail", `${base}/organizations/${slug}/insights/executions/${encodeURIComponent(firstExec)}?window=24h`, "page-InsightExecutionDetail");
  }
  await shot("04-agents", `${base}/organizations/${slug}/insights/agents`, "page-InsightAgents");
  await shot("05-projects", `${base}/organizations/${slug}/insights/projects`, "page-InsightProjects");

  const summary = {
    id: ID,
    binary: BIN,
    capture_mode: pack.i53_capture_mode,
    capture_fallback_reason: pack.i53_capture_fallback_reason,
    web_url: base,
    org_slug: pack.signin.org_slug,
    project_id: pack.entity_refs?.project_id,
    agent_id: pack.agent?.id,
    dispatched_task_id: pack.agent?.dispatched_task_id,
    overview_summary: overview.summary,
    overview_freshness: overview.freshness,
    executions_count: executions.executions?.length ?? 0,
    first_execution: executions.executions?.[0] ?? null,
    agents_count: Array.isArray(agents) ? agents.length : (Array.isArray(agents?.agents) ? agents.agents.length : 0),
    projects_count: Array.isArray(projects) ? projects.length : (Array.isArray(projects?.projects) ? projects.projects.length : 0),
    console_errors: consoleErrors,
    screenshots: shots,
  };
  await writeFile(join(OUT, "RESULTS.json"), JSON.stringify(summary, null, 2));
  log("summary", JSON.stringify({
    completed: overview.summary?.completed_executions,
    coverage: overview.summary?.slot_coverage_ratio,
    executions: summary.executions_count,
    consoleErrors: consoleErrors.length,
  }));

  await context.close();
  await browser.close();
  if (!KEEP) teardownInstance();
}

function spawnInstance() {
  log("install test-instance", ID);
  let mode = "with-agent";
  let fallback_reason = "";
  let out = "";
  try {
    out = execFileSync(BIN, ["install", "test-instance", "--id", ID, "--with-agent", "--workers", "1", "--output", "json"], { encoding: "utf8", timeout: 180_000 });
  } catch (err) {
    fallback_reason = String(err.stderr || err.message || err).slice(0, 1000);
    log("with-agent unavailable; falling back to with-seed", fallback_reason);
    teardownInstance();
    mode = "with-seed";
    out = execFileSync(BIN, ["install", "test-instance", "--id", ID, "--with-seed", "--workers", "1", "--output", "json"], { encoding: "utf8", timeout: 180_000 });
  }
  const pack = JSON.parse(out);
  pack.i53_capture_mode = mode;
  pack.i53_capture_fallback_reason = fallback_reason;
  log("started", pack.web_url, pack.signin?.org_slug);
  return pack;
}

function teardownInstance() {
  log("uninstall test-instance", ID);
  spawnSync(BIN, ["uninstall", "test-instance", "--id", ID], { encoding: "utf8", stdio: "inherit", timeout: 90_000 });
}

async function waitForInsight(request, url, label) {
  const deadline = Date.now() + 90_000;
  let last = "";
  while (Date.now() < deadline) {
    try {
      const data = await getJSON(request, url, label);
      if (data?.freshness?.state === "fresh") return data;
      last = JSON.stringify(data?.freshness || data).slice(0, 300);
    } catch (err) {
      last = String(err).slice(0, 300);
    }
    await sleep(2500);
  }
  throw new Error(`timed out waiting for ${label}: ${last}`);
}

async function getJSON(request, url, label) {
  const resp = await request.get(url);
  const text = await resp.text();
  if (!resp.ok()) throw new Error(`${label} ${resp.status()} ${text.slice(0, 400)}`);
  return JSON.parse(text);
}

main().catch(async (err) => {
  console.error("[i53] FATAL", err);
  if (!KEEP) teardownInstance();
  process.exit(1);
});
