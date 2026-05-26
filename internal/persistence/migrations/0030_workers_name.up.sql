-- v2.4-D-X1 (@oopslink ask): workers.name as a separate, editable
-- field. id stays immutable; name is the friendly label the operator
-- typed in the Add Worker Modal and can rename later from Fleet.
--
-- Default the column to '' rather than worker_id so we can detect
-- "not set yet" in handlers / projection layer; a backfill below
-- materialises the historical equivalent (name = id) so existing
-- rows display sensibly without a code path.
ALTER TABLE workers ADD COLUMN name TEXT NOT NULL DEFAULT '';
UPDATE workers SET name = id WHERE name = '';
