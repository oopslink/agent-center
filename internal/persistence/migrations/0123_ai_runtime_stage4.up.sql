ALTER TABLE team_roles
    ADD COLUMN runtime_selection_json TEXT NOT NULL DEFAULT '{"mode":"inherit"}';
