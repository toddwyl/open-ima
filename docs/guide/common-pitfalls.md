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
