-- 0098_remove_builtin_cycle_template.down.sql
--
-- 不可逆回滚：内置 cycle 模版的内容已从代码移除（cycle_template.md 删除、
-- SeedBuiltinTemplates 置空），无法在此还原其 content。若确需恢复，从
-- git 历史取回 cycle_template.md 内容并重新 INSERT。故本 down 为 no-op。
SELECT 1;
