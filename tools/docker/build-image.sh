#!/usr/bin/env sh
set -eu

IMAGE_NAME="${1:-superflare}"
TAG="${2:-latest}"
PLATFORM="${PLATFORM:-}"
PLATFORMS="${PLATFORMS:-}"
PUSH="${PUSH:-0}"
NO_CACHE="${NO_CACHE:-0}"
BUILDER="${BUILDER:-}"

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

# ---------------------------------------------------------------------------
# 代理转发
#
# HTTP(S)_PROXY 只对 `RUN` 里的命令（apk add、go mod download）生效，不会影响
# buildkitd 拉取基础镜像元数据那一步 —— 后者要在 builder 上配置：
#   docker buildx create --driver-opt env.HTTPS_PROXY=http://host:port ...
#
# 大小写都转发一份：Go 工具链、apk/libfetch、curl 认的变量名不一致，两个都给最稳。
# 这些是 BuildKit 的预定义参数，不会污染层缓存，也不会留在镜像历史里。
# ---------------------------------------------------------------------------

# 打印代理地址时把密码打码，避免日志泄露凭据。
mask_proxy() {
  printf '%s' "$1" | sed -e 's#\(://[^:/@]*:\)[^@]*@#\1***@#'
}

proxy_get() {
  # $1 = 大写变量名，$2 = 小写变量名；未设置时输出空串（set -u 下也安全）。
  eval "value=\${$1:-}"
  if [ -z "$value" ]; then
    eval "value=\${$2:-}"
  fi
  printf '%s' "$value"
}

PROXY_ARGS=""
PROXY_NOTES=""
PROXY_LOCALHOST=0

add_proxy() {
  key_upper="$1"
  key_lower="$2"
  label="$3"

  value=$(proxy_get "$key_upper" "$key_lower")
  [ -z "$value" ] && return 0

  # 含空白的值无法安全地拼进 --build-arg，提前报错而不是静默传错。
  case "$value" in
    *[[:space:]]*)
      echo "$label contains whitespace and cannot be forwarded safely: $(mask_proxy "$value")" >&2
      exit 1
      ;;
  esac

  PROXY_ARGS="$PROXY_ARGS --build-arg $key_upper=$value --build-arg $key_lower=$value"
  PROXY_NOTES="$PROXY_NOTES
  $key_upper=$(mask_proxy "$value")"

  # 127.0.0.1 / localhost 在构建容器里指向容器自身，代理跑在宿主机上时必须换成
  # 宿主机地址，否则连接被拒。这里只提示，不擅自改写（可能用户就是在容器里跑代理）。
  case "$key_upper" in
    *PROXY)
      case "$value" in
        *//127.0.0.1:* | *//localhost:* | *//[::1]:*)
          PROXY_LOCALHOST=1
          ;;
      esac
      ;;
  esac
}

add_proxy HTTP_PROXY http_proxy "HTTP_PROXY"
add_proxy HTTPS_PROXY https_proxy "HTTPS_PROXY"
add_proxy ALL_PROXY all_proxy "ALL_PROXY"
add_proxy NO_PROXY no_proxy "NO_PROXY"

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

# 拆分 PROXY_ARGS 时关掉通配符展开，代理串里的 * / ? 不应被当成 glob。
if [ -n "$PROXY_ARGS" ]; then
  set -f
  set -- "$@" $PROXY_ARGS
  set +f
fi

if [ "$NO_CACHE" = "1" ]; then
  set -- "$@" --no-cache
fi

if [ -n "$BUILDER" ]; then
  if [ "$USE_BUILDX" = "1" ]; then
    set -- "$@" --builder "$BUILDER"
  else
    echo "warning: BUILDER=$BUILDER ignored, it only applies to PUSH=1 / PLATFORMS builds." >&2
  fi
fi

set -- "$@" "$TEMP_ROOT"

echo "GOPROXY: $GOPROXY_OVERRIDE"

if [ -n "$PROXY_NOTES" ]; then
  echo "Build args (proxy):$PROXY_NOTES"

  # metadata 拉取由 buildkitd 发起，--build-arg 管不到，只能提前提示。
  if [ "$USE_BUILDX" = "1" ]; then
    echo "note: if loading base image metadata still times out, configure the proxy on the builder itself:" >&2
    echo "      docker buildx rm ${BUILDER:-<builder>} && docker buildx create --name ${BUILDER:-<builder>} \\" >&2
    echo "        --driver-opt env.HTTPS_PROXY=<proxy> --driver-opt env.HTTP_PROXY=<proxy> --use --bootstrap" >&2
  fi

  if [ "$PROXY_LOCALHOST" = "1" ]; then
    echo "warning: the proxy points at 127.0.0.1/localhost, which inside a build container means the container itself." >&2
    echo "         use the host address (e.g. 172.17.0.1) or --network host for the builder." >&2
  fi
fi

docker "$@"
