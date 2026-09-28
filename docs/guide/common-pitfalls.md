# Common Pitfalls

## 本地依赖监听地址与 `localhost` 的 IPv6 解析

本地依赖若显式监听 `127.0.0.1`，调用方默认地址也应使用 `127.0.0.1`，不要写成 `localhost`。部分系统会优先把 `localhost` 解析为 `::1`，导致服务健康检查通过，但运行时请求报 `dial tcp [::1]:<port>: connect: connection refused`。

## Parser tests cannot import `app`

Run parser tests from `parser/` with the virtual environment's Python module entrypoint:

```bash
cd parser
.venv/bin/python -m pytest -q
```

Invoking `.venv/bin/pytest` directly can leave `.venv/bin` at `sys.path[0]` under some Python/pytest combinations, causing `ModuleNotFoundError: No module named 'app'` even when the current directory is `parser/`.

## Worktree creation may not have `origin/main`

Check branches and remotes before following the default worktree command:

```bash
git branch --all --verbose
git remote -v
```

Some local clones are intentionally project-local and have no remote configured. In that case, create the task worktree from local `main` instead of `origin/main`:

```bash
git worktree add .worktrees/<topic> -b <branch> main
```

## Background `go run` leaks the compiled child on kill

`go run` does not forward signals to the compiled child process (golang/go#40467). A script that starts `go run ./cmd/... &`, tracks `$!`, and later `kill`s that PID only kills the `go run` wrapper — the compiled binary keeps running and holds its ports. The next run then fails with "address already in use" or, worse, health checks silently hit the leaked stale process and the whole verification runs against the wrong binary.

Standard fix (applied in `scripts/start.sh` and `scripts/smoke.sh`): `go build -o <tmp>/bin/ ./cmd/...` once, execute the binaries directly, and track those PIDs. For background subshells like `(cd parser && .venv/bin/python ...)`, add `exec` (`(cd parser && exec .venv/bin/python ...)`) so the subshell process *becomes* the server and `$!` is killable.

Related: non-interactive shells start background jobs with SIGINT ignored, so test cleanup paths with SIGTERM, not `kill -INT`.

## Worktree 里给 `.local/bin` 建符号链接:先确认 cwd,`ln -sf` 会覆盖真文件

**现象**:在 worktree 中想用相对路径给 `.local/bin/meilisearch` 建指向主检出的符号链接,结果把主检出的真二进制替换成了悬空链接,只能重新下载。

**根因**:两个叠加错误。
1. Bash 工具的 cwd 在不同调用间可能已回到主检出(如 worktree 被移除后),相对路径命令实际在主检出执行。
2. `ln -sf target link` 当 `link` 是已存在的**普通文件**时会直接把它替换成符号链接——`-f` 不保护真文件。

**标准解法**:
- 涉及 `.local/`、`.env` 等跨 worktree 共享资源时,先 `pwd` 确认位置,或用绝对路径。
- 覆盖前先检查:`[[ -f link && ! -L link ]] && echo "是真文件,不能 ln -sf"`。
- 从 `.worktrees/<topic>/.local/bin/` 指回主检出需要 4 级 `../`:`../../../../.local/bin/meilisearch`。
- parser `.venv` 同理不可共享拷贝;用符号链接指向主检出的 venv 即可(`ln -s ../../../parser/.venv parser/.venv`),worktree 里若已有不完整的 venv 目录先删除再链接。

## `npm ci` 会顺着 node_modules 符号链接清空共享目标

**现象**:worktree 里把 `web/node_modules` 符号链接到主检出,harness 执行 `npm ci` 后,主检出的 node_modules 被清空。

**根因**:`npm ci` 的语义是先删除 node_modules 再重装;遇到符号链接时它删除的是**链接目标的内容**,而不是链接本身。

**标准解法**:node_modules 不要跨目录符号链接。worktree 里让 harness 自己 `npm ci` 出一份真实的(它会自动做);主检出被误清空后在主检出 `npm ci --prefer-offline --no-audit` 恢复。同类教训见上一条 `ln -sf` 覆盖真文件——凡涉及"删除重建"语义的工具(npm ci、rm -rf、ln -sf)都不能指向共享资源。

## 删库重建后仍报 "database schema is outdated"：有残留旧二进制又建了旧库

**现象**：schema 大版本升级（如 v1→v2 media 改名）后按流程删掉了 `data/open-ima.db`，跑 `scripts/start.sh` 却仍然报 `database schema is outdated; delete the db file and restart`。检查 `data/open-ima.db` 是刚建的，但表结构是旧版（`documents` 而非 `medias`），`PRAGMA user_version` 是旧版本号。

**根因**：`start.sh` 每次都是 `go build` 当前源码，本身不会产出旧库。是**残留的旧版本二进制**（典型：早先用 `nohup .local/bin/open-ima &` 之类方式手动拉起的实例，或旧 worktree 里的二进制）在同一 cwd 下先启动/重启，按它的旧 schema 建了库并写入旧 `user_version`；随后新代码打开这个"新文件旧 schema"的库，守卫正确拒绝。这类残留进程同时还会占住 8080/8100/7700 端口，制造"端口经常冲突"的假象。用 `go version -m <二进制>` 看 `mod ... v0.0.0-<时间>-<commit>` 可确认它落后 HEAD 多少提交。

**标准解法**：
1. 先清残留：`lsof -nP -iTCP:8080 -iTCP:8100 -iTCP:7700 -sTCP:LISTEN`、`ps aux | grep -E "open-ima|uvicorn"`，杀掉旧实例和它的 wrapper（nohup 的父 shell 可能还活着并会重拉子进程）。
2. 再删库：连同 `-wal`/`-shm` 一起删（`rm -f data/open-ima.db*`），旧 meili 索引目录（`data/meili`）也一并删。
3. 重跑 `start.sh` 验证：`sqlite3 data/open-ima.db "PRAGMA user_version;"` 应为当前版本，`.tables` 应为新表名。
4. 预防：`.local/bin/open-ima` 这类手动放置的二进制不属于任何脚本管理，版本一过期就是隐患；确认无用后删除，或至少 `go version -m` 核对与 HEAD 同提交再使用。

## Agent 引用句柄必须在「流式发射前」改写，且工具输出要自带句柄

**现象**：浏览器实测发现最终答案渲染出 `[c2]`、`[分块2/33]` 这类内部记号——前者是流式 token 直接用了引擎原始答案（改写只在持久化前做），后者是模型照抄 read_document 输出里的 `[分块 N/M]` 标签当引用。

**根因**：句柄改写（`[cN]/[wN]` → references 序号）放错了阶段；工具输出的分块标签用的是人类记号而非模型可引用的 cN 句柄。

**标准解法**：
1. 改写前移到引擎 `emitAnswer`：references 确定后、token 回放前完成，保证流式内容、持久化内容、引用区三者一致。
2. 工具输出里凡是希望模型引用的条目，一律带已注册的句柄标签（如 `[c1 分块 1/3]`），并把 citations+handles 回填进 `ToolResult.Data`，引擎据此收集 references。
3. 改写索引除分块/URL 精确键外补媒体键兜底，让 `[dN]` 文档句柄能落到该媒体的首条引用。

## DuckDuckGo 匿名入口高频请求返回 202 异常挑战

**现象**：ReAct 多轮规划一次问答可能发出多次 web_search；DDG `html.duckduckgo.com` 在短时间多次请求后返回 HTTP 202（anomaly challenge），表现为联网搜索连续失败、答案声明"联网检索不可用"。

**标准解法**：`internal/infrastructure/websearch/duckduckgo.go` 对 202/403/429 按 2s/4s 退避重试，末次降级到 `lite.duckduckgo.com` 精简页（DOM 类名不同：`result-link`/`result-snippet`）。注意 202 可能是 IP 级短时封禁，重试只能缓解不能根治；追求稳定应在设置中心切到自建 SearxNG。

## 出口 IP 被 DuckDuckGo 长期封禁（202 持续整天不恢复）

**现象**：`html`/`lite` 两个子域对任何 UA、参数、POST 都返回 202 空结果页，持续超过 12 小时，近似 IP 级永久封禁；`api.duckduckgo.com` Instant Answer JSON 能通但对中文财经查询基本返回空。

**标准解法**：设置中心切到 AnySearch provider（结构化 API，Bearer key 鉴权，`websearch.AnySearch`）。DuckDuckGo 保留 Instant Answer JSON 作为结果页被封时的最后兜底。

**免密钥替代实测全灭（2026-09-28，勿再浪费时间）**：Bing HTML 反爬返回无关内容、Bing RSS 多词查询 0 结果且忽略关键词、Mojeek/Ecosia/Qwant 有 JS 挑战或 403、公共 SearxNG 实例全部禁用匿名 JSON、百度对无 cookie 裸请求 302 到 wappass 图形验证码（cookie 预热可用但脆弱，已实现后移除）、搜狗第二次请求即弹 antispider 验证码、360 跳转链接匿名 400。结论：中文场景免密钥抓页没有稳定解，要么 AnySearch 这类免费额度 API，要么自建 SearxNG。
