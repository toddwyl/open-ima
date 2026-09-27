# AGENTS

本文件定义 coding agent 在本仓库中的工作契约。

> Open IMA 的仓库级开发与验证契约。

## 核心原则

1. **小步可回滚**：每一轮变更以 Git commit 结束；commit 是进度管理、回退和审查的唯一控制面。
2. **编码前先思考**：在写代码前，AI 必须先阐述思路与方案，而不是直接盲目生成代码。修改前必须读取相关文件，禁止猜测结构或行为。
3. **验证后再提交**：改完必须跑 `./scripts/harness.sh`，通过后才能 commit。

## 技术栈

| 层级 | 选型 |
| ---- | ---- |
| 前端 | React 18 + TypeScript + Vite + Tailwind CSS |
| 后端 | Go 1.26 `net/http` + SQLite (`modernc.org/sqlite`) |
| 解析 | Python + FastAPI；PDF/DOCX/PPTX/Markdown/Text/HTML |
| 检索 | Meilisearch v1.10.3 + Ollama `bge-m3`，Meilisearch 托管 embedding |
| 测试 | Go `testing` + Vitest/Testing Library + Pytest + HTTP smoke |

## 仓库地图

```
open-ima/
├── AGENTS.md                   # 代理工作契约（本文件）
├── scripts/
│   └── harness.sh              # 验证门禁（提交前必跑）
├── docs/
│   ├── design/                 # 设计文档
│   ├── guide/
│   │   └── common-pitfalls.md  # 常见陷阱（持续学习承载文件）
│   ├── spec/
│   │   └── worktree-workflow.md
│   └── plans/
│       ├── active/             # 进行中（文件在即任务在）
│       └── completed/          # 已完成归档
├── .worktrees/                 # Git worktree 目录（按 topic 隔离开发）
├── cmd/                        # server、reindex 与 smoke mock 入口
├── internal/                   # Go 业务与基础设施包
├── parser/                     # Python 解析 sidecar
└── web/                        # React SPA 与 Go embed
```

**Worktree 隔离（强制）**：所有代码变更**必须**在 `.worktrees/<topic>` 下创建独立 worktree 开发，禁止在 `main` 直接开发。详见 [`docs/spec/worktree-workflow.md`](docs/spec/worktree-workflow.md)。

## 强制工作流

1. `git status -sb` 确认工作区状态。
2. 创建主题 worktree：`git worktree add .worktrees/<topic> -b <branch> origin/main`。
3. 先读相关文件再修改。
4. 每轮只聚焦一个完整目标，不混入无关改动。
5. 改完后 `./scripts/harness.sh`，通过后 commit 再结束。

## 提交策略

- 每轮文件变更必须以 commit 结束；不得将无关改动塞进同一个 commit。
- 提交信息使用 `feat:` / `fix:` / `refactor:` / `docs:` / `chore:`。
- 未经用户明确要求，不得 `--amend` 已有 commit。
- 分支完成后合并回 `main` 并清理 worktree。

## 安全规则

- 不硬编码公网地址、密码、API 真密钥；环境值放环境变量或 CI Secret。
- 禁止破坏性命令（`git reset --hard`、`git push --force`），除非用户明确授权。
- 遇到阻塞时停止并报告，不得伪造完成状态。

## 验证

**任何代码修改后，必须先运行 `./scripts/harness.sh` 且完整通过**，才能提交或声称完成。harness 执行 `git diff --check` 及项目配置的 lint / typecheck / test / build 检查。

本地端到端测试直接运行 `./scripts/smoke.sh`，由脚本启动 app、parser 和确定性依赖进程；本地验收不要求 Docker。Docker Compose 只用于部署或显式要求的容器联调。

需要持久保存但不提交的项目级工具放在 `.local/`（例如 `.local/bin/meilisearch`），不要依赖 `/tmp` 路径。

端到端完成必须由设计文档导出的业务用例矩阵逐项证明；单条 happy path 或单个真实依赖联调不得代表全量业务验收。

| 改动范围 | 必须验证 |
| -------- | -------- |
| 任意代码修改 | `./scripts/harness.sh` |
| 前端代码 | harness + lint + build |
| 重大 UI 变更 | 以上 + 浏览器实测（Playwright / MCP），附截图 |
| 后端代码 | harness + 服务可正常启动 |
| API 变更 | 至少完成一次请求-响应往返验证 |

若尚无自动化测试覆盖，必须在总结中明确说明，仍然提交。

## 持续学习

1. **踩坑即沉淀**：非显然的坑（环境、依赖、隐式约定），把根因与标准解法补进 [`docs/guide/common-pitfalls.md`](docs/guide/common-pitfalls.md)。
2. **防遗忘回放**：已修复的真实失败固化为回归测试，纳入 `scripts/harness.sh`。
3. **偏好写回契约**：用户稳定的工作偏好或纠正，写回本 `AGENTS.md`。
4. **压缩历史**：吸收反馈后主动简化，不无限叠加补丁。

## 会话交接（Handoff）

跨会话的长任务通过交接文档传递上下文，存放在 `docs/plans/` 体系。

**何时创建**：预计超过 1 小时的后台任务 / 会话即将结束但未完成 / 用户要求 / 多步骤流程部分完成。

**命名**：`docs/plans/active/YYYY-MM-DD-<topic>.md`，内容至少包含：**状态**、**当前进度**、**关键信息**、**下一步**、**注意事项**。

**强制行为**：

1. 会话开始时扫描 `docs/plans/active/`，有 `.md` 则读取并汇报。
2. 启动长任务后立即创建交接文档。
3. 会话结束前更新进度。
4. 任务完成后文档从 `active/` 移到 `completed/`。

## 阻塞处理

**禁止**：提交 commit、伪造通过状态、假装任务已完成。

**必须**：在总结中记录已完成进度和阻塞原因，向用户输出清晰说明，然后停止。

## 默认收尾顺序

1. 实现 → 2. 验证 → 3. 总结 → 4. 提交
