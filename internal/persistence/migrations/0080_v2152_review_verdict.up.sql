-- 0080_v2152_review_verdict.up.sql — v2.13.0 I18/B3 (T468 / issue-f7ad5a54):
-- structured Review verdict feeding B3 auto-decision. A Review node records ONE
-- single-slot, round-tagged verdict (latest-wins per plan_id,task_id — each review
-- round overwrites); B3 reads the CURRENT-round verdict to auto-pass / auto-reject,
-- replacing the brittle "open review comment count" proxy. ADDITIVE: the table starts
-- empty, so existing plans behave exactly as before (B3 falls back to the legacy
-- comment-count rule when no verdict is recorded).
CREATE TABLE pm_plan_review_verdicts (
    plan_id     TEXT NOT NULL,
    task_id     TEXT NOT NULL,
    verdict     TEXT NOT NULL,            -- 'pass' | 'reject'
    blocking    INTEGER NOT NULL DEFAULT 0,
    reason      TEXT NOT NULL DEFAULT '',
    sha         TEXT NOT NULL DEFAULT '',
    round       INTEGER NOT NULL DEFAULT 0, -- the decision's loop round this verdict is for
    recorded_at TEXT NOT NULL,
    PRIMARY KEY (plan_id, task_id)         -- single-slot, latest-wins (overwrite each round)
);
CREATE INDEX idx_pm_plan_review_verdicts_plan ON pm_plan_review_verdicts (plan_id);
