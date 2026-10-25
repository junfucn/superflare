#!/usr/bin/env sh
set -eu

IMAGE_NAME="${1:-superflare}"
TAG="${2:-latest}"
PLATFORM="${PLATFORM:-}"
PLATFORMS="${PLATFORMS:-}"
PUSH="${PUSH:-0}"
NO_CACHE="${NO_CACHE:-0}"

# 构建容器不继承宿主机 Go 配置，必须显式传入。默认优先使用国内可访问的镜像，
# 需要走公司私服或完全离线时用环境变量覆盖：
#   GOPROXY_OVERRIDE=https://goproxy.cn,direct ./tools/docker/build-image.sh ...
GOPROXY_OVERRIDE="${GOPROXY_OVERRIDE:-https://goproxy.cn,https://proxy.golang.org,direct}"
GOSUMDB_OVERRIDE="${GOSUMDB_OVERRIDE:-sum.golang.org}"

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
TEMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/superflare-docker.XXXXXX")
IMAGE_REF="${IMAGE_NAME}:${TAG}"

cleanup() {
  rm -rf "$TEMP_ROOT"
}

trap cleanup EXIT INT TERM

if ! command -v docker >/dev/null 2>&1; then
  echo "docker executable was not found in PATH." >&2
  exit 1
fi

copy_item() {
  src="$1"
  dest="$2"
  mkdir -p "$(dirname "$dest")"
  cp -R "$src" "$dest"
}

for item in \
  build \
  cmd \
  config \
  embed \
  internal \
  tools/docker/Dockerfile \
  tools/docker/docker-entrypoint.sh \
  go.mod \
  go.sum \
  main.go \
  doc.go \
  README.md
do
  if [ ! -e "$REPO_ROOT/$item" ]; then
    echo "missing required path: $REPO_ROOT/$item" >&2
    exit 1
  fi
  copy_item "$REPO_ROOT/$item" "$TEMP_ROOT/$item"
done

DOCKERFILE="$TEMP_ROOT/tools/docker/Dockerfile"

# PUSH=1 或 PLATFORMS 多架构时走 buildx（多架构镜像无法 docker load 到本地，
# 必须构建期间直接推送）。
USE_BUILDX=0
if [ "$PUSH" = "1" ] || [ -n "$PLATFORMS" ]; then
  USE_BUILDX=1
fi

if [ "$USE_BUILDX" = "1" ]; then
  if ! docker buildx version >/dev/null 2>&1; then
    echo "docker buildx is required for PUSH=1 / PLATFORMS builds." >&2
    echo "Install the buildx plugin (Docker 23+ ships it) or use docker login + docker push." >&2
    exit 1
  fi

  if [ -z "$PLATFORMS" ]; then
    PLATFORMS="${PLATFORM:-linux/amd64}"
  fi

  set -- buildx build \
    --file "$DOCKERFILE" \
    --tag "$IMAGE_REF" \
    --platform "$PLATFORMS" \
    --build-arg "GOPROXY=$GOPROXY_OVERRIDE" \
    --build-arg "GOSUMDB=$GOSUMDB_OVERRIDE" \
    --push

  echo "Building and pushing $IMAGE_REF"
  echo "Platforms: $PLATFORMS"
else
  set -- build \
    --file "$DOCKERFILE" \
    --tag "$IMAGE_REF" \
    --build-arg "GOPROXY=$GOPROXY_OVERRIDE" \
    --build-arg "GOSUMDB=$GOSUMDB_OVERRIDE"

  if [ -n "$PLATFORM" ]; then
    set -- "$@" --platform "$PLATFORM"
  fi

  echo "Building Docker image $IMAGE_REF"
  if [ -n "$PLATFORM" ]; then
    echo "Platform: $PLATFORM"
  fi
fi

if [ "$NO_CACHE" = "1" ]; then
  set -- "$@" --no-cache
fi

set -- "$@" "$TEMP_ROOT"

echo "GOPROXY: $GOPROXY_OVERRIDE"

docker "$@"
