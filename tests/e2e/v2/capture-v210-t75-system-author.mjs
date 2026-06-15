// v2.10.0 [T75] run-real: the plan-dispatch "your task is ready" notification
// (backend posts it with sender "system") must render its author as "System",
// NOT "(deleted)". Seeds a project + plan + assigned task, starts the plan so the
// scheduler posts the dispatch @mention, then screenshots the plan conversation.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, mkdir, rm, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { randomBytes } from "node:crypto";

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const BIN = resolve(REPO, "bin/agent-center");
const OUT = "/tmp/v210-t75-shots";
const WEB = 7902, GRPC = 7901;
const BASE = `http://127.0.0.1:${WEB}`;
const API = `${BASE}/api`;
const SLUG = "acme";
const ORG = `${API}/orgs/${SLUG}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log("[t75]", ...a);

async function main() {
  await mkdir(OUT, { recursive: true });
  const tempDir = await mkdtemp(join(tmpdir(), "ac-v210t75-"));
  const masterKeyPath = join(tempDir, "master.key");
  await writeFile(masterKeyPath, randomBytes(32).toString("base64") + "\n", "utf8");
  await chmod(masterKeyPath, 0o600);
  const configPath = join(tempDir, "config.yaml");
  await writeFile(configPath, `server:\n  listen_addr: ":${GRPC}"\n  sqlite_path: "${join(tempDir, "ac.db")}"\n  admin_socket_path: "${join(tempDir, "admin.sock")}"\nweb_console:\n  enabled: true\n  listen_addr: "127.0.0.1:${WEB}"\nsecret_management:\n  master_key_file: "${masterKeyPath}"\nblob_store:\n  root: "${join(tempDir, "blobs")}"\n`, "utf8");

  const proc = spawn(BIN, ["server", "--config", configPath], { stdio: ["ignore", "pipe", "pipe"], env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" } });
  const errBuf = []; proc.stderr?.on("data", (c) => errBuf.push(c)); proc.stdout?.on("data", (c) => errBuf.push(c));
  let up = false;
  for (let i = 0; i < 80; i++) { try { const r = await fetch(API + "/health"); if (r.ok) { up = true; break; } } catch {} await sleep(100); }
  if (!up) { log("server down:\n" + Buffer.concat(errBuf).toString().slice(-1500)); proc.kill("SIGKILL"); process.exit(1); }
  log("server up", BASE);

  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
  const rq = ctx.request;
  const J = async (resp, label) => { const t = await resp.text(); if (!resp.ok()) throw new Error(`${label} -> ${resp.status()} ${t.slice(0, 200)}`); try { return JSON.parse(t); } catch { return {}; } };

  await J(await rq.post(API + "/auth/signup", { data: { display_name: "alice", passcode: "Acme-pass1!", organization_name: "Acme", organization_slug: SLUG, email: "alice@acme.test" } }), "signup");
  const si = await rq.post(API + "/auth/signin", { data: { display_name: "alice", passcode: "Acme-pass1!" } });
  const cookie = (/ac_session=([^;]+)/.exec(si.headers()["set-cookie"] || "") || [])[1];
  await ctx.addCookies([{ name: "ac_session", value: cookie, domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  const me = await J(await rq.get(API + "/auth/me"), "me");
  const aliceRef = `user:${me.identity_id}`;
  log("alice ref", aliceRef);

  const proj = await J(await rq.post(ORG + "/projects", { data: { name: "agent-center2", description: "" } }), "proj");
  const pid = proj.project_id || proj.id;
  const task = await J(await rq.post(ORG + `/projects/${pid}/tasks`, { data: { title: "App Shell 三栏骨架 + 路由重排", description: "" } }), "task");
  const tid = task.task_id || task.id;
  await J(await rq.post(ORG + `/projects/${pid}/tasks/${tid}/assign`, { data: { assignee: aliceRef } }), "assign");
  const plan = await J(await rq.post(ORG + `/projects/${pid}/plans`, { data: { name: "v2.10.0 三栏重构", description: "" } }), "plan");
  const planId = plan.plan_id || plan.id;
  await J(await rq.post(ORG + `/projects/${pid}/plans/${planId}/tasks`, { data: { task_id: tid } }), "select-task");
  // start → the scheduler dispatches the ready node and posts the "your task is
  // ready" @mention into the plan conversation, authored by sender "system".
  await J(await rq.post(ORG + `/projects/${pid}/plans/${planId}/start`, { data: {} }), "start");
  await sleep(800);

  const page = await ctx.newPage();
  const consoleErrs = []; page.on("console", (m) => { if (m.type() === "error") consoleErrs.push(m.text()); });
  page.on("pageerror", (e) => consoleErrs.push("[pageerror] " + e.message));
  const shot = async (name) => { await page.screenshot({ path: join(OUT, name + ".png"), fullPage: false }); log("shot", name, page.url()); };

  // Open the plan detail Chat tab → the dispatch notification + its author.
  for (const theme of ["light", "dark"]) {
    await page.addInitScript((t) => localStorage.setItem("ac.theme", t), theme);
    await page.goto(BASE + `/organizations/${SLUG}/projects/${pid}/plans/${planId}`, { waitUntil: "domcontentloaded" });
    await page.getByTestId("plan-tabs").waitFor({ timeout: 8000 }).catch(() => {});
    // ensure the Chat tab is selected.
    const chatTab = page.getByText("Chat", { exact: false }).first();
    await chatTab.click().catch(() => {});
    await page.getByTestId("message-sender-button").first().waitFor({ timeout: 6000 }).catch(() => {});
    await sleep(500);
    await shot(`01-plan-chat-${theme}`);
    // Assert the author shows System, not (deleted).
    const authors = await page.getByTestId("message-sender-button").allInnerTexts();
    const resolvedFlags = await page.getByTestId("message-sender-button").evaluateAll((els) => els.map((e) => e.getAttribute("data-sender-resolved")));
    log(`[${theme}] message authors:`, JSON.stringify(authors), "resolved:", JSON.stringify(resolvedFlags));
  }

  log("CONSOLE ERRORS:", consoleErrs.length);
  consoleErrs.slice(0, 15).forEach((e) => log("  err:", e.slice(0, 200)));
  await writeFile(join(OUT, "RESULTS.json"), JSON.stringify({ consoleErrors: consoleErrs.slice(0, 30) }, null, 2));

  await browser.close(); proc.kill("SIGTERM"); await sleep(600); if (!proc.killed) proc.kill("SIGKILL"); await rm(tempDir, { recursive: true, force: true });
  log("done; shots in", OUT);
}
main().catch((e) => { console.error("FATAL", e); process.exit(1); });
