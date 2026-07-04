-- 0098_remove_builtin_cycle_template.up.sql
--
-- 2026-07 复盘决定：没有内置模版类型。删掉此前 SeedBuiltinTemplates 种下的
-- `template-builtin-cycle`（System/只读）行——它的 9 步全真验收开发流程内容
-- 已搬进用户模版「全真验收开发模式」（UI 可编辑）。SeedBuiltinTemplates 现为 no-op，
-- 但已存在的这行不会被 seed 逻辑删除（seed 是 skip-if-exists），故用本迁移删除。
--
-- 幂等：WHERE id 命中 0 或 1 行；重复执行安全。

DELETE FROM pm_templates WHERE id = 'template-builtin-cycle';
