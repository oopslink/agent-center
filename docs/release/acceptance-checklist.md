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

*文档版本：v2.7-draft | 编写：AgentCenterPD | 验法由 AgentCenterTester 补充*
