# T1460 独立验收报告 — Access / Team Role / Subject access

## 结论

**REJECT（blocking）**。被测候选 `00adf6b7b3c6871e3dff118d4af2b85a2c23a1c6` 可构建、可部署，已有 Team Role / Subject access / RAM Role 主路径和安全回归大面积通过；但 RAM Role 的“完整版本化编辑”契约不成立：

1. `PATCH /api/access/ram-roles/{id}` 接受 `stable_key`，生产更新 SQL 不持久化它；创建 `original-key` 后 PATCH 为 `renamed-key`，响应仍为 `original-key`。
2. `authorization_role_versions` 只保存 permissions/risk。详情查询把当前 `authorization_roles.name/description/scope_kind` 投影到每个历史版本；v1 创建值在 v2 编辑后不可恢复。独立探针实际得到 v1/v2 均为 `Renamed / renamed description / team`，违反版本不可变与审计要求。

这两项均属于任务明确要求的 CRUD、stable key/scope 和 versioned edit，不能以其余回归为绿而豁免。

## 计划项执行结果

| # | 状态 | 证据 / 备注 |
|---|---|---|
| A1 | PASS | migration `0140_ram_role_stable_key_scope` 增加独立列及 system/org-custom 唯一索引；推翻“main 完全没有 schema/migration”的迟到 finding |
| A2 | **FAIL blocking** | 独立临时 Go HTTP probe 两次确定性失败：`stable_key="original-key" want renamed-key`；`versions[1]` 被污染为当前 name/description/scope。探针执行后已移除，日志结论保存在本报告 |
| A3 | PASS（现有覆盖） | `TestAccessRAMRolesPersistVersionsCASRevokeAndReferences`、`TestAccessRAMRoleV4EditDeleteAndReferenceBlocking` 覆盖 stale 409、Team Role 引用阻断；生产代码也检查 active direct bindings |
| A4 | PARTIAL | fresh deployed pipeline 中 RAM Role create/list/detail、Team Role 引用阻断/迁移/revoke 通过；现有浏览器 spec 覆盖 create/edit permissions/delete，但未发现 stable-key 编辑与历史 display-field 断言，无法抵消 A2 |
| T1 | PASS | Team service/API/UI 全套现有测试通过，含 Role 配置、RAM mapping 与 CAS |
| T2 | PASS（现有覆盖） | Team Role 删除保护/改派及成员状态由 TeamDetail/API tests 覆盖；全套 SPA/Go 回归通过 |
| T3 | PARTIAL | HTTP/UI 生产链和 admin 相关回归通过；本轮未建立“每个 CRUD 动作均由 MCP 独立驱动”的额外 deployed case，因 A2 已触发 blocking reject |
| S1 | PASS | deployed pipeline 的 direct binding preview/confirm/revoke/重放通过；Access SPA 来源链测试通过 |
| S2 | PASS（现有覆盖） | member 写 403、cross-org RAM mapping 拒绝及 authorization fail-closed tests 通过 |
| S3 | PASS（现有覆盖） | confirm/revoke replay 幂等及 CAS/preview-token 路径在 Go/deployed specs 通过 |
| R1 | PASS | targeted Go packages 通过；SPA 全套 191 files / 1790 tests 通过 |
| R2 | PASS | `make lint` 通过；`make smoke` 内 fresh `make build` 通过（tsc + vite + Go binaries） |
| R3 | PASS | `make smoke`：Playwright `v22-deployed-pipeline.spec.ts` 1/1；runtime-version Go smoke 通过；总耗时 187s |
| R4 | PASS with infra retry | `go test ./...` 唯一失败为并行资源耗尽：子 `go build` 报 `resource temporarily unavailable`；同一失败 case 单独重跑 6.883s 通过。其余 packages 全绿 |

## 独立失败探针步骤

1. fresh DB + owner session，通过真实 HTTP handler 创建 RAM Role：name=`Original name`、stable_key=`original-key`、description=`original description`、scope=`project`。
2. PATCH 同一 role：name=`Renamed`、stable_key=`renamed-key`、description=`renamed description`、scope=`team`、`expected_latest_version=1`。
3. 断言响应 stable_key：失败，实际仍为 `original-key`。
4. 放行该断言后检查 versions：失败，v1 与 v2 都返回当前 display fields，v1 原值丢失。

根因定位：`accessCreateRAMRoleVersion` 的 UPDATE 只写 `name, description, scope_kind, updated_at, version`，没有 `stable_key`；`authorization_role_versions` 与 `insertAccessRAMRoleVersion` 未保存 name/description/scope/stable_key，历史查询又 JOIN 当前 role 行。

## 测试分层

| 层 | 计数 / 入口 | 结果 |
|---|---|---|
| Unit / package | 全量 `go test ./...`；SPA 191 files / 1790 tests | 功能测试绿；1 个资源耗尽 infra failure 单测重跑绿 |
| Integration with mocks / temp DB | authorization/team/webconsole/persistence targeted packages；独立 HTTP probe | 基线绿；独立 probe 2 个 blocking 断言失败 |
| Deployed-binary smoke | Playwright 1 case + runtime-version Go cases，`make smoke` | PASS，187s |

## Remediation 验收要求

- 明确 stable_key 是否允许编辑；既然 API/UI 暴露 edit 字段，本候选必须持久化并做 org-scoped 唯一冲突映射；若产品要求不可变，则必须从编辑契约/UI 删除并以规范明确。
- 版本 projection 必须让每一版的 name、description、scope、stable_key、permissions、risk 均可恢复，不能读取当前行伪装历史。
- 把上述两个独立探针转为永久回归；补 stable-key 冲突、stale CAS 不部分写、历史 v1/v2 字段断言。
- 修复后从新的 fresh `origin/main` 重跑本报告 A1–R4；不得只重跑新增单测。
