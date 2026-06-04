# UX Standards

This document codifies UX rules for the agent-center Web Console. It is the
canonical reference for all frontend work from v2.7 onwards. Any deviation
must be justified in the PR description and called out for product owner
sign-off.

The rules below grew out of v2.7 acceptance feedback: every entry below
exists because we shipped (or almost shipped) something that confused or
misled a user. They are listed in plain language with their failure mode
first — read them as "do this, and here is what goes wrong if you don't."

---

## 1 — Entity selection: dropdown + search filter (never free text)

**Rule.** Any UI element that asks the user to pick an entity — a member,
an agent, a project, a worker, a task, a channel — MUST be a dropdown /
typeahead control with a search filter over the candidate set. Free text
input ("type `agent:<id>`") is forbidden.

**Why.** A raw identity-ref input is opaque to humans, allows typos that
silently fail, requires the user to know an internal format, and produces
unhelpful errors when wrong. A search-filtered dropdown shows what is
selectable, lets the user pick by name, and makes invalid choices
impossible.

**How to apply.** Reuse `MemberInviteModal` (channel-invite, #167) as the
canonical pattern: an `Invite` / `Assign` / `Select` button opens a modal
or popover with a search box that filters the candidate list (rendered by
name with a kind / status badge), supports multi-select where the action
allows it, and submits via the kind-aware ref (`user:<id>` / `agent:<id>`).
Disabled / ineligible candidates remain visible but unselectable with a
hover-tooltip reason.

## 2 — Display names everywhere; identity refs hide behind hover

**Rule.** Anywhere we render an entity in the UI — message senders,
participant lists, conversation owners, project / agent / worker links,
audit columns, task assignees — we show the entity's **display name**.
The underlying identity ref (`user:<id>` / `agent:<id>` / `T123`) is
acceptable in a hover tooltip / right-click "Copy ID", **never** as the
primary label.

**Why.** Raw refs leak our internal addressing into the operator's mental
model; they are unreadable, they make screenshots unhelpful for support,
and changes to the ref format (e.g., the v2.7 Phabricator-style ID
migration, #187) ripple into every screen.

**How to apply.** Backend list / detail endpoints enrich entity payloads
with `display_name` (see `/api/members`, #160). The frontend uses a
`useDisplayNameResolver` hook (or equivalent for the entity type) to map
ref → name with a fallback to the raw ref **only** when the resolver
genuinely has no record (e.g., a left-org member referenced in
historical audit data). Hover provides the ref for power users; rendering
a raw ref as the primary label is a review-blocking finding.

### 2a — DMs render the *other* party's name, not the DM's own ID

**Sub-rule.** A direct-message conversation is identified to the user by
**the name of the other participant(s)**, not by the conversation's own
ID. In the DM list, the DM header, the inbox, and any DM
cross-reference, the label is "Alice" (or "Alice, Bob, Carol" for a
group DM with three other people), never "DM 01KT5VYZQ5HZWSF7SOVOQTHXTM"
or "01KT…". The DM's conversation ID is acceptable on hover, in URLs,
and in audit logs — never as the primary label.

**Why.** Users think of DMs as "my conversation with Alice", not as a
named conversation with an opaque ID. A DM list rendered by conversation
ID is unreadable — the user has to open each DM to discover whom it is
with. This is the same Rule 2 principle, specialised to the DM case
where the entity name is *another participant*, not the conversation
itself.

**How to apply.** The DM list rendering computes `display_name` by
filtering out the current viewer's identity from the participant list
and joining the remaining names ("Alice" for one, "Alice, Bob" for two,
"Alice and 2 others" if the list grows past two). The same rule applies
to the DM header and any cross-references in messages or notifications.
For a hypothetical 1-on-1 DM where the viewer is the only remaining
participant (the other party left), fall back to "Empty DM" rather than
the raw ID.

### 2b — The raw-ID boundary: chrome vs content vs id-as-content

**Sub-rule.** Rule 2's "display names, refs behind hover" governs
**chrome** — not content the user or an agent authored, and not a surface
whose whole purpose is to *be* an identifier. Three categories, ruled
during the v2.7.1 retrospective (see
`docs/rules/v271-retrospective.md` § 6):

- **Chrome** — entity references rendered as part of the page frame
  (message-sender labels, sidebar items, breadcrumb leaves, participant
  lists, assignees, "by &lt;creator&gt;"). **Strict**: the user sees the
  display name; the raw ID appears only on hover (`title`). Deleted
  entities render `(deleted)`.
- **Content** — material the user authored or an agent produced (message
  bodies, `tool_use` arguments, expandable JSON payload viewers, agent
  thinking text). **Exempt**: if a user typed `task-abc12` into a
  message, the message shows exactly that. The acceptance sweep skips the
  JSON-viewer subtree by its testid (`agent-activity-payload-json`).
- **Id-as-content** — a surface whose rendered value *is* the identifier
  (the table `ID` column, a short hash handle, the URL segment).
  **Chrome by design**: show a short form with the full ID on hover.
  This is the git-short-SHA / GitHub-`#123` idiom.

**Why.** Rule 2 read literally would forbid an ID anywhere, which is
wrong: a chat message quoting an ID, a debug JSON viewer, and a table's
ID column are all legitimate. Naming the three categories keeps the
strict rule strict — no quietly broadening "chrome" to excuse a real
leak — while not flagging content or id-as-content as violations.

**How to apply.**

- Chrome references use the `EntityRef` component (`id` + resolved
  `name`, optional `to` link); never hand-assemble a `<span>` around a
  raw ID.
- An id-as-content handle uses the ULID **tail** segment
  (`id.slice(-6)`), not the head — the ULID head is a millisecond
  timestamp, so entities created in the same window collide on the
  prefix (`#126`: three work-items all rendered `#01KT8Q`). The full ID
  goes on hover (`title`). When a real human-facing sequence exists
  (`org_ref` `T<n>` / `I<n>`, `#245`), show that instead.
- Detail-page URLs use the entity hash ID as the path segment
  (`/projects/:id`, `/channels/:channelId` — the latter unified from
  by-name in `#247`); chrome on the page still renders the name.
- API writes that carry an **identity** reference send the prefixed
  form (`agent:<id>` / `user:<id>`); a bare business ID is rejected
  `400` by the backend. Entity IDs (project / task / issue / channel)
  are sent bare. **Target state**: every identity-ref producer routes
  through one shared helper (`identityRef(kind, id)`) so no call site can
  forget the prefix. **Current status**: the prefix is still assembled
  per-component (`DMStartModal` / `ProjectMemberAddModal` have local
  helpers; `AssignModal` and the AgentDetail Message button inline
  `agent:${id}`) — that duplication is the root cause of `#240` (the new
  Message button re-inlined and dropped the prefix → `400`). Converging
  on the shared helper is tracked as `#254` (v2.8). The
  identity-ref-vs-entity-ID rule itself is canonical in
  `docs/rules/conventions.md` § 12.x (ADR-0033).
- The `#192` acceptance sweep encodes exactly this split: it walks
  `inner_text`, treats the JSON-viewer subtree as content-exempt, and
  accepts id-as-content table cells. When a genuinely new category
  appears, name it, document it here, and add its testid to the sweep —
  do not silently broaden "chrome" or "content".

## 3 — In-app modals; never native browser dialogs

**Rule.** Confirmation, alert, and prompt UIs are in-app components
(`ConfirmModal`, `AlertModal`, etc.). `window.confirm`,
`window.alert`, `window.prompt` are banned.

**Why.** Native browser dialogs render the host URL ("`127.0.0.1:7100`
says…"), break the visual language of the app, are inaccessible to our
focus-trap / keyboard handling, and feel unprofessional. They were the
single most reported "this looks broken" item in v2.7 acceptance.

**How to apply.** Reuse `ConfirmModal` (built on `useModalA11y`) for any
yes/no, destructive, or acknowledge interaction (see #169 for the v2.7
sweep). The rule is enforced in the build pipeline by an ESLint
`no-restricted-globals` + `no-restricted-properties` rule wired into
`make lint` — both bare globals and `window.confirm` member access are
blocked. Removing or bypassing those ESLint rules requires a
documented exception and PD review.

## 4 — One UI language; English throughout

**Rule.** The Web Console UI ships in English. Page copy, button labels,
error messages, toasts, modals, and inline help are all English. No
mixed Chinese / English strings.

**Why.** Mixed-language UIs look unfinished and confuse non-native
operators in both directions. v2.7's Organization Settings page shipped
with several Chinese labels left over from prototyping — that confused
every English-speaking operator and was an obvious "this isn't ready"
signal.

**How to apply.** A `grep` for CJK characters across `web/src` in PR
review is the simplest enforcement; this should eventually be added to
`make lint` as a hard rule. Translation is a separate, post-v2.7
concern — until then, English is the only canonical surface.

## 5 — Spell out names; no in-product abbreviations

**Rule.** Use the full word for product nouns: "Organization", not
"Org"; "Conversation", not "Conv"; "Project", not "Proj". This applies
to sidebar labels, page titles, toast wording, button text, form
labels, and modal copy. Internal variable names and code comments are
exempt — this is about user-facing strings only.

**Why.** Abbreviations make the product feel hurried and grow in
ambiguity over time ("Org" reads as part of a brand to some readers,
short for "Organization" to others). v2.7's Member sidebar group used
"Agents (organization)" deliberately to distinguish from System →
Agents — that disambiguation now lives in the sidebar IA (see § 7),
so the parenthetical is dropped (single Agents entry per #165).

**How to apply.** PR review checks user-facing strings against this
rule; #151 was the v2.7 cleanup sweep. New abbreviations require a
plain-language justification in the PR description.

## 6 — Entity IDs are Phabricator-style; surfaced sparingly

**Rule.** Every user-visible entity has an ID that follows the
prefix-then-payload pattern. Task and Issue use a monotonic integer
suffix (`T123`, `I456`) — these are the things humans cite in
conversation, so the ID is itself a part of the product surface.
Project, Agent, Worker, DM use a short opaque hash suffix (`P<hash>`,
`A<hash>`, `W<hash>`, `D<hash>`). Channel uses a bare hash (no prefix)
to align with the familiar `#name` channel-link idiom.

**Why.** ULIDs are great identifiers but they are unmemorable, look
identical across entity types, and force the user to copy / paste rather
than speak about them. Phabricator-style IDs ("T123") are short,
type-tagged, and quotable in conversation. Splitting tasks/issues
(integer) from infrastructure entities (hash) reflects how often each
appears in plain conversation — operators reference T123 daily, A-foo
rarely.

**How to apply.** New entity types pick a prefix consistent with the
above (PM-style → integer; infra → hash). Cross-references in copy and
chat use `#T123` / `#abc-channel` syntax, which the renderer will
linkify (deferred to v2.8, #190). The ID itself stays subordinate to
the entity's display name in the UI (see § 2).

## 7 — Pages identify themselves; breadcrumbs at the top

**Rule.** Every detail page has a breadcrumb or page-identity header at
the top that names (a) the kind of thing the user is looking at and
(b) where it sits in the hierarchy. A user landing on a deep link
should know "this is a task page" without scrolling, looking at the
URL, or comparing to a sibling page.

**Why.** v2.7's task detail page rendered the task title without
any "this is a Task" affordance — to a first-time user it looked
indistinguishable from any other detail surface. Breadcrumbs are the
cheapest possible disambiguator and they double as navigation.

**How to apply.** The breadcrumb format is `[Project name] › Tasks ›
[task title]` (see #186-1). For pages without a natural parent — Fleet,
Environment, Settings — the breadcrumb degenerates to a single section
label, e.g. `Environment`. The breadcrumb uses the same display-name
resolution as § 2 (never raw IDs in the breadcrumb itself).

## 8 — No misleading affordances; show only what works

**Rule.** If a feature is not functional in the current release — backend
not wired, execution path is a stub, the user lacks permission — the
UI must not present it as something the user can do. Hide, disable
with a hover-reason, or replace with an explicit "coming in vN+1"
placeholder. **A button that does nothing, or that pretends to start
work and then silently fails, is a defect.**

**Why.** Two v2.7 acceptance items were exactly this class of bug:
(a) the agent-create modal listed `codex` and `opencode` as runtime
choices, but only `claude-code` actually executes (FINDING-F, #181)
— users could create agents that would never run; (b) the DM-with-agent
flow let you create a DM with an agent and send a message, but no wake
path existed, so the agent never responded (FINDING-H, #185). Both
were misleading affordances: the UI promised something the system did
not deliver.

**How to apply.** PR review checks every new control / form field / menu
item for "what happens if the user picks this, and does the backend
actually do that?" The two enforcement patterns established in v2.7:
**allowlist + 4xx at the boundary** (the agent-create `cli` allowlist
rejects non-`claude-code` values server-side, the dropdown shows only
the executable option, #181) and **a visible system message instead of
a silent drop** (a stopped agent receiving a DM gets an in-conversation
"agent is offline" system message, not silence, #185). When in doubt,
err toward "tell the user something happened, even if the answer is
'this isn't supported yet'."

## 9 — No silent failures; surface state changes

**Rule.** Every user action that produces a state change in the system
must produce a visible signal back to the user — a status pill update,
a toast, a new message in the conversation, an animated row, an explicit
error. A "nothing visible happened" outcome for any user-initiated
action is a defect.

**Why.** The §-1 family of bugs across the v2.7 acceptance — the SSE
"connecting" 30s stall (#172), the stopped-agent DM black hole (FINDING-H),
the L2-on-Mode-B WorkItem-id loss — all share the same shape: the
system was doing something, but the user could not tell. The fix is
always the same: route a visible signal back to the surface the user
is looking at.

**How to apply.** Lifecycle endpoints surface state via the existing
SSE projection. Tool-use surfaces (e.g., agent CLI auto-discovery,
#147 + #176) render the result in the page that motivated the action
(the Environment worker card now shows the detected CLIs, not just a
silent backend reconciliation). New features include a "what does the
user see when this happens?" review point in their design.

## 10 — Form validation: clean 4xx, not 5xx

**Rule.** Any user input that the backend will reject for a format /
shape / domain-rule reason returns a `400` with a machine-readable error
code and a human-readable message. `500 Internal Server Error` for a
bad-input case is a defect.

**Why.** A `500` reads to the operator as "the server is broken"; the
correct read is "your input was wrong, here's why." The v2.7 invite-with-
malformed-identity case was returning `500` until #158 — the operator
thought we'd shipped a broken server when in fact they had pasted a
typo.

**How to apply.** Server handlers validate at the boundary and translate
domain-validation errors to `400 invalid_<field>` (see #158, #181, #148).
A `500` should only appear for genuinely unexpected internal failures
(panic, persistence layer outage, etc.). The frontend surfaces the
returned error message verbatim, scoped to the field that produced it.

## 11 — Lint enforces the rules that can be linted

**Rule.** UX rules that have a mechanically-checkable shape go into
`make lint`. A rule that is only documented in this file but not
enforced will regress — that's the lesson from `--setting-sources ""`
(FINDING-G, #182): the comment "Tester verified this is OK" did not
prevent a real bug.

**Why.** Mechanism beats memory. The rule "do not call `window.confirm`"
became `no-restricted-globals` + `no-restricted-properties` in ESLint
(#169); the rule "no Chinese strings in the UI" is a candidate next.
A rule in lint cannot be forgotten by a hurried PR.

**How to apply.** New documented rules in this file ask, in the PR
introducing them, "can this be linted?" If yes, the lint rule lands in
the same PR. The lint rule + the prose rule cite each other, so the
test failure points the reader at the rationale.

## 12 — Icon-only controls carry a tooltip and an aria-label

**Rule.** A control rendered as an icon (no visible text label) uses an
inline single-stroke SVG glyph — never an emoji — and always carries
both a `title` tooltip and an `aria-label`. A destructive icon keeps a
`text-danger` colour. An icon whose meaning depends on state flips its
glyph **and** its `title` / `aria-label` together with the state.

**Why.** An icon with no accessible name is invisible to screen readers
and ambiguous to sighted users; an emoji renders inconsistently across
platforms and fonts. v2.7.1 icon-ised the AgentDetail header (`#240`
Message, `#250` Stop / Restart / Reset) and the sidebar collapse toggle
(`#253`); each has to say what it does without a visible label.

**How to apply.** The control is `<button aria-label="Stop agent"
title="Stop">` wrapping an inline `<svg>` (one `path`, no `<rect>`
chrome). Destructive actions (Reset) carry the `text-danger` token, so
the *computed* colour is the danger red (`rgb(239,68,68)`); acceptance
verifies the computed colour, not a guessed class string (see
`docs/rules/acceptance-methodology.md`). A state-toggle control (sidebar
collapse) swaps both the glyph (`‹` ⇄ `›`) and the
`title` / `aria-label` ("Collapse sidebar" ⇄ "Expand sidebar") on
toggle. When an icon sits in a control group, the primary call-to-action
keeps its text label (the AgentDetail `Start` button stayed text while
its siblings became icons).

---

## Process notes

- **Where this lives.** This file is the canonical reference for frontend
  UX patterns. Other docs may extend it (e.g., a per-domain styleguide),
  but no other doc may contradict it without updating this file first.
- **PR review.** PD (`@AgentCenterPD`) reviews user-facing changes against
  these rules. Tester (`@AgentCenterTester`) treats a rule violation as a
  finding with the same weight as a functional defect. Dev /
  IntegrationDev do not adjudicate rule conflicts in code review — those
  go to PD.
- **Adding a rule.** New rules are PD-authored, generally in response to
  a specific shipped (or nearly-shipped) regression. Each rule names the
  acceptance case that produced it so the rationale stays attached.
