# Team Memory Rules Migration

Team Memory now has two first-class directories:

- `entries/`: knowledge and experience entries.
- `rules/`: operational rules with `enabled` and `applies_to`.

The directory is the only type source. Rule files must not add `kind: rule` or
`type: rule` frontmatter. `MEMORY.md` is generated from both directories.

## Runtime Loading

`get_team_rules` resolves the calling agent's current team and reads enabled
rules for the requested phase: `plan`, `execute`, `review`, or `recovery`.
Executor forks snapshot the matching rules immediately before launch and persist
the snapshot in `input.json` with the team memory repo `commit`.

Refresh semantics are intentionally explicit: in-flight executors and tier-1 /
tier-2 recovery keep their persisted input and runner command. A new executor
fork, or a tier-3 reset that causes a fresh fork, reloads rules from the current
team repo HEAD.

## Legacy Workflow Template Migration

Use `internal/team/migration` to plan and apply the migration from old
org-scoped workflow templates:

- If `created_by` is an `agent:<id>` that maps to exactly one team in the same
  org, the template becomes an enabled `rules/` file with `applies_to: [plan]`.
- If ownership is missing, non-agent, builtin, cross-org, or ambiguous, the
  planner returns an unclaimed record with a reason.
- The planner never broadcasts an unclaimed template to all teams.
- The applier writes only planned claims and does not delete `pm_templates`.

## Rollback

For each affected team repo, either revert the migration commit or delete the
listed `rules/<slug>-<uuid>.md` files, regenerate `MEMORY.md`, and push. Because
the helper leaves legacy `pm_templates` rows in place, database restore is not
required unless a caller performed a separate cleanup after verification.
