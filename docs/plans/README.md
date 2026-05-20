# 实现计划

> 工作计划集 · 按 DDD 依赖方向分 7 个 phase · **严格按顺序执行，不允许跳级或并行多 phase**

## § 0. 顺序原则（DDD 视角）

1. **被依赖者先**：Shared Kernel → Core → Open Host / ACL → Customer
2. **横切组件早建**：events 总线 + EventSink 在 Phase 1 就埋；后续所有 BC emit 走它
3. **BC 内部按战术层级**：Aggregate Root → Entity / VO → Repository → Domain Service → Application Service（CLI handler）
4. **ACL（Bridge）围绕 Core**：领域 BC 零 vendor 依赖
5. **Customer（Cognition Supervisor）最后**：等所有被调 BC 就绪

详细 DDD 顺序推导见 [docs/design/ddd-blueprint § 3](../design/ddd-blueprint.md)。

---

## § 1. Phase 索引

| Phase | 主题 | DDD 角色 | 上游依赖 | 状态 |
|---|---|---|---|---|
| **1** | [Shared Kernel + Events 总线](phase-1-shared-kernel-events.md) | Shared Kernel (Workforce / Conversation) + Open Host (Observability events 表) | — | Draft |
| **2** | [TaskRuntime Core](phase-2-task-runtime.md) | Core BC | Phase 1 | Draft |
| **3** | [Discussion Core](phase-3-discussion.md) | Core BC | Phase 1 + 2 | Draft |
| **4** | [Observability 投影 + 查询面](phase-4-observability.md) | Open Host 完整化 | Phase 1-3 | Draft |
| **5** | [Bridge ACL Outbound（飞书）](phase-5-bridge-feishu-outbound.md) | ACL (Anti-Corruption Layer) | Phase 1-4 | Draft |
| **6** | [Cognition Supervisor](phase-6-cognition-supervisor.md) | Customer | Phase 1-5 | Draft |
| **7** | [Bridge Inbound + 部署收尾](phase-7-bridge-inbound-deploy.md) | ACL inbound + 运维 | Phase 1-6 | Draft |

---

## § 2. 执行纪律（每个 phase 都必须遵守）

### 2.1 顺序

- **严格按 phase 顺序执行**：Phase N 的 DoD 100% 达成才能进 Phase N+1
- 不允许跳级
- 不允许多 phase 并行（同一时刻只能一个 phase in_progress）
- Phase 完成后**冻结**该 phase 工件接口；后续 phase 只能扩列，不能改语义

### 2.2 完备性（模块不允许半成品）

每个 phase 完成定义（DoD）必须 **100%** 达成才算交付。半成品红线：

- Repository 接口存在但没全部实现 → ❌
- Domain Service 写完但没集成到 CLI / 事件链 → ❌
- CLI 命令注册但 handler 是 stub / 报 unimplemented → ❌
- Aggregate 状态机有路径没实现 → ❌
- 端到端能跑通 happy path 但异常路径没覆盖 → ❌
- "MVP 先这样，后续再补" → ❌（**禁止 MVP-then-补 模式**）

完整 DoD 清单在每个 phase 文档 § 4。

### 2.3 测试

每个 phase 必须交付三层测试 + 一份测试报告：

| 层级 | 范围 | 工具 |
|---|---|---|
| **单测（Unit）** | Aggregate / Domain Service / VO / Repository 实现 / Application Service / CLI handler。外部依赖 mock（SQLite 可用 `:memory:`；Bridge / agent CLI 走 mock interface） | Go `testing` + table-driven + `testify` |
| **集成测试（Integration）** | Repository ↔ 真实 SQLite + migration / 跨聚合 tx / domain event 同事务双写 / DispatchService 全路径 | Go `testing` + `testcontainers` (如有) / 真实 SQLite file |
| **e2e** | 从 CLI 入口到 SQL / BlobStore / 事件流，完整用户场景；外部 vendor（飞书）走 fake server / record-replay | Go `testing` + 自建 e2e harness（`testdata/e2e/`） |

**指标**：

- **单测行覆盖率 ≥ 90%**（diff 90% + 整体 90%；以 `go test -cover` 为准）
  - 外部系统可 mock（SQLite `:memory:` / 飞书 / agent CLI 通过 Adapter mock）
- 测试报告归档：`docs/plans/reports/phase-N-test-report.md`，按 [§ 4 报告模板](#-4-测试报告模板) 填写
- 测试与实现 **1:1 对位**（每个 service / repository / aggregate method 都有对应测试用例）

**测试纪律**（[conventions § 14 / § 14.x](../rules/conventions.md) + [testing.md](../rules/testing.md)）：

- 不允许 sleep / 真连外部服务（除明示标记 `//+build integration` 的集成段）
- 测试时间穿越用 `clock.Clock` interface 注入
- DB 测试用真实 SQLite `:memory:` 或临时文件（不用 mock DB）
- 异常路径必须有用例（CAS 冲突 / not found / state machine 非法跃迁 / tx 回滚 / etc）

### 2.4 错误处理

[conventions § 17 错误显式化（不允许吞）](../rules/conventions.md)：每个 `if err != nil` 分支 emit event / return / panic 三选一；log 不算处理；未知协议 / 字段当 noop 不上报禁止。

### 2.5 可观测性

[conventions § 2 可观测性优先](../rules/conventions.md)：每个 phase 新增工件必须列出：

- 它产生哪些 domain event（events 表）
- inspect / query / ps / stats 是否要扩列
- 失败 / 异常路径如何被 emit 出来

---

## § 3. Phase 文档模板

每个 `phase-N-<slug>.md` 严格按以下结构组织：

```
# Phase N: <Name>

> DDD <BC / Stage> · 依赖 Phase <upstream> · 解锁 Phase <downstream>
> 纪律：按里程碑顺序 / 模块完备不半成品 / 单测 ≥ 90% + 集成 + e2e + 测试报告

## § 0. 目标
1-2 段：本 phase 交付什么能力（用户视角 / supervisor 视角 / 运维视角）+ DDD 意义

## § 1. DDD 工件清单
（按战术层级分小节）
### 1.1 Aggregate Roots
### 1.2 Entities（子从属）
### 1.3 Value Objects
### 1.4 Repositories
### 1.5 Domain Services
### 1.6 Application Services（CLI handler 层）
### 1.7 Domain Events（emit 给 events 表）
### 1.8 Context Map 关系（被依赖 / 依赖谁 / 哪种关系）

## § 2. 上游依赖（来自 Phase < ...> 的工件）
表：上游工件 → 本 phase 哪一步会用

## § 3. 工作项分解（严格按依赖顺序）
（小节按依赖前后排列；每节一个原子工件 / 一组紧耦合工件）
### 3.1 <第一个工件>
- 工件名 / 类型（Aggregate / Repository / Service / CLI）
- 输入（依赖谁）
- 输出（产出什么 + 文件路径 + Go 包路径）
- 实现步骤（高 level，不到代码细节）
- 与 P8b 02-persistence-schema § X 的对位
- DoD（完成标准；必须可机器验证 / 测试通过）

### 3.2 ...
（按依赖顺序排）

## § 4. Definition of Done（整体）
- [ ] § 1 所有工件实现并通过单元测试
- [ ] § 5 所有测试场景通过（unit + 集成 + e2e）
- [ ] 单测行覆盖率 ≥ 90%（diff + 整体）
- [ ] 测试报告归档到 docs/plans/reports/phase-N-test-report.md
- [ ] 触发的 domain event 实际进 events 表（集成测试验证）
- [ ] CLI 命令 `--help` 跟 03-cli-subcommands § 8.X 对齐
- [ ] 项目本地 lint + go vet + go test ./... 全过
- [ ] § 7 风险项要么处理要么显式 defer 到具体后续 phase（不能"待定"）

## § 5. 测试计划
### 5.1 单测场景（按工件分类）
| 工件 | 测试场景 | 关键断言 |
| ... | ... | ... |

### 5.2 集成测试场景
| 场景 | 涉及工件 | 关键断言 |
| ... | ... | ... |

### 5.3 e2e 测试场景
| 场景 | 用户视角 / 入口 CLI | 关键断言 |
| ... | ... | ... |

## § 6. 风险 / Spike 项
- 风险描述
- 缓解方案 或 标 Spike 项（要在 phase 内消化）

## § 7. 下游解锁
本 phase 完成后，哪些 phase 可以开始；提供给下游的接口 surface
```

---

## § 4. 测试报告模板（每 phase 完成后填）

文件位置：`docs/plans/reports/phase-N-test-report.md`。结构：

```markdown
# Phase N 测试报告

> 完成日期：YYYY-MM-DD · 提交 SHA：<sha>

## § 1. 覆盖率汇总
| 维度 | 数值 | 是否达标（≥ 90%） |
| 整体行覆盖率 | XX% | ✅/❌ |
| 本 phase diff 行覆盖率 | XX% | ✅/❌ |
| 分支覆盖率（参考） | XX% | - |

## § 2. 测试场景执行结果
### 2.1 单测
| 场景 | 用例数 | pass / fail | 备注 |
| ... | ... | ... | ... |

### 2.2 集成测试
（同结构）

### 2.3 e2e 测试
（同结构）

## § 3. 跟测试计划（phase-N § 5）的对位
| § 5 行号 | 场景描述 | 实际用例文件:函数 | 状态 |
| ... | ... | ... | ✅/❌ |

> 每条 § 5 测试计划行**必须**在本表有对应用例（1:1）；遗漏 = phase 未完成。

## § 4. 失败 / 已知问题
- （列已知 flaky / 未覆盖的 corner case；必须有处置：要么修，要么标 spike defer 到下个 phase）

## § 5. DoD 自检
| § 4 DoD 行 | 状态 |
| 所有工件实现 | ✅ |
| ... | ... |

## § 6. 提交清单
- 实现代码：`internal/<package>/...`
- 测试代码：`internal/<package>/*_test.go` / `tests/integration/...` / `tests/e2e/...`
- migration：`internal/persistence/migrations/000X_*.up.sql` / `*.down.sql`
- skill 文档变更（若有）：`assets/skills/...`
- 文档变更（若有）：`docs/...`
```

---

## § 5. Git workflow

- 每个 phase 完成在一个或多个 commit，最后一个 commit message 体现 phase 完成（"feat(phase-N): <主题> 完成"）
- 每个工件 / 子任务可独立 commit
- Phase 完成后**不允许**回头改该 phase 工件接口；只能在后续 phase 扩列

---

> **维护**：本 README 是契约。如果 phase 顺序 / 纪律 / 模板要改，需要先改本文档 + 同步影响的 phase 文档，再继续。
