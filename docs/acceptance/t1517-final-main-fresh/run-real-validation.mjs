import { chromium, request as playwrightRequest, expect } from "../../../tests/e2e/v2/node_modules/@playwright/test/index.mjs";
import crypto from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

const baseURL = process.env.T1517_BASE_URL || "http://127.0.0.1:18123";
const out = path.resolve("docs/acceptance/t1517-final-main-fresh/evidence");
const rawDir = path.join(out, "raw");
const screenshotDir = path.join(out, "screenshots");
const networkDir = path.join(out, "network");
const consoleDir = path.join(out, "console");

await Promise.all([rawDir, screenshotDir, networkDir, consoleDir].map((dir) => fs.mkdir(dir, { recursive: true })));

const checklist = [];
const apiLog = [];
const network = [];
const consoleMessages = [];

function record(name, status, detail, evidence = []) {
  checklist.push({ name, status, detail, evidence });
}

async function writeJSON(file, value) {
  await fs.writeFile(file, JSON.stringify(value, null, 2) + "\n");
}

async function api(ctx, method, url, data, cookie) {
  const options = data === undefined ? {} : { data };
  if (cookie) options.headers = { Cookie: cookie };
  const response = await ctx[method.toLowerCase()](baseURL + url, options);
  const text = await response.text();
  let body = text;
  try {
    body = JSON.parse(text);
  } catch {
    // keep plain text/null bodies as-is
  }
  const entry = { method, url, status: response.status(), body };
  apiLog.push(entry);
  await writeJSON(path.join(rawDir, `${String(apiLog.length).padStart(2, "0")}-${method}-${url.replace(/[^a-zA-Z0-9]+/g, "_").replace(/^_+|_+$/g, "").slice(0, 90)}.json`), entry);
  return { response, status: response.status(), body, text, headers: response.headers() };
}

function assertStatus(actual, expected, label) {
  if (actual !== expected) throw new Error(`${label}: status ${actual}, want ${expected}`);
}

const req = await playwrightRequest.newContext();
try {
  const bootstrap = await api(req, "GET", "/api/auth/bootstrap");
  assertStatus(bootstrap.status, 200, "bootstrap before signup");
  record("fresh bootstrap", bootstrap.body.initialized === false ? "MATCH" : "DEVIATION", `initialized=${bootstrap.body.initialized}`, ["raw/http-bootstrap.json"]);

  const suffix = Date.now().toString(36);
  const signup = await api(req, "POST", "/api/auth/signup", {
    display_name: `t1517-owner-${suffix}`,
    email: `t1517-owner-${suffix}@example.test`,
    passcode: "Smoke123!",
    organization_name: "T1517 Fresh Org",
  });
  assertStatus(signup.status, 201, "signup");
  const setCookie = signup.headers["set-cookie"] || "";
  const cookie = /ac_session=[^;]+/.exec(setCookie)?.[0];
  if (!cookie) throw new Error("signup did not set ac_session cookie");
  const orgSlug = signup.body.organization_slug;
  const orgID = signup.body.organization_id;
  const identityID = signup.body.identity_id;
  const orgAPI = `/api/orgs/${orgSlug}`;
  const accessAPI = `${orgAPI}/access`;
  record("fresh signup", "MATCH", `org=${orgSlug} identity=${identityID}`, []);

  const me = await api(req, "GET", "/api/auth/me", undefined, cookie);
  record("authenticated session endpoint", me.status === 200 ? "MATCH" : "DEVIATION", `status=${me.status}`, []);

  const rolesInitial = await api(req, "GET", `${accessAPI}/ram-roles`, undefined, cookie);
  assertStatus(rolesInitial.status, 200, "initial RAM roles");
  const systemRoles = rolesInitial.body.roles.filter((role) => role.kind === "system");
  record("RAM Role System roles", systemRoles.length >= 1 ? "MATCH" : "DEVIATION", `${systemRoles.length} system roles visible`, []);
  const visibleInternal = rolesInitial.body.roles.filter((role) => role.visibility === "internal" || role.kind === "managed" || role.id.startsWith("role-access-"));
  record("Managed/Internal hidden from RAM catalog", visibleInternal.length === 0 ? "MATCH" : "DEVIATION", `${visibleInternal.length} internal/managed/role-access rows in catalog`, []);

  const createCustom = await api(req, "POST", `${accessAPI}/ram-roles`, {
    name: `Custom reviewer ${suffix}`,
    stable_key: `custom-reviewer-${suffix}`,
    description: "T1517 custom role",
    scope: "team",
    permissions: ["team.read", "team.memory.review"],
  }, cookie);
  assertStatus(createCustom.status, 201, "custom RAM role create");
  const customRole = createCustom.body;
  record("RAM Role Custom create", customRole.kind === "custom" && customRole.latest?.version === 1 ? "MATCH" : "DEVIATION", `kind=${customRole.kind} version=${customRole.latest?.version}`, []);

  const staleCustom = await api(req, "POST", `${accessAPI}/ram-roles/${customRole.id}/versions`, {
    expected_latest_version: 0,
    permissions: ["team.read"],
  }, cookie);
  record("RAM Role 409 stale version", staleCustom.status === 409 ? "MATCH" : "DEVIATION", `status=${staleCustom.status}`, []);

  const updateCustom = await api(req, "POST", `${accessAPI}/ram-roles/${customRole.id}/versions`, {
    expected_latest_version: 1,
    name: customRole.name,
    stable_key: customRole.stable_key,
    description: "T1517 custom role updated",
    scope: "team",
    permissions: ["team.read"],
  }, cookie);
  assertStatus(updateCustom.status, 201, "custom RAM role update");

  const runtime = await api(req, "GET", `${orgAPI}/ai-runtime`, undefined, cookie);
  assertStatus(runtime.status, 200, "runtime catalog");
  let revision = runtime.body.revision;
  if (!runtime.body.clis?.some((cli) => cli.key === "codex")) {
    const cli = await api(req, "POST", `${orgAPI}/ai-runtime/clis`, {
      expected_revision: revision,
      value: { key: "codex", display_name: "Codex", executable: "codex", required_features: [], enabled: true },
    }, cookie);
    assertStatus(cli.status, 201, "create codex CLI");
    revision = cli.body.revision;
  }
  if (!runtime.body.models?.some((model) => model.key === "gpt-5" || model.model_key === "gpt-5")) {
    const model = await api(req, "POST", `${orgAPI}/ai-runtime/models`, {
      expected_revision: revision,
      value: { key: "gpt-5", model_key: "gpt-5", display_name: "GPT-5", compatible_cli_keys: ["codex"], default_parameters: {}, enabled: true },
    }, cookie);
    assertStatus(model.status, 201, "create gpt-5 model");
  }

  const teamRoleA = await api(req, "POST", `${accessAPI}/ram-roles`, {
    name: `Team contributor ${suffix}`,
    stable_key: `team-contributor-${suffix}`,
    description: "T1517 team contributor",
    scope: "team",
    permissions: ["team.read", "project.read", "project.write"],
  }, cookie);
  assertStatus(teamRoleA.status, 201, "team contributor role");
  const teamRoleB = await api(req, "POST", `${accessAPI}/ram-roles`, {
    name: `Team observer ${suffix}`,
    stable_key: `team-observer-${suffix}`,
    description: "T1517 team observer",
    scope: "team",
    permissions: ["team.read"],
  }, cookie);
  assertStatus(teamRoleB.status, 201, "team observer role");

  const teamCreate = await api(req, "POST", `${orgAPI}/teams`, {
    name: `T1517 Platform ${suffix}`,
    description: "RAM Role layering validation",
    visibility: "org-private",
    roles: [
      { role: "developer", cli: "codex", model: "gpt-5", max_concurrency: 2, count: 2, tags: "ram,ui", ram_role_keys: [teamRoleA.body.name] },
      { role: "operator", cli: "codex", model: "gpt-5", max_concurrency: 1, count: 1, tags: "ram", ram_role_keys: [] },
    ],
  }, cookie);
  assertStatus(teamCreate.status, 201, "team create");
  const team = teamCreate.body;

  const mapping = await api(req, "GET", `${orgAPI}/teams/${team.id}/roles/developer/ram-roles`, undefined, cookie);
  assertStatus(mapping.status, 200, "developer mapping read");
  record("Team Role default RAM Role binding", mapping.body.ram_role_ids.includes(teamRoleA.body.id) ? "MATCH" : "DEVIATION", JSON.stringify(mapping.body.ram_role_ids), []);

  const mappingPreview = await api(req, "POST", `${orgAPI}/teams/${team.id}/roles/developer/ram-roles/preview`, {
    ram_role_ids: [teamRoleA.body.id, teamRoleB.body.id],
  }, cookie);
  assertStatus(mappingPreview.status, 200, "mapping preview");
  record("Team Role layering preview", "MATCH", `preview status=200 keys=${Object.keys(mappingPreview.body).join(",")}`, []);

  const mappingSave = await api(req, "PUT", `${orgAPI}/teams/${team.id}/roles/developer/ram-roles`, {
    ram_role_ids: [teamRoleA.body.id, teamRoleB.body.id],
    expected_version: mapping.body.version,
  }, cookie);
  assertStatus(mappingSave.status, 200, "mapping save");

  const mappingConflict = await api(req, "PUT", `${orgAPI}/teams/${team.id}/roles/developer/ram-roles`, {
    ram_role_ids: [teamRoleA.body.id],
    expected_version: mapping.body.version,
  }, cookie);
  record("Team Role mapping 409 stale version", mappingConflict.status === 409 ? "MATCH" : "DEVIATION", `status=${mappingConflict.status}`, []);

  const referencedRevoke = await api(req, "POST", `${accessAPI}/ram-roles/${teamRoleA.body.id}/revoke`, {
    expected_latest_version: teamRoleA.body.latest.version,
    reason: "referenced revoke should fail",
  }, cookie);
  record("RAM Role referenced revoke 409", referencedRevoke.status === 409 ? "MATCH" : "DEVIATION", `status=${referencedRevoke.status}`, []);

  const emptyOperator = await api(req, "GET", `${orgAPI}/teams/${team.id}/roles/operator/ram-roles`, undefined, cookie);
  assertStatus(emptyOperator.status, 200, "operator mapping read");
  record("Direct binding default no Team Role resources", Array.isArray(emptyOperator.body.ram_role_ids) && emptyOperator.body.ram_role_ids.length === 0 ? "MATCH" : "DEVIATION", `operator ram_role_ids=${JSON.stringify(emptyOperator.body.ram_role_ids)}`, []);

  const directMixed = await api(req, "POST", `${accessAPI}/batch/apply`, {
    subject_refs: [`user:${identityID}`],
    permission_keys: ["org.analytics.read"],
    resources: [
      { kind: "org", id: orgID, org_id: orgID, label: "T1517 Fresh Org" },
      { kind: "team", id: team.id, org_id: orgID, label: team.name },
    ],
    reason: "T1517 permission/resource-kind linkage",
  }, cookie);
  assertStatus(directMixed.status, 200, "direct mixed apply");
  const statusByKind = Object.fromEntries(directMixed.body.items.map((item) => [item.resource.kind, item.status]));
  record("Direct binding permission/resource-kind linkage", statusByKind.org === "allowed" && statusByKind.team === "not_applicable" ? "MATCH" : "DEVIATION", JSON.stringify(statusByKind), []);

  const directGrant = directMixed.body.items.find((item) => item.status === "allowed")?.grant_id;
  if (!directGrant) throw new Error("direct grant id missing");
  const directRoleID = "role-access-" + crypto.createHash("sha256").update("org.analytics.read|org").digest("hex").slice(0, 16);
  const directRoleDetail = await api(req, "GET", `${accessAPI}/ram-roles/${directRoleID}`, undefined, cookie);
  record("Managed/Internal direct role hidden by detail", directRoleDetail.status === 404 ? "MATCH" : "DEVIATION", `status=${directRoleDetail.status}`, []);

  const directPreview = await api(req, "POST", `${accessAPI}/grants/revoke/preview`, {
    grant_ids: [directGrant],
    reason: "T1517 direct revoke",
    message: "T1517 direct revoke",
  }, cookie);
  assertStatus(directPreview.status, 200, "direct revoke preview");
  record("Direct binding revoke preview success", directPreview.body.items?.[0]?.status === "allowed" ? "MATCH" : "DEVIATION", JSON.stringify(directPreview.body.items?.[0]), []);

  const directWrongConfirm = await api(req, "POST", `${accessAPI}/grants/revoke/confirm`, {
    grant_ids: [directGrant],
    reason: "T1517 direct revoke",
    message: "T1517 direct revoke",
    preview_id: directPreview.body.preview_id,
    token: "wrong-token",
    idempotency_key: `t1517-wrong-${suffix}`,
  }, cookie);
  record("Direct binding revoke 403/409-class bad token", directWrongConfirm.status >= 400 ? "MATCH" : "DEVIATION", `status=${directWrongConfirm.status}`, []);

  const directConfirm = await api(req, "POST", `${accessAPI}/grants/revoke/confirm`, {
    grant_ids: [directGrant],
    reason: "T1517 direct revoke",
    message: "T1517 direct revoke",
    preview_id: directPreview.body.preview_id,
    token: directPreview.body.token,
    idempotency_key: `t1517-direct-revoke-${suffix}`,
  }, cookie);
  assertStatus(directConfirm.status, 200, "direct revoke confirm");
  record("Direct binding revoke confirm", directConfirm.body.summary?.succeeded === 1 ? "MATCH" : "DEVIATION", JSON.stringify(directConfirm.body.summary), []);

  const derivedGrantID = `grant:org_role:user:${identityID}:org.member.role.manage:org:${orgID}`;
  const derivedPreview = await api(req, "POST", `${accessAPI}/grants/revoke/preview`, {
    grant_ids: [derivedGrantID],
    reason: "T1517 derived revoke blocked",
  }, cookie);
  assertStatus(derivedPreview.status, 200, "derived revoke preview");
  record("Derived revoke blocked/not_applicable", derivedPreview.body.items?.[0]?.status === "not_applicable" ? "MATCH" : "DEVIATION", JSON.stringify(derivedPreview.body.items?.[0]), []);

  const overview = await api(req, "GET", `${accessAPI}/overview`, undefined, cookie);
  assertStatus(overview.status, 200, "access overview");
  const serializedOverview = JSON.stringify(overview.body);
  const visibleRoleAccessCount = (serializedOverview.match(/role-access-/g) || []).length;
  record("No visible role-access-* proliferation in access overview", visibleRoleAccessCount === 0 ? "MATCH" : "DEVIATION", `role-access-* occurrences=${visibleRoleAccessCount}`, []);

  await writeJSON(path.join(rawDir, "api-summary.json"), apiLog);
  await writeJSON(path.join(rawDir, "scenario-ids.json"), { orgSlug, orgID, identityID, teamID: team.id, customRoleID: customRole.id, directGrant });

  const browser = await chromium.launch();
  const context = await browser.newContext({
    baseURL,
    viewport: { width: 1440, height: 900 },
  });
  await context.addCookies([{ name: "ac_session", value: cookie.split("=")[1], domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  const page = await context.newPage();
  page.on("console", (message) => consoleMessages.push({ type: message.type(), text: message.text(), location: message.location() }));
  page.on("response", async (response) => {
    const url = response.url();
    if (url.includes("/api/")) network.push({ method: response.request().method(), url, status: response.status() });
  });

  async function shot(name, url, width, height, waitForTestId) {
    await page.setViewportSize({ width, height });
    await page.goto(url, { waitUntil: "domcontentloaded" });
    if (waitForTestId) await expect(page.getByTestId(waitForTestId)).toBeVisible({ timeout: 15_000 });
    await page.screenshot({ path: path.join(screenshotDir, `${name}-${width}x${height}.png`), fullPage: true });
    const text = await page.locator("body").innerText();
    await fs.writeFile(path.join(rawDir, `${name}-${width}x${height}.txt`), text);
    return text;
  }

  const ramText = await shot("access-ram-roles", `/organizations/${orgSlug}/access/ram-roles`, 1440, 900, "access-roles-view");
  record("UI RAM Roles product names", ramText.includes("RAM Roles") && ramText.includes("Custom reviewer") && !ramText.includes("Access grant org.analytics") ? "MATCH" : "DEVIATION", "RAM Roles page text inspected", ["screenshots/access-ram-roles-1440x900.png"]);
  record("UI no visible role-access-* on RAM Roles", !ramText.includes("role-access-") ? "MATCH" : "DEVIATION", `contains role-access=${ramText.includes("role-access-")}`, []);

  await shot("access-ram-roles", `/organizations/${orgSlug}/access/ram-roles`, 768, 900, "access-roles-view");
  await shot("access-ram-roles", `/organizations/${orgSlug}/access/ram-roles`, 390, 844, "access-roles-view");

  const subjectText = await shot("subject-access", `/organizations/${orgSlug}/access/subject-access`, 1440, 900, "page-Access");
  record("UI Subject access direct binding", subjectText.includes("Subject access") && subjectText.includes("Add direct binding") ? "MATCH" : "DEVIATION", "Subject access page text inspected", ["screenshots/subject-access-1440x900.png"]);
  record("UI no visible role-access-* on Subject access", !subjectText.includes("role-access-") ? "MATCH" : "DEVIATION", `contains role-access=${subjectText.includes("role-access-")}`, []);

  const teamText = await shot("team-role-developer", `/organizations/${orgSlug}/teams/${team.id}/roles/developer`, 1440, 900, "page-TeamRoleDetail");
  record("UI Team Role RAM layering", teamText.includes(teamRoleA.body.name) && teamText.includes(teamRoleB.body.name) && teamText.includes("RAM Roles") ? "MATCH" : "DEVIATION", "Team Role detail text inspected", ["screenshots/team-role-developer-1440x900.png"]);

  const operatorText = await shot("team-role-operator-empty", `/organizations/${orgSlug}/teams/${team.id}/roles/operator`, 1280, 720, "page-TeamRoleDetail");
  record("UI Team Role empty default", operatorText.includes("No RAM Roles") ? "MATCH" : "DEVIATION", "Operator role has no RAM Roles", ["screenshots/team-role-operator-empty-1280x720.png"]);

  await writeJSON(path.join(networkDir, "browser-network.json"), network);
  await writeJSON(path.join(consoleDir, "browser-console.json"), consoleMessages);
  await browser.close();

  const failedNetwork = network.filter((entry) => entry.status >= 400 && !entry.url.includes("/api/version"));
  record("Browser network no unexpected failures", failedNetwork.length === 0 ? "MATCH" : "DEVIATION", `${failedNetwork.length} failing API responses`, ["network/browser-network.json"]);
  const severeConsole = consoleMessages.filter((entry) => ["error"].includes(entry.type));
  record("Browser console no errors", severeConsole.length === 0 ? "MATCH" : "DEVIATION", `${severeConsole.length} console errors`, ["console/browser-console.json"]);

  await writeJSON(path.join(rawDir, "checklist.json"), checklist);
  const deviations = checklist.filter((item) => item.status !== "MATCH");
  await fs.writeFile(path.join(rawDir, "verdict.txt"), deviations.length === 0 ? "PASS\n" : `FAIL: ${deviations.length} deviations\n`);
  if (deviations.length > 0) {
    throw new Error(`validation found ${deviations.length} deviations`);
  }
} finally {
  await req.dispose();
}
