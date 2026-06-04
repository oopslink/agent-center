# v2.7 / v2.7.1 development retrospective

This document captures the cross-role retrospective conducted at the end of
the v2.7 + v2.7.1 release cycle. Each lane (PD, Backend, Frontend, Tester,
UI Tester, Integration) wrote its own self-retro; the rules below are the
consolidated normative output the team committed to going forward.

The retrospective itself (thread `#agent-center:c93d2059`) is the source
material; individual messages are not copied here verbatim — what follows
is the distilled set of rules the team agreed to keep.

---

## 1. Acceptance methodology

The lessons under this heading converge on a single principle: **acceptance
is a test of real usage, not parity reasoning over code**. Across the
cycle, almost every escaped finding (`#159` files 501, `#161` AirPlay
port, `#150` `launchctl` legacy API, `#155`/`#160` raw refs, `#199` worker
run flag set, `#202`/`#203`/`#204`, `#211` default-prefix
discovery → `#212`, `#240` `createDm` bare ref, `#241`
`find_org_agent → assign_task` chain) landed in the same crack — code or
unit tests said "fine" but the real install / real browser / real
end-to-end usage said otherwise.

**T-1 — Real-usage path is what counts.** Acceptance runs against the real
`install` / `upgrade` path (real launchd / systemd unit, real generated
`config.yaml`, no hand-rolled config), a real browser, the real target
environment (this cycle: macOS — AirPlay holds `:7000`, modern macOS uses
`launchctl bootout`/`bootstrap` after the legacy `load`/`unload` was
deprecated, multi-instance coexistence), and a real agent / worker
running the exact command a user would copy from the Web Console. UI
assertions inside a real browser anchor on rendered / computed truth
(`getComputedStyle` for color, `.value` property for controlled inputs,
rendered DOM for presence), not on class-name guesses; this avoided
false negatives like the `#250` Reset-button red check where the class
token was `danger` rather than `red`. The full how-to lives in the
acceptance-methodology document (see follow-up). By-parity reasoning, code reads, `--dry-run`, and shared
predicate inference do **not** count as acceptance evidence.

**T-2 — Path / discovery / layout features must run under the product's
default prefix.** `~/.agent-center[.<instance>]`, not `/tmp`. The
`/tmp`-isolation testing convention bypasses the path-convention bugs
themselves — `#212` (`list-local-centers` returned worker installs as
centers) was hiding behind `/tmp` for exactly this reason. Data /
content tests may still use `/tmp` for cleanup ease.

**T-3 — Test the chain, then sweep the class.** Multi-tool flows must
feed the output of one tool into the input of the next, end-to-end
(`#241` `find_org_agent → assign_task` was missed by per-tool unit
testing). When a single finding reveals a class of issue (e.g. v2.7.1's
"bare business-id fed into an ADR-0033 ref-validating endpoint" class
that surfaced as `#240` and `#241`), enumerate and test **every**
reachable consumer in that class before signing off.

**T-4 — Full-product integration runs before the owner sees it.** A real
`install` + real browser + full-product walkthrough must happen in our
own cycle prior to the owner ever needing to dogfood it. v2.7's
`E2`-phase first real integration was a sequence inversion (owner found
the gaps before we did), and that's what every Tester / Tester2 lesson
this cycle is fundamentally pointing at.

**T-5 — Evidence is code.** Acceptance evidence (report + screenshots)
must be committed into the same commit being tagged. Before signing
"evidence is commit-aligned", run `git ls-tree -r <tag> <path>` and
confirm the files are in the tagged tree. "It's on disk" / "I just
wrote it" / "untracked in worktree" do **not** count — v2.7.1's
final (re-tagged) tag (`bdc9818`) shipped without the acceptance
report or screenshots in its tree, and every role's ship post
(including PD's) echoed "commit-aligned" without verifying.
Acknowledged and corrected.

**T-6 — Release-gate verification commands.** Ship verification must
include `make lint` and a `make release` dry-run (frontend `tsc` truly
compiles; `vitest` does not type-check by itself). The frontend
type-check has to be its own step in CI; relying on `vitest` to catch
type errors will let regressions through.

**T-7 — Test-case independence.** Tester (criteria) and PD (intent) own
test design from the spec / product intent. Dev has **zero** design
participation in those test cases — Dev surfaces findings after the
fact and fixes them, but doesn't co-design pass/fail criteria for their
own implementation. (Already in place; restating it because it is what
keeps Tester's "unit-green / real-broken" instincts honest.)

**T-8 — Reports use the ubiquitous language, not implementation
shorthand.** Acceptance reports speak to the product owner in domain /
module / feature terms ("DM detail header now shows `@<peer>` instead
of 'Direct message'") rather than implementation shorthand
(`participants[*] != self`). Code coordinates and PR / commit
references belong in the developer-facing thread, not in the body of
the report.

**Deployed-smoke ≥ 1 is a hard release gate.** A release is not accepted
until at least one deployed-smoke run has executed the cross-version
upgrade path (e.g. real v2.7.0 → v2.7.1 install, with the migrations
actually running on real prior-version data, on the canonical target
environment). The cycle's first attempt at this happened in `E2`; from
v2.7.1 onward it is part of the standard acceptance program.

The above complements (and is consistent with) `docs/release/acceptance-checklist.md`.

---

## 2. Class-sweep audit

When a finding reveals a class of issue, the response is not to patch the
single reported instance — it is to enumerate the class and audit every
reachable consumer. v2.7.1's ref-prefix class
(`#240` → `#241` → `#244` v2.8 follow-up) is the canonical example.

The audit pattern that worked:

1. **Tester** posts the finding plus the conceptual class ("bare
   business-id fed into an ADR-0033 identity-ref endpoint").
2. **Dev** confirms the class boundary at the backend layer (which
   endpoints validate refs vs accept entity IDs raw, surfacing the
   identity-ref ↔ entity-ID distinction along the way).
3. **Dev2** does a preempt audit of the corresponding frontend code
   paths against the same class boundary, reports "class clean" or
   surfaces additional sites.
4. **Tester** spot-verifies the class boundary holds end-to-end (it's
   not enough that each site is reviewed; the chain has to be exercised).

This pattern was used for the ref-prefix class, the ULID-handle class
(`#126` taught us ULID heads are timestamps and collide within the
same window — always use the **tail** segment for visible distinguishing
handles), and the `find_*_by_name` MCP discovery class
(`#239` + `#246` + `assignee_ref`).

Keep doing this. The cost of an audit is far less than the cost of
shipping a half-swept class.

---

## 3. Decision making — no middle state

This is the cycle's most-cited principle and the owner's most explicit
directive. Almost every architectural decision in the cycle came back to
it:

- **Don't ship half-implementations**: ship X or ship not-X. "We'll
  finish it in the next version" is almost always worse than either
  doing it now or skipping it now. v2.7.1's "tag waits on
  `#207`+`#208`+`#209`" / `#227` auto-join / `#251` migrate-config-and-
  rewrite-unit all chose "do it now" over "leave half".
- **Single source of truth for any one fact**: the v2.7.1 worker config
  rework chose `<prefix>/etc/config.yaml` as the only source for
  `worker_id` / `bootstrap` / `token` / `server_fingerprint`; the launch
  command and the service unit no longer carry them. CLI flags remain
  as backward-compat overrides (a flag wins over config), but the
  canonical surface is the file.
- **Boundary discipline — implementation constraints must not leak
  across the model boundary**. v2.7.1 forcing `worker_id` at the API
  handler (because this release ships one specific binding) is fine;
  baking that constraint into the agent BC's `NewAgent` domain
  constructor would not be. This is the inverse — and the necessary
  partner — of the "no middle state" rule: clean **business surface**
  (no half-states the user sees), clean **model layer** (no
  implementation-layer constraints baked into the domain).

In practice this means: when the team disagrees on direction, pick one
and execute clean. When the owner asks for X, deliver X end-to-end (or
clearly refuse and say why); don't deliver "X for the part that's easy".

---

## 4. Authorization & permissions — β is not a default

When asked whether a new MCP tool, error message, or upgrade affordance
should also widen write permissions ("let an org-member agent
`create_task` in any project of the org"), the owner's standing answer is
**no**. New tools may discover scope (read), surface precise errors
(diagnostics), and improve UX (display); they may **not** become a
backdoor that lets a caller act outside its existing authorization.

In v2.7.1 this rule was repeatedly invoked under the label "β": the
question "should org-agent acquire default write across the org's
projects?" came up four separate times (`#224` / `#227` / `#239` /
`#246`) and was rejected every time. The MCP tools `get_my_profile`,
`find_org_agent`, `find_org_channel`, and the precise 404 / 403 error
messages are all read-side / diagnostic-side; the participant write
gates in `pm` and `conversation` were not touched.

**Tester negative-verification of write gates is mandatory** any time
new discovery / diagnostic surfaces are added. A non-member agent that
has just resolved a project / channel ID via a new MCP tool must still
return 403 on the write path. This is the rule that keeps a "we just
made it easier for agents to find IDs" patch from quietly becoming a
"we just gave agents write access" patch.

---

## 5. Identity references — prefixed identity ref vs raw entity ID

ADR-0033 introduced the `kind:id` identity-reference format
(`agent:agent-<8hex>` / `user:user-<8hex>`); entity IDs (project, task,
issue, channel, work item) are stable opaque hashes and remain bare.
v2.7.1's ref-prefix class showed up because frontend / MCP tools were
emitting bare IDs into the endpoints that validate ADR-0033 identity
refs.

The rule that fell out of the audit:

- **Identity (agent / user / member) → ref must be prefixed**
  (`agent:<id>` / `user:<id>`) at every consumer.
- **Entity (project / task / issue / channel / work-item) → ID is
  bare** at every consumer; consumers must not require a prefix and
  producers must not add one.
- **Tools / DTOs that produce an identity reference should produce the
  complete consumable form**, not the bare ID with the expectation that
  the caller will prepend `agent:`. v2.7.1's `find_org_agent` now returns
  `{id, name, assignee_ref: "agent:<id>"}` for exactly this reason — the
  agent feeds `assignee_ref` straight into `assign_task` with no
  concatenation.

This belongs in `docs/rules/conventions.md` alongside ADR-0033's textual
description.

---

## 6. UI rendering — chrome vs content vs id-as-content

`#192`'s "no raw IDs leak into the UI" rule has held since v2.7, but the
boundary needs to be stated precisely. v2.7.1 clarified three categories:

- **Chrome** — entity references rendered as part of the page's frame
  (sender labels in message rows, sidebar items, breadcrumb leaves,
  participant lists, "by `<creator>`", etc.). **Chrome is strict**: the
  user sees the display name; the raw ID appears only on hover (`title`
  attribute). `EntityRef` is the canonical component; deleted entities
  render as `(deleted)`.
- **Content** — material that the user authored or that an agent
  produced (message bodies, `tool_use` arguments, expandable JSON
  payload viewers, agent thinking text). Content is **exempt** from the
  raw-ID rule — if a user wrote `task-abc12` into a message, the
  message displays exactly that. (`agent-activity-payload-json` is the
  testid the Tester2 sweep skips by design.)
- **Id-as-content** — a small, deliberately exposed identifier surface
  that is the column or token *itself* (the table's `ID` column, a
  shortened ULID-tail handle like `#abc123`, the URL segment for
  `/projects/<project-id>/...`). These are chrome by design — the ID is
  the rendered value — and they intentionally show a short form with
  the full ID on hover.

Tester2's `#192` sweep encodes exactly this distinction: it walks
`inner_text`, treats the JSON-viewer subtree as a content-exempt zone,
and accepts the id-as-content table cells. Future verification work
should reuse the same testids (`agent-activity-payload-json`,
`message-workitem-tag`, etc.) and the same exemption rule.

When a third category genuinely shows up, name it, document it, and add
its testid to the sweep — do not silently broaden "chrome" or "content".

This belongs in `docs/rules/ux-standards.md` (next to the existing
Rule 2a DM-rendering note).

---

## 7. ID layering — business surface vs internal

A user-visible agent ID is a **member-id** (`agent-<8hex>`,
Phabricator-style hash). The internal entity ID is a **ULID** that
addresses the agent on the worker filesystem, in the DB primary-key
column, and in cross-BC references. The mapping lives in the database;
neither the UI, the REST API, the `@mention` token, nor the ref token
should ever expose the ULID. The owner's standing answer to "should the
operator-visible filesystem layout also use member-id?" was **accept the
current ULID** — operator-surface IDs are implementation detail in
exactly the way `git`'s object SHAs are implementation detail.

When in doubt: would an end-user (not an operator) see this? If yes,
member-id. If only an operator reading `~/.agent-center` would see it,
either ID is acceptable.

This belongs in `docs/rules/conventions.md`.

---

## 8. Schema changes — independent work unit

A schema change is its own work unit, never a sub-step of a feature
ticket. v2.7.1 enforced this twice: deferring `reasoning_level` /
`mode` / `provider` Profile fields (`#229`) and work-item type /
priority (`#231`) to v2.8 rather than dragging four migrations into
`#228`'s UI refactor; and splitting out the org-sequence migration
(`#245`) and the worker-config upgrade migration (`#251`) as their own
PRs.

A schema-change PR must, at minimum:

1. Add the migration up + down SQL files.
2. Bump the five `schema_version` assertion sites
   (`migrator_test` / `round_trip` / `handlers_migrate` constant +
   test / integration test). This is the `#214` lesson — the assertion
   sites are hand-maintained and **must** be kept in lockstep.
3. Update `collectKnownKeys` if a new `worker:` /
   `bootstrap_public_url` / etc. section is being added to the config
   schema. This is the `#211` lesson — the YAML known-keys allowlist
   is also hand-maintained, and a missing key causes
   `unknown YAML key` errors that look like config corruption.
4. Backfill at migration time if any new NOT NULL column is being
   added; include the watermark / max-id seeding so that
   post-migration allocations continue cleanly from where the
   backfill left off (`#245` `org_sequence.next_value = MAX + 1`).
5. Provide a real upgrade smoke (v_prev → v_new on real install with
   real data, not just round-trip on an empty DB) — schema correctness
   on empty fixtures is necessary but never sufficient.

Schema changes also need explicit Tester upgrade-path verification:
fresh install validates the schema; only a real cross-version upgrade
validates that the migration actually ran on real prior-version data.

This belongs in `docs/rules/conventions.md` and is reinforced in
`docs/release/acceptance-checklist.md` as a release gate.

---

## 9. Release engineering — explicit refs, explicit auth, evidence in tree

Three operational rules surfaced through the v2.7.1 retag (and from the
final ship-evidence gap):

**Tag operations use explicit `refs/tags/` refspecs.** Same-name
branch + tag (`v2.7.1` was both) makes
`git push origin v2.7.1` ambiguous and Git's "matches more than one
ref" error halts you mid-operation. The team's accepted form is

```bash
git push origin refs/tags/v2.7.1     # publish
git push origin :refs/tags/v2.7.1    # delete
git tag -d v2.7.1                    # local cleanup
```

**Destructive tag operations require explicit owner authorization.**
Force-deleting a published tag, force-moving a tag, force-pushing to
`main`, and `git reset --hard` against an origin branch all require the
owner to say "yes, delete / yes, force-push" in plain words. The first
v2.7.1 tag (`cacfcbd`) was deleted only after the owner confirmed "no
one is using v2.7.1 yet"; the same standard applies any future time.

**`PR §-1 approve + merge it` is a discrete signal posted in the PR's
own thread.** It is **not** transferable from contract approval in a
task thread, from a `tag waits on X+Y+Z` coordination message, or from
implicit context. IntegrationDev does not merge without that explicit
signal. PD posts that signal in every PR. ("`#88` / `#97` taught us
this twice; the discipline holds in v2.7.1's 30+ PRs.")

**Evidence is in the tagged tree, or it doesn't exist.** Before
announcing that an acceptance report or screenshot set is
"commit-aligned" with a tag, run `git ls-tree -r <tag> <path>` and
confirm the files are actually there. Working-directory copies,
just-written-files, and `git stash` entries are **not** evidence. The
v2.7.1 evidence-archive gap caught five separate roles echoing
"commit-aligned" when no one had actually verified; we don't repeat that.
IntegrationDev also verifies at PR-merge time that any "added
evidence" or "screenshots commit" PR description matches the PR's
actual change set — a PR claiming to add evidence must have the
evidence files in its diff.

These rules belong in `docs/rules/v2.7-delivery-process.md` (which
already exists for v2.7's release cycle and should be extended).

---

## 10. Cross-role collaboration patterns

These are protocol-level patterns the team built during the cycle and
explicitly want to keep:

- **Shared-instance double verification.** Tester starts a real install
  on the canonical ports (`7101` / `7051` / `7301`, avoiding `:7000`
  AirPlay) under a `/tmp/<round>` prefix or the product default;
  Tester2 piggy-backs on the same instance for UI / UX coverage. The
  rule "announce the instance + ports + expected duration in
  `#agent-center` (the main channel, not a thread — so Dev / Dev2
  also see it and can avoid port collisions if they need to bring up
  a sandbox) before starting; announce release when done" came out of
  v2.7.1's parallel rounds and worked through round-3 with no
  conflict. Dev and Dev2 confirmed zero-contention on the same machine
  (their unit suites don't bind those ports).
- **(a) / (b) / (c) option framing for owner decisions.** PD never
  forwards a raw owner question to Dev / Dev2; PD expands it into 2–4
  options with PD's recommended pick, a one-line tradeoff for each, and
  an effort estimate. The owner picks a letter; the team executes
  cleanly. v2.7.1 used this for the issue / task ID format
  (T-numbers vs hash), the URL unification, the chat-`#`-reference
  deferral, and most of the dogfood-driven decisions.
- **Reality-check before scoping.** Dev's pattern of grep-ing the
  current schema / API / handler before estimating a feature
  (`#228` modeled-vs-not split, `#251` config-only-vs-`+unit-rewrite`,
  `#239` "is the field actually plumbed end-to-end?") catches the
  scope-creep that otherwise happens *after* a PR is half-written. PD
  should accept "let me reality-check first" as a legitimate response
  to a routing message.
- **Preempt audit.** Dev2 auditing all four front-end identity-ref
  emission sites before Tester2 swept them — and reporting "class
  clean, no v2.7.1 frontend gap" — closed the ref-prefix class one
  round earlier than a finding-by-finding loop would have. Keep.
- **One-role-one-domain ownership for retag.** During the v2.7.1 retag
  cycle, each ship-tail PR (`#249` worker config, `#251` migration,
  `#252` README, `#253` collapse icon, `#250` icon-ize) had exactly one
  owner. IntegrationDev kept the merge order linear (last-merged is
  the README that names all prior commits). No file-overlap conflicts.
  This pattern requires the team to be small and trusting; it does not
  generalize to wide-team teams, but for a tight team it cuts
  coordination overhead substantially.

The cross-role pattern that **needs improvement** is ship-post
cross-verification: when multiple roles echo the same claim
(`bdc9818` is commit-aligned), at least one of them must be the
verifier. We almost shipped on a chain of unverified echoes. Going
forward, the IntegrationDev ship post owns the `git ls-tree` /
`git log` verification, and other roles' celebratory acks do not
substitute for it.

---

## 11. Product completeness — CRUD enumeration before ship

This rule was added late in the retrospective at the owner's request, but
it is upstream of almost every other rule in this document: **a feature
isn't shipped until the basic management operations around it are
present**. In v2.7 / v2.7.1 the team repeatedly created an entity (Agent,
DM, Channel, Project member) and then discovered, after the user
dogfooded it, that the basic CRUD around that entity was incomplete:

- Agent create shipped without a delete affordance; `#197` patched it
  later.
- DM create allowed duplicate DMs against the same peer; `#215` patched
  it later (1:1 + dedup).
- Project members were read-only in the UI; `#207` added add / remove
  during v2.7's final lap.
- `find_org_channel` was missing from the MCP toolset because `#239`
  only added the agent / project lookup tools; `#246` patched it later
  after the owner hit the gap in chat.

In each case the gap was easy to enumerate at design time and easy to
miss at design time, because the team's attention was on the new
capability rather than on the basic management surface around it.

**The rule**: for every user-facing entity in the system
(Agent, Worker, Member, Channel, DM, Project, Issue, Task, ...), before
the release that introduces or modifies that entity ships, complete a
**CRUD-and-lifecycle audit**:

- [ ] **Create** — UI and API affordance present.
- [ ] **Read** — list view + detail view + (where applicable) cross-org
      / cross-project search.
- [ ] **Update** — rename, edit metadata, move (or explicitly deferred,
      with a release-note pointer at which version completes it).
- [ ] **Delete** — including cascade behavior (what happens to the
      entity's references in conversations, tasks, etc.) — or explicitly
      deferred with a documented reason.
- [ ] **Lifecycle / state transitions** — start / stop / archive /
      reset / suspend / etc., with the inverse transition available
      where it makes sense (entry symmetry: if you can enter a state,
      you must be able to leave it, or the lock has to be a deliberate
      product decision).
- [ ] **Entry-point symmetry** — if there's a way in, there's a way out.
      Members → Add Agent must have a corresponding "Remove" or
      "Archive". A Project Detail page that lets you add a member must
      let you remove one.
- [ ] **Cross-BC relations** — where the entity participates in another
      bounded context (assignee, participant, owner), enumerate the
      management surface in both contexts.

Anything that doesn't ship goes in the release notes under a
"deferred" section, with the target version named. Implicit gaps are
unacceptable.

### Tester verification dimension — T-9

The Tester lane adds the corresponding acceptance dimension: **acceptance
must verify what should be there, not only what is there**. The CRUD
audit list above is part of the acceptance program for every release
that introduces or modifies a managed entity. A missing basic operation
is itself a finding — to be reported in the acceptance report, not
silently passed through. (This is the dimension Tester noted was missing
from the v2.7.1 acceptance: every operation that *was* implemented was
verified end-to-end, but the question "is the basic management set
complete?" was not asked as a check.)

The PM owns the prioritization (this release does delete, that release
doesn't — it's a product call); the Tester owns surfacing the absence
during acceptance so that the choice is conscious rather than implicit.

## Closing note

The most consistent meta-lesson across all six self-retros was that
the cycle's escaped findings clustered into a small number of
recurring failure modes (unit-green / real-broken; reasoning over
parity; implicit signals; one-off patches for class-level issues). The
rules above are deliberately phrased so future cycles can grep them
when those failure modes start to repeat — not as virtues to admire,
but as forcing functions to invoke at the moment of decision.

`docs/rules/` is the canonical home; the threads in `#agent-center`
are the working memory.
