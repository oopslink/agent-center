# T248 — Reminders list page three-column fix (AFTER evidence)

Issue: issue-c438cde1. The Reminders **list page** rendered its filter rail as a
page-internal `<aside>` inside col③, breaking the v2.10.0 three-column desktop
layout. Fixed to match the approved mockup
(`docs/design/v2.11.0/mockups/reminder-mockup-v0.1-I4.png`): the filter rail
(search + Scope + Status) now lives in **col②** (`RemindersSecondaryNav`), and
the list occupies the **middle column** (col③). Filter state is shared between
the two columns via the URL query (`?range=&status=&q=`).

## AFTER-desktop-3col.png
Desktop (≥768px): col① module rail · col② Reminders filter rail (search / Scope:
All·Created by me·Reminding me / Status: All·Active·Paused) · col③ the list
(Reminders · {scope} header + New reminder + Active/Paused/Next-run stats + the
7-column table). No page-internal sidebar.

## AFTER-mobile-fullscreen.png
Narrow (<768px): col① + col② collapse (the shell renders the filter rail in the
nav sheet, reached from navigation — same mechanism every module uses); the
Reminders list is a single full-screen page, not a sidebar floating over another
page.

Captured from the real `RemindersSecondaryNav` + `Reminders` components rendered
in a faux shell that reuses AppLayout's `md:`-only column pattern, with seeded
data (no backend). Verified by `make build` + `make lint` (green) and the
component tests (`Reminders.test.tsx`, `RemindersSecondaryNav.test.tsx`,
`AppLayout.shell.test.tsx`).
