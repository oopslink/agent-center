# T227 / I7-D1 wakeguard — four gates wired into the live wake path

Closes the T223 run-real NO-GO ("wakeguard 未接线"). The guard is now invoked in
`WakeProjector.enqueueWake` (the real agent→agent wake delivery point) via
`Guard.EvaluateHop`, and **all four gates demonstrably fire** with observable
traces on the projector path — verified by deterministic integration tests built
on the REAL projector + real repos + real Guard (`internal/environment/service`).

## Why integration tests are the run-real-equivalent here

The daemon control loop is still DORMANT (`ControlClient` nil — agent.wake commands
are enqueued onto the worker control stream but not executed by a live session).
So the load-bearing, currently-active layer for storm suppression is the projector's
enqueue decision. The tests drive exactly that layer end-to-end (event → projector →
Guard → ControlLog enqueue-or-suppress), so they ARE the run-real at the active seam.

## How depth ① + cost ④ became real (the D1 gap)

The first wiring (commit f73d442c) minted a FRESH root chain per wake → depth stayed
0 and budget stayed full forever, so only cycle ② + rate ③ could ever fire. The fix
carries the chain across hops via per-agent Guard state (`EvaluateHop`): `to` inherits
and EXTENDS the chain `from` received when it was woken, so a real A→B→C… chain grows
depth and spends budget across deliveries. Staleness is bounded to `CycleWindow`
(an idle gap resets a stale carry to a fresh root — no false depth/cost denials later).

## Trace evidence (go test -run WakeGuard -v)

```
RUN  TestWakeProjector_WakeGuard_CycleBreaks_ABA
  suppressed by wake-chain guard from=A to=B gate=cycle depth=2 reason="round-trip cycle detected for this pair"
PASS

RUN  TestWakeProjector_WakeGuard_HumanBypasses
PASS   (human sender bypasses all gates — rate=0 config still delivers)

RUN  TestWakeProjector_WakeGuard_RateLimitsAgent
  suppressed by wake-chain guard from=D to=B gate=rate depth=0 reason="target agent wake rate exceeded"
PASS

RUN  TestWakeProjector_WakeGuard_DepthBreaks_Chain
  suppressed by wake-chain guard from=D to=E gate=depth depth=3 reason="depth limit reached"
PASS   (A→B→C→D delivered at depth 1/2/3; D→E at depth 4 > MaxDepth 3 suppressed)

RUN  TestWakeProjector_WakeGuard_CostBreaks_Chain
  suppressed by wake-chain guard from=C to=D gate=cost depth=2 reason="chain token budget exhausted"
PASS   (budget 2: A→B, B→C delivered; C→D suppressed — budget spent)
```

unit gates (`internal/cognition/wakeguard`): TestEvaluateHop_DepthGrowsAcrossHops,
TestEvaluateHop_CostSpentAcrossHops, TestEvaluateHop_StaleCarryResets — all PASS.

## Gate coverage matrix

| gate         | fires on live path | test (projector)                          |
|--------------|--------------------|-------------------------------------------|
| ① depth      | yes                | DepthBreaks_Chain (A→B→C→D→E, MaxDepth 3)  |
| ② cycle      | yes                | CycleBreaks_ABA                           |
| ③ rate       | yes                | RateLimitsAgent                           |
| ④ cost       | yes                | CostBreaks_Chain (budget 2)               |
| human bypass | yes (ungated)      | HumanBypasses                             |

## Config / observability

- Config (`MaxDepth/CycleWindow/CycleN/RatePerMin/TokenBudget`) injected as a value
  via `webconsole_wiring.go` (`wakeguard.DefaultConfig()`); the seam for the I7-D3
  settings panel — gate thresholds are NOT hardcoded in the gate logic.
- One Guard singleton per process (holds rate/cycle/carried state shared across
  deliveries).
- Every suppression logs a Trace (from/to/gate/depth/reason).

## Gates / known sandbox note

`make build` ✓, `make lint` ✓, `go vet` ✓, `go test ./internal/cognition/... ./internal/environment/...` ✓.
`go test ./...`: only `internal/agentsupervisor` TestServer_{InjectReachesStdin,
ReadFromOffset,AckTruncateOffsetStability} fail with `connect: invalid argument`
(unix-socket path limitation in this sandbox) — pre-existing on the base branch,
unrelated to this change.
