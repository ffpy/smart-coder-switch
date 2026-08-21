#!/usr/bin/env bash

if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

set -euo pipefail

ROOT_DIR="$(
  cd "$(dirname "${BASH_SOURCE[0]}")/.."
  pwd
)"

cd "${ROOT_DIR}"

PID_FILE="./log/smart-coder-switch.pid"
CONFIG_PATH="config.yaml"

# 解析参数
while [[ $# -gt 0 ]]; do
  case "$1" in
    -config)
      if [[ -z "${2:-}" ]]; then
        echo "error: -config requires a path argument" >&2
        exit 1
      fi
      CONFIG_PATH="$2"
      shift 2
      ;;
    -v)
      if [[ -z "${2:-}" ]]; then
        echo "error: -v requires a version argument" >&2
        exit 1
      fi
      BUILD_VERSION="$2"
      shift 2
      ;;
    --help|-h)
      echo "Usage: $0 [options] [-- <server-args>...]"
      echo ""
      echo "Options:"
      echo "  -config <path>   配置文件路径（默认: config.yaml）"
      echo "  -v <version>     覆盖构建版本号"
      echo "  --help, -h       显示帮助"
      echo ""
      echo "示例:"
      echo "  $0                              # 编译并重启服务"
      echo "  $0 -config /etc/scs.yaml        # 使用指定配置文件"
      echo "  $0 -v 0.2.0                     # 指定版本号编译并重启"
      echo "  $0 -- -port 8080                # 传递额外参数给服务进程"
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      echo "error: unknown option: $1" >&2
      echo "usage: $0 [-config <path>] [-v <version>] [-- <server-args>...]" >&2
      exit 1
      ;;
  esac
done

# 第一步：编译
echo "===== BUILD ====="
BUILD_VERSION="${BUILD_VERSION:-}" ./scripts/build.sh

echo ""
echo "===== DEPLOY ====="

# 第二步：停止旧进程
if [[ -f "${PID_FILE}" ]]; then
  OLD_PID="$(cat "${PID_FILE}")"
  if kill -0 "${OLD_PID}" 2>/dev/null; then
    echo "stopping old process (pid ${OLD_PID})"
    kill -TERM "${OLD_PID}" 2>/dev/null || true

    # 等待进程退出（最多 15 秒）
    for _ in $(seq 1 150); do
      if ! kill -0 "${OLD_PID}" 2>/dev/null; then
        break
      fi
      sleep 0.1
    done

    # 强制杀死仍未退出的进程
    if kill -0 "${OLD_PID}" 2>/dev/null; then
      echo "old process (pid ${OLD_PID}) did not exit gracefully, killing"
      kill -KILL "${OLD_PID}" 2>/dev/null || true
    fi

    wait "${OLD_PID}" 2>/dev/null || true
    echo "old process stopped"
  else
    echo "old process (pid ${OLD_PID}) is not running"
  fi
  rm -f "${PID_FILE}"
else
  echo "no pid file found at ${PID_FILE}, skipping stop"
fi

# 第三步：启动新进程
mkdir -p ./log
setsid ./dist/smart-coder-switch \
  -config "${CONFIG_PATH}" \
  "$@" \
  >/dev/null 2>&1 &

NEW_PID="$!"
echo "${NEW_PID}" > "${PID_FILE}"
echo "started smart-coder-switch (pid ${NEW_PID}) with config ${CONFIG_PATH}"

# 等待进程启动并检查健康
WAIT_URL="http://127.0.0.1:18082/health"

# 从配置中提取监听地址（如果自定义了配置文件）
if [[ -f "${CONFIG_PATH}" ]]; then
  CONFIG_ADDR="$(grep -oP '^\s*address:\s*\K.*' "${CONFIG_PATH}" 2>/dev/null || true)"
  if [[ -n "${CONFIG_ADDR}" ]]; then
    CONFIG_ADDR="$(echo "${CONFIG_ADDR}" | tr -d '[:space:]')"
    CONFIG_ADDR="${CONFIG_ADDR/0.0.0.0/127.0.0.1}"
    WAIT_URL="http://${CONFIG_ADDR}/health"
  fi
fi

echo "waiting for health check at ${WAIT_URL} ..."

for _ in $(seq 1 50); do
  if curl --noproxy '*' -fsS "${WAIT_URL}" >/dev/null 2>&1; then
    echo "health check passed"
    echo "===== DEPLOY SUCCESS ====="
    exit 0
  fi
  sleep 0.1
done

echo "error: health check timed out after 5 seconds" >&2
echo "check logs for details" >&2
exit 1
