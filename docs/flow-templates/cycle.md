# 开发周期编排模板（Cycle Flow Template）— 指针

> ⚠️ **本模板的唯一真源是 `internal/projectmanager/sqlite/cycle_template.md`**
> （`//go:embed` 进 binary → seed 进 DB → agent 通过 `get_template("cycle")` 拿到的就是它）。
>
> 历史上本文与运行时模版是**两份、会漂移**——2026-07 复盘逮到：曾改了本文、运行时那份没改 →
> agent 建 plan 拿的还是旧的（`验收=make test`）。为消除这个 double-truth：
>
> - **编辑 cycle 模版请只改运行时那份** `internal/projectmanager/sqlite/cycle_template.md`，本文仅作指针。
> - 看模版内容：`get_template("cycle")`，或直接读上面那个文件。
>
> 全真验收怎么做（起隔离实例、逐项、截图证据）见 `docs/rules/acceptance-methodology.md` + `docs/rules/acceptance-checklist.md`。
