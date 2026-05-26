-- Remove the v2.4-D-X1 name column. SQLite >= 3.35 supports
-- ALTER TABLE DROP COLUMN directly (Mac ships 3.39+, our deployment
-- minimum is well above 3.35).
ALTER TABLE workers DROP COLUMN name;
