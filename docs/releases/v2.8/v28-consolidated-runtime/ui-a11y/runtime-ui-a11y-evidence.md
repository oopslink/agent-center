# v2.8 Consolidated Runtime — Tester2 §3.3/§4.3 UI/a11y Evidence

- **Instance**: acme-t1 @ `http://127.0.0.1:50265` (oopslink@home, same-machine direct)
- **Build (initial run)**: trunk `ae4697a` (v2.8 100%, 12 PRs)
- **Re-verify build (#192 fix)**: trunk `8780511` (PR #193 micro-patch) — pending instance rebuild
- **Method**: Playwright run-real — computed-truth (DOM/aria/getComputedStyle), real keyboard, real XSS injection. NOT parity/code-only.
- **Signin**: display_name `Owner t1` / `123456` (NOT email; form field = `#display_name`). Peer: `Peer Bob` / `178972`.
- **Harness**: `runs/v28-consolidated/*.py` (agent workspace)

## Verdict matrix (7-PR runtime)

| PR | item | verdict | key run-real evidence | shot |
|----|------|---------|----------------------|------|
| #182 | P1 unread/mention/follow badges | ✅ PASS | Peer Bob `@Owner t1 ` → sidebar `conversation-mention-badge`="1" + aria-label "1 unread, 1 mention" (not-color-only/SR); unread-dot=0 correct (mention supersedes dot, #176 contract); follow-toggle aria-pressed=true | 31,34 |
| #185 | (archived) chip | ✅ PASS | /tasks OrgWorkItemsView renders `(archived)` chip (testid `org-workitem-assignee-archived`), reads nested `assignee.assignee_lifecycle` | 32 |
| #187 | markdown + XSS strict-escape | ✅ PASS | injected `<script>`/`<img onerror>`/`javascript:` → script-elems=0, img[onerror]=0, **window.__pwn undefined (no execution)**, js-link href=0; ```js fence → CollapsibleCodeBlock; inline **bold**/_italic_/~~strike~~ render | 35,36 |
| #188 | Activity Load-older + Checking-fold | ✅ PASS | ≤50 → "No more activity" terminal + no Load-older button (correct); Checking-fold group renders w/ disclosure (aria-expanded=false). >50 multi-page = fakeagent-bounded (cursor triple-proven code+data+run-real) | 40 |
| #189 | tool rich-render (SVG, not emoji) | ✅ PASS | activity badges carry SVG icon (NOT emoji char) + tool_result `data-tool-status`; tool_result row-expand → CollapsibleCodeBlock | 22,41 |
| #190 | WorkerDetail 4-tab + dangling | ✅ PASS | **manual tab arrow-nav RUNTIME-confirmed**: ArrowRight → aria-selected unchanged (focus-only, no async fetch), Enter → activates; Profile id-as-content + status not-color-only + 4 tabs + "Coming v2.9"; **dangling**: deleted worker → `worker-<hex>` plain span (href=null, not broken link, #215 deleted-ref fallback), availability=unavailable, no crash | 20,21,50 |
| #192 | #/@ picker ARIA combobox | 🔧 2 findings FIXED (PR #193) → runtime re-verify pending rebuild | aria-activedescendant **by stable option-id RUNTIME-confirmed** (= option-id, not DOM-index; ArrowDown changes it by id) + listbox/combobox + not-color-only + email-no-trigger + No-matches PASS | 10-14 |

## Runtime findings (caught by run-real; unit/code were green)

1. **FINDING-1 — Esc no-op (picker not dismissed)**: Esc → aria-expanded stays true (30ms+530ms, no flicker = pure no-op); only Backspace (delete trigger) closes. Root-cause (Dev): composer `onKeyUp={sync()}` re-detects the trigger same-frame → reopens. → **FIXED PR #193** (dismissed-trigger tracking + composer-integration regression test). Code+test re-verified PASS; runtime re-verify pending rebuild.
2. **FINDING-2 — picker option exposed raw internal id as visible secondary** (`user-<8hex>`): #192 chrome should be hover-only. → **FIXED PR #193** (hover-only `title`, visible=name only; regression: `queryByText(id)` not in document). Code+test re-verified PASS; runtime re-verify pending rebuild.

## False-alarms self-ruled-out (闻报先证伪)

- **#182 no-badge** → my own prior channel-view cleared the unread (#268 view auto-advance); re-tested with fresh Peer Bob mention, checked badge BEFORE viewing → renders. (Convergent with Tester's independent author-auto-advance false-alarm on the same trap.)
- **#187 inline-bold not matched** → harness exact-match + same-line escaped-HTML context; clean markdown renders fine.
- **#189 no-codeblock** → default-collapsed row; expand reveals CollapsibleCodeBlock.

## Shipped-build 8780511 re-verify (acme-t3 @ 58687, build with PR #193 fix) — DONE, 7/7 PASS

- **#192 FINDING-1 (Esc) FIXED ✅**: Esc → aria-expanded=false @40ms+540ms (no keyup-sync reopen) + listbox gone; new trigger reopens (dismissal expires). shots 60.
- **#192 FINDING-2 (id) FIXED ✅**: option visible text = "Owner t3" only (no raw `user-<hex>`; was "Owner t1\nuser-3c02457e" on ae4697a). shots 61.
- **#182 badge ✅**: mention-badge="1" + aria "1 unread, 1 mention" (checked before opening channel, avoiding #268 trap). shots 62.
- **#185 chip ✅**: "(archived)" renders (task-0cc6520c ArchivedBot). shots 63.
- **#190 dangling ✅**: DanglingBot AgentDetail renders deleted `worker-f3a0b973` as plain SPAN href=null (#215 fallback) + unavailable + no crash. shots 64.
- **minor ✅**: inline **bold** → `<strong>`; 25-line fenced → CollapsibleCodeBlock collapsed + "Show more" (>20 trigger). shots 65.
- aria-activedescendant by option-id re-confirmed on shipped; 0 console errors.

= **§3.3/§4.3 consolidated runtime COMPLETE: 7/7 PASS on shipped trunk 8780511** (2 findings caught by run-real → fixed PR #193 → runtime-re-verified; 3 false-alarms self-ruled-out). API↔UI同源 jointly confirmed with Tester (data/API 7/7 PASS on shipped). Evidence §21 chain-commit (git-tracked, `git ls-tree` verified) per the v2.7.1 evidence-in-tree lesson.
