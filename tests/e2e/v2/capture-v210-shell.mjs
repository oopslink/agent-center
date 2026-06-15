// v2.10.0 [T1] three-column shell run-real capture. Boots a fresh real
// instance, seeds an org + channels + a project + tasks, and screenshots the
// new module rail + secondary nav + content across modules in light & dark.
// One-shot: `node tests/e2e/v2/capture-v210-shell.mjs`.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, mkdir, rm, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { randomBytes } from "node:crypto";

// Repo root derived from this script's location (tests/e2e/v2/ → ../../..).
const REPO = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const BIN = resolve(REPO, "bin/agent-center");
const OUT = "/tmp/v210-shell-shots";
const WEB = 7882, GRPC = 7881;
const BASE = `http://127.0.0.1:${WEB}`;
const API = `${BASE}/api`;
const SLUG = "acme";
const ORG = `${API}/orgs/${SLUG}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log("[shell]", ...a);

async function main() {
  await mkdir(OUT, { recursive: true });
  const tempDir = await mkdtemp(join(tmpdir(), "ac-v210-"));
  const masterKeyPath = join(tempDir, "master.key");
  await writeFile(masterKeyPath, randomBytes(32).toString("base64") + "\n", "utf8");
  await chmod(masterKeyPath, 0o600);
  const configPath = join(tempDir, "config.yaml");
  await writeFile(
    configPath,
    `server:\n  listen_addr: ":${GRPC}"\n  sqlite_path: "${join(tempDir, "ac.db")}"\n  admin_socket_path: "${join(tempDir, "admin.sock")}"\nweb_console:\n  enabled: true\n  listen_addr: "127.0.0.1:${WEB}"\nsecret_management:\n  master_key_file: "${masterKeyPath}"\nblob_store:\n  root: "${join(tempDir, "blobs")}"\n`,
    "utf8",
  );

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

  // seed content so each module column has something to render.
  await J(await rq.post(ORG + "/conversations", { data: { kind: "channel", name: "agent-center-dev", description: "v2.10 集成" } }), "ch1");
  await J(await rq.post(ORG + "/conversations", { data: { kind: "channel", name: "general", description: "" } }), "ch2");
  const proj = await J(await rq.post(ORG + "/projects", { data: { name: "agent-center2", description: "三栏式重构" } }), "proj");
  const pid = proj.project_id || proj.id;
  try {
    await rq.post(ORG + `/projects/${pid}/tasks`, { data: { title: "App Shell 三栏骨架", description: "T1" } });
    await rq.post(ORG + `/projects/${pid}/issues`, { data: { title: "导航重排", description: "" } });
  } catch (e) { log("seed task/issue skipped:", String(e).slice(0, 120)); }

  const page = await ctx.newPage();
  const consoleErrs = []; page.on("console", (m) => { if (m.type() === "error") consoleErrs.push(m.text()); });
  page.on("pageerror", (e) => consoleErrs.push("[pageerror] " + e.message));
  const go = async (p) => { await page.goto(BASE + p, { waitUntil: "domcontentloaded" }); await sleep(1100); };
  const shot = async (name) => { await page.screenshot({ path: join(OUT, name + ".png"), fullPage: false }); log("shot", name, page.url()); };

  // Light mode across modules.
  await page.addInitScript(() => localStorage.setItem("ac.theme", "light"));
  await go(`/organizations/${SLUG}`); await shot("01-index-redirect");          // index → Workspace/Projects
  await go(`/organizations/${SLUG}/channels`); await shot("02-conversations");   // col②=Channels/DMs
  await go(`/organizations/${SLUG}/tasks`); await shot("03-workspace-tasks");    // col②=Projects/Issues/Tasks
  await go(`/organizations/${SLUG}/members/humans`); await shot("04-members");
  await go(`/organizations/${SLUG}/settings`); await shot("05-system-settings");

  // Dark mode spot.
  await page.addInitScript(() => localStorage.setItem("ac.theme", "dark"));
  await go(`/organizations/${SLUG}/channels`); await shot("06-dark-conversations");

  log("CONSOLE ERRORS:", consoleErrs.length);
  consoleErrs.slice(0, 15).forEach((e) => log("  err:", e.slice(0, 200)));
  await writeFile(join(OUT, "RESULTS.json"), JSON.stringify({ consoleErrors: consoleErrs.slice(0, 30), bin: BIN }, null, 2));

  await browser.close(); proc.kill("SIGTERM"); await sleep(600); if (!proc.killed) proc.kill("SIGKILL"); await rm(tempDir, { recursive: true, force: true });
  log("done; shots in", OUT);
}
main().catch((e) => { console.error("FATAL", e); process.exit(1); });
