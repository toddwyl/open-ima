# open-ima Copilot Agent 模块设计(V2 增量)

**状态**:待评审
**上游文档**:[open-ima 技术方案(V1)](2026-09-26-open-ima-v1-design.md)
**对标基线**:WeKnora 本地源码 `~/work/WeKnora`(2026-09-28 工作副本;核心参考 `internal/agent/engine.go`、`internal/types/agent.go`、`internal/agent/tools/`、`internal/event/event.go`、`internal/handler/session/agent_stream_handler.go`)

---

## 1. 背景与差距

V1 的 `application/chat` 是固定管线:改写 → 双路检索 → Go 侧 RRF 融合 → 单次流式生成 → 落库。模型没有"决定搜不搜、搜知识库还是搜网络、结果不够要不要补搜"的决策权,`port.ChatModel` 也没有工具调用能力。

ima copilot(及 WeKnora 智能推理模式)的核心是 **agent 模块**:统一知识库与外部数据源(联网搜索),由模型在多轮循环中规划与执行。这是当前与对标产品最大的未对齐部分,本设计补齐这一块。

**结论:采用多轮 ReAct(function calling)方案,尽量对齐 WeKnora 的引擎结构、工具命名与事件契约。** 已否决的备选:单次 JSON 路由规划(无迭代能力,不对齐)。

## 2. 对标结论(WeKnora 源码事实)

### 2.1 双模式并存

WeKnora 问答分两种模式,同一套知识库与会话体系:

- **快速问答(quick answer)**:RAG 管线 + 引用,即我们 V1 已有能力;
- **智能推理(smart reasoning)**:ReAct agent,多步规划执行,逐步展示。

我们沿用这一划分:V1 的 RAG 快问保留为默认模式,copilot 智能推理作为可切换的新模式(对齐 WeKnora "对话策略配置"的产品形态)。

### 2.2 ReAct 引擎结构(`internal/agent/engine.go`)

一次请求 = 一个 turn;`AgentEngine.Execute` → `executeLoop` → `runReActIteration`,每轮四阶段:

1. **Think**:携带工具定义调用 LLM(function calling);
2. **Analyze**:判定终止条件——无 tool_calls 且自然停止 = 最终答案;含 tool_calls 的纯文本是 preamble("let me look that up"),归入该轮 Thought,不当答案;
3. **Act**:并发执行本轮全部 tool_calls;
4. **Observe**:工具结果作为 tool 消息追加进上下文,进入下一轮。

引擎跨 turn 无状态:历史每 turn 从 DB 重建后传入(注释明确 "The engine is stateless across turns")。终止时恰好一次 `agent.complete` 事件,步骤轨迹写入 assistant 消息的 `AgentSteps` 持久化。

### 2.3 循环守护(`loopGuards` + `const.go`)

| 守护 | 阈值(WeKnora 默认) | 行为 |
|---|---|---|
| MaxIterations | 20(可配,-1 不限) | 超轮次未给出答案 → 用已有上下文强制生成兜底答案 |
| maxEmptyResponseRetries | 2 | 自然停止但内容为空 → 注入 "请给出完整答案" 重试 |
| maxRepeatedResponseRounds | 2 | 连续纯文本轮内容相同(卡死)→ 停止,末轮文本或兜底文案作答 |
| maxConsecutiveLengthRounds | 3 | 连续轮被 completion token 上限截断 → 停止,标记 truncated |
| ctx cancel | — | 用户停止:有工具结果则合成答案,否则终止;`agent.complete` 用 `WithoutCancel` 保证恰好发一次 |

### 2.4 核心类型(`internal/types/agent.go`)

- `AgentState{CurrentRound, RoundSteps, IsComplete, FinalAnswer, KnowledgeRefs, TurnUsage}`;
- `AgentStep{Iteration, Thought, ReasoningContent, ReasoningSignature, ToolCalls, Timestamp, Truncated}`;
- `ToolCall{ID, Name, Args, Result, Duration, Reflection}`;
- `ToolResult{Success, Output, Data, Error, Images}`;
- `Tool` 接口:`Name() / Description() / Parameters() json.RawMessage / Execute(ctx, args) (*ToolResult, error)`;
- `ToolRegistry`:注册 first-wins(防同名劫持),输出统一限长(`MaxToolOutputChars`,默认 16000,超长 head+tail 截断)。

### 2.5 工具集(命名直接对齐)

WeKnora v0.8 将检索面收敛为三个工具 + 联网开关:

- `search_knowledge(query, mode=hybrid|semantic|keyword, knowledge_base_ids?, limit)`——三分块返回 cN/dN 句柄;
- `read_document(id, offset/limit/query)`——按文档或分块读正文;
- `list_documents(keyword, page)`——浏览/按标题找文档;
- `web_search`——由 `WebSearchEnabled` 开关控制注入,不进入可选工具清单。

## 3. 目标与非目标

### 目标(V2)

1. 新增 **copilot(智能推理)模式**,与 RAG 快问并存、前端可切换;
2. **多轮 ReAct 循环**:模型自主决策调用知识库检索 / 联网搜索 / 读文档,迭代至给出最终答案;
3. **SSE 步骤事件流**:思考、工具调用、工具结果、引用、最终答案分事件推送,前端逐步展示;
4. **步骤轨迹持久化**:历史会话可回看每轮 thought 与工具调用;
5. **联网搜索可配置**:provider 进设置中心。

### 非目标(V2 不做,留演进位)

- MCP 服务、技能/沙箱、本地浏览器、长期记忆、wiki/知识图谱(对标 WeKnora 的 P3 生态面);
- 上下文压缩(compaction)、运行中 steering(追加要求)、thought 逐 token 流式(P2);
- 多知识库同时检索(copilot 仍以单库会话为边界,工具内 `knowledge_base_ids` 退化为当前库)。

## 4. 总体架构与分层落位

```
interfaces/http/copilot.go        # POST /api/kbs/{id}/copilot → SSE
application/copilot/
  engine.go                       # ReAct 主循环(think→analyze→act→observe)
  state.go                        # AgentState / AgentStep / ToolCall / ToolResult(领域语义)
  tools/registry.go               # ToolRegistry(first-wins)
  tools/search_knowledge.go       # 包装 port.Searcher
  tools/read_document.go          # 读分块正文(DB + Searcher)
  tools/list_documents.go         # 媒体列表(domain/media)
  tools/web_search.go             # 包装 port.WebSearcher
application/port/
  chat.go                         # ChatModel 增加 CompleteTools;新增 ChatResponse/ToolCall/ToolDef
  tool.go                         # Tool 接口 + ToolResult(新文件)
  websearch.go                    # WebSearcher 端口(新文件)
domain/conversation               # Message 增加 AgentSteps、citation source_type/url
infrastructure/websearch/         # duckduckgo / searxng 实现(SSRF 收敛)
infrastructure/llm/               # OpenAI tools 协议;anthropic tool_use 映射同构类型
```

依赖方向遵守 `internal/app/architecture_test.go`:`copilot` 依赖 `port`、`domain` 与 `pkg`;工具实现位于 application 层内部(与 WeKnora 一致,工具是编排的一部分,不构成新分层)。

## 5. ReAct Agent 引擎设计

### 5.1 入口与时序

```
POST /api/kbs/{id}/copilot {conversation_biz_id?, model_biz_id?, query}
  ↓ SSE
1. 会话落 user 消息(与 RAG 模式同一套 conversation/message 模型)
2. 从历史重建 llmContext(引擎跨 turn 无状态)
3. engine.Run(ctx, query, llmContext):
     for round < max_iterations:
       resp = model.CompleteTools(messages, toolDefs)      # Think
       if 无 tool_calls 且自然停止:                          # Analyze
           流式生成即最终答案 → 推 token → break             # final answer
       thought 事件推送(resp.Content 作本轮 Thought)
       并发执行 tool_calls → tool_call / tool_result 事件    # Act
       结果截断至预算,作为 tool 消息追加 messages            # Observe
     超轮次:用已有上下文非流式生成兜底答案
4. references 事件(KB 分块 + 网页统一引用)→ citations 落库
5. done 事件(带 conversation_biz_id / rounds / citations)
```

### 5.2 关键语义(逐项对齐 WeKnora)

- **preamble**:含 tool_calls 的轮次,其纯文本是"让我查一下"式前导,只作 Thought 展示,绝不作为答案;
- **答案流式**:自然停止的纯文本轮边生成边推 `token`,结束时 `done` 不再重复推送全文(避免结尾"跳变");
- **取消**:ctx 取消时若已有工具结果则合成答案收尾,否则终止;`done` 必须恰好一次;
- **截断标记**:被 completion 上限截断的答案在 done 数据中携带 `truncated`,历史回放时仍显示"戛然而止"而非假装完整;
- **工具并发**:同一轮多个 tool_calls 并发执行(WeKnora `ParallelToolCalls`);
- **工具输出预算**:默认 16000 字符,超长 head+tail 保留截断。

### 5.3 守护配置(默认值对齐 WeKnora §2.3)

| 配置项 | 默认 | 说明 |
|---|---|---|
| `copilot_max_iterations` | 20 | ReAct 轮次上限 |
| `copilot_max_tool_output_chars` | 16000 | 单工具输出预算 |
| `copilot_max_completion_tokens` | 4096 | 单轮 completion 预算(无沙箱写文件场景) |
| `copilot_llm_call_timeout` | 120s | 单次 LLM 调用超时 |

## 6. 工具集设计(命名与 schema 对齐 WeKnora)

### 6.1 工具清单

| 工具 | 参数 | 实现 | 注入条件 |
|---|---|---|---|
| `search_knowledge` | `query`, `mode`(hybrid/keyword/semantic), `limit`(默认 10) | 包装 `port.Searcher`,filter 固定当前 KB | 总是 |
| `read_document` | `id`(媒体或分块句柄), `query?`(文内定位), `offset/limit?` | DB 读分块内容 | 总是 |
| `list_documents` | `keyword?`, `page?` | `domain/media` 列表 | 总是 |
| `web_search` | `query`, `limit`(默认取设置) | 包装 `port.WebSearcher` | `web_search_enabled=true` |

### 6.2 语义映射说明

- **mode 映射**:Meilisearch 侧 `keyword` → text 检索;`hybrid`/`semantic` → hybrid(semanticRatio 0.5)。引擎不支持所请求 mode 时在结果中注明 fallback(对齐 WeKnora "mode fallback" 语义),不静默。
- **句柄**:工具输出内为媒体/分块分配 `dN`/`cN` 句柄并附映射表,`read_document` 通过句柄解析回 `media_biz_id` / `chunk_biz_id`;会话内句柄表随 AgentStep 持久化,保证历史回放可解析。
- **工具描述即检索指南**:`search_knowledge` 的 Description 照搬 WeKnora 的写法——"写完整自然语言问句而非关键词串;标识符/错误码用 keyword 模式重试;无结果时换文档会用的术语改写",把检索经验固化在工具描述里。

### 6.3 Tool 接口(`application/port/tool.go`)

```go
type ToolResult struct {
    Success bool
    Output  string            // 给模型读的文本
    Data    map[string]any    // 结构化数据(事件/持久化用)
    Error   string
}

type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage // JSON Schema
    Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error)
}
```

Registry first-wins;未知工具名、参数校验失败、执行 panic 均回收为 `Success=false` 的 ToolResult 回灌模型(对齐 WeKnora registry_outcome 语义),不让单工具故障中断整轮。

## 7. LLM 端口扩展

```go
// application/port/chat.go 新增
type ToolDef struct {
    Name        string
    Description string
    Parameters  json.RawMessage
}

type LLMToolCall struct {
    ID        string
    Name      string
    Arguments json.RawMessage // 原文保留,便于回放与审计
}

type ChatResponse struct {
    Content      string
    Reasoning    string // reasoning_content 透传(P2 严格校验模型需要)
    ToolCalls    []LLMToolCall
    FinishReason string // stop / length / content_filter ...
}

type ChatModel interface {
    Complete(ctx, messages) (string, error)                    // 现有,保留
    Stream(ctx, messages, onToken) error                       // 现有,保留(答案轮)
    CompleteTools(ctx, messages, tools) (*ChatResponse, error) // 新增
}
```

- OpenAI 协议原生 `tools` 字段实现;Anthropic 协议 `tool_use` 块映射到同一 `ChatResponse` 结构(设置中心协议切换的既有兼容性约束,见 `docs/plans/completed/2026-09-27-llm-protocol-compatibility.md`);
- 模型不支持 tools(或返回格式异常):`CompleteTools` 返回明确错误,copilot 请求降级为 RAG 快问并在响应中标注,不静默伪劣执行。

## 8. SSE 事件契约(对齐步骤展示)

帧格式沿用现有 `event: <type>\ndata: <json>\n\n`。RAG 模式的 `token/citations/done/error` 语义不变;copilot 模式事件全集:

| event | data | 时机 |
|---|---|---|
| `thought` | `{round, content}` | 每轮 Analyze 后;含工具调用的轮,preamble 以 thought 呈现 |
| `tool_call` | `{round, id, name, args}` | 工具执行前 |
| `tool_result` | `{round, id, name, success, output(截断展示), duration_ms}` | 工具执行后 |
| `references` | `{items: [{source_type: kb_chunk\|web, title, url?, media_biz_id?, chunk_biz_id?, snippet}]}` | 答案生成前,统一知识库与联网引用 |
| `token` | `{token}` | 最终答案流式 |
| `done` | `{conversation_biz_id, rounds, truncated, citations}` | turn 结束,恰好一次 |
| `error` | `{error, conversation_biz_id}` | 现有语义 |

一次典型 turn 的事件序列:

```
thought(1) → tool_call(search_knowledge) → tool_result → thought(2)
→ tool_call(web_search) → tool_result → thought(3)
→ references → token* → done
```

前端按轮次把 thought/tool_call/tool_result 渲染为步骤树(WeKnora 前端同构),答案区只收 `token` 流;历史会话从 `agent_steps` 重建同一棵树。

## 9. 数据模型扩展

SQLite(`data/open-ima.db`,沿用现有迁移机制,`ALTER TABLE` 加列):

- `messages` 新增 `agent_steps TEXT`(JSON 序列化的 `[]AgentStep`,含 ToolCall 全量与句柄映射表,旧数据为 NULL 即 RAG 消息);
- `messages.citations` JSON 元素新增 `source_type`(`kb_chunk`/`web`,缺省视为 `kb_chunk` 兼容旧数据)与 `url`(web 引用必填);
- `conversations` 新增 `mode TEXT`(`rag`/`copilot`,缺省 `rag`)。

## 10. 配置(设置中心三层覆盖沿用)

| 键 | 默认 | 说明 |
|---|---|---|
| `web_search_enabled` | false | 联网搜索开关(控制 `web_search` 工具注入) |
| `web_search_provider` | `duckduckgo` | `duckduckgo` / `searxng` / `tavily`(后两者留实现位) |
| `searxng_base_url` | — | provider=searxng 时必填 |
| `web_search_max_results` | 5 | web_search 默认 limit |
| §5.3 四项 copilot 运行参数 | 见表 | 高级设置 |

DuckDuckGo 为默认 provider:免费、无需 API key,符合本地单用户部署定位;出站请求收敛到固定 provider 域名(SSRF 面与 V1 网页链接抓取同一套 `infrastructure/fetch` 的出站策略)。

## 11. API 契约

```
POST /api/kbs/{id}/copilot
  请求:{conversation_biz_id?, model_biz_id?, query}
  响应:SSE(thought / tool_call / tool_result / references / token / done / error)

GET /api/kbs/{id}/conversations          # 现有,响应增加 mode 字段
GET /api/conversations/{id}/messages     # 现有,copilot 消息增加 agent_steps 与 source_type/url 引用
```

## 12. 前端

- 对话页模式切换(快问 / Copilot),会话级生效,沿用现有会话列表;
- 新增 **步骤树组件**:轮次分组 → thought 文本 → 工具卡片(图标 + 名称 + 参数摘要 + 结果摘要 + 耗时,可展开原文)→ 答案流式区;
- 引用区区分知识库(点击定位阅读器,复用 document-reader)与网页(新窗口打开 URL);
- 组件契约:只消费第 8 节事件与 `agent_steps` 数据,不推导、不补发请求。

## 13. 分阶段实施与验证

### P1(最小闭环,本设计主体)

端口扩展(`CompleteTools`/`Tool`/`WebSearcher`)→ ReAct 引擎(四阶段 + 守护 + 兜底)→ 四个工具 → SSE 步骤事件 → 持久化与回放 → DuckDuckGo 联网搜索 → 前端模式切换与步骤树。

**验收**(对齐 AGENTS.md 端到端要求,由 mock LLM 驱动确定性用例):

| 用例 | 预期 |
|---|---|
| 知识库可答 | 1 轮内 search_knowledge 后作答,引用为 kb_chunk |
| 库内无答案 | 自动补 web_search,引用含 web URL |
| 双路综合 | 两轮工具调用,references 混合来源编号 |
| 无需检索 | 模型直接作答,零工具调用,events 无 tool_call |
| 多跳补搜 | 首轮结果不足,第二轮换 query 再搜 |
| 卡死守护 | 相同内容重复 2 轮 → 停止并兜底文案 |
| 超轮次 | 达到 max_iterations → 已有上下文合成答案,truncated 正确 |
| 用户取消 | ctx cancel → done 恰好一次,消息不残缺 |
| 模型不支持 tools | 降级 RAG 并标注 |

验证命令:`go test ./...`、`./scripts/harness.sh`、`./scripts/smoke.sh`(新增 copilot 场景)。

### P2(体验与深模型)

上下文压缩(compaction + 溢出重试一次)、thought 逐 token 流式、`reasoning_content` 透传回放(DeepSeek/MiMo 类)、steering、reflection。

### P3(生态)

MCP 服务接入、技能/沙箱、`read_web_page` 网页正文读取、更多 web search provider、多知识库 scope。

## 14. 决策记录

| 决策 | 结论 | 理由 |
|---|---|---|
| 规划方式 | 多轮 ReAct(function calling),不用单次 JSON 路由 | 用户拍板;对齐 WeKnora 引擎结构,具备迭代补搜能力 |
| 引擎状态 | 跨 turn 无状态,历史每 turn 重建 | 对齐 WeKnora;落库即真相,回放与故障恢复简单 |
| 工具命名 | `search_knowledge`/`read_document`/`list_documents`/`web_search` | 与 WeKnora 对齐,降低认知与文档迁移成本 |
| 双模式 | RAG 快问保留,copilot 并存可切换 | 对齐 WeKnora 快速问答/智能推理产品形态;快问成本低延迟小 |
| web search 默认 | DuckDuckGo(免 key),provider 可换 | 本地单用户定位;出站收敛防 SSRF |
| 步骤展示 | SSE 分事件 + `agent_steps` 持久化,前端步骤树 | 对齐 WeKnora "shows each step in the conversation" |
