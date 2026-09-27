# Open IMA

Open IMA 是一个本地优先的个人知识工作台。它支持上传文档或收录网页、异步解析与索引、文本/混合检索，以及带引用的流式 RAG 问答。

## 组成

- Go 单体服务：HTTP API、SQLite 元数据、任务队列、入库流水线、检索与对话。
- Python parser sidecar：解析 PDF、DOCX、PPTX、Markdown、文本和 HTML。
- Meilisearch：全文与向量混合检索。
- React SPA：知识库、文档、搜索和对话工作区，由 Go 二进制内嵌托管。
- OpenAI-compatible API：配置聊天模型。
- Ollama `bge-m3`：由 Meilisearch 直接调用并生成、查询向量。

## Docker 启动

```bash
cp .env.example .env
# 在 .env 中填写 IMA_LLM_API_KEY 和需要的聊天模型 provider URL
docker compose up -d --build
```

打开 <http://localhost:8080>。运行状态可通过 `docker compose ps` 和 `curl http://localhost:8080/health` 检查。

默认数据保存在 `data/app` 和 `data/meili`。生产或共享环境请修改 `.env` 中的 Meilisearch key，不要提交真实密钥。

## 本地开发

需要 Go 1.26、Node.js 20+、Python 3.11+、Ollama，以及本地 Meilisearch。

```bash
# parser
python3 -m venv parser/.venv
parser/.venv/bin/pip install -r parser/requirements-dev.txt

# frontend
npm --prefix web ci
npm --prefix web run dev

# dependencies and backend
brew install ollama
brew services start ollama
ollama pull bge-m3
./scripts/dev-up.sh
go run ./cmd/server
```

应用配置使用 `IMA_` 环境变量。完整示例见 [.env.example](.env.example)，关键项包括 `IMA_LLM_PROTOCOL`、`IMA_LLM_BASE_URL`、`IMA_LLM_API_KEY`、`IMA_MEILI_EMBEDDER_URL`、`IMA_MEILI_EMBEDDER_MODEL` 和 `IMA_MEILI_EMBEDDER_DIMENSIONS`。`IMA_LLM_PROTOCOL` 支持 `openai` 和 `anthropic`；Meilisearch 1.10.3 需要 Ollama 的兼容端点 `/api/embeddings`。

Kimi Coding 两种协议示例：

```bash
# OpenAI Chat Completions
IMA_LLM_PROTOCOL=openai
IMA_LLM_BASE_URL=https://api.kimi.com/coding/v1

# Anthropic Messages
IMA_LLM_PROTOCOL=anthropic
IMA_LLM_BASE_URL=https://api.kimi.com/coding/

IMA_LLM_MODEL=kimi-for-coding
```

可用同一把本地 key 显式验证两种协议：`KIMI_TEST_API_KEY="$IMA_LLM_API_KEY" go test ./internal/llm -run TestKimiCompatibleProtocols -v`。测试默认跳过，不会在常规门禁中调用外部模型。

## 验证

```bash
./scripts/harness.sh

# 默认直接启动本地进程和确定性 mock，完成 HTTP 端到端断言
./scripts/smoke.sh

# 推荐：把本机 Meilisearch 放到项目内的忽略目录，脚本会自动使用
mkdir -p .local/bin
curl -L --fail -o .local/bin/meilisearch \
  https://github.com/meilisearch/meilisearch/releases/download/v1.10.3/meilisearch-macos-apple-silicon
chmod +x .local/bin/meilisearch
./scripts/smoke.sh

# 也可显式指定其他本机 Meilisearch 二进制
SMOKE_MEILI_BIN=/path/to/meilisearch ./scripts/smoke.sh

```

Business E2E 使用真实 Meilisearch 和本地 Ollama embedding，覆盖知识库、文件与 URL 入库、失败重试、文本/混合检索、对话历史、删除清理、错误状态和内嵌 SPA。

## 重建索引

embedding 模型、维度或索引设置变化后，可重新投递所有有效文档：

```bash
./scripts/reindex.sh
```

该命令会把文档重置为待处理状态并加入解析队列；随后由正常 worker 流水线重新解析、分块、向量化和索引。

## 目录

```text
cmd/server/       Go 服务入口
cmd/reindex/      全量重建索引工具
internal/         后端领域与基础设施包
parser/           Python 解析 sidecar
web/              React SPA 与 Go embed
scripts/          harness、smoke 和开发脚本
design/           V1 设计文档
docs/plans/       分阶段实施与验收记录
```
