package sqlite

import (
	"context"
	"database/sql"
)

// SeedBuiltinTemplates is retained for boot wiring but seeds nothing.
//
// 2026-07 复盘决定：**没有内置模版类型**——所有模版都是用户自管 / UI 可编辑的。
// 原来的 "cycle" 内置模版已移除，其 9 步全真验收开发流程内容搬进用户模版
// 「全真验收开发模式」（UI 可编辑）。migration 0098 删掉此前已 seed 的
// `template-builtin-cycle` 行。保留本函数（app.go 仍调用）以免改 boot 接线，现为 no-op。
func SeedBuiltinTemplates(ctx context.Context, db *sql.DB, repo *TemplateRepo) error {
	return nil
}
