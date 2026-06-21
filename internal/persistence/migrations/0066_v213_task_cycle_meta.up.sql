-- 0066_v213_task_cycle_meta.up.sql — v2.13.0 I18/F2: cycle-node git metadata on
-- pm_tasks (see docs/design/v2.13.0/cycle-node-graph-spec.md §4). scaffold_cycle_plan
-- stamps these when it builds a cycle plan's nodes; F3's Integrate-complete guard
-- reads branch+base to verify `origin/<base> --contains <branch>`. ALTER so an
-- existing DB picks up the columns without a tasks-table rebuild; existing rows
-- backfill to '' / '' / 0 (= no cycle metadata, the ordinary-backlog default).
--   branch           — the feature branch a node works on (default the node's T<n>).
--   base             — the integration trunk (dev/vX.Y.0); 'main' for the S0 node.
--   skip_merge_check — structural exemption from the F3 merge guard (doc-only nodes).
ALTER TABLE pm_tasks ADD COLUMN branch TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_tasks ADD COLUMN base TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_tasks ADD COLUMN skip_merge_check BOOLEAN NOT NULL DEFAULT 0;
