// v2.10.0 [T4] Projects + project detail three-column run-real capture. Boots a
// fresh instance, seeds a project + tasks/issues, screenshots the Projects list,
// then the project detail (col② project sub-nav + tab content), light + dark.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, mkdir, rm, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { randomBytes } from "node:crypto";

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const BIN = resolve(REPO, "bin/agent-center");
const OUT = "/tmp/v210-t4-shots";
const WEB = 7898, GRPC = 7897;
const BASE = `http://127.0.0.1:${WEB}`;
const API = `${BASE}/api`;
const SLUG = "acme";
const ORG = `${API}/orgs/${SLUG}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log("[t4]", ...a);

async function main() {
  await mkdir(OUT, { recursive: true });
  const tempDir = await mkdtemp(join(tmpdir(), "ac-v210t4-"));
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

  const proj = await J(await rq.post(ORG + "/projects", { data: { name: "agent-center2", description: "三栏式重构" } }), "proj");
  const pid = proj.project_id || proj.id;
  await rq.post(ORG + "/projects", { data: { name: "sandbox", description: "scratch" } });
  for (const t of ["Thread 验收 run-real", "ADR-0046 状态机精简", "data/API class-guard"]) {
    await rq.post(ORG + `/projects/${pid}/tasks`, { data: { title: t, description: "" } });
  }
  await rq.post(ORG + `/projects/${pid}/issues`, { data: { title: "导航重排", description: "" } });

  const page = await ctx.newPage();
  const consoleErrs = []; page.on("console", (m) => { if (m.type() === "error") consoleErrs.push(m.text()); });
  page.on("pageerror", (e) => consoleErrs.push("[pageerror] " + e.message));
  await page.addInitScript(() => localStorage.setItem("ac.theme", "light"));
  const shot = async (name) => { await page.screenshot({ path: join(OUT, name + ".png"), fullPage: false }); log("shot", name, page.url()); };

  // ① Projects list — top-level Workspace col② (Projects/Issues/Tasks/Plan).
  await page.goto(BASE + `/organizations/${SLUG}/projects`, { waitUntil: "domcontentloaded" });
  await page.getByTestId("page-Projects").waitFor({ timeout: 8000 });
  await sleep(600);
  await shot("01-projects-list");

  // ② Project detail — col② project sub-nav + Tasks tab.
  await page.goto(BASE + `/organizations/${SLUG}/projects/${pid}`, { waitUntil: "domcontentloaded" });
  await page.getByTestId("workspace-nav-project").waitFor({ timeout: 6000 });
  await sleep(500);
  await shot("02-project-detail-issues");
  await page.getByTestId("project-subnav-tasks").click();
  await page.getByTestId("project-tasks-panel").waitFor({ timeout: 4000 });
  await sleep(400);
  await shot("03-project-detail-tasks");

  // dark mode spot.
  await page.addInitScript(() => localStorage.setItem("ac.theme", "dark"));
  await page.goto(BASE + `/organizations/${SLUG}/projects/${pid}?tab=tasks`, { waitUntil: "domcontentloaded" });
  await page.getByTestId("workspace-nav-project").waitFor({ timeout: 6000 });
  await sleep(400);
  await shot("04-project-detail-dark");

  log("CONSOLE ERRORS:", consoleErrs.length);
  consoleErrs.slice(0, 15).forEach((e) => log("  err:", e.slice(0, 200)));
  await writeFile(join(OUT, "RESULTS.json"), JSON.stringify({ consoleErrors: consoleErrs.slice(0, 30) }, null, 2));

  await browser.close(); proc.kill("SIGTERM"); await sleep(600); if (!proc.killed) proc.kill("SIGKILL"); await rm(tempDir, { recursive: true, force: true });
  log("done; shots in", OUT);
}
main().catch((e) => { console.error("FATAL", e); process.exit(1); });
