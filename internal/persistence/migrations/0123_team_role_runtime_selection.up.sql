ALTER TABLE team_roles
  ADD COLUMN runtime_selection_json TEXT NOT NULL DEFAULT '{"mode":"inherit"}';

UPDATE team_roles
SET runtime_selection_json = '{"mode":"override","cli_id":"' || cli || '","model_id":"' || model || '"}'
WHERE cli <> '' OR model <> '';
