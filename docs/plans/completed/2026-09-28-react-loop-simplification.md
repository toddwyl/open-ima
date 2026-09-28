# ReAct 循环简化方案调研与计划

## 状态

调研完成，待用户拍板方案后再动代码。本文档仅为计划，不含代码变更。

## 背景

用户反馈：copilot 的 ReAct 流程（工具注册、工具执行、思考-观察-行动循环）全部是手写实现，问能否用 LangChain 或 Go 现有库简化。

## 现状盘点

| 模块 | 文件 | 行数 | 职责 |
| ---- | ---- | ---- | ---- |
| ReAct 引擎 | `internal/application/copilot/engine.go` | 385 | think→act→observe 循环、守护兜底、流式事件、引用改写 |
| 工具注册表 | `internal/application/copilot/tools/registry.go` | 83 | 注册（first-wins）、panic 回收、输出限长 |
| 协议编解码 | `internal/infrastructure/llm/tools.go` | 230 | OpenAI / Anthropic 双协议 tool-calling wire 格式 |
| 聊天客户端 | `internal/infrastructure/llm/chat.go` | 205 | HTTP 请求、流式/非流式补全 |

测试覆盖：`engine_test.go` 422 行、`tools_test.go`（llm）194 行、`tools_test.go`（copilot/tools）258 行，行为已固化。

## 关键判断：手写的代码里有多少是"框架能给的"

引擎 385 行中，真正的循环骨架（调模型→有工具调用就执行→回填历史→重复）只有约 100 行。其余全部是**领域行为**，任何框架都不会替我们做：

1. **WeKnora 对齐的守护参数**：空回答重试、连续 length 截断累积续写、卡死签名检测、超轮次兜底合成答案（`Guards` / `fallback`）。
2. **流式事件协议**：`thought / tool_call / tool_result / references / token` 五类事件直推前端，service 层与 HTTP 层已按此契约实现。
3. **来源句柄改写**：工具分配 `[c1]/[w1]` 句柄，答案落库前改写为 references 序号，保证流式内容、持久化内容、引用区三者一致（`emitAnswer` / `rewriteHandleCitations`）。
4. **工具执行的工程细节**：并发执行、panic 回收为 `Success=false`、输出 head+tail 限长、同名注册 first-wins 防劫持。

换成框架后，这些行为仍需以 callback / middleware 形式重写一遍，代码量不会显著下降，反而多一层框架抽象需要穿越。

## 候选方案调研（2026-09 时点）

| 方案 | 说明 | 结论 |
| ---- | ---- | ---- |
| **langchaingo** | LangChain 的 Go 移植，社区维护 | **排除**。2026 年已事实停维护（162+ 未处理 PR，停留在 pre-1.0），其 ReAct 是 prompt 文本驱动的老式实现，不支持原生 tool calling 循环，比我们现有实现还弱 |
| **Eino（CloudWeGo）** | 字节开源，Go 生态最活跃（13k+ stars），ADK 提供开箱 ReAct agent、工具从 struct 推导、事件流 | 能力匹配最好，但仍是 0.x、API 变动频繁；接入后守护/句柄/事件协议仍需用它的 callback 机制重写 |
| **Genkit Go** | Google 官方，类型安全强 | Gemini/GCP 导向，与多模型（OpenAI+Anthropic 双协议）现状不匹配，排除 |
| **官方 SDK 局部替换** | `openai/openai-go` + `anthropics/anthropic-sdk-go` 替换 `llm/tools.go`+`chat.go` 的手写 wire 编码 | 唯一"真简化"点，但收益有限（替换的是已测试的 400 行），且引入两个大依赖树 |
| **Python LangChain sidecar** | 把 agent 循环搬到 Python | 破坏 Go 单体架构，事件流要跨进程转发，复杂度不降反升，排除 |

## 推荐方案

**方案 A（推荐）：保留自研循环，只做小范围收敛。**

理由：当前实现已按 `port.ChatModel` / `ToolSet` 端口解耦，测试完备；框架替换省不掉领域行为，反而引入 0.x 依赖的升级维护成本。可做的收敛项：

1. 文档补全：在 `docs/design/` 补一节说明"为什么自研 ReAct 而不引入框架"，把本调研结论固化，避免后续重复讨论。
2. （可选）若后续新增模型协议（如 Gemini），再评估引入对应官方 SDK，而不是提前抽象。

**方案 B（备选）：Eino ADK 重写引擎。**

仅当未来出现以下信号时再启动：需要多 agent 协作 / interrupt-resume / 复杂 graph 编排——这些才是框架真正省力的场景。届时以 `port.ChatModel` 为边界做 Eino 适配器，引擎层整体替换，守护与句柄改写迁移为 Eino callback。

**方案 C（不推荐）：官方 SDK 替换协议层。**

`llm/tools.go` 的双协议编码是手写但确定性强、测试覆盖好的纯函数代码；换 SDK 省的是"未来的协议兼容性维护"，代价是依赖树膨胀与重新适配。暂不做，记录为已知选项。

## 下一步

1. 用户确认方案（默认按方案 A 执行）。
2. 方案 A：在 `docs/design/` 新增设计说明，引用本文档结论；代码零改动。
3. 若选方案 B/C：另建 worktree，按仓库工作流实施，harness 全量验证。

## 注意事项

- 框架调研结论有时效性（2026-09），Eino 若发布 1.0 稳定版应重新评估方案 B。
- 引擎行为已被 `engine_test.go` 固化，任何替换都必须先保证这些测试语义不丢（守护阈值、事件顺序、句柄改写）。
- 不引入 langchaingo：维护状态差，且其 ReAct 实现模式落后于现有代码。
