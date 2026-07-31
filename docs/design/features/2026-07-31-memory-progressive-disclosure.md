# Memory Progressive Disclosure

## Status

Draft implementation plan, 2026-07-31.

## Problem

Supervisor memory currently behaves like an unbounded prompt append. The runtime
loads broad memory context into every fresh supervisor session, so old operational
notes, obsolete fallback advice, and unrelated work history compete with the
current task. The failure mode is not only token cost: a large startup prompt can
push critical access rules and current work state into a weaker position, and it
keeps reopening MCP tool-index initialization during fresh session rebuilds.

The existing file-based git memory decision remains correct. The problem is the
retrieval policy: the runtime needs to disclose memory progressively instead of
injecting everything that might be useful.

## Goals

- Keep memory file-based, git-backed, and human-readable.
- Make `MEMORY.md` an index and recovery map, not a full transcript dump.
- Bound the memory bytes/tokens injected into a fresh supervisor session.
- Prefer current task, current conversation, active project, and durable rules.
- Preserve a path for the agent to load more memory when needed.
- Make stale or dangerous memory less likely to override current access policy.

## Non-Goals

- No SQL `agent_memory` table.
- No background LLM summarizer in this change.
- No deletion of existing historical memory by default.
- No replacement of project repository instructions such as `AGENTS.md`.

## Proposed Contract

### 1. Memory Index First

Each agent memory root must have a `MEMORY.md` index. It is the only file that is
eligible for unconditional startup disclosure. The index should contain:

- role and stable identity notes;
- links to detailed memory files;
- active context for the current task/thread;
- short-lived warnings that must survive compaction;
- a small "read next" section with explicit file paths.

Detailed work history moves to referenced files such as `notes/work-log.md`,
`notes/channels.md`, or project-specific notes. The runtime may include a small
prefix of the index, but it should not recursively inline linked files.

### 2. Scoped Candidate Selection

For each invocation, the runtime builds a candidate list in this order:

1. access policy and hard runtime rules, outside memory budget;
2. `MEMORY.md` index;
3. current conversation or thread memory;
4. current task or issue memory;
5. current project memory;
6. supervisor self-memory;
7. global memory.

The runtime includes candidates only while the configured budget remains.
Candidates that do not fit are listed as available paths with one-line reasons,
so the agent can read them later with normal filesystem tools.

### 3. Budgeted Startup Disclosure

Default startup disclosure target:

- hard policy: not budgeted and always first;
- memory excerpts: 24 KiB total;
- per-file excerpt: 8 KiB;
- omitted-file manifest: 4 KiB.

These limits are byte-oriented for deterministic implementation. Token estimates
may be added later for better model-specific tuning.

### 4. Explicit Expansion

The injected prompt must tell the supervisor:

- the memory excerpt is intentionally incomplete;
- omitted memory paths are listed;
- read a listed file only when it is relevant to the current work;
- update `MEMORY.md` as an index, not as an append-only transcript.

This preserves progressive disclosure even after context compaction: the model
gets a concise map first, then pulls detail when justified.

### 5. Stale Memory Guard

Memory is advisory. The runtime access policy and current tool availability are
authoritative. If memory mentions SQLite/admin socket/admin HTTP/worker-token
fallback for agent-center state, the composer must drop that line before prompt
injection and prefer the hard policy.

## Implementation Plan

### Phase 1: Prompt Composition

- Add a memory disclosure composer in `internal/cognition/memory`.
- Keep `HarnessContext(ctx)` as the compatibility entry point, but make it call a
  budgeted variant with defaults.
- Return both included excerpts and omitted-path hints.
- Add tests for byte budget, per-file truncation, deterministic ordering, and
  stale-bypass line filtering.

### Phase 2: Runtime Wiring

- Use the budgeted memory context in Codex and Claude supervisor startup prompts.
- Keep the agent-center access policy outside the memory budget and before all
  memory text.
- Record disclosure statistics in agent activity or worker logs:
  included files, omitted files, and byte counts.

### Phase 3: Operational Cleanup

- Migrate large production `MEMORY.md` files into index plus `notes/` detail files.
- Keep the migration as ordinary git commits in each agent memory repo.
- Add an operator check that flags `MEMORY.md` files above the startup budget.

### Phase 4: Follow-Up Enhancements

- Add topic tags or front matter only if path-based selection is insufficient.
- Add an interactive memory search MCP/tool only after the file-index workflow is
  proven inadequate.
- Add background summarization only with explicit review hooks.

## Acceptance Criteria

- A fresh supervisor prompt includes hard policy plus a bounded memory section.
- Large historical memory files no longer inflate startup context by default.
- Omitted memory is discoverable by path from the initial prompt.
- Dangerous center-bypass memory lines do not appear in the injected prompt.
- Existing memory repositories continue to work without schema migration.

## Open Questions

- Whether the default memory budget should be per model family or global.
- Whether omitted-file hints should include git commit age once cheap metadata is
  available.
- Whether production migration should be manual for high-value agents or automatic
  with a conservative splitter.
