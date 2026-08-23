import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const specs = [
  {
    source: "access-ram-roles.html",
    png: "access-ram-roles-canonical-1672x941.png",
    states: ["default", "loading", "forbidden", "empty", "search", "filter", "pagination", "detail", "create", "edit", "version-conflict", "delete-typed", "reference-blocked"],
    accessNav: true,
    required: ["Used by Team Roles", "(read-only)", "Open Team Role", "Publish version", "Version conflict"],
  },
  {
    source: "team-role.html",
    png: "team-role-canonical-1672x941.png",
    states: ["default", "edit", "member-impact", "member-removal-impact", "save-readback", "conflict"],
    required: ["Runtime configuration", "Edit RAM Roles", "Affected members", "Save changes", "Refresh latest"],
  },
  {
    source: "subject-access.html",
    png: "subject-access-canonical-1672x941.png",
    states: ["chain", "direct-coexist", "grant-preview", "forbidden", "conflict", "revoke", "derived-revoke-blocked", "deny-precedence"],
    accessNav: true,
    required: ["Permission source chain", "Direct source", "Grantable", "Version conflict (409)", "Confirm revoke", "Explicit deny takes precedence"],
  },
];

let failures = 0;
const check = (condition, message) => {
  if (!condition) { failures += 1; process.stderr.write(`FAIL ${message}\n`); }
  else process.stdout.write(`PASS ${message}\n`);
};

for (const spec of specs) {
  const html = readFileSync(resolve(here, spec.source), "utf8");
  const png = readFileSync(resolve(here, spec.png));
  const width = png.readUInt32BE(16);
  const height = png.readUInt32BE(20);
  check(png.subarray(1, 4).toString() === "PNG", `${spec.png} is PNG`);
  check(width === 1672 && height === 941, `${spec.png} is 1672x941`);
  for (const state of spec.states) check(html.includes(`value="${state}"`), `${spec.source} exposes ${state}`);
  for (const phrase of spec.required) check(html.includes(phrase), `${spec.source} contains ${phrase}`);
  const retiredUiLabels = ["Roles " + "& mappings", "Team Role " + "mappings"];
  for (const phrase of retiredUiLabels) check(!html.includes(phrase), `${spec.source} excludes retired UI label`);
  if (spec.accessNav) {
    const side = html.match(/<aside class="side"[\s\S]*?<\/aside>/)?.[0] || "";
    check((side.match(/class="nav-item/g) || []).length === 2, `${spec.source} has exactly two Access nav items`);
    check(side.includes("RAM Roles") && side.includes("Subject access"), `${spec.source} has the two canonical Access labels`);
  }
  if (spec.source === "access-ram-roles.html") {
    check(html.includes('<select class="form-input" id="ram-role-risk" name="risk">'), "RAM Role create/edit drawer exposes Risk selector");
    check(html.includes('data-delete-confirm data-stable-key="admin-token-manager" value=""'), "delete confirmation input starts empty with exact stable key contract");
    check(html.includes('data-delete-action disabled'), "delete action starts disabled");
    check(html.includes("deleteButton.disabled = confirmInput.value !== confirmInput.dataset.stableKey"), "delete action enables only for the exact stable key");
  }
}

if (failures) process.exit(1);
