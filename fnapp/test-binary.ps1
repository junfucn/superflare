function Build-TestAppBinary([string]$OutputDirectory) {
    $repo = Split-Path -Parent $PSScriptRoot
    $go = Join-Path $repo ".tools\go\bin\go.exe"
    if (-not (Test-Path -LiteralPath $go -PathType Leaf)) {
        $go = (Get-Command go -ErrorAction Stop).Source
    }
    $binary = Join-Path $OutputDirectory "superflare-test.exe"
    Push-Location $repo
    try {
        & $go build -o $binary .
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to build the lifecycle test binary."
        }
    }
    finally {
        Pop-Location
    }
    return $binary.Replace('\', '/')
}

function Write-TestAppShim([string]$Path, [string]$Binary) {
    $null = [System.IO.Directory]::CreateDirectory((Split-Path -Parent $Path))
    $script = @'
#!/bin/sh
case "${1:-}" in
    config-read|config-login) exec '__BINARY__' "$@" ;;
esac
while true; do sleep 60; done
'@
    $script = $script.Replace('__BINARY__', $Binary).Replace("`r`n", "`n") + "`n"
    [System.IO.File]::WriteAllText($Path, $script, [System.Text.UTF8Encoding]::new($false))
}
