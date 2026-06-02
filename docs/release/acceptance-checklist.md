# v2.7 发布验收清单（完整产品功能）

> **方法论约束（铁律，每次执行必须遵守）**：
> 1. 全程使用 `agent-center install` **真实生成的 config** 运行，禁止手工配置文件绕测
> 2. 每条关键路径至少一次 **deployed-smoke**（真二进制端到端），单测绿不替代真实路径
> 3. 验收由 PD + Tester 独立执行，Dev 不参与设计和执行
> 4. 清单全部通过才发布，任何失败项开 task 修复后重测

---

## §1 安装与启动

- [ ] `agent-center install center` 全新安装成功（macOS，无 AirPlay 端口冲突，默认 server :7050 + web :7100）
- [ ] 安装生成的 config 包含 `blob_store`（文件上传不返回 501）
- [ ] `agent-center install center` 从旧版本升级成功（launchctl bootstrap/bootout 新 API）
- [ ] `agent-center version` 显示 v2.7.0
- [ ] `agent-center install worker` 安装 worker daemon 成功
- [ ] Worker daemon 升级正常（`agent-center upgrade worker`）
- [ ] `agent-center uninstall center` / `uninstall worker` 卸载正常
- [ ] 卸载后服务停止，数据目录清理

## §2 用户注册与登录

- [ ] 全新安装首屏：未初始化系统跳转 /signup（`GET /api/auth/bootstrap` → `{"initialized":false}`）
- [ ] 已有用户但未登录：跳转 /signin（`bootstrap` → `{"initialized":true}`）
- [ ] 注册新用户成功（passcode 流程，至少 6 位）
- [ ] 登录成功，`/api/auth/me` 返回正确 identity_id（`user:<id>` 格式，非 `user:hayang`）
- [ ] 修改 passcode（`/api/auth/me/passcode`）
- [ ] 退出登录后重定向 /signin

## §3 Organization 管理

- [ ] 创建 Organization 成功
- [ ] 侧栏显示"Organization"分组（含 Humans / Agents (organization) / Organization Settings，无"Org"缩写）
- [ ] Organization Settings 页正确展示，位于独立入口
- [ ] 邀请 Human 成员（合法格式 `user:alice` → 201；非法格式无前缀 → 400，非 500）
- [ ] 移除成员正常
- [ ] 多 Organization 切换（org switcher，切换不串数据）

## §4 Secrets（UserSecret BC）

- [ ] Secrets 页面正常加载
- [ ] 创建 secret（kind=mcp / cloud_credential）
- [ ] 查看/编辑/删除 secret
- [ ] master_key 缺失时系统降级处理（不崩溃，有明确提示）

## §5 Worker 注册与管理

- [ ] Fleet 页面正确显示（Organization 范围，workforce.Worker 数据源）
- [ ] "Add Worker" 生成安装命令
- [ ] Worker 通过 install command 安装并上线，延迟 <2s（不需等 30s 心跳）
- [ ] Worker 从 offline 变 online 实时反映在 Fleet 页（SSE 推送）
- [ ] Worker CLI 自动发现（ProbeAllAdapters：上线时 capabilities 自动上报，`GET /api/workers/:id/capabilities` 可见）
- [ ] Fleet 页 Remove Worker 正常（token revoke + 行消失）
- [ ] Worker 重启后 Agent 继续执行（survive-reattach，daemon 重启不杀 agent 进程）

## §6 Agent 创建与生命周期

- [ ] System → Agents → Add Agent 成功（worker_id 必填，从 Fleet 选）
- [ ] Members → Add Agent 一步完成（创建 Execution Agent + 自动加入 org 成为成员）— 等 #157
- [ ] 点击 Agent Member 打开 Agent 管理页（profile / 启停 / 重置 / active events）— 等 #157
- [ ] Agent Start / Stop / Reset 操作生效
- [ ] Agent 列表显示状态、active events 正确
- [ ] Fleet 页 agent active_count / work_items 数量正确

## §7 项目管理

- [ ] 创建 Project 成功
- [ ] 添加/移除项目成员
- [ ] 创建 Issue（状态流转：open → closed）
- [ ] 创建 Task（状态流转：open → assigned → …）
- [ ] 指派 Task 给 Agent → 生成 WorkItem → Agent 收到开始执行
- [ ] Task 完成后状态正确更新

## §8 Agent 执行任务（核心流程）

- [ ] Task 指派后 Agent 自动开始执行（WorkItem: queued → active）
- [ ] Agent 正常完成任务（WorkItem: done，Task 同步更新）
- [ ] Agent 请求用户输入（waiting_input：用户在会话里回复 → Agent 继续执行）
- [ ] Task 失败时状态可见更新（no-silent-failure，无静默僵死）
- [ ] task kill / abandon / cancel 分支（agent 收到停止指令后正确终止）
- [ ] worker daemon 重启后 Agent 继续执行，ack-offset 跨重启正确续传（不重放/不跳命令，#140 step-3 红线）
- [ ] crash-idle 自愈 relaunch 走 backoff（1/2/4/8/16s，非固定 60s，FINDING-3 回归）

## §9 控制推送（D5 SSE）

- [ ] Worker 控制命令实时推送（stream，非 poll，低延迟）
- [ ] SSE 断线自动回落 poll，无命令丢失
- [ ] 重连后 offset 续传（无重放，无跳命令）
- [ ] 同一 WorkerControlEvent 日志，stream/poll 路径命令相同
- [ ] Center 重启后浏览器 SSE 连接自动恢复（前端 live 状态恢复）

## §10 DM（Direct Message）

- [ ] 创建 DM 成功
- [ ] DM 内发送/接收消息
- [ ] DM 上传附件并发送
- [ ] DM 附件下载（创建者下载自己的附件 200）
- [ ] 跨 org DM 附件下载被拒（403）

## §11 Channel（频道）

- [ ] 创建 channel 成功
- [ ] 发送消息成功（201，消息立即显示，发送者显示真实姓名而非 `user:user-xxx`）
- [ ] 上传文件附件并发送（`POST /api/files` → 201，非 501）
- [ ] 下载已附加的文件（创建者 200，字节匹配）
- [ ] 跨 org 附件 attach 被拒（opaque 403，与不存在不可区分，字节级相等）
- [ ] 未附加 blob 不可下载（attach≠download 红线）
- [ ] 跨 org download ≡ 不存在 uri（字节级不可区分 403）
- [ ] 邀请参与者（invite panel 显示 + 操作后 participant 出现）
- [ ] 移除参与者后其下载权限被撤（403，HasActiveParticipant 只认 live participant）
- [ ] 非发起人完成上传被拒（403 + 零 uploader-ref）
- [ ] 客户端不能伪造 scope=uploader（400）

## §12 文件传输（Files BC）

- [ ] 文件上传端点正常（真 install config 下，非手搓 config）
- [ ] 上传→附加→下载正路 end-to-end（创建者 200 字节匹配）
- [ ] uploader-ref 可达 / conversation-ref 可达（两条可达路径各自正确）
- [ ] 纯 uploader-ref 不授下载（未附加进会话时 403）

## §13 可观测性

- [ ] Fleet 页 work_items 显示（新模型，非旧 task_executions）
- [ ] Agent 活动流（active events）正确反映执行进度
- [ ] 项目议题列表 org-scoped（不跨 org 泄漏）
- [ ] stats/聚合数据（token/tool_calls/working_secs）正确

## §14 环境页（Environment）

- [ ] Workers 列表显示（workforce.Worker，status=enrolled-set，非控制连接态）
- [ ] Agents 列表显示（含 agent 状态）
- [ ] File Transfer Sessions 列表（org-scoped，跨 org 不泄漏）
- [ ] 跨 org worker 访问返回 404（E-10b 不变式）
- [ ] Environment Worker 无 `last_acked_offset` 字段（已删，#140 step-3）

## §15 UI 通用

- [ ] 全站无"Org"缩写（全部为"Organization"）
- [ ] SSE 连接状态显示"live"（127.0.0.1，无干扰实例，无 AirPlay 端口冲突）
- [ ] 消息发送者和参与者显示真实姓名（非 raw identity ref）
- [ ] currentUserId 来自 `/api/auth/me`（非静态 default_user）

## §16 安全

- [ ] admin token 认证（无 token → 401）
- [ ] 跨 org 资源访问被拒（worker/agent/file/participant 各类资源）
- [ ] 非法 identity ref 格式 → 400（非 500）
- [ ] CLI 管理命令已退役（agent/secret/channel/conversation create 等命令不存在；serve/install/version/worker 仍在）— 等 #162

---

# 执行指引：验法 + 出口标准（Tester）

> 黑盒原则：以下验法只依赖**产品可见行为**（HTTP 状态/响应体、UI 可见态、CLI 输出、日志、字节级断言），不依赖实现细节。任一出口标准不满足 = 该项 FAIL → 开 task 修复后重测。

## 通用 harness（真 install，所有域共用，铁律 §1/§2）

```bash
# 1) 真实安装（非手搓 config）。隔离 prefix 避免撞机器上已有实例/AirPlay。
agent-center install center            # 默认 server :7050 + web :7100（#161 后）
# 2) 验安装生成的 config 真实含 blob_store + 非 :7000
cat ~/.agent-center/etc/config.yaml    # 必须有 blob_store 段；server.listen_addr 非 :7000
# 3) center 起来：
launchctl print gui/$(id -u)/com.agent-center.center  # state=running
curl -s http://127.0.0.1:7100/api/health              # {"status":"ok","version":"v2.7.0"}
WEB=http://127.0.0.1:7100
# 4) 注册拿 session（浏览器走 UI；脚本走 API）：
curl -s -c jar -H 'content-type: application/json' -X POST $WEB/api/auth/signup \
  -d '{"display_name":"alice","passcode":"123456","organization_name":"OrgA","organization_slug":"orga"}'
```
> 浏览器项用 Chrome DevTools（Network 看状态码 / Console 看红错 / 看 /api/sse 是否稳）。SSE 项必须在 center 确认 running 时验（center 挂 = 浏览器永远 reconnecting，非 bug）。

## §1 安装与启动
- 全新安装：`agent-center install center` exit 0；`launchctl print …center` state=running；`curl /api/health` 200。**出口**：center 进程起、:7100 可达、err 日志无 panic。
- config 含 blob_store：`grep blob_store ~/.agent-center/etc/config.yaml` 命中。**出口**：有 blob_store 段（否则 §12 文件上传必 501）。
- 升级：装旧版→`install center`(新)→`agent-center version`=v2.7.0；center 重启成功。**出口**：升级后 running、版本对、launchctl 用 bootstrap/bootout（非废弃 load）。
- worker 安装：`install worker`(用 center 给的 install command)→Fleet 出现该 worker。**出口**：worker 在 Fleet 列表、上线。
- 卸载：`uninstall center`/`uninstall worker`→`launchctl print` 服务消失、端口关。**出口**：服务停、数据目录按预期清理。

## §2 注册与登录
- fresh→/signup：全新 DB，浏览器开 `$WEB/` → 落 `/signup`；`curl $WEB/api/auth/bootstrap`→`{"initialized":false}`。**出口**：未初始化系统首屏=注册页，不是 JSON 401、不是 /signin。
- 已初始化→/signin：已有用户后未登录开 `/` → 落 `/signin`；bootstrap→`{"initialized":true}`。
- 注册/登录：signup 201；`/api/auth/me`→`{"identity_id":"user-..."}`（真实 id，**非 `user:hayang`**）。
- 改密：`PATCH /api/auth/me/passcode` 旧→新；用新密 signin 成功、旧密 401。
- 登出→`/signin`。

## §3 Organization
- 建 org：UI 建组织成功、可进其主页。侧栏有 "Organization" 分组（Humans/Agents(organization)/Organization Settings），**全文无 "Org" 缩写**。
- 邀请成员：`POST /api/members`(或 invite) 合法 `user:alice`→201；**非法格式 `not-a-ref`→400（非 500）**（#158）。
- 移除成员→成员消失。
- 多 org 切换：建第二 org，org switcher 切换；切后列表/数据是新 org 的（不串旧 org 数据）。

## §4 Secrets
- Secrets 页加载、建 secret(kind=mcp/cloud_credential)、查/改/删各生效（UI + 对应 API 2xx）。
- master_key 缺失：临时移走 master.key 重启→Secrets 操作有明确降级提示、**center 不崩**。

## §5 Worker
- Fleet 显示（org-scoped，数据=workforce.Worker）。Add Worker 出 install command。
- 上线**<2s**：worker run 后掐表，Fleet 该 worker 转 online ≤2s（#154 立即心跳，非等 30s）。
- offline→online 实时：停 worker→Fleet 转 offline；重启→秒回 online（SSE 推送）。
- CLI 自动发现：worker 上线后 `GET /api/workers/:id`（或 capabilities）含探测到的 CLI（claude-code/codex/opencode 中已装的），detected=true。
- Remove Worker→token revoke、行消失。
- survive-reattach：worker 跑着 agent 时重启 daemon→agent 进程不被杀、继续。

## §6 Agent 生命周期
- System→Agents→Add Agent：worker_id 必填(从 Fleet 选)→建成功、列表出现。
- Members→Add Agent 一步（#157）：建 Execution Agent 同时成为 org 成员；点 Agent Member 开管理页（profile/启停/重置/active events）。
- Start/Stop/Reset 各生效（UI 状态 + active events 反映）。
- Fleet agent active_count/work_items 数与实际一致。

## §7 项目管理
- 建 Project / 增删成员 / 建 Issue(open→closed) / 建 Task(状态流转) 各生效。
- 指派 Task 给 Agent → 生成 WorkItem → agent 收到、Task 完成后状态同步。

## §8 Agent 执行（核心）
- 指派后自动执行：WorkItem queued→active；正常完成→done、Task 同步。
- input request：agent waiting_input → 用户在会话回复 → agent 继续（恢复执行）。
- 失败可见：注入失败→Task/WorkItem 状态可见更新，**无静默僵死**。
- kill/abandon/cancel：发停止指令→agent 正确终止、状态对。
- **ack-offset 跨重启（#140 step-3 红线）**：agent 执行中重启 worker daemon→resume 不重放已 ack 命令、不跳未 ack（对照 D5 子矩阵场景 3/6/A4）。
- **crash-idle 自愈 backoff**：idle 时 agent crash→relaunch 间隔走 1/2/4/8/16s（非固定 ~60s）。

## §9 控制推送（D5 SSE）
- 实时推送：dispatch 命令→worker 经 SSE stream 低延迟收到执行（非 1s poll 才到）。
- 断线回落：掐 SSE 连接→worker 自动 poll 续命令、**无丢失**。
- offset 续传：重连带 `?after=<lastAcked>`→不重投已 ack、不跳未 ack。
- 同一日志：stream 与 poll 取同一 WorkerControlEvent 序列（命令集合相同）。
- center 重启恢复：重启 center→浏览器 SSE 自动重连、状态回 "live"。

## §10 DM
- 建 DM（members=`["user:<对端>"]`）→201；DM 内收发消息；上传附件并发送；创建者下载自己附件 200 字节匹配；**跨 org DM 附件下载 403**。

## §11 Channel
- 建 channel 201。发消息：输入框打字→Send enabled→点击/回车→`POST .../messages` **201**、消息**立即显示**、发送者显示**真实姓名非 `user:user-xxx`**（#155/#160）。
- 附件：`POST /api/files`(真 install config)→201（**非 501**）；上传→附加→创建者下载 200 字节匹配。
- **安全红线**（attach-authz，对照 F142 矩阵）：跨 org attach→opaque 403 与不存在 uri **字节级相等**；未附加 blob 不可下(403)；跨 org download≡不存在(字节级 403)；非发起人 complete→403 零 uploader-ref；client `scope=uploader`→400。
- 邀请：invite panel 显示、操作后 participant 出现；**移除 participant 后其下载 403**（只认 live participant）。

## §12 文件传输（Files BC）
- **必须用真 install config 跑**（#159 教训：手搓 config 会掩盖 501）。上传→附加→下载端到端创建者 200 字节匹配。
- uploader-ref / conversation-ref 两条可达路径各自正确；纯 uploader-ref（未附加进会话）不授下载 403。

## §13 可观测性
- Fleet 显示 work_items（**新模型**，非旧 task_executions）；agent active events 反映进度；议题列表 org-scoped 不跨 org；stats(token/tool_calls/working_secs) 数值合理非空。

## §14 环境页
- Workers（workforce.Worker、status=enrolled-set 非控制连接态）/Agents/File Transfer Sessions（org-scoped）各显示；**跨 org worker GET→404（E-10b）**；Environment Worker 响应**无 `last_acked_offset` 字段**（#140 step-3 已删，`curl /api/workers | grep -c last_acked_offset`=0）。

## §15 UI 通用
- 全文无 "Org" 缩写；SSE 状态 "live"（127.0.0.1、center running、无 AirPlay 撞 :7000）；发送者/参与者显示真实姓名非 raw ref；currentUserId 来自 `/api/auth/me`（非静态 default_user — 多用户各自身份正确）。

## §16 安全
- admin token：无 token 打 `/admin/*`→401；跨 org 资源（worker/agent/file/participant）一律拒；非法 identity ref→400 非 500；**CLI 管理命令已退役**（#162：`agent-center agent/secret/channel/conversation create` 等不存在/报未知命令；`serve/install/version/worker/migrate/admin` 仍在）。

---

*文档版本：v2.7-draft | 功能域：AgentCenterPD | 验法+出口标准：AgentCenterTester（黑盒、真-install、独立设计）*
