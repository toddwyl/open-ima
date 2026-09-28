# open-ima 技术方案(V1)

> **修订说明(v1.1,2026-09-27)**:本文按当前实现校准。与初稿的主要差异:
>
> - Go 侧从平面包(`internal/kb|media|upload|rag|storage`)落地为 **DDD 分层**(`domain / application+port / infrastructure / interfaces / app`),依赖方向由 `internal/app/architecture_test.go` 固化;
> - 新增 **应用设置域**(`app_settings` 表 + `GET/PUT /api/settings`),支持运行时热切换聊天模型与更新 embedder 配置;
> - SQLite 演进为 **Schema v2**:自增物理主键 + `<entity>_biz_id` 业务键;
> - Meilisearch 文档字段为 `kb_biz_id` / `media_biz_id`,embedder 带 `documentTemplate`;
> - 分块器按**字符**切分(512/80),chunk 检索内容带标题路径上下文(非初稿的 tiktoken 500 token 方案);
> - LLM 支持 **openai / anthropic 双协议**;
> - 新增确定性 mock(`cmd/dev/mock-meili`、`cmd/dev/mock-model`)支撑默认 smoke;
> - 前端新增知识库概览(HomeDeck)与配置中心。
>
> **修订说明(v1.2,2026-09-27)**:对标腾讯设计统一命名——`document` 概念整体更名为 **`media`**:
>
> - 表 `documents` → `medias`,`document_biz_id` → `media_biz_id`(SQLite / Meilisearch 字段 / API JSON 键 / 前端类型全链路);代码 `user_version` 1 → 2,守卫收紧为"非当前版本库一律拒绝启动";
> - Go 包 `internal/domain/document` → `internal/domain/media`,`Document/DocumentService/DocumentRepository` → `Media/MediaService/MediaRepository`;
> - API 路由 `/api/kbs/:id/documents`、`/api/documents/:id` → `/api/kbs/:id/medias`、`/api/medias/:id`;job 类型 `parse_document`/`delete_document` → `parse_media`/`delete_media`;KB 列表 `doc_count` → `media_count`;
> - `chunks` 表、Meili index 名、parser 协议中的 Block 概念保持不变。历史计划文档(completed/)不回改。

> 复刻腾讯 ima 知识库的本地部署版。参考:
> - 文章 1:《腾讯 AI 智能工作台 IMA 的知识库后端系统从 0 到 1 架构实践》(cloud.tencent.com/developer/article/2608466)
> - 文章 2:《腾讯 ima AI 知识库 Elasticsearch 检索实践》(developer.cloud.tencent.com/article/2747411)
>
> 定位:**单机本地部署**。Embedding 由 Meilisearch 调用本地 Ollama `bge-m3`;LLM 使用可配置的 OpenAI 兼容 API。第一版优先简洁,但架构分层对标两篇文章,保留向生产级演进的空间。

---

## 1. 设计目标与范围

### V1 功能范围

- **多知识库管理**:创建、删除、查看知识库
- **知识入库**:本地文件上传(PDF / DOCX / PPTX / Markdown / TXT / HTML)+ 网页链接收录(静态抓取,不做 JS 渲染)
- **异步解析入库**:文档 → 解析 → 分块 → embedding → 索引,全程异步任务驱动
- **RAG 智能问答**:基于单知识库的对话式问答,SSE 流式输出,答案带引用来源(可点击定位)
- **内容搜索**:知识库内全文/混合检索,带高亮

### 明确不做(V2 及以后)

- ima 原生内容(智能笔记)及其入库
- 多用户 / 权限体系(本地单用户)
- 扫描件 OCR、复杂版面分析(预留 docling 升级口)
- JS 渲染网页抓取、音视频的解析
- 分布式、多租户路由、消息队列等生产级组件

### 文章架构思想的本地化映射

| 文章做法(千万级租户) | 本地 V1 做法 |
|---|---|
| Media / Chunk 两层数据模型 | `medias` / `chunks` 两层模型,解耦文件管理与检索单元 |
| 统一接入层:知识库 / 媒体中心 / 文件上传管理服务 | Go 内部三个独立包:`internal/kb`、`internal/media`、`internal/upload`,职责与文章一致 |
| 独立解析层(媒体解析 + 解析基础能力) | Python parser sidecar,内部 parser 注册表按类型路由,HTTP 接口可插拔 |
| COS 对象存储 | `internal/storage` 接口(Put/Get/Delete),V1 实现为本地目录,签名对齐对象存储语义 |
| 消息队列异步削峰 | SQLite 任务表 + Go worker pool(状态机 + 指数退避重试),零外部 MQ |
| 原子/聚合服务 + 异步对账 | 删除走补偿式清理(Meili → 文件 → DB);media 状态机驱动失败重试 |
| ES 双路召回 + RRF + tenant routing | Meilisearch hybrid search 原生 RRF,`kb_biz_id` filterable 做库级隔离 |
| Query 改写(多 query 扩展) | LLM 生成 1 条扩展 query,与原 query 合并召回 |

---

## 2. 总体架构

四个进程,通过 `docker compose -f deploy/docker/compose.yml up` 一键启动,数据全部落在本地 `./data` 卷。

![Open IMA 架构图](assets/architecture.svg)

> 初稿曾按文章的组件边界设计 `internal/kb|media|upload|rag|storage` 平面包,实现落地为 DDD 分层(见第 3 节),"统一接入层"三个组件的职责由 application 层各用例承接,边界保持一一对应。

### 进程组成

| 进程 | 技术 | 职责 |
|---|---|---|
| `app` | Go 1.26(内嵌托管 React 构建产物) | REST API + SSE、用例编排、异步 worker、对账 ticker |
| `parser` | Python 3.11 + FastAPI(轻量库,无模型权重) | 媒体解析,按类型路由到具体解析器 |
| `meilisearch` | Meilisearch v1.x(官方镜像) | 全文 BM25 + 向量 KNN + RRF 融合检索 |
| 数据 | SQLite(内嵌于 app)+ `./data` 目录卷 | 元数据、任务队列、原始文件 |

### 技术选型理由(关键决策记录)

- **检索引擎 = Meilisearch + SQLite**,而非 ES 或纯 SQLite:
  - ES 单节点 JVM 过重(镜像 600MB+、内存 1GB 起),违背本地简洁部署;Meilisearch 单二进制(镜像 ~150MB、常驻 ~100-200MB)、秒启动,且**官方原生 hybrid search(q + vector + RRF)**,最贴文章 2"一套引擎承载全文、向量与混合检索"的思想。
  - 纯 SQLite(FTS5 + sqlite-vec)对中文有硬伤:FTS5 内置 `unicode61` 分词器对中文基本不可用,jieba 预分词方案召回质量明显低;且 sqlite-vec 加载需要 CGO。Meilisearch **内置 jieba 中文分词**,开箱可用。
- **解析 = Python sidecar 而非 Go 库**:PDF/DOCX/PPTX 解析的 Go 生态质量不达标;Python 生态(pypdf / python-docx / python-pptx / bs4)成熟。sidecar 用轻量库、**不下载任何模型权重**,镜像小、秒启动;接口预留,后续可换 docling 不动主流程。
- **元数据 = SQLite(纯 Go 驱动 `modernc.org/sqlite`)**:零 CGO、单文件、单机场景足够,免去 PostgreSQL 运维。

---

## 3. 分层架构(Go 内部包设计)

实现采用经典 DDD 分层,而非初稿设想的 `internal/kb|media|upload|rag|storage` 平面包。依赖方向由 `internal/app/architecture_test.go` 固化并回归守护:

```
interfaces/http ──▶ application ──▶ domain ◀── infrastructure
                      │  ▲                         (实现 application/port)
                      ▼  │
                   application/port(外部能力接口)
                      ▲
                   app(唯一装配根,可依赖所有层)
```

- **`internal/pkg`**:仅依赖标准库(如 `idgen`)。
- **`internal/domain`**:实体、仓储契约与领域服务,只依赖标准库、`pkg` 与同层包。四个聚合:`knowledgebase`、`document`(含状态机与 `Chunker` 分块策略)、`conversation`、`settings`。
- **`internal/application`**:用例编排(`knowledgebase`、`ingest`、`chat`、`settings`),只依赖 `domain` 与 `application/port`。端口统一定义外部能力:`Queue`/`JobRegistrar`、`FileStore`、`Parser`、`Indexer`/`Searcher`/`SearchAdmin`、`ChatModel`、`Fetcher`。
- **`internal/infrastructure`**:技术实现——`db`(+ `db/dao` 行级 SQL,仅标准库)、`queue`(SQLite 任务队列与 worker)、`meili`、`parser`(HTTP client)、`llm`(openai/anthropic 双协议)、`fetch`、`storage`、`config`。
- **`internal/interfaces/http`**:Go 1.22 `net/http` 路由与编解码,按 handler 分文件。
- **`internal/app`**:唯一装配根,负责配置加载 → 设置覆盖 → 组件构建 → 路由挂载。

初稿接入层组件在实现中的落点:

| 初稿组件 | 实现落点 |
| --- | --- |
| `internal/kb` 知识库服务 | `application/knowledgebase`(CRUD、级联删除、URL 摄取)+ `domain/knowledgebase` |
| `internal/upload` 上传管理 | `interfaces/http/medias.go`(multipart、50MB 上限)+ `application/knowledgebase` 的 SHA-256 去重入库 |
| `internal/media` 媒体中心 | `domain/document`(生命周期状态机)+ `application/ingest`(解析→分块→索引编排)+ `infrastructure/queue`(任务表 + worker pool) |
| `internal/storage` | `infrastructure/storage`,`port.FileStore` 的本地实现,兼作内部文件 HTTP 端点(带随机 secret) |
| `internal/rag` RAG 服务 | `application/chat`(改写/召回/RRF 融合/流式生成)+ `domain/conversation` |
| (初稿没有) 应用设置 | `domain/settings` + `application/settings` + SQLite `app_settings` 键值表,见 3.6 |

### 3.1 `application/knowledgebase` — 知识库用例

- 知识库 CRUD;删除为级联:逐文档走删除流水线 → 删会话与消息 → 删知识库
- `IngestURL`:校验 http(s) → `fetch.Fetcher` 抓取(10s 超时、10MB 上限)→ 内容 SHA-256 落盘为 html 文件 → 经 ingest 登记为 `file_type=html` 的 document
- 与初稿不同:URL 页面由 Go 侧抓取落盘(保持 parser 只解析、不触网的边界),parser 仍通过 app 内部 URL 拉取文件字节

### 3.2 `application/ingest` — 摄取流水线(媒体中心核心)

- `CreateMedia`:kb 存在性校验 → 建 document(pending,同库同 hash 唯一索引去重,重复时直接返回既有文档)→ 投递 `parse_media` job,立即返回
- `HandleParseMedia`:parsing → 调 parser sidecar 取结构化 blocks → chunking(按字符 512/80 切分,检索内容 = 标题路径上下文 + 正文)→ indexing(先按 `media_biz_id` 清旧 chunk,再批量写 Meili)→ `ready`
- `HandleDeleteMedia`:补偿式删除 Meili chunk → storage 文件 → medias 行
- `HandleReconcile`:扫描卡在 `deleting` 的文档,重新投递删除任务
- 失败语义:可重试错误按 job 策略指数退避(1m/5m/15m,共 3 次);`port.FatalError`(如解析无内容、坏 payload)直接置 failed 不重试

### 3.3 `infrastructure/queue` — 内嵌任务队列

- `jobs` 表(INTEGER 自增主键)+ 事务内 `Claim`(抢任务改 running)+ `run_at` 退避重排
- `Worker`:固定并发 goroutine(默认 4),轮询间隔 1s;无心跳,由每个 worker 每 5min 调用 `ResetStale` 把超时 10min 仍 running 的任务重置为 pending
- 处理器经 `port.JobRegistrar` 注册,ingest 用例在装配时挂载

### 3.4 `infrastructure/storage` — 存储抽象

`port.FileStore` 接口(`Put/Get/Delete/URL/Handler`),`LocalStorage` 实现根目录 `data/files`;`URL()` 生成带随机 secret 的内部地址,parser 凭此拉文件——**parser 不共享文件系统**。

### 3.5 `application/chat` — 检索与问答

- `Search`:单库检索,`mode=hybrid(默认)|text`,filter `kb_biz_id = '...'`,limit 8,带回高亮
- `Chat`:改写(用最近 4 条历史,失败静默跳过原 query 单路召回)→ 原 query + 改写 query 双路 hybrid 召回 → Go 侧 RRF(`1/(60+rank+1)`)融合去重取 Top-8 → system prompt 编号引用 + 最近 10 条历史 → LLM SSE 流式 → 落库(流结束后随 assistant 消息保存 citations)
- `SetModel`:`settings` 用例的热切换回调,替换 `port.ChatModel` 实现

### 3.6 `domain/settings` + `application/settings` — 应用设置(初稿之外的新增)

- 配置三层覆盖:**启动配置(yaml/默认值)→ `IMA_` 环境变量 → SQLite `app_settings` 持久化设置**(运行时 PUT 写入,重启后仍生效)
- 可调项:LLM 协议(openai/anthropic)、base_url、api_key(写出时脱敏为 `api_key_configured`,支持清除)、model,以及 embedder URL/model/dimensions
- `Update` 顺序:校验 → 对 Meili 重新 `EnsureIndex`(应用 embedder)→ 持久化 → 热切换聊天模型;任一步失败不落库
- 前端配置中心提供协议切换与表单编辑

---

## 4. 独立解析层(parser sidecar)

### 接口

```
POST /parse
{ "file_url": "http://app:8080/internal/files/<token>", "file_type": "pdf" }
→ 200 { "title": "...", "blocks": [ { "type": "heading"|"paragraph"|"table"|"list", "text": "..." } ] }
→ 422 { "error": "unsupported/encrypted/corrupted ..." }
```

返回**结构化 block 序列**(带类型),让 Go 侧分块器能按标题/段落边界智能切分,而不是返回一坨纯文本。

### 解析基础能力(插件注册表)

| file_type | 库 | 说明 |
|---|---|---|
| pdf | pypdf | 文本型 PDF;加密/扫描件返回明确错误 |
| docx | python-docx | 段落 + 标题层级 |
| pptx | python-pptx | 按页提取文本框 |
| md / txt | 原生 + markdown-it | md 按标题结构解析 |
| html / url | beautifulsoup4 + readability-lxml | 正文提取,去导航/页脚 |

注册表模式:`PARSERS: dict[str, Parser]`,新增类型 = 注册新 parser,不动路由代码。docling 升级路径:实现同一 `Parser` 协议即可替换。

---

## 5. 数据模型

### 5.1 SQLite(元数据,`data/open-ima.db`,Schema v2)

v2 的关键变化:每表以 **`id INTEGER PRIMARY KEY AUTOINCREMENT` 为物理主键**,另设 `<实体>_biz_id TEXT NOT NULL UNIQUE` 业务键;外键列引用业务键并与其同名列对应(如 `medias.kb_biz_id → knowledge_bases.kb_biz_id`)。与 Meilisearch 文档 id 对齐的是 `chunk_biz_id`。版本由 `db.Open` 经 `PRAGMA user_version` 守卫;不做数据迁移,旧版库直接报错要求删库重建。`db` 单连接(`SetMaxOpenConns(1)`)+ WAL + busy_timeout,规避 `database is locked`。

```sql
CREATE TABLE knowledge_bases (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kb_biz_id   TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE medias (                    -- 对标文章 Media
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    media_biz_id TEXT NOT NULL UNIQUE,
    kb_biz_id       TEXT NOT NULL REFERENCES knowledge_bases(kb_biz_id),
    title           TEXT NOT NULL,
    source_type     TEXT NOT NULL,          -- file | url
    source_uri      TEXT NOT NULL,          -- storage key(抓取 URL 也落盘,故统一为 key)
    file_type       TEXT NOT NULL,          -- pdf | docx | pptx | md | txt | html
    file_hash       TEXT NOT NULL DEFAULT '', -- sha256,url 源为抓取内容 hash
    status          TEXT NOT NULL DEFAULT 'pending',
                    -- pending|parsing|chunking|indexing|ready|failed|deleting
    error           TEXT NOT NULL DEFAULT '',
    chunk_count     INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_medias_kb ON medias(kb_biz_id, status);
CREATE UNIQUE INDEX uq_medias_hash ON medias(kb_biz_id, file_hash)
    WHERE file_hash != '';

CREATE TABLE chunks (                       -- 对标文章 Chunk(仅管理元数据)
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    chunk_biz_id    TEXT NOT NULL UNIQUE,   -- 与 Meilisearch 文档 id 一致
    media_biz_id TEXT NOT NULL REFERENCES medias(media_biz_id),
    kb_biz_id       TEXT NOT NULL,
    seq             INTEGER NOT NULL,       -- 在文档内的顺序
    token_count     INTEGER NOT NULL DEFAULT 0  -- 实为 rune 计数(见 6.1)
);
CREATE INDEX idx_chunks_doc ON chunks(media_biz_id);

CREATE TABLE jobs (                         -- 内嵌任务队列(INTEGER 自增主键)
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT NOT NULL,              -- parse_media | delete_media | reconcile
    payload     TEXT NOT NULL,              -- JSON
    status      TEXT NOT NULL DEFAULT 'pending', -- pending|running|done|failed
    retry_count INTEGER NOT NULL DEFAULT 0,
    run_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_jobs_poll ON jobs(status, run_at);

CREATE TABLE conversations (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_biz_id TEXT NOT NULL UNIQUE,
    kb_biz_id           TEXT NOT NULL REFERENCES knowledge_bases(kb_biz_id),
    title               TEXT NOT NULL DEFAULT '',
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE messages (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    message_biz_id      TEXT NOT NULL UNIQUE,
    conversation_biz_id TEXT NOT NULL REFERENCES conversations(conversation_biz_id),
    role                TEXT NOT NULL,      -- user | assistant
    content             TEXT NOT NULL,
    citations           TEXT NOT NULL DEFAULT '[]', -- JSON:[{document_id,title,chunk_id,snippet,score}]
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE app_settings (                 -- 运行时设置(见 3.6)
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key        TEXT NOT NULL UNIQUE,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**职责划分**:chunk 正文与向量只存 Meilisearch;SQLite 的 `chunks` 表仅存管理元数据(归属、顺序),用于删除对账与引用定位——与文章 1"Media 面向管理、Chunk 面向检索"的分工一致。

### 5.2 Meilisearch(检索)

单 index `chunks`(对标文章 2 单引擎承载,`kb_biz_id` 过滤等价于其 tenant routing):

```jsonc
{
  "primaryKey": "id",                    // 与 SQLite chunks.chunk_biz_id 相同
  // 文档字段
  "id": "chunk-uuid",
  "kb_biz_id": "kb-uuid",                // filterable,库级隔离(等价 tenant routing)
  "media_biz_id": "doc-uuid",         // filterable,删除/重建按此清理
  "title": "文档标题",                    // searchable + displayed
  "content": "标题路径上下文 + chunk 正文" // searchable + displayed;向量由 Meili 生成
}
```

Settings(`EnsureIndex` 每次启动与设置变更时幂等应用):

- `filterableAttributes`: `kb_biz_id`, `media_biz_id`
- `searchableAttributes`: `title`, `content`
- `embedders.default`: `source: ollama`,`url` 指向本地 Ollama `/api/embeddings`,`model: bge-m3`,`dimensions: 1024`,`documentTemplate: "{{doc.title}}\n{{doc.content}}"`(标题参与向量化)
- 检索请求 hybrid:`semanticRatio: 0.5`,双 query 召回后由 Go 侧做 RRF 融合(见 6.3)

索引重建策略:embedding 模型变更时(维度变化)需重建索引,V1 提供 `scripts/reindex.sh`(扫 SQLite chunks → 从 Meili 读回正文不可用,因此**chunk 正文同时写入 Meili,重建时从原始 document 重新走 chunking+indexing job**)。

---

## 6. 核心流程

### 6.1 入库流程(异步,对标文章 1 知识入库)

```
文件上传:                          网页链接:
POST /api/kbs/:id/medias        POST /api/kbs/:id/medias:url
  → multipart 校验(≤50MB)/hash 去重    → 校验 http(s) → fetch 抓取(10s/10MB)
  → 落盘 storage                       → 内容 hash 落盘为 html 文件
  ↓                                ↓
ingest 建 document(pending) + job(parse_media),立即 202 返回
  ↓ worker 事务内 Claim(SELECT 最早已到 run_at 的 pending → running)
[parsing]  POST parser/parse(内部 URL) → blocks;失败→重试(指数退避 1m/5m/15m,≤3 次)→failed
  ↓
[chunking] Go 分块器:按字符(rune)512 / overlap 80,按标题层级维护上下文
           (检索内容 = "H1 > H2 > ..." 路径 + 正文);chunks 元数据写 SQLite
  ↓
[indexing] 先按 media_biz_id 删除 Meili 旧 chunk(保证重试幂等)
           → 批量 addDocuments → Meilisearch 调本地 Ollama 生成并保存向量
  ↓
document → ready,chunk_count 更新
```

- **削峰**:worker pool 固定并发 4,任务表自然缓冲上传洪峰(对标文章 1 MQ 的作用)
- **hash 去重**:同库同文件秒回,不重复解析
- **手动重试**:failed document 提供 `POST /api/medias/:id/retry`,按失败阶段续跑

### 6.2 删除流程(对标文章 1 聚合服务 + 删除补偿)

```
DELETE /api/medias/:id
  1. document.status → deleting(BeginDelete),立即返回
  2. job(delete_media) 补偿式执行:
     a. Meili: deleteDocuments(filter media_biz_id=?)
     b. storage: Delete(source_uri)
     c. SQLite: 删 medias 行(级联删 chunks)
  任一步失败 → job 按策略重试;主进程每 10min 投递 reconcile job,
  扫描仍卡在 deleting 的文档重新投递删除任务 → 删除补偿式最终一致
```

完整的 DB / storage / Meili 三方对账能力尚未实现,设计见
[Media 对账设计(V2 增量)](2026-09-28-media-reconciliation-design.md)。

### 6.3 RAG 问答流程(对标文章 2 检索链路)

```
POST /api/kbs/:id/chat  { conversation_id?, query }
  ↓ (SSE 流式响应)
1. Query 改写:LLM 结合最近 4 条历史生成 1 条独立检索 query,超时/失败则静默跳过
2. 召回:原 query(+改写 query)各自调 Meilisearch hybrid search
     { q, hybrid: { semanticRatio: 0.5, embedder: "default" },
       filter: "kb_biz_id = ?", limit: 8, attributesToHighlight: ["content"] }
   Go 侧 RRF 融合:score = Σ 1/(60+rank+1),去重排序 → Top-8
3. 生成:system prompt(编号来源 + "来源不足要明说")+ 最近 10 条历史 + 当前 query
   → 云端 LLM stream(OpenAI 或 Anthropic 协议)→ SSE 逐 token 推送
4. 流结束后落库:assistant 消息携 citations [{document_id, title, chunk_id, snippet, score}]
   (conversation 自动创建,首条 query 截断为 title)
```

**搜索接口**(对标文章 2 内容搜索):

```
GET /api/kbs/:id/search?q=&mode=hybrid|text
  → Meilisearch(q + 可选 vector),attributesToHighlight
  → 返回 [{document_id, title, snippet(带 <em> 高亮), score}]
```

---

## 7. 错误处理与可靠性

| 场景 | 策略 |
|---|---|
| parser 不可用 / 解析失败 | job 指数退避重试(1m/5m/15m,≤3 次)→ document failed,错误信息透传前端 |
| Ollama embedding/LLM 失败 | Meili 入库任务失败时按 job 策略重试;问答时依赖不可用 → SSE 返回明确错误事件,不吞错 |
| Meili 写入失败 | job 重试;document 不进 ready,不产生半拉子可检索状态 |
| worker 崩溃 | 无心跳;每个 worker 每 5min 把 running 超时 10min 的任务重置为 pending 重新派发 |
| 删除清理失败 | 对账 job 周期重试,保证 Meili/文件/DB 最终一致(对标文章 1 对账服务) |
| 加密/扫描件 PDF | parser 返回 422 + 可读错误,document 直接 failed(不重试) |

**不追求单机场景无意义的复杂度**:不做分布式锁、不做多副本;SQLite WAL 模式 + 单写者已足够。

---

## 8. 前端(React + Vite + TS + Tailwind)

| 页面 | 功能 |
|---|---|
| 知识库列表 | 卡片式列表、新建/删除(二次确认)、文档数统计;侧边栏底部进入配置中心 |
| 知识库详情 - 概览(HomeDeck) | 入库引导卡片,快捷跳转文档/问答/搜索 |
| 知识库详情 - 文档 Tab | 上传(拖拽 + 进度)、URL 收录、文档列表(状态轮询 3s:pending/.../ready/failed + 错误 tooltip + 重试按钮)、删除 |
| 知识库详情 - 问答 Tab | 对话列表 + 流式回答渲染(markdown)、引用卡片(点击展开 snippet)、新建对话 |
| 知识库详情 - 搜索 Tab | 搜索框 + mode 切换(hybrid/text)+ 高亮结果列表 |
| 配置中心 | 对话模型(协议切换 OpenAI/Anthropic、base_url、api_key 写入/清除、model)与本地检索引擎(embedder URL/模型/维度)的运行时配置 |

前端为单文件组件结构(`web/src/App.tsx` + `api.ts` / `types.ts`),构建产物由 Go `embed` 托管,同端口同域。

---

## 9. API 一览

```
POST   /api/kbs                              创建知识库
GET    /api/kbs                              列表
DELETE /api/kbs/:id                          删除(级联)
POST   /api/kbs/:id/medias                上传文件(multipart,202)
POST   /api/kbs/:id/medias:url            收录网页 {url}
GET    /api/kbs/:id/medias                文档列表(含 status)
DELETE /api/medias/:id                    删除文档
POST   /api/medias/:id/retry              失败重试
POST   /api/kbs/:id/chat                     RAG 问答(SSE)
GET    /api/kbs/:id/search?q=&mode=          内容搜索
GET    /api/kbs/:id/conversations            对话历史
GET    /api/conversations/:id/messages       消息列表
GET    /api/settings                         读取生效设置(api_key 脱敏)
PUT    /api/settings                         更新设置:校验 → 应用 embedder → 持久化 → 热切换模型
GET    /internal/files/{key}                 parser 拉文件(内部,带随机 secret)
GET    /health                               健康检查
```

---

## 10. 配置与部署

### 配置(三层覆盖:yaml → 环境变量 → 运行时持久化设置)

1. **启动配置**:`IMA_CONFIG` 指向的 yaml(可选,不填则用内置默认值);
2. **环境变量**:`IMA_` 前缀覆盖(`config.applyEnv`);
3. **运行时设置**:`app_settings` 表持久化的键值在装配时覆盖前两层,并可在运行期经 `PUT /api/settings` 热更新(见 3.6)。

```yaml
llm:
  protocol: openai       # openai 或 anthropic; IMA_LLM_PROTOCOL
  base_url: https://api.deepseek.com/v1
  api_key: ""            # IMA_LLM_API_KEY
  model: deepseek-chat
meili:
  url: http://localhost:7700
  api_key: ""            # IMA_MEILI_API_KEY
  index: chunks
  embedder_url: http://127.0.0.1:11434/api/embeddings
  embedder_model: bge-m3
  embedder_dimensions: 1024
parser:
  url: http://parser:8100
data_dir: ./data
worker:
  concurrency: 4
```

### 部署(docker compose)

```yaml
services:
  meilisearch:  # 官方镜像 v1.10,卷 ./data/meili
  parser:       # python:3.11-slim + FastAPI
  app:          # 多阶段构建:node 构建前端 → go build 单二进制,卷 ./data
                # 端口 8080 对外;依赖 meilisearch/parser healthcheck
  model-mock:   # profile: smoke,确定性 LLM mock(:8200)
```

一键:`docker compose -f deploy/docker/compose.yml up -d`,打开 `http://localhost:8080`。本地开发可 `./scripts/dev-up.sh` 分步启动依赖,或 `./scripts/start.sh` 一键拉起全栈(Meilisearch + Ollama + parser + app),SQLite/文件落在 `./data`。

### 目录规划(与实现对齐)

```
open-ima/
├── cmd/open-ima/          # Go 服务入口(HTTP + worker + 对账 ticker)
├── cmd/reindex/           # 全量重建索引工具
├── cmd/dev/mock-meili/    # 确定性 Meilisearch mock(smoke 默认)
├── cmd/dev/mock-model/    # 确定性 LLM mock(smoke 默认)
├── internal/pkg/          # 仅标准库的共享工具(idgen)
├── internal/domain/       # 实体、仓储契约与领域服务(knowledgebase/document/conversation/settings)
├── internal/application/  # 用例编排 + port(外部能力接口)
├── internal/infrastructure/ # config/db(+dao)/queue/meili/parser/llm/fetch/storage
├── internal/interfaces/   # HTTP 入站适配
├── internal/app/          # 唯一装配根(含 architecture_test 依赖守卫)
├── parser/                # Python sidecar(FastAPI,注册表式解析器)
├── web/                   # React 前端(embed 托管)
├── docs/                  # design / guide / spec / plans
├── deploy/docker/         # Compose、Dockerfile 与 ignore
├── scripts/               # harness.sh / smoke.sh / business_e2e.py / start.sh / dev-up.sh / reindex.sh
└── data/                  # 运行时数据(gitignore)
```

---

## 11. 测试策略

- **Go 单测**:`go test`,内存 SQLite;Meilisearch、LLM、parser 均以 HTTP mock 覆盖状态机/重试/分块器/召回组装等核心逻辑;`internal/app/architecture_test.go` 回归守护分层依赖方向
- **LLM 协议集成**:`TestKimiCompatibleProtocols` 用真实 key 显式验证 openai/anthropic 双协议,默认跳过,不进常规门禁
- **parser 测试**:`pytest`,样例文件(pdf/docx/pptx/md/html)解析结果快照断言
- **前端**:`vitest` 覆盖关键组件(上传状态轮询、引用卡片渲染)
- **门禁**:`scripts/harness.sh` = `git diff --check` + lint / typecheck / test / build,提交前必跑(模板契约)
- **Smoke(默认,确定性)**:`scripts/smoke.sh` 本地直接起 app + parser + `cmd/dev/mock-meili` + `cmd/dev/mock-model`,不依赖真实 Meilisearch / Ollama / LLM,完成 HTTP 端到端断言;`SMOKE_MEILI_BIN` 可切真实 Meilisearch
- **业务 E2E(真实依赖)**:`scripts/business_e2e.py` 使用真实 Meilisearch + 本地 Ollama embedding,覆盖知识库、文件与 URL 入库、失败重试、文本/混合检索、对话历史、删除清理、错误状态和内嵌 SPA

---

## 12. 演进路径(架构生命力,对标文章 1 结语)

| 方向 | 触发条件 | 演进动作 |
|---|---|---|
| 解析质量 | 复杂排版/扫描件需求 | parser sidecar 换 docling(实现同一 Parser 协议) |
| 笔记 | 用户需要 ima 原生内容 | 媒体中心新增 source_type=note,入库/检索链路零改动 |
| 存储上云 | 多机部署 | storage 换 S3/COS 实现;Meili → ES(检索层抽象已隔离) |
| 并发 | 多人共用 | 任务表 → Redis/MQ;worker 独立进程;加用户体系与权限网关(对标文章 1 权限建模) |
| 检索质量 | 召回不达标 | Query 改写扩到多 query、加 rerank 模型、按文档类型调 semanticRatio |
