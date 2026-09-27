# Media / Chunk 统一命名改造计划（对齐腾讯 ima 设计）

**状态**：待评审
**关联分支**：`feat/media-rename`（开发时创建 worktree）
**关联文档**：[设计文档](../design/2026-09-26-open-ima-v1-design.md)、参考文章 1《IMA 知识库从 0 到 1》（Media/Chunk 数据模型）、参考文章 2《ima 基于腾讯云 ES 的检索实践》

---

## 背景

代码落地时把腾讯设计的两层模型映射为 `documents` / `chunks`（设计文档 5.1 注释即"对标文章 Media"）。模型职责已与腾讯设计一致，差异集中在**命名与概念宽窄**：

- 腾讯 **Media** 是"用户添加的任意知识资产"的统一管理结构（20+ 种格式：文件、网页、笔记、音频）；代码的 **Document** 命名暗示"文档"，且字段 `file_type` / `file_hash` 与 url / 未来 note 源的语义不符。
- 腾讯 **Chunk** 是"RAG 系统内部流通的最小载体"，解析层产出后整体交给 RAG；代码的 Chunk 在领域层是瞬态对象，落库时拆成两半（元数据 → SQLite `chunks` 表，正文+向量 → Meilisearch）。职责划分一致，但一个逻辑 Chunk 横跨两个存储，命名统一后可消除歧义。
- **Block** 在腾讯设计里不存在（解析层内部产物，不对外）；代码的 `port.Block` 是 parser sidecar 与 Go chunker 之间的协议 DTO，不落库、不进索引，与腾讯"解析层内部完成解析切分"的划分兼容——chunker 留在 Go 侧领域层是有意的本地决策（设计文档 §4"parser 只解析"），本次**不动**。

目标：把 `document` 概念整体更名为 `media`，使代码、Schema、API、前端与腾讯设计的 Media/Chunk 两层模型逐字对齐，为演进路径中的 `source_type=note` 等扩展扫清命名障碍。

## 现状对比结论

| 维度 | 腾讯设计 | 当前代码 | 是否需改 |
| --- | --- | --- | --- |
| 用户侧统一管理结构 | Media（格式无关的知识资产） | `domain/document` + `documents` 表 | 改名：document → media |
| 生命周期状态机 | 以 Media 状态为准 + 异步对账 | `Document` 状态机 + `reconcile` job | 已对齐，仅随改名 |
| RAG 侧检索单元 | Chunk（带内容的一等单位） | 领域层瞬态 `Chunk` + SQLite 元数据行 + Meili 文档 | 逻辑等价，仅字段改名 |
| 解析中间产物 | 解析层内部，无对外概念 | `port.Block`（parser↔chunker 协议 DTO） | 不动 |
| 管理/检索职责划分 | Media 面向管理、Chunk 面向检索 | 同左（正文只在 Meili，SQLite 只存归属/顺序） | 已对齐 |

## 改造范围

Schema v3 策略与现有一致：**不做数据迁移，PRAGMA user_version 守卫，旧库报错要求删库重建**。

### 1. Go domain 层

- `internal/domain/document` → `internal/domain/media`（目录、包名、文件）
- `Document` → `Media`、`DocumentService` → `MediaService`、`DocumentRepository` → `MediaRepository`
- `KBBizID/Title/SourceType/SourceURI/FileType/FileHash` 字段保留，`chunker.go` 中 `Chunk`/`Block` 类型不动（避免与 Meili/SQLite 存储字段混淆，`Chunk` 名与腾讯一致，保留）
- `StoredChunk` 中 `DocumentBizID` → `MediaBizID`

### 2. Schema（SQLite）

- 表 `documents` → `medias`；`document_biz_id` → `media_biz_id`（含 `chunks`、`messages.citations` JSON 内的 `document_id` 键名统一评估：对外 API 一并改为 `media_id`）
- 索引 `idx_documents_kb`、`uq_documents_hash` 同步改名；`user_version` 3

### 3. Meilisearch

- 文档字段 `document_biz_id` → `media_biz_id`（`EnsureIndex` filterable、删除/重建 filter、chat 召回 filter）

### 4. application / infrastructure / interfaces

- `application/ingest`、`application/chat`、`application/knowledgebase` 中所有 `document` 引用与错误文案
- `infrastructure/db/dao` 行级 SQL、repository 实现
- HTTP 路由与 DTO：`/api/kbs/:id/documents`、`/api/documents/:id...` 评估是否改为 `/medias`（**决策点 ①**：对外 API 改名是 breaking change，本地单机无兼容负担，建议一并改齐）
- 错误类型 `document.ErrNotFound` 等

### 5. 前端

- `web/src/types.ts` / `api.ts` 中 Document 类型、API 路径、文档 Tab 组件文案

### 6. 文档与脚本

- `docs/design/2026-09-26-open-ima-v1-design.md` 5.1 数据模型、§3.2、API 一览同步修订
- `scripts/business_e2e.py`、`scripts/smoke.sh`（如涉及路径断言）、README

## 实施步骤（每步一个 commit，先建 worktree）

1. `git worktree add .worktrees/media-rename -b feat/media-rename origin/main`，`git status -sb` 确认。
2. 决策点确认：对外 API 路径是否 `/documents` → `/medias`（本计划默认改齐）。
3. domain 层改名（包、实体、仓储契约、服务、chunker 注释）。
4. Schema v3 + dao/repository 实现改名。
5. application 层（ingest / chat / knowledgebase）与 Meili 字段改名。
6. interfaces/http 路由与 DTO 改名。
7. 前端 types/api/组件改名。
8. 设计文档 + README + e2e 脚本同步。
9. 验证：`./scripts/harness.sh` 全通过；`./scripts/smoke.sh` 默认确定性 smoke 通过。
10. 合并回 main：`git merge feat/media-rename`，清理 worktree。

## 验证

- `./scripts/harness.sh`（lint / typecheck / test / build）
- `./scripts/smoke.sh`（确定性 mock 全链路：上传 → 入库 → 检索 → 问答 → 删除）
- `./scripts/business_e2e.py`（真实 Meilisearch + Ollama，覆盖改名后的 API 与字段）
- 手动验收：前端文档 Tab 上传/删除/重试、搜索高亮、问答引用跳转

## 关键信息

- worktree：`.worktrees/media-rename`，分支 `feat/media-rename`
- 改名主清单：`rg -l 'document|Document' internal/ web/src scripts/`（执行时逐项核对）
- Schema 版本：v2 → v3，`internal/infrastructure/db` 中 `user_version` 常量

## 下一步

评审本计划，确认决策点 ①（API 路径是否随改名）后按「实施步骤」开工；开工前重读 `docs/spec/worktree-workflow.md`。

## 注意事项

- `document` 是高频词，`rg document` 会命中大量注释与测试夹具，逐文件改，不要全局 sed。
- `messages.citations` 是 JSON 文本列，键名 `document_id` 在 Go 结构体、前端类型、 e2e 脚本三处定义，漏改会导致引用卡片拿不到标题。
- 不改 `chunks` 表名与 Meili index 名（仍叫 `chunks`，与腾讯 Chunk 一致）。
- Block 概念保留在 parser 协议内，不进入任何对外模型。
