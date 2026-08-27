ALTER TABLE pm_gate_verdicts ADD COLUMN subject_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_gate_verdicts ADD COLUMN subject_locator TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_gate_verdicts ADD COLUMN subject_immutable_version TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_gate_verdicts ADD COLUMN subject_execution_generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pm_gate_verdicts ADD COLUMN subject_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_gate_verdicts ADD COLUMN contract_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_gate_verdicts ADD COLUMN authority_rank INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pm_gate_verdicts ADD COLUMN required_checks_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE pm_gate_verdicts ADD COLUMN reviewed_at TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_pm_gate_verdicts_subject_contract
    ON pm_gate_verdicts(subject_digest, contract_revision, authority_rank);
