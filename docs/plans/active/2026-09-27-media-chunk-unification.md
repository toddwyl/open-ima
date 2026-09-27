# Media 统一改造详细计划（document → media，对齐腾讯 ima 设计）

**状态**：待评审（决策点已默认按"全部改齐"拟定）
**关联分支**：`feat/media-rename`（worktree：`.worktrees/media-rename`）
**关联文档**：[设计文档](../design/2026-09-26-open-ima-v1-design.md)、[腾讯参考文章 1](../reference/2608466-ima-architecture-from-0-to-1.html)、[参考文章 2](../reference/2747411-ima-tencent-es-knowledge-base.html)

---

## 背景

腾讯 ima 的两层模型为 **Media**（用户侧统一管理结构，格式无关）/ **Chunk**（RAG 侧检索单元）。当前代码 `documents`/`chunks` 模型职责已与之一致，差异仅在命名：`Document` 暗示"文档"，且 `file_*` 字段对 url / 未来 note 源不贴切。本计划把 `document` 概念**整体改名**为 `media`，Schema 升级 v2，使代码、DB、Meilisearch、API、前端与腾讯设计逐字对齐。`chunks` 表、Meili index 名、Block 概念全部保留不动。

**Schema 策略**：不做数据迁移，`PRAGMA user_version` 守卫，旧库报错删库重建（与现行策略一致）。注意当前 `internal/infrastructure/db/db.go` 的 `schemaVersion = 1` 且守卫只挡 `version == 0` 的库——本次需把守卫改为"version 与 schemaVersion 不符即报错"，否则 v1 旧库会被静默建出新的空 `medias` 表。

## 命名映射总表

| 现名 | 新名 | 所在层 |
| --- | --- | --- |
| 表 `documents` | `medias` | SQLite |
| `document_biz_id`（列 / JSON 键 / Meili 字段） | `media_biz_id` | 全链路 |
| 包 `internal/domain/document` | `internal/domain/media` | Go domain |
| `Document` / `DocumentService` / `DocumentRepository` | `Media` / `MediaService` / `MediaRepository` | Go domain |
| 索引 `idx_documents_kb` / `uq_documents_hash` | `idx_medias_kb` / `uq_medias_hash` | SQLite |
| `documentsHandler` / `documents.go` | `mediasHandler` / `medias.go` | interfaces/http |
| 路由 `/api/kbs/:id/documents[:url]`、`/api/documents/:id[/retry]` | `/api/kbs/:id/medias[:url]`、`/api/medias/:id[/retry]` | API |
| job 类型 `parse_document` / `delete_document` | `parse_media` / `delete_media` | jobs 表 |
| job payload 键 `document_biz_id` | `media_biz_id` | jobs payload |
| KB 列表字段 `doc_count` | `media_count` | API + 前端 |
| `Citation.DocumentBizID`（JSON `document_biz_id`） | `MediaBizID`（JSON `media_biz_id`） | domain + API |
| 前端 `Document` 类型 / `listDocuments` 等 | `Media` 类型 / `listMedias` 等 | web/src |

不改：`chunks` 表名、`chunk_biz_id`、Meili index 名 `chunks`、`file_type`/`file_hash`/`source_type`/`source_uri` 字段名（值不变，仅所属表改名）、parser sidecar 全部（其中 `document` 是 python-docx 的类名，属误报）、`slides/`（讲稿，非产品代码）。

## 实施步骤（每步独立 commit，全部编译 + 相关测试通过）

开工前置：读 `docs/spec/worktree-workflow.md` → `git worktree add .worktrees/media-rename -b feat/media-rename origin/main` → `git status -sb` 确认。

### Commit 1 — refactor(domain): document 包更名为 media

- `internal/domain/document/` → `internal/domain/media/`（git mv 保留历史）
- `entity.go`：`Document` → `Media`；注释对齐
- `service.go`：`DocumentService` → `MediaService`，错误文案 document → media
- `repository.go`：`DocumentRepository` → `MediaRepository`；`StoredChunk.DocumentBizID` → `MediaBizID`
- `errors.go`、`chunker.go`、`chunker_test.go`：包名与注释
- 验证：`go build ./... && go test ./internal/domain/...`（此时其他包仍引用旧包名，build 会失败——本步骤允许仓库中间态？**不允许**：先在本步骤内加兼容不可能。调整顺序：Commit 1 同时改所有 import 方，即 domain 改名 + 全仓库 `internal/domain/document` → `internal/domain/media` 的 import 路径替换（goimports 机械替换），类型名留待下一步。**最终顺序以"每步可编译"为准，见下方修正。**）

> **修正后的提交切片**（保证每步 `go build ./...` 通过）：
>
> 1. **refactor(domain): 包路径 document → media**——`git mv` 目录 + 全仓库 import 路径机械替换（`document.Xxx` 限定符不变，仅包路径变）。验证：`go build ./... && go test ./...`。
> 2. **refactor(domain): 类型与标识符改名**——`Document`→`Media`、`DocumentService`→`MediaService`、`DocumentRepository`→`MediaRepository`、方法名、`StoredChunk.DocumentBizID`→`MediaBizID`、`ErrNotFound`/`ErrDeleting`/`ErrNotFailed` 等。仓库内 `go doc` 全绿。验证：`go build ./... && go test ./...`（测试一并改名）。
> 3. **refactor(db): Schema v2（medias 表）+ migrate 守卫收紧**——`migrations.sql` 建表改名；`db.go`：`schemaVersion = 1 → 2`，守卫改为 `version != 0 && version != schemaVersion` 时报"delete the db file and restart"，`hasLegacyTables` 的表名清单 `documents` → `medias`；`dao/document_dao.go` → `dao/media_dao.go`（SQL 列名、索引名全改）、`db/document_repository.go` 同步。验证：`go test ./internal/infrastructure/db/...`（注意 `db_test.go`、`document_repository_test.go`、`knowledgebase_repository_test.go`、`conversation_repository_test.go` 里的手工 SQL 与断言）。
> 4. **refactor(app): 用例层与 port 改名**——`application/ingest`（job 类型常量、`HandleParseDocument`→`HandleParseMedia`、payload 键）、`application/knowledgebase`（`doc_count`→`media_count`、URL 摄取）、`application/chat`（`ChunkDoc.DocumentBizID`→`MediaBizID`、`Citation` 字段、检索 filter 字符串 `"document_biz_id = ..."`→`"media_biz_id = ..."`）、`application/port/search.go`、`internal/domain/conversation/entity.go`（`Citation` JSON 键）。验证：`go test ./internal/application/... ./internal/domain/...`。
> 5. **refactor(meili): 检索引擎字段与 mock**——`infrastructure/meili/client.go` `filterableAttributes`；`cmd/dev/mock-meili/main.go` 的 `document_biz_id` 字段断言。验证：`go test ./internal/infrastructure/meili/...`。
> 6. **refactor(http): 路由 /documents → /medias**——`documents.go`→`medias.go`、`documentsHandler`→`mediasHandler`、`router.go`、全部 handler 测试与 DTO 字段（`document_biz_id`→`media_biz_id` 的响应体键）。验证：`go test ./internal/interfaces/...`。
> 7. **refactor(cmd): reindex 工具**——`cmd/reindex/main.go` + `main_test.go`（SQL 表名、payload 键）。验证：`go test ./cmd/...`。
> 8. **refactor(web): 前端改名**——`web/src/types.ts`（`Document`→`Media`、`document_biz_id`→`media_biz_id`、`doc_count`→`media_count`）、`api.ts`（路径与函数名）、`App.tsx`（组件/变量/Tab、`DocumentRow`→`MediaRow`、`DocumentsView`→`MediasView`，CSS 类名 `.documents-view` 等同步 `styles.css`）、`App.test.tsx`、`api.test.ts`、`web/scripts/mock-api.mjs`。验证：`cd web && npm run lint && npm run test && npm run build`。
> 9. **refactor(scripts): e2e 与 smoke**——`scripts/business_e2e.py`（路径、`document_biz_id` 键、变量名）；`scripts/smoke.sh` 无需改（仅文案）。验证：`./scripts/smoke.sh`。
> 10. **docs: 设计文档与 README 同步**——`docs/design/2026-09-26-open-ima-v1-design.md`（5.1 Schema、§3.2、§6 流程、§9 API 一览、修订说明加 v1.2 条目）；`README.md` 中 API/概念提及。`docs/plans/completed/` 历史文档**不回改**。
> 11. **merge 回 main**：`./scripts/harness.sh` 全绿 → `git merge feat/media-rename` → 清理 worktree → 归档本计划到 `docs/plans/completed/`。

### 每步验证基线

- Go：`go build ./... && go test ./...`（相关范围）
- 前端（步骤 8）：`npm run lint && npm run test && npm run build`
- 终验（步骤 11 前）：`./scripts/harness.sh`、`./scripts/smoke.sh`、`./scripts/business_e2e.py`（真实 Meilisearch + Ollama）、前端手动验收（上传/删除/重试、搜索高亮、问答引用跳转）

## 关键信息

- worktree：`.worktrees/media-rename`，分支 `feat/media-rename`，基线 `origin/main`
- 改名核对清单：`grep -ril document internal/ web/src scripts/ cmd/ --include='*.go' --include='*.ts*' --include='*.py'`（完成后应只剩 parser 的 python-docx 类名与历史文档）
- Schema：`internal/infrastructure/db/db.go` `schemaVersion`，`internal/infrastructure/db/migrations.sql`
- 运行中的旧数据：`./data/open-ima.db` 在 Schema v2 下会报"delete the db file and restart"——验证前需 `rm -rf ./data`（本地测试数据，可重建）

## 下一步

评审确认后：读 `docs/spec/worktree-workflow.md` → 建 worktree → 按 Commit 1–10 顺序执行，每步跑基线验证再 commit。

## 注意事项

- `document` 高频出现于注释/测试夹具，`python-docx` 的 `Document` 类与 `word/document.xml` 是**误报**，parser 目录一律不动。
- `messages.citations` 是 JSON 文本列，`document_biz_id` 键在 Go（`conversation.Citation`）、前端（`types.ts`）、e2e（`business_e2e.py`）三处独立定义，漏改会导致引用卡片拿不到标题。
- job payload 的 `document_biz_id` 键在 `cmd/reindex/main.go` 与 `application/ingest` 两处读写，必须同改，否则 reindex 入队的任务解析不到媒体 ID。
- 步骤 3 的 migrate 守卫收紧是**行为变更**：任何非当前版本库（含 v1）都拒绝启动，验证脚本若复用旧 `./data` 会失败，记得清理。
- mock-meili 对 `document_biz_id` 有硬编码断言，smoke 依赖它，属步骤 5 的必改项。
- 不要 `rg -i document` 全局 sed：先按本计划的文件清单逐文件改，完成后用核对清单复查。
