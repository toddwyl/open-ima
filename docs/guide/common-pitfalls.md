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
