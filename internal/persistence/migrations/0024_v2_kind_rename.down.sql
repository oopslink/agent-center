-- 0024_v2_kind_rename.down.sql — P10 § 3.0
UPDATE conversations SET kind = 'group_thread' WHERE kind = 'channel';
