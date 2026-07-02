-- 0092_v228_task_node_plan_graph.down.sql — reverse the node_id / graph_id additions.
ALTER TABLE pm_tasks DROP COLUMN node_id;
ALTER TABLE pm_plans DROP COLUMN graph_id;
