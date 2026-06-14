// v2.9.2 chat-attachment run-real BASELINE capture — boots a real agent-center
// instance (v292 bin), seeds a realistic multi-user scenario via the SAME /api
// the Web Console uses, then drives the SPA in a real browser to exercise
// send-image / send-file / receive / download / cross-user-authz across ALL
// conversation types (channel / DM / issue / task / plan) and captures key-step
// PNGs into docs/release/evidence/v2.9.2-attachments-screenshots/.
//
// One-shot reproducible: `node capture-v292-attachments.mjs` (deps: @playwright/test).
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, mkdir, rm, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { randomBytes } from "node:crypto";
import { deflateSync, crc32 } from "node:zlib";

const REPO = "/Users/oopslink/works/codes/oopslink/ac-wt-composer";
const BIN = resolve(REPO, "bin/agent-center");
const OUT = resolve(REPO, "docs/release/evidence/v2.9.2-attachments-screenshots");
const WEB = 7861;
const GRPC = 7860;
const BASE = `http://127.0.0.1:${WEB}`;
const API = `${BASE}/api`;
const SLUG = "acme";
const ORG = `${API}/orgs/${SLUG}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log("[cap]", ...a);

const results = [];
const findings = [];
function record(name, ok, detail) {
  results.push([name, ok ? "OK" : "FAIL", detail || ""]);
  log(ok ? "✅" : "❌", name, detail || "");
}
async function shot(page, name, locator) {
  try {
    const path = join(OUT, name + ".png");
    if (locator) await locator.screenshot({ path });
    else await page.screenshot({ path, fullPage: false });
    log("shot OK", name);
  } catch (e) {
    log("shot FAIL", name, String(e).slice(0, 140));
  }
}

// ---- build a real WxH solid-color PNG (valid, visible) ----
function png(w, h, [r, g, b]) {
  const sig = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
  const chunk = (type, data) => {
    const len = Buffer.alloc(4); len.writeUInt32BE(data.length, 0);
    const td = Buffer.concat([Buffer.from(type, "ascii"), data]);
    const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(td) >>> 0, 0);
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; ihdr[9] = 2; // 8-bit, truecolor RGB
  const row = Buffer.alloc(1 + w * 3);
  for (let x = 0; x < w; x++) { row[1 + x * 3] = r; row[2 + x * 3] = g; row[3 + x * 3] = b; }
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([sig, chunk("IHDR", ihdr), chunk("IDAT", deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]);
}
const IMG = png(96, 96, [37, 99, 235]); // blue square
const PDFISH = Buffer.from("%PDF-1.4\n% v2.9.2 attachment baseline fixture\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n", "utf8");

async function main() {
  await mkdir(OUT, { recursive: true });
  const tempDir = await mkdtemp(join(tmpdir(), "ac-v292-cap-"));
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

  let up = false;
  for (let i = 0; i < 80; i++) {
    try { const r = await fetch(API + "/health"); if (r.ok) { up = true; break; } } catch {}
    await sleep(100);
  }
  if (!up) { log("server did not come up:\n" + Buffer.concat(errBuf).toString().slice(-1500)); proc.kill("SIGKILL"); process.exit(1); }
  log("server up on", BASE);

  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
  const rq = ctx.request;
  const J = async (resp, label) => {
    const t = await resp.text();
    if (!resp.ok()) throw new Error(`${label} -> ${resp.status()} ${t.slice(0, 200)}`);
    try { return JSON.parse(t); } catch { return {}; }
  };

  // ---- signup alice (owner) ----
  const suj = await J(await rq.post(API + "/auth/signup", {
    data: { display_name: "alice", passcode: "Acme-pass1!", organization_name: "Acme", organization_slug: SLUG, email: "alice@acme.test" },
  }), "signup");
  const aliceId = suj.identity_id;
  log("alice", aliceId);
  const si = await rq.post(API + "/auth/signin", { data: { display_name: "alice", passcode: "Acme-pass1!" } });
  const aliceCookie = (/ac_session=([^;]+)/.exec(si.headers()["set-cookie"] || "") || [])[1];
  if (!aliceCookie) throw new Error("no alice ac_session");
  await ctx.addCookies([{ name: "ac_session", value: aliceCookie, domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);

  // ---- create bob (2nd org member) ----
  const bob = await J(await rq.post(ORG + "/members", { data: { display_name: "bob", role: "member" } }), "member-bob");
  const bobId = bob.identity_id;
  const bobPass = bob.temp_passcode;
  log("bob", bobId, "pass?", !!bobPass);
  // bob's own browser context + session (for cross-user authz)
  const bobCtx = await browser.newContext();
  const bsi = await bobCtx.request.post(API + "/auth/signin", { data: { display_name: "bob", passcode: bobPass } });
  const bobCookie = (/ac_session=([^;]+)/.exec(bsi.headers()["set-cookie"] || "") || [])[1];
  if (bobCookie) await bobCtx.addCookies([{ name: "ac_session", value: bobCookie, domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  record("seed_bob_signin", !!bobCookie, bobCookie ? "" : "bob could not sign in");

  // ---- project ----
  const proj = await J(await rq.post(ORG + "/projects", { data: { name: "Phoenix", description: "v2.9.2 attachment dogfood" } }), "project");
  const pid = proj.id || proj.project_id;

  // ---- channel ----
  const chan = await J(await rq.post(ORG + "/conversations", { data: { kind: "channel", name: "general", description: "team channel" } }), "channel");
  const cid = chan.conversation_id || chan.id;

  // ---- DM (alice <-> bob) ----
  let dmId = "";
  try {
    // DM member ref is an ADR-0033 IdentityRef: "user:<identity_id>" (FE: DMStartModal).
    const dm = await J(await rq.post(ORG + "/conversations", { data: { kind: "dm", members: ["user:" + bobId] } }), "dm");
    dmId = dm.conversation_id || dm.id;
  } catch (e) { findings.push("DM create failed: " + String(e).slice(0, 160)); }
  record("seed_dm", !!dmId, dmId || "no dm id");

  // ---- issue + task (conversations pinned by owner_ref #137) ----
  let issueId = "", taskId = "";
  try { const iss = await J(await rq.post(ORG + `/projects/${pid}/issues`, { data: { title: "Investigate flaky upload", description: "attach evidence here" } }), "issue"); issueId = iss.id || iss.issue_id; } catch (e) { findings.push("issue create failed: " + String(e).slice(0, 140)); }
  try { const tk = await J(await rq.post(ORG + `/projects/${pid}/tasks`, { data: { title: "Ship attachment UX", description: "discussion + files" } }), "task"); taskId = tk.id || tk.task_id; } catch (e) { findings.push("task create failed: " + String(e).slice(0, 140)); }

  // verify issue/task conversations resolve by owner_ref
  for (const [kind, id] of [["issues", issueId], ["tasks", taskId]]) {
    if (!id) continue;
    try {
      const list = await J(await rq.get(ORG + `/conversations?owner_ref=` + encodeURIComponent(`pm://${kind}/${id}`)), "owner-ref");
      record(`seed_${kind}_conversation`, Array.isArray(list) && list.length > 0, Array.isArray(list) ? `${list.length} conv` : "no list");
    } catch (e) { record(`seed_${kind}_conversation`, false, String(e).slice(0, 120)); }
  }

  // ---- plan (+start so it's a live conversation surface) ----
  let planId = "";
  try {
    const plan = await J(await rq.post(ORG + `/projects/${pid}/plans`, { data: { name: "Attachment rollout", description: "ship plan" } }), "plan");
    planId = plan.id || plan.plan_id;
    try { await rq.post(ORG + `/projects/${pid}/plans/${planId}/start`); } catch {}
  } catch (e) { findings.push("plan create failed: " + String(e).slice(0, 140)); }
  record("seed_plan", !!planId, planId || "no plan id");

  // ============ BROWSER WALK (as alice) ============
  const page = await ctx.newPage();
  const consoleErrs = [];
  page.on("console", (m) => { if (m.type() === "error") consoleErrs.push(m.text()); });
  await page.addInitScript(() => localStorage.setItem("ac.theme", "light"));
  const go = async (path) => { await page.goto(BASE + path, { waitUntil: "domcontentloaded" }); await sleep(1200); };

  // attach a file via the hidden composer input, then send; assert it renders.
  async function attachSend(prefix, kind, fileSpec, caption, bobParticipant = false) {
    const isImg = fileSpec.mimeType.startsWith("image/");
    try {
      await page.getByTestId("message-composer").first().waitFor({ timeout: 8000 });
    } catch (e) { record(`${prefix}_composer`, false, "no composer on " + kind); await shot(page, `${prefix}_NO_COMPOSER`); return; }
    record(`${prefix}_composer`, true, "composer present");

    // type a caption (optional) + set the file
    try { await page.getByTestId("composer-textarea").first().fill(caption); } catch {}
    await page.getByTestId("composer-file").first().setInputFiles({ name: fileSpec.name, mimeType: fileSpec.mimeType, buffer: fileSpec.buffer });
    // in-composer staged preview/chip
    try {
      await page.getByTestId("composer-attachments").first().waitFor({ timeout: 4000 });
      const hasPreview = isImg ? (await page.getByTestId("composer-attachment-preview").count()) > 0 : true;
      record(`${prefix}_staged`, true, isImg ? `img-preview=${hasPreview}` : "file-chip");
    } catch (e) { record(`${prefix}_staged`, false, String(e).slice(0, 100)); }
    await shot(page, `${prefix}_1_staged`);

    // send
    const beforeCount = await page.getByTestId("message-attachment").count();
    await page.getByTestId("composer-send").first().click();
    let rendered = false;
    for (let i = 0; i < 30; i++) {
      const n = await page.getByTestId("message-attachment").count();
      if (n > beforeCount) { rendered = true; break; }
      await sleep(400);
    }
    if (!rendered) { // SSE may lag — reload and recount
      await page.reload({ waitUntil: "domcontentloaded" }); await sleep(1500);
      rendered = (await page.getByTestId("message-attachment").count()) > 0;
    }
    record(`${prefix}_received`, rendered, rendered ? `${kind} attachment rendered` : "attachment never rendered");
    if (isImg && rendered) record(`${prefix}_img_preview`, (await page.getByTestId("attachment-preview").count()) > 0, "inline image preview");
    await shot(page, `${prefix}_2_received`);

    // download reachability: as alice (participant) 200, as bob (non-participant) expect 403
    try {
      const href = await page.getByTestId("attachment-link").last().getAttribute("href");
      if (href) {
        const aliceDl = await rq.get(BASE + href);
        record(`${prefix}_download_alice`, aliceDl.ok(), `alice GET ${aliceDl.status()}`);
        const bobDl = await bobCtx.request.get(BASE + href);
        if (bobParticipant) {
          record(`${prefix}_authz_bob_participant_200`, bobDl.ok(), `bob(participant) GET ${bobDl.status()} (expect 200)`);
        } else {
          record(`${prefix}_authz_bob_403`, bobDl.status() === 403, `bob(non-participant) GET ${bobDl.status()} (expect 403)`);
        }
      } else record(`${prefix}_download_alice`, false, "no attachment-link href");
    } catch (e) { record(`${prefix}_download_alice`, false, String(e).slice(0, 120)); }
  }

  // --- CHANNEL ---
  await go(`/organizations/${SLUG}/channels/${cid}`);
  await attachSend("A_channel", "channel", { name: "evidence.png", mimeType: "image/png", buffer: IMG }, "Here is the screenshot (channel)");
  await attachSend("A_channel_file", "channel", { name: "report.pdf", mimeType: "application/pdf", buffer: PDFISH }, "And the report file (channel)");

  // --- DM ---
  if (dmId) {
    await go(`/organizations/${SLUG}/dms/${dmId}`);
    await attachSend("B_dm", "dm", { name: "dm-pic.png", mimeType: "image/png", buffer: IMG }, "DM image", true);
    await attachSend("B_dm_file", "dm", { name: "dm-doc.pdf", mimeType: "application/pdf", buffer: PDFISH }, "DM file", true);
  } else record("B_dm_skip", false, "no DM seeded");

  // --- ISSUE ---
  if (issueId) { await go(`/organizations/${SLUG}/projects/${pid}/issues/${issueId}`); await attachSend("C_issue", "issue", { name: "issue.png", mimeType: "image/png", buffer: IMG }, "Issue image"); }
  else record("C_issue_skip", false, "no issue seeded");

  // --- TASK ---
  if (taskId) { await go(`/organizations/${SLUG}/projects/${pid}/tasks/${taskId}`); await attachSend("D_task", "task", { name: "task.png", mimeType: "image/png", buffer: IMG }, "Task image"); }
  else record("D_task_skip", false, "no task seeded");

  // --- PLAN ---
  if (planId) { await go(`/organizations/${SLUG}/projects/${pid}/plans/${planId}`); await attachSend("E_plan", "plan", { name: "plan.png", mimeType: "image/png", buffer: IMG }, "Plan image"); }
  else record("E_plan_skip", false, "no plan seeded");

  // --- GAP PROBES (structural, on channel composer) ---
  await go(`/organizations/${SLUG}/channels/${cid}`);
  try {
    const probe = await page.evaluate(() => {
      const form = document.querySelector('[data-testid="message-composer"]');
      const fileInput = document.querySelector('[data-testid="composer-file"]');
      return {
        hasAcceptAttr: !!(fileInput && fileInput.getAttribute("accept")),
        acceptVal: fileInput ? fileInput.getAttribute("accept") : null,
        multiple: !!(fileInput && fileInput.hasAttribute("multiple")),
        // drop-zone / progress testids that a polished composer would expose
        hasDropZone: !!document.querySelector('[data-testid*="drop"]'),
        hasProgress: !!document.querySelector('[data-testid*="progress"],[role="progressbar"]'),
        formHTMLHasDnDHandlers: form ? /ondrop|ondragover/i.test(form.outerHTML) : false,
        textareaHasPasteHandler: (() => {
          const ta = document.querySelector('[data-testid="composer-textarea"]');
          return ta ? typeof ta.onpaste === "function" || /onpaste/i.test(ta.outerHTML) : false;
        })(),
      };
    });
    findings.push("GAP-probe(channel composer): " + JSON.stringify(probe));
    record("gap_type_accept_filter", probe.hasAcceptAttr, probe.hasAcceptAttr ? `accept=${probe.acceptVal}` : "NO accept= → all types allowed (no client type filter)");
    record("gap_upload_progress_ui", probe.hasProgress, probe.hasProgress ? "progress UI present" : "NO progress UI element");
    record("gap_dropzone", probe.hasDropZone || probe.formHTMLHasDnDHandlers, "drag&drop drop-zone");
    record("gap_paste_screenshot", probe.textareaHasPasteHandler, probe.textareaHasPasteHandler ? "onPaste handler present" : "NO onPaste handler → paste-screenshot won't attach");
  } catch (e) { findings.push("gap probe failed: " + String(e).slice(0, 120)); }

  // dark-mode receive (both-mode AA spot)
  await page.addInitScript(() => localStorage.setItem("ac.theme", "dark"));
  await go(`/organizations/${SLUG}/channels/${cid}`);
  await shot(page, "F_dark_channel_attachments");

  log("CONSOLE ERRORS:", consoleErrs.length);
  consoleErrs.slice(0, 12).forEach((e) => log("  err:", e.slice(0, 160)));

  log("\n==== RESULTS ====");
  results.forEach(([n, s, d]) => log(`  ${s === "OK" ? "✅" : "❌"} ${n}: ${s} ${d}`));
  log("\n==== FINDINGS / NOTES ====");
  findings.forEach((f) => log("  • " + f));

  // write a machine-readable summary next to the screenshots
  await writeFile(join(OUT, "RESULTS.json"), JSON.stringify({ results, findings, consoleErrors: consoleErrs.slice(0, 20), bin: BIN }, null, 2));

  await browser.close();
  await bobCtx.close();
  proc.kill("SIGTERM");
  await sleep(800);
  if (!proc.killed) proc.kill("SIGKILL");
  await rm(tempDir, { recursive: true, force: true });
}

main().catch((e) => { console.error("FATAL", e); process.exit(1); });
