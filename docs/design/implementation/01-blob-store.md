# BlobStore

agent-center 自产的"大块内容"通过 BlobStore 抽象访问。DB 只存元数据 + 相对路径。

> 设计动机 / 决策见 [ADR-0006](../decisions/0006-blob-store-for-large-content.md)。

## 原则

- 大块内容**不进数据库**
- 实际内容由 BlobStore 实现持久化
- DB 字段存"相对路径"（如 `tasks/42/log.log.gz`）
- 未来从本地目录迁移到 S3 / OSS / MinIO，**DB 数据不用动**

## 走 BlobStore 的内容

| 内容 | 大小级别 | 备注 |
|---|---|---|
| 任务原始日志归档（`.log.gz`） | MB 级 | 任务结束 worker 上传 |
| Agent trace 归档（`.jsonl.gz`） | KB-MB 级 | 可选：结构化事件已在 events 表，归档作为完整备份 |
| Issue / 任务附件（未来） | 任意 | v1 不实现 |

## **不走** BlobStore 的内容

| 内容 | 存储 |
|---|---|
| Project 元信息 | DB 字段 |
| Task / Issue / Comment 文本 | DB 字段，单条结构化数据 |
| Supervisor memory 单条 | DB 字段 |
| Events 表条目 | append-only 关系表 |
| 项目 CLAUDE.md / AGENTS.md | 在项目仓库里，**不归 agent-center 管** |
| `worker-agent.md` / `supervisor.md` skill | 跟随 binary embed |

## 接口（伪 Go）

```go
type BlobStore interface {
    Put(ctx context.Context, relPath string, content io.Reader, size int64) error
    Get(ctx context.Context, relPath string) (io.ReadCloser, error)
    Delete(ctx context.Context, relPath string) error
    Exists(ctx context.Context, relPath string) (bool, error)
    List(ctx context.Context, prefix string) ([]string, error)
    URL(relPath string) string  // 给 UI / 飞书卡片用的可显示 URL（v1: file://；未来: https://）
}
```

## 实现

| 实现 | 状态 | 说明 |
|---|---|---|
| **LocalDirBlobStore** | v1 默认 | root 是个目录（如 `/var/lib/agent-center/blobs/`），`relPath` 拼到 root 下；`URL()` 返回 `file://...` |
| **S3CompatibleBlobStore** | 未来扩展点 | root 是 `s3://bucket/prefix`，`URL()` 返回预签名 URL 或公开 URL；底层 minio-go / aws-sdk-go-v2 |

## 路径约定

相对路径按"语义路径"组织，便于浏览和迁移：

```
projects/<project_id>/                       # 项目相关聚合（v1 暂无）

tasks/<task_id>/
  ├─ log.log.gz                              # 任务原始日志归档
  ├─ trace.jsonl.gz                          # agent trace 完整归档（可选）
  └─ artifacts/                              # 未来的附件 / 产物（v1 不用）

supervisor_invocations/<invocation_id>/
  └─ session.jsonl.gz                        # supervisor 一次调用产生的 claude code JSONL（可选）
```

DB 表里只存相对路径（concept，schema 见 [02-persistence-schema.md](02-persistence-schema.md) TBD）：

```
task.log_blob_path                  TEXT  -- 'tasks/42/log.log.gz' (NULL 表示还没上传)
task.trace_blob_path                TEXT  -- 'tasks/42/trace.jsonl.gz'
supervisor_invocation.session_blob_path  TEXT
```

## 配置

```yaml
# agent-center server config
blob_store:
  kind: local           # 'local' | 's3' (未来)
  root: /var/lib/agent-center/blobs
  # S3 示例:
  # kind: s3
  # endpoint: https://oss.example.com
  # bucket: agent-center-prod
  # prefix: blobs/
  # access_key_id: ...
  # secret_access_key: ...
  # region: cn-shanghai
```

## 从 local 迁移到 S3（流程）

操作流程，**零 DB 改动**：

1. 安装 / 配置好目标对象存储
2. 工具：`agent-center admin blob-migrate --to=s3://...`
   - 遍历 LocalDirBlobStore 的全部内容
   - 逐文件 PutObject 到 S3
   - 校验大小 / hash
3. 修改 `agent-center` 配置 `blob_store.kind: s3` + 凭据
4. 重启 server
5. （可选）清理本地目录

DB 里的 `log_blob_path` / `trace_blob_path` 等相对路径**不变**，仍然有效。

## 生命周期管理

- **写入**：任务结束 worker 上传 → server 通过 `BlobStore.Put` → 入库回填路径
- **保留期**：默认 90 天（可配）；超期 GC 任务定期扫表 + Delete blob + 置空路径
- **读取**：CLI `agent-center logs <task-id> --download` / `inspect` 等通过 `BlobStore.Get`；飞书卡片附 URL 用 `BlobStore.URL`（v1 是本机路径，未来是 https）
