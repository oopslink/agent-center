// v2.9.2 chat-attachment POLISH run-real verification (task-dccd3a11 终验骨架).
// Verifies dev2's composer polish (62413fc) in a REAL browser: G1 drag&drop,
// G2 paste-screenshot, G3 per-file upload progress, G4 size validation —
// plus end-to-end send/receive of dropped/pasted images and both-mode AA.
// One-shot: `node capture-v292-attachments-polish.mjs`.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, mkdir, rm, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { randomBytes } from "node:crypto";
import { deflateSync, crc32 } from "node:zlib";

const REPO = "/Users/oopslink/works/codes/oopslink/ac-wt-v292acc";
const BIN = resolve(REPO, "bin/agent-center");
const OUT = resolve(REPO, "docs/release/evidence/v2.9.2-attachments-polish-screenshots");
const WEB = 7871, GRPC = 7870;
const BASE = `http://127.0.0.1:${WEB}`;
const API = `${BASE}/api`;
const SLUG = "acme";
const ORG = `${API}/orgs/${SLUG}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log("[pol]", ...a);

const results = [];
const notes = [];
function record(name, ok, detail) { results.push([name, ok ? "OK" : "FAIL", detail || ""]); log(ok ? "✅" : "❌", name, detail || ""); }
async function shot(page, name, locator) {
  try { const path = join(OUT, name + ".png"); if (locator) await locator.screenshot({ path }); else await page.screenshot({ path, fullPage: false }); log("shot", name); }
  catch (e) { log("shot FAIL", name, String(e).slice(0, 120)); }
}

function png(w, h, [r, g, b]) {
  const sig = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
  const chunk = (type, data) => { const len = Buffer.alloc(4); len.writeUInt32BE(data.length, 0); const td = Buffer.concat([Buffer.from(type, "ascii"), data]); const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(td) >>> 0, 0); return Buffer.concat([len, td, crc]); };
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2;
  const row = Buffer.alloc(1 + w * 3); for (let x = 0; x < w; x++) { row[1 + x * 3] = r; row[2 + x * 3] = g; row[3 + x * 3] = b; }
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([sig, chunk("IHDR", ihdr), chunk("IDAT", deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]);
}
const IMG = png(96, 96, [16, 185, 129]); // green
const BIG = png(2200, 2200, [220, 38, 38]); // ~a few MB raw, compresses small — see BIGBYTES for true oversize
// A genuinely >25MB buffer for the oversize-reject gate (random = incompressible, but we pass raw bytes to setInputFiles).
const OVERSIZE = randomBytes(26 * 1024 * 1024); // 26 MB

async function main() {
  await mkdir(OUT, { recursive: true });
  const tempDir = await mkdtemp(join(tmpdir(), "ac-v292pol-"));
  const masterKeyPath = join(tempDir, "master.key");
  await writeFile(masterKeyPath, randomBytes(32).toString("base64") + "\n", "utf8");
  await chmod(masterKeyPath, 0o600);
  const configPath = join(tempDir, "config.yaml");
  await writeFile(configPath, `server:\n  listen_addr: ":${GRPC}"\n  sqlite_path: "${join(tempDir, "ac.db")}"\n  admin_socket_path: "${join(tempDir, "admin.sock")}"\nweb_console:\n  enabled: true\n  listen_addr: "127.0.0.1:${WEB}"\nsecret_management:\n  master_key_file: "${masterKeyPath}"\nblob_store:\n  root: "${join(tempDir, "blobs")}"\n`, "utf8");

  const proc = spawn(BIN, ["server", "--config", configPath], { stdio: ["ignore", "pipe", "pipe"], env: { ...process.env, AGENT_CENTER_INVOCATION_ID: "" } });
  const errBuf = []; proc.stderr?.on("data", (c) => errBuf.push(c)); proc.stdout?.on("data", (c) => errBuf.push(c));
  let up = false; for (let i = 0; i < 80; i++) { try { const r = await fetch(API + "/health"); if (r.ok) { up = true; break; } } catch {} await sleep(100); }
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
  const chan = await J(await rq.post(ORG + "/conversations", { data: { kind: "channel", name: "general", description: "polish test" } }), "channel");
  const cid = chan.conversation_id || chan.id;

  const page = await ctx.newPage();
  const consoleErrs = []; page.on("console", (m) => { if (m.type() === "error") consoleErrs.push(m.text()); });
  await page.addInitScript(() => localStorage.setItem("ac.theme", "light"));
  const go = async (p) => { await page.goto(BASE + p, { waitUntil: "domcontentloaded" }); await sleep(1200); };
  const CH = `/organizations/${SLUG}/channels/${cid}`;

  // synthetic DnD / paste helpers — build a File in-page and dispatch the event
  // dev2's handlers read (drag: dataTransfer.types includes 'Files'; paste: clipboardData.files).
  const b64 = (buf) => buf.toString("base64");
  async function dispatchFileEvent(kind, name, mime, b64bytes) {
    return page.evaluate(async ({ kind, name, mime, b64bytes }) => {
      const bin = atob(b64bytes); const arr = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
      const file = new File([arr], name, { type: mime });
      const dt = new DataTransfer(); dt.items.add(file);
      const form = document.querySelector('[data-testid="message-composer"]');
      const ta = document.querySelector('[data-testid="composer-textarea"]');
      const typesHasFiles = Array.from(dt.types).includes("Files");
      if (kind === "drag") {
        form.dispatchEvent(new DragEvent("dragenter", { bubbles: true, cancelable: true, dataTransfer: dt }));
        form.dispatchEvent(new DragEvent("dragover", { bubbles: true, cancelable: true, dataTransfer: dt }));
        window.__dt = dt;
        return { typesHasFiles, dtFiles: dt.files.length };
      }
      if (kind === "drop") {
        form.dispatchEvent(new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: window.__dt || dt }));
        return { typesHasFiles, dtFiles: dt.files.length };
      }
      if (kind === "paste") {
        const ev = new ClipboardEvent("paste", { bubbles: true, cancelable: true, clipboardData: dt });
        ta.dispatchEvent(ev);
        return { typesHasFiles, clipFiles: ev.clipboardData ? ev.clipboardData.files.length : -1 };
      }
    }, { kind, name, mime, b64bytes });
  }

  // ===================== G4: size validation =====================
  await go(CH);
  await page.getByTestId("message-composer").first().waitFor({ timeout: 8000 });
  // oversize (>25MB) → rejection
  await page.getByTestId("composer-file").first().setInputFiles({ name: "huge.png", mimeType: "image/png", buffer: OVERSIZE });
  let rejected = false, rejText = "";
  try { await page.getByTestId("composer-rejection").first().waitFor({ timeout: 4000 }); rejected = true; rejText = (await page.getByTestId("composer-rejection").first().innerText()).trim(); } catch {}
  record("G4_oversize_rejected", rejected && /25 MB|exceeds/i.test(rejText), `reject="${rejText}"`);
  await shot(page, "G4_1_oversize_rejection");
  // empty file → rejection "is empty"
  await page.getByTestId("composer-file").first().setInputFiles({ name: "empty.txt", mimeType: "text/plain", buffer: Buffer.alloc(0) });
  let emptyRej = "";
  try { await page.getByTestId("composer-rejection").first().waitFor({ timeout: 4000 }); emptyRej = (await page.getByTestId("composer-rejection").first().innerText()).trim(); } catch {}
  record("G4_empty_rejected", /empty/i.test(emptyRej), `reject="${emptyRej}"`);
  // valid small image → stages, no rejection
  await page.getByTestId("composer-file").first().setInputFiles({ name: "ok.png", mimeType: "image/png", buffer: IMG });
  const staged = await page.getByTestId("composer-attachment").count();
  record("G4_valid_staged", staged > 0, `${staged} staged chip(s)`);
  await shot(page, "G4_2_valid_staged");
  // clear staged by reload (fresh composer)
  await go(CH);

  // ===================== G1: drag & drop =====================
  await page.getByTestId("message-composer").first().waitFor({ timeout: 8000 });
  const dragRes = await dispatchFileEvent("drag", "dropped.png", "image/png", b64(IMG));
  let dropzone = false;
  try { await page.getByTestId("composer-dropzone").waitFor({ timeout: 2000 }); dropzone = true; } catch {}
  record("G1_dropzone_overlay", dropzone, `dragenter→overlay; types.Files=${dragRes?.typesHasFiles}`);
  await shot(page, "G1_1_dropzone_overlay");
  await dispatchFileEvent("drop", "dropped.png", "image/png", b64(IMG));
  let dropStaged = false;
  try { await page.getByTestId("composer-attachment").first().waitFor({ timeout: 3000 }); dropStaged = (await page.getByTestId("composer-attachment").count()) > 0; } catch {}
  record("G1_drop_staged", dropStaged, dropStaged ? "dropped file staged" : "drop did not stage");
  await shot(page, "G1_2_drop_staged");

  // send the dropped image → receives end-to-end
  if (dropStaged) {
    const before = await page.getByTestId("message-attachment").count();
    await page.getByTestId("composer-send").first().click();
    let recv = false; for (let i = 0; i < 30; i++) { if ((await page.getByTestId("message-attachment").count()) > before) { recv = true; break; } await sleep(400); }
    record("G1_drop_received", recv, recv ? "dropped image rendered in message list" : "not rendered");
    await shot(page, "G1_3_drop_received");
  }

  // ===================== G2: paste screenshot =====================
  await go(CH);
  await page.getByTestId("message-composer").first().waitFor({ timeout: 8000 });
  const pasteRes = await dispatchFileEvent("paste", "pasted.png", "image/png", b64(IMG));
  notes.push("paste synthetic event clipFiles=" + JSON.stringify(pasteRes));
  let pasteStaged = false;
  try { await page.getByTestId("composer-attachment").first().waitFor({ timeout: 3000 }); pasteStaged = (await page.getByTestId("composer-attachment").count()) > 0; } catch {}
  record("G2_paste_staged", pasteStaged, pasteStaged ? "pasted screenshot staged" : `not staged (clipboardData.files=${pasteRes?.clipFiles})`);
  await shot(page, "G2_1_paste_staged");
  if (pasteStaged) {
    const before = await page.getByTestId("message-attachment").count();
    await page.getByTestId("composer-send").first().click();
    let recv = false; for (let i = 0; i < 30; i++) { if ((await page.getByTestId("message-attachment").count()) > before) { recv = true; break; } await sleep(400); }
    record("G2_paste_received", recv, recv ? "pasted image rendered" : "not rendered");
    await shot(page, "G2_2_paste_received");
  }

  // ===================== G3: upload progress =====================
  await go(CH);
  await page.getByTestId("message-composer").first().waitFor({ timeout: 8000 });
  // throttle the PUT so the progress bar is observable in real time
  await page.route("**/files/transfer/**", async (route) => { try { if (route.request().method() === "PUT") { await sleep(1500); } await route.continue(); } catch { /* route already handled / context closing */ } });
  await page.getByTestId("composer-file").first().setInputFiles({ name: "progress.png", mimeType: "image/png", buffer: BIG });
  await page.getByTestId("composer-attachment").first().waitFor({ timeout: 4000 });
  await page.getByTestId("composer-send").first().click();
  // poll for the progressbar element / aria-valuenow during upload
  let sawProgressbar = false, maxVal = -1, sawUploadingShot = false;
  for (let i = 0; i < 60; i++) {
    const pb = page.getByTestId("composer-attachment-progress");
    if (await pb.count()) {
      sawProgressbar = true;
      const v = await pb.first().getAttribute("aria-valuenow");
      if (v != null) maxVal = Math.max(maxVal, Number(v));
      if (!sawUploadingShot) { await shot(page, "G3_1_uploading_progress"); sawUploadingShot = true; }
    }
    // done when the attachment lands in the message list
    if ((await page.getByTestId("message-attachment").count()) > 0) break;
    await sleep(80);
  }
  record("G3_progressbar_shown", sawProgressbar, sawProgressbar ? `progressbar role rendered, max aria-valuenow=${maxVal}` : "no progressbar element observed");
  let g3recv = false; for (let i = 0; i < 20; i++) { if ((await page.getByTestId("message-attachment").count()) > 0) { g3recv = true; break; } await sleep(300); }
  record("G3_progress_received", g3recv, g3recv ? "file uploaded+sent after progress" : "not received");
  await shot(page, "G3_2_progress_done");
  await page.unroute("**/files/transfer/**");

  // ===================== both-mode AA spot =====================
  await page.addInitScript(() => localStorage.setItem("ac.theme", "dark"));
  await go(CH);
  await page.getByTestId("message-composer").first().waitFor({ timeout: 6000 });
  // dark dropzone overlay
  await dispatchFileEvent("drag", "d.png", "image/png", b64(IMG));
  await shot(page, "H_dark_dropzone");
  await dispatchFileEvent("drop", "d.png", "image/png", b64(IMG));
  // dark rejection notice
  await page.getByTestId("composer-file").first().setInputFiles({ name: "huge.png", mimeType: "image/png", buffer: OVERSIZE });
  await sleep(600);
  await shot(page, "H_dark_rejection_and_received");

  log("CONSOLE ERRORS:", consoleErrs.length); consoleErrs.slice(0, 12).forEach((e) => log("  err:", e.slice(0, 160)));
  log("\n==== RESULTS ===="); results.forEach(([n, s, d]) => log(`  ${s === "OK" ? "✅" : "❌"} ${n}: ${s} ${d}`));
  log("\n==== NOTES ===="); notes.forEach((n) => log("  • " + n));
  await writeFile(join(OUT, "RESULTS.json"), JSON.stringify({ results, notes, consoleErrors: consoleErrs.slice(0, 20), bin: BIN, commit: "62413fc" }, null, 2));

  await browser.close(); proc.kill("SIGTERM"); await sleep(800); if (!proc.killed) proc.kill("SIGKILL"); await rm(tempDir, { recursive: true, force: true });
}
main().catch((e) => { console.error("FATAL", e); process.exit(1); });
