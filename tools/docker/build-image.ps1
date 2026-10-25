param(
    [string]$ImageName = "superflare",
    [string]$Tag = "latest",
    [string]$Platform = "",
    [switch]$NoCache,
    [switch]$Push,
    [string]$GoProxy = "https://goproxy.cn,https://proxy.golang.org,direct",
    [string]$GoSumDB = "sum.golang.org",
    [string]$Builder = ""
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDir "..\.."))
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("superflare-docker-" + [guid]::NewGuid().ToString("N"))
$imageRef = "${ImageName}:${Tag}"

$copyItems = @(
    "build",
    "cmd",
    "config",
    "embed",
    "internal",
    "tools\docker\Dockerfile",
    "tools\docker\docker-entrypoint.sh",
    "go.mod",
    "go.sum",
    "main.go",
    "doc.go",
    "README.md"
)

# HTTP(S)_PROXY 只影响 RUN 里的命令（apk add、go mod download）。buildkitd 拉取基础
# 镜像元数据那一步读取不到这些值，需要在 builder 上配置：
#   docker buildx create --driver-opt env.HTTPS_PROXY=http://host:port ...
# 大小写各转发一份：Go 工具链 / apk / curl 认的变量名不一致。BuildKit 把它们当作
# 预定义参数，不会写入镜像历史，也不会破坏层缓存。
function Get-ProxyValue {
    param([string]$Upper, [string]$Lower)
    $value = [Environment]::GetEnvironmentVariable($Upper)
    if ([string]::IsNullOrEmpty($value)) {
        $value = [Environment]::GetEnvironmentVariable($Lower)
    }
    return $value
}

function Get-MaskedProxy {
    param([string]$Value)
    return ($Value -replace '(://[^:/@]*:)[^@]*@', '$1***@')
}

$proxyArgs = New-Object System.Collections.Generic.List[string]
$proxyNotes = New-Object System.Collections.Generic.List[string]

foreach ($pair in @(
        @("HTTP_PROXY", "http_proxy"),
        @("HTTPS_PROXY", "https_proxy"),
        @("ALL_PROXY", "all_proxy"),
        @("NO_PROXY", "no_proxy"))) {
    $upper = $pair[0]
    $lower = $pair[1]
    $value = Get-ProxyValue -Upper $upper -Lower $lower

    if ([string]::IsNullOrEmpty($value)) {
        continue
    }

    if ($value -match '\s') {
        throw "$upper contains whitespace and cannot be forwarded safely: $(Get-MaskedProxy $value)"
    }

    $proxyArgs.Add("--build-arg")
    $proxyArgs.Add("$upper=$value")
    $proxyArgs.Add("--build-arg")
    $proxyArgs.Add("$lower=$value")
    $proxyNotes.Add("  $upper=$(Get-MaskedProxy $value)")
}

try {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw "docker executable was not found in PATH."
    }

    New-Item -ItemType Directory -Path $tempRoot | Out-Null

    foreach ($item in $copyItems) {
        $src = Join-Path $repoRoot $item
        if (-not (Test-Path $src)) {
            throw "missing required path: $src"
        }

        $dest = Join-Path $tempRoot $item
        $parent = Split-Path -Parent $dest
        if ($parent) {
            New-Item -ItemType Directory -Force -Path $parent | Out-Null
        }
        Copy-Item -Path $src -Destination $dest -Recurse -Force
    }

    $dockerfile = Join-Path $tempRoot "tools\docker\Dockerfile"

    if ($Push) {
        $args = @(
            "buildx", "build",
            "--file", $dockerfile,
            "--tag", $imageRef,
            "--platform", $(if ($Platform -ne "") { $Platform } else { "linux/amd64" }),
            "--build-arg", "GOPROXY=$GoProxy",
            "--build-arg", "GOSUMDB=$GoSumDB",
            "--push"
        )
        if ($Builder -ne "") {
            $args += @("--builder", $Builder)
        }
    }
    else {
        $args = @(
            "build",
            "--file", $dockerfile,
            "--tag", $imageRef,
            "--build-arg", "GOPROXY=$GoProxy",
            "--build-arg", "GOSUMDB=$GoSumDB"
        )

        if ($Platform -ne "") {
            $args += @("--platform", $Platform)
        }

        if ($Builder -ne "") {
            Write-Warning "Builder=$Builder ignored, it only applies to -Push builds."
        }
    }

    if ($proxyArgs.Count -gt 0) {
        $args += $proxyArgs
    }

    if ($NoCache) {
        $args += "--no-cache"
    }

    $args += $tempRoot

    if ($Push) {
        Write-Host "Building and pushing Docker image $imageRef"
    }
    else {
        Write-Host "Building Docker image $imageRef"
    }
    Write-Host "GOPROXY: $GoProxy"
    if ($Platform -ne "") {
        Write-Host "Platform: $Platform"
    }

    if ($proxyArgs.Count -gt 0) {
        Write-Host "Build args (proxy):"
        $proxyNotes | ForEach-Object { Write-Host $_ }

        if ($Push) {
            $builderName = if ($Builder -ne "") { $Builder } else { "<builder>" }
            Write-Warning ("if loading base image metadata still times out, configure the proxy on the builder itself:" +
                "`n  docker buildx rm $builderName" +
                "`n  docker buildx create --name $builderName --driver-opt env.HTTPS_PROXY=<proxy> --driver-opt env.HTTP_PROXY=<proxy> --use --bootstrap")
        }
    }

    & docker @args
    if ($LASTEXITCODE -ne 0) {
        throw "docker build failed with exit code $LASTEXITCODE."
    }
}
finally {
    if (Test-Path $tempRoot) {
        Remove-Item -Path $tempRoot -Recurse -Force
    }
}
