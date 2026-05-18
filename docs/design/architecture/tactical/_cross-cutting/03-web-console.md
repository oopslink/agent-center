# Web Console

> **跨切 / 非 BC**

本地 web UI，覆盖 agent-center 的查看与基础管理。

> **状态**: TBD 详细设计 —— 占位文档，等 §11 讨论展开。

## 定位

| 维度 | 决定 |
|---|---|
| 进程形态 | **嵌入 `agent-center server` 进程**（同二进制同进程，独立端口） |
| 网络绑定 | **仅 loopback**（如 `127.0.0.1:8080`），不监听公网 |
| 访问方式 | 用户通过 **SSH 隧道**从笔记本浏览器访问；隧道由用户自理（不在本项目范围） |
| 认证 | **无**（loopback 默认信任本机用户） |
| TLS | 不需要（loopback 本地）|

跟 [admin CLI 本机姿态](02-skill-cli-tooling.md) 一致，零认证负担、零公网暴露、零证书管理。

## v1 范围

| 类型 | 页面 | 能干啥 |
|---|---|---|
| **查看** | Task 列表 | 过滤 / 排序 / 翻页 / 进详情 |
| **查看** | Task 详情 | 时间线（事件 / tool call / 状态变化）/ 产物 / 子任务 / 关联 issue |
| **查看** | Issue 列表 | 过滤 / 翻页 / 进详情 |
| **查看** | Issue 详情 | thread / comment 时间线 / 关联 task |
| **查看** | Worker 列表 | 在线状态 / 当前并发数 / 历史统计 |
| **查看** | Fleet view (Agent ps) | 实时跨 worker 活跃 agent 列表 |
| **查看** | Supervisor invocation 列表 | 触发原因 / 时长 / 决策摘要 |
| **管理** | Task 取消 / 重派 | （按需限定权限）|
| **管理** | Issue 评论 / conclude / close | （等同 CLI 操作）|

不在 v1 范围（推迟到 [roadmap.md](../../../roadmap.md)）：

- 高级时间轴可视化（flamegraph / Gantt）—— v3
- 复杂 dashboard / 度量趋势图 —— 接 Prometheus / Grafana
- 远程访问（公网监听 + 认证）—— 跟 Remote CLI 一起 v2

## 技术形态（待定）

- 前端：TBD（vanilla HTML + htmx / vue / react，看复杂度）
- 后端：嵌在 server 进程，直接读 DB / BlobStore
- 不引入额外 binary，遵循 [conventions § 10 单一二进制 / 多模式](../../../../rules/conventions.md#-10-单一二进制--多模式)

## 配置

```yaml
# agent-center server config
web_console:
  enabled: true
  listen: 127.0.0.1:8080   # 默认 loopback
```

## 跟 CLI / Feishu 的关系

- CLI / Web Console / Feishu **三个独立的前端**，背后都对同一份 server / DB / BlobStore
- 三者**没有功能割据**：能在 CLI 做的事原则上能在 Web Console 做（也能从 Feishu 触发）
- 但**主流交互入口仍是 Feishu**；Web Console 主要给"宅在电脑前精细操作 / 调试"用

## 自检（按 conventions § 15）

设计本模块时已对照：

- [x] § 1 单一来源：Web Console 是查询 / 操作面板，不绕过 center 造任务
- [x] § 2 可观测性优先：本身就是观测面板，emit `WebConsoleAction` 事件审计每次管理操作
- [ ] § 3 AI Native：Web Console 不是给 agent 用的；agent 走 CLI
- [x] § 4 零 LLM SDK：不引入
- [x] § 8 BlobStore：日志 / trace 文件通过 BlobStore URL 显示
- [x] § 10 单一二进制：嵌入 server 进程
- [x] § 11 渠道：Web Console 是查询面板，事件 / Issue / InputRequest 仍走原通道
- [x] § 13 安全：loopback 绑定，无外部攻击面
