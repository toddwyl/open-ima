# open-ima Media 对账设计(V2 增量)

**状态**:待评审
**上游文档**:[open-ima 技术方案(V1)](2026-09-26-open-ima-v1-design.md)
**当前事实**:V1 的 `HandleReconcile` 只扫描 `medias.status = deleting` 并重新投递 `delete_media`;它是删除补偿,不是完整对账。

---

## 1. 背景与问题

当前删除链路是补偿式删除:

1. `DeleteMedia` 将 media 标记为 `deleting`,并投递 `delete_media`;
2. `HandleDeleteMedia` 依次删除 Meili chunk、storage 源文件、SQLite media 行;
3. 主进程每 10 分钟投递 `reconcile`,由 `HandleReconcile` 重新投递仍卡在 `deleting` 的 media。

这能处理"media 行还在且状态为 deleting"的中断,但不能发现三方账本已经分叉的情况。例如:

- storage 里有孤儿文件,但 `medias.source_uri` 已无引用;
- Meili 里仍有某个 `media_biz_id` 的 chunk,但 SQLite 已无对应 media;
- SQLite 里 media 是 `ready`,但 Meili chunk 缺失或数量不一致;
- SQLite 里 media 引用了文件,但 storage 文件已经丢失;
- `delete_media` job 重试耗尽后变成 `failed`,media 仍停在 `deleting`。

因此 V2 需要引入真正的对账:以 `medias` 为业务事实账本,由主程序定时比对 media、storage、Meili 和 jobs,发现差异后直接执行幂等补偿或投递既有任务,并输出日志摘要。

## 2. 目标与非目标

### 目标

1. **明确账本权威**:`medias` 是业务事实账本;`chunks` 是 media 的派生索引元数据;`jobs` 是异步补偿执行账本;storage 和 Meili 是可重建的外部投影。
2. **发现差异**:对比 DB、storage、Meili 的 media/chunk 视图,产出结构化 anomaly。
3. **幂等修复**:删除孤儿外部资源、重新投递删除、重新解析/重建索引,所有动作可重复执行。
4. **低成本运行**:默认每 10 分钟做轻量扫描;重型全量扫描低频执行,不进入用户请求链路。
5. **轻量可观测**:对账任务输出日志摘要、计数和最近错误;首版不新增对账持久表。

### 非目标

- 不在 V2 引入分布式事务或两阶段提交;外部系统仍通过补偿达到最终一致。
- 不要求 storage 与 Meili 成为强一致读写路径;用户请求仍以 SQLite 状态为准。
- 不做跨设备/远端对象存储的实时变更订阅;只做内部周期扫描。
- 不提供 HTTP/admin/CLI/API 入口,也不新增供其他用例调用的 reconcile port;对账由主程序定时投递后台 job。

## 3. 术语

| 术语 | 含义 |
| --- | --- |
| 业务事实账本 | SQLite 中的 `medias`,记录 media 是否存在、属于哪个 KB、当前生命周期状态和源文件引用。 |
| 派生元数据 | SQLite 中的 `chunks`,由 media 解析结果生成,用于记录本地 chunk 业务键与顺序。 |
| 执行账本 | SQLite 中的 `jobs`,记录异步任务、重试和补偿状态,不能反向决定 media 是否存在。 |
| 外部投影 | storage 源文件与 Meili chunk 文档,可由 media 事实与派生元数据重建或删除。 |
| anomaly | 一条具体不一致事实,例如 `storage_orphan_file`。 |
| repair | 针对 anomaly 的幂等补偿动作,例如投递 `delete_media` 或 `parse_media`。 |
| deleting sweep | V1 已有的 `deleting` 状态扫尾,是对账的一个子集。 |

## 4. 一致性规则

以 `media_biz_id` 为业务键,以 `medias` 行为判断业务存在性的第一依据,各层满足以下规则:

1. `medias.status = ready`:
   - storage 必须存在 `source_uri`;
   - SQLite `chunks` 至少有 1 行,数量应等于 `medias.chunk_count`;
   - Meili 应存在同 `media_biz_id` 的 chunk 文档,数量应等于 SQLite chunks 数量。
2. `medias.status in (pending, parsing, chunking, indexing, failed)`:
   - storage 必须存在 `source_uri`;
   - Meili 可以没有 chunk;若有旧 chunk,解析或重建索引时先按 `media_biz_id` 删除。
3. `medias.status = deleting`:
   - 最终目标是 storage、Meili、SQLite media 行全部删除;
   - 对账必须确保存在一个可运行的 `delete_media` 补偿路径。
4. storage 文件:
   - 若 key 不被任何 `medias.source_uri` 引用,它是 `storage_orphan_file`;
   - 孤儿文件默认进入隔离删除策略,不得影响仍有引用的 media。
5. Meili chunk:
   - 若 `media_biz_id` 在 SQLite 不存在或状态为 `deleting`,它是 `meili_orphan_media`;
   - 若 chunk id 不在 SQLite `chunks.chunk_biz_id` 中,它是 `meili_orphan_chunk`;
   - 若 SQLite chunk 在 Meili 缺失,它是 `meili_missing_chunk`。

## 5. Anomaly 类型与修复动作

| anomaly | 发现方式 | 修复动作 | 幂等性 |
| --- | --- | --- | --- |
| `deleting_stuck` | DB: `medias.status = deleting` | 投递 `delete_media` | `HandleDeleteMedia` 可重复删除 Meili/filter 与 storage key |
| `delete_job_exhausted` | DB: `deleting` media 且对应 `delete_media` job 为 `failed` | 投递新的 `delete_media`,记录旧 job id | 新 job 与旧 job 不共享状态 |
| `storage_missing_file` | DB 引用的 `source_uri` 在 storage 不存在 | 将 media 标记 `failed`,错误为 `storage missing`;禁止直接删除 DB | 保留业务事实,等用户重新上传或删除 |
| `storage_orphan_file` | storage key 不在 DB 引用集合 | 首版只打日志;若开启自动清理,超过保留窗口后删除文件 | `Delete` 对不存在文件成功 |
| `meili_orphan_media` | Meili 有 `media_biz_id`,DB 无 media 或状态 `deleting` | `DeleteByFilter(media_biz_id = ?)` | Meili filter 删除可重复 |
| `meili_orphan_chunk` | Meili chunk id 不在 SQLite chunks | `DeleteByFilter(id = ?)` 或按 media 重建 | 删除可重复 |
| `meili_missing_chunk` | SQLite ready chunk 不在 Meili | 投递 `parse_media` 重建该 media 索引 | parse 前先清旧索引 |
| `chunk_count_mismatch` | `medias.chunk_count != count(chunks)` | 投递 `parse_media` 重建 | ReplaceChunks + DeleteByFilter 幂等 |
| `ready_without_chunks` | ready media 无 SQLite chunks | 投递 `parse_media` 重建 | 同上 |

V2 首期只自动修复低风险项:`deleting_stuck`、`delete_job_exhausted`、`meili_orphan_media`、`meili_missing_chunk`、`chunk_count_mismatch`。`storage_orphan_file` 默认只打日志,不自动删除。

## 6. 运行形态

首版不新增 `reconcile_runs` / `reconcile_anomalies` 表,也不提供外部触发接口。对账是一个内部后台任务:

1. `cmd/open-ima/main.go` 按配置周期投递 `reconcile` job;
2. worker 执行 `HandleReconcile`;
3. handler 在一次运行内累计 `scanned/anomaly/repaired/failed` 计数;
4. 对可安全修复的差异直接执行幂等修复,或投递 `delete_media` / `parse_media`;
5. 每个修复动作输出一条结构化日志,说明"因为哪个 anomaly 处理了哪个资源";
6. 结束时输出一条结构化日志摘要。

示例日志:

```text
reconcile action=enqueue_delete anomaly=deleting_stuck media_biz_id=m_123 reason="media deleting without active delete job" job_type=delete_media job_id=42 result=ok
reconcile action=delete_external anomaly=meili_orphan_media media_biz_id=m_404 resource=meili resource_key="media_biz_id = 'm_404'" reason="media row not found" result=ok
reconcile scope=light scanned=120 anomalies=3 repaired=2 failed=1 duration=438ms last_error="storage missing: m_123"
```

如果后续需要 dry-run、UI 展示、审计追溯或 storage orphan 延迟删除记录,再单独引入持久化对账表。

日志字段约定:

| 字段 | 含义 |
| --- | --- |
| `scope` | `light` / `full` / `media`。 |
| `action` | `enqueue_delete` / `enqueue_parse` / `delete_external` / `mark_failed` / `none` / `summary`。 |
| `anomaly` | 触发动作的不一致类型,例如 `meili_orphan_media`。 |
| `media_biz_id` | 能定位到 media 时必须记录。 |
| `resource` | 被处理的外部资源类型,例如 `meili` 或 `storage`。 |
| `resource_key` | 被删除或检查的具体 key/filter/chunk id。 |
| `reason` | 面向排查的人类可读原因。 |
| `result` | `ok` / `failed` / `skipped`。 |
| `error` | 失败时记录错误。 |

## 7. 外部投影检查能力

### 7.1 Storage Inspector

当前 `port.FileStore` 没有枚举能力,只能 `Put/Get/Delete/URL`。storage 对账需要读取外部投影视图,因此在 storage 依赖侧补充窄接口:

```go
type StoredObject struct {
    Key       string
    Size      int64
    UpdatedAt time.Time
}

type FileStoreInspector interface {
    Exists(ctx context.Context, key string) (bool, error)
    List(ctx context.Context, prefix string, cursor string, limit int) ([]StoredObject, string, error)
}
```

`LocalStorage` 实现:

- `Exists` 用 `os.Stat`;
- `List` 遍历 `root/<hash-prefix>/<key>`,跳过临时文件 `.tmp-*`;
- 返回 cursor,避免一次性载入全部文件。

对象存储实现:

- `Exists` 映射 HEAD Object;
- `List` 映射 ListObjectsV2;
- cursor 直接透传 provider token。

### 7.2 Search Inspector

当前 `port.Indexer` 只有写入和 filter 删除。Meili 对账需要读取外部投影视图,因此在 search 依赖侧补充窄接口:

```go
type IndexedChunkRef struct {
    ID         string
    MediaBizID string
    KBBizID    string
}

type IndexInspector interface {
    ListByMedia(ctx context.Context, index, mediaBizID string, limit int) ([]IndexedChunkRef, error)
    ListMediaIDs(ctx context.Context, index string, cursor string, limit int) ([]string, string, error)
}
```

Meili 实现建议:

- `ListByMedia`:使用 documents endpoint,filter `media_biz_id = '<id>'`,只取 `id/media_biz_id/kb_biz_id`;
- `ListMediaIDs`:分页拉取 documents 后在 Go 侧按 `media_biz_id` 去重;Meili 不提供 group by,所以要限制批量和超时。

V2 首期也可以先不实现全量 `ListMediaIDs`,只从 SQLite media 集合出发做 `ListByMedia`;Meili 孤儿全量扫描留给低频 `full` scope。

## 8. 对账任务设计

### 8.1 Job 类型

保留现有 `reconcile`,但 payload 显式化:

```json
{
  "scope": "light",
  "media_biz_id": "",
  "repair": true,
  "cursor": ""
}
```

scope:

- `light`:默认周期任务,只扫 DB 中 `deleting`、`ready`、`failed` 中需要低成本验证的 media;
- `media`:只对一个 `media_biz_id` 做完整检查,供测试或内部流程局部验收使用;
- `full`:低频周期任务,扫描 storage 与 Meili 全量投影。

### 8.2 调度

1. 主程序仍每 10 分钟投递一次 `reconcile{scope:light, repair:true}`。
2. 主程序可按较低频率投递 `reconcile{scope:full, repair:true}`;首版可以先不启用 full scope。
3. 删除、重试、全量重建后的局部校验只在内部测试或后续需要时使用,不暴露给 HTTP/API 调用方。
4. 同一 scope 建议加去重闸门:若存在 `type = reconcile` 且 `status in (pending,running)`,周期任务不重复投递。

## 9. 核心流程

### 9.1 Light Reconcile

```text
HandleReconcile(scope=light):
  stats = NewStats(light)
  ids = Query medias where status in ('deleting', 'ready', 'failed')

  for media in ids:
    stats.scanned++
    if media.status == deleting:
      ensureDeleteJob(media)
      log repair action=enqueue_delete anomaly=deleting_stuck media_biz_id=... reason="deleting media has no active delete job" result=ok
      stats.repaired++
      continue

    if source_uri != '':
      exists = store.Exists(source_uri)
      if !exists:
        markFailed(media, "reconcile: storage file missing")
        log repair action=mark_failed anomaly=storage_missing_file media_biz_id=... resource=storage resource_key=source_uri reason="media source_uri missing in storage" result=ok
        stats.failed++
        continue

    if media.status == ready:
      chunks = db.ListChunks(media_biz_id)
      indexed = index.ListByMedia(media_biz_id)
      if len(chunks) == 0 or len(chunks) != media.chunk_count:
        enqueue(parse_media)
        log repair action=enqueue_parse anomaly=chunk_count_mismatch media_biz_id=... reason="media chunk_count differs from chunk rows" result=ok
        stats.repaired++
      else if indexed IDs != chunk IDs:
        enqueue(parse_media)
        log repair action=enqueue_parse anomaly=meili_missing_chunk media_biz_id=... resource=meili reason="indexed chunks differ from media chunks" result=ok
        stats.repaired++

  log stats summary
```

### 9.2 Full Reconcile

```text
HandleReconcile(scope=full):
  stats = NewStats(full)
  dbSourceURIs = set(SELECT source_uri FROM medias WHERE source_uri != '')
  dbMediaIDs = set(SELECT media_biz_id FROM medias)

  scan storage pages:
    if key not in dbSourceURIs:
      log anomaly action=none anomaly=storage_orphan_file resource=storage resource_key=key reason="no media references source_uri" result=skipped
      if configured auto-delete and key older than retention:
        store.Delete(key)
        log repair action=delete_external anomaly=storage_orphan_file resource=storage resource_key=key reason="storage key has no media reference" result=ok
        stats.repaired++

  scan Meili media ids:
    if media_biz_id not in dbMediaIDs:
      if repair:
        index.DeleteByFilter(media_biz_id)
        log repair action=delete_external anomaly=meili_orphan_media media_biz_id=... resource=meili resource_key="media_biz_id = ..." reason="media row not found or deleting" result=ok
        stats.repaired++

  run light reconcile checks
  log stats summary
```

### 9.3 Delete Job 去重

删除补偿要避免每 10 分钟无限堆积 pending job。新增 DAO 查询:

```sql
SELECT COUNT(*)
FROM jobs
WHERE type = 'delete_media'
  AND status IN ('pending', 'running')
  AND json_extract(payload, '$.media_biz_id') = ?
```

若 SQLite JSON 函数兼容性存在风险,替代方案是新增 `jobs.dedupe_key`:

```sql
ALTER TABLE jobs ADD COLUMN dedupe_key TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_jobs_dedupe ON jobs(type, dedupe_key, status);
```

入队时 `delete_media` 使用 `dedupe_key = media_biz_id`,`reconcile` 使用 `dedupe_key = scope`。推荐采用 `dedupe_key`,因为它不依赖 payload 字符串结构,也便于普通索引。

## 10. 分层落位

```text
application/ingest/
  reconciliation.go        # Reconciler: Detect + Repair 编排
  reconciliation_types.go  # Scope/Stats/Anomaly 类型(内存态,不落表)
  service.go              # HandleReconcile 委托给 Reconciler

application/port/
  storage.go              # 增加 FileStoreInspector
  search.go               # 增加 IndexInspector

domain/media/
  repository.go           # 增加 ListAll/StatsForReconcile 所需契约
  service.go              # 暴露对账读取与 MarkFailed/ResetForReindex

infrastructure/db/
  media_repository.go     # 对账查询实现
  migrations.sql          # jobs.dedupe_key 迁移(如采用)

infrastructure/storage/
  local.go                # Exists/List

infrastructure/meili/
  client.go               # ListByMedia/ListMediaIDs
```

依赖方向保持不变:`application/ingest` 只依赖 domain service 与 port;DB、Meili、storage 的枚举实现仍在 infrastructure。

## 11. 幂等与并发规则

1. 所有 repair 任务必须可重复执行:
   - Meili 删除按 filter;
   - storage 删除对不存在文件返回 nil;
   - reindex 先删除旧 Meili chunk,再写入新 chunk;
   - delete media 最后才删 SQLite 行。
2. `reconcile` 不直接删除 SQLite media 行;业务删除仍走 `delete_media`。
3. 对账发现 `storage_missing_file` 时不自动删 DB,而是标记 `failed`,保留用户可见错误。
4. 同一 media 同时存在 parse/delete 时,`deleting` 优先;parse handler 遇到 deleting 已经直接返回。
5. Full scan 要分页执行,每页处理后释放内存,避免长事务和超大内存集合。
6. 周期 reconcile 不应阻塞上传与聊天请求;它只通过 jobs worker pool 消耗后台并发。

## 12. 配置与运维

建议新增配置:

```yaml
reconcile:
  interval: 10m
  light_batch_size: 200
  full_batch_size: 1000
  orphan_retention: 24h
  auto_delete_storage_orphans: false
  max_run_duration: 15m
```

默认策略保守:只自动修复可重建的索引差异和删除补偿;storage 孤儿文件先记录,不自动删除。

## 13. 对外接口边界

首版不提供任何对外调用入口:

- 不新增 HTTP admin API;
- 不新增 CLI 命令;
- 不新增给其他 application service 调用的 reconcile port;
- 不做前端 UI 展示。

对账唯一入口是主程序定时投递 `reconcile` job。测试可以直接调用 `HandleReconcile` 或 `Reconciler.Run`,但这是包内/用例内验证手段,不是产品接口。

## 14. 验收用例矩阵

| 用例 | 准备 | 期望 |
| --- | --- | --- |
| 删除中断 | media 为 `deleting`,无可运行 `delete_media` | light reconcile 投递 `delete_media` |
| 删除 job 耗尽 | media 为 `deleting`,旧 job 为 `failed` | 投递新 `delete_media`,记录 `delete_job_exhausted` |
| Meili 孤儿 | Meili 有 media chunk,DB 无 media | full reconcile 删除 Meili chunk |
| Meili 缺失 | DB ready 有 chunks,Meili 无文档 | light reconcile 投递 `parse_media` |
| chunk 数不一致 | `media.chunk_count` 与 `chunks` 行数不一致 | 投递 `parse_media`,记录 mismatch |
| storage 缺失 | DB 引用的 source_uri 不存在 | 标记 media failed,不删 DB |
| storage 孤儿 | storage 文件无 DB 引用 | full reconcile 打日志;默认不删除 |
| 幂等重跑 | 同一 reconcile 连跑两次 | 第二次无重复破坏,无重复 pending repair job |
| 并发删除 | reconcile 与用户 DELETE 同时发生 | 最终只保留 deleting/delete_media 路径,无 ready 回退 |

## 15. 分阶段落地

### Phase 1: 名实对齐与轻量对账

- 将现有 `HandleReconcile` 内部重命名为 deleting sweep 语义,保留 job 类型 `reconcile`;
- 增加 `dedupe_key`,避免周期任务堆积;
- 实现 `delete_job_exhausted` 检测;
- 输出日志摘要;不新增对账表。

### Phase 2: DB ↔ Meili 对账

- 增加 `IndexInspector.ListByMedia`;
- 对 ready media 检查 SQLite chunks 与 Meili chunk 集合;
- 缺失或不一致时投递 reindex/parse;
- 全量 Meili orphan 扫描作为低频 full scope。

### Phase 3: DB ↔ Storage 对账

- 增加 `FileStoreInspector.Exists/List`;
- light 检查 DB 引用文件是否存在;
- full 发现 storage orphan,默认记录,可配置保留窗口后删除。

## 16. 文档修正建议

V1 文档中"对标文章 1 聚合服务 + 对账"应改为"删除补偿";完整对账能力以本文为准。当前已实现能力只能声称:

> 周期任务会扫描卡在 `deleting` 的 media 并重新投递 `delete_media`,用于补偿删除流程中断。

不能声称已经实现:

> 发现 storage/Meili 与 SQLite 的孤儿资源或缺失投影,并自动修复。
