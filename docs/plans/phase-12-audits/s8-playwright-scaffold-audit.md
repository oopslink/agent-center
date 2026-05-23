# P12 S8 — Playwright e2e scaffold + smoke test audit

> Run 2026-05-24 · per x9527 M3 oversight: scaffold lives at
> `tests/e2e/v2/`; playwright.config.ts supports dual chromium-mac
> / chromium-linux projects; helper starts the agent-center binary
> on a temp port + temp sqlite DB and cleans up; first smoke
> verifies the SPA loads. Run smoke 3× to prove non-flaky. Audit
> log lands SEPARATELY from the scaffold commit.

## § 0. Scope

S8 is the M3 entry point. It must demonstrate:

1. Playwright + chromium installed locally; can launch a browser.
2. A reusable Playwright `fixture` that spins up the v2 binary on
   a temp loopback port with an ephemeral sqlite DB + cleans up.
3. A smoke test that hits `http://127.0.0.1:<port>/` and asserts
   the React shell renders (sidebar visible).
4. The smoke test passes 3 consecutive runs (anti-flake gate).

Out of scope for S8: any business scenario tests — those land in
S9-S11.

## § 1. Prereqs verified

- `node v25.6.0` + `pnpm 10.31.0` + `npm 10.31.0` available
  (matches what P11 SPA already uses).
- `./bin/agent-center` Mach-O arm64 binary, freshly built via
  `make build` (includes go:embed'd SPA — `dist/index.html` +
  `assets/index-WrIMvqyt.js` + `assets/index-DS74qIFV.css`).
- Manual smoke (pre-Playwright) — binary serves SPA + API:
  ```
  $ ./bin/agent-center server --config=/tmp/agent-center-test.yaml &
  $ curl -s http://127.0.0.1:18100/      # → index.html
  $ curl -s http://127.0.0.1:18100/api/conversations  # → []
  ```
  Confirmed working.

## § 2. Directory layout

```
tests/e2e/v2/
├── package.json              — pnpm scripts + @playwright/test devDep
├── pnpm-lock.yaml            — locked deps
├── playwright.config.ts      — dual mac/linux projects + artifact paths
├── fixtures/
│   └── agent-center.ts       — start binary on temp port + tempDB; auto-cleanup
├── helpers/
│   └── ports.ts              — pick a free loopback port (race-safe)
├── tests/
│   └── smoke.spec.ts         — load /, assert sidebar visible
├── artifacts/                — trace / video / screenshot (latest run only)
│   ├── .gitignore            — exclude playwright-report html/zip noise
│   └── playwright-report/    — html report (committable per S8 oversight ③)
└── tsconfig.json             — TS strict
```

## § 3. playwright.config.ts design

- `projects`: `chromium-mac` (default; darwin only) + `chromium-linux`
  (headless; runs on Linux CI / docker; skipped on macOS).
- `testDir: 'tests'`
- `fullyParallel: true` — every test spawns its own binary instance
  on its own port, so no shared state.
- `retries: 0` on local; CI may bump to 1. **S8 oversight ⑤**: flake
  fix root cause, not retry-mask.
- `reporter: [['html', {outputFolder: 'artifacts/playwright-report'}], ['list']]`
- `use.trace: 'on-first-retry'`, `screenshot: 'only-on-failure'`,
  `video: 'retain-on-failure'` — artifacts only when something
  actually breaks; size-bounded.
- `workers: 2` on local (chromium spawn ~1s + binary spawn ~1s ⇒ avoid
  saturating port range).

## § 4. Fixture: starting the binary

`fixtures/agent-center.ts` exports a Playwright fixture
`agentCenter` that:

1. Picks two free loopback ports (gRPC + WebConsole) via Node
   `net.createServer().listen(0)` trick.
2. Materializes a temp config YAML pointing sqlite + admin sock +
   web console at unique temp paths (`os.tmpdir()/agent-center-e2e-<id>/`).
3. Spawns `./bin/agent-center server --config=<path>` as a child
   process.
4. Polls `GET /` until 200 OK (with timeout) — proves the server
   is accepting connections.
5. Yields `{baseURL, configPath, dbPath}` to the test.
6. On teardown: `kill SIGTERM`, wait for exit, `rm -rf` temp dir.

## § 5. Smoke test

`tests/smoke.spec.ts`:
- Navigates to `baseURL/`.
- Asserts the SPA shell renders (`<nav>` sidebar present;
  `Channels` link visible — that link is in every page per the P11
  SPA layout).
- Asserts that an XHR to `/api/conversations` succeeds (proves the
  API mux + DB are wired, not just the static SPA).

## § 6. Anti-flake protocol

Run `pnpm test` **3×** back-to-back; all 3 runs must pass with
identical assertion counts. Recorded in § 8 execution log.

If any run fails: investigate root cause (not retry), document fix
in audit, re-run 3× from zero.

## § 7. Artifact policy

- `tests/e2e/v2/artifacts/` is committed to repo (per S8 oversight
  ③). A `.gitignore` inside excludes per-test trace zips and the
  playwright internal cache directory, but **keeps the
  playwright-report html artifact** (the human-readable summary).
- Each run **overwrites** the previous run's artifacts — no
  cumulative history. Size cap: ~5MB after a green run.

## § 8. Execution log

To be filled in by the scaffold commit.

## § 9. M3 risk log

- **chromium-linux project unverifiable on darwin host**: the
  scaffold declares the project but `pnpm test:linux` only runs
  when `process.platform === 'linux'`. macOS host runs the
  chromium-mac project only. CI / Linux VPS verification deferred
  to user (`@oopslink`) — audit will document this explicitly so
  it's not a silent skip.

- **Binary spawn coordination**: each Playwright worker spawns
  its own binary. Worst-case 2 workers × ~50ms binary boot +
  ~1s "port open" poll ⇒ 2s per test setup. Estimated S9-S11
  4 scenarios × 5 sub-tests × 2s setup = ~40s setup overhead;
  acceptable.

- **Port exhaustion**: tests use random free ports per worker; no
  hardcoded ports. If a test leaks a child binary, the next run
  could collide — fixture's `afterEach` enforces kill + wait.
