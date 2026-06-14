// v2.9.1 acceptance screenshot capture — boots a real agent-center instance,
// seeds a realistic scenario via the same /api the Web Console uses (so this is
// a true user-perspective walk), then drives the SPA in a real browser and
// captures key-step PNGs into docs/release/evidence/v2.9.1-screenshots/.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, chmod, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { randomBytes } from "node:crypto";

const REPO = "/Users/oopslink/works/codes/oopslink/ac-wt-v291";
const BIN = resolve(REPO, "bin/agent-center");
const OUT = resolve(REPO, "docs/release/evidence/v2.9.1-screenshots");
const WEB = 7799;
const GRPC = 7798;
const BASE = `http://127.0.0.1:${WEB}`;
const API = `${BASE}/api`;
const SLUG = "acme";
const ORG = `${API}/orgs/${SLUG}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log("[cap]", ...a);

const results = [];
async function shot(page, name, locator) {
  try {
    const path = join(OUT, name + ".png");
    if (locator) await locator.screenshot({ path });
    else await page.screenshot({ path, fullPage: false });
    results.push([name, "OK"]);
    log("shot OK", name);
  } catch (e) {
    results.push([name, "FAIL " + String(e).slice(0, 120)]);
    log("shot FAIL", name, String(e).slice(0, 160));
  }
}

async function main() {
  await mkdir(OUT, { recursive: true });
  const tempDir = await mkdtemp(join(tmpdir(), "ac-v291-cap-"));
  const dbPath = join(tempDir, "ac.db");
  const sockPath = join(tempDir, "admin.sock");
  const masterKeyPath = join(tempDir, "master.key");
  await writeFile(masterKeyPath, randomBytes(32).toString("base64") + "\n", "utf8");
  await chmod(masterKeyPath, 0o600);
  const configPath = join(tempDir, "config.yaml");
  await writeFile(
    configPath,
    `server:\n  listen_addr: ":${GRPC}"\n  sqlite_path: "${dbPath}"\n  admin_socket_path: "${sockPath}"\nweb_console:\n  enabled: true\n  listen_addr: "127.0.0.1:${WEB}"\nsecret_management:\n  master_key_file: "${masterKeyPath}"\nblob_store:\n  root: "${join(tempDir, "blobs")}"\n`,
    "utf8",
  );

  const proc = spawn(BIN, ["server", "--config", configPath], {
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" },
  });
  const errBuf = [];
  proc.stderr?.on("data", (c) => errBuf.push(c));
  proc.stdout?.on("data", (c) => errBuf.push(c));

  // wait for health
  let up = false;
  for (let i = 0; i < 80; i++) {
    try {
      const r = await fetch(API + "/health");
      if (r.ok) { up = true; break; }
    } catch {}
    await sleep(100);
  }
  if (!up) {
    log("server did not come up:\n" + Buffer.concat(errBuf).toString().slice(-1500));
    proc.kill("SIGKILL");
    process.exit(1);
  }
  log("server up on", BASE);

  const browser = await chromium.launch();
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2,
  });
  const rq = ctx.request;
  const J = async (resp, label) => {
    const t = await resp.text();
    if (!resp.ok()) throw new Error(`${label} -> ${resp.status()} ${t.slice(0, 200)}`);
    try { return JSON.parse(t); } catch { return {}; }
  };

  // ---- signup (creates user + org + session cookie in this context) ----
  const su = await rq.post(API + "/auth/signup", {
    data: {
      display_name: "alice",
      passcode: "Acme-pass1!",
      organization_name: "Acme",
      organization_slug: SLUG,
      email: "alice@acme.test",
    },
  });
  const suj = await J(su, "signup");
  const aliceId = suj.identity_id;
  log("signed up alice", aliceId, "org", suj.organization_id);

  // Explicit signin → grab Set-Cookie → inject into the context so both
  // ctx.request (seeding) and pages (navigation) carry the session.
  const si = await rq.post(API + "/auth/signin", { data: { display_name: "alice", passcode: "Acme-pass1!" } });
  const setCookie = si.headers()["set-cookie"] || "";
  const mck = /ac_session=([^;]+)/.exec(setCookie);
  if (!mck) throw new Error("no ac_session in signin Set-Cookie: " + setCookie.slice(0, 120));
  await ctx.addCookies([{ name: "ac_session", value: mck[1], domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  log("session cookie injected");

  // ---- seed: project ----
  const proj = await J(
    await rq.post(ORG + "/projects", { data: { name: "Phoenix", description: "v2.9.1 dogfood project" } }),
    "project",
  );
  const pid = proj.id || proj.project_id;
  log("project", pid);

  // ---- seed: channel + threaded messages ----
  const chan = await J(
    await rq.post(ORG + "/conversations", { data: { kind: "channel", name: "general", description: "team channel" } }),
    "channel",
  );
  const cid = chan.conversation_id || chan.id;
  log("channel", cid);
  const postMsg = async (content, parent) => {
    const body = { content };
    if (parent) body.parent_message_id = parent;
    const m = await J(await rq.post(ORG + `/conversations/${cid}/messages`, { data: body }), "msg");
    return m.message_id || m.id;
  };
  const root1 = await postMsg("Shipping v2.9.1 today — who can take the release-notes pass?");
  await postMsg("I can take it — will draft CHANGELOG + README highlights.", root1);
  await postMsg("Great, I'll review once it's up.", root1);
  const root2 = await postMsg("Heads up: the work board now has three segments (backlog / assignment pool / plans).");
  await postMsg("Nice — the assignment pool is exactly what we needed for pull-mode claims.", root2);
  log("threads seeded root1", root1, "root2", root2);

  // ---- seed: tasks (backlog + builtin pool + structured plan) ----
  const mkTask = async (title, desc) => {
    const t = await J(await rq.post(ORG + `/projects/${pid}/tasks`, { data: { title, description: desc || title } }), "task");
    return t.id || t.task_id;
  };
  const tBacklog1 = await mkTask("Backlog: investigate SSE reconnect jitter");
  const tBacklog2 = await mkTask("Backlog: polish empty-states copy");
  const tPool1 = await mkTask("Pool: triage incoming bug reports");
  const tPool2 = await mkTask("Pool: weekly dependency bump");
  const tp1 = await mkTask("Write release notes");
  const tp2 = await mkTask("Cut release branch");
  const tp3 = await mkTask("Deploy + smoke");

  // builtin assignment pool: find the is_builtin plan, add + assign (→ claimable)
  const plans0 = await J(await rq.get(ORG + `/projects/${pid}/plans`), "plans");
  const planList0 = Array.isArray(plans0) ? plans0 : plans0.plans || [];
  const pool = planList0.find((p) => p.is_builtin === true);
  log("builtin pool", pool && (pool.id || pool.plan_id));
  if (pool) {
    const poolId = pool.id || pool.plan_id;
    for (const tid of [tPool1, tPool2]) {
      try { await J(await rq.post(ORG + `/projects/${pid}/plans/${poolId}/tasks`, { data: { task_id: tid } }), "pool-add"); } catch (e) { log("pool-add err", String(e).slice(0, 120)); }
      try { await rq.post(ORG + `/projects/${pid}/tasks/${tid}/assign`, { data: { assignee: aliceId } }); } catch (e) { log("assign err", String(e).slice(0, 120)); }
    }
  }

  // structured plan with a DAG
  const plan = await J(await rq.post(ORG + `/projects/${pid}/plans`, { data: { name: "Release v2.9.1", description: "ship the release" } }), "plan");
  const planId = plan.id || plan.plan_id;
  log("structured plan", planId);
  for (const tid of [tp1, tp2, tp3]) {
    try { await J(await rq.post(ORG + `/projects/${pid}/plans/${planId}/tasks`, { data: { task_id: tid } }), "plan-add"); } catch (e) { log("plan-add err", String(e).slice(0, 120)); }
  }
  try { await rq.post(ORG + `/projects/${pid}/plans/${planId}/dependencies`, { data: { from_task_id: tp2, to_task_id: tp1 } }); } catch (e) { log("dep err", String(e).slice(0, 120)); }
  try { await rq.post(ORG + `/projects/${pid}/plans/${planId}/dependencies`, { data: { from_task_id: tp3, to_task_id: tp2 } }); } catch (e) { log("dep err", String(e).slice(0, 120)); }
  try { await rq.post(ORG + `/projects/${pid}/tasks/${tp1}/assign`, { data: { assignee: aliceId } }); } catch {}
  try { await rq.post(ORG + `/projects/${pid}/plans/${planId}/start`); } catch (e) { log("start err", String(e).slice(0, 120)); }

  // ---- seed: an archived channel ----
  const chan2 = await J(await rq.post(ORG + "/conversations", { data: { kind: "channel", name: "old-incidents", description: "retired channel" } }), "channel2");
  const cid2 = chan2.conversation_id || chan2.id;
  const arResp = await rq.post(ORG + `/conversations/${cid2}/archive`, { data: {} });
  log("archive status", arResp.status(), (await arResp.text()).slice(0, 160));

  // ============ CAPTURES ============
  const page = await ctx.newPage();
  const errs = [];
  page.on("console", (m) => { if (m.type() === "error") errs.push(m.text()); });
  const go = async (path) => { await page.goto(BASE + path, { waitUntil: "domcontentloaded" }); await sleep(1200); };
  const setTheme = async (t) => { await page.addInitScript((v) => localStorage.setItem("ac.theme", v), t); };

  // --- A. Thread (light) ---
  await setTheme("light");
  await go(`/organizations/${SLUG}/channels/${cid}`);
  try { await page.getByTestId("thread-button").first().waitFor({ timeout: 8000 }); } catch {}
  await shot(page, "A1_channel_threads");

  // open the first thread that has replies
  try {
    const tb = page.getByTestId("thread-button").first();
    await tb.click();
    await page.getByTestId("thread-sidebar").waitFor({ timeout: 6000 });
    await sleep(500);
    await shot(page, "A2_thread_sidebar");
  } catch (e) { log("thread sidebar open failed", String(e).slice(0, 140)); results.push(["A2_thread_sidebar", "FAIL " + String(e).slice(0, 80)]); }

  // thread list in participants panel (try to reveal it)
  try {
    const close = page.getByTestId("thread-sidebar-close");
    if (await close.count()) await close.click();
  } catch {}
  try {
    const tl = page.getByTestId("thread-list");
    if (!(await tl.count())) {
      // try a participants toggle button if the panel is collapsed
      const cand = page.getByRole("button", { name: /participant|details|info/i }).first();
      if (await cand.count()) { await cand.click(); await sleep(400); }
    }
    await page.getByTestId("thread-list").waitFor({ timeout: 4000 });
    await shot(page, "A3_thread_list", page.getByTestId("thread-list"));
  } catch (e) { log("thread-list not found", String(e).slice(0, 120)); results.push(["A3_thread_list", "FAIL " + String(e).slice(0, 80)]); await shot(page, "A3_channel_fullpage"); }

  // --- C. Work board (3 segments) ---
  await go(`/organizations/${SLUG}/projects/${pid}/plans`);
  try { await page.getByTestId("work-board").waitFor({ timeout: 8000 }); } catch {}
  await sleep(400);
  await shot(page, "C1_work_board");

  // --- H. Plan detail tabs + DAG ---
  await go(`/organizations/${SLUG}/projects/${pid}/plans/${planId}`);
  try { await page.getByTestId("plan-tabs").waitFor({ timeout: 8000 }); } catch {}
  await sleep(400);
  await shot(page, "H1_plan_chat_tab");
  try {
    await page.getByRole("tab", { name: /dag|graph/i }).click();
    await page.getByTestId("plan-dag").waitFor({ timeout: 5000 });
    await sleep(500);
    await shot(page, "H2_plan_dag");
  } catch (e) { log("dag tab fail", String(e).slice(0, 120)); results.push(["H2_plan_dag", "FAIL " + String(e).slice(0, 80)]); }
  try {
    await page.getByRole("tab", { name: /task/i }).click();
    await sleep(500);
    await shot(page, "H3_plan_tasks");
  } catch (e) { results.push(["H3_plan_tasks", "FAIL " + String(e).slice(0, 80)]); }

  // --- D. Org tasks list (backlog excludes terminal) ---
  await go(`/organizations/${SLUG}/tasks`);
  await sleep(400);
  await shot(page, "D1_org_tasks");

  // --- G. Channels archived group ---
  await go(`/organizations/${SLUG}/channels`);
  await sleep(400);
  try {
    const arch = page.getByRole("button", { name: /archived/i }).first();
    if (await arch.count()) { await arch.click(); await sleep(600); }
  } catch {}
  await shot(page, "G1_channels_archived");

  // --- I. Dark mode (both-mode) ---
  await setTheme("dark");
  await go(`/organizations/${SLUG}/channels/${cid}`);
  try { await page.getByTestId("thread-button").first().waitFor({ timeout: 6000 }); } catch {}
  await shot(page, "I1_dark_channel");
  await go(`/organizations/${SLUG}/projects/${pid}/plans`);
  try { await page.getByTestId("work-board").waitFor({ timeout: 6000 }); } catch {}
  await sleep(300);
  await shot(page, "I2_dark_work_board");

  log("CONSOLE ERRORS:", errs.length);
  errs.slice(0, 10).forEach((e) => log("  err:", e.slice(0, 160)));
  log("==== RESULTS ====");
  results.forEach(([n, s]) => log(`  ${s.startsWith("OK") ? "✅" : "❌"} ${n}: ${s}`));

  await browser.close();
  proc.kill("SIGTERM");
  await sleep(800);
  if (!proc.killed) proc.kill("SIGKILL");
  await rm(tempDir, { recursive: true, force: true });
}

main().catch((e) => { console.error("FATAL", e); process.exit(1); });
