# Computer Use bundle drift regression report

1. Empty shared bundle + healthy guest-local app: failed on baseline (node_repl absent), passed with local service resolution. Existing host/source-backed configurations also pass.
2. Missing enabled dependencies: failed on baseline (nil error), passed with explicit error and preservation of previous config. Disabled Computer Use remains optional.
3. Runtime package passed in full suite. Initial unrestricted `go test ./...` hit a cleanup deadline in `TestTaskInputPlan569_RealAdminHandlersEndToEnd`; that test passed standalone. Complete `go test -p 2 ./...` then passed. Frontend build (tsc -b + Vite) passed. No concurrent state or goroutine changes.
4. Actual VM screenshot/text acceptance passed twice using the deployed main-71cde362 runtime and its generated config/environment:25.53s (thread01a0cc9c-9b94-7c23-80c7-02c2ea9b87ed), then27.16s after an additional targeted runtime restart (thread01a0cc9e-4de8-7513-9f44-0f1cb02f6c14). Both runs assert an actual successful node_repl.js result containing Safari text and screenshotPresent. Runtime PIDs694→1193; config retains required node_repl after regeneration.

Coverage: targeted configuration suite exercises 100% of the changed service resolver and the new enabled-dependency error branch. Package-wide targeted coverage is 2.9% because unrelated runtime tests were excluded from that coverage run; no claim of whole-package 90% is made. Full suite ran separately.

Operational evidence: production 9e09f2a2, guest-local app 1000968 runs and socket exists; host app 1001067 exists, but guest VirtioFS view of the shared app has no Contents directory. Exact filesystem event that invalidated the shared view is not recorded. The confirmed causal defect is generation depending on that view despite a healthy local installation.

Deployment: worker-only main-71cde362; server stays main-9e09f2a2 and healthy. VM reboot refreshed the shared app view and synchronized local native app1001067; no new macOS permission grants were needed. The resolver fix separately covers losing that shared view again. No schema changes or DB migration.
