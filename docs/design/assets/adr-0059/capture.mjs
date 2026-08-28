import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const chrome = process.env.CHROME_BIN || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";

const captures = [
  ["access-ram-roles.html", "default", "access-ram-roles-canonical-1672x941.png"],
  ["team-role.html", "default", "team-role-canonical-1672x941.png"],
  ["subject-access.html", "chain", "subject-access-canonical-1672x941.png"],
];

for (const [source, state, output] of captures) {
  const url = new URL(pathToFileURL(resolve(here, source)));
  url.searchParams.set("state", state);
  url.searchParams.set("capture", "1");
  const profile = mkdtempSync(resolve(tmpdir(), "adr0059-capture-"));
  const result = spawnSync(chrome, [
    "--headless=new",
    "--disable-gpu",
    "--disable-background-networking",
    "--disable-component-update",
    "--no-default-browser-check",
    "--no-first-run",
    "--hide-scrollbars",
    "--force-device-scale-factor=1",
    `--user-data-dir=${profile}`,
    "--window-size=1672,941",
    `--screenshot=${resolve(here, output)}`,
    url.href,
  ], { encoding: "utf8", timeout: 10000, killSignal: "SIGKILL" });
  rmSync(profile, { recursive: true, force: true });
  if (!existsSync(resolve(here, output))) {
    process.stderr.write(result.stderr || result.stdout);
    process.exit(result.status || 1);
  }
  process.stdout.write(`${output}\n`);
}
