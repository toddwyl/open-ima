# open-ima 技术方案(V1)

> 复刻腾讯 ima 知识库的本地部署版。参考:
> - 文章 1:《腾讯 AI 智能工作台 IMA 的知识库后端系统从 0 到 1 架构实践》(cloud.tencent.com/developer/article/2608466)
> - 文章 2:《腾讯 ima AI 知识库 Elasticsearch 检索实践》(developer.cloud.tencent.com/article/2747411)
>
> 定位:**单机本地部署**(数据不出本机),LLM 与 Embedding 走云端 OpenAI 兼容 API。第一版优先简洁,但架构分层对标两篇文章,保留向生产级演进的空间。

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
| Media / Chunk 两层数据模型 | `documents` / `chunks` 两层模型,解耦文件管理与检索单元 |
| 统一接入层:知识库 / 媒体中心 / 文件上传管理服务 | Go 内部三个独立包:`internal/kb`、`internal/media`、`internal/upload`,职责与文章一致 |
| 独立解析层(媒体解析 + 解析基础能力) | Python parser sidecar,内部 parser 注册表按类型路由,HTTP 接口可插拔 |
| COS 对象存储 | `internal/storage` 接口(Put/Get/Delete),V1 实现为本地目录,签名对齐对象存储语义 |
| 消息队列异步削峰 | SQLite 任务表 + Go worker pool(状态机 + 指数退避重试),零外部 MQ |
| 原子/聚合服务 + 异步对账 | 删除走补偿式清理(Meili → 文件 → DB);document 状态机驱动失败重试 |
| ES 双路召回 + RRF + tenant routing | Meilisearch hybrid search 原生 RRF,`kb_id` filterable 做库级隔离 |
| Query 改写(多 query 扩展) | LLM 生成 1 条扩展 query,与原 query 合并召回 |

---

## 2. 总体架构

四个进程,`docker compose up` 一键启动,数据全部落在本地 `./data` 卷。

```
  网页链接        ima原生内容(V2)          本地文件
     │                                      │
┌────┼────────────── 统一接入层 (Go) ────────┼──────────────┐
│    ▼                                      ▼              │
│ ┌─────────┐                      ┌──────────────────┐   │
│ │ 知识库服务│                      │ 文件上传管理服务   │   │
│ │ (KB管理/ │                      │ (校验/去重/落盘)  │   │
│ │  网页收录)│                      └───────┬──────────┘   │
│ └────┬────┘                              │              │
│      ▼                                   │              │
│ ┌─────────────────┐                      │              │
│ │    媒体中心      │◀─────────────────────┘              │
│ │ (Media统一模型/  │                                     │
│ │  生命周期/任务调度)│                                     │
│ └────┬────────────┘                                     │
└──────┼──────────────────────────────────────────────────┘
       │                    ┌──────────────────┐
┌──────┼──── 独立解析层 ─────┤                  ▼
│      ▼                    │           ┌────────────┐
│ ┌─────────────────┐       │           │ 本地对象存储 │
│ │    媒体解析      │       │           │ (data/files,│
│ │  ┌─────────────┐│       │           │  COS 接口化) │
│ │  │ 解析基础能力  ││       │           └────────────┘
│ │  │ PDF/DOC/PPT/││       │
│ │  │ HTML/其他   ││       │        (Python sidecar)
│ │  │ (插件注册表) ││       │
│ │  └─────────────┘│       │
│ └────┬────────────┘       │
└──────┼────────────────────┘
       ▼
┌─────────────┐
│   RAG 服务   │── Meilisearch(全文 + 向量 + RRF)
│ (召回/问答)  │── SQLite(元数据 + 任务队列)
└──────┬──────┘
       ▼
  云端 OpenAI 兼容 API(LLM 生成 + Embedding)
```

### 进程组成

| 进程 | 技术 | 职责 |
|---|---|---|
| `app` | Go 1.23+(内嵌托管 React 构建产物) | REST API + SSE、统一接入层、媒体中心、RAG 服务、异步 worker |
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

## 3. 统一接入层(Go 内部包设计)

单体二进制内按文章的组件边界拆包,接口先行,保证演进空间。

### 3.1 `internal/kb` — 知识库服务

- 知识库 CRUD
- 网页链接收录:抓取 URL(超时 10s、限大小 10MB)→ 生成 Media(source_type=url)→ 交媒体中心
- 知识库删除 → 级联触发媒体中心的批量删除流程

### 3.2 `internal/upload` — 文件上传管理服务

- multipart 上传,校验:类型白名单、单文件 ≤ 50MB
- 内容 hash(SHA-256)去重:同知识库内同 hash 文件直接复用已有 document,提示用户
- 通过 `internal/storage` 落盘,然后把文件引用交媒体中心建 Media

### 3.3 `internal/media` — 媒体中心(核心)

- **Media 统一模型**:所有入库源(文件、URL、未来的笔记)收敛为 `documents` 表记录,字段含 `source_type(file|url)`、`source_uri`(storage key 或原始 URL)
- **生命周期状态机**:`pending → parsing → chunking → indexing → ready | failed`,另有终态旁路 `deleting`(见删除流程)
  - 每次状态推进由 job 完成;失败进入 `failed` 并记录 `error`,支持手动重试(重置到失败前阶段)
- **任务调度**:入库各阶段产出 job 写入任务表;worker pool(默认 4 goroutine)消费
- **删除(对标文章 1 聚合服务)**:补偿式顺序清理——Meilisearch 删 chunk 文档 → storage 删原文件 → SQLite 删 documents/chunks/jobs 记录;任一步失败记录待清理项,由定期对账任务(每 10 分钟)重试,保证最终一致

### 3.4 `internal/storage` — 存储抽象

```go
type Storage interface {
    Put(ctx context.Context, key string, r io.Reader) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    URL(key string) string // 供 parser sidecar 拉取
}
```

V1 实现 `LocalStorage`(`data/files/<sha256前2位>/<sha256>`),`URL()` 返回 app 内部 HTTP 端点(带随机 token,仅容器网络内可达),parser 通过该 URL 拉文件——**parser 不共享文件系统**,保持独立解析层的进程边界。

### 3.5 `internal/rag` — RAG 服务

- 召回:query 改写 → embedding → Meilisearch hybrid search → Top-K
- 生成:prompt 组装 → 云端 LLM SSE 流式 → 引用收集
- 对话历史读写

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

### 5.1 SQLite(元数据,`data/open-ima.db`)

```sql
CREATE TABLE knowledge_bases (
    id          TEXT PRIMARY KEY,          -- uuid
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE documents (                    -- 对标文章 Media
    id          TEXT PRIMARY KEY,
    kb_id       TEXT NOT NULL REFERENCES knowledge_bases(id),
    title       TEXT NOT NULL,
    source_type TEXT NOT NULL,              -- file | url
    source_uri  TEXT NOT NULL,              -- storage key 或原始 URL
    file_type   TEXT NOT NULL,              -- pdf | docx | pptx | md | txt | html
    file_hash   TEXT NOT NULL DEFAULT '',   -- sha256,url 源为空
    status      TEXT NOT NULL DEFAULT 'pending',
                -- pending|parsing|chunking|indexing|ready|failed|deleting
    error       TEXT NOT NULL DEFAULT '',
    chunk_count INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_documents_kb ON documents(kb_id, status);
CREATE UNIQUE INDEX uq_documents_hash ON documents(kb_id, file_hash)
    WHERE file_hash != '';

CREATE TABLE chunks (                       -- 对标文章 Chunk(仅管理元数据)
    id          TEXT PRIMARY KEY,           -- 与 Meilisearch 文档 id 一致
    document_id TEXT NOT NULL REFERENCES documents(id),
    kb_id       TEXT NOT NULL,
    seq         INTEGER NOT NULL,           -- 在文档内的顺序
    token_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_chunks_doc ON chunks(document_id);

CREATE TABLE jobs (                         -- 内嵌任务队列
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,              -- parse_document | delete_document | reconcile
    payload     TEXT NOT NULL,              -- JSON
    status      TEXT NOT NULL DEFAULT 'pending', -- pending|running|done|failed
    retry_count INTEGER NOT NULL DEFAULT 0,
    run_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_jobs_poll ON jobs(status, run_at);

CREATE TABLE conversations (
    id          TEXT PRIMARY KEY,
    kb_id       TEXT NOT NULL REFERENCES knowledge_bases(id),
    title       TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    role            TEXT NOT NULL,          -- user | assistant
    content         TEXT NOT NULL,
    citations       TEXT NOT NULL DEFAULT '[]', -- JSON:[{document_id,title,chunk_id,snippet,score}]
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**职责划分**:chunk 正文与向量只存 Meilisearch;SQLite 的 `chunks` 表仅存管理元数据(归属、顺序),用于删除对账与引用定位——与文章 1"Media 面向管理、Chunk 面向检索"的分工一致。

### 5.2 Meilisearch(检索)

单 index `chunks`(对标文章 2 单引擎承载,`kb_id` 过滤等价于其 tenant routing):

```jsonc
{
  "primaryKey": "id",                    // 与 SQLite chunks.id 相同
  // 文档字段
  "id": "chunk-uuid",
  "kb_id": "kb-uuid",                    // filterable
  "document_id": "doc-uuid",             // filterable
  "title": "文档标题",                    // searchable + displayed
  "content": "chunk 正文",               // searchable + displayed
  "_vectors": { "default": [0.1, ...] }  // userProvided,由后端写入
}
```

Settings:

- `filterableAttributes`: `kb_id`, `document_id`
- `searchableAttributes`: `title`, `content`
- `embedders.default`: `source: userProvided`,`dimensions` 按所选 embedding 模型(默认 1024,随配置)

索引重建策略:embedding 模型变更时(维度变化)需重建索引,V1 提供 `scripts/reindex.sh`(扫 SQLite chunks → 从 Meili 读回正文不可用,因此**chunk 正文同时写入 Meili,重建时从原始 document 重新走 chunking+indexing job**)。

---

## 6. 核心流程

### 6.1 入库流程(异步,对标文章 1 知识入库)

```
文件上传:                          网页链接:
POST /api/kbs/:id/documents        POST /api/kbs/:id/documents:url
  → upload 校验/去重/落盘            → kb 服务抓取(URL 落盘为 html 文件)
  ↓                                ↓
media 建 document(pending) + job(parse_document),立即 202 返回
  ↓ worker 领取(job 状态 running,SELECT ... FOR UPDATE 语义)
[parsing]  POST parser/parse(file_url) → blocks;失败→重试(指数退避,≤3 次)→failed
  ↓
[chunking] Go 分块器:按标题/段落边界递归切分,目标 ~500 token,overlap 50
           (tiktoken-go 计数);产出 chunks 写 SQLite
  ↓
[indexing] 批量调云端 embedding(每批 ≤64 条,失败整批重试)
           → 正文+向量写 Meilisearch(批量 addDocuments)
  ↓
document → ready,chunk_count 更新
```

- **削峰**:worker pool 固定并发 4,任务表自然缓冲上传洪峰(对标文章 1 MQ 的作用)
- **hash 去重**:同库同文件秒回,不重复解析
- **手动重试**:failed document 提供 `POST /api/documents/:id/retry`,按失败阶段续跑

### 6.2 删除流程(对标文章 1 聚合服务 + 对账)

```
DELETE /api/documents/:id
  1. document.status → deleting,删 SQLite chunks 元数据(保留 documents 行直至清理完成)
  2. job(delete_document) 补偿式执行:
     a. Meili: deleteDocuments(filter document_id=?)
     b. storage: Delete(source_uri)
     c. SQLite: 删 documents 行
  任一步失败 → 记录 error,对账 job(每 10min)扫描重试 → 最终一致
```

### 6.3 RAG 问答流程(对标文章 2 检索链路)

```
POST /api/kbs/:id/chat  { conversation_id?, query }
  ↓ (SSE 流式响应)
1. Query 改写:LLM 生成 1 条扩展 query(结合最近 2 轮对话历史),超时/失败则静默跳过
2. 召回:embedding(query + 扩展 query)
   → Meilisearch hybrid search:
     { q, vector, hybrid: { semanticRatio: 0.5, embedder: "default" },
       filter: "kb_id = ?", limit: 8, attributesToHighlight: ["content"] }
   两个 query 各自召回后按 RRF 分数合并去重 → Top-8
3. 生成:system prompt + 编号引用格式([1][2]...) + 对话历史(近 5 轮)
   → 云端 LLM stream → SSE 逐 token 推送
4. 流末尾 SSE event: citations [{document_id, title, chunk_id, snippet, score}]
5. 对话落 SQLite(conversation 自动创建,首条 query 截断为 title)
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
| 云端 embedding/LLM 失败 | 同 job 重试策略;问答时 LLM 不可用 → SSE 返回明确错误事件,不吞错 |
| Meili 写入失败 | job 重试;document 不进 ready,不产生半拉子可检索状态 |
| worker 崩溃 | job `running` 超时(10min 未心跳)自动重置为 `pending` 重新派发 |
| 删除清理失败 | 对账 job 周期重试,保证 Meili/文件/DB 最终一致(对标文章 1 对账服务) |
| 加密/扫描件 PDF | parser 返回 422 + 可读错误,document 直接 failed(不重试) |

**不追求单机场景无意义的复杂度**:不做分布式锁、不做多副本;SQLite WAL 模式 + 单写者已足够。

---

## 8. 前端(React + Vite + TS + Tailwind)

单页应用,构建产物由 Go `embed` 托管,同端口同域。

| 页面 | 功能 |
|---|---|
| 知识库列表 | 卡片式列表、新建/删除(二次确认)、文档数统计 |
| 知识库详情 - 文档 Tab | 上传(拖拽 + 进度)、URL 收录、文档列表(状态轮询 3s:pending/.../ready/failed + 错误 tooltip + 重试按钮)、删除 |
| 知识库详情 - 问答 Tab | 对话列表 + 流式回答渲染(markdown)、引用卡片(点击展开 snippet)、新建对话 |
| 知识库详情 - 搜索 Tab | 搜索框 + mode 切换(hybrid/text)+ 高亮结果列表 |

---

## 9. API 一览

```
POST   /api/kbs                              创建知识库
GET    /api/kbs                              列表
DELETE /api/kbs/:id                          删除(级联)
POST   /api/kbs/:id/documents                上传文件(multipart,202)
POST   /api/kbs/:id/documents:url            收录网页 {url}
GET    /api/kbs/:id/documents                文档列表(含 status)
DELETE /api/documents/:id                    删除文档
POST   /api/documents/:id/retry              失败重试
POST   /api/kbs/:id/chat                     RAG 问答(SSE)
GET    /api/kbs/:id/search?q=&mode=          内容搜索
GET    /api/kbs/:id/conversations            对话历史
GET    /api/conversations/:id/messages       消息列表
GET    /internal/files/:token                parser 拉文件(内部)
```

---

## 10. 配置与部署

### 配置(`config.yaml` + 环境变量覆盖,前缀 `IMA_`)

```yaml
llm:
  base_url: https://api.deepseek.com/v1
  api_key: ""            # IMA_LLM_API_KEY
  model: deepseek-chat
embedding:
  base_url: https://api.openai.com/v1
  api_key: ""            # IMA_EMBEDDING_API_KEY
  model: text-embedding-3-small
  dimensions: 1024
parser:
  url: http://parser:8100
data_dir: ./data
worker:
  concurrency: 4
```

### 部署(docker compose)

```yaml
services:
  meilisearch:  # 官方镜像,卷 ./data/meili
  parser:       # python:3.11-slim + FastAPI,镜像目标 <200MB
  app:          # 多阶段构建:node 构建前端 → go build 单二进制,卷 ./data
                # 端口 8080 对外;依赖 meilisearch/parser healthcheck
```

一键:`docker compose up -d`,打开 `http://localhost:8080`。本地开发:`make dev`(分别热跑三个进程,SQLite/文件落在 ./data)。

### 目录规划

```
open-ima/
├── cmd/server/            # Go 入口
├── internal/{kb,upload,media,storage,rag,queue,config,llm,meili}/
├── parser/                # Python sidecar(FastAPI)
├── web/                   # React 前端
├── design/                # 技术方案(本文档)
├── scripts/               # harness.sh / smoke.sh / reindex.sh
├── docker-compose.yml
└── data/                  # 运行时数据(gitignore)
```

---

## 11. 测试策略

- **Go 单测**:`go test`,内存 SQLite;Meilisearch、LLM、embedding、parser 均为接口注入,mock 实现覆盖状态机/重试/分块器/召回组装等核心逻辑
- **parser 测试**:`pytest`,样例文件(pdf/docx/pptx/md/html)解析结果快照断言
- **前端**:`vitest` 覆盖关键组件(上传状态轮询、引用卡片渲染)
- **门禁**:`scripts/harness.sh` = `golangci-lint` + `go vet` + `go test` + `pytest` + `tsc` + `vitest`,提交前必跑(模板契约)
- **冒烟**:`scripts/smoke.sh`——compose 起栈 → 建库 → 上传 md → 轮询 ready → 提问断言回答非空且 citations 命中该文档

---

## 12. 演进路径(架构生命力,对标文章 1 结语)

| 方向 | 触发条件 | 演进动作 |
|---|---|---|
| 解析质量 | 复杂排版/扫描件需求 | parser sidecar 换 docling(实现同一 Parser 协议) |
| 笔记 | 用户需要 ima 原生内容 | 媒体中心新增 source_type=note,入库/检索链路零改动 |
| 存储上云 | 多机部署 | storage 换 S3/COS 实现;Meili → ES(检索层抽象已隔离) |
| 并发 | 多人共用 | 任务表 → Redis/MQ;worker 独立进程;加用户体系与权限网关(对标文章 1 权限建模) |
| 检索质量 | 召回不达标 | Query 改写扩到多 query、加 rerank 模型、按文档类型调 semanticRatio |
