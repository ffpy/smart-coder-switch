#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(
  cd "$(dirname "${BASH_SOURCE[0]}")/.."
  pwd
)"

cd "${ROOT_DIR}"

VERSION_FILE="${ROOT_DIR}/VERSION"

if [[ ! -f "${VERSION_FILE}" ]]; then
  echo "VERSION file not found: ${VERSION_FILE}" >&2
  exit 1
fi

FILE_VERSION="$(
  tr -d '[:space:]' < "${VERSION_FILE}"
)"

# 正常构建自动读取 VERSION。
# BUILD_VERSION 仅供烟测或 CI 临时覆盖。
BUILD_VERSION="${BUILD_VERSION:-${FILE_VERSION}}"

if [[ -z "${BUILD_VERSION}" ]]; then
  echo "version must not be empty" >&2
  exit 1
fi

if [[ ! "${BUILD_VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "invalid version: ${BUILD_VERSION}" >&2
  echo "expected format such as 0.1.0 or 0.1.0-rc1" >&2
  exit 1
fi

BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

if [[ -n "${COMMIT:-}" ]]; then
  BUILD_COMMIT="${COMMIT}"
elif git rev-parse --verify HEAD >/dev/null 2>&1; then
  BUILD_COMMIT="$(
    git rev-parse --short=12 HEAD
  )"

  if ! git diff \
    --quiet \
    --ignore-submodules \
    HEAD -- 2>/dev/null
  then
    BUILD_COMMIT="${BUILD_COMMIT}-dirty"
  fi
else
  BUILD_COMMIT="unknown"
fi

# 前端构建：产物经 //go:embed all:dist 嵌入 Go 二进制。
# 仅当 web/frontend/package.json 存在时执行，兼容不含前端的分支/环境。
if [[ -f "${ROOT_DIR}/web/frontend/package.json" ]]; then
  echo "===== FRONTEND BUILD ====="
  (
    cd "${ROOT_DIR}/web/frontend"

    if ! command -v pnpm >/dev/null 2>&1; then
      echo "error: pnpm is required to build the frontend" >&2
      exit 1
    fi

    if [[ ! -d node_modules ]]; then
      pnpm install --frozen-lockfile
    fi

    pnpm build
  )
  echo "frontend built into internal/web/dist"
fi

TARGET_OS="${GOOS:-$(go env GOOS)}"
TARGET_ARCH="${GOARCH:-$(go env GOARCH)}"
OUTPUT_NAME="smart-coder-switch"
if [[ "${TARGET_OS}" == "windows" ]]; then
  OUTPUT_NAME+=".exe"
fi
OUTPUT_PATH="dist/${OUTPUT_NAME}"

mkdir -p dist

GOOS="${TARGET_OS}" \
GOARCH="${TARGET_ARCH}" \
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags="\
-s -w \
-X smart-coder-switch/internal/buildinfo.Version=${BUILD_VERSION} \
-X smart-coder-switch/internal/buildinfo.Commit=${BUILD_COMMIT} \
-X smart-coder-switch/internal/buildinfo.BuildTime=${BUILD_TIME}" \
  -o "${OUTPUT_PATH}" \
  ./cmd/smart-coder-switch

echo "built ${OUTPUT_PATH} (${TARGET_OS}/${TARGET_ARCH})"
if [[ "${TARGET_OS}" == "$(go env GOOS)" && "${TARGET_ARCH}" == "$(go env GOARCH)" ]]; then
  "./${OUTPUT_PATH}" -version
fi
