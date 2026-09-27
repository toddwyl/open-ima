# Common Pitfalls

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
