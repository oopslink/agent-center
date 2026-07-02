-- 0092_v228_task_node_plan_graph.up.sql — wire orchestration engine to PM
ALTER TABLE pm_tasks ADD COLUMN node_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_plans ADD COLUMN graph_id TEXT NOT NULL DEFAULT '';
