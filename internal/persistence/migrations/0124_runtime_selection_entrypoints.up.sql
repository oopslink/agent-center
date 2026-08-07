ALTER TABLE team_roles ADD COLUMN runtime_selection_json TEXT NOT NULL DEFAULT '';

UPDATE team_roles
SET runtime_selection_json = json_object(
    'mode', 'override',
    'cli_id', cli,
    'model_id', model,
    'parameters', json_object()
)
WHERE runtime_selection_json = ''
  AND cli <> ''
  AND model <> '';
