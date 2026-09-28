# 腾讯官方公布了 ima 的架构文章，我照着它手搓了一个本地版 ima.copilot

---

先放结果。下面是 Open IMA 的实拍：上传 PDF / Word / 网页链接，异步解析入库，然后对着自己的知识库提问——流式输出、答案带可点击定位的引用，Agent 模式还能自己决定「这轮搜知识库还是联网搜」。

![Open IMA 知识库问答实拍：流式答案 + 引用定位 + Agent 步骤树](images/01-cover.png)

> 上图为界面概览占位，发布时替换为实测截图（知识库列表 + 问答引用定位 + Agent 步骤树三张）。

先说清楚这是什么：**腾讯 ima（ima.copilot）的开源本地部署复刻版**。文件、网页、对话数据全部落在自己机器上，检索和问答引擎完全本地运行，只有最后生成答案那一步走你自配的 LLM API。它不是 ima 的套壳，也不是「又一个小飞机 RAG demo」——它是照着腾讯官方公布的两篇 ima 后端架构文章，把千万级租户的架构一对一映射到单机的一次工程复刻。

这篇文章讲三件事：**它和 ima.copilot 有什么区别、实现上有多像、以及你怎么在自己机器上跑起来**。不构成任何商业用途背书，纯个人技术项目。

## 为什么复刻： ima 很好用，但我的文件不想上云

ima.copilot 是腾讯的 AI 智能工作台：知识库 + RAG 问答 + 联网搜索 + 智能笔记，产品完成度很高，我自己也在用。但两个实际问题绕不开：

1. **数据在云端**。合同、财报、未公开的研究笔记，很多人不愿意（或不被允许）传到第三方云。
2. **黑盒**。检索怎么召回的、引用怎么对齐的、模型什么时候决定联网——看不到，也改不动。

巧的是，腾讯技术团队自己把答案公布了大半：《腾讯 AI 智能工作台 IMA 的知识库后端系统从 0 到 1 架构实践》和《腾讯 ima AI 知识库 Elasticsearch 检索实践》两篇文章，把数据模型、分层架构、检索方案讲得相当坦诚。**官方文章当规格书，这事就变得可做了。**

## 区别：一个是云产品，一个是本地工程

先把差异摆清楚，避免误会：

| 维度 | ima.copilot（腾讯官方） | Open IMA（本项目） |
| --- | --- | --- |
| 部署 | 云端 SaaS，App / 小程序 / PC | 单机本地部署，Docker 一键或裸进程 |
| 数据归属 | 腾讯云端 | 全部本地 `./data` 目录 + SQLite |
| 用户体系 | 多租户、微信生态登录 | 单用户，无账号系统 |
| 外部依赖 | 全托管 | 只有 LLM 生成走你自配的 API（OpenAI / Anthropic 兼容） |
| 智能笔记、OCR、音视频 | 有 | 明确不做（V2 以后的事） |
| 源码 | 闭源 | 开源，架构分层可读可改 |

一句话：**ima 是产品，Open IMA 是「把 ima 公开架构落到本地」的工程实现**。功能上是子集，架构上是对齐。

## 实现有多像：千万级租户架构的单机映射

这是这个项目最有意思的部分。腾讯文章里每一层生产级组件，我都给了一个「语义不变、规模降级」的本地等价物：

| 腾讯 ima 的做法（千万级租户） | Open IMA 的本地做法 |
| --- | --- |
| Media / Chunk 两层数据模型 | `medias` / `chunks` 两层模型，文件管理与检索单元解耦 |
| 统一接入层：知识库 / 媒体中心 / 文件上传三个服务 | Go DDD 分层里 application 层的三个用例域，职责一一对应 |
| 独立解析层（媒体解析 + 解析基础能力） | Python FastAPI parser sidecar，注册表按类型路由，HTTP 可插拔 |
| COS 对象存储 | 存储接口对齐对象存储语义（Put/Get/Delete），V1 实现为本地目录 |
| 消息队列异步削峰 | SQLite 任务表 + Go worker pool，状态机 + 指数退避重试，零外部 MQ |
| 原子/聚合服务 + 异步对账 | 删除走补偿式清理（Meili → 文件 → DB），media 状态机驱动失败重试 |
| ES 双路召回 + RRF + 租户路由 | Meilisearch 混合检索原生 RRF，`kb_biz_id` 过滤做库级隔离 |
| Query 改写（多 query 扩展） | LLM 生成扩展 query，与原 query 合并召回 |

四个进程，一条流水线：`app`（Go，REST + SSE + 异步 worker，内嵌前端）、`parser`（Python 解析 sidecar）、`meilisearch`（BM25 + 向量 KNN + RRF，内置 jieba 中文分词）、embedding 用本地 Ollama 跑 `bge-m3`。**除了生成答案的 LLM，没有任何字节离开你的机器。**

![Open IMA 架构图：四个进程一条流水线](images/02-architecture.png)

> 发布时替换为仓库 `docs/design/assets/architecture.svg` 导出的横版架构图。

## Copilot 模块：对着 WeKnora 源码对齐 ReAct 引擎

知识库问答之外，ima copilot 的核心是 **Agent 模式**——模型自己决定搜不搜、搜知识库还是联网、结果不够要不要再补一轮。V1 的固定管线（改写 → 检索 → 生成）没有这个决策权，这是和官方产品最大的差距。

腾讯没有公布这部分实现，但腾讯开源的 WeKnora 有同构的「智能推理模式」。所以这一块的做法是**直接读 WeKnora 源码，对齐它的引擎结构、工具命名和事件契约**：

- **ReAct 四阶段循环**：Think（带工具定义调 LLM）→ Analyze（判定终止）→ Act（并发执行工具调用）→ Observe（结果进上下文进入下一轮）。
- **引擎跨 turn 无状态**：历史每轮从 DB 重建，和 WeKnora 一致。
- **循环守护全套照搬**：最大轮次 20、空回答重试 2 次、连续重复内容判定卡死、连续截断检测、用户取消时兜底合成答案。
- **四个工具**：`search_knowledge` / `read_document` / `list_documents` / `web_search`。
- **快问 / Agent 双模式只是同一个引擎的两个配置档**，SSE 事件契约统一，前端单一渲染管线——不是两套代码。

实测效果：投资知识库提问，Agent 跑 2~7 轮不等；一个需要「知识库 + 联网」结合的问题跑了 7 轮，产出混合引用答案（库内分块 + 网页共 92 条引用），步骤树实时渲染，落库后回放完全一致。

## 【核心】复刻的真正难点：不是照抄，是那些文章里不会写的坑

架构映射表看着顺滑，真写起来，费时间的全是规格书里没有的东西。举三个我真实踩过的坑（都进了仓库的 `docs/guide/common-pitfalls.md`）：

**坑一：流式输出里的引用句柄，改写时机错一拍就全错。**
Agent 生成的答案内部用 `[c2]` 这样的句柄引用检索分块，展示前要改写成可读引用。第一版只在持久化前改写，结果 SSE 流式 token 里残留裸 `[c2]` 给用户看到了；更阴的是 `read_document` 工具输出里的 `[分块3/12]` 人类记号会被模型照抄进答案当成引用格式。解法：分块一律带 cN 句柄并回填 citations，改写逻辑前移到引擎 emitAnswer，流式与落库内容逐字一致。

**坑二：免密钥联网搜索没有稳定解。**
DuckDuckGo 匿名入口对部分出口 IP 长期 202 限流（退避重试只能缓解）；百度对无 cookie 的程序化请求直接弹图形验证码（要先 cookie 预热）；Bing 匿名输出已降级。折腾一圈的结论：**免配置提供方不存在**，老老实实接一个结构化搜索 API（项目用的 AnySearch，免费 Key），密钥只存本地 SQLite。

**坑三：`npm ci` 顺着符号链接把主仓库的 node_modules 清空了。**
开发用 git worktree 隔离，为了省空间把 worktree 里的 `node_modules` 软链到主检出——`npm ci` 的语义是先删再装，它删的是**链接目标的内容**。主仓库依赖瞬间蒸发。类似的还有 `go run` 后台进程杀掉的只是 wrapper、编译产物继续持有端口，下一次启动健康检查打到残留的旧进程上，验证全在跑错的二进制。

这类坑的共同点是：**架构文章告诉你组件怎么摆，不告诉你组件接缝处的语义会咬人**。复刻的价值一半在图纸，另一半在把这些接缝处全部趟一遍。

## 怎么复现：十分钟在你机器上跑起来

前置条件只有三个：Docker、Ollama（`ollama pull bge-m3`）、一个 LLM API Key（任何 OpenAI / Anthropic 兼容端点都行，我用的 Kimi Coding）。

```bash
git clone https://github.com/toddwyl/open-ima.git
cd open-ima
cp .env.example .env
# 编辑 .env：填 IMA_LLM_API_KEY、IMA_LLM_BASE_URL、IMA_LLM_MODEL
docker compose -f deploy/docker/compose.yml up -d --build
```

打开 `http://localhost:8080`，建知识库、拖文件进去、等解析状态变绿，然后开问。要联网搜索的话，去 AnySearch 控制台免费拿个 Key，在配置中心「联网搜索」里粘贴启用即可。

想本地开发或验证，`./scripts/harness.sh` 是全量门禁（lint + typecheck + 前后端测试 + 构建），`./scripts/smoke.sh` 会拉起确定性 mock 依赖跑 HTTP 端到端——不依赖任何外部服务也能验证全链路。

## 开源 & 讨论

完整设计文档（V1 架构 + Copilot Agent 模块两份）、踩坑记录、业务 E2E 用例矩阵都在仓库里：

**👉 `github.com/toddwyl/open-ima`** —— 欢迎来看实现细节、提 Issue 讨论。RAG 工程化、Agent 引擎设计、或者「把大厂公开架构当规格书复刻」这个玩法本身，都欢迎在评论区聊。

> **说明：** 本项目为个人技术研究项目，与腾讯公司无关；ima、ima.copilot、WeKnora 均为腾讯相关产品/项目名称。架构参考自腾讯官方公开发表的技术文章与开源代码，无任何私有信息。

---

## References

- 腾讯云开发者社区. 《腾讯 AI 智能工作台 IMA 的知识库后端系统从 0 到 1 架构实践》. https://cloud.tencent.com/developer/article/2608466
- 腾讯云开发者社区. 《腾讯 ima AI 知识库 Elasticsearch 检索实践》. https://developer.cloud.tencent.com/article/2747411
- WeKnora（腾讯开源知识库问答框架）. https://github.com/Tencent/WeKnora
