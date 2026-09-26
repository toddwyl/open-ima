# P2: Go 骨架 + 知识入库链路 实现计划

**完成状态（2026-09-27）：** 10 个任务均已实现，Go 全量测试通过。项目内 `.local/bin/meilisearch` 保存官方 Meilisearch v1.10.3，`./scripts/smoke.sh` 已由脚本启动真实 Meilisearch、app、parser 和模型 double，验证 Markdown/PDF 上传、轮询 `ready` 与真实索引可检索。启动实测发现并修复了 vector store 未启用问题。

> 下方复选框是原始实施步骤，不作为完成状态记录；本节完成状态与最终验证记录为准。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 Go 单体后端的入库链路:配置/存储/SQLite/任务队列骨架 + 统一接入层(kb/upload/media)+ worker + Meilisearch 索引,实现"上传/收录 → 异步解析 → 分块 → embedding → 可检索"。

**Architecture:** 单 Go 二进制,stdlib `net/http` ServeMux 路由。统一接入层三个包(kb/upload/media)对标 spec §3;media 为状态机核心,异步阶段由 SQLite jobs 表 + worker pool 驱动;外部依赖(parser sidecar / Meilisearch / 云端 embedding)全部走裸 `net/http` client,测试以 httptest mock。

**Tech Stack:** Go 1.23+, modernc.org/sqlite(纯 Go), gopkg.in/yaml.v3, github.com/google/uuid(无其他依赖;分块按字符计数,不引 tiktoken)

**Spec:** [design/2026-09-26-open-ima-v1-design.md](../../../design/2026-09-26-open-ima-v1-design.md) §3、§5、§6.1、§6.2、§7、§9、§10

**Roadmap:** [2026-09-26-roadmap.md](2026-09-26-roadmap.md)

**参考实现:** chunker 递归切分/标题面包屑/保护区域与 worker 幂等照搬 WeKnora(`~/work/WeKnora/internal/infrastructure/chunker/splitter.go`、`internal/application/service/knowledge_process.go`),其生产验证参数 512/80 直接采用

**前置:** P1 已完成(parser sidecar 提供 `POST /parse` 契约,见 Roadmap;本计划测试全部 mock,不依赖 parser 真实运行)。

## Current Progress

- 2026-09-26: Task 1 已完成并提交 `36e0c11`(`chore(server): scaffold config, db migrations, httpx and /health main`)。
- 2026-09-26: Task 2 已完成并提交 `f8f41f7`(`feat(server): add storage abstraction with local impl and internal file endpoint`)。
- 2026-09-26: Task 3 已完成并提交 `3db1dfc`(`feat(server): add sqlite-backed job queue and worker pool`)。
- 2026-09-27: Task 4 已完成并提交 `90abfa4`(`feat(server): add recursive-separator chunker with heading breadcrumbs`)。
- 2026-09-27: Task 5 已完成并提交 `02c142b`(`feat(server): add meilisearch http client with task waiting`)。
- 2026-09-27: Task 6 已完成并提交 `f2f5272`(`feat(server): add parser sidecar and embedding http clients`)。
- 2026-09-27: Task 7 已完成并提交 `40c46fe`(`feat(server): add media center with parse pipeline, delete compensation and reconcile`)。
- 2026-09-27: Task 8 已完成并提交 `efbdb23`(`feat(server): add knowledge base service with url ingestion`)。
- 2026-09-27: Task 9 已完成并提交 `113ee30`(`feat(server): add multipart upload handler with hash dedup`)。
- 2026-09-27: Task 10 已完成并提交 `bf7d1c3`(`feat(server): wire routes, worker and reconcile ticker; add e2e ingestion test`)。
- P2 实现与 mock 外部依赖的端到端测试已完成；待 P5 真实 Meilisearch smoke 通过后一并归档。

## Global Constraints

- Go module 名 `open-ima`;路由只用 stdlib `net/http`(Go 1.22+ pattern),禁止 web 框架、禁止引入 SDK(Meilisearch/OpenAI 均裸 HTTP)
- SQLite 驱动 `modernc.org/sqlite`(禁 CGO);`CGO_ENABLED=0 go build ./...` 必须通过
- 测试一律 `:memory:` SQLite + `httptest.Server` mock 外部服务;不访问外网、不需要 docker
- `internal/db/migrations.sql` 使用 spec §5.1 的 DDL 原样(含 `deleting` 状态注释),不增删列
- API 路径与 spec §9 逐字一致;`POST /api/kbs/{id}/documents` 成功返回 **202**
- 参数固定值:chunk 目标 512 字符 / overlap 80 字符(字符≈token,照搬 WeKnora splitter 默认);worker 并发 4;job 重试 ≤3 次、退避 1m/5m/15m;running stale 阈值 10min;上传 ≤50MB;URL 抓取 ≤10MB、超时 10s;embedding 批 ≤64
- 每个 Task 结束提交一次,commit message 以 `feat(server):` 开头(Task 1 用 `chore(server):`)
- 配置在 spec §10 基础上扩展 `meili`、`http_addr`、`public_base_url` 三节(见 Task 1;已裁决的偏差,原因:spec 未给 Meilisearch 连接与 parser 回拉地址的配置位)

### 已裁决的 spec 偏差(执行时照此,勿再"修正"回 spec)

1. **Retry 全量重跑,不按阶段续跑**。spec §3.3 说"重置到失败前阶段";但 chunk 正文不落 SQLite(spec §5.1 职责划分),中间产物(blocks/chunk 文本)只存在于 job 执行内存中,阶段续跑无法拿到中间数据。改为:`Retry` 重置为 `pending` 全量重跑,流水线全程幂等(重跑前先删旧 chunk 行与旧 Meili 文档)。
2. **documents 表不加 stage 列**(承接上一条:无续跑即无 stage 记录需求),migrations.sql 与 spec §5.1 完全一致。
3. **配置扩展** `meili.url/api_key/index`、`http_addr`、`public_base_url`(parser 回拉文件的 app 对外地址,compose 中为 `http://app:8080`)。
4. **不启用 SQLite 外键约束**(默认 off,不加 `PRAGMA foreign_keys=ON`):知识库删除是"先删 KB 行 + 异步清理 documents",开启 FK 会阻塞该流程;单机单用户由应用层保证完整性。
5. **不可重试错误的语义**:parser 返回 422 → media 标记 document `failed` 并让 job 正常 `done`(错误已在 document 上,不浪费重试);其余错误走重试,重试耗尽时由 media 在最后一次尝试时标记 document `failed`。
6. **chunk 尺寸 512 字符 / overlap 80**(controller WeKnora 裁决):替代 spec §6.1 的 "~500 token / overlap 50 + tiktoken"。字符≈token(中文场景),参数经 WeKnora 生产验证,且免去 tiktoken 依赖。`chunks.token_count` 语义随之变为 rune 计数。
7. **URL 源文档同样计算内容 hash 并参与去重**:spec §5.1 DDL 注释说 "url 源为空",但 URL 抓取后已落盘为 html 文件,算 hash 零成本;同内容 URL 重复收录/与上传文件撞内容时去重是更好行为。`uq_documents_hash` 唯一索引对 URL 源同样生效。

## File Structure

```
go.mod
cmd/server/main.go                 # 装配入口:config→db→server→worker/ticker→ListenAndServe
internal/httpx/httpx.go            # JSON(w,status,v) / Error(w,status,msg) 响应辅助
internal/config/config.go          # Load(path):yaml 默认值 + 文件 + IMA_ 环境变量覆盖
internal/db/db.go                  # Open(path):WAL/busy_timeout/migrate;embed migrations.sql
internal/db/migrations.sql         # spec §5.1 DDL 原样
internal/storage/storage.go        # Storage 接口 + tokenFor()
internal/storage/local.go          # LocalStorage + Handler()(GET /internal/files/{key}?token=)
internal/queue/queue.go            # Queue:Enqueue/Claim/Done/Fail/FailPermanent/ResetStale + PermanentError
internal/queue/worker.go           # Worker:Register/Start/RunOnce
internal/chunker/chunker.go        # Chunker(递归分隔符 512/80、标题面包屑、保护 table/list)
internal/meili/client.go           # EnsureIndex/AddDocuments/DeleteByFilter/waitTask
internal/parserclient/client.go    # Parse(fileURL,fileType);422→FatalError
internal/llm/embedding.go          # EmbeddingClient.Embed(texts) 批≤64
internal/media/service.go          # CreateDocument/List/Get/Retry/Delete/EnqueueReconcile
internal/media/handlers.go         # HandleParseDocument/HandleDeleteDocument/HandleReconcile
internal/kb/service.go             # Create/List/Delete/IngestURL
internal/kb/handlers.go            # KB 与 URL 收录 HTTP handlers
internal/upload/handler.go         # multipart 上传 handler
internal/server/server.go          # 路由装配 + 全部 API handlers 接线
scripts/dev-up.sh                  # 本地依赖启动说明脚本
```

---

### Task 1: config + db + httpx + cmd/server(/health)

**Files:**
- Create: `go.mod`
- Create: `internal/httpx/httpx.go`, `internal/httpx/httpx_test.go`
- Create: `internal/config/config.go`, `internal/config/config_test.go`
- Create: `internal/db/db.go`, `internal/db/migrations.sql`, `internal/db/db_test.go`
- Create: `cmd/server/main.go`

**Interfaces:**
- Produces(全部后续 Task 依赖):
  - `httpx.JSON(w http.ResponseWriter, status int, v any)`;`httpx.Error(w http.ResponseWriter, status int, msg string)`(body `{"error": msg}`)
  - `config.Config` 结构(字段见下)与 `config.Load(path string) (*Config, error)`;`cfg.DBPath() string`(=`filepath.Join(DataDir, "open-ima.db")`)
  - `db.Open(path string) (*sql.DB, error)`:`path == ":memory:"` 时用 `file::memory:?cache=shared` 并 `SetMaxOpenConns(1)`;文件路径先 `os.MkdirAll` 父目录,开 WAL + `busy_timeout=5000`;随后执行内嵌 migrations(幂等,`IF NOT EXISTS`)

- [ ] **Step 1: 初始化 module 并写失败测试**

```bash
go mod init open-ima
go get modernc.org/sqlite@latest gopkg.in/yaml.v3@latest github.com/google/uuid@latest
```

`internal/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "./data" || cfg.HTTPAddr != ":8080" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Worker.Concurrency != 4 {
		t.Errorf("worker concurrency = %d, want 4", cfg.Worker.Concurrency)
	}
	if cfg.Meili.URL != "http://localhost:7700" || cfg.Meili.Index != "chunks" {
		t.Errorf("meili defaults: %+v", cfg.Meili)
	}
	if cfg.Parser.URL != "http://localhost:8100" {
		t.Errorf("parser url = %q", cfg.Parser.URL)
	}
	if cfg.Embedding.Dimensions != 1024 {
		t.Errorf("dimensions = %d", cfg.Embedding.Dimensions)
	}
	if cfg.PublicBaseURL != "http://localhost:8080" {
		t.Errorf("public base = %q", cfg.PublicBaseURL)
	}
}

func TestLoadYAMLAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "llm:\n  base_url: https://api.deepseek.com/v1\n  model: deepseek-chat\ndata_dir: /tmp/ima\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMA_LLM_API_KEY", "sk-test")
	t.Setenv("IMA_DATA_DIR", "/tmp/ima-env")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.BaseURL != "https://api.deepseek.com/v1" || cfg.LLM.Model != "deepseek-chat" {
		t.Errorf("yaml not applied: %+v", cfg.LLM)
	}
	if cfg.LLM.APIKey != "sk-test" {
		t.Errorf("env api key not applied")
	}
	if cfg.DataDir != "/tmp/ima-env" {
		t.Errorf("env should beat yaml: %q", cfg.DataDir)
	}
	if cfg.DBPath() != "/tmp/ima-env/open-ima.db" {
		t.Errorf("DBPath = %q", cfg.DBPath())
	}
}
```

`internal/db/db_test.go`:
```go
package db

import (
	"path/filepath"
	"testing"
)

func TestOpenMemoryCreatesTables(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, table := range []string{"knowledge_bases", "documents", "chunks", "jobs", "conversations", "messages"} {
		var name string
		err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestOpenFileIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "test.db")
	d1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d1.Close()
	d2, err := Open(path) // 二次打开重复 migrate 不报错
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	var mode string
	if err := d2.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %s, want wal", mode)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ ./internal/db/`
Expected: FAIL,`no required module provides package` / 编译错误(包不存在)

- [ ] **Step 3: 实现**

`internal/httpx/httpx.go`:
```go
// Package httpx 提供统一的 JSON 响应辅助。
package httpx

import (
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}
```

`internal/httpx/httpx_test.go`:
```go
package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, 400, "bad input")
	if rec.Code != 400 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"error\":\"bad input\"}\n" {
		t.Fatalf("body = %q", got)
	}
}
```

`internal/config/config.go`:
```go
// Package config 加载 config.yaml 并应用 IMA_ 前缀环境变量覆盖。
package config

import (
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

type LLMConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
}

type EmbeddingConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
}

type ParserConfig struct {
	URL string `yaml:"url"`
}

type MeiliConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
	Index  string `yaml:"index"`
}

type WorkerConfig struct {
	Concurrency int `yaml:"concurrency"`
}

type Config struct {
	LLM           LLMConfig       `yaml:"llm"`
	Embedding     EmbeddingConfig `yaml:"embedding"`
	Parser        ParserConfig    `yaml:"parser"`
	Meili         MeiliConfig     `yaml:"meili"`
	DataDir       string          `yaml:"data_dir"`
	Worker        WorkerConfig    `yaml:"worker"`
	HTTPAddr      string          `yaml:"http_addr"`
	PublicBaseURL string          `yaml:"public_base_url"`
}

func defaults() *Config {
	cfg := &Config{
		DataDir:       "./data",
		HTTPAddr:      ":8080",
		PublicBaseURL: "http://localhost:8080",
	}
	cfg.Worker.Concurrency = 4
	cfg.Meili.URL = "http://localhost:7700"
	cfg.Meili.Index = "chunks"
	cfg.Parser.URL = "http://localhost:8100"
	cfg.Embedding.Dimensions = 1024
	return cfg
}

func Load(path string) (*Config, error) {
	cfg := defaults()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}
	applyEnv(cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	setStr := func(dst *string, keys ...string) {
		for _, k := range keys {
			if v := os.Getenv(k); v != "" {
				*dst = v
			}
		}
	}
	setStr(&cfg.LLM.BaseURL, "IMA_LLM_BASE_URL")
	setStr(&cfg.LLM.APIKey, "IMA_LLM_API_KEY")
	setStr(&cfg.LLM.Model, "IMA_LLM_MODEL")
	setStr(&cfg.Embedding.BaseURL, "IMA_EMBEDDING_BASE_URL")
	setStr(&cfg.Embedding.APIKey, "IMA_EMBEDDING_API_KEY")
	setStr(&cfg.Embedding.Model, "IMA_EMBEDDING_MODEL")
	if v := os.Getenv("IMA_EMBEDDING_DIMENSIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Embedding.Dimensions = n
		}
	}
	setStr(&cfg.Parser.URL, "IMA_PARSER_URL")
	setStr(&cfg.Meili.URL, "IMA_MEILI_URL")
	setStr(&cfg.Meili.APIKey, "IMA_MEILI_API_KEY")
	setStr(&cfg.Meili.Index, "IMA_MEILI_INDEX")
	setStr(&cfg.DataDir, "IMA_DATA_DIR")
	setStr(&cfg.HTTPAddr, "IMA_HTTP_ADDR")
	setStr(&cfg.PublicBaseURL, "IMA_PUBLIC_BASE_URL")
}

// DBPath 返回 SQLite 数据库文件路径。
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "open-ima.db")
}
```

`internal/db/migrations.sql`(spec §5.1 原样):
```sql
CREATE TABLE IF NOT EXISTS knowledge_bases (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS documents (
    id          TEXT PRIMARY KEY,
    kb_id       TEXT NOT NULL REFERENCES knowledge_bases(id),
    title       TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_uri  TEXT NOT NULL,
    file_type   TEXT NOT NULL,
    file_hash   TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'pending',
                -- pending|parsing|chunking|indexing|ready|failed|deleting
    error       TEXT NOT NULL DEFAULT '',
    chunk_count INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_documents_kb ON documents(kb_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_documents_hash ON documents(kb_id, file_hash)
    WHERE file_hash != '';

CREATE TABLE IF NOT EXISTS chunks (
    id          TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES documents(id),
    kb_id       TEXT NOT NULL,
    seq         INTEGER NOT NULL,
    token_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_chunks_doc ON chunks(document_id);

CREATE TABLE IF NOT EXISTS jobs (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    payload     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    retry_count INTEGER NOT NULL DEFAULT 0,
    run_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_jobs_poll ON jobs(status, run_at);

CREATE TABLE IF NOT EXISTS conversations (
    id          TEXT PRIMARY KEY,
    kb_id       TEXT NOT NULL REFERENCES knowledge_bases(id),
    title       TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    role            TEXT NOT NULL,
    content         TEXT NOT NULL,
    citations       TEXT NOT NULL DEFAULT '[]',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

`internal/db/db.go`:
```go
// Package db 打开并迁移 SQLite 元数据库。
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations.sql
var migrations string

func Open(path string) (*sql.DB, error) {
	dsn := path
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared"
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单写者:避免 database is locked;也保证 :memory: 共享连接看到同一份数据
	d.SetMaxOpenConns(1)
	if path != ":memory:" {
		if _, err := d.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
			d.Close()
			return nil, err
		}
	}
	if _, err := d.ExecContext(context.Background(), migrations); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}
```

`cmd/server/main.go`:
```go
package main

import (
	"log"
	"net/http"
	"os"

	"open-ima/internal/config"
	"open-ima/internal/db"
	"open-ima/internal/httpx"
)

func main() {
	cfg, err := config.Load(os.Getenv("IMA_CONFIG"))
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, 200, map[string]string{"status": "ok"})
	})
	log.Printf("open-ima listening on %s", cfg.HTTPAddr)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, mux))
}
```

- [ ] **Step 4: 跑测试确认通过 + 构建**

Run: `go test ./internal/... && CGO_ENABLED=0 go build ./...`
Expected: 全 PASS,构建成功。

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd internal
git commit -m "chore(server): scaffold config, db migrations, httpx and /health main"
```

---

### Task 2: storage 接口 + LocalStorage + 内部文件端点

**Files:**
- Create: `internal/storage/storage.go`, `internal/storage/local.go`, `internal/storage/local_test.go`

**Interfaces:**
- Consumes: 无(独立包)
- Produces(Task 7/8/9/10 依赖):
  - `storage.Storage` 接口:`Put(ctx, key string, r io.Reader) error` / `Get(ctx, key string) (io.ReadCloser, error)` / `Delete(ctx, key string) error` / `URL(key string) string`
  - `storage.NewLocalStorage(root, publicBase, secret string) (*LocalStorage, error)`
  - `(*LocalStorage).Handler() http.Handler`:匹配 `GET /internal/files/{key}`,校验 `?token=` 后 ServeFile;token 错误 403,key 非法 400
  - key 约定:sha256 hex(64 字符);落盘 `root/<key[:2]>/<key>`;token = `sha256(secret+":"+key)` hex 的前 16 字符

- [ ] **Step 1: 写失败测试**

`internal/storage/local_test.go`:
```go
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestStore(t *testing.T) *LocalStorage {
	t.Helper()
	s, err := NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPutGetDeleteRoundtrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, testKey, strings.NewReader("hello world")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, testKey[:2], testKey)); err != nil {
		t.Fatalf("file not at sharded path: %v", err)
	}
	rc, err := s.Get(ctx, testKey)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "hello world" {
		t.Fatalf("got %q", data)
	}
	if err := s.Delete(ctx, testKey); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, testKey); err != nil { // 幂等
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.Get(ctx, testKey); err == nil {
		t.Fatal("get after delete should fail")
	}
}

func TestRejectsInvalidKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put(context.Background(), "../evil", strings.NewReader("x")); err == nil {
		t.Fatal("expected error for non-hex key")
	}
}

func TestURLAndHandlerToken(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put(context.Background(), testKey, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	url := s.URL(testKey)
	if !strings.HasPrefix(url, "http://app:8080/internal/files/"+testKey+"?token=") {
		t.Fatalf("url = %q", url)
	}
	token := strings.Split(url, "token=")[1]
	sum := sha256.Sum256([]byte("test-secret" + ":" + testKey))
	if token != hex.EncodeToString(sum[:])[:16] {
		t.Fatalf("token mismatch")
	}
	h := s.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/internal/files/"+testKey+"?token="+token, nil))
	if rec.Code != 200 || rec.Body.String() != "data" {
		t.Fatalf("valid token: code=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/internal/files/"+testKey+"?token=bad", nil))
	if rec.Code != 403 {
		t.Fatalf("bad token: code=%d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/storage/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/storage/storage.go`:
```go
// Package storage 抽象对象存储(COS 语义),V1 实现为本地目录。
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	URL(key string) string // 供 parser sidecar 回拉
}

func tokenFor(secret, key string) string {
	sum := sha256.Sum256([]byte(secret + ":" + key))
	return hex.EncodeToString(sum[:])[:16]
}

func validKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	for _, c := range key {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
```

`internal/storage/local.go`:
```go
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

type LocalStorage struct {
	root       string
	publicBase string
	secret     string
}

var _ Storage = (*LocalStorage)(nil)

func NewLocalStorage(root, publicBase, secret string) (*LocalStorage, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalStorage{root: root, publicBase: publicBase, secret: secret}, nil
}

func (s *LocalStorage) path(key string) string {
	return filepath.Join(s.root, key[:2], key)
}

func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader) error {
	if !validKey(key) {
		return fmt.Errorf("invalid storage key %q", key)
	}
	if err := os.MkdirAll(filepath.Dir(s.path(key)), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path(key)), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path(key))
}

func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, fmt.Errorf("invalid storage key %q", key)
	}
	return os.Open(s.path(key))
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	if !validKey(key) {
		return fmt.Errorf("invalid storage key %q", key)
	}
	if err := os.Remove(s.path(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *LocalStorage) URL(key string) string {
	return fmt.Sprintf("%s/internal/files/%s?token=%s", s.publicBase, key, tokenFor(s.secret, key))
}

// Handler 提供 GET /internal/files/{key}?token=...,仅供容器网络内 parser 回拉。
func (s *LocalStorage) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if !validKey(key) {
			http.Error(w, "invalid key", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("token") != tokenFor(s.secret, key) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.ServeFile(w, r, s.path(key))
	})
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/storage/`
Expected: 3 passed

- [ ] **Step 5: Commit**

```bash
git add internal/storage
git commit -m "feat(server): add storage abstraction with local impl and internal file endpoint"
```

---

### Task 3: queue(jobs 表 + worker pool + 退避重试)

**Files:**
- Create: `internal/queue/queue.go`, `internal/queue/worker.go`, `internal/queue/queue_test.go`

**Interfaces:**
- Consumes: Task 1 `db.Open`
- Produces(Task 7/10 依赖):
  - `queue.New(db *sql.DB) *Queue`;`q.Backoff []time.Duration`(默认 `[1m, 5m, 15m]`);`q.Now func() time.Time`(测试钩子,默认 `time.Now`);`q.MaxRetries() int`(= `len(q.Backoff)`)
  - `q.Enqueue(ctx, jobType string, payload any) (string, error)`
  - `q.Claim(ctx) (*Job, error)`(无待领 job 返回 `nil, nil`;领取即置 `running` 并把 `run_at` 记为领取时刻)
  - `q.Done(ctx, id) error`;`q.Fail(ctx, id) error`(`retry_count+1`;未达上限→`pending`,`run_at = now + Backoff[retry_count]`;达上限→`failed`);`q.FailPermanent(ctx, id) error`(直接 `failed`)
  - `q.ResetStale(ctx, staleAfter time.Duration) (int64, error)`(`running` 且 `run_at < now-staleAfter` → 重置 `pending`)
  - `queue.Permanent(err error) *PermanentError`(worker 收到 handler 返回的 PermanentError → `FailPermanent`,不重试)
  - `queue.NewWorker(q *Queue) *Worker`;`w.Register(jobType string, h Handler)`;`w.Start(ctx, concurrency int)`;`w.RunOnce(ctx) bool`(同步处理一个 job,测试用);`w.PollInterval`、`w.StaleAfter`(默认 `1s`、`10min`)
  - `Job` 字段:`ID string; Type string; Payload json.RawMessage; Status string; RetryCount int; RunAt time.Time`

- [ ] **Step 1: 写失败测试**

`internal/queue/queue_test.go`:
```go
package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"open-ima/internal/db"
)

func newTestQueue(t *testing.T) *Queue {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	q := New(d)
	q.Backoff = []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	return q
}

func TestEnqueueClaimDone(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	id, err := q.Enqueue(ctx, "parse_document", map[string]string{"document_id": "d1"})
	if err != nil || id == "" {
		t.Fatalf("enqueue: %v id=%q", err, id)
	}
	job, err := q.Claim(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim: %v", err)
	}
	if job.ID != id || job.Type != "parse_document" || job.Status != StatusRunning {
		t.Fatalf("job = %+v", job)
	}
	if string(job.Payload) != `{"document_id":"d1"}` {
		t.Fatalf("payload = %s", job.Payload)
	}
	// 已被领走,再次 Claim 为空
	again, _ := q.Claim(ctx)
	if again != nil {
		t.Fatalf("double claim: %+v", again)
	}
	if err := q.Done(ctx, id); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = q.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, id).Scan(&status)
	if status != StatusDone {
		t.Fatalf("status = %s", status)
	}
}

func TestFailBackoffAndExhaustion(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	base := time.Now()
	q.Now = func() time.Time { return base }
	id, _ := q.Enqueue(ctx, "t", map[string]int{"n": 1})

	mustClaim := func() *Job {
		j, err := q.Claim(ctx)
		if err != nil || j == nil {
			t.Fatalf("claim: %v", err)
		}
		return j
	}
	j := mustClaim()
	_ = q.Fail(ctx, j.ID)
	var runAt time.Time
	var retry int
	_ = q.db.QueryRow(`SELECT retry_count, run_at FROM jobs WHERE id=?`, id).Scan(&retry, &runAt)
	if retry != 1 || !runAt.Equal(base.Add(time.Second)) {
		t.Fatalf("retry=%d runAt=%v", retry, runAt)
	}
	// 未到 run_at 不可领取
	if j, _ := q.Claim(ctx); j != nil {
		t.Fatal("claimed before run_at")
	}
	base = base.Add(time.Second)
	_ = q.Fail(ctx, mustClaim().ID)
	base = base.Add(2 * time.Second)
	_ = q.Fail(ctx, mustClaim().ID) // 第 3 次失败,达上限
	var status string
	_ = q.db.QueryRow(`SELECT status, retry_count FROM jobs WHERE id=?`, id).Scan(&status, &retry)
	if status != StatusFailed || retry != 3 {
		t.Fatalf("status=%s retry=%d", status, retry)
	}
}

func TestFailPermanent(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	id, _ := q.Enqueue(ctx, "t", nil)
	j, _ := q.Claim(ctx)
	_ = q.FailPermanent(ctx, j.ID)
	var status string
	_ = q.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, id).Scan(&status)
	if status != StatusFailed {
		t.Fatalf("status = %s", status)
	}
}

func TestResetStale(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	base := time.Now()
	q.Now = func() time.Time { return base }
	id, _ := q.Enqueue(ctx, "t", nil)
	j, _ := q.Claim(ctx)
	base = base.Add(11 * time.Minute)
	n, err := q.ResetStale(ctx, 10*time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("reset: n=%d err=%v", n, err)
	}
	var status string
	_ = q.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, id).Scan(&status)
	if status != StatusPending {
		t.Fatalf("status = %s", status)
	}
	_ = j
}

func TestWorkerRunOnceAndPermanent(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	w := NewWorker(q)
	var handled []string
	w.Register("ok", func(_ context.Context, job *Job) error {
		handled = append(handled, job.ID)
		return nil
	})
	w.Register("perm", func(_ context.Context, _ *Job) error {
		return Permanent(errors.New("unparseable"))
	})
	idOK, _ := q.Enqueue(ctx, "ok", nil)
	idPerm, _ := q.Enqueue(ctx, "perm", nil)
	if !w.RunOnce(ctx) || !w.RunOnce(ctx) {
		t.Fatal("RunOnce should process both jobs")
	}
	if len(handled) != 1 || handled[0] != idOK {
		t.Fatalf("handled = %v", handled)
	}
	var status string
	_ = q.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, idPerm).Scan(&status)
	if status != StatusFailed {
		t.Fatalf("permanent job status = %s", status)
	}
	if w.RunOnce(ctx) {
		t.Fatal("no jobs left, RunOnce should be false")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/queue/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/queue/queue.go`:
```go
// Package queue 基于 SQLite jobs 表的内嵌任务队列(对标消息队列的本地化简化)。
package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

type Job struct {
	ID         string
	Type       string
	Payload    json.RawMessage
	Status     string
	RetryCount int
	RunAt      time.Time
}

// PermanentError 标记不可重试的错误(如 parser 422)。
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

func Permanent(err error) *PermanentError { return &PermanentError{Err: err} }

type Queue struct {
	db      *sql.DB
	Backoff []time.Duration
	Now     func() time.Time
}

func New(db *sql.DB) *Queue {
	return &Queue{
		db:      db,
		Backoff: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		Now:     time.Now,
	}
}

func (q *Queue) MaxRetries() int { return len(q.Backoff) }

func (q *Queue) Enqueue(ctx context.Context, jobType string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	_, err = q.db.ExecContext(ctx,
		`INSERT INTO jobs (id, type, payload) VALUES (?, ?, ?)`, id, jobType, string(data))
	return id, err
}

// Claim 原子领取最早到期的 pending job;无则返回 nil, nil。
func (q *Queue) Claim(ctx context.Context) (*Job, error) {
	now := q.Now()
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var j Job
	var payload string
	var runAt string
	err = tx.QueryRowContext(ctx,
		`SELECT id, type, payload, retry_count, run_at FROM jobs
		 WHERE status = ? AND run_at <= ? ORDER BY run_at LIMIT 1`,
		StatusPending, now.UTC().Format(time.DateTime)).Scan(&j.ID, &j.Type, &payload, &j.RetryCount, &runAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.Payload = json.RawMessage(payload)
	j.Status = StatusRunning
	j.RunAt = now
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status = ?, run_at = ? WHERE id = ?`,
		StatusRunning, now.UTC().Format(time.DateTime), j.ID); err != nil {
		return nil, err
	}
	return &j, tx.Commit()
}

func (q *Queue) Done(ctx context.Context, id string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE jobs SET status = ? WHERE id = ?`, StatusDone, id)
	return err
}

// Fail 记录一次失败:未达上限则按 Backoff 延后重试,达上限置 failed。
func (q *Queue) Fail(ctx context.Context, id string) error {
	var retry int
	if err := q.db.QueryRowContext(ctx, `SELECT retry_count FROM jobs WHERE id = ?`, id).Scan(&retry); err != nil {
		return err
	}
	retry++
	if retry >= q.MaxRetries() {
		_, err := q.db.ExecContext(ctx,
			`UPDATE jobs SET status = ?, retry_count = ? WHERE id = ?`, StatusFailed, retry, id)
		return err
	}
	runAt := q.Now().Add(q.Backoff[retry-1]).UTC().Format(time.DateTime)
	_, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, retry_count = ?, run_at = ? WHERE id = ?`,
		StatusPending, retry, runAt, id)
	return err
}

func (q *Queue) FailPermanent(ctx context.Context, id string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE jobs SET status = ? WHERE id = ?`, StatusFailed, id)
	return err
}

// ResetStale 把超时未完成的 running job(worker 崩溃残留)重置为 pending。
func (q *Queue) ResetStale(ctx context.Context, staleAfter time.Duration) (int64, error) {
	cutoff := q.Now().Add(-staleAfter).UTC().Format(time.DateTime)
	res, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET status = ? WHERE status = ? AND run_at < ?`,
		StatusPending, StatusRunning, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
```

`internal/queue/worker.go`:
```go
package queue

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

type Handler func(ctx context.Context, job *Job) error

type Worker struct {
	q            *Queue
	handlers     map[string]Handler
	PollInterval time.Duration
	StaleAfter   time.Duration
}

func NewWorker(q *Queue) *Worker {
	return &Worker{
		q:            q,
		handlers:     map[string]Handler{},
		PollInterval: time.Second,
		StaleAfter:   10 * time.Minute,
	}
}

func (w *Worker) Register(jobType string, h Handler) {
	w.handlers[jobType] = h
}

// RunOnce 同步领取并处理一个 job;有 job 被执行返回 true(测试与手动驱动用)。
func (w *Worker) RunOnce(ctx context.Context) bool {
	job, err := w.q.Claim(ctx)
	if err != nil || job == nil {
		return false
	}
	w.handle(ctx, job)
	return true
}

func (w *Worker) handle(ctx context.Context, job *Job) {
	h, ok := w.handlers[job.Type]
	if !ok {
		_ = w.q.FailPermanent(ctx, job.ID)
		return
	}
	err := h(ctx, job)
	var pe *PermanentError
	switch {
	case err == nil:
		_ = w.q.Done(ctx, job.ID)
	case errors.As(err, &pe):
		_ = w.q.FailPermanent(ctx, job.ID)
	default:
		_ = w.q.Fail(ctx, job.ID)
	}
}

// Start 启动 worker pool,直到 ctx 取消。
func (w *Worker) Start(ctx context.Context, concurrency int) {
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stale := time.NewTicker(w.StaleAfter / 2)
			defer stale.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-stale.C:
					if _, err := w.q.ResetStale(ctx, w.StaleAfter); err != nil {
						log.Printf("queue: reset stale: %v", err)
					}
				default:
				}
				if !w.RunOnce(ctx) {
					select {
					case <-ctx.Done():
						return
					case <-time.After(w.PollInterval):
					}
				}
			}
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/queue/`
Expected: 5 passed

- [ ] **Step 5: Commit**

```bash
git add internal/queue
git commit -m "feat(server): add sqlite-backed job queue and worker pool"
```

---

### Task 4: chunker(递归分隔符切分 + 标题面包屑,照搬 WeKnora splitter 设计)

**Files:**
- Create: `internal/chunker/chunker.go`, `internal/chunker/chunker_test.go`

**Interfaces:**
- Consumes: 无
- Produces(Task 7/10 依赖):
  - `chunker.Block{Type string; Text string; Level int}`(与 parserclient.Block 同构,media 负责转换)
  - `chunker.Chunk{Content, ContextHeader string; Seq, Start, End int}`;`Start/End` 为内容来源 block 的下标区间 `[Start, End)`(输入是 block 列表,对 WeKnora rune 偏移语义的适配);`Chunk.EmbeddingContent() string` = 有 header 时 `ContextHeader + "\n\n" + trim(Content)`,否则 `trim(Content)`
  - `chunker.New(chunkSize, overlap int) *Chunker`;`(*Chunker).Chunk(blocks []Block) []Chunk`
  - 生产固定参数:`chunker.New(512, 80)`;separators 固定 `["\n\n", "\n", "。", "?", "!", ";", " "]`

行为约定(测试逐条锁定):
1. 计数单位 = rune 数(中文场景字符≈token,WeKnora 生产验证,**不引 tiktoken**)
2. heading 维护层级栈:新 heading 弹出所有 `level >= 其 level` 的条目后入栈;`ContextHeader` = 栈内 1→6 级文本以 `" > "` 连接;heading 结束当前 chunk(不缝 overlap),heading 文本不进入 Content
3. 超长切分递归降维:以当前 separator 切分、保留分隔符在片段尾部;片段仍超限降一级;separator 耗尽按 rune 硬切
4. 保护区域:`table`/`list` block 不腰斩——合并时整块移动;仅整块自身超限才走第 3 条
5. size 超限触发的 flush,新 chunk 以前一 chunk 尾部 ~overlap rune 开头;heading 触发或紧邻保护块的 flush 不缝 overlap
6. 空文本 block 跳过;全部为空 → 返回空切片

- [ ] **Step 1: 写失败测试**

`internal/chunker/chunker_test.go`:
```go
package chunker

import (
	"strings"
	"testing"
)


func TestHeadingBreadcrumb(t *testing.T) {
	c := New(512, 80)
	chunks := c.Chunk([]Block{
		{Type: "heading", Text: "第一章", Level: 1},
		{Type: "paragraph", Text: "正文A"},
		{Type: "heading", Text: "第一节", Level: 2},
		{Type: "paragraph", Text: "正文B"},
		{Type: "heading", Text: "第二章", Level: 1},
		{Type: "paragraph", Text: "正文C"},
	})
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d: %+v", len(chunks), chunks)
	}
	if chunks[0].ContextHeader != "第一章" {
		t.Fatalf("header0 = %q", chunks[0].ContextHeader)
	}
	if chunks[1].ContextHeader != "第一章 > 第一节" {
		t.Fatalf("header1 = %q", chunks[1].ContextHeader)
	}
	if chunks[2].ContextHeader != "第二章" { // 新 H1 弹出旧栈
		t.Fatalf("header2 = %q", chunks[2].ContextHeader)
	}
	if chunks[0].EmbeddingContent() != "第一章\n\n正文A" {
		t.Fatalf("embed0 = %q", chunks[0].EmbeddingContent())
	}
	if chunks[0].Seq != 0 || chunks[1].Seq != 1 || chunks[2].Seq != 2 {
		t.Fatalf("seq wrong: %+v", chunks)
	}
	if chunks[0].Start != 1 || chunks[0].End != 2 || chunks[1].Start != 3 || chunks[1].End != 4 {
		t.Fatalf("block range wrong: %+v", chunks)
	}
}

func TestRecursiveSplitKeepsSeparators(t *testing.T) {
	c := New(20, 4)
	text := "AAAAAAAAAA。BBBBBBBBBB。CCCCCCCCCC。DDDD" // 34 runes
	chunks := c.Chunk([]Block{{Type: "paragraph", Text: text}})
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	for _, ch := range chunks {
		if runeLen(ch.Content) > 20 {
			t.Fatalf("oversize chunk %q (%d runes)", ch.Content, runeLen(ch.Content))
		}
	}
	if !strings.HasSuffix(chunks[0].Content, "。") {
		t.Fatalf("separator lost: chunk0 = %q", chunks[0].Content)
	}
}

func TestProtectedTableBlock(t *testing.T) {
	c := New(30, 4)
	table := "| 列A | 列B |\n| 1 | 2 |\n| 3 | 4 |" // 24 runes < 30
	chunks := c.Chunk([]Block{
		{Type: "paragraph", Text: "前文前文前文"},
		{Type: "table", Text: table},
		{Type: "paragraph", Text: "后文后文后文"},
	})
	found := false
	for _, ch := range chunks {
		if strings.Contains(ch.Content, table) {
			found = true
		}
	}
	if !found {
		t.Fatalf("table was split: %+v", chunks)
	}
}

func TestOversizedTableHardSplit(t *testing.T) {
	c := New(10, 2)
	table := strings.Repeat("表格行内容", 5) // 20 runes,整块超限才硬切
	chunks := c.Chunk([]Block{{Type: "table", Text: table}})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d: %+v", len(chunks), chunks)
	}
	total := 0
	for _, ch := range chunks {
		total += runeLen(ch.Content)
	}
	if total != 20 {
		t.Fatalf("content lost: total = %d", total)
	}
}

func TestOverlapBetweenParagraphs(t *testing.T) {
	c := New(16, 4)
	chunks := c.Chunk([]Block{
		{Type: "paragraph", Text: "AAAAAAAAAAAA"}, // 12
		{Type: "paragraph", Text: "BBBBBBBBBBBB"}, // 12,合并超限 → flush
	})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	if chunks[0].Content != "AAAAAAAAAAAA" {
		t.Fatalf("chunk0 = %q", chunks[0].Content)
	}
	if !strings.HasPrefix(chunks[1].Content, "AAAA") { // overlap 4 runes
		t.Fatalf("chunk1 = %q", chunks[1].Content)
	}
}

func TestEmpty(t *testing.T) {
	c := New(16, 4)
	if got := c.Chunk([]Block{{Type: "paragraph", Text: "  "}}); len(got) != 0 {
		t.Fatalf("got = %+v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/chunker/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/chunker/chunker.go`:
```go
// Package chunker 把解析 block 序列切分为检索 chunk。
// 设计照搬 WeKnora internal/infrastructure/chunker/splitter.go:
// 512/80 字符、递归分隔符降维、标题面包屑与 Content 分离。
package chunker

import (
	"strings"
	"unicode/utf8"
)

type Block struct {
	Type  string // heading | paragraph | list | table
	Text  string
	Level int
}

// Chunk Content 与 ContextHeader 分离:header 在 embedding/检索时前置,
// 不进 Content(WeKnora 同款不变量)。
type Chunk struct {
	Content       string
	ContextHeader string
	Seq           int
	Start         int // 首个内容源 block 下标
	End           int // 最后内容源 block 下标 + 1
}

// EmbeddingContent 返回喂给 embedding 模型的文本。
func (c Chunk) EmbeddingContent() string {
	body := strings.TrimSpace(c.Content)
	if c.ContextHeader == "" {
		return body
	}
	return c.ContextHeader + "\n\n" + body
}

type Chunker struct {
	size       int
	overlap    int
	separators []string
}

func New(chunkSize, overlap int) *Chunker {
	return &Chunker{
		size:       chunkSize,
		overlap:    overlap,
		separators: []string{"\n\n", "\n", "。", "?", "!", ";", " "},
	}
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// acc 当前累积中的 chunk 缓冲。
type acc struct {
	buf          strings.Builder
	length       int // buf 的 rune 数
	firstBlock   int
	lastBlock    int
	hasContent   bool
	hasProtected bool // 含 table/list 整块
}

func (c *Chunker) Chunk(blocks []Block) []Chunk {
	var chunks []Chunk
	headings := map[int]string{}
	header := func() string {
		var parts []string
		for lvl := 1; lvl <= 6; lvl++ {
			if t, ok := headings[lvl]; ok {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " > ")
	}
	var cur acc
	flush := func(withOverlap bool) {
		if !cur.hasContent {
			return
		}
		content := cur.buf.String()
		chunks = append(chunks, Chunk{
			Content: content, ContextHeader: header(),
			Start: cur.firstBlock, End: cur.lastBlock + 1,
		})
		cur = acc{firstBlock: cur.lastBlock + 1}
		if withOverlap {
			tail := tailRunes(content, c.overlap)
			cur.buf.WriteString(tail)
			cur.length = runeLen(tail)
			// overlap 残尾不算内容来源,不动 hasContent/firstBlock
		}
	}
	appendBlock := func(i int, text string, protected bool) {
		if !cur.hasContent && cur.length == 0 {
			cur.firstBlock = i
		}
		if cur.hasContent {
			cur.buf.WriteString("\n\n")
			cur.length += 2
		}
		cur.buf.WriteString(text)
		cur.length += runeLen(text)
		cur.lastBlock = i
		cur.hasContent = true
		cur.hasProtected = cur.hasProtected || protected
	}

	for i, b := range blocks {
		text := strings.TrimSpace(b.Text)
		if b.Type == "heading" {
			flush(false)
			for lvl := range headings {
				if lvl >= b.Level {
					delete(headings, lvl)
				}
			}
			headings[b.Level] = text
			continue
		}
		if text == "" {
			continue
		}
		protected := b.Type == "table" || b.Type == "list"
		if blen := runeLen(text); blen > c.size {
			// 整块超限:flush 后递归降维切,片段各自成 chunk
			flush(false)
			for _, piece := range c.recursiveSplit(text, c.separators) {
				chunks = append(chunks, Chunk{
					Content: piece, ContextHeader: header(), Start: i, End: i + 1,
				})
			}
			cur = acc{firstBlock: i + 1}
			continue
		}
		need := runeLen(text)
		if cur.hasContent {
			need += 2 // "\n\n" 连接符
		}
		if cur.hasContent && cur.length+need > c.size {
			flush(!cur.hasProtected && !protected)
		}
		appendBlock(i, text, protected)
	}
	flush(false)
	for i := range chunks {
		chunks[i].Seq = i
	}
	return chunks
}

func tailRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// recursiveSplit 递归分隔符降维切分,分隔符保留在片段尾部。
func (c *Chunker) recursiveSplit(text string, seps []string) []string {
	if runeLen(text) <= c.size {
		return []string{text}
	}
	if len(seps) == 0 { // separator 耗尽,rune 硬切
		var out []string
		runes := []rune(text)
		for start := 0; start < len(runes); start += c.size {
			end := start + c.size
			if end > len(runes) {
				end = len(runes)
			}
			out = append(out, string(runes[start:end]))
		}
		return out
	}
	sep := seps[0]
	raw := strings.Split(text, sep)
	parts := make([]string, len(raw))
	for i, p := range raw {
		if i < len(raw)-1 {
			parts[i] = p + sep // 保留分隔符
		} else {
			parts[i] = p
		}
	}
	var out []string
	var cur strings.Builder
	curLen := 0
	for _, p := range parts {
		pl := runeLen(p)
		if curLen+pl <= c.size {
			cur.WriteString(p)
			curLen += pl
			continue
		}
		if curLen > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curLen = 0
		}
		if pl > c.size {
			out = append(out, c.recursiveSplit(p, seps[1:])...)
		} else {
			cur.WriteString(p)
			curLen = pl
		}
	}
	if curLen > 0 {
		out = append(out, cur.String())
	}
	return out
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/chunker/ && CGO_ENABLED=0 go build ./...`
Expected: 6 passed,构建成功

- [ ] **Step 5: Commit**

```bash
git add internal/chunker
git commit -m "feat(server): add recursive-separator chunker with heading breadcrumbs (WeKnora-style)"
```

---

### Task 5: meili client

**Files:**
- Create: `internal/meili/client.go`, `internal/meili/client_test.go`

**Interfaces:**
- Consumes: 无
- Produces(Task 7/10 依赖):
  - `meili.New(baseURL, apiKey string) *Client`;字段 `PollInterval time.Duration`(默认 200ms)、`TaskTimeout time.Duration`(默认 30s),测试可调小
  - `meili.ChunkDoc{ID, KBID, DocumentID, Title, Content string; Vectors map[string][]float32}`(JSON tag:`id, kb_id, document_id, title, content, _vectors`)
  - `(*Client).EnsureIndex(ctx, uid string, dimensions int) error`(不存在则建索引,然后 PATCH settings:`searchableAttributes=[title,content]`,`filterableAttributes=[kb_id,document_id]`,`embedders.default={source:userProvided, dimensions:d}`;同步等待任务完成)
  - `(*Client).AddDocuments(ctx, uid string, docs []ChunkDoc) error`
  - `(*Client).DeleteByFilter(ctx, uid, filter string) error`
  - 全部写操作等待 Meili 异步 task `succeeded`;task `failed` 返回包含 task error message 的 error
  - `apiKey` 非空时每个请求带 `Authorization: Bearer <key>`

- [ ] **Step 1: 写失败测试**

`internal/meili/client_test.go`:
```go
package meili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeMeili struct {
	mu       sync.Mutex
	requests []string // METHOD path
	bodies   []string
	existing map[string]bool
	failTask bool
}

func (f *fakeMeili) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /indexes/{uid}", func(w http.ResponseWriter, r *http.Request) {
		if f.existing[r.PathValue("uid")] {
			w.Write([]byte(`{"uid":"` + r.PathValue("uid") + `","primaryKey":"id"}`))
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"not found"}`))
	})
	mux.HandleFunc("POST /indexes", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, nil)
		w.WriteHeader(202)
		w.Write([]byte(`{"taskUid": 1}`))
	})
	mux.HandleFunc("PATCH /indexes/{uid}/settings", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, nil)
		w.WriteHeader(202)
		w.Write([]byte(`{"taskUid": 2}`))
	})
	mux.HandleFunc("POST /indexes/{uid}/documents", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, nil)
		w.WriteHeader(202)
		w.Write([]byte(`{"taskUid": 3}`))
	})
	mux.HandleFunc("POST /indexes/{uid}/documents/delete", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, nil)
		w.WriteHeader(202)
		w.Write([]byte(`{"taskUid": 4}`))
	})
	mux.HandleFunc("GET /tasks/{uid}", func(w http.ResponseWriter, r *http.Request) {
		if f.failTask {
			w.Write([]byte(`{"status":"failed","error":{"message":"index already exists"}}`))
			return
		}
		w.Write([]byte(`{"status":"succeeded"}`))
	})
	return mux
}

func (f *fakeMeili) record(r *http.Request, _ any) {
	var body []byte
	if r.Body != nil {
		body, _ = readAll(r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.bodies = append(f.bodies, string(body))
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

func newFake(t *testing.T) (*Client, *fakeMeili) {
	t.Helper()
	f := &fakeMeili{existing: map[string]bool{}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c := New(srv.URL, "test-key")
	c.PollInterval = time.Millisecond
	c.TaskTimeout = 5 * time.Second
	return c, f
}

func TestEnsureIndexCreatesAndConfigures(t *testing.T) {
	c, f := newFake(t)
	if err := c.EnsureIndex(context.Background(), "chunks", 1024); err != nil {
		t.Fatal(err)
	}
	joined := fmt.Sprint(f.requests)
	want := []string{"GET /indexes/chunks", "POST /indexes", "PATCH /indexes/chunks/settings"}
	for _, w := range want {
		if !contains(joined, w) {
			t.Errorf("missing request %s in %v", w, f.requests)
		}
	}
	var settings map[string]any
	for i, req := range f.requests {
		if req == "PATCH /indexes/chunks/settings" {
			_ = json.Unmarshal([]byte(f.bodies[i]), &settings)
		}
	}
	if settings == nil {
		t.Fatal("settings body not recorded")
	}
	emb := settings["embedders"].(map[string]any)["default"].(map[string]any)
	if emb["source"] != "userProvided" || emb["dimensions"].(float64) != 1024 {
		t.Errorf("embedder settings = %v", emb)
	}
}

func TestEnsureIndexSkipsCreateWhenExists(t *testing.T) {
	c, f := newFake(t)
	f.existing["chunks"] = true
	if err := c.EnsureIndex(context.Background(), "chunks", 1024); err != nil {
		t.Fatal(err)
	}
	for _, req := range f.requests {
		if req == "POST /indexes" {
			t.Fatal("should not create existing index")
		}
	}
}

func TestAddDocumentsPostsDocsAndWaits(t *testing.T) {
	c, f := newFake(t)
	docs := []ChunkDoc{{
		ID: "c1", KBID: "kb1", DocumentID: "d1", Title: "t", Content: "hello",
		Vectors: map[string][]float32{"default": {0.1, 0.2}},
	}}
	if err := c.AddDocuments(context.Background(), "chunks", docs); err != nil {
		t.Fatal(err)
	}
	var posted []map[string]any
	for i, req := range f.requests {
		if req == "POST /indexes/chunks/documents" {
			_ = json.Unmarshal([]byte(f.bodies[i]), &posted)
		}
	}
	if len(posted) != 1 || posted[0]["kb_id"] != "kb1" {
		t.Fatalf("posted = %v", posted)
	}
	vec := posted[0]["_vectors"].(map[string]any)["default"].([]any)
	if len(vec) != 2 {
		t.Fatalf("vectors = %v", vec)
	}
}

func TestDeleteByFilter(t *testing.T) {
	c, f := newFake(t)
	if err := c.DeleteByFilter(context.Background(), "chunks", "document_id = 'd1'"); err != nil {
		t.Fatal(err)
	}
	found := false
	for i, req := range f.requests {
		if req == "POST /indexes/chunks/documents/delete" && f.bodies[i] == `{"filter":"document_id = 'd1'"}` {
			found = true
		}
	}
	if !found {
		t.Fatalf("requests = %v bodies = %v", f.requests, f.bodies)
	}
}

func TestFailedTaskReturnsError(t *testing.T) {
	c, f := newFake(t)
	f.failTask = true
	err := c.AddDocuments(context.Background(), "chunks", []ChunkDoc{{ID: "x"}})
	if err == nil || !contains(err.Error(), "index already exists") {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthHeaderSent(t *testing.T) {
	c, _ := newFake(t)
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(404)
	}))
	defer srv.Close()
	c.baseURL = srv.URL
	_ = c.EnsureIndex(context.Background(), "x", 8)
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth = %q", gotAuth)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/meili/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/meili/client.go`:
```go
// Package meili 是 Meilisearch 的裸 HTTP client(不引 SDK,便于 mock)。
package meili

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL      string
	apiKey       string
	hc           *http.Client
	PollInterval time.Duration
	TaskTimeout  time.Duration
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:      baseURL,
		apiKey:       apiKey,
		hc:           &http.Client{Timeout: 60 * time.Second},
		PollInterval: 200 * time.Millisecond,
		TaskTimeout:  30 * time.Second,
	}
}

type ChunkDoc struct {
	ID         string              `json:"id"`
	KBID       string              `json:"kb_id"`
	DocumentID string              `json:"document_id"`
	Title      string              `json:"title"`
	Content    string              `json:"content"`
	Vectors    map[string][]float32 `json:"_vectors"`
}

func (c *Client) EnsureIndex(ctx context.Context, uid string, dimensions int) error {
	code, err := c.do(ctx, http.MethodGet, "/indexes/"+uid, nil, nil)
	if err != nil {
		return err
	}
	if code == http.StatusNotFound {
		var task struct {
			TaskUID int64 `json:"taskUid"`
		}
		if _, err := c.do(ctx, http.MethodPost, "/indexes", map[string]string{
			"uid": uid, "primaryKey": "id",
		}, &task); err != nil {
			return err
		}
		if err := c.waitTask(ctx, task.TaskUID); err != nil {
			return err
		}
	}
	settings := map[string]any{
		"searchableAttributes": []string{"title", "content"},
		"filterableAttributes": []string{"kb_id", "document_id"},
		"embedders": map[string]any{
			"default": map[string]any{"source": "userProvided", "dimensions": dimensions},
		},
	}
	var task struct {
		TaskUID int64 `json:"taskUid"`
	}
	if _, err := c.do(ctx, http.MethodPatch, "/indexes/"+uid+"/settings", settings, &task); err != nil {
		return err
	}
	return c.waitTask(ctx, task.TaskUID)
}

func (c *Client) AddDocuments(ctx context.Context, uid string, docs []ChunkDoc) error {
	var task struct {
		TaskUID int64 `json:"taskUid"`
	}
	if _, err := c.do(ctx, http.MethodPost, "/indexes/"+uid+"/documents", docs, &task); err != nil {
		return err
	}
	return c.waitTask(ctx, task.TaskUID)
}

func (c *Client) DeleteByFilter(ctx context.Context, uid, filter string) error {
	var task struct {
		TaskUID int64 `json:"taskUid"`
	}
	if _, err := c.do(ctx, http.MethodPost, "/indexes/"+uid+"/documents/delete",
		map[string]string{"filter": filter}, &task); err != nil {
		return err
	}
	return c.waitTask(ctx, task.TaskUID)
}

// do 发请求并把 2xx 响应解码进 out;非 2xx 返回 (statusCode, error)。
func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return resp.StatusCode, nil
	}
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("meili %s %s: status %d: %s", method, path, resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("meili decode %s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

func (c *Client) waitTask(ctx context.Context, taskUID int64) error {
	deadline := time.Now().Add(c.TaskTimeout)
	for {
		var task struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if _, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/tasks/%d", taskUID), nil, &task); err != nil {
			return err
		}
		switch task.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			msg := "unknown"
			if task.Error != nil {
				msg = task.Error.Message
			}
			return fmt.Errorf("meili task %d %s: %s", taskUID, task.Status, msg)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("meili task %d: timeout after %s", taskUID, c.TaskTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.PollInterval):
		}
	}
}
```

注意:测试中 `readAll`/`contains`/`indexOf` 是测试辅助,如标准库 `io.ReadAll`/`strings.Contains` 可用则直接替换(测试代码以此计划为准逐字实现亦可)。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/meili/`
Expected: 6 passed

- [ ] **Step 5: Commit**

```bash
git add internal/meili
git commit -m "feat(server): add meilisearch http client with task waiting"
```

---

### Task 6: parserclient + llm embedding client

**Files:**
- Create: `internal/parserclient/client.go`, `internal/parserclient/client_test.go`
- Create: `internal/llm/embedding.go`, `internal/llm/embedding_test.go`

**Interfaces:**
- Produces(Task 7/10 依赖):
  - `parserclient.New(baseURL string) *Client`;`(*Client).Parse(ctx, fileURL, fileType string) (*ParseResult, error)`
  - `parserclient.ParseResult{Title string; Blocks []Block}`;`parserclient.Block{Type, Text string; Level int}`(JSON 与 P1 契约一致)
  - parser 返回 422 → error 为 `*parserclient.FatalError`(`FatalError{Message string}`);502/5xx/网络错误 → 普通 error(可重试)
  - `llm.NewEmbeddingClient(baseURL, apiKey, model string) *EmbeddingClient`;字段 `BatchSize int`(默认 64)
  - `(*EmbeddingClient).Embed(ctx, texts []string) ([][]float32, error)`(按 BatchSize 分批 POST `{baseURL}/embeddings`,body `{"model": model, "input": [...]}`;响应 `data` 按 `index` 排序拼接;空输入返回空切片,nil error)

- [ ] **Step 1: 写失败测试**

`internal/parserclient/client_test.go`:
```go
package parserclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/parse" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"title":"doc","blocks":[{"type":"heading","text":"H","level":1},{"type":"paragraph","text":"P","level":0}]}`))
	}))
	defer srv.Close()
	res, err := New(srv.URL).Parse(context.Background(), "http://x/f.md", "md")
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "doc" || len(res.Blocks) != 2 || res.Blocks[0].Level != 1 {
		t.Fatalf("res = %+v", res)
	}
}

func TestParse422IsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"error":"pdf: encrypted file is not supported"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL).Parse(context.Background(), "http://x/f.pdf", "pdf")
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %T %v", err, err)
	}
	if fatal.Message != "pdf: encrypted file is not supported" {
		t.Fatalf("msg = %q", fatal.Message)
	}
}

func TestParse502IsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(502)
		w.Write([]byte(`{"error":"fetch failed"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL).Parse(context.Background(), "http://x/f.pdf", "pdf")
	var fatal *FatalError
	if err == nil || errors.As(err, &fatal) {
		t.Fatalf("err = %T %v", err, err)
	}
}
```

`internal/llm/embedding_test.go`:
```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbedBatchesAndSortsByIndex(t *testing.T) {
	var batches []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "test-emb" {
			t.Errorf("model = %q", req.Model)
		}
		batches = append(batches, len(req.Input))
		// 故意乱序返回,校验按 index 归位
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": []float64{float64(i), float64(len(req.Input))}}
		}
		if len(data) == 2 {
			data[0], data[1] = data[1], data[0]
		}
		resp := map[string]any{"data": data}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	c := NewEmbeddingClient(srv.URL, "k", "test-emb")
	c.BatchSize = 2
	vecs, err := c.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(batches) != "[2 1]" {
		t.Fatalf("batches = %v", batches)
	}
	if len(vecs) != 3 || vecs[0][0] != 0 || vecs[1][0] != 1 || vecs[2][0] != 0 {
		t.Fatalf("vecs = %v", vecs)
	}
	if vecs[0][1] != 2 || vecs[2][1] != 1 { // 第二维 = 该批 input 长度
		t.Fatalf("batch boundary wrong: %v", vecs)
	}
}

func TestEmbedEmptyInput(t *testing.T) {
	c := NewEmbeddingClient("http://unused", "k", "m")
	vecs, err := c.Embed(context.Background(), nil)
	if err != nil || len(vecs) != 0 {
		t.Fatalf("vecs=%v err=%v", vecs, err)
	}
}

func TestEmbedHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	_, err := NewEmbeddingClient(srv.URL, "k", "m").Embed(context.Background(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/parserclient/ ./internal/llm/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/parserclient/client.go`:
```go
// Package parserclient 调用独立解析层(Python parser sidecar)。
package parserclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Block struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

type ParseResult struct {
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
}

// FatalError 内容不可解析(HTTP 422),不可重试。
type FatalError struct{ Message string }

func (e *FatalError) Error() string { return e.Message }

type Client struct {
	baseURL string
	hc      *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, hc: &http.Client{Timeout: 120 * time.Second}}
}

func (c *Client) Parse(ctx context.Context, fileURL, fileType string) (*ParseResult, error) {
	body, _ := json.Marshal(map[string]string{"file_url": fileURL, "file_type": fileType})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/parse", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var errBody struct {
		Error string `json:"error"`
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, &FatalError{Message: errBody.Error}
	}
	if resp.StatusCode != http.StatusOK {
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("parser: status %d: %s", resp.StatusCode, errBody.Error)
	}
	var result ParseResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parser: decode: %w", err)
	}
	return &result, nil
}
```

`internal/llm/embedding.go`:
```go
// Package llm 封装云端 OpenAI 兼容 API(本文件为 embedding;chat 在 P3)。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

type EmbeddingClient struct {
	baseURL   string
	apiKey    string
	model     string
	hc        *http.Client
	BatchSize int
}

func NewEmbeddingClient(baseURL, apiKey, model string) *EmbeddingClient {
	return &EmbeddingClient{
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     model,
		hc:        &http.Client{Timeout: 120 * time.Second},
		BatchSize: 64,
	}
}

func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += c.BatchSize {
		end := start + c.BatchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (c *EmbeddingClient) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": c.model, "input": batch})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: status %d", resp.StatusCode)
	}
	var result struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embedding: decode: %w", err)
	}
	if len(result.Data) != len(batch) {
		return nil, fmt.Errorf("embedding: got %d vectors for %d inputs", len(result.Data), len(batch))
	}
	sort.Slice(result.Data, func(i, j int) bool { return result.Data[i].Index < result.Data[j].Index })
	out := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/parserclient/ ./internal/llm/`
Expected: 6 passed

- [ ] **Step 5: Commit**

```bash
git add internal/parserclient internal/llm
git commit -m "feat(server): add parser sidecar and embedding http clients"
```

---

### Task 7: media service(状态机 + 流水线 + 删除/对账)

**Files:**
- Create: `internal/media/service.go`, `internal/media/handlers.go`, `internal/media/service_test.go`

**Interfaces:**
- Consumes: Task 1 `db`;Task 2 `storage.Storage`;Task 3 `queue`;Task 4 `chunker`;Task 5 `meili`;Task 6 `parserclient`/`llm`
- Produces(Task 8/9/10 依赖):
  - 常量:`media.StatusPending/Parsing/Chunking/Indexing/Ready/Failed/Deleting`(小写字符串);`media.JobParseDocument = "parse_document"`、`media.JobDeleteDocument = "delete_document"`、`media.JobReconcile = "reconcile"`
  - `media.Document` 结构:`ID, KBID, Title, SourceType, SourceURI, FileType, FileHash, Status, Error string; ChunkCount int; CreatedAt, UpdatedAt time.Time`
  - `media.Deps{DB *sql.DB; Store storage.Storage; Queue *queue.Queue; Parser *parserclient.Client; Embedder *llm.EmbeddingClient; Meili *meili.Client; Chunker *chunker.Chunker; MeiliIndex string}`
  - `media.NewService(d Deps) *Service`
  - `(*Service).CreateDocument(ctx, kbID, title, sourceType, sourceURI, fileType, fileHash string) (docID string, duplicate bool, err error)`:fileHash 非空且同库已存在 → 返回既有 docID,`duplicate=true`(不新建不入队);否则插入 `pending` 并入队 `parse_document`(payload `{"document_id": id}`)
  - `(*Service).RegisterHandlers(w *queue.Worker)`:注册三种 job handler
  - `(*Service).List(ctx, kbID string) ([]Document, error)`(按 created_at DESC);`(*Service).Get(ctx, id string) (*Document, error)`
  - `(*Service).Retry(ctx, id string) error`:仅 `failed` 可重试 → 置 `pending`、清空 error、入队 `parse_document`
  - `(*Service).Delete(ctx, id string) error`:置 `deleting`、删 chunks 行、入队 `delete_document`(payload `{"document_id": id}`)
  - `(*Service).EnqueueReconcile(ctx) error`:入队 `reconcile`(payload `{}`,供定时器)
  - handler:`HandleParseDocument` / `HandleDeleteDocument` / `HandleReconcile`,签名均为 `func(ctx context.Context, job *queue.Job) error`

行为约定(测试逐条锁定):
- `HandleParseDocument` 流水线:`parsing`(parser.Parse,422→标记 failed 并返回 nil 让 job done)→ `chunking`(删旧 chunks 行 → 分块 → 写新 chunks 行)→ `indexing`(Meili DeleteByFilter 清旧 → embedding → AddDocuments)→ `ready`(chunk_count 更新);可重试错误且 `job.RetryCount+1 >= Queue.MaxRetries()` 时标记 failed;document 已是 `ready`/`deleting` 时直接返回 nil(幂等)
- `HandleDeleteDocument`:Meili DeleteByFilter(`document_id = '<id>'`)→ Store.Delete(source_uri)→ 删 documents 行;document 不存在直接返回 nil
- `HandleReconcile`:扫描 `status='deleting'` 的 documents,逐个入队 `delete_document`

- [ ] **Step 1: 写失败测试**

`internal/media/service_test.go`(测试基建:sqlite :memory: + LocalStorage(t.TempDir)+ 三个 httptest mock):
```go
package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"open-ima/internal/chunker"
	"open-ima/internal/db"
	"open-ima/internal/llm"
	"open-ima/internal/meili"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/storage"
)

type testRig struct {
	db         *sql.DB
	svc        *Service
	q          *queue.Queue
	w          *queue.Worker
	parserSrv  *httptest.Server
	meiliDocs  [][]byte // 记录 AddDocuments 的 body
	meiliDels  []string // 记录 delete filter
	mu         sync.Mutex
	parserCode int
	parserBody string
}

func newRig(t *testing.T) *testRig {
	t.Helper()
	rig := &testRig{parserCode: 200, parserBody: `{"title":"doc","blocks":[{"type":"heading","text":"H1","level":1},{"type":"paragraph","text":"正文内容"}]}`}

	rig.parserSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rig.mu.Lock()
		code, body := rig.parserCode, rig.parserBody
		rig.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		w.Write([]byte(body))
	}))
	t.Cleanup(rig.parserSrv.Close)

	meiliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/documents") && r.Method == "POST":
			buf := make([]byte, r.ContentLength)
			r.Body.Read(buf)
			rig.meiliDocs = append(rig.meiliDocs, buf)
		case strings.HasSuffix(r.URL.Path, "/documents/delete"):
			var b struct {
				Filter string `json:"filter"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			rig.meiliDels = append(rig.meiliDels, b.Filter)
		case strings.HasPrefix(r.URL.Path, "/tasks/"):
			w.Write([]byte(`{"status":"succeeded"}`))
			return
		}
		w.WriteHeader(202)
		w.Write([]byte(`{"taskUid":1}`))
	}))
	t.Cleanup(meiliSrv.Close)

	embSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": []float64{0.1, 0.2, 0.3}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(embSrv.Close)

	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO knowledge_bases (id, name) VALUES ('kb1', '测试库')`); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "secret")
	if err != nil {
		t.Fatal(err)
	}
	q := queue.New(d)
	q.Backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	mc := meili.New(meiliSrv.URL, "")
	mc.PollInterval = time.Millisecond
	ec := llm.NewEmbeddingClient(embSrv.URL, "", "test")
	svc := NewService(Deps{
		DB: d, Store: store, Queue: q,
		Parser: parserclient.New(rig.parserSrv.URL), Embedder: ec, Meili: mc,
		Chunker: chunker.New(512, 80), MeiliIndex: "chunks",
	})
	w := queue.NewWorker(q)
	svc.RegisterHandlers(w)
	rig.db, rig.svc, rig.q, rig.w = d, svc, q, w
	return rig
}

func seedFile(t *testing.T, rig *testRig, content string) (key string) {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	key = hex.EncodeToString(sum[:])
	if err := rig.svc.deps.Store.Put(context.Background(), key, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	return key
}

func (rig *testRig) drainJobs(ctx context.Context) {
	for rig.w.RunOnce(ctx) {
	}
}

func TestParsePipelineToReady(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	key := seedFile(t, rig, "# 你好")
	docID, dup, err := rig.svc.CreateDocument(ctx, "kb1", "你好.md", "file", key, "md", key)
	if err != nil || dup {
		t.Fatalf("create: dup=%v err=%v", dup, err)
	}
	rig.drainJobs(ctx)
	doc, err := rig.svc.Get(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != StatusReady || doc.ChunkCount != 1 || doc.Error != "" {
		t.Fatalf("doc = %+v", doc)
	}
	var chunkRows int
	_ = rig.svc.deps.DB.QueryRow(`SELECT COUNT(*) FROM chunks WHERE document_id=?`, docID).Scan(&chunkRows)
	if chunkRows != 1 {
		t.Fatalf("chunk rows = %d", chunkRows)
	}
	if len(rig.meiliDocs) != 1 {
		t.Fatalf("meili add calls = %d", len(rig.meiliDocs))
	}
	var posted []map[string]any
	_ = json.Unmarshal(rig.meiliDocs[0], &posted)
	if posted[0]["kb_id"] != "kb1" || posted[0]["document_id"] != docID {
		t.Fatalf("meili doc = %v", posted[0])
	}
	if _, ok := posted[0]["_vectors"].(map[string]any)["default"]; !ok {
		t.Fatalf("missing vector: %v", posted[0])
	}
}

func TestHashDedup(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	key := seedFile(t, rig, "same content")
	id1, dup1, _ := rig.svc.CreateDocument(ctx, "kb1", "a.md", "file", key, "md", key)
	id2, dup2, err := rig.svc.CreateDocument(ctx, "kb1", "b.md", "file", key, "md", key)
	if err != nil || !dup2 || id1 != id2 || dup1 {
		t.Fatalf("id1=%s id2=%s dup1=%v dup2=%v err=%v", id1, id2, dup1, dup2, err)
	}
	var jobs int
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE type=?`, JobParseDocument).Scan(&jobs)
	if jobs != 1 {
		t.Fatalf("jobs = %d, want 1", jobs)
	}
}

func TestParser422FailsDocumentWithoutRetry(t *testing.T) {
	rig := newRig(t)
	rig.parserCode = 422
	rig.parserBody = `{"error":"pdf: encrypted"}`
	ctx := context.Background()
	key := seedFile(t, rig, "x")
	docID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "x.pdf", "file", key, "pdf", key)
	rig.drainJobs(ctx)
	doc, _ := rig.svc.Get(ctx, docID)
	if doc.Status != StatusFailed || !strings.Contains(doc.Error, "encrypted") {
		t.Fatalf("doc = %+v", doc)
	}
	var pending int
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status IN ('pending','running')`).Scan(&pending)
	if pending != 0 {
		t.Fatalf("should not retry: pending = %d", pending)
	}
}

func TestRetryableErrorExhaustionMarksFailed(t *testing.T) {
	rig := newRig(t)
	rig.parserCode = 502
	rig.parserBody = `{"error":"fetch failed"}`
	rig.q.Now = time.Now // 退避为 1ms,drain 前 sleep 即可
	ctx := context.Background()
	key := seedFile(t, rig, "x")
	docID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "x.md", "file", key, "md", key)
	for i := 0; i < 5; i++ {
		time.Sleep(5 * time.Millisecond)
		rig.drainJobs(ctx)
	}
	doc, _ := rig.svc.Get(ctx, docID)
	if doc.Status != StatusFailed || doc.Error == "" {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestRetryRequeuesFailedDocument(t *testing.T) {
	rig := newRig(t)
	rig.parserCode = 422
	rig.parserBody = `{"error":"broken"}`
	ctx := context.Background()
	key := seedFile(t, rig, "x")
	docID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "x.md", "file", key, "md", key)
	rig.drainJobs(ctx)
	// 修复 parser 后重试
	rig.mu.Lock()
	rig.parserCode, rig.parserBody = 200, `{"title":"doc","blocks":[{"type":"paragraph","text":"恢复"}]}`
	rig.mu.Unlock()
	if err := rig.svc.Retry(ctx, docID); err != nil {
		t.Fatal(err)
	}
	rig.drainJobs(ctx)
	doc, _ := rig.svc.Get(ctx, docID)
	if doc.Status != StatusReady || doc.Error != "" {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestRetryRejectsNonFailed(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	key := seedFile(t, rig, "x")
	docID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "x.md", "file", key, "md", key)
	if err := rig.svc.Retry(ctx, docID); err == nil {
		t.Fatal("pending doc should not retry")
	}
}

func TestDeleteFlowAndReconcile(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	key := seedFile(t, rig, "# 你好")
	docID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "你好.md", "file", key, "md", key)
	rig.drainJobs(ctx)

	if err := rig.svc.Delete(ctx, docID); err != nil {
		t.Fatal(err)
	}
	doc, err := rig.svc.Get(ctx, docID)
	if err != nil || doc.Status != StatusDeleting {
		t.Fatalf("doc = %+v err=%v", doc, err)
	}
	var chunkRows int
	_ = rig.svc.deps.DB.QueryRow(`SELECT COUNT(*) FROM chunks WHERE document_id=?`, docID).Scan(&chunkRows)
	if chunkRows != 0 {
		t.Fatalf("chunks should be cleared at delete request: %d", chunkRows)
	}
	rig.drainJobs(ctx) // 执行 delete_document
	if _, err := rig.svc.Get(ctx, docID); err == nil {
		t.Fatal("document row should be gone")
	}
	wantFilter := fmt.Sprintf("document_id = '%s'", docID)
	found := false
	for _, f := range rig.meiliDels {
		if f == wantFilter {
			found = true
		}
	}
	if !found {
		t.Fatalf("meili delete filters = %v", rig.meiliDels)
	}
	if _, err := rig.svc.deps.Store.Get(ctx, key); err == nil {
		t.Fatal("storage file should be deleted")
	}

	// 对账:手工造一个卡在 deleting 的文档,Reconcile 应重新入队删除
	stuckID := "stuck-doc"
	_, err = rig.svc.deps.DB.Exec(
		`INSERT INTO documents (id, kb_id, title, source_type, source_uri, file_type, status) VALUES (?, 'kb1', 's', 'file', ?, 'md', 'deleting')`,
		stuckID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.EnqueueReconcile(ctx); err != nil {
		t.Fatal(err)
	}
	rig.drainJobs(ctx) // reconcile → 入队 delete_document;再 drain 执行删除
	rig.drainJobs(ctx)
	var cnt int
	_ = rig.svc.deps.DB.QueryRow(`SELECT COUNT(*) FROM documents WHERE id=?`, stuckID).Scan(&cnt)
	if cnt != 0 {
		t.Fatal("reconcile should clean stuck deleting document")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/media/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/media/service.go`:
```go
// Package media 媒体中心:Media 统一模型、生命周期状态机与任务调度。
package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"open-ima/internal/chunker"
	"open-ima/internal/llm"
	"open-ima/internal/meili"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/storage"
)

const (
	StatusPending  = "pending"
	StatusParsing  = "parsing"
	StatusChunking = "chunking"
	StatusIndexing = "indexing"
	StatusReady    = "ready"
	StatusFailed   = "failed"
	StatusDeleting = "deleting"

	JobParseDocument  = "parse_document"
	JobDeleteDocument = "delete_document"
	JobReconcile      = "reconcile"
)

type Document struct {
	ID         string    `json:"id"`
	KBID       string    `json:"kb_id"`
	Title      string    `json:"title"`
	SourceType string    `json:"source_type"`
	SourceURI  string    `json:"source_uri"`
	FileType   string    `json:"file_type"`
	FileHash   string    `json:"file_hash"`
	Status     string    `json:"status"`
	Error      string    `json:"error"`
	ChunkCount int       `json:"chunk_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Deps struct {
	DB         *sql.DB
	Store      storage.Storage
	Queue      *queue.Queue
	Parser     *parserclient.Client
	Embedder   *llm.EmbeddingClient
	Meili      *meili.Client
	Chunker    *chunker.Chunker
	MeiliIndex string
}

type Service struct {
	deps Deps
}

func NewService(d Deps) *Service { return &Service{deps: d} }

func (s *Service) RegisterHandlers(w *queue.Worker) {
	w.Register(JobParseDocument, s.HandleParseDocument)
	w.Register(JobDeleteDocument, s.HandleDeleteDocument)
	w.Register(JobReconcile, s.HandleReconcile)
}

// CreateDocument 建 Media 并入队解析;同库同 hash 直接去重。
func (s *Service) CreateDocument(ctx context.Context, kbID, title, sourceType, sourceURI, fileType, fileHash string) (string, bool, error) {
	if fileHash != "" {
		var existing string
		err := s.deps.DB.QueryRowContext(ctx,
			`SELECT id FROM documents WHERE kb_id = ? AND file_hash = ?`, kbID, fileHash).Scan(&existing)
		if err == nil {
			return existing, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
	}
	id := uuid.NewString()
	_, err := s.deps.DB.ExecContext(ctx,
		`INSERT INTO documents (id, kb_id, title, source_type, source_uri, file_type, file_hash) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, kbID, title, sourceType, sourceURI, fileType, fileHash)
	if err != nil {
		return "", false, err
	}
	if _, err := s.deps.Queue.Enqueue(ctx, JobParseDocument, map[string]string{"document_id": id}); err != nil {
		return "", false, err
	}
	return id, false, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Document, error) {
	row := s.deps.DB.QueryRowContext(ctx,
		`SELECT id, kb_id, title, source_type, source_uri, file_type, file_hash, status, error, chunk_count, created_at, updated_at FROM documents WHERE id = ?`, id)
	return scanDocument(row)
}

func (s *Service) List(ctx context.Context, kbID string) ([]Document, error) {
	rows, err := s.deps.DB.QueryContext(ctx,
		`SELECT id, kb_id, title, source_type, source_uri, file_type, file_hash, status, error, chunk_count, created_at, updated_at FROM documents WHERE kb_id = ? ORDER BY created_at DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDocument(row scanner) (*Document, error) {
	var d Document
	var createdAt, updatedAt string
	err := row.Scan(&d.ID, &d.KBID, &d.Title, &d.SourceType, &d.SourceURI, &d.FileType,
		&d.FileHash, &d.Status, &d.Error, &d.ChunkCount, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	d.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	d.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &d, nil
}

// Retry 失败文档全量重跑(流水线幂等,见计划"已裁决的 spec 偏差"第 1 条)。
func (s *Service) Retry(ctx context.Context, id string) error {
	res, err := s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`,
		StatusPending, id, StatusFailed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("document %s is not in failed status", id)
	}
	_, err = s.deps.Queue.Enqueue(ctx, JobParseDocument, map[string]string{"document_id": id})
	return err
}

// Delete 删除请求:置 deleting、清 chunk 元数据、入队补偿式清理。
func (s *Service) Delete(ctx context.Context, id string) error {
	res, err := s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status != ?`,
		StatusDeleting, id, StatusDeleting)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("document %s not found or already deleting", id)
	}
	if _, err := s.deps.DB.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, id); err != nil {
		return err
	}
	_, err = s.deps.Queue.Enqueue(ctx, JobDeleteDocument, map[string]string{"document_id": id})
	return err
}

func (s *Service) EnqueueReconcile(ctx context.Context) error {
	_, err := s.deps.Queue.Enqueue(ctx, JobReconcile, map[string]string{})
	return err
}

func (s *Service) setStatus(ctx context.Context, id, status string) error {
	_, err := s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
	return err
}

func (s *Service) markFailed(ctx context.Context, id string, cause error) error {
	_, err := s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		StatusFailed, cause.Error(), id)
	return err
}
```

`internal/media/handlers.go`:
```go
package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"open-ima/internal/chunker"
	"open-ima/internal/meili"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
)

type docPayload struct {
	DocumentID string `json:"document_id"`
}

// HandleParseDocument 执行 parsing → chunking → indexing 流水线。
func (s *Service) HandleParseDocument(ctx context.Context, job *queue.Job) error {
	var p docPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return queue.Permanent(fmt.Errorf("bad payload: %w", err))
	}
	doc, err := s.Get(ctx, p.DocumentID)
	if err != nil {
		return queue.Permanent(fmt.Errorf("document %s: %w", p.DocumentID, err))
	}
	if doc.Status == StatusReady || doc.Status == StatusDeleting {
		return nil // 幂等(参照 WeKnora asynq 处理器:ready 跳过、deleting/已删退出、attempt=job.RetryCount)
	}

	fail := func(stage string, err error) error {
		var fatal *parserclient.FatalError
		if errors.As(err, &fatal) {
			_ = s.markFailed(ctx, doc.ID, err)
			return nil // 不可重试:错误落在 document 上,job 正常完成
		}
		if job.RetryCount+1 >= s.deps.Queue.MaxRetries() {
			_ = s.markFailed(ctx, doc.ID, fmt.Errorf("%s: %w", stage, err))
		}
		return err
	}

	// parsing
	if err := s.setStatus(ctx, doc.ID, StatusParsing); err != nil {
		return err
	}
	parsed, err := s.deps.Parser.Parse(ctx, s.deps.Store.URL(doc.SourceURI), doc.FileType)
	if err != nil {
		return fail(StatusParsing, err)
	}

	// chunking
	if err := s.setStatus(ctx, doc.ID, StatusChunking); err != nil {
		return err
	}
	blocks := make([]chunker.Block, len(parsed.Blocks))
	for i, b := range parsed.Blocks {
		blocks[i] = chunker.Block{Type: b.Type, Text: b.Text, Level: b.Level}
	}
	pieces := s.deps.Chunker.Chunk(blocks)
	if len(pieces) == 0 {
		return fail(StatusChunking, &parserclient.FatalError{Message: "no content chunks produced"})
	}
	if _, err := s.deps.DB.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, doc.ID); err != nil {
		return err
	}
	chunkIDs := make([]string, len(pieces))
	texts := make([]string, len(pieces))
	for i, piece := range pieces {
		chunkIDs[i] = uuid.NewString()
		texts[i] = piece.EmbeddingContent() // 标题面包屑前置(WeKnora 同款)
		if _, err := s.deps.DB.ExecContext(ctx,
			`INSERT INTO chunks (id, document_id, kb_id, seq, token_count) VALUES (?, ?, ?, ?, ?)`,
			chunkIDs[i], doc.ID, doc.KBID, i, len([]rune(piece.Content))); err != nil {
			return err
		}
	}

	// indexing
	if err := s.setStatus(ctx, doc.ID, StatusIndexing); err != nil {
		return err
	}
	if err := s.deps.Meili.DeleteByFilter(ctx, s.deps.MeiliIndex,
		fmt.Sprintf("document_id = '%s'", doc.ID)); err != nil {
		return fail(StatusIndexing, err)
	}
	vectors, err := s.deps.Embedder.Embed(ctx, texts)
	if err != nil {
		return fail(StatusIndexing, err)
	}
	meiliDocs := make([]meili.ChunkDoc, len(pieces))
	for i := range pieces {
		meiliDocs[i] = meili.ChunkDoc{
			ID: chunkIDs[i], KBID: doc.KBID, DocumentID: doc.ID,
			Title: doc.Title, Content: pieces[i].Content,
			Vectors: map[string][]float32{"default": vectors[i]},
		}
	}
	if err := s.deps.Meili.AddDocuments(ctx, s.deps.MeiliIndex, meiliDocs); err != nil {
		return fail(StatusIndexing, err)
	}
	_, err = s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', chunk_count = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		StatusReady, len(pieces), doc.ID)
	return err
}

// HandleDeleteDocument 补偿式清理:Meili → 文件 → DB 行。
func (s *Service) HandleDeleteDocument(ctx context.Context, job *queue.Job) error {
	var p docPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return queue.Permanent(fmt.Errorf("bad payload: %w", err))
	}
	doc, err := s.Get(ctx, p.DocumentID)
	if err != nil {
		return nil // 行已不存在,清理完成
	}
	if err := s.deps.Meili.DeleteByFilter(ctx, s.deps.MeiliIndex,
		fmt.Sprintf("document_id = '%s'", doc.ID)); err != nil {
		return err
	}
	if err := s.deps.Store.Delete(ctx, doc.SourceURI); err != nil {
		return err
	}
	_, err = s.deps.DB.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, doc.ID)
	return err
}

// HandleReconcile 对账:卡在 deleting 的文档重新入队清理。
func (s *Service) HandleReconcile(ctx context.Context, _ *queue.Job) error {
	rows, err := s.deps.DB.QueryContext(ctx, `SELECT id FROM documents WHERE status = ?`, StatusDeleting)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if _, err := s.deps.Queue.Enqueue(ctx, JobDeleteDocument, map[string]string{"document_id": id}); err != nil {
			return err
		}
	}
	return rows.Err()
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/media/ -v`
Expected: 7 passed。注意:`rig.svc.deps` 是同包未导出访问(允许);jobs 表断言走 `rig.db`

- [ ] **Step 5: Commit**

```bash
git add internal/media
git commit -m "feat(server): add media center with parse pipeline, delete compensation and reconcile"
```

---

### Task 8: kb service + handlers(KB CRUD + URL 收录)

**Files:**
- Create: `internal/kb/service.go`, `internal/kb/handlers.go`, `internal/kb/service_test.go`

**Interfaces:**
- Consumes: Task 1 `db`;Task 2 `storage.Storage`;Task 7 `media.Service`
- Produces(Task 10 依赖):
  - `kb.KB{ID, Name, Description string; DocCount int; CreatedAt time.Time}`(JSON:`id, name, description, doc_count, created_at`)
  - `kb.NewService(db *sql.DB, mediaSvc *media.Service, store storage.Storage) *Service`
  - `(*Service).Create(ctx, name, description string) (*KB, error)`(重名 → 409 语义:error `ErrNameTaken`)
  - `(*Service).List(ctx) ([]KB, error)`(含各库文档数)
  - `(*Service).Delete(ctx, id string) error`:库内每个 document 调 `media.Delete`;删 conversations/messages;删 KB 行
  - `(*Service).IngestURL(ctx, kbID, rawURL string) (docID string, duplicate bool, err error)`:GET 抓取(超时 10s、限 10MB、非 200 → error)→ sha256 → `store.Put` → `media.CreateDocument(kbID, title, "url", key, "html", hash)`;title = URL 末段非空 path segment,否则 host
  - `(*Service).RegisterRoutes(mux *http.ServeMux)`:注册 `POST /api/kbs`、`GET /api/kbs`、`DELETE /api/kbs/{id}`、`POST /api/kbs/{id}/documents:url`

- [ ] **Step 1: 写失败测试**

`internal/kb/service_test.go`:
```go
package kb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"open-ima/internal/chunker"
	"open-ima/internal/db"
	"open-ima/internal/llm"
	"open-ima/internal/meili"
	"open-ima/internal/media"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/storage"
)

func newMediaForKB(t *testing.T, d *sql.DB) (*media.Service, storage.Storage) {
	t.Helper()
	store, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "s")
	if err != nil {
		t.Fatal(err)
	}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500) // 本测试不执行流水线,外部依赖全部不可达
	}))
	t.Cleanup(stub.Close)
	mc := meili.New(stub.URL, "")
	q := queue.New(d)
	svc := media.NewService(media.Deps{
		DB: d, Store: store, Queue: q,
		Parser: parserclient.New(stub.URL), Embedder: llm.NewEmbeddingClient(stub.URL, "", "m"),
		Meili: mc, Chunker: chunker.New(512, 80), MeiliIndex: "chunks",
	})
	return svc, store
}

func newKBService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	mediaSvc, store := newMediaForKB(t, d)
	return NewService(d, mediaSvc, store), d
}

func TestCreateListDeleteKB(t *testing.T) {
	svc, d := newKBService(t)
	ctx := context.Background()
	k, err := svc.Create(ctx, "工作笔记", "描述")
	if err != nil || k.ID == "" {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Create(ctx, "工作笔记", ""); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("duplicate name err = %v", err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "工作笔记" || list[0].DocCount != 0 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	// 放一个文档再删库:级联
	sum := sha256.Sum256([]byte("x"))
	hash := hex.EncodeToString(sum[:])
	_, err = d.Exec(`INSERT INTO documents (id, kb_id, title, source_type, source_uri, file_type, file_hash) VALUES ('d1', ?, 't', 'file', ?, 'md', ?)`, k.ID, hash, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	var kbCount, docDeleting int
	_ = d.QueryRow(`SELECT COUNT(*) FROM knowledge_bases WHERE id=?`, k.ID).Scan(&kbCount)
	_ = d.QueryRow(`SELECT COUNT(*) FROM documents WHERE kb_id=? AND status='deleting'`, k.ID).Scan(&docDeleting)
	if kbCount != 0 || docDeleting != 1 {
		t.Fatalf("kb=%d deleting=%d", kbCount, docDeleting)
	}
	var delJobs int
	_ = d.QueryRow(`SELECT COUNT(*) FROM jobs WHERE type=?`, media.JobDeleteDocument).Scan(&delJobs)
	if delJobs != 1 {
		t.Fatalf("delete jobs = %d", delJobs)
	}
}

func TestIngestURL(t *testing.T) {
	svc, d := newKBService(t)
	ctx := context.Background()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<html><body><p>网页正文内容</p></body></html>"))
	}))
	defer page.Close()
	docID, dup, err := svc.IngestURL(ctx, "kb9", page.URL+"/articles/hello")
	if err != nil || dup {
		t.Fatalf("ingest: dup=%v err=%v", dup, err)
	}
	// kb9 不存在于 KB 表也能入库(FK 关闭,见计划偏差第 4 条);校验 document 字段
	var title, sourceType, fileType, hash string
	err = d.QueryRow(`SELECT title, source_type, file_type, file_hash FROM documents WHERE id=?`, docID).
		Scan(&title, &sourceType, &fileType, &hash)
	if err != nil {
		t.Fatal(err)
	}
	if title != "hello" || sourceType != "url" || fileType != "html" || len(hash) != 64 {
		t.Fatalf("doc row: title=%q st=%q ft=%q hash=%q", title, sourceType, fileType, hash)
	}
	// 重复收录同内容 → 去重
	_, dup2, err := svc.IngestURL(ctx, "kb9", page.URL+"/articles/hello")
	if err != nil || !dup2 {
		t.Fatalf("re-ingest: dup=%v err=%v", dup2, err)
	}
}

func TestKBHandlers(t *testing.T) {
	svc, _ := newKBService(t)
	mux := http.NewServeMux()
	svc.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/kbs", strings.NewReader(`{"name":"API库","description":"d"}`)))
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	kbID := created["id"].(string)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/kbs", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "API库") {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/kbs/"+kbID, nil))
	if rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/kb/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/kb/service.go`:
```go
// Package kb 知识库服务:KB CRUD 与网页链接收录。
package kb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"open-ima/internal/media"
	"open-ima/internal/storage"
)

var ErrNameTaken = errors.New("knowledge base name already taken")

type KB struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DocCount    int       `json:"doc_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type Service struct {
	db    *sql.DB
	media *media.Service
	store storage.Storage
	hc    *http.Client
}

func NewService(db *sql.DB, mediaSvc *media.Service, store storage.Storage) *Service {
	return &Service{
		db:    db,
		media: mediaSvc,
		store: store,
		hc:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) Create(ctx context.Context, name, description string) (*KB, error) {
	k := &KB{ID: uuid.NewString(), Name: name, Description: description, CreatedAt: time.Now()}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO knowledge_bases (id, name, description) VALUES (?, ?, ?)`, k.ID, name, description)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrNameTaken
		}
		return nil, err
	}
	return k, nil
}

func (s *Service) List(ctx context.Context) ([]KB, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT k.id, k.name, k.description, k.created_at,
		       (SELECT COUNT(*) FROM documents d WHERE d.kb_id = k.id) AS doc_count
		FROM knowledge_bases k ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KB
	for rows.Next() {
		var k KB
		var createdAt string
		if err := rows.Scan(&k.ID, &k.Name, &k.Description, &createdAt, &k.DocCount); err != nil {
			return nil, err
		}
		k.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
		out = append(out, k)
	}
	return out, rows.Err()
}

// Delete 级联删除:文档走 media 补偿式清理,KB 行立即删(单机应用层完整性,FK 关闭)。
func (s *Service) Delete(ctx context.Context, id string) error {
	docs, err := s.media.List(ctx, id)
	if err != nil {
		return err
	}
	for _, d := range docs {
		if d.Status == media.StatusDeleting {
			continue
		}
		if err := s.media.Delete(ctx, d.ID); err != nil {
			return fmt.Errorf("delete document %s: %w", d.ID, err)
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM messages WHERE conversation_id IN (SELECT id FROM conversations WHERE kb_id = ?)`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE kb_id = ?`, id); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM knowledge_bases WHERE id = ?`, id)
	return err
}

const maxURLBytes = 10 << 20 // 10MB

// IngestURL 抓取网页落盘为 html 并建 Media。
func (s *Service) IngestURL(ctx context.Context, kbID, rawURL string) (string, bool, error) {
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return "", false, fmt.Errorf("invalid url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "open-ima/1.0")
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxURLBytes+1))
	if err != nil {
		return "", false, err
	}
	if len(content) > maxURLBytes {
		return "", false, fmt.Errorf("fetch %s: page exceeds 10MB limit", rawURL)
	}
	sum := sha256.Sum256(content)
	key := hex.EncodeToString(sum[:])
	if err := s.store.Put(ctx, key, strings.NewReader(string(content))); err != nil {
		return "", false, err
	}
	return s.media.CreateDocument(ctx, kbID, titleFromURL(rawURL), "url", key, "html", key)
}

func titleFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if last := parts[len(parts)-1]; last != "" {
		return last
	}
	return u.Host
}
```

`internal/kb/handlers.go`:
```go
package kb

import (
	"encoding/json"
	"errors"
	"net/http"

	"open-ima/internal/httpx"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kbs", s.handleCreate)
	mux.HandleFunc("GET /api/kbs", s.handleList)
	mux.HandleFunc("DELETE /api/kbs/{id}", s.handleDelete)
	mux.HandleFunc("POST /api/kbs/{id}/documents:url", s.handleIngestURL)
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	k, err := s.Create(r.Context(), req.Name, req.Description)
	if errors.Is(err, ErrNameTaken) {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, k)
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	list, err := s.List(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Delete(r.Context(), r.PathValue("id")); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleIngestURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		httpx.Error(w, http.StatusBadRequest, "url is required")
		return
	}
	docID, duplicate, err := s.IngestURL(r.Context(), r.PathValue("id"), req.URL)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"document_id": docID, "duplicate": duplicate})
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/kb/`
Expected: 3 passed

- [ ] **Step 5: Commit**

```bash
git add internal/kb
git commit -m "feat(server): add knowledge base service with url ingestion"
```

---

### Task 9: upload handler(multipart 上传)

**Files:**
- Create: `internal/upload/handler.go`, `internal/upload/handler_test.go`

**Interfaces:**
- Consumes: Task 2 `storage.Storage`;Task 7 `media.Service`;Task 1 `httpx`
- Produces(Task 10 依赖):
  - `upload.NewHandler(mediaSvc *media.Service, store storage.Storage) *Handler`;字段 `MaxBytes int64`(默认 `50<<20`)
  - `(*Handler).RegisterRoutes(mux *http.ServeMux)`:注册 `POST /api/kbs/{id}/documents`
  - 扩展名白名单:`.pdf→pdf .docx→docx .pptx→pptx .md→md .txt→txt .html/.htm→html`
  - 响应:`202 {"document_id": ..., "duplicate": bool}`;类型不允许 400;超限 400;body 读取/hash/落盘与 Task 7 的 `CreateDocument` 衔接

- [ ] **Step 1: 写失败测试**

`internal/upload/handler_test.go`:
```go
package upload

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"open-ima/internal/chunker"
	"open-ima/internal/db"
	"open-ima/internal/llm"
	"open-ima/internal/meili"
	"open-ima/internal/media"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/storage"
)

func newUploadRig(t *testing.T) (*Handler, *sql.DB) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO knowledge_bases (id, name) VALUES ('kb1', 'k')`); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "s")
	if err != nil {
		t.Fatal(err)
	}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(stub.Close)
	mediaSvc := media.NewService(media.Deps{
		DB: d, Store: store, Queue: queue.New(d),
		Parser: parserclient.New(stub.URL), Embedder: llm.NewEmbeddingClient(stub.URL, "", "m"),
		Meili: meili.New(stub.URL, ""), Chunker: chunker.New(512, 80), MeiliIndex: "chunks",
	})
	return NewHandler(mediaSvc, store), d
}

func multipartBody(t *testing.T, field, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(fw, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

func TestUploadAccepted(t *testing.T) {
	h, d := newUploadRig(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	body, ct := multipartBody(t, "file", "笔记.md", "# 标题\n\n正文")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/kbs/kb1/documents", body))
	_ = ct
	req := httptest.NewRequest("POST", "/api/kbs/kb1/documents", body)
	req.Header.Set("Content-Type", ct)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		DocumentID string `json:"document_id"`
		Duplicate  bool   `json:"duplicate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.DocumentID == "" || resp.Duplicate {
		t.Fatalf("resp = %+v err=%v", resp, err)
	}
	var status, fileType string
	_ = d.QueryRow(`SELECT status, file_type FROM documents WHERE id=?`, resp.DocumentID).Scan(&status, &fileType)
	if status != "pending" || fileType != "md" {
		t.Fatalf("status=%s type=%s", status, fileType)
	}
	var jobCount int
	_ = d.QueryRow(`SELECT COUNT(*) FROM jobs WHERE type='parse_document'`).Scan(&jobCount)
	if jobCount != 1 {
		t.Fatalf("jobs = %d", jobCount)
	}
	// 同内容再传 → duplicate=true 且不新增 job
	body2, ct2 := multipartBody(t, "file", "改名.md", "# 标题\n\n正文")
	req2 := httptest.NewRequest("POST", "/api/kbs/kb1/documents", body2)
	req2.Header.Set("Content-Type", ct2)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	var resp2 struct {
		DocumentID string `json:"document_id"`
		Duplicate  bool   `json:"duplicate"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if !resp2.Duplicate || resp2.DocumentID != resp.DocumentID {
		t.Fatalf("dedup resp = %+v", resp2)
	}
}

func TestUploadRejectsBadExtension(t *testing.T) {
	h, _ := newUploadRig(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	body, ct := multipartBody(t, "file", "evil.exe", "MZ")
	req := httptest.NewRequest("POST", "/api/kbs/kb1/documents", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("code = %d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/upload/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/upload/handler.go`:
```go
// Package upload 文件上传管理服务:校验、去重、落盘、移交媒体中心。
package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"open-ima/internal/httpx"
	"open-ima/internal/media"
	"open-ima/internal/storage"
)

var allowedExt = map[string]string{
	".pdf":  "pdf",
	".docx": "docx",
	".pptx": "pptx",
	".md":   "md",
	".txt":  "txt",
	".html": "html",
	".htm":  "html",
}

type Handler struct {
	media    *media.Service
	store    storage.Storage
	MaxBytes int64
}

func NewHandler(mediaSvc *media.Service, store storage.Storage) *Handler {
	return &Handler{media: mediaSvc, store: store, MaxBytes: 50 << 20}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/kbs/{id}/documents", h.handleUpload)
}

func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, h.MaxBytes)
	if err := r.ParseMultipartForm(h.MaxBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	fileType, ok := allowedExt[ext]
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "unsupported file type: "+ext)
		return
	}
	// 边写临时文件边算 hash
	tmp, err := os.CreateTemp("", "open-ima-upload-*")
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), file); err != nil {
		tmp.Close()
		httpx.Error(w, http.StatusBadRequest, "file too large or read failed")
		return
	}
	if err := tmp.Close(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := hex.EncodeToString(hasher.Sum(nil))
	f, err := os.Open(tmp.Name())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	if err := h.store.Put(r.Context(), key, f); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	title := strings.TrimSuffix(header.Filename, ext)
	docID, duplicate, err := h.media.CreateDocument(r.Context(), kbID, title, "file", key, fileType, key)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"document_id": docID, "duplicate": duplicate})
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/upload/`
Expected: 2 passed

- [ ] **Step 5: Commit**

```bash
git add internal/upload
git commit -m "feat(server): add multipart upload handler with hash dedup"
```

---

### Task 10: server 装配 + 端到端集成测试 + dev-up 脚本

**Files:**
- Create: `internal/server/server.go`, `internal/server/server_test.go`
- Modify: `cmd/server/main.go`(改用 server.New + worker + reconcile ticker)
- Create: `scripts/dev-up.sh`

**Interfaces:**
- Consumes: Task 1-9 全部
- Produces(P3/P4/P5 依赖):
  - `server.New(cfg *config.Config, database *sql.DB) (*Server, error)`
  - `Server` 字段:`Handler http.Handler; Worker *queue.Worker; Media *media.Service; Queue *queue.Queue; KB *kb.Service`
  - 启动时 `meili.EnsureIndex(cfg.Meili.Index, cfg.Embedding.Dimensions)`(失败返回 error)
  - 路由(spec §9 一致):`GET /health`;kb 四条(Task 8);`POST /api/kbs/{id}/documents`(Task 9);`GET /api/kbs/{id}/documents`;`DELETE /api/documents/{id}`;`POST /api/documents/{id}/retry`;`GET /internal/files/{key}`
  - `main.go`:worker `Start(ctx, cfg.Worker.Concurrency)`;每 10min `Media.EnqueueReconcile`

- [ ] **Step 1: 写失败测试(全链路:mock parser/meili/embedding,真实 SQLite+storage+queue)**

`internal/server/server_test.go`:
```go
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"open-ima/internal/config"
	"open-ima/internal/db"
)

type extMocks struct {
	mu        sync.Mutex
	meiliDocs []map[string]any
}

func newExtMocks(t *testing.T) (*extMocks, *config.Config) {
	t.Helper()
	m := &extMocks{}
	parserSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"title":"集成测试文档","blocks":[{"type":"heading","text":"第一章","level":1},{"type":"paragraph","text":"这是正文内容,用于验证端到端入库链路。"}]}`))
	}))
	t.Cleanup(parserSrv.Close)
	meiliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			w.Write([]byte(`{"status":"succeeded"}`))
			return
		}
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/indexes/") && !strings.HasSuffix(r.URL.Path, "/settings") {
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"not found"}`))
			return
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/documents") {
			var docs []map[string]any
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &docs)
			m.mu.Lock()
			m.meiliDocs = append(m.meiliDocs, docs...)
			m.mu.Unlock()
		}
		w.WriteHeader(202)
		w.Write([]byte(`{"taskUid":1}`))
	}))
	t.Cleanup(meiliSrv.Close)
	embSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": []float64{0.1, 0.2, 0.3}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(embSrv.Close)
	cfg := &config.Config{
		DataDir:       filepath.Join(t.TempDir(), "data"),
		PublicBaseURL: "http://app:8080",
	}
	cfg.Parser.URL = parserSrv.URL
	cfg.Meili.URL = meiliSrv.URL
	cfg.Meili.Index = "chunks"
	cfg.Embedding.BaseURL = embSrv.URL
	cfg.Embedding.Model = "test"
	cfg.Embedding.Dimensions = 3
	cfg.Worker.Concurrency = 1
	return m, cfg
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

func TestEndToEndIngestion(t *testing.T) {
	m, cfg := newExtMocks(t)
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	srv, err := New(cfg, database)
	if err != nil {
		t.Fatal(err)
	}
	srv.Worker.PollInterval = time.Millisecond

	// 建库
	rec, kb := doJSON(t, srv.Handler, "POST", "/api/kbs", map[string]string{"name": "e2e库"})
	if rec.Code != 201 {
		t.Fatalf("create kb: %d", rec.Code)
	}
	kbID := kb["id"].(string)

	// 上传 md
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "测试.md")
	fw.Write([]byte("# 标题\n\n正文"))
	w.Close()
	req := httptest.NewRequest("POST", "/api/kbs/"+kbID+"/documents", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var up struct {
		DocumentID string `json:"document_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &up)

	// 驱动 worker 直到文档 ready
	deadline := time.Now().Add(5 * time.Second)
	var docs []map[string]any
	for {
		srv.Worker.RunOnce(context.Background())
		rec, _ = doJSON(t, srv.Handler, "GET", "/api/kbs/"+kbID+"/documents", nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &docs)
		if len(docs) == 1 && docs[0]["status"] == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("document never became ready: %v", docs)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if docs[0]["id"] != up.DocumentID || docs[0]["chunk_count"].(float64) != 1 {
		t.Fatalf("doc = %v", docs[0])
	}

	// Meili 收到带向量的 chunk
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.meiliDocs) != 1 || m.meiliDocs[0]["kb_id"] != kbID {
		t.Fatalf("meili docs = %v", m.meiliDocs)
	}
	if _, ok := m.meiliDocs[0]["_vectors"].(map[string]any)["default"]; !ok {
		t.Fatalf("no vector: %v", m.meiliDocs[0])
	}

	// 删除文档 → 清理完成
	rec, _ = doJSON(t, srv.Handler, "DELETE", "/api/documents/"+up.DocumentID, nil)
	if rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	srv.Worker.RunOnce(context.Background())
	rec, _ = doJSON(t, srv.Handler, "GET", "/api/kbs/"+kbID+"/documents", nil)
	if rec.Body.String() != "null\n" && rec.Body.String() != "[]\n" {
		t.Fatalf("docs after delete: %s", rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	_, cfg := newExtMocks(t)
	database, _ := db.Open(":memory:")
	defer database.Close()
	srv, err := New(cfg, database)
	if err != nil {
		t.Fatal(err)
	}
	rec, body := doJSON(t, srv.Handler, "GET", "/health", nil)
	if rec.Code != 200 || body["status"] != "ok" {
		t.Fatalf("health: %d %v", rec.Code, body)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/server/`
Expected: FAIL,包不存在

- [ ] **Step 3: 实现**

`internal/server/server.go`:
```go
// Package server 装配全部 HTTP 路由与后台组件。
package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"

	"open-ima/internal/chunker"
	"open-ima/internal/config"
	"open-ima/internal/httpx"
	"open-ima/internal/kb"
	"open-ima/internal/llm"
	"open-ima/internal/media"
	"open-ima/internal/meili"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/storage"
	"open-ima/internal/upload"
)

type Server struct {
	Handler http.Handler
	Worker  *queue.Worker
	Media   *media.Service
	Queue   *queue.Queue
	KB      *kb.Service
}

func New(cfg *config.Config, database *sql.DB) (*Server, error) {
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	store, err := storage.NewLocalStorage(
		filepath.Join(cfg.DataDir, "files"), cfg.PublicBaseURL, hex.EncodeToString(secret))
	if err != nil {
		return nil, err
	}
	mc := meili.New(cfg.Meili.URL, cfg.Meili.APIKey)
	if err := mc.EnsureIndex(contextTODO(), cfg.Meili.Index, cfg.Embedding.Dimensions); err != nil {
		return nil, fmt.Errorf("meilisearch ensure index: %w", err)
	}
	q := queue.New(database)
	mediaSvc := media.NewService(media.Deps{
		DB: database, Store: store, Queue: q,
		Parser:   parserclient.New(cfg.Parser.URL),
		Embedder: llm.NewEmbeddingClient(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.Model),
		Meili:    mc, Chunker: chunker.New(512, 80), MeiliIndex: cfg.Meili.Index,
	})
	worker := queue.NewWorker(q)
	mediaSvc.RegisterHandlers(worker)
	kbSvc := kb.NewService(database, mediaSvc, store)
	uploadH := upload.NewHandler(mediaSvc, store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, 200, map[string]string{"status": "ok"})
	})
	kbSvc.RegisterRoutes(mux)
	uploadH.RegisterRoutes(mux)
	mux.HandleFunc("GET /api/kbs/{id}/documents", func(w http.ResponseWriter, r *http.Request) {
		docs, err := mediaSvc.List(r.Context(), r.PathValue("id"))
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		httpx.JSON(w, 200, docs)
	})
	mux.HandleFunc("DELETE /api/documents/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := mediaSvc.Delete(r.Context(), r.PathValue("id")); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/documents/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if err := mediaSvc.Retry(r.Context(), r.PathValue("id")); err != nil {
			httpx.Error(w, 409, err.Error())
			return
		}
		httpx.JSON(w, 202, map[string]string{"status": "requeued"})
	})
	mux.Handle("GET /internal/files/{key}", store.Handler())

	return &Server{Handler: mux, Worker: worker, Media: mediaSvc, Queue: q, KB: kbSvc}, nil
}
```

注意:`contextTODO()` 是笔误——实现时用 `context.Background()` 并 import `context`。`EnsureIndex` 在 `New` 中同步执行;集成测试的 fake Meili 对 `GET /indexes/chunks` 返回 404,会走 POST 创建路径,fake 已覆盖。

`cmd/server/main.go`(全量替换):
```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"open-ima/internal/config"
	"open-ima/internal/db"
	"open-ima/internal/server"
)

func main() {
	cfg, err := config.Load(os.Getenv("IMA_CONFIG"))
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()
	srv, err := server.New(cfg, database)
	if err != nil {
		log.Fatalf("build server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Worker.Start(ctx, cfg.Worker.Concurrency)
	go func() { // 对账定时器(每 10 分钟,对标文章 1 异步对账)
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := srv.Media.EnqueueReconcile(ctx); err != nil {
					log.Printf("reconcile enqueue: %v", err)
				}
			}
		}
	}()
	log.Printf("open-ima listening on %s", cfg.HTTPAddr)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, srv.Handler))
}
```

`scripts/dev-up.sh`:
```bash
#!/usr/bin/env bash
# 本地开发依赖启动(docker 不可用时的进程模式;P5 提供 compose 一键模式)。
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> 1/3 Meilisearch"
if command -v meilisearch >/dev/null; then
  (meilisearch --http-addr 127.0.0.1:7700 --no-analytics --db-path ./data/meili &) 
elif command -v docker >/dev/null; then
  (docker run --rm -p 7700:7700 -v "$PWD/data/meili:/meili_data" getmeili/meilisearch:v1.10 &)
else
  echo "!! 未找到 meilisearch 二进制或 docker。安装:curl -L https://install.meilisearch.com | sh" >&2
  exit 1
fi

echo "==> 2/3 parser sidecar"
if [ ! -d parser/.venv ]; then
  (cd parser && python3 -m venv .venv && .venv/bin/pip install -r requirements.txt)
fi
(cd parser && .venv/bin/uvicorn app.main:app --port 8100 &)

echo "==> 3/3 app"
echo "配置 LLM/Embedding 后运行: IMA_LLM_API_KEY=... IMA_EMBEDDING_API_KEY=... go run ./cmd/server"
```

- [ ] **Step 4: 跑测试确认通过 + 全量回归**

Run: `chmod +x scripts/dev-up.sh && go test ./... && CGO_ENABLED=0 go build ./... && go vet ./...`
Expected: 全部 PASS,构建/vet 干净

- [ ] **Step 5: Commit**

```bash
git add internal/server cmd scripts/dev-up.sh
git commit -m "feat(server): wire routes, worker and reconcile ticker; add e2e ingestion test"
```

---

## Self-Review 结论(编写时已完成)

- **Spec 覆盖**:§3.1 kb(Task 8)、§3.2 upload(Task 9)、§3.3 media 状态机/任务调度/删除补偿/对账(Task 7,偏差已裁决)、§3.4 storage(Task 2)、§5.1 DDL(Task 1,逐字)、§5.2 Meili settings(Task 5)、§6.1 入库流水线(Task 7/9/10)、§6.2 删除+对账(Task 7/10)、§7 错误处理(422 不重试/退避重试/stale 重置/对账,Task 3/7)、§9 API 中入库相关 7 条(Task 8/9/10)、§10 配置(Task 1,扩展已裁决)。RAG(§6.3)、chat/search API、对话历史属 P3;前端属 P4;compose/冒烟属 P5——不在本计划。
- **接口一致性**:`queue.Job/Queue/Worker/PermanentError`、`media.Deps/Service` 方法名、`meili.ChunkDoc`、`parserclient.FatalError`、`chunker.Block/Chunk/EmbeddingContent`、`kb.ErrNameTaken` 在 Task 间逐一核对一致;`rig.db` 修正后无跨包未导出访问。
- **Placeholder 扫描**:无 TBD/TODO 残留(`contextTODO()` 已在计划内明确指出为笔误并给出正确写法——执行时以文字说明为准);每个代码步骤含完整代码。
