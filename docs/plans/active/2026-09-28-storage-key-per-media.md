# Storage key 改为按 media 维度生成（修复跨库共享文件误删）

## 状态

待实施（方案已评审对齐，仅记录待办）。

## 背景与问题

当前上传链路中，`source_uri` 直接取文件内容的 SHA-256 十六进制（内容寻址），
同一 `key` 同时兼任 storage 存储 key 与 `medias.file_hash`：

- `internal/interfaces/http/medias.go` `handleUpload`：`key = hex(sha256(file))`，
  先 `store.Put(key, ...)` 再 `CreateMedia(..., sourceURI=key, fileHash=key)`。
- 同一文件上传到不同 KB 时，因同库去重索引 `uq_medias_hash(kb_biz_id, file_hash)`
  作用域为单库，两个 KB 会各自生成 media 行，但**共享同一份物理文件**。
- `internal/application/ingest/service.go` `HandleDeleteMedia` 按 `doc.SourceURI`
  **无条件删除 storage 文件**，不检查是否还有其他 media 引用。

后果：在 KB-A 删除该文件后，KB-B 的 media（status=ready）引用的 `source_uri`
已不存在；对账扫描命中 `storage_missing_file`，KB-B 的 media 被标记 `failed`，
用户数据实质受损。这违反了对账设计的一致性规则
（`docs/design/2026-09-28-media-reconciliation-design.md` §4：
ready media 的 storage 必须存在 `source_uri`）。

## 方案（已对齐）

**storage key 改为按 media 维度生成：`source_uri = media_biz_id`（可带扩展名）。**

- `kb_biz_id` 与 `hash` 不进入 key：`media_biz_id` 全局唯一，已足够寻址；
  key 组合越长，`validKey` 校验越复杂。
- key 语义直接反映实体（media），符合命名契约。

### 连带改动清单

1. **上传顺序调整**（`handleUpload`）：
   现顺序为「算哈希 → Put → CreateMedia（内部去重）」。改为：
   算哈希 → 先 `FindIDByHash(kb, hash)` 查重，命中直接返回 `duplicate=true`（不 Put）；
   未命中 → 生成 `media_biz_id` → `Put(media_biz_id 派生 key)` → 登记 media。
   否则重复上传会按新 key 留下无人引用的孤儿文件。
2. **`validKey` 校验重写**（`internal/infrastructure/storage/local.go`）：
   现要求 key 为 64 位十六进制；改为 media_biz_id 形态的白名单校验
   （如 UUID 字符集 + 可选扩展名）。该校验是 parser 回拉
   `GET /internal/files/{key}?token=...` 的安全边界，不得放宽为任意字符串。
3. **`file_hash` 列保留**：不再兼任 storage key，仅承担同库去重
   （`FindIDByHash` + `uq_medias_hash`）。哈希管去重、key 管寻址，职责分离。
4. **删除路径**：`HandleDeleteMedia` 无需加引用检查——key 独占后按
   `source_uri` 删除天然安全。 Meili / DB 删除逻辑不变。
5. **存量数据迁移**：已有 media 的 `source_uri` 仍为旧哈希 key。二选一：
   - 写一次性迁移：按 media_biz_id 重命名搬移文件并更新 `source_uri`；
   - 或 `user_version` +1，按既有守卫约定删库重建（本地项目可接受，
     参照 v1→v2 media 改名的先例）。

### 放弃的换取

内容寻址的跨库物理去重：同一文件传 N 个库将存 N 份。单机本地场景
磁盘成本可忽略，正确性优先。

## 下一步

1. 建 worktree：`git worktree add .worktrees/storage-key-per-media -b storage-key-per-media main`。
2. 按「连带改动清单」1–5 实施。
3. 补回归测试：跨 KB 传同一文件 → 删除其中一个 → 另一个仍 ready 且文件可读
   （固化进 harness；对账不应再报 `storage_missing_file`）。
4. `./scripts/harness.sh` 全量通过后 commit。

## 注意事项

- `HandleDeleteMedia` 的 Meili 删除（`DeleteByFilter` 按 `media_biz_id`）与
  DB 行删除顺序不变，本方案只动 storage key 的生成与上传顺序。
- 若选择删库重建路线，需同时清理 `data/meili` 索引目录（参照
  `docs/guide/common-pitfalls.md`「删库重建」条目）。
- 实施完成后将本文档从 `active/` 移至 `completed/`。
