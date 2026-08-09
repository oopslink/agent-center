-- T1299: make remediation lineage explicit on the Stage ledger.
ALTER TABLE pm_stages ADD COLUMN supersedes_stage_id TEXT NOT NULL DEFAULT '';
