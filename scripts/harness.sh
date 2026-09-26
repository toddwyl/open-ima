#!/usr/bin/env bash
#
# scripts/harness.sh —— Open IMA 验证门禁
#
# 任何代码改动完成后、提交或声称完成之前，都必须运行本脚本且完整通过。
#
# 用法（在仓库根目录执行）：
#   ./scripts/harness.sh                      # 跑全部门禁
#   HARNESS_BASELINE_CHECK=1 ./scripts/harness.sh   # 额外跑基线回归（见文末）
#
set -euo pipefail

# 脚本位于 scripts/ 下，仓库根目录为其上一级。
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

# ---- 1. 空白/冲突标记检查（栈无关，必留）-------------------------------------
# 检出残留的行尾空白、冲突标记（<<<<<<<）等。仅在 git 仓库内有意义。
if git rev-parse --git-dir >/dev/null 2>&1; then
  echo "[harness] git diff --check"
  git diff --check
else
  echo "[harness] (跳过 git diff --check：当前目录不是 git 仓库，请先 git init)"
fi

# ---- 2. Lint --------------------------------------------------------------
echo "[harness] gofmt"
UNFORMATTED="$(find cmd internal web -name '*.go' -not -path 'web/node_modules/*' -print0 | xargs -0 gofmt -l)"
if [[ -n "${UNFORMATTED}" ]]; then
  echo "${UNFORMATTED}"
  echo "[harness] Go files need gofmt" >&2
  exit 1
fi
echo "[harness] go vet"
go vet ./...

# ---- 3. 类型检查 -----------------------------------------------------------
echo "[harness] frontend install"
(cd web && npm ci --prefer-offline --no-audit)
echo "[harness] frontend dependency audit"
(cd web && npm audit --audit-level=high)
echo "[harness] frontend typecheck"
(cd web && npm run typecheck)

# ---- 4. 单元测试 -----------------------------------------------------------
echo "[harness] Go tests"
go test ./... "$@"
echo "[harness] parser tests"
if [[ ! -x parser/.venv/bin/python ]]; then
  python3 -m venv parser/.venv
  parser/.venv/bin/python -m pip install --disable-pip-version-check -r parser/requirements-dev.txt
fi
(cd parser && .venv/bin/python -m pytest -q)
echo "[harness] frontend tests"
(cd web && npm test)

# ---- 5. 构建（可选）--------------------------------------------------------
echo "[harness] frontend build"
(cd web && npm run build)
echo "[harness] static Go build"
CGO_ENABLED=0 go build ./...
echo "[harness] shell syntax"
bash -n scripts/*.sh
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  echo "[harness] compose config"
  docker compose config --quiet
else
  echo "[harness] compose config skipped (Docker unavailable)"
fi

# ---- 6. 基线回归（可选）----------------------------------------------------
# 启发式探索项目可在此挂接「基线不退化」检查：把当前产物与已接受基线对比，
# 若关键指标退化则失败。设 HARNESS_BASELINE_CHECK=1 时才运行。
if [[ "${HARNESS_BASELINE_CHECK:-}" == "1" ]]; then
  echo ""
  echo "[harness] baseline regression check (HARNESS_BASELINE_CHECK=1)"
  echo "[harness] process smoke"
  SMOKE_MODE=process ./scripts/smoke.sh
fi

echo "[harness] OK"
