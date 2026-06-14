// v2.10.0 [T6] global Plan list run-real capture. Boots a fresh instance, seeds
// a project + several plans, navigates to the global Workspace > Plan list,
// selects a plan for the col④ summary, and verifies the new org plans endpoint.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, mkdir, rm, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { randomBytes } from "node:crypto";

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const BIN = resolve(REPO, "bin/agent-center");
const OUT = "/tmp/v210-t6-shots";
const WEB = 7896, GRPC = 7895;
const BASE = `http://127.0.0.1:${WEB}`;
const API = `${BASE}/api`;
const SLUG = "acme";
const ORG = `${API}/orgs/${SLUG}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log("[t6]", ...a);

async function main() {
  await mkdir(OUT, { recursive: true });
  const tempDir = await mkdtemp(join(tmpdir(), "ac-v210t6-"));
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
  for (const name of ["v2.9.2 收尾", "聊天框附件增强", "管理升级 ADR-0046/47", "看板可观测性"]) {
    await rq.post(ORG + `/projects/${pid}/plans`, { data: { name, description: "" } });
  }

  // Verify the new org plans endpoint directly.
  const orgPlans = await J(await rq.get(ORG + "/plans"), "GET /plans");
  log("GET /api/orgs/acme/plans →", orgPlans.total, "plans:", (orgPlans.items || []).map((p) => p.name).join(" | "));

  const page = await ctx.newPage();
  const consoleErrs = []; page.on("console", (m) => { if (m.type() === "error") consoleErrs.push(m.text()); });
  page.on("pageerror", (e) => consoleErrs.push("[pageerror] " + e.message));
  await page.addInitScript(() => localStorage.setItem("ac.theme", "light"));
  const shot = async (name) => { await page.screenshot({ path: join(OUT, name + ".png"), fullPage: false }); log("shot", name, page.url()); };

  await page.goto(BASE + `/organizations/${SLUG}/plans`, { waitUntil: "domcontentloaded" });
  await page.getByTestId("org-plan-row").first().waitFor({ timeout: 8000 });
  await sleep(500);
  await shot("01-plan-list-3col");           // Workspace > Plan, global list

  // click the Updated cell (no link) so the row-select fires (name/project cells
  // are links that stopPropagation + navigate).
  await page.getByTestId("org-plan-updated").first().click();
  await page.getByTestId("org-plan-meta-panel").waitFor({ timeout: 4000 });
  await sleep(400);
  await shot("02-plan-selected-4col");        // col④ summary

  // Open the plan detail (Chat/DAG/Task tabs — existing PlanDetail).
  await page.getByTestId("org-plan-meta-open").click();
  await page.getByTestId("plan-tabs").waitFor({ timeout: 6000 }).catch(() => {});
  await sleep(700);
  await shot("03-plan-detail-tabs");

  await page.addInitScript(() => localStorage.setItem("ac.theme", "dark"));
  await page.goto(BASE + `/organizations/${SLUG}/plans`, { waitUntil: "domcontentloaded" });
  await page.getByTestId("org-plan-row").first().waitFor({ timeout: 6000 });
  await sleep(400);
  await shot("04-plan-list-dark");

  log("CONSOLE ERRORS:", consoleErrs.length);
  consoleErrs.slice(0, 15).forEach((e) => log("  err:", e.slice(0, 200)));
  await writeFile(join(OUT, "RESULTS.json"), JSON.stringify({ orgPlansTotal: orgPlans.total, consoleErrors: consoleErrs.slice(0, 30) }, null, 2));

  await browser.close(); proc.kill("SIGTERM"); await sleep(600); if (!proc.killed) proc.kill("SIGKILL"); await rm(tempDir, { recursive: true, force: true });
  log("done; shots in", OUT);
}
main().catch((e) => { console.error("FATAL", e); process.exit(1); });
