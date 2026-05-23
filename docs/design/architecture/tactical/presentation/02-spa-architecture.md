# SPA Architecture

> **DDD 战术层** · 主题: presentation / implementation detail
>
> Companion to [01-web-console.md](./01-web-console.md). Covers the React
> SPA's internal layering, state boundaries, SSE wiring, and test stance.

## § 1. Tech stack (locked v2)

| Layer | Choice | Why |
|---|---|---|
| Framework | React 19 | Match supervisor + adapter ecosystem familiarity |
| Build | Vite 5 | Fast dev / lazy chunks / clean go:embed integration |
| Type system | TypeScript strict | catches contract drift early; tsconfig sets `strict + noUnusedLocals + noImplicitReturns + noUncheckedSideEffectImports` |
| Server state | `@tanstack/react-query` v5 | retry + staleTime + cache invalidation policy in one place |
| Cross-page UI state | Zustand 5 | tiny, no boilerplate; deliberately narrow store |
| Router | react-router v7 | lazy + Suspense per route, brought all 14 paths in 1 ST |
| Realtime | native `EventSource` + `useSSE` hook | one connection per app instance |
| Styling | Tailwind + headlessui primitives | no shadcn dep bloat per F1 decision |
| Testing | Vitest + RTL + MSW (Node setupServer) | jsdom env; ≥ 80% coverage gate enforced in CI |

Frozen via `web/package.json`; new deps require a plan ADR.

## § 2. State management — three-tier separation

Every piece of state has exactly one canonical home. Don't mirror.

| Tier | Tool | Examples |
|---|---|---|
| **Server state** | `@tanstack/react-query` | conversation list, messages, agents, secrets metadata, fleet snapshot, trace events |
| **Cross-page UI state** | Zustand single store (`store/app.ts`) | `currentUserId`, `sseStatus`, `sseLastEventId`, `navBadges` |
| **Local component state** | `useState` / `useReducer` | message draft, modal open/closed, form inputs, selection-mode toggle |

Anti-patterns to refuse:
- Mirroring server data into Zustand — kills react-query cache invalidation
- Pushing local form state into the store — kills component encapsulation
- Reading the store from a deep child for data that's already in react-query — fetch directly

## § 3. SSE wiring

**One** EventSource per app instance, opened in `<AppLayout>` mount, closed on unmount. Per-page subscriptions are not used today (the Bus broadcasts all events the user is allowed to see; pages filter via react-query cache scope).

```
                         AppLayout mounts
                                │
                                ▼
                       useSSE() opens
                  EventSource('/api/sse?user_id=...')
                                │
   onerror ──── exp backoff ◀───┤
   onmessage ─────────┐         │
                       ▼         ▼
          dispatchToQueryClient(qc, event)
                      │
                      ▼
        switch event.event_type {
          conversation.*       → invalidate messages/conversations
          input_request.*      → invalidate inputRequests
          workforce.*          → invalidate agents / fleet
          secretmgmt.*         → invalidate secrets
          task_execution.*     → invalidate fleet
          conversation.message_references_added → invalidate refs(child)
          ...
        }
                      │
                      ▼
        react-query refetches affected queries
                      │
                      ▼
        Subscribed pages re-render with new data
```

Critical correctness notes (from F13 SSE wire-in audit):
- The dispatch case names are **literal backend EventType strings** — BC-prefixed (`workforce.agent_instance.created`, `secretmgmt.user_secret.created`) because `sse/fanout.go` passes them through verbatim. Unprefixed names silently never match.
- Backend emits `input_request.canceled` (single-L) — frontend must match exactly.
- See [sse-wiring-audit.md](../../../../plans/sse-wiring-audit.md) for the full table.

Reconnect strategy (computeBackoff):
- `base = 1s, max = 30s, jitter = ±20%`
- attempt counter resets to 0 on successful `onopen`
- `Last-Event-ID` persisted in `useAppStore.sseLastEventId`, sent on reconnect via `?last_event_id=N` query param (backend reads header OR query) — manual reconnect can't set headers via `new EventSource(url)`

Heartbeat timeout:
- `HEARTBEAT_TIMEOUT_MS = 30s` (no event of any kind, including server-sent ping) → force close + reconnect (Safari / iOS half-open guard)

Degraded mode:
- SSE failure does NOT throw / suspend a page. Pages still render react-query stale data; user sees a small "offline" indicator in the topbar (`<SSEIndicator>`).

## § 4. Routing + lazy loading

- 14 page modules + 1 NotFound, each `lazy(() => import('./pages/X'))`.
- `<AppLayout>` wraps the `<Outlet>` in a `<Suspense fallback={<PageFallback />}>` so the per-page chunk streams without blocking the shell.
- Result: main bundle 279 kB / 88 kB gzip; per-page chunks 2–7 kB; all routes work on deep link + reload because the embed handler serves `index.html` for any non-`/api` non-`/assets` path.

## § 5. Component library

Tiny, hand-rolled. The cost of a UI lib > the cost of a few divs.

| Component | Use |
|---|---|
| `MessageList` | timeline rendering; selectable mode for derive |
| `MessageComposer` | Enter to send / Shift+Enter newline / error inline / no re-typing on retry |
| `ParticipantsPanel` | owner-only invite + remove; channel + issue + task surfaces |
| `ChannelCreateModal` / `DMStartModal` | parallel modals for `POST /api/conversations` |
| `RespondInputRequestModal` | option chips + adopt-suggestion (forward-compat) |
| `SecretCreateModal` | type=password + autocomplete=off + value-wipe-before-success per ADR-0026 § 5 |
| `CarryOverDivider` | groups carried messages by source_conv + "Discussion below" divider |
| `TraceTimeline` | collapsible event rows with JSON payload reveal |
| `DeriveBar` + `DeriveModal` + `useSelection` | universal multi-select → open issue/task (CV4 UI) |
| `SSEIndicator` | topbar dot reflecting `sseStatus` |
| `ErrorBoundary` | root-level fallback with reload button |

## § 6. API client

`src/api/client.ts` is a 60-line `fetch` wrapper — no axios. Convention:
- Path prefix `/api` (vite dev proxies to `:7100`, production embedded binary serves from same origin).
- `ApiError(status, code, message)` for typed catches.
- 10s timeout via `Promise.race` (not `AbortController` — jsdom installs a DOM-spec `AbortSignal` that fails undici's `instanceof` check inside MSW v2; `Promise.race` is sufficient for a UI client).
- Throws on non-2xx with the parsed `{error, message}` envelope.

Per-domain hook files in `src/api/`:
- `conversations.ts`, `agents.ts`, `secrets.ts`, `inputRequests.ts`, `fleet.ts`, `derive.ts`
- All hooks lazy + enabled-aware (no fetch when id is undefined).
- `queryKeys.ts` centralizes cache key factories so writers + readers agree on shape.

## § 7. Test stance

Per [phase-11-frontend-plan.md § 6](../../../../plans/phase-11-frontend-plan.md):

- Vitest + RTL + MSW node setupServer (one global instance in `src/test/setup.ts`)
- Coverage gate 80% / 80% / 80% / 80% (lines / branches / funcs / statements) enforced via `vitest.config.ts > test.coverage.thresholds`
- Current snapshot: **188 tests / 39 files / 98.6% lines / 90.4% branches**

Two known fixes-on-the-fly recorded in commit history:
- `await act(async () => await mutateAsync(...))` left React 19's dispatcher in a state where next renderHook's `result.current` was null. **Mitigation**: use sync `.mutate(args)` + `waitFor(isSuccess)` for mutation assertions.
- jsdom's `AbortSignal` fails undici's instanceof check (MSW v2). **Mitigation**: `Promise.race` timeout in api client (see § 6).

Browser-level E2E (Playwright) is deferred to P12 per plan § 6.

## § 8. Build + embed

```
make build (= build-frontend + build-backend)
  ├── pnpm install --frozen-lockfile
  ├── pnpm run build       (vite outDir = ../internal/webconsole/spa/dist)
  ├── restore .gitkeep     (vite emptyOutDir wipes it)
  └── go build -o ./bin/agent-center ./cmd/agent-center
        (internal/webconsole/spa/spa.go has //go:embed all:dist)
```

Runtime path:
```
http://127.0.0.1:7100/anything
  ├── /api/*       → ServeMux pattern wins → JSON handler
  ├── /api/sse     → SSE Bus
  └── /            → spa.Handler() (registered LAST as catch-all)
                        ├── /assets/<hashed>.{js,css,svg} → http.FileServer (verbatim, cacheable)
                        └── anything else → index.html (react-router takes over)
```

Binary size impact: ~17.7 MB total; SPA contributes ~0.5 MB compressed (~700 KB uncompressed).

## § 9. Future-proofing

The single highest-leverage future change is **multi-user auth** (post-v3). The current loopback bind makes it OK to ship; the hooks already pass `currentUserId` through the store, so swapping to real auth means:
1. Add `/api/auth/*` + session cookie middleware
2. Replace Zustand `currentUserId` initialization to read from server
3. ErrorBoundary catches 401 → redirect to login

That work is deferred to v3+ explicitly per [ADR-0037](../../decisions/0037-web-console-loopback-only.md).
