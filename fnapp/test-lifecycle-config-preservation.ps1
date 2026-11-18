Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "test-binary.ps1")

$protectedConfigFiles = @(".env", "config.yml", "apps.yml", "bookmarks.yml", "ports.yaml")
$tempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$tempRoot = [System.IO.Path]::GetFullPath((Join-Path $tempBase ("superflare-lifecycle-config-" + [System.Guid]::NewGuid().ToString("N"))))
$installCallback = Join-Path $PSScriptRoot "superflare\cmd\install_callback"
$upgradeCallback = Join-Path $PSScriptRoot "superflare\cmd\upgrade_callback"
$uninstallCallback = Join-Path $PSScriptRoot "superflare\cmd\uninstall_callback"
$bashPath = $null

function Resolve-GitBash {
    $bashCandidates = @(
        "C:\Program Files\Git\bin\bash.exe",
        "C:\Program Files\Git\usr\bin\bash.exe"
    )
    foreach ($candidate in $bashCandidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return $candidate
        }
    }

    $bashCommand = Get-Command bash -ErrorAction SilentlyContinue
    if ($bashCommand -and $bashCommand.Source -and $bashCommand.Source -notlike "*\system32\bash.exe") {
        return $bashCommand.Source
    }

    throw "Git Bash not found. Install Git for Windows or provide a bash-compatible shell to run fnapp script tests."
}

function To-ShPath([string]$Path) {
    return $Path.Replace('\', '/')
}

function Invoke-LifecycleBash([string[]]$BashArguments) {
    # Windows PowerShell 5.1 can turn redirected stderr into a terminating error.
    $ErrorActionPreference = "Continue"
    $PSNativeCommandUseErrorActionPreference = $false
    $output = @(& $bashPath @BashArguments 2>&1)
    return [pscustomobject]@{
        ExitCode = $LASTEXITCODE
        Output = $output -join "`n"
    }
}

function Write-Utf8NoBom([string]$Path, [string]$Content) {
    $parent = Split-Path -Parent $Path
    if ($parent) {
        $null = [System.IO.Directory]::CreateDirectory($parent)
    }
    $encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Content, $encoding)
}

function New-LifecycleScenario([string]$Name, [bool]$WithExistingConfig) {
    $root = Join-Path $tempRoot $Name
    $appRoot = Join-Path $root "app"
    $etcRoot = Join-Path $root "etc"
    $varRoot = Join-Path $root "var"
    $shareRoot = Join-Path $root "shared files"
    $null = [System.IO.Directory]::CreateDirectory($shareRoot)
    $defaultsRoot = Join-Path $appRoot "server\defaults"
    $defaultTitle = "default-title-$Name"
    $defaultApplicationSentinel = "default-application-$Name"
    $defaultBookmarkSentinel = "default-bookmark-$Name"
    $defaultPortSentinel = "default-port-$Name"

    $null = [System.IO.Directory]::CreateDirectory($defaultsRoot)
    $null = [System.IO.Directory]::CreateDirectory($etcRoot)
    $null = [System.IO.Directory]::CreateDirectory($varRoot)
    Write-TestAppShim (Join-Path $appRoot "server/superflare") $testBinary

    Write-Utf8NoBom (Join-Path $defaultsRoot "config.yml") @"
Title: '$defaultTitle'
Theme: blackboard
Locale: zh
LoginUser: 'default-user'
LoginPass: 'default-pass'
"@
    Write-Utf8NoBom (Join-Path $defaultsRoot "apps.yml") @"
- name: '$defaultApplicationSentinel'
  url: 'https://default.invalid/$Name'
"@
    Write-Utf8NoBom (Join-Path $defaultsRoot "bookmarks.yml") @"
- name: '$defaultBookmarkSentinel'
  url: 'https://bookmark.invalid/$Name'
"@
    Write-Utf8NoBom (Join-Path $defaultsRoot "ports.yaml") @"
${defaultPortSentinel}: 7000
"@

    if ($WithExistingConfig) {
        Write-Utf8NoBom (Join-Path $etcRoot ".env") @"
# existing-env-$Name
FLARE_PORT=9443
FLARE_DISABLE_LOGIN=false
FLARE_EDITOR=true
FLARE_GUIDE=true
FLARE_COOKIE_NAME=superflare
export FLARE_COOKIE_SECRET = "custom-secret"
FLARE_USER=existing-user-$Name
FLARE_PASS=existing-pass-$Name
"@
        Write-Utf8NoBom (Join-Path $etcRoot "config.yml") @"
# existing-config-$Name
Title: 'existing-title-$Name'
LoginUser: 'existing-user-$Name'
LoginPass: 'existing-pass-$Name'
"@
        Write-Utf8NoBom (Join-Path $etcRoot "apps.yml") @"
- name: 'existing-application-$Name'
  url: 'https://existing-app.invalid/$Name'
"@
        Write-Utf8NoBom (Join-Path $etcRoot "bookmarks.yml") @"
- name: 'existing-bookmark-$Name'
  url: 'https://existing-bookmark.invalid/$Name'
"@
        Write-Utf8NoBom (Join-Path $etcRoot "ports.yaml") @"
existing-port-${Name}: 9443
"@
    }

    return [pscustomobject]@{
        Root = $root
        AppRoot = $appRoot
        EtcRoot = $etcRoot
        VarRoot = $varRoot
        ShareRoot = $shareRoot
        SharedEtcRoot = Join-Path $shareRoot "data/etc"
        SharedVarRoot = Join-Path $shareRoot "data/var"
        DefaultTitle = $defaultTitle
        DefaultApplicationSentinel = $defaultApplicationSentinel
        DefaultBookmarkSentinel = $defaultBookmarkSentinel
        DefaultPortSentinel = $defaultPortSentinel
    }
}

function Invoke-LifecycleCallback([string]$ScriptPath, [string]$ScenarioRoot, [hashtable]$WizardValues, [bool]$ExpectFailure = $false) {
    $appRoot = Join-Path $ScenarioRoot "app"
    $etcRoot = Join-Path $ScenarioRoot "etc"
    $varRoot = Join-Path $ScenarioRoot "var"
    $logPath = Join-Path $ScenarioRoot "lifecycle.log"
    $managedNames = @(
        "TRIM_APPDEST",
        "TRIM_PKGETC",
        "TRIM_PKGVAR",
        "TRIM_DATA_SHARE_PATHS",
        "TRIM_SERVICE_PORT",
        "TRIM_TEMP_LOGFILE",
        "wizard_install_login_user",
        "wizard_install_login_pass",
        "wizard_install_login_pass_confirm"
    )
    $previousValues = @{}

    foreach ($name in $managedNames) {
        $previousValues[$name] = [System.Environment]::GetEnvironmentVariable($name, "Process")
        [System.Environment]::SetEnvironmentVariable($name, $null, "Process")
    }

    try {
        [System.Environment]::SetEnvironmentVariable("TRIM_APPDEST", (To-ShPath $appRoot), "Process")
        [System.Environment]::SetEnvironmentVariable("TRIM_PKGETC", (To-ShPath $etcRoot), "Process")
        [System.Environment]::SetEnvironmentVariable("TRIM_PKGVAR", (To-ShPath $varRoot), "Process")
        [System.Environment]::SetEnvironmentVariable("TRIM_DATA_SHARE_PATHS", (To-ShPath (Join-Path $ScenarioRoot "shared files")), "Process")
        [System.Environment]::SetEnvironmentVariable("TRIM_SERVICE_PORT", "3636", "Process")
        [System.Environment]::SetEnvironmentVariable("TRIM_TEMP_LOGFILE", (To-ShPath $logPath), "Process")

        foreach ($entry in $WizardValues.GetEnumerator()) {
            if ($managedNames -notcontains [string]$entry.Key) {
                throw "Unsupported lifecycle wizard variable: $($entry.Key)"
            }
            [System.Environment]::SetEnvironmentVariable([string]$entry.Key, [string]$entry.Value, "Process")
        }

        $scriptSh = To-ShPath ([System.IO.Path]::GetFullPath($ScriptPath))
        $result = Invoke-LifecycleBash @($scriptSh)
        if ($ExpectFailure) {
            if ($result.ExitCode -eq 0) {
                throw "Lifecycle callback '$ScriptPath' unexpectedly succeeded."
            }
            return
        }
        if ($result.ExitCode -ne 0) {
            throw "Lifecycle callback '$ScriptPath' failed with code $($result.ExitCode)`n$($result.Output)"
        }
    }
    finally {
        foreach ($name in $managedNames) {
            [System.Environment]::SetEnvironmentVariable($name, $previousValues[$name], "Process")
        }
    }
}

function Get-ConfigHashes([string]$EtcRoot) {
    $hashes = @{}
    foreach ($name in $protectedConfigFiles) {
        $path = Join-Path $EtcRoot $name
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Expected configuration file is missing: $path"
        }
        $hashes[$name] = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash
    }
    return $hashes
}

function Assert-HashesEqual([hashtable]$Expected, [hashtable]$Actual, [string]$Context) {
    $differences = @()
    foreach ($name in $protectedConfigFiles) {
        if (-not $Expected.ContainsKey($name)) {
            $differences += "expected hash missing for '$name'"
            continue
        }
        if (-not $Actual.ContainsKey($name)) {
            $differences += "actual hash missing for '$name'"
            continue
        }
        if ($Expected[$name] -ne $Actual[$name]) {
            $differences += "'$name' (expected $($Expected[$name]), actual $($Actual[$name]))"
        }
    }
    if ($differences.Count -gt 0) {
        throw "$Context changed persisted configuration: $($differences -join '; ')."
    }
}

function Find-PreservedLinkPath([string]$EtcRoot) {
    $basePath = Join-Path $EtcRoot "var.pre-superflare-link"
    for ($index = 0; $index -le 1000; $index++) {
        $candidate = $basePath
        if ($index -gt 0) {
            $candidate = "$basePath.$index"
        }
        if (Test-Path -LiteralPath (Join-Path $candidate "operator-var-marker.txt") -PathType Leaf) {
            return $candidate
        }
    }
    return $null
}

function ConvertFrom-SimpleQuotedValue([string]$Value) {
    if ($Value.Length -ge 2 -and $Value[0] -eq "'" -and $Value[$Value.Length - 1] -eq "'") {
        return $Value.Substring(1, $Value.Length - 2).Replace("''", "'")
    }
    if ($Value.Length -ge 2 -and $Value[0] -eq '"' -and $Value[$Value.Length - 1] -eq '"') {
        return $Value.Substring(1, $Value.Length - 2)
    }
    return $Value
}

function Get-UniqueEnvValue([string]$Path, [string]$Key, [string]$Context) {
    $content = Get-Content -Raw -LiteralPath $Path
    $keyPattern = [System.Text.RegularExpressions.Regex]::Escape($Key)
    $pattern = "(?m)^[\t ]*(?:export[\t ]+)?${keyPattern}[\t ]*=[\t ]*(?<value>[^\r\n]*?)[\t ]*\r?$"
    $matches = [System.Text.RegularExpressions.Regex]::Matches($content, $pattern)
    if ($matches.Count -ne 1) {
        throw "$Context expected exactly one active '$Key' assignment, found $($matches.Count). Actual content:`n$content"
    }
    $value = $matches[0].Groups["value"].Value
    return (ConvertFrom-SimpleQuotedValue $value)
}

function Assert-EnvValue([string]$Path, [string]$Key, [string]$Expected, [string]$Context) {
    $actual = Get-UniqueEnvValue $Path $Key $Context
    if ($actual -cne $Expected) {
        throw "$Context expected '$Key=$Expected', found '$Key=$actual'."
    }
}

function Assert-EnvValueNonEmpty([string]$Path, [string]$Key, [string]$Context) {
    $actual = Get-UniqueEnvValue $Path $Key $Context
    if ([string]::IsNullOrWhiteSpace($actual)) {
        throw "$Context expected '$Key' to have a non-empty value."
    }
}

function Get-UniqueYamlValue([string]$Path, [string]$Key, [string]$Context) {
    $content = Get-Content -Raw -LiteralPath $Path
    $keyPattern = [System.Text.RegularExpressions.Regex]::Escape($Key)
    $pattern = "(?m)^[\t ]*${keyPattern}[\t ]*:[\t ]*(?<value>[^\r\n]*?)[\t ]*\r?$"
    $matches = [System.Text.RegularExpressions.Regex]::Matches($content, $pattern)
    if ($matches.Count -ne 1) {
        throw "$Context expected exactly one active '$Key' mapping, found $($matches.Count). Actual content:`n$content"
    }

    $value = $matches[0].Groups["value"].Value
    return (ConvertFrom-SimpleQuotedValue $value)
}

function Assert-YamlValue([string]$Path, [string]$Key, [string]$Expected, [string]$Context) {
    $actual = Get-UniqueYamlValue $Path $Key $Context
    if ($actual -cne $Expected) {
        throw "$Context expected '$Key' to equal '$Expected', found '$actual'."
    }
}

function Assert-FileHashEqual([string]$Path, [string]$ExpectedHash, [string]$Context) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Context is missing: $Path"
    }
    $actualHash = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash
    if ($actualHash -ne $ExpectedHash) {
        throw "$Context changed: expected $ExpectedHash, actual $actualHash."
    }
}

$tempPrefix = $tempBase.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
$tempLeaf = [System.IO.Path]::GetFileName($tempRoot)
$cleanupRootVerified = $tempRoot.StartsWith($tempPrefix, [System.StringComparison]::OrdinalIgnoreCase) -and $tempLeaf.StartsWith("superflare-lifecycle-config-", [System.StringComparison]::Ordinal)
if (-not $cleanupRootVerified) {
    throw "Refusing to use unverified lifecycle test root: $tempRoot"
}

try {
    $null = [System.IO.Directory]::CreateDirectory($tempRoot)
    $bashPath = Resolve-GitBash
    $testBinary = Build-TestAppBinary $tempRoot
    $resource = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot "superflare/config/resource") | ConvertFrom-Json
    if ($resource.'data-share'.shares.Count -ne 1 -or $resource.'data-share'.shares[0].name -ne "superflare") {
        throw "The package must register exactly one superflare application shared folder."
    }

    $installHarnessRoot = Join-Path $tempRoot "install-write-failure-harness"
    $installHarnessCallback = Join-Path $installHarnessRoot "install_callback"
    $installHarnessCommon = Join-Path $installHarnessRoot "common.sh"
    $installHarnessLog = Join-Path $installHarnessRoot "lifecycle.log"
    $null = [System.IO.Directory]::CreateDirectory($installHarnessRoot)
    Copy-Item -LiteralPath $installCallback -Destination $installHarnessCallback
    Write-Utf8NoBom $installHarnessCommon @'
has_existing_runtime_config() { return 1; }
trim_value() { printf '%s' "$1"; }
ensure_runtime_layout() { return 0; }
sync_login_config() { return 1; }
log_info() { :; }
log_lifecycle() { printf '%s\n' "$*" >> "${HARNESS_LOG}"; }
'@
    $installHarnessCallbackSh = To-ShPath $installHarnessCallback
    $installHarnessLogSh = To-ShPath $installHarnessLog
    $chmodResult = Invoke-LifecycleBash @("-c", "chmod +x '$installHarnessCallbackSh'")
    if ($chmodResult.ExitCode -ne 0) {
        throw "Failed to make the install test harness executable.`n$($chmodResult.Output)"
    }
    $installHarnessCommand = "HARNESS_LOG='$installHarnessLogSh' wizard_install_login_user='write-failure-user' wizard_install_login_pass='write-failure-pass' wizard_install_login_pass_confirm='write-failure-pass' '$installHarnessCallbackSh'"
    $installHarnessResult = Invoke-LifecycleBash @("-c", $installHarnessCommand)
    if ($installHarnessResult.ExitCode -eq 0) {
        throw "install_callback succeeded when credential synchronization failed.`n$($installHarnessResult.Output)"
    }
    if (-not (Test-Path -LiteralPath $installHarnessLog -PathType Leaf) -or
        (Get-Content -Raw -LiteralPath $installHarnessLog) -notmatch [regex]::Escape("Failed to apply install login settings.")) {
        throw "install_callback did not log the credential synchronization failure."
    }

    $existingInstall = New-LifecycleScenario "existing-install" $true
    $existingUpgrade = New-LifecycleScenario "existing-upgrade" $true
    $freshInstall = New-LifecycleScenario "fresh-install" $false

    $before = Get-ConfigHashes $existingInstall.EtcRoot
    Invoke-LifecycleCallback $installCallback $existingInstall.Root @{
        wizard_install_login_user = "replacement-user"
        wizard_install_login_pass = "replacement-pass"
        wizard_install_login_pass_confirm = "replacement-pass"
    }
    Assert-HashesEqual $before (Get-ConfigHashes $existingInstall.SharedEtcRoot) "repeated install"
    Assert-HashesEqual $before (Get-ConfigHashes $existingInstall.EtcRoot) "install legacy source preservation"

    $before = Get-ConfigHashes $existingUpgrade.EtcRoot
    $legacyPayload = @{
        "uploads/background/image.jpg" = "uploaded-background"
        "cache/site-icons/example.svg" = "cached-icon"
        "run/previous.log" = "previous-log"
        "backups/custom.zip" = "user-backup"
        ".hidden-state" = "hidden-state"
    }
    foreach ($entry in $legacyPayload.GetEnumerator()) {
        Write-Utf8NoBom (Join-Path $existingUpgrade.VarRoot $entry.Key) $entry.Value
    }
    Invoke-LifecycleCallback $upgradeCallback $existingUpgrade.Root @{}
    Assert-HashesEqual $before (Get-ConfigHashes $existingUpgrade.SharedEtcRoot) "upgrade"
    Assert-HashesEqual $before (Get-ConfigHashes $existingUpgrade.EtcRoot) "upgrade legacy source preservation"
    foreach ($entry in $legacyPayload.GetEnumerator()) {
        $source = Join-Path $existingUpgrade.VarRoot $entry.Key
        Assert-FileHashEqual (Join-Path $existingUpgrade.SharedVarRoot $entry.Key) (Get-FileHash -LiteralPath $source).Hash "upgrade all runtime files"
    }

    $sharedBookmark = Join-Path $existingUpgrade.SharedEtcRoot "bookmarks.yml"
    Write-Utf8NoBom $sharedBookmark "# updated after shared migration`n"
    $sharedBookmarkHash = (Get-FileHash -LiteralPath $sharedBookmark).Hash
    Invoke-LifecycleCallback $upgradeCallback $existingUpgrade.Root @{}
    Invoke-LifecycleCallback $installCallback $existingUpgrade.Root @{
        wizard_install_login_user = "replacement-user"
        wizard_install_login_pass = "replacement-pass"
        wizard_install_login_pass_confirm = "replacement-pass"
    }
    Assert-FileHashEqual $sharedBookmark $sharedBookmarkHash "replayed migration must not overwrite shared data"
    Assert-EnvValue (Join-Path $existingUpgrade.SharedEtcRoot ".env") "FLARE_USER" "existing-user-existing-upgrade" "replayed install shared credentials"

    $failedCopy = New-LifecycleScenario "failed-shared-copy" $true
    $failedCopyHashes = Get-ConfigHashes $failedCopy.EtcRoot
    $copyHarnessRoot = Join-Path $tempRoot "copy-failure-harness"
    $null = [System.IO.Directory]::CreateDirectory($copyHarnessRoot)
    $copyHarnessCallback = Join-Path $copyHarnessRoot "upgrade_callback"
    Copy-Item -LiteralPath $upgradeCallback -Destination $copyHarnessCallback
    $commonSh = To-ShPath (Join-Path $PSScriptRoot "superflare/cmd/common.sh")
    Write-Utf8NoBom (Join-Path $copyHarnessRoot "common.sh") @"
. '$commonSh'
cp() { return 73; }
"@
    Invoke-LifecycleCallback $copyHarnessCallback $failedCopy.Root @{} $true
    Assert-HashesEqual $failedCopyHashes (Get-ConfigHashes $failedCopy.EtcRoot) "failed migration source preservation"
    if (Test-Path -LiteralPath (Join-Path $failedCopy.ShareRoot "data")) {
        throw "A failed copy published incomplete shared storage."
    }
    Invoke-LifecycleCallback $upgradeCallback $failedCopy.Root @{}
    Assert-HashesEqual $failedCopyHashes (Get-ConfigHashes $failedCopy.SharedEtcRoot) "migration retry after failed copy"

    $occupiedShare = New-LifecycleScenario "occupied-shared-storage" $true
    $occupiedMarker = Join-Path $occupiedShare.SharedVarRoot "restored.txt"
    Write-Utf8NoBom $occupiedMarker "restored application files`n"
    $occupiedHash = (Get-FileHash -LiteralPath $occupiedMarker).Hash
    Invoke-LifecycleCallback $upgradeCallback $occupiedShare.Root @{} $true
    Assert-FileHashEqual $occupiedMarker $occupiedHash "unmarked shared data preservation"
    if (Test-Path -LiteralPath (Join-Path $occupiedShare.ShareRoot "data/.shared-storage")) {
        throw "An occupied shared folder was incorrectly marked as migrated."
    }

    $missingSharedData = New-LifecycleScenario "missing-shared-data" $true
    Write-Utf8NoBom (Join-Path $missingSharedData.ShareRoot "data/.shared-storage") "superflare-shared-storage-v1`n"
    Invoke-LifecycleCallback $upgradeCallback $missingSharedData.Root @{} $true
    if (Test-Path -LiteralPath (Join-Path $missingSharedData.SharedEtcRoot "config.yml")) {
        throw "Missing shared storage was silently replaced with legacy or default configuration."
    }

    Invoke-LifecycleCallback $installCallback $freshInstall.Root @{
        wizard_install_login_user = "fresh-user"
        wizard_install_login_pass = "fresh-pass"
        wizard_install_login_pass_confirm = "fresh-pass"
    }
    $null = Get-ConfigHashes $freshInstall.SharedEtcRoot
    $freshEnv = Join-Path $freshInstall.SharedEtcRoot ".env"
    $freshConfig = Join-Path $freshInstall.SharedEtcRoot "config.yml"
    Assert-EnvValue $freshEnv "FLARE_PORT" "3636" "fresh install service port"
    Assert-EnvValue $freshEnv "FLARE_DISABLE_LOGIN" "false" "fresh install login flag"
    Assert-EnvValue $freshEnv "FLARE_EDITOR" "true" "fresh install editor flag"
    Assert-EnvValue $freshEnv "FLARE_GUIDE" "true" "fresh install guide flag"
    Assert-EnvValue $freshEnv "FLARE_COOKIE_NAME" "superflare" "fresh install cookie name"
    Assert-EnvValueNonEmpty $freshEnv "FLARE_COOKIE_SECRET" "fresh install cookie secret"
    Assert-EnvValue $freshEnv "FLARE_USER" "fresh-user" "fresh install .env username"
    Assert-EnvValue $freshEnv "FLARE_PASS" "fresh-pass" "fresh install .env password"
    Assert-YamlValue $freshConfig "Title" $freshInstall.DefaultTitle "fresh install config title"
    Assert-YamlValue $freshConfig "LoginUser" "fresh-user" "fresh install config username"
    Assert-YamlValue $freshConfig "LoginPass" "fresh-pass" "fresh install config password"
    Assert-YamlValue (Join-Path $freshInstall.SharedEtcRoot "apps.yml") "- name" $freshInstall.DefaultApplicationSentinel "fresh install application defaults"
    Assert-YamlValue (Join-Path $freshInstall.SharedEtcRoot "bookmarks.yml") "- name" $freshInstall.DefaultBookmarkSentinel "fresh install bookmark defaults"
    Assert-YamlValue (Join-Path $freshInstall.SharedEtcRoot "ports.yaml") $freshInstall.DefaultPortSentinel "7000" "fresh install port defaults"
    foreach ($directory in @("run", "home", "tmp", "cache/backgrounds", "cache/site-icons", "uploads")) {
        if (-not (Test-Path -LiteralPath (Join-Path $freshInstall.SharedVarRoot $directory) -PathType Container)) {
            throw "Shared runtime directory missing: $directory"
        }
    }
    if (@(Get-ChildItem -LiteralPath $freshInstall.EtcRoot -Force).Count -gt 0 -or
        @(Get-ChildItem -LiteralPath $freshInstall.VarRoot -Force).Count -gt 0) {
        throw "A fresh install wrote application data into legacy package directories."
    }

    $danglingEnv = New-LifecycleScenario "dangling-env-install" $false
    Invoke-LifecycleCallback (Join-Path $PSScriptRoot "superflare/cmd/config_init") $danglingEnv.Root @{}
    $danglingEnvPath = Join-Path $danglingEnv.SharedEtcRoot ".env"
    $danglingEnvTarget = Join-Path $danglingEnv.Root "dangling-env-target"
    $danglingEnvLog = Join-Path $danglingEnv.Root "lifecycle.log"
    $null = [System.IO.Directory]::CreateDirectory($danglingEnvTarget)
    $danglingJunction = New-Item -ItemType Junction -Path $danglingEnvPath -Target $danglingEnvTarget
    if ($danglingJunction.LinkType -ne "Junction") {
        throw "Expected dangling .env test path to begin as a Junction, found '$($danglingJunction.LinkType)'."
    }
    Remove-Item -LiteralPath $danglingEnvTarget
    Invoke-LifecycleCallback $installCallback $danglingEnv.Root @{
        wizard_install_login_user = "should-not-write"
        wizard_install_login_pass = "should-not-write"
        wizard_install_login_pass_confirm = "should-not-write"
    } $true
    if (Test-Path -LiteralPath $danglingEnvTarget) {
        throw "Install recreated the target of a dangling .env Junction."
    }
    $danglingEnvItem = Get-Item -LiteralPath $danglingEnvPath -Force
    if ($danglingEnvItem.LinkType -ne "Junction") {
        throw "Install did not preserve the dangling .env Junction."
    }
    if (-not (Test-Path -LiteralPath $danglingEnvLog -PathType Leaf) -or
        (Get-Content -Raw -LiteralPath $danglingEnvLog) -notmatch [regex]::Escape("Refusing to initialize a dangling .env symbolic link:")) {
        throw "Install did not log rejection of the dangling .env Junction."
    }

    $partialInstall = New-LifecycleScenario "partial-install" $false
    Write-Utf8NoBom (Join-Path $partialInstall.EtcRoot ".env") @"
# partial-env-sentinel
FLARE_PORT=9443
FLARE_COOKIE_SECRET=partial-secret
FLARE_USER=partial-custom-user
"@
    Write-Utf8NoBom (Join-Path $partialInstall.EtcRoot "config.yml") @"
# partial-config-sentinel
Title: 'partial-title'
LoginUser: 'partial-custom-user'
"@
    Invoke-LifecycleCallback $installCallback $partialInstall.Root @{
        wizard_install_login_user = "replacement-user"
        wizard_install_login_pass = "replacement-pass"
        wizard_install_login_pass_confirm = "replacement-pass"
    }
    $partialEnv = Join-Path $partialInstall.SharedEtcRoot ".env"
    $partialConfig = Join-Path $partialInstall.SharedEtcRoot "config.yml"
    Assert-EnvValue $partialEnv "FLARE_PORT" "9443" "partial install service port"
    Assert-EnvValue $partialEnv "FLARE_DISABLE_LOGIN" "false" "partial install login flag repair"
    Assert-EnvValue $partialEnv "FLARE_EDITOR" "true" "partial install editor flag repair"
    Assert-EnvValue $partialEnv "FLARE_GUIDE" "true" "partial install guide flag repair"
    Assert-EnvValue $partialEnv "FLARE_COOKIE_NAME" "superflare" "partial install cookie name repair"
    Assert-EnvValue $partialEnv "FLARE_COOKIE_SECRET" "partial-secret" "partial install cookie secret"
    Assert-EnvValue $partialEnv "FLARE_USER" "partial-custom-user" "partial install .env username"
    Assert-EnvValue $partialEnv "FLARE_PASS" "admin" "partial install .env password repair"
    Assert-YamlValue $partialConfig "Title" "partial-title" "partial install config title"
    Assert-YamlValue $partialConfig "LoginUser" "partial-custom-user" "partial install config username"
    Assert-YamlValue $partialConfig "LoginPass" "admin" "partial install config password repair"
    Assert-YamlValue (Join-Path $partialInstall.SharedEtcRoot "apps.yml") "- name" $partialInstall.DefaultApplicationSentinel "partial install application defaults"
    Assert-YamlValue (Join-Path $partialInstall.SharedEtcRoot "bookmarks.yml") "- name" $partialInstall.DefaultBookmarkSentinel "partial install bookmark defaults"
    Assert-YamlValue (Join-Path $partialInstall.SharedEtcRoot "ports.yaml") $partialInstall.DefaultPortSentinel "7000" "partial install port defaults"

    $partialUpgrade = New-LifecycleScenario "partial-upgrade" $false
    Write-Utf8NoBom (Join-Path $partialUpgrade.EtcRoot ".env") @"
# partial-upgrade-env-sentinel
FLARE_PORT=9443
FLARE_COOKIE_SECRET=partial-upgrade-secret
FLARE_USER=partial-upgrade-user
"@
    Write-Utf8NoBom (Join-Path $partialUpgrade.EtcRoot "config.yml") @"
# partial-upgrade-config-sentinel
Title: 'partial-upgrade-title'
LoginUser: 'partial-upgrade-user'
"@
    Invoke-LifecycleCallback $upgradeCallback $partialUpgrade.Root @{}
    $partialUpgradeEnv = Join-Path $partialUpgrade.SharedEtcRoot ".env"
    $partialUpgradeConfig = Join-Path $partialUpgrade.SharedEtcRoot "config.yml"
    Assert-EnvValue $partialUpgradeEnv "FLARE_PORT" "9443" "partial upgrade service port"
    Assert-EnvValue $partialUpgradeEnv "FLARE_DISABLE_LOGIN" "false" "partial upgrade login flag repair"
    Assert-EnvValue $partialUpgradeEnv "FLARE_EDITOR" "true" "partial upgrade editor flag repair"
    Assert-EnvValue $partialUpgradeEnv "FLARE_GUIDE" "true" "partial upgrade guide flag repair"
    Assert-EnvValue $partialUpgradeEnv "FLARE_COOKIE_NAME" "superflare" "partial upgrade cookie name repair"
    Assert-EnvValue $partialUpgradeEnv "FLARE_COOKIE_SECRET" "partial-upgrade-secret" "partial upgrade cookie secret"
    Assert-EnvValue $partialUpgradeEnv "FLARE_USER" "partial-upgrade-user" "partial upgrade .env username"
    Assert-EnvValue $partialUpgradeEnv "FLARE_PASS" "admin" "partial upgrade .env password repair"
    Assert-YamlValue $partialUpgradeConfig "Title" "partial-upgrade-title" "partial upgrade config title"
    Assert-YamlValue $partialUpgradeConfig "LoginUser" "partial-upgrade-user" "partial upgrade config username"
    Assert-YamlValue $partialUpgradeConfig "LoginPass" "admin" "partial upgrade config password repair"
    Assert-YamlValue (Join-Path $partialUpgrade.SharedEtcRoot "apps.yml") "- name" $partialUpgrade.DefaultApplicationSentinel "partial upgrade application defaults"
    Assert-YamlValue (Join-Path $partialUpgrade.SharedEtcRoot "bookmarks.yml") "- name" $partialUpgrade.DefaultBookmarkSentinel "partial upgrade bookmark defaults"
    Assert-YamlValue (Join-Path $partialUpgrade.SharedEtcRoot "ports.yaml") $partialUpgrade.DefaultPortSentinel "7000" "partial upgrade port defaults"

    $partialLegacy = New-LifecycleScenario "partial-legacy-install" $false
    $partialLegacyRoot = Join-Path $partialLegacy.VarRoot "runtime"
    $partialLegacyConfig = Join-Path $partialLegacyRoot "config.yml"
    $null = [System.IO.Directory]::CreateDirectory($partialLegacyRoot)
    Write-Utf8NoBom $partialLegacyConfig @"
Title: 'partial-legacy-title'
LoginUser: 'partial-legacy-user'
LoginPass: 'partial-legacy-pass'
"@
    $partialLegacyConfigHash = (Get-FileHash -LiteralPath $partialLegacyConfig -Algorithm SHA256).Hash
    Invoke-LifecycleCallback $installCallback $partialLegacy.Root @{
        wizard_install_login_user = "replacement-user"
        wizard_install_login_pass = "replacement-pass"
        wizard_install_login_pass_confirm = "replacement-pass"
    }
    $partialLegacyEnv = Join-Path $partialLegacy.SharedEtcRoot ".env"
    $migratedPartialLegacyConfig = Join-Path $partialLegacy.SharedEtcRoot "config.yml"
    $null = Get-ConfigHashes $partialLegacy.SharedEtcRoot
    Assert-FileHashEqual $migratedPartialLegacyConfig $partialLegacyConfigHash "partial legacy config migration"
    Assert-FileHashEqual $partialLegacyConfig $partialLegacyConfigHash "partial legacy source preservation"
    Assert-EnvValue $partialLegacyEnv "FLARE_PORT" "3636" "partial legacy install service port repair"
    Assert-EnvValue $partialLegacyEnv "FLARE_DISABLE_LOGIN" "false" "partial legacy install login flag repair"
    Assert-EnvValue $partialLegacyEnv "FLARE_EDITOR" "true" "partial legacy install editor flag repair"
    Assert-EnvValue $partialLegacyEnv "FLARE_GUIDE" "true" "partial legacy install guide flag repair"
    Assert-EnvValue $partialLegacyEnv "FLARE_COOKIE_NAME" "superflare" "partial legacy install cookie name repair"
    Assert-EnvValueNonEmpty $partialLegacyEnv "FLARE_COOKIE_SECRET" "partial legacy install cookie secret repair"
    Assert-EnvValue $partialLegacyEnv "FLARE_USER" "admin" "partial legacy install .env username repair"
    Assert-EnvValue $partialLegacyEnv "FLARE_PASS" "admin" "partial legacy install .env password repair"
    Assert-YamlValue $migratedPartialLegacyConfig "Title" "partial-legacy-title" "partial legacy config title"
    Assert-YamlValue $migratedPartialLegacyConfig "LoginUser" "partial-legacy-user" "partial legacy config username"
    Assert-YamlValue $migratedPartialLegacyConfig "LoginPass" "partial-legacy-pass" "partial legacy config password"
    Assert-YamlValue (Join-Path $partialLegacy.SharedEtcRoot "apps.yml") "- name" $partialLegacy.DefaultApplicationSentinel "partial legacy application defaults"
    Assert-YamlValue (Join-Path $partialLegacy.SharedEtcRoot "bookmarks.yml") "- name" $partialLegacy.DefaultBookmarkSentinel "partial legacy bookmark defaults"
    Assert-YamlValue (Join-Path $partialLegacy.SharedEtcRoot "ports.yaml") $partialLegacy.DefaultPortSentinel "7000" "partial legacy port defaults"

    $legacyOnly = New-LifecycleScenario "legacy-only" $false
    $legacyRoot = Join-Path $legacyOnly.VarRoot "runtime"
    $null = [System.IO.Directory]::CreateDirectory($legacyRoot)
    Write-Utf8NoBom (Join-Path $legacyRoot ".env") @"
# legacy-env-sentinel
FLARE_PORT=9443
FLARE_DISABLE_LOGIN=false
FLARE_EDITOR=true
FLARE_GUIDE=true
FLARE_COOKIE_NAME=superflare
FLARE_COOKIE_SECRET=legacy-secret
FLARE_USER=legacy-user
FLARE_PASS=legacy-pass
"@
    Write-Utf8NoBom (Join-Path $legacyRoot "config.yml") @"
# legacy-config-sentinel
LoginUser: 'legacy-user'
LoginPass: 'legacy-pass'
"@
    Write-Utf8NoBom (Join-Path $legacyRoot "apps.yml") "# legacy-apps-sentinel`n"
    Write-Utf8NoBom (Join-Path $legacyRoot "bookmarks.yml") "# legacy-bookmarks-sentinel`n"
    Write-Utf8NoBom (Join-Path $legacyRoot "ports.yaml") "# legacy-ports-sentinel`n"
    $legacyHashes = Get-ConfigHashes $legacyRoot
    Invoke-LifecycleCallback $installCallback $legacyOnly.Root @{
        wizard_install_login_user = "replacement-user"
        wizard_install_login_pass = "replacement-pass"
        wizard_install_login_pass_confirm = "replacement-pass"
    }
    Assert-HashesEqual $legacyHashes (Get-ConfigHashes $legacyOnly.SharedEtcRoot) "legacy-only install migration"
    Assert-HashesEqual $legacyHashes (Get-ConfigHashes $legacyRoot) "legacy runtime source preservation"
    Assert-EnvValue (Join-Path $legacyOnly.SharedEtcRoot ".env") "FLARE_USER" "legacy-user" "legacy-only install .env username"
    Assert-YamlValue (Join-Path $legacyOnly.SharedEtcRoot "config.yml") "LoginUser" "legacy-user" "legacy-only install config username"

    $junctionLegacy = New-LifecycleScenario "junction-legacy-install" $false
    # Git Bash cp emulates symlinks as copies without Windows symlink privileges.
    # Exercise the native Junction guard after preparing the shared layout.
    Invoke-LifecycleCallback (Join-Path $PSScriptRoot "superflare/cmd/config_init") $junctionLegacy.Root @{}
    $junctionLegacyRoot = Join-Path $junctionLegacy.SharedVarRoot "runtime"
    $junctionLegacyTarget = Join-Path $junctionLegacy.Root "operator-legacy-target"
    $junctionLegacyTargetConfig = Join-Path $junctionLegacyTarget "config.yml"
    $null = [System.IO.Directory]::CreateDirectory($junctionLegacyTarget)
    Write-Utf8NoBom $junctionLegacyTargetConfig @"
Title: 'operator-junction-title'
LoginUser: 'operator-junction-user'
LoginPass: 'operator-junction-pass'
"@
    $junctionLegacyTargetHash = (Get-FileHash -LiteralPath $junctionLegacyTargetConfig -Algorithm SHA256).Hash
    $junction = New-Item -ItemType Junction -Path $junctionLegacyRoot -Target $junctionLegacyTarget
    if ($junction.LinkType -ne "Junction") {
        throw "Expected legacy runtime test path to be a Junction, found '$($junction.LinkType)'."
    }
    Invoke-LifecycleCallback $installCallback $junctionLegacy.Root @{
        wizard_install_login_user = "junction-fresh-user"
        wizard_install_login_pass = "junction-fresh-pass"
        wizard_install_login_pass_confirm = "junction-fresh-pass"
    }
    $null = Get-ConfigHashes $junctionLegacy.SharedEtcRoot
    Assert-EnvValue (Join-Path $junctionLegacy.SharedEtcRoot ".env") "FLARE_USER" "junction-fresh-user" "junction-backed legacy install .env username"
    Assert-EnvValue (Join-Path $junctionLegacy.SharedEtcRoot ".env") "FLARE_PASS" "junction-fresh-pass" "junction-backed legacy install .env password"
    Assert-YamlValue (Join-Path $junctionLegacy.SharedEtcRoot "config.yml") "LoginUser" "junction-fresh-user" "junction-backed legacy install config username"
    Assert-YamlValue (Join-Path $junctionLegacy.SharedEtcRoot "config.yml") "LoginPass" "junction-fresh-pass" "junction-backed legacy install config password"
    Assert-FileHashEqual $junctionLegacyTargetConfig $junctionLegacyTargetHash "junction-backed legacy target config"
    if ((Get-Item -LiteralPath $junctionLegacyRoot).LinkType -ne "Junction") {
        throw "Install did not preserve the legacy runtime Junction."
    }
    $junctionTargetEntries = @(Get-ChildItem -LiteralPath $junctionLegacyTarget -Force)
    if ($junctionTargetEntries.Count -ne 1 -or $junctionTargetEntries[0].Name -ne "config.yml") {
        throw "Install changed the legacy Junction target contents: $($junctionTargetEntries.Name -join ', ')."
    }

    $legacyConflict = New-LifecycleScenario "legacy-conflict" $true
    $legacyConflictRoot = Join-Path $legacyConflict.VarRoot "runtime"
    $legacyConflictBookmark = Join-Path $legacyConflictRoot "bookmarks.yml"
    $currentConflictBookmark = Join-Path $legacyConflict.EtcRoot "bookmarks.yml"
    $null = [System.IO.Directory]::CreateDirectory($legacyConflictRoot)
    Write-Utf8NoBom $legacyConflictBookmark "# legacy-conflict-bookmark-sentinel`n"
    $legacyConflictHash = (Get-FileHash -LiteralPath $legacyConflictBookmark -Algorithm SHA256).Hash
    $currentConflictHash = (Get-FileHash -LiteralPath $currentConflictBookmark -Algorithm SHA256).Hash
    Invoke-LifecycleCallback $upgradeCallback $legacyConflict.Root @{}
    Assert-FileHashEqual (Join-Path $legacyConflict.SharedEtcRoot "bookmarks.yml") $currentConflictHash "shared current bookmark conflict copy"
    Assert-FileHashEqual (Join-Path $legacyConflict.SharedVarRoot "runtime/bookmarks.yml") $legacyConflictHash "shared legacy bookmark conflict copy"
    Assert-FileHashEqual $currentConflictBookmark $currentConflictHash "upgrade current bookmark conflict copy"
    Assert-FileHashEqual $legacyConflictBookmark $legacyConflictHash "upgrade legacy bookmark conflict copy"

    $realVar = New-LifecycleScenario "real-etc-var" $true
    $realVarPath = Join-Path $realVar.EtcRoot "var"
    $collisionPath = Join-Path $realVar.EtcRoot "var.pre-superflare-link"
    $realVarMarker = Join-Path $realVarPath "operator-var-marker.txt"
    $collisionMarker = Join-Path $collisionPath "collision-marker.txt"
    $runtimeMarker = Join-Path $realVar.VarRoot "managed-runtime-marker.txt"
    $null = [System.IO.Directory]::CreateDirectory($realVarPath)
    $null = [System.IO.Directory]::CreateDirectory($collisionPath)
    Write-Utf8NoBom $realVarMarker "operator-var-data-sentinel`n"
    Write-Utf8NoBom $collisionMarker "preexisting-backup-collision-sentinel`n"
    Write-Utf8NoBom $runtimeMarker "managed-runtime-data-sentinel`n"
    $realVarMarkerHash = (Get-FileHash -LiteralPath $realVarMarker -Algorithm SHA256).Hash
    $collisionMarkerHash = (Get-FileHash -LiteralPath $collisionMarker -Algorithm SHA256).Hash
    $runtimeMarkerHash = (Get-FileHash -LiteralPath $runtimeMarker -Algorithm SHA256).Hash
    Invoke-LifecycleCallback $upgradeCallback $realVar.Root @{}
    Assert-FileHashEqual $collisionMarker $collisionMarkerHash "upgrade existing var preservation collision"
    $preservedVarPath = Find-PreservedLinkPath $realVar.SharedEtcRoot
    if (-not $preservedVarPath) {
        throw "upgrade did not preserve the displaced real etc/var under a unique var.pre-superflare-link.N path."
    }
    $expectedPreservedVarPath = Join-Path $realVar.SharedEtcRoot "var.pre-superflare-link.1"
    if ($preservedVarPath -ne $expectedPreservedVarPath) {
        throw "upgrade preserved real etc/var at '$preservedVarPath'; expected first free collision suffix '$expectedPreservedVarPath'."
    }
    Assert-FileHashEqual (Join-Path $preservedVarPath "operator-var-marker.txt") $realVarMarkerHash "upgrade displaced real etc/var marker"
    if (-not (Test-Path -LiteralPath $realVarPath -PathType Container)) {
        throw "upgrade did not recreate etc/var as the managed runtime container after preserving operator data."
    }
    Assert-FileHashEqual (Join-Path $realVar.SharedEtcRoot "var/managed-runtime-marker.txt") $runtimeMarkerHash "upgrade managed runtime projection"
    if (Test-Path -LiteralPath (Join-Path $realVar.SharedEtcRoot "var/operator-var-marker.txt")) {
        throw "upgrade left displaced operator data under the managed etc/var runtime path."
    }

    $uninstall = New-LifecycleScenario "uninstall" $false
    $uninstallVarPath = Join-Path $uninstall.EtcRoot "var"
    $uninstallLegacyRoot = Join-Path $uninstall.VarRoot "runtime"
    $uninstallVarMarker = Join-Path $uninstallVarPath "operator-uninstall-marker.txt"
    $uninstallLegacyMarker = Join-Path $uninstallLegacyRoot "config.yml"
    $null = [System.IO.Directory]::CreateDirectory($uninstallVarPath)
    $null = [System.IO.Directory]::CreateDirectory($uninstallLegacyRoot)
    $uninstallSharedVarPath = Join-Path $uninstall.SharedEtcRoot "var"
    $null = [System.IO.Directory]::CreateDirectory($uninstallSharedVarPath)
    Write-Utf8NoBom (Join-Path $uninstallSharedVarPath "shared-uninstall-marker.txt") "shared-data`n"
    Write-Utf8NoBom $uninstallVarMarker "operator-uninstall-data-sentinel`n"
    Write-Utf8NoBom $uninstallLegacyMarker "# legacy-uninstall-config-sentinel`n"
    $uninstallVarHash = (Get-FileHash -LiteralPath $uninstallVarMarker -Algorithm SHA256).Hash
    $uninstallLegacyHash = (Get-FileHash -LiteralPath $uninstallLegacyMarker -Algorithm SHA256).Hash
    Invoke-LifecycleCallback $uninstallCallback $uninstall.Root @{}
    Assert-FileHashEqual $uninstallVarMarker $uninstallVarHash "uninstall real etc/var marker"
    Assert-FileHashEqual $uninstallLegacyMarker $uninstallLegacyHash "uninstall legacy runtime configuration"
    if (-not (Test-Path -LiteralPath (Join-Path $uninstallSharedVarPath "shared-uninstall-marker.txt"))) {
        throw "Uninstall removed shared application data."
    }

    $uninstallOperatorLink = New-LifecycleScenario "uninstall-operator-link" $false
    $uninstallOperatorLinkPath = Join-Path $uninstallOperatorLink.SharedEtcRoot "var"
    $uninstallOperatorTarget = Join-Path $uninstallOperatorLink.Root "operator-var-target"
    $uninstallOperatorMarker = Join-Path $uninstallOperatorTarget "operator-link-marker.txt"
    $uninstallOperatorLog = Join-Path $uninstallOperatorLink.Root "lifecycle.log"
    $null = [System.IO.Directory]::CreateDirectory($uninstallOperatorTarget)
    $null = [System.IO.Directory]::CreateDirectory($uninstallOperatorLink.SharedEtcRoot)
    Write-Utf8NoBom $uninstallOperatorMarker "operator-link-data-sentinel`n"
    $uninstallOperatorMarkerHash = (Get-FileHash -LiteralPath $uninstallOperatorMarker -Algorithm SHA256).Hash
    $operatorJunction = New-Item -ItemType Junction -Path $uninstallOperatorLinkPath -Target $uninstallOperatorTarget
    if ($operatorJunction.LinkType -ne "Junction") {
        throw "Expected uninstall operator path to be a Junction, found '$($operatorJunction.LinkType)'."
    }
    Invoke-LifecycleCallback $uninstallCallback $uninstallOperatorLink.Root @{}
    if (-not (Test-Path -LiteralPath $uninstallOperatorLinkPath -PathType Container) -or
        (Get-Item -LiteralPath $uninstallOperatorLinkPath).LinkType -ne "Junction") {
        throw "Uninstall removed the operator-owned etc/var Junction."
    }
    Assert-FileHashEqual $uninstallOperatorMarker $uninstallOperatorMarkerHash "uninstall operator-owned link target marker"
    if (-not (Test-Path -LiteralPath $uninstallOperatorLog -PathType Leaf) -or
        (Get-Content -Raw -LiteralPath $uninstallOperatorLog) -notmatch [regex]::Escape("Preserving unmanaged etc/var symbolic link during uninstall:")) {
        throw "Uninstall did not log preservation of the operator-owned etc/var Junction."
    }

    Write-Host "fnapp lifecycle configuration preservation verified."
}
finally {
    $cleanupCandidate = [System.IO.Path]::GetFullPath($tempRoot)
    if ($cleanupRootVerified -and $cleanupCandidate -eq $tempRoot -and (Test-Path -LiteralPath $cleanupCandidate)) {
        Remove-Item -LiteralPath $cleanupCandidate -Recurse -Force
    }
}
