-- 0095_v228_worker_system_info.down.sql — reverse the system_info_json addition.
ALTER TABLE workers DROP COLUMN system_info_json;
