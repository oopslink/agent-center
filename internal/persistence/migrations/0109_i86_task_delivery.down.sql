-- 0109_i86_task_delivery.down.sql — reverse the I86 task delivery signal + fruitless-
-- reopen bound. Both columns are additive with safe defaults and nothing outside the
-- I86 consumers reads them, so dropping them reverts to the pre-I86 behavior. No data
-- restore is needed.
ALTER TABLE pm_tasks DROP COLUMN fruitless_reopens;
ALTER TABLE pm_tasks DROP COLUMN delivery;
