# v2.43.0 — Team 一等实体 Phase 1 验收

**结论**：部署级真跑验收 PASS（tester3 独立真跑 corroborated，R3 / task-e090b637）。标的 `feat/team-phase1-wiring@b511ffe0`，隔离全真实例 `t1023team`（make build 绿）。

## 四主线 + 修复端到端全绿
- **① team + 成员**：agent 独占（第二个 team → 409）/ human 多 team / associate_project / remove 释放索引。
- **② team-memory git rw**（核心 ref-vs-id 阻断，已修）：成员真 `git clone`+`git push` 自己 team repo → 200；非成员 → 403；全局 repo 读 200/写 403。修法：git-http 授权把运行时 ULID 经 `IdentityMemberID` 桥到 identity-member 命名空间再查 membership（跨命名空间回归锁 `git_team_membership_regression_test.go`）。
- **③(a) extract 健壮**：无-frontmatter/游离条目 → `extract_from_team` 200 + `skipped_nonstandard` 标注（曾 500）。
- **③(b) export/import + curation 强制**：未 curate → `export_team_template` 409 template_not_curated；curate → export JSON → import round-trip 201。
- **③(c) instantiate 真建身份**：`instantiate_team` 复用 `AgentIdentityProvisionService` 建真 `Identity[kind=agent]` 行（DB 实证 identities 5 真行、member ref 非悬空）。runtime provisioning（派 worker + auth）是设计 §9 单列步、不在 Phase 1。
- **④ role→agent**：Review≠Dev / 单 dev unsatisfiable → 400。

## 交付历程（真跑红线价值）
S1/S2/S3 单测绿但部署面零接线（死代码）→ 首轮真跑验收 REJECT 逮到 → 补 Wiring 切片 → 三轮真跑验收（R1 ref-vs-id 阻断 / R2 三残留 / R3 全绿）逐层挡住 unit-green 缺陷。全程 executor 自报 succeeded 均被独立真跑证伪，最终以真实行为为准。

完整原始证据见 task-e090b637（ACCEPTANCE-T1023.md / RAW）。
