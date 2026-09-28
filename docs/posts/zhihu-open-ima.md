# 腾讯官方公布了 ima 的架构文章，我照着它手搓了一个本地版 ima.copilot

---

Open IMA 支持上传 PDF、Word、网页链接，后台异步解析入库，本地化对应的文档数据，包括向量化都是本地操作，支持向自己的知识库提问，流式输出，答案带能点击定位的引用，Agent 模式会综合知识库和联网搜的多方数据源。

![Open IMA 概览，本地部署版 ima 知识库，数据不出你的机器](images/01-cover.png)

它是腾讯 ima 的开源本地部署复刻版，文件、网页和对话数据全部落在自己机器上，检索和问答都在本地跑，只有最后生成答案那一步走你自己配的 LLM API。做法也直接，腾讯官方公布过两篇 ima 后端架构文章，我把它们当规格书，照着把千万级租户的架构一层一层映射到了单机。

## 为什么要复刻一个 ima

ima.copilot 我自己在用，知识库、问答、联网搜索、智能笔记都有，完成度很高。云端的ima.copilot其实出发点就是每个人在使用AI工具时都可以有一个实时的共享知识库，第二个大脑，帮我们存储所有特定领域的知识。

但我找到一个需求点是，有些资料合同、财报、没公开的研究笔记很多人不愿意传到第三方云，另一方面我也希望参考他们的技术框架实现一个本地化的ima。

腾讯技术团队在《腾讯 AI 智能工作台 IMA 的知识库后端系统从 0 到 1 架构实践》和《腾讯 ima AI 知识库 Elasticsearch 检索实践》中详细介绍了ima.copilot的技术方案，数据模型、分层架构、检索方案都讲得相当坦诚。

所以本次我将复刻一个本地化的ima.copilot。

## 它和 ima.copilot 的区别

先把差异摆清楚，免得误会。

| 维度           | ima.copilot（腾讯官方）  | Open IMA                      |
| ------------ | ------------------ | ----------------------------- |
| 部署           | 云端 SaaS，App、小程序、PC | 单机本地部署                        |
| 数据归属         | 腾讯云端               | 本地 ./data 目录                  |
| 用户体系         | 多租户，微信生态登录         | 单用户                           |
| 外部依赖         | 全托管                | LLM 生成走自配 API，本地使用embedding模型 |
| 智能笔记、OCR、音视频 | 有                  | 未来或许会支持                       |
| 源码           | 闭源                 | 开源                            |

说得简单一点。ima 是产品，Open IMA 是把 ima 公开架构落到本地的工程实现。功能上是子集，架构思想同源。

## 实现有多像

先说腾讯原文怎么讲架构，因为它直接决定了这个项目的读法。原文不讲组件清单，它把知识库拆成入库、管理、应用三个环节，每个环节先摆一个具体挑战，再给解法，图纸跟着解法一步步长大。我复刻时按同样的顺序走，每一层生产级组件都落到一个语义不变、规模降级的本地等价物。

| 腾讯 ima 的做法（千万级租户）         | Open IMA 的本地做法                                 |
| ------------------------- | ---------------------------------------------- |
| Media 和 Chunk 两层数据模型      | medias 和 chunks 两层模型，文件管理与检索单元解耦               |
| 统一接入层，拆成知识库、媒体中心、文件上传三个服务 | 领域拆分类似                                         |
| 独立解析层，媒体解析加解析基础能力         | 同样是独立解析层，以Python解析作为独立进程，注册表按类型路由，HTTP 可插拔     |
| COS 对象存储                  | 存储接口保持对象存储语义（Put、Get、Delete），V1 实现成本地目录        |
| 消息队列异步削峰                  | SQLite 任务表加 Go worker pool，状态机加指数退避重试，不引入外部 MQ |
| 原子聚合服务加异步对账               | 删除走补偿式清理，Meili、文件、DB 依次清，media 状态机驱动失败重试       |
| ES 双路召回加 RRF 加租户路由        | Meilisearch 混合检索自带 RRF，类似ES都是倒排索引支持向量化和混合检索    |
| Query 改写，多 query 扩展       | LLM 生成一条扩展 query，和原 query 合并召回                 |

下面这张图把两边并排画了出来，左边是腾讯官方文章里的架构图，右边是我按同一骨架画的本地图，可以对着表看。

![腾讯 ima 官方架构图与 Open IMA 本地图并排对比](images/05-arch-compare.png)

这张图不要按组件名硬背，要按知识从进入系统到被回答的生命周期看。它分成三段，先入库，再管理，最后应用。每一段我都保留腾讯架构里的关键语义，只是把多租户、大规模、云服务的实现，降成单机上可维护的等价物。

**第一段是入库，把任意资料变成可检索的 Chunk。** 这里的难点有三个。

- **格式杂。** PDF、Word、PPT、Markdown、网页链接都要进来，不能每种格式在后面长出一套模型。所以内部先统一成两层，Media 管文件本身，记录标题、来源、状态、文件 hash；Chunk 管检索单元，按 512 字符切分、80 字符重叠，并带上标题路径上下文。这两层必须拆开，因为生命周期完全不同。删文档时要连带清切片，重建索引时切片全部重切但文档记录不动，去重看的是整个文件的 hash。
- **流程杂。** 上传、解析、切分、索引不是一个层面的事。Go 只负责接入和用例编排，解析单独放到 Python FastAPI sidecar 里，再用注册表按文件类型路由。这样做不是为了好看，而是因为解析是整条链路里最脏的活，PDF、Word、PPT、网页各有各的问题，Python 生态更合适；解析进程挂了也拖不垮主服务，后面换解析实现也不用动 Go 的主链路。
- **洪峰明显。** 用户拖一批文件进来时，解析能力是恒定的，接口不能跟着一起卡住。腾讯用消息队列削峰填谷，单机版用 SQLite 任务表加 Go worker pool 就够。上传只做落盘和登记，立刻返回；后台任务按状态机往前跑，失败后按一分钟、五分钟、十五分钟退避重试，三次还不行就标记 failed，前端能看到具体原因。

**第二段是管理，重点不是能不能删，而是删得干不干净。** 一篇文档背后至少牵着四样东西，列表里的 Media、SQLite 里的 Chunk 元数据、Meilisearch 里的检索文档、本地文件。哪一步漏掉，都会留下垃圾数据。腾讯的做法是原子服务加聚合服务，再配异步对账服务兜底；Open IMA 这边走补偿式清理，先清 Meili，再删文件，最后删 DB 行，失败就继续作为任务重试。对账 ticker 会周期扫描卡在 deleting 的记录，进程重启后也能接着收尾。权限层是我唯一整块砍掉的部分，单用户本地部署没有 RBAC，这一层直接不存在。

**第三段是应用，把检索和问答链路压到一个轻量检索引擎上。** 腾讯用 ES 做关键词召回和向量召回，再用 RRF 融合，还会做 query 改写。Open IMA 也是类似的思路，没有再拆出存储层、向量库、rerank 服务，而是让 Meilisearch 一个进程承担倒排、向量 KNN 和 hybrid 融合。普通检索就是一次 API 调用，做库级隔离，可以控制全文和语义的比例。这里要分清楚，SQLite 保存的是知识库、Media、Chunk 归属和状态这些管理数据；真正用于检索的正文和向量托管在 Meilisearch。embedding 也交给 Meilisearch 管，它会把标题和正文渲染成文本，再调用本地 Ollama 的 `bge-m3` 向量化。

最后落到进程边界上，就是四个进程一条流水线。`app` 是 Go 写的，管 REST API、SSE、异步 worker 和对账 ticker，前端构建产物直接嵌在二进制里。`parser` 是 Python 解析 sidecar。`meilisearch` 管 BM25、向量 KNN、hybrid 融合和内置 jieba 中文分词。`ollama` 只负责本地 embedding。除了你主动配置的 LLM 生成和可选联网搜索，知识库文件、切片和 embedding 都不需要离开这台机器。

![Open IMA 架构图，四个进程一条流水线](images/02-architecture.png)

## Copilot 的 Agent 引擎是怎么做的

知识库问答之外，ima copilot 的核心是 Agent 模式，其实就是ReAct框架。模型自己决定搜不搜，搜知识库还是联网，结果不够要不要再补一轮。

腾讯没有公布这部分实现，只能自己把引擎设计出来。最后落地的结构长这样。

- **ReAct 四阶段循环。** Think 带着工具定义调 LLM，Analyze 判定该不该停，Act 并发执行这一轮的工具调用，Observe 把结果塞回上下文，进下一轮。
- **引擎跨 turn 无状态。** 历史每轮从 DB 重建。
- **循环守护。** 最大轮次 20，空回答重试 2 次，连续重复内容判定卡死，连续截断有检测，用户取消时用已有结果兜底合成答案。
- **四个工具。** search_knowledge、read_document、list_documents、web_search。

## 真正难的在规格书之外

架构映射表看着顺滑，真写起来，费时间的反而是规格书里不会展开的小边界。这里最典型的是联网搜索。

**Agent联网搜索能力。** Agent 模式里，联网搜索不是一个锦上添花的按钮，而是模型判断“知识库不够时去哪补证据”的工具。如果搜索结果不稳定，Agent 后面的推理、引用和答案都会跟着飘。最开始我也想做成零配置，试过 DuckDuckGo、百度、Bing 这些匿名入口。DuckDuckGo 对部分出口 IP 会长期 202 限流，退避重试只能缓解；百度无 cookie 的程序化请求很容易进图形验证码；Bing 匿名结果质量也不稳定。搜索这件事只要偶发空结果，用户看到的就不是“搜索 provider 挂了”，而是 Agent 像在胡说。

最后我把联网搜索使用了AnySearch：固定接结构化搜索 API，项目里用 AnySearch，Key 存在本地 SQLite。这样牺牲了一点开箱即用，但换来的是可解释、可观测、可重试。对本地知识库产品来说，这个取舍更重要：知识库内容可以完全本地，联网搜索可以关闭；一旦打开，就必须给 Agent 一个稳定的数据源，而不是把反爬页面当搜索结果塞进上下文。

这个坑也说明，复刻架构不只是把组件摆出来。真正决定产品能不能用的，往往是组件边界上的失败语义。

## 怎么在自己机器上跑起来

依赖比一个命令多一点，先摊开说清楚。

- Go 1.26：编译后端和内嵌前端产物。
- Node.js 20+：安装 React/Vite 前端依赖，`start.sh` 会在 `web/` 下跑 `npm ci` 和构建。
- Python 3.11+：跑解析 sidecar，依赖包括 FastAPI、uvicorn、pypdf、python-docx、python-pptx、markdown-it-py、beautifulsoup4、readability-lxml、lxml。
- Meilisearch v1.10.3：本地倒排、向量和混合检索。可以装到 PATH，也可以按下面这样放进项目 `.local/bin`，脚本会优先识别。
- Ollama + `bge-m3`：本地 embedding。`start.sh` 会检查 Ollama，必要时自动 `ollama pull bge-m3`，但机器上要先装好 Ollama。
- LLM API Key：生成答案用，支持 OpenAI / Anthropic 兼容端点。我本地用的是 Kimi Coding。
- AnySearch API Key：只有打开 Agent 联网搜索时才需要，Key 保存在本地 SQLite；不配也能跑知识库问答。

```bash
git clone https://github.com/toddwyl/open-ima.git
cd open-ima
cp .env.example .env

# 编辑 .env，至少填 IMA_LLM_API_KEY。
# 如果不用默认 DeepSeek 兼容端点，再改 IMA_LLM_BASE_URL / IMA_LLM_PROTOCOL / IMA_LLM_MODEL。

# 安装 Python parser 依赖
python3 -m venv parser/.venv
parser/.venv/bin/pip install -r parser/requirements.txt

# 把 Meilisearch 放进项目内，脚本会自动识别
mkdir -p .local/bin
curl -L --fail -o .local/bin/meilisearch \
  https://github.com/meilisearch/meilisearch/releases/download/v1.10.3/meilisearch-macos-apple-silicon
chmod +x .local/bin/meilisearch

./scripts/start.sh   # 一键拉起 Meilisearch、parser、app，Ctrl+C 全部停止
```

打开 http://localhost:8080，建知识库，拖文件进去，等解析状态变绿，然后开问。要联网搜索的话，去 AnySearch 控制台免费拿个 Key，在配置中心的联网搜索里粘贴启用。

想验证改动就跑 ./scripts/harness.sh，lint、typecheck、前后端测试、构建全在里面。./scripts/smoke.sh 会拉起确定性 mock 依赖做 HTTP 端到端断言，不依赖外部服务。

## 开源和讨论

完整设计文档、踩坑记录、业务 E2E 用例矩阵都在仓库里。

**👉 github.com/toddwyl/open-ima**。欢迎来看实现细节，提 Issue 讨论。RAG 工程化、Agent 引擎设计，或者把大厂公开架构当规格书复刻这个玩法本身，评论区都可以聊。

> **说明。** 本项目是个人技术研究项目，与腾讯公司无关。ima、ima.copilot 均为腾讯相关产品或项目名称。架构参考自腾讯官方公开发表的技术文章，不涉及任何私有信息。

---

## References

- 腾讯云开发者社区《腾讯 AI 智能工作台 IMA 的知识库后端系统从 0 到 1 架构实践》 https://cloud.tencent.com/developer/article/2608466
- 腾讯云开发者社区《腾讯 ima AI 知识库 Elasticsearch 检索实践》 https://developer.cloud.tencent.com/article/2747411
