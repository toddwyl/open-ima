# Copilot Agent P1 实现(进行中)

**状态**:进行中
**开始**:2026-09-28
**分支/Worktree**:`.worktrees/copilot-agent`(`feat/copilot-agent`,基于本地 main `b6ca2f9`)

## 目标

按 [设计文档](../../design/2026-09-28-copilot-agent-design.md) §13 P1 实现统一 agent 引擎:端口扩展 → ReAct 引擎 → 四个工具 → SSE 统一事件 → 持久化回放 → DuckDuckGo 联网 → 前端步骤树与模式选择。

## 当前进度

- [x] 差距分析、对标调研、设计文档(commit `ee3c6bc`/`b6ca2f9`)
- [x] worktree 创建,现有代码已读(chat/port/llm/db/settings/前端/smoke)
- [ ] 端口扩展:`CompleteTools`/`ChatResponse`/`ToolDef`/`LLMToolCall`、`Tool`+`ToolResult`、`WebSearcher`
- [ ] LLM 客户端 tools 协议(OpenAI 原生 + Anthropic tool_use 映射)
- [ ] 领域与 DB:`conversations.mode`、`messages.agent_steps`、citation `source_type`/`url`(schema v2→v3 ALTER)
- [ ] copilot 引擎:四阶段循环 + 守护 + 兜底 + mock 单测
- [ ] 工具:search_knowledge / read_document / list_documents / web_search + Registry(first-wins)
- [ ] infrastructure/websearch DuckDuckGo
- [ ] copilot service + HTTP SSE 统一(thought/tool_call/tool_result/references/token/done/error),tools 不支持时降级
- [ ] 设置中心:web_search_enabled/provider/searxng_base_url/max_results
- [ ] 前端:模式选择(默认 agent)+ 步骤树 + 引用区 web/kb 分源
- [ ] smoke:mock-model 支持 tools,新增 agent/快问场景

## 关键信息

- 设计文档即唯一权威:`docs/design/2026-09-28-copilot-agent-design.md`。
- 守护默认值:max_iterations 20、空回答重试 2、卡死 2、连续截断 3、工具输出 16000 字符。
- 架构约束:`AgentStep` 等持久化类型须落 `domain/conversation`(application 不能反向被 domain 依赖);工具实现位于 `application/copilot/tools`。
- 旧 `chat.Service.Chat` 固定管线被 copilot 引擎取代,`chat.Service.Search` 保留(检索端点)。
- DB 迁移:schemaVersion 2→3,仅 v2 走 ALTER 升级路径,更老版本维持"删库重建"报错。

## 下一步

按上述清单顺序逐项实现,每项 `go test ./...` 或相应验证后 commit,全部完成后 `./scripts/harness.sh` + `./scripts/smoke.sh`,再合并回 main 并清理 worktree。

## 注意事项

- 仓库契约:每轮变更以 commit 结束;不 `--amend`;阻塞即停并报告。
- 业务键命名 `<entity>_biz_id`;句柄表(dN/cN)随 AgentStep 持久化。
- SSE 契约不分模式;`citations` 事件被 `references` 取代,前端渲染管线只有一套。
