# Computer Use bundle drift regression report

1. Empty shared bundle + healthy guest-local app: failed on baseline (node_repl absent), passed with local service resolution. Existing host/source-backed configurations also pass.
2. Missing enabled dependencies: failed on baseline (nil error), passed with explicit error and preservation of previous config. Disabled Computer Use remains optional.
3. Runtime package passed in full suite. Initial unrestricted `go test ./...` hit a cleanup deadline in `TestTaskInputPlan569_RealAdminHandlersEndToEnd`; that test passed standalone. Complete `go test -p 2 ./...` then passed. Frontend build (tsc -b + Vite) passed. No concurrent state or goroutine changes.
4. Actual VM screenshot/text acceptance is a deployment gate and remains pending at this commit; unit/config success is not claimed as Computer Use success.

Coverage: targeted configuration suite exercises 100% of the changed service resolver and the new enabled-dependency error branch. Package-wide targeted coverage is 2.9% because unrelated runtime tests were excluded from that coverage run; no claim of whole-package 90% is made. Full suite ran separately.

Operational evidence: production 9e09f2a2, guest-local app 1000968 runs and socket exists; host app 1001067 exists, but guest VirtioFS view of the shared app has no Contents directory. Exact filesystem event that invalidated the shared view is not recorded. The confirmed causal defect is generation depending on that view despite a healthy local installation.
