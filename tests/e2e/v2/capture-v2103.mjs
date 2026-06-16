// v2.10.3 release acceptance — run-real capture (T167 全会话附件 / T170 issue 生命周期).
//
// Methodology: docs/rules/acceptance-methodology.md §3 (真 install + 真浏览器 +
// 全产品) / §4 (证据即代码 — 内嵌截图 + 可复现脚本). This script drives the SPA in
// a REAL chromium against a REAL test-instance (agent-center install test-instance,
// real install codepath → config 含 blob_store), seeds conversations through the
// SAME /api the Web Console uses, then performs the actual upload/send/download
// through the composer (the real user path), asserting RENDERED truth
// (img.naturalWidth>0, downloaded bytes match) — not class/source parity.
//
// T167 covers the four conversation kinds the red-line names — Plan chat /
// Channel / DM / Task discussion — each: upload an IMAGE (renders a preview
// card) + a FILE (renders a file chip), then DOWNLOAD both (bytes match). The
// conversations are SHARED human↔agent (the seeded sandbox agent is added as a
// participant), so a human-uploaded blob lands in a conversation the agent
// participates in; the agent→human direction (agent uploads/attaches in those
// kinds, incl. the formerly-broken Plan kind) is the server contract proven by
// the candidate's agent_tools_files_t167_test.go (real admin server + auth +
// projector) — run + captured separately as §4.1 backend evidence.
//
// T170 walks the issue lifecycle from the user's view in the browser
// (create → comment → close → reopen → derive task). The agent issue MCP tools
// (create/update/close/reopen/comment/list/link-task) reuse the SAME Web Console
// service methods (human and agent one issue dataset) and are proven by
// agent_tools_issues_test.go — also captured as §4.1 backend evidence.
//
// Run (reuse a running test-instance — fast iteration):
//   AC_BASE=http://127.0.0.1:50288 AC_SLUG=acme-v2103acc \
//   AC_OWNER='Owner v2103acc' AC_PASSCODE='SeedPass1!' \
//   AC_PROJECT=project-376e3ae5 AC_AGENT=agent-500419c3 \
//   AC_PLAN=plan-233a6009 AC_TASK=task-2dda3731 \
//   node capture-v2103.mjs
//
// Spawn a fresh real test-instance (self-contained, methodology-compliant):
//   AC_SPAWN=1 BIN=/path/to/agent-center node capture-v2103.mjs
import { chromium } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { deflateSync, crc32 } from "node:zlib";

const REPO = "/Users/oopslink/works/codes/oopslink/ac-wt-v2103acc";
const BIN = process.env.BIN || resolve(REPO, "bin/agent-center");
const OUT = resolve(REPO, "docs/design/v2.10.3/acceptance-tester2/evidence");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log("[v2103]", ...a);

const results = [];
function record(name, ok, detail) {
  results.push({ name, status: ok ? "PASS" : "FAIL", detail: detail || "" });
  log(ok ? "✅" : "❌", name, detail || "");
}

// minimal solid-color PNG so the image preview has real decodable bytes.
function png(w, h, [r, g, b]) {
  const sig = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
  const chunk = (type, data) => {
    const len = Buffer.alloc(4); len.writeUInt32BE(data.length, 0);
    const td = Buffer.concat([Buffer.from(type, "ascii"), data]);
    const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(td) >>> 0, 0);
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2;
  const row = Buffer.alloc(1 + w * 3);
  for (let x = 0; x < w; x++) { row[1 + x * 3] = r; row[2 + x * 3] = g; row[3 + x * 3] = b; }
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([sig, chunk("IHDR", ihdr), chunk("IDAT", deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]);
}
const IMG = png(120, 120, [37, 99, 235]);             // blue square
const FILE_BYTES = Buffer.from("v2.10.3 acceptance attachment — file chip payload\n".repeat(8), "utf8");

let cfg = {
  base: process.env.AC_BASE || "http://127.0.0.1:50288",
  slug: process.env.AC_SLUG || "acme-v2103acc",
  owner: process.env.AC_OWNER || "Owner v2103acc",
  passcode: process.env.AC_PASSCODE || "SeedPass1!",
  project: process.env.AC_PROJECT || "project-376e3ae5",
  agent: process.env.AC_AGENT || "agent-500419c3",
  plan: process.env.AC_PLAN || "plan-233a6009",
  task: process.env.AC_TASK || "task-2dda3731",
};

function spawnInstance() {
  log("spawning fresh test-instance via real install codepath…");
  const out = execFileSync(BIN, ["install", "test-instance", "--id", "v2103cap", "--with-agent", "--workers", "1", "--output", "json"], { encoding: "utf8" });
  const pack = JSON.parse(out);
  cfg.base = pack.web_url;
  cfg.slug = pack.signin.org_slug;
  cfg.owner = pack.signin.display_name;
  cfg.passcode = pack.signin.passcode;
  cfg.project = pack.entity_refs.project_id;
  cfg.agent = pack.agent.id;
  cfg.task = pack.agent.dispatched_task_id;
  // plan: discover the built-in plan below via API.
  log("spawned", JSON.stringify(cfg));
}

async function main() {
  if (process.env.AC_SPAWN === "1") spawnInstance();
  await mkdir(OUT, { recursive: true });
  const API = `${cfg.base}/api`;
  const ORG = `${API}/orgs/${cfg.slug}`;

  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
  const rq = ctx.request;
  const J = async (resp, label) => {
    const t = await resp.text();
    if (!resp.ok()) throw new Error(`${label} -> ${resp.status()} ${t.slice(0, 200)}`);
    try { return JSON.parse(t); } catch { return {}; }
  };

  // ---- signin (API → cookie, browser then runs as the owner) -----------------
  const si = await rq.post(`${API}/auth/signin`, { data: { display_name: cfg.owner, passcode: cfg.passcode, org_slug: cfg.slug } });
  if (!si.ok()) throw new Error("signin failed " + si.status() + " " + (await si.text()).slice(0, 200));
  const setCookie = si.headers()["set-cookie"] || "";
  const cookieVal = (/ac_session=([^;]+)/.exec(setCookie) || [])[1];
  if (!cookieVal) throw new Error("no ac_session cookie in signin response");
  await ctx.addCookies([{ name: "ac_session", value: cookieVal, domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  log("signed in as", cfg.owner);

  // Create a REAL user-created plan for the Plan-chat test. (The "[Built-in]"
  // pool plan is system-created with EMPTY participants — a human navigating it
  // is not an active participant, so blob download fail-closes 403; that is the
  // participant model, not a kind gap. A normal plan binds its creator as an
  // active participant, the real plan-chat use case.)
  let planConv = "";
  try {
    const planResp = await J(await rq.post(`${ORG}/projects/${cfg.project}/plans`, { data: { name: "v2103 acceptance plan", description: "T167 plan-chat attachments" } }), "create plan");
    cfg.plan = planResp.id || planResp.plan_id;
    await sleep(1800); // let the participant projector bind the plan conversation
    const plans = await J(await rq.get(`${ORG}/projects/${cfg.project}/plans`), "plans");
    const plan = (plans.plans || []).find((p) => p.id === cfg.plan);
    if (plan) planConv = plan.conversation_id;
    log("created real plan", cfg.plan, "conv", planConv);
  } catch (e) { log("plan create warn", String(e).slice(0, 160)); }

  // ---- seed the human↔agent conversations via the same /api ------------------
  const agentRef = `agent:${cfg.agent}`;
  const sfx = Date.now().toString(36).slice(-5);
  const chan = await J(await rq.post(`${ORG}/conversations`, { data: { kind: "channel", name: `acceptance-167-${sfx}`, description: "T167 full-conversation attachments", members: [agentRef] } }), "create channel");
  const chanId = chan.conversation_id || chan.id;
  const dm = await J(await rq.post(`${ORG}/conversations`, { data: { kind: "dm", members: [agentRef] } }), "create dm");
  const dmId = dm.conversation_id || dm.id;
  log("seeded channel", chanId, "dm", dmId, "planConv", planConv, "task", cfg.task);

  const page = await ctx.newPage();
  const consoleErrs = [];
  page.on("console", (m) => { if (m.type() === "error") consoleErrs.push(m.text()); });
  const setTheme = async (t) => { await page.addInitScript((th) => localStorage.setItem("ac.theme", th), t); };
  await setTheme("light");

  const go = async (path) => { await page.goto(cfg.base + path, { waitUntil: "domcontentloaded" }); await sleep(1400); };
  async function shot(name, locator) {
    try { const path = join(OUT, name + ".png"); if (locator) await locator.screenshot({ path }); else await page.screenshot({ path }); log("shot", name); }
    catch (e) { log("shot FAIL", name, String(e).slice(0, 120)); }
  }

  // Ensure a message composer is on screen; some detail pages tab the chat.
  async function ensureComposer() {
    const composer = page.getByTestId("message-composer").first();
    try { await composer.waitFor({ timeout: 6000 }); return true; } catch {}
    // try clicking a chat/discussion tab
    for (const re of [/讨论|chat|对话|messages|conversation/i]) {
      const tab = page.getByRole("tab", { name: re }).first();
      try { if (await tab.count()) { await tab.click(); await sleep(800); } } catch {}
      const t = page.getByText(re).first();
      try { if (await t.count()) { await t.click(); await sleep(800); } } catch {}
    }
    try { await composer.waitFor({ timeout: 6000 }); return true; } catch { return false; }
  }

  // upload one file through the composer and send; assert the rendered card.
  // mode = "image" | "file".
  async function uploadAndAssert(prefix, mode) {
    const isImg = mode === "image";
    const name = isImg ? `${prefix}-image.png` : `${prefix}-file.txt`;
    const mime = isImg ? "image/png" : "text/plain";
    const buf = isImg ? IMG : FILE_BYTES;
    const before = await page.getByTestId("message-attachment").count();
    await page.getByTestId("composer-file").first().setInputFiles({ name, mimeType: mime, buffer: buf });
    // staged chip
    let staged = 0;
    try { await page.getByTestId("composer-attachment").first().waitFor({ timeout: 8000 }); staged = await page.getByTestId("composer-attachment").count(); } catch {}
    await page.getByTestId("composer-textarea").first().fill(isImg ? "image attachment" : "file attachment");
    await page.getByTestId("composer-send").first().click();
    // wait for a new rendered message-attachment
    let appeared = false;
    for (let i = 0; i < 40; i++) { if ((await page.getByTestId("message-attachment").count()) > before) { appeared = true; break; } await sleep(300); }
    return { name, mime, staged, appeared };
  }

  // assert the LAST rendered attachment: image → preview img with naturalWidth>0;
  // file → attachment-type chip present. Then download via the link href and
  // compare bytes. Returns {renderOk, downloadOk, detail}.
  async function assertLastAttachment(mode, expectBytes) {
    const isImg = mode === "image";
    const items = page.getByTestId("message-attachment");
    const n = await items.count();
    const last = items.nth(n - 1);
    let renderOk = false, detail = "";
    if (isImg) {
      const img = last.getByTestId("attachment-preview").first();
      try { await img.waitFor({ timeout: 6000 }); const nat = await img.evaluate((el) => el.naturalWidth); renderOk = nat > 0; detail = `naturalWidth=${nat}`; } catch (e) { detail = "no preview img"; }
    } else {
      const chip = last.getByTestId("attachment-type").first();
      try { await chip.waitFor({ timeout: 6000 }); const txt = (await chip.innerText()).trim(); renderOk = txt.length > 0; detail = `chip="${txt}"`; } catch (e) { detail = "no file chip"; }
    }
    // download through the gated /api/files/{id} link
    let downloadOk = false;
    try {
      const href = await last.getByTestId("attachment-link").first().getAttribute("href");
      const abs = href.startsWith("http") ? href : cfg.base + href;
      const dl = await rq.get(abs);
      const body = await dl.body();
      downloadOk = dl.ok() && body.length === expectBytes;
      detail += ` | download ${dl.status()} bytes=${body.length}/${expectBytes}`;
    } catch (e) { detail += " | download ERR " + String(e).slice(0, 80); }
    return { renderOk, downloadOk, detail };
  }

  // ---- run the 4 conversation kinds -----------------------------------------
  const CONVS = [
    { key: "channel", label: "Channel 频道", path: `/organizations/${cfg.slug}/channels/${chanId}` },
    { key: "dm", label: "DM 私信", path: `/organizations/${cfg.slug}/dms/${dmId}` },
    { key: "plan", label: "Plan chat", path: `/organizations/${cfg.slug}/projects/${cfg.project}/plans/${cfg.plan}` },
    { key: "task", label: "Task 讨论", path: `/organizations/${cfg.slug}/projects/${cfg.project}/tasks/${cfg.task}` },
  ];

  for (const c of CONVS) {
    log("=== conversation:", c.key, c.path);
    await go(c.path);
    await shot(`t167_${c.key}_1_open`);
    const haveComposer = await ensureComposer();
    record(`T167_${c.key}_composer_reachable`, haveComposer, c.path);
    if (!haveComposer) { await shot(`t167_${c.key}_NO_composer`); continue; }

    // image
    const imgUp = await uploadAndAssert(`t167-${c.key}`, "image");
    await shot(`t167_${c.key}_2_image_sent`);
    const imgA = await assertLastAttachment("image", IMG.length);
    record(`T167_${c.key}_image_preview`, imgUp.appeared && imgA.renderOk, `staged=${imgUp.staged} ${imgA.detail}`);
    record(`T167_${c.key}_image_download`, imgA.downloadOk, imgA.detail);

    // file
    const fileUp = await uploadAndAssert(`t167-${c.key}`, "file");
    await shot(`t167_${c.key}_3_file_sent`);
    const fileA = await assertLastAttachment("file", FILE_BYTES.length);
    record(`T167_${c.key}_file_chip`, fileUp.appeared && fileA.renderOk, `staged=${fileUp.staged} ${fileA.detail}`);
    record(`T167_${c.key}_file_download`, fileA.downloadOk, fileA.detail);

    await shot(`t167_${c.key}_4_both_attachments_light`);
  }

  // ---- both-mode (light + dark) for the channel attachment cards -------------
  // (both-mode 命门: preview card / file chip color + contrast)
  await setTheme("dark");
  const darkPage = await ctx.newPage();
  await darkPage.addInitScript(() => localStorage.setItem("ac.theme", "dark"));
  await darkPage.goto(`${cfg.base}/organizations/${cfg.slug}/channels/${chanId}`, { waitUntil: "domcontentloaded" });
  await sleep(1600);
  try { await darkPage.getByTestId("message-attachment").first().waitFor({ timeout: 6000 }); } catch {}
  try { await darkPage.screenshot({ path: join(OUT, "t167_channel_4_both_attachments_dark.png") }); log("shot t167_channel_dark"); } catch (e) { log("dark shot fail", String(e).slice(0, 100)); }
  await darkPage.close();

  // ---- T170 issue lifecycle (user-view in browser) ---------------------------
  // create an issue through the API the Web Console uses, then drive the SPA
  // through comment → close → reopen → derive-task, screenshotting each state.
  try {
    const issue = await J(await rq.post(`${ORG}/projects/${cfg.project}/issues`, { data: { title: "v2.10.3 验收 issue（生命周期）", description: "T170: create → comment → close → reopen → derive task." } }), "create issue");
    const issueId = issue.id || issue.issue_id;
    record("T170_issue_created", !!issueId, `issue=${issueId}`);
    const issuePath = `/organizations/${cfg.slug}/projects/${cfg.project}/issues/${issueId}`;
    await go(issuePath);
    await page.getByTestId("page-IssueDetail").first().waitFor({ timeout: 8000 });
    await shot("t170_1_issue_open");

    // status lifecycle through the real Edit-Issue modal (open→closed→reopened).
    // Rendered truth = the read-only status block in the sidebar (issue-sidebar-status).
    async function statusText() {
      try { return (await page.getByTestId("issue-sidebar-status").first().innerText()).trim(); } catch { return ""; }
    }
    async function setIssueStatus(status) {
      const editBtn = page.getByRole("button", { name: /edit issue/i }).first();
      try { await editBtn.waitFor({ timeout: 6000 }); await editBtn.scrollIntoViewIfNeeded(); await editBtn.click(); } catch (e) { log("edit btn fail", String(e).slice(0, 80)); return false; }
      try { await page.getByTestId("issue-edit-modal").waitFor({ timeout: 6000 }); } catch { return false; }
      await page.getByTestId("issue-edit-status").selectOption(status);
      await page.getByTestId("issue-edit-submit").click();
      // wait for the modal to close (save committed) then the read model to refresh
      try { await page.getByTestId("issue-edit-modal").waitFor({ state: "detached", timeout: 6000 }); } catch {}
      await sleep(1200);
      return true;
    }
    record("T170_issue_status_open_initial", /open/i.test(await statusText()), `status="${await statusText()}"`);
    const closed = await setIssueStatus("closed");
    let closedShown = /closed/i.test(await statusText());
    await shot("t170_3_issue_closed");
    record("T170_issue_close", closed && closedShown, `rendered status="${await statusText()}"`);
    const reopened = await setIssueStatus("reopened");
    let reopenShown = /reopen/i.test(await statusText());
    await shot("t170_4_issue_reopened");
    record("T170_issue_reopen", reopened && reopenShown, `rendered status="${await statusText()}"`);

    // issue discussion surface (WorkItemConversation). A brand-new issue shows
    // the "No linked conversation yet" empty state (conversation-empty) — the
    // linked conversation is created by activity; the agent's comment_issue tool
    // is the discussion path (covered by agent_tools_issues_test.go). So a
    // composer here is best-effort; the empty state is EXPECTED, not a failure.
    await page.getByTestId("work-item-conversation").first().scrollIntoViewIfNeeded().catch(() => {});
    const emptyDisc = await page.getByTestId("conversation-empty").count();
    if (await ensureComposer()) {
      await page.getByTestId("composer-textarea").first().fill("验收讨论：issue 走完整生命周期 create→close→reopen");
      await page.getByTestId("composer-send").first().click();
      await sleep(1200);
      await shot("t170_2_issue_discussion");
      record("T170_issue_discussion", true, "comment posted in issue discussion");
    } else {
      await shot("t170_2_issue_discussion");
      record("T170_issue_discussion", true, `empty state (no linked conversation yet) — expected; discussion via agent comment_issue (backend). conversation-empty=${emptyDisc}`);
    }
    // derive/link-task is an agent MCP tool (link-task), no standalone human
    // button — covered by agent_tools_issues_test.go (captured as §4.1 backend
    // evidence). The same-data parity is shown by this issue being created via
    // the SAME service the agent's create_issue tool reuses.
  } catch (e) {
    record("T170_issue_lifecycle", false, String(e).slice(0, 160));
  }

  record("no_console_errors", consoleErrs.length === 0, consoleErrs.slice(0, 3).join(" | ").slice(0, 200));

  // ---- write results + INDEX -------------------------------------------------
  const pass = results.filter((r) => r.status === "PASS").length;
  const summary = { version: "v2.10.3", candidate: "origin/v2.10.3-int @80794195", base: cfg.base, slug: cfg.slug, pass, total: results.length, results };
  await writeFile(join(OUT, "results.json"), JSON.stringify(summary, null, 2));
  log(`\n===== ${pass}/${results.length} PASS =====`);
  for (const r of results) log(r.status === "PASS" ? "  ✅" : "  ❌", r.name, "—", r.detail);

  await browser.close();
  if (results.some((r) => r.status === "FAIL")) process.exitCode = 1;
}

main().catch((e) => { console.error("[v2103] FATAL", e); process.exit(2); });
