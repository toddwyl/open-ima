#!/usr/bin/env bash
# scripts/install.sh —— 新人一键安装 Open IMA 全部依赖并生成配置。
#
# 做完的事：
#   1. 检查 / 安装工具链（Go 1.26+、Node.js 20+、Python 3.11+、Ollama）
#   2. 下载 Meilisearch v1.10.3 到 .local/bin（按当前系统与架构自动选包）
#   3. 创建 parser/.venv 并安装 Python 依赖
#   4. 安装前端 npm 依赖
#   5. 拉取本地 embedding 模型 bge-m3
#   6. 生成 .env（随机 Meilisearch 密钥；LLM / AnySearch 密钥留空，
#      首次使用时在前端配置中心填写即可）
#
# 完成后直接运行 ./scripts/start.sh 启动。
set -euo pipefail
cd "$(dirname "$0")/.."

MEILI_VERSION="v1.10.3"
EMBED_MODEL="bge-m3"

info() { echo "==> $*"; }
warn() { echo "!!  $*" >&2; }
die() { echo "xx  $*" >&2; exit 1; }

# ---- 0. 平台 -----------------------------------------------------------------
OS="$(uname -s)"
ARCH="$(uname -m)"
case "${OS}" in
  Darwin|Linux) ;;
  *) die "暂不支持 ${OS}，请在 macOS 或 Linux 上运行（Windows 请用 deploy/docker）" ;;
esac

BREW=""
if command -v brew >/dev/null 2>&1; then BREW="brew"; fi

# ---- 1. 工具链 ---------------------------------------------------------------
info "1/6 检查工具链（Go / Node.js / Python / Ollama）"

MISSING=()
command -v go      >/dev/null 2>&1 || MISSING+=("go")
command -v node    >/dev/null 2>&1 || MISSING+=("node")
command -v npm     >/dev/null 2>&1 || MISSING+=("node")
command -v python3 >/dev/null 2>&1 || MISSING+=("python@3.11")
command -v ollama  >/dev/null 2>&1 || MISSING+=("ollama")

if [[ ${#MISSING[@]} -gt 0 ]]; then
  # 去重
  mapfile -t MISSING < <(printf '%s\n' "${MISSING[@]}" | sort -u)
  if [[ -n "${BREW}" ]]; then
    info "    缺少：${MISSING[*]}，使用 Homebrew 自动安装"
    brew install "${MISSING[@]}"
  else
    die "缺少依赖：${MISSING[*]}。请先安装 Go 1.26+、Node.js 20+、Python 3.11+、Ollama（https://ollama.com）后重跑本脚本"
  fi
fi

GO_VER="$(go version | grep -oE 'go[0-9]+\.[0-9]+' | head -1 | tr -d 'go')"
GO_MAJOR="${GO_VER%%.*}"; GO_MINOR="${GO_VER##*.}"
if (( GO_MAJOR < 1 || (GO_MAJOR == 1 && GO_MINOR < 26) )); then
  die "Go 版本过低（$(go version | awk '{print $3}')），需要 1.26+"
fi
NODE_MAJOR="$(node --version | tr -d 'v' | cut -d. -f1)"
(( NODE_MAJOR >= 20 )) || die "Node.js 版本过低（$(node --version)），需要 20+"
python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)' \
  || die "Python 版本过低（$(python3 --version)），需要 3.11+"

# ---- 2. Meilisearch ----------------------------------------------------------
info "2/6 Meilisearch ${MEILI_VERSION}"
MEILI_BIN=".local/bin/meilisearch"
if [[ -x "${MEILI_BIN}" ]] || command -v meilisearch >/dev/null 2>&1; then
  info "    已存在，跳过下载"
else
  case "${OS}-${ARCH}" in
    Darwin-arm64)  ASSET="meilisearch-macos-apple-silicon" ;;
    Darwin-x86_64) ASSET="meilisearch-macos-amd64" ;;
    Linux-x86_64)  ASSET="meilisearch-linux-amd64" ;;
    Linux-aarch64) ASSET="meilisearch-linux-aarch64" ;;
    *) die "没有适配 ${OS}/${ARCH} 的 Meilisearch 预编译包，请手动安装到 PATH" ;;
  esac
  mkdir -p .local/bin
  curl -L --fail --retry 3 -o "${MEILI_BIN}" \
    "https://github.com/meilisearch/meilisearch/releases/download/${MEILI_VERSION}/${ASSET}"
  chmod +x "${MEILI_BIN}"
fi

# ---- 3. parser Python 依赖 ----------------------------------------------------
info "3/6 parser Python 依赖"
if [[ ! -x parser/.venv/bin/python ]]; then
  python3 -m venv parser/.venv
fi
parser/.venv/bin/pip install --quiet --upgrade pip
parser/.venv/bin/pip install --quiet -r parser/requirements-dev.txt

# ---- 4. 前端依赖 ---------------------------------------------------------------
info "4/6 前端依赖"
npm --prefix web ci --prefer-offline --no-audit

# ---- 5. embedding 模型 ---------------------------------------------------------
info "5/6 embedding 模型 ${EMBED_MODEL}"
OLLAMA_STARTED_BY_US=""
if ! curl --fail --silent http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
  info "    临时拉起 Ollama 服务用于拉取模型"
  mkdir -p data
  ollama serve >data/ollama-install.log 2>&1 &
  OLLAMA_STARTED_BY_US="$!"
  for _ in $(seq 1 60); do
    curl --fail --silent http://127.0.0.1:11434/api/tags >/dev/null 2>&1 && break
    sleep 0.5
  done
fi
if ! curl --fail --silent http://127.0.0.1:11434/api/tags | grep -q "${EMBED_MODEL}"; then
  ollama pull "${EMBED_MODEL}"
fi
if [[ -n "${OLLAMA_STARTED_BY_US}" ]]; then
  kill "${OLLAMA_STARTED_BY_US}" 2>/dev/null || true
  wait "${OLLAMA_STARTED_BY_US}" 2>/dev/null || true
fi

# ---- 6. .env -------------------------------------------------------------------
info "6/6 生成 .env"
if [[ -f .env ]]; then
  info "    .env 已存在，保持不变"
else
  MEILI_KEY="$(openssl rand -hex 24 2>/dev/null || head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  sed "s/^IMA_MEILI_API_KEY=.*/IMA_MEILI_API_KEY=${MEILI_KEY}/" .env.example > .env
  info "    已生成随机 Meilisearch 密钥；LLM / AnySearch 密钥留空，首次使用在前端配置中心填写"
fi

echo
info "安装完成。启动：./scripts/start.sh"
info "打开 http://localhost:8080 后，在左下角「配置中心」填写聊天模型 API Key；"
info "需要 Agent 联网搜索时，在同一面板粘贴 AnySearch Key。"
