param(
    [string]$ImageName = "superflare",
    [string]$Tag = "latest",
    [string]$Platform = "",
    [switch]$NoCache,
    [switch]$Push,
    [string]$GoProxy = "https://goproxy.cn,https://proxy.golang.org,direct",
    [string]$GoSumDB = "sum.golang.org"
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
