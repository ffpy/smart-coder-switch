#!/usr/bin/env bash

if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

set -euo pipefail

cd "$(dirname "$0")"/..

PID_FILE="./log/smart-coder-switch.pid"

# 杀死旧进程
if [[ -f "${PID_FILE}" ]]; then
  OLD_PID="$(cat "${PID_FILE}")"
  if kill -0 "${OLD_PID}" 2>/dev/null; then
    echo "killing old process ${OLD_PID}"
    kill "${OLD_PID}"
    wait "${OLD_PID}" 2>/dev/null || true
  fi
  rm -f "${PID_FILE}"
fi

# 启动新进程
mkdir -p ./log
setsid ./dist/smart-coder-switch "$@" >/dev/null 2>&1 &
NEW_PID="$!"
echo "${NEW_PID}" > "${PID_FILE}"
echo "started smart-coder-switch (pid ${NEW_PID})"
