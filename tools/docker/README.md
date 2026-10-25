# Docker 构建说明

本目录存放 SuperFlare 的 Docker 构建、运行和 Compose 示例文件。

项目根目录下的 `config.yml`、`apps.yml`、`bookmarks.yml`、`ports.yaml` 是运行时数据文件，默认被 `.gitignore` 排除，不会随 Git 仓库提交。Docker 镜像构建不依赖这些本地文件；容器首次启动后，SuperFlare 会在 `/data` 中自动补齐缺失的运行配置。

## 文件说明

- `Dockerfile`：Linux 容器多阶段构建文件
- `docker-entrypoint.sh`：容器入口脚本，会从 `/data` 工作目录启动 SuperFlare
- `build-image.sh`：Linux / macOS 构建脚本
- `build-image.ps1`：Windows PowerShell 构建脚本
- `docker-compose.yml`：基础 Compose 运行示例
- `docker-compose.host-proc.yml`：读取宿主机 `/proc` 的可选叠加配置

## 运行时数据

容器内可写运行时数据统一放在 `/data`：

- `/data/config.yml`
- `/data/apps.yml`
- `/data/bookmarks.yml`
- `/data/ports.yaml`
- `/data/.env`（可选）
- `/data/var/`

建议用 Docker volume 或宿主机目录持久化 `/data`。镜像升级或容器重建时，只要复用同一个 `/data`，已有配置不会被覆盖。

## 构建镜像

Linux / macOS：

```bash
chmod +x ./tools/docker/build-image.sh
./tools/docker/build-image.sh superflare latest
```

Windows PowerShell：

```powershell
.\tools\docker\build-image.ps1 -ImageName superflare -Tag latest
```

强制无缓存构建：

```bash
NO_CACHE=1 ./tools/docker/build-image.sh superflare latest
```

指定平台构建：

```bash
PLATFORM=linux/amd64 ./tools/docker/build-image.sh superflare latest
PLATFORM=linux/arm64 ./tools/docker/build-image.sh superflare latest
```

## 构建时下载依赖失败（GOPROXY）

`go mod download` 在构建容器内执行，**不会继承宿主机或宿主 Docker 的 Go 网络设置**。如果服务器直连 `proxy.golang.org` 超时，会出现：

```text
=> ERROR [builder  5/12] RUN go mod download
go: github.com/andybalholm/brotli@v1.2.0: Get "https://proxy.golang.org/...": dial tcp ...: i/o timeout
```

构建脚本已经默认把 `GOPROXY` 通过 `--build-arg` 传进容器（默认 `https://goproxy.cn,https://proxy.golang.org,direct`），通常无需额外操作。需要改用别的源时用环境变量覆盖：

```bash
# 换镜像源
GOPROXY_OVERRIDE=https://goproxy.io,direct ./tools/docker/build-image.sh superflare latest

# 公司内部 Go 私服（私服无法访问 sum.golang.org 时关掉校验）
GOPROXY_OVERRIDE=http://goproxy.example.internal,direct \
GOSUMDB_OVERRIDE=off \
./tools/docker/build-image.sh superflare latest
```

Windows PowerShell 对应参数是 `-GoProxy` / `-GoSumDB`：

```powershell
.\tools\docker\build-image.ps1 -ImageName superflare -Tag latest `
  -GoProxy "https://goproxy.cn,direct"
```

### 离线 / 内网构建

如果构建服务器完全不能访问 `proxy.golang.org`，且没有可用的 Go 镜像源，不必死磕网络，用「别处构建、镜像搬运」更省事：

```bash
# 1) 在能联网的机器上正常构建
GOPROXY_OVERRIDE=https://goproxy.cn,direct ./tools/docker/build-image.sh junfucn/superflare latest

# 2) 导出镜像
docker save junfucn/superflare:latest -o superflare-latest.tar

# 3) 传到构建服务器后导入
scp superflare-latest.tar user@server:/tmp/
docker load -i /tmp/superflare-latest.tar

# 4) 在服务器上推送（推送只需要访问 registry-1.docker.io）
docker push junfucn/superflare:latest
```

如果服务器能连 Docker Hub 但连不上 Go 代理，也可以在能联网的机器上导出 module 缓存，再用 `RUN --mount=type=cache,target=/go/pkg/mod` 或 `COPY` 把它带进 builder 阶段。本仓库默认的 `Dockerfile` 走线上 `go mod download`，未启用缓存挂载。

## 推送到 Docker Hub

先登录 Docker Hub（推送需要 `registry-1.docker.io` 可达）：

```bash
docker login -u junfucn
```

构建并打上 Docker Hub 仓库标签：

```bash
./tools/docker/build-image.sh junfucn/superflare latest
```

推送：

```bash
docker push junfucn/superflare:latest
```

建议同时推送一个固定版本标签，方便回滚（用当天日期）：

```bash
docker tag junfucn/superflare:latest junfucn/superflare:20261025
docker push junfucn/superflare:20261025
```

### 一条命令构建并推送（buildx）

构建和推送可以合并，避免本地 `docker push` 二次上传：

```bash
# 单架构
PUSH=1 ./tools/docker/build-image.sh junfucn/superflare latest

# 多架构（amd64 + arm64），多架构必须用 buildx 直接推送，无法 docker load 到本地
PLATFORMS=linux/amd64,linux/arm64 \
PUSH=1 \
./tools/docker/build-image.sh junfucn/superflare 20261025
```

首次使用多架构需要先建好 builder（只需一次）：

```bash
docker buildx create --name superflare-builder --use --bootstrap
docker run --privileged --rm tonistiigi/binfmt --install all   # 非 amd64 宿主机模拟 arm64 时需要
```

没有 buildx 时，退回「本地构建 + `docker push`」的两步方式同样可用。

## 运行容器

基础 `docker run`：

```bash
docker run -d \
  --name superflare \
  -p 3636:3636 \
  -v superflare-data:/data \
  --restart unless-stopped \
  superflare:latest
```

基础 Docker Compose：

```bash
docker compose -f ./tools/docker/docker-compose.yml up -d
```

首次部署前建议编辑 `tools/docker/docker-compose.yml`：

- `FLARE_DISABLE_LOGIN=false`：启用登录
- `FLARE_DISABLE_LOGIN=true`：关闭登录
- `FLARE_COOKIE_SECRET`：请替换为稳定且足够随机的值，避免容器重建后登录会话失效

登录用户名和密码来自 `/data/config.yml`、`/data/.env` 或设置页保存结果。

## 读取宿主机端口信息

如果希望 Docker 部署时读取宿主机端口信息并用于 `/settings/ports`，需要显式挂载宿主机 `/proc`，并设置 `FLARE_PORT_PROC_ROOT`：

```bash
docker run -d \
  --name superflare \
  -p 3636:3636 \
  -v superflare-data:/data \
  -v /proc:/host/proc:ro \
  -e FLARE_PORT_PROC_ROOT=/host/proc \
  --restart unless-stopped \
  superflare:latest
```

Compose 叠加方式：

```bash
docker compose \
  -f ./tools/docker/docker-compose.yml \
  -f ./tools/docker/docker-compose.host-proc.yml \
  up -d
```

停止：

```bash
docker compose -f ./tools/docker/docker-compose.yml down
```

带宿主机 `/proc` 叠加配置时：

```bash
docker compose \
  -f ./tools/docker/docker-compose.yml \
  -f ./tools/docker/docker-compose.host-proc.yml \
  down
```
