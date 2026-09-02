WITH RECURSIVE generation_lineage(plan_id, generation_id) AS (
  SELECT id, active_generation_id
  FROM pm_plans
  WHERE active_generation_id != ''
  UNION ALL
  SELECT generation_lineage.plan_id, pm_plan_generations.parent_generation_id
  FROM generation_lineage
  JOIN pm_plan_generations ON pm_plan_generations.id = generation_lineage.generation_id
  WHERE pm_plan_generations.parent_generation_id != ''
),
superseded_open_tasks AS (
  SELECT DISTINCT generation_lineage.plan_id AS plan_id, json_extract(node.value, '$.task_id') AS task_id
  FROM generation_lineage
  JOIN pm_plan_generations ON pm_plan_generations.id = generation_lineage.generation_id
  JOIN json_each(pm_plan_generations.diff_json, '$.node_decisions') AS node
  WHERE json_extract(node.value, '$.action') = 'supersede'
)
UPDATE pm_tasks
SET status = 'discarded',
    status_changed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    completed_at = NULL,
    blocked_reason = '',
    blocked_reason_type = '',
    blocked_comment = '',
    failed_reason = '',
    execution_lease_expires_at = NULL,
    version = version + 1
WHERE status = 'open'
  AND EXISTS (
    SELECT 1
    FROM superseded_open_tasks
    WHERE superseded_open_tasks.plan_id = pm_tasks.plan_id
      AND superseded_open_tasks.task_id = pm_tasks.id
  );
