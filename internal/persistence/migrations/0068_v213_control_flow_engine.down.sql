-- 0068_v213_control_flow_engine.down.sql — revert v2.13.0 I18/B1 control-flow engine.
DROP TABLE IF EXISTS pm_plan_loop_rounds;
DROP TABLE IF EXISTS pm_plan_decision_outcomes;
ALTER TABLE pm_task_dependencies DROP COLUMN max_rounds;
ALTER TABLE pm_task_dependencies DROP COLUMN "when";
ALTER TABLE pm_task_dependencies DROP COLUMN kind;
