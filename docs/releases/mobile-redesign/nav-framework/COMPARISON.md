# Nav Framework — Mockup Comparison

Compared against the nav-framework mockup on 2026-07-16.

**Mockup source note:** the plan/task brief cites `docs/design/assets/mobile-redesign-nav-framework-mockup.html`, but that path does not exist in the repo (confirmed via `git log --all` — it was never committed). The actual 4-frame mockup ("移动端全局导航框架 — 定稿 mockup") was located at `.superpowers/brainstorm/50562-1783759437/content/nav-final.html` (a brainstorm scratch artifact, not checked into `docs/`) and used as the comparison reference below. This is a documentation gap worth flagging to the plan owner — the mockup should be copied into `docs/design/assets/` so future verification passes don't have to hunt for it.

**Environment:** real backend + real browser, not vitest-only, per `docs/rules/acceptance-methodology.md` § 1. Built `./bin/agent-center` from this branch's source (`make build`, commit `17cc0fc1`) and spun up an isolated, dynamically-ported sandbox via `agent-center install test-instance --id nav341 --with-seed` (prefix `~/.agent-center-test/nav341/`, does not conflict with any other running instance). Navigated directly to the sandbox's `web_url` (`http://127.0.0.1:58709`, go:embed-served production SPA build of this branch) rather than `pnpm dev`, since the fixed vite proxy target (`127.0.0.1:7100`) can't point at a dynamically-allocated test-instance port without editing tracked config — going straight to the real deployed instance is the more faithful "real usage path" anyway. Browser: `agent-browser` CLI at a 375×812 viewport. Signed in with the seeded owner credentials (`Owner nav341` / seeded org `Alpha` project, 0 attention items).

| Mockup frame | State | Result | Evidence |
|---|---|---|---|
| ① 默认（Projects 列表） | Default content view | Pass | `after_default-view.png` |
| ② 点 ☰ → 二级导航抽屉 | Secondary nav drawer | Pass | `after_secondary-nav-drawer.png` |
| ③ 详情页 → Context Panel 抽屉 | Context Panel drawer | Pass (mechanism-level only; no real page has adopted `useContextPanelMobileTrigger` yet — adoption is per-page in later batch plans per Task 2's rationale) | `pnpm vitest run src/AppLayout.mobilenav.test.tsx -t "Context Panel bottom sheet"` → 1 passed |
| ④ 点 🔔 → Attention 抽屉 | Attention drawer | Pass (empty state — the seeded test-instance org has 0 attention items, so "Nothing needs your attention" renders instead of a populated list; sheet chrome/position verified) | `after_attention-drawer.png` |

## Per-state notes

**① Default view** — bottom tab bar shows exactly 5 tabs: Work / Chat / Teams / Remind / Sys. Confirmed "Sys" (not "System") per Task 1's fix, rendered via the `nav.short.system` i18n key. Top bar shows "☰ Workspace" title button on the left, a bell/alerts icon (⚠️-styled icon, no numeric badge since 0 unread) and an account-menu avatar circle on the right — matches mockup frame ① layout (`☰ Workspace` … `🔔 ●N 👤`).

**② Secondary nav drawer** — tapping the top-bar "Workspace" button opens a `BottomSheet` sliding up from the bottom with a drag handle, a "WORKSPACE" section header, and links for Projects (highlighted active) / Issues / Tasks / Plans, plus additional Workspace-module links (Repos / Templates / Model catalog) beyond the mockup's 4-item illustrative list — consistent with the mockup's intent (list the active module's sections) since the mockup was illustrative, not exhaustive. Verified interaction: clicking "Tasks" navigated to `/organizations/<org>/tasks` and the sheet auto-closed (matches the existing `AppLayout.mobilenav.test.tsx` coverage cited in the plan).

**③ Context Panel drawer** — per Task 2's scope, this mechanism (`useContextPanelMobileTrigger` + the `mobile-context-panel-sheet` `BottomSheet` in `AppLayout.tsx`) is infrastructure only; no shipped page calls it yet. Verified at the mechanism level: `pnpm vitest run src/AppLayout.mobilenav.test.tsx -t "Context Panel bottom sheet"` passes (1 passed), proving a page-level ⓘ trigger opens the bottom sheet with that page's `<ContextPanel>` content. Full page-level visual confirmation is deferred to whichever later batch plan first wires a real page to this mechanism.

**④ Attention drawer** — tapping the bell opens a panel positioned with `fixed inset-x-2 top-12 z-40` (confirmed directly in `web/src/AppLayout.tsx:433`, matching the brief's cited positioning exactly) — i.e. it slides in under the top bar, not from the very bottom, matching mockup frame ④'s intent. The seeded test-instance org (`install test-instance --with-seed`) has no attention items, so the empty state ("Needs your attention 0" / "Nothing needs your attention") rendered instead of a populated list. Tap-to-close verified (second bell tap closes the panel, `mobile-alerts-panel` testid unmounts) — this exercises the same toggle path Task 3's regression test locks in.

## Full test suite

`cd web && pnpm test` — 181 test files, 1640 tests, 0 failures (run 2026-07-16, includes the Context Panel and Attention regression tests referenced above).
