# 0048. File URI + BlobStore + FileTransfer 边界（v2.7）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-29 |
| Delivered | v2.7 design phase；详 [v2.7-domain-refactor-plan § 2.7 / § 10 OQ8 / § 10 OQ9](../../plans/v2.7-domain-refactor-plan.md) |
| Supersedes | — |
| Related | [ADR-0047 Conversation owner_ref + context_refs](0047-conversation-owner-ref-and-context-refs.md)（附件引用 File URI） / [ADR-0050 Environment BC](0050-environment-bc.md)（FileTransfer 归属 Environment） |

## Context

v2.7 需要在 Conversation 附件、Task/Issue 产出、Agent 产物之间共享文件，且文件可被**分发到多个业务范围**（一份设计稿同时挂在 Task、贴进 Conversation、被 Agent 存进 memory）。设计讨论中明确：**不引入 `file_id` 业务概念，按 URI 引用**；进一步否决了把 scope 写进 URI（同一文件多 scope 会冲突），也否决了内容寻址（CAS / 客户端声明哈希），最终选择**不透明 ULID 身份 + 横向通用存储模块**（plan § 10 OQ8）。

## Decision

### 1. 文件存储是横向通用模块（BlobStore）

- BlobStore 不认识 Task/Issue/Agent/Conversation，只存取 blob。
- blob 身份 = 服务端生成的**不透明 ULID**。**不做内容哈希寻址**。
- 物理布局按 `hash(ulid)` 分桶：`~/.agent-center/files/objects/{h1}/{h2}/{ulid}`，`{h1}{h2}` 取 `hash(ulid)` 前缀。对 ULID 再 hash 是为了打散其时间戳前缀、避免目录热点。
- blob 写一次（write-once）；同名上传永不覆盖（每次新 ULID，构造上无冲突）。
- v1 不做自动去重；元数据存 **content sha256** 仅用于完整性校验 + 将来 opt-in 去重，**不参与寻址**。
- 首个后端 = 中心本地文件系统；resolver 把 URI 映射到物理桶路径，后端可换而 URI 不变。

### 2. File URI = scope-free

```text
ac://files/{ulid}
```

- 创建上传会话时即返回（ULID 即时已知），调用方始终先拿到最终 URI。
- 传输 URI：`ac://transfers/...`。

### 3. 身份 / 放置分离（identity ≠ placement）

- "文件用在哪" = **引用记录** `{scope, scope_id} -> file_uri`，scope ∈ `task | issue | project | conversation | agent | tmp`。
- 多对一：一个 blob 可被任意多处引用；分发到不同 scope = 多加引用，**不复制 blob**。
- 文件名 / mime / size / 显示名落**引用/附件元数据**，不在 blob 身份上；同一 blob 不同处可显示不同名。

### 4. 授权 = 可达性（reachability）

- URI 是引用，不是授权。下载/上传仍走 Environment/FileTransfer AppService 鉴权。
- 调用方能读某 blob，当且仅当其能在自己 Org/Project 域内访问到至少一条指向它的引用（与 [ADR-0046](0046-projectmanager-bc.md) / plan § 10 OQ6 域隔离一致）。

### 5. 保留 / GC

- 删业务对象 = **软删其引用**，不删 blob。
- 异步 GC：blob 的存活引用计数归零且过宽限期（默认 **7 天**）后物理删除。
- 跨引用安全：任一 Org/Project 仍存活引用则不回收。
- 孤儿上传（从未被引用）同样按此回收。

### 6. 阶段归属

- A0：FileURI 值对象 + resolver 骨架 + 引用记录骨架 + BlobStore 模块接缝（**不含上传/下载机制**）。
- D（Environment + FileTransfer）：上传/下载会话、分块、完成/取消/过期、本地后端、引用计数 GC job。

## Consequences

### 正面

- 文件存储与业务彻底解耦，可独立演进/换后端。
- 跨 scope 共享天然支持，零复制。
- 身份不透明 + 可达性授权，避免内容哈希的侧信道与校验负担，同时保留"创建即拿最终 URI"。

### 负面 / 待跟进

- v1 无自动去重，同内容多份占空间（单用户可接受；可后补 content-sha256 索引去重，URI 不变）。
- 可达性授权需 join（file_uri → 引用 → scope → 域校验），比解析路径略重；单用户无感。
- GC 为异步最终回收，删除后磁盘空间有 7 天宽限延迟。

## Alternatives Considered

### A. scope 写进 URI（`ac://files/{scope}/{scope_id}/{key}`）

- ✅ 一眼看出归属、按前缀清理
- ❌ 同一文件分发多 scope 时要么复制、要么 URI 自相矛盾
- 否决（@oopslink 2026-05-29 指出）

### B. 内容寻址（CAS，git 式）/ 客户端声明哈希

- ✅ 自动去重 + 完整性自校验
- ❌ 需客户端算哈希 + 服务端强制校验；且 @oopslink 评估"感觉也不好"，偏好不透明 ULID
- 否决（content sha256 降为可选元数据，仅校验/未来去重）

### C. `file_id` 业务外键

- ✅ 关系型直观
- ❌ @oopslink 明确不要 file_id；不利于可移植/可换后端的 URI 引用语义
- 否决

## References

- [v2.7-domain-refactor-plan § 2.7 File URI/BlobStore/FileTransfer](../../plans/v2.7-domain-refactor-plan.md) / [§ 3.3 BlobStore + FileReference](../../plans/v2.7-domain-refactor-plan.md) / [§ 10 OQ8](../../plans/v2.7-domain-refactor-plan.md) / [§ 10 OQ9](../../plans/v2.7-domain-refactor-plan.md)
- [ADR-0047 Conversation owner_ref + context_refs](0047-conversation-owner-ref-and-context-refs.md)
- [ADR-0050 Environment BC](0050-environment-bc.md)
- 来源：2026-05-29 DM 设计讨论（@oopslink ↔ @AgentCenterPD）
