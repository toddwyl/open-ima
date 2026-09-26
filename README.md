# Vibe Coding Template

一个**栈无关、可复用**的 Web Coding 工作框架模板。它不含业务代码，只沉淀「代码代理（coding agent）该如何在仓库里工作」的一整套契约与脚手架——抽取自一个成熟项目的 Harness 框架，去除了领域耦合，使其适用于任何新 Web 项目。

## 它提供什么

| 能力 | 载体 |
|------|------|
| **Agent 工作契约** | [`AGENTS.md`](AGENTS.md)（`CLAUDE.md` 为其软链接） |
| **验证门禁** | [`scripts/harness.sh`](scripts/harness.sh)（栈无关骨架，提交前必跑） |
| **Worktree 隔离开发 + 分支规范** | [`docs/spec/worktree-workflow.md`](docs/spec/worktree-workflow.md) + `.worktrees/` |
| **持续学习** | [`docs/guide/common-pitfalls.md`](docs/guide/common-pitfalls.md) + `docs/plans/` 交接生命周期 |
| **启发式探索（HL）** | [`docs/design/heuristic-exploration-framework.md`](docs/design/heuristic-exploration-framework.md) + [`.claude/skills/heuristic-exploration/`](.claude/skills/heuristic-exploration/SKILL.md) skill |

## 目录导航

```
.
├── AGENTS.md                                  # 代理工作契约（唯一可编辑的契约源）
├── CLAUDE.md  ->  AGENTS.md                    # 软链接，勿单独编辑
├── .gitignore
├── .worktrees/                                # worktree 检出目录（内容被 git 忽略）
├── .claude/
│   └── skills/heuristic-exploration/          # 可复用的启发式探索 skill
├── scripts/
│   └── harness.sh                             # 验证门禁（按你的栈填实 TODO 段）
└── docs/
    ├── spec/worktree-workflow.md              # worktree 工作流与分支规范
    ├── design/heuristic-exploration-framework.md  # 启发式探索：循环 + 固定评估器 + 单一可编辑程序范式
    ├── guide/common-pitfalls.md               # 踩坑沉淀（持续学习）
    └── plans/{active,completed}/              # 计划与会话交接
```

## 关键约定：CLAUDE.md 与 AGENTS.md 软链接

`CLAUDE.md` 是 `AGENTS.md` 的符号链接，二者内容始终一致。不同工具默认读取其中之一（Claude 读 `CLAUDE.md`，部分工具读 `AGENTS.md`），软链接让两者自动同步。**修改契约时只编辑 `AGENTS.md`。**

如软链接丢失（例如某些打包/拷贝方式不保留软链接），在仓库根目录重建：

```bash
ln -sf AGENTS.md CLAUDE.md
```

## 用它起一个新项目

```bash
# 1) 拷贝模板内容到新项目目录后，初始化 git
git init && git add -A && git commit -m "chore: bootstrap from vibe-coding-template"

# 2) 确认软链接存在（拷贝可能丢失软链接）
ls -l CLAUDE.md   # 应显示 CLAUDE.md -> AGENTS.md；缺失则 ln -sf AGENTS.md CLAUDE.md

# 3) 按你的技术栈填实门禁与契约
#    - scripts/harness.sh：替换 lint / typecheck / test / build 的 TODO 段
#    - AGENTS.md：填「技术栈」「常用命令」「验证」三节
#    - docs/design/heuristic-exploration-framework.md：仅当采用启发式探索时，填实固定评估器与评分公式

# 4) 跑一次门禁，确认链路通
./scripts/harness.sh
```

之后所有开发都走 worktree 工作流（见 `docs/spec/worktree-workflow.md`），不在 `main` 直接开发。
