ARG GO_VERSION=1.26.0
ARG ALPINE_VERSION=3.22

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

# 构建容器有独立的网络环境，不会继承宿主机的 GOPROXY / GOFLAGS 等配置。
# 默认使用可访问性较好的代理，按需通过 --build-arg 覆盖，例如：
#   --build-arg GOPROXY=https://goproxy.cn,direct
#   --build-arg GOPROXY=off --build-arg GOSUMDB=off   # 完全离线/内网私服
ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ARG GOTOOLCHAIN=local
ARG MOD_DOWNLOAD_RETRIES=3

ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB} \
    GOTOOLCHAIN=${GOTOOLCHAIN}

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./

# 代理不可用时明确报错并重试，避免只看到 150s 的 i/o timeout。
RUN set -eux; \
    attempt=1; \
    while ! go mod download; do \
        if [ "$attempt" -ge "${MOD_DOWNLOAD_RETRIES}" ]; then \
            echo "go mod download failed after ${attempt} attempt(s)." >&2; \
            echo "The build container cannot reach the Go module proxy." >&2; \
            echo "Retry with: --build-arg GOPROXY=<mirror>,direct (and GOSUMDB=off if that mirror cannot reach sum.golang.org)." >&2; \
            exit 1; \
        fi; \
        attempt=$((attempt + 1)); \
        echo "go mod download failed, retrying (${attempt}/${MOD_DOWNLOAD_RETRIES}) ..." >&2; \
        sleep 5; \
    done

COPY build ./build
COPY cmd ./cmd
COPY config ./config
COPY embed ./embed
COPY internal ./internal
COPY main.go doc.go README.md ./

RUN go run ./build/build.go && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/superflare .

FROM alpine:${ALPINE_VERSION} AS runtime

RUN apk add --no-cache ca-certificates tzdata iproute2 net-tools

WORKDIR /data

COPY --from=builder /out/superflare /opt/superflare/superflare
COPY tools/docker/docker-entrypoint.sh /opt/superflare/docker-entrypoint.sh

RUN chmod +x /opt/superflare/superflare /opt/superflare/docker-entrypoint.sh && \
    mkdir -p /data /data/var/uploads /data/var/cache

ENV FLARE_PORT=3636

VOLUME ["/data"]
EXPOSE 3636

ENTRYPOINT ["/opt/superflare/docker-entrypoint.sh"]
CMD []
