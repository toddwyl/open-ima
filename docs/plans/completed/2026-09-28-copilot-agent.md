# Copilot Agent P1 实现(已完成,待合并)

**状态**:实现与验证全部完成,待合并回 main
**开始/完成**:2026-09-28
**分支/Worktree**:`.worktrees/copilot-agent`(`feat/copilot-agent`,基于本地 main `b6ca2f9`)

## 目标

按 [设计文档](../../design/2026-09-28-copilot-agent-design.md) §13 P1 实现统一 agent 引擎:端口扩展 → ReAct 引擎 → 四个工具 → SSE 统一事件 → 持久化回放 → DuckDuckGo 联网 → 前端步骤树与模式选择。

## 当前进度(全部完成)

- [x] 端口扩展:`CompleteTools`/`ChatResponse`/`ToolDef`/`LLMToolCall`、`Tool`+`ToolResult`、`WebSearcher`
- [x] LLM 客户端 tools 协议(OpenAI 原生 + Anthropic tool_use 映射)
- [x] 领域与 DB:`conversations.mode`、`messages.agent_steps`、citation `source_type`/`url`(schema v2→v3 ALTER)
- [x] copilot 引擎:四阶段循环 + 守护 + 兜底 + 单测
- [x] 工具:search_knowledge / read_document / list_documents / web_search + Registry(first-wins)
- [x] infrastructure/websearch DuckDuckGo(含 202 限流退避重试 + lite 降级)与 SearxNG
- [x] copilot service + HTTP SSE 统一(thought/tool_call/tool_result/references/token/done/error),tools 不支持时降级
- [x] 设置中心:web_search_enabled/provider/searxng_base_url/max_results
- [x] 前端:模式选择(默认 agent)+ 步骤树 + 引用区 web/kb 分源
- [x] smoke:mock-model 支持 tools,e2e 断言 references/rounds/mode + quick 场景
- [x] `./scripts/harness.sh` 全绿(多轮),`./scripts/smoke.sh` BUSINESS E2E OK
- [x] 浏览器实测(Playwright,真实 Kimi 模型 + 真实 meili/ollama/parser 栈,8081 端口)

## 浏览器实测结论(真实栈)

- SSE 流正常:步骤树随 thought/tool_call/tool_result 实时渲染,token 回放流畅,done 后引用区就位。
- ReAct 多轮规划:投资知识库实测 2~7 轮不等(search_knowledge → list_documents → read_document → web_search 组合)。
- 联网+知识库结合:招商南油问题实测 7 轮,产出混合引用答案(kb_chunk + web 共 92 条引用),落库与回放一致。
- 实测揪出并修复三个缺陷(各有 commit 与回归测试):
  1. 句柄改写只在持久化前做,流式 token 残留 `[c2]` → 改写前移到引擎 emitAnswer。
  2. read_document 输出 `[分块N/M]` 人类记号被模型照抄当引用 → 分块一律带 cN 句柄并回填 citations。
  3. `[dN]` 文档句柄无媒体级键无法改写 → 改写索引补媒体键兜底。
  4. DuckDuckGo 高频请求 202 限流 → 2s/4s 退避重试 + lite 精简页降级。
- 注意:DDG 匿名入口会 IP 级短时封禁(202),重试只能缓解;设置中心可切自建 SearxNG。实测后期 DDG 对本机封禁未解除,web_search 优雅降级(模型如实说明联网不可用,知识库部分正常作答)。

## 关键信息

- 设计文档即唯一权威:`docs/design/2026-09-28-copilot-agent-design.md`。
- 守护默认值:max_iterations 20、空回答重试 2、卡死 2、连续截断 3、工具输出 16000 字符。
- 架构约束:`AgentStep` 等持久化类型落 `domain/conversation`;工具实现位于 `application/copilot/tools`。
- 旧 `chat.Service.Chat` 固定管线已被 copilot 引擎取代,`chat.Service.Search` 保留(检索端点)。
- DB 迁移:schemaVersion 2→3,仅 v2 走 ALTER 升级路径,更老版本维持"删库重建"报错。
- 踩坑已沉淀 `docs/guide/common-pitfalls.md`(句柄改写时机、工具输出句柄化、DDG 202)。

## 下一步

合并 `feat/copilot-agent` 回 main(ff),删除 worktree,本文档移到 `docs/plans/completed/`。

## 注意事项

- 真机实测用 8081 端口 + worktree 内 `data/`(主库 DB 备份拷贝而来),不影响主仓库 8080 实例。
- 验证截图曾保存在主仓库根目录 `copilot*.png`，已在后续仓库清理中删除。
