Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "test-binary.ps1")

$repoRoot = Split-Path -Parent $PSScriptRoot
$scriptPath = Join-Path $PSScriptRoot "superflare\cmd\config_init"

$tmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("superflare-config-init-" + [System.Guid]::NewGuid().ToString("N"))
$null = New-Item -ItemType Directory -Path $tmpRoot -Force

try {
    $testBinary = Build-TestAppBinary $tmpRoot
    $testAppRoot = Join-Path $tmpRoot "app"
    Write-TestAppShim (Join-Path $testAppRoot "server/superflare") $testBinary
    $testAppSh = $testAppRoot.Replace('\', '/')
    $shareRoot = Join-Path $tmpRoot "shared files"
    $null = [System.IO.Directory]::CreateDirectory($shareRoot)
    $shareSh = $shareRoot.Replace('\', '/')
    $varSh = (Join-Path $tmpRoot "var").Replace('\', '/')
    $configFile = Join-Path $tmpRoot "config.yml"
    $envFile = Join-Path $tmpRoot ".env"

    @"
LoginUser: 'junfu'
LoginPass: 'admin'
"@ | Set-Content -Path $configFile -Encoding UTF8

    @"
FLARE_USER=admin
FLARE_PASS=123456
"@ | Set-Content -Path $envFile -Encoding UTF8

    $bashCandidates = @(
        "C:\Program Files\Git\bin\bash.exe",
        "C:\Program Files\Git\usr\bin\bash.exe"
    )

    $bashPath = $null
    foreach ($candidate in $bashCandidates) {
        if (Test-Path $candidate -PathType Leaf) {
            $bashPath = $candidate
            break
        }
    }
    if (-not $bashPath) {
        $bashCmd = Get-Command bash -ErrorAction SilentlyContinue
        if ($bashCmd -and $bashCmd.Source -and $bashCmd.Source -notlike "*\system32\bash.exe") {
            $bashPath = $bashCmd.Source
        }
    }
    if (-not $bashPath) {
        throw "Git Bash not found. Install Git for Windows or provide a bash-compatible shell to run fnapp script tests."
    }

    $missingShareSh = (Join-Path $tmpRoot "missing share").Replace('\', '/')
    $output = & $bashPath -c "TRIM_DATA_SHARE_PATHS='$missingShareSh' '$($scriptPath.Replace('\', '/'))'" 2>&1
    if ($LASTEXITCODE -eq 0 -or (Test-Path -LiteralPath $missingShareSh)) {
        throw "config_init did not reject an unavailable shared folder.`n$output"
    }

    # Keep legacy configuration separate from the share and test binaries.
    $legacyEtc = Join-Path $tmpRoot "etc"
    $null = [System.IO.Directory]::CreateDirectory($legacyEtc)
    Move-Item -LiteralPath $configFile -Destination $legacyEtc
    Move-Item -LiteralPath $envFile -Destination $legacyEtc
    $trimPkgEtc = $legacyEtc.Replace('\', '/')
    $command = "TRIM_APPDEST='$testAppSh' TRIM_PKGETC='$trimPkgEtc' TRIM_PKGVAR='$varSh' TRIM_DATA_SHARE_PATHS='$shareSh' '$($scriptPath.Replace('\', '/'))'"
    $output = & $bashPath -c $command 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "config_init exited with code $LASTEXITCODE`n$output"
    }

    $lines = @($output | ForEach-Object { $_.ToString().TrimEnd("`r","`n") })
    $expected = @(
        "wizard_login_enabled=true",
        "wizard_login_user=",
        "wizard_login_pass=",
        "wizard_login_pass_confirm="
    )
    if ($lines.Count -lt $expected.Count) {
        $actualText = $lines -join "`n"
        throw "Expected at least $($expected.Count) output lines, got:`n$actualText"
    }
    for ($i = 0; $i -lt $expected.Count; $i++) {
        if ($lines[$i] -ne $expected[$i]) {
            $actualText = $lines -join "`n"
            throw "Expected '$($expected[$i])', got '$($lines[$i])'. Full output:`n$actualText"
        }
    }

    $envFile = Join-Path $shareRoot "data/etc/.env"
    Remove-Item -LiteralPath $envFile -Force
    $null = New-Item -ItemType Directory -Path $envFile
    $output = & $bashPath -c $command 2>&1
    if ($LASTEXITCODE -eq 0) {
        throw "config_init succeeded when .env was an unreadable configuration path.`n$output"
    }

    Write-Host "config_init output verified."
    Write-Host ($lines -join "`n")
}
finally {
    if (Test-Path $tmpRoot) {
        Remove-Item -Path $tmpRoot -Recurse -Force
    }
}
