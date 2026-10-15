[CmdletBinding()]
param(
    [string]$WorkspaceRoot = 'D:\Jobs\G2Tech\Mr.SB\easiiot_project',
    [string]$DataRoot,
    [string]$ExecutablePath,
    [ValidateNotNullOrEmpty()]
    [string]$ReviewerProviderId = 'provider_ca930ef2716e451f95b5478731e99b24',
    [ValidatePattern('^127\.0\.0\.1:[0-9]{1,5}$')]
    [string]$Listen = '127.0.0.1:18801',
    [switch]$Rebuild,
    [switch]$Open,
    [switch]$CheckOnly,
    [switch]$Foreground,
    [ValidateRange(5, 120)]
    [int]$StartupTimeoutSec = 30
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$resolvedWorkspace = (Resolve-Path -LiteralPath $WorkspaceRoot).Path
if (-not $DataRoot) { $DataRoot = Join-Path $repoRoot '.tmp-ui\easiiot-pilot-data-20261001' }
if (-not (Test-Path -LiteralPath $DataRoot -PathType Container)) {
    throw "Pilot data is missing: $DataRoot. Restore the preserved pilot data before launching."
}
$resolvedData = (Resolve-Path -LiteralPath $DataRoot).Path
$executable = if ($ExecutablePath) { [IO.Path]::GetFullPath($ExecutablePath) } else { Join-Path $repoRoot '.tmp-ui\hermetrix-easiiot-pilot.exe' }
$url = "http://$Listen"
$port = [int]($Listen.Split(':')[1])
if ($port -lt 1 -or $port -gt 65535) { throw 'Listen port must be between 1 and 65535.' }

function Get-HarnessHealth {
    try {
        $health = Invoke-RestMethod -Uri "$url/api/health" -TimeoutSec 1
        if ($health.ok -eq $true -and $health.service -eq 'hermetrix-harness') { return $health }
    } catch { }
    return $null
}

function Assert-HarnessIdentity {
    $connection = Get-NetTCPConnection -LocalAddress '127.0.0.1' -LocalPort $port -State Listen -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if (-not $connection) { throw "Harness listener disappeared at $Listen. Run the launcher again." }
    $owner = Get-CimInstance Win32_Process -Filter "ProcessId = $($connection.OwningProcess)"
    $dataPattern = '(?:^|\s)--data\s+(?:"' + [regex]::Escape($resolvedData) + '"|' +
        [regex]::Escape($resolvedData) + ')(?=\s|$)'
    $reviewerPattern = '(?:^|\s)--reviewer-provider-id\s+(?:"' + [regex]::Escape($ReviewerProviderId) + '"|' +
        [regex]::Escape($ReviewerProviderId) + ')(?=\s|$)'
    if ($owner.ExecutablePath -ne $executable -or $owner.CommandLine -notmatch $dataPattern -or
        $owner.CommandLine -notmatch $reviewerPattern) {
        throw "Port $Listen belongs to another process, data directory or reviewer selection. It was left untouched."
    }
    $projects = Invoke-RestMethod -Uri "$url/api/projects" -TimeoutSec 3
    if (-not ($projects | Where-Object { $_.root_path -eq $resolvedWorkspace })) {
        throw "Port $Listen serves another harness workspace. It was left untouched."
    }
    $providers = Invoke-RestMethod -Uri "$url/api/providers" -TimeoutSec 3
    $reviewer = $providers | Where-Object { $_.id -eq $ReviewerProviderId -and $_.enabled -and $_.credential_ready }
    if (-not $reviewer) { throw 'The explicitly requested reviewer profile is disabled, missing or lacks credentials. No provider fallback was selected.' }
    return $reviewer
}

function Write-HarnessReady($Reviewer) {
    Write-Host "Harness ready: $url/"
    Write-Host "Workspace: $resolvedWorkspace"
    Write-Host "Background reviewer: $($Reviewer.name) ($ReviewerProviderId)"
    $endpoint = [uri]$Reviewer.base_url
    # A local model outage must not prevent opening the UI or existing work.
    # Do not contact remote gateways or select a different reviewer implicitly.
    if ($endpoint.IsLoopback) {
        $probe = New-Object System.Net.Sockets.TcpClient
        try {
            $connection = $probe.ConnectAsync($endpoint.Host, $endpoint.Port)
            if (-not $connection.Wait(1000) -or -not $probe.Connected) { throw 'Not reachable' }
        } catch {
            Write-Warning 'The local reviewer is offline. UI/history are available; background model reviews need this provider restored. Choose an available provider explicitly when starting a chat.'
        } finally { $probe.Dispose() }
    }
    if ($Open) {
        try { Start-Process $url } catch { Write-Warning "Harness is ready; open $url/ manually. Browser launch failed: $($_.Exception.Message)" }
    }
}

# Start-Process joins arguments on Windows. Quote values when needed, including
# paths containing spaces and trailing backslashes, using the Windows argv convention.
function ConvertTo-WindowsArgument([string]$Value) {
    if ($Value.Length -gt 0 -and $Value -notmatch '[\s"]') { return $Value }
    '"' + [regex]::Replace([regex]::Replace($Value, '(\\*)"', '$1$1\"'), '(\\+)$', '$1$1') + '"'
}

$liveHealth = Get-HarnessHealth
if ($liveHealth) {
    $reviewer = Assert-HarnessIdentity
    if ($Rebuild) { throw 'Stop this running instance explicitly before rebuilding its executable.' }
    Write-HarnessReady $reviewer
    return
}
if ($CheckOnly) { throw "Harness is not running at $url" }
if (Get-NetTCPConnection -LocalAddress '127.0.0.1' -LocalPort $port -State Listen -ErrorAction SilentlyContinue) {
    throw "Port $Listen is occupied but does not report a healthy Hermetrix instance. It was left untouched."
}
if ($Rebuild -or -not (Test-Path -LiteralPath $executable -PathType Leaf)) {
    Push-Location -LiteralPath $repoRoot
    try {
        & go build -o $executable ./cmd/hermetrix
        if ($LASTEXITCODE -ne 0) { throw 'Harness build failed.' }
    } finally { Pop-Location }
}
$serveArgs = @('serve', '--data', $resolvedData, '--listen', $Listen, '--workspace', $resolvedWorkspace,
    '--reviewer-provider-id', $ReviewerProviderId)
if ($Foreground) {
    if ($Open) { $serveArgs += '--open' }
    Write-Host "Harness: $url (Ctrl+C stops this foreground process)"
    & $executable @serveArgs
    if ($LASTEXITCODE -ne 0) { throw "Harness exited with code $LASTEXITCODE" }
    return
}
$logPrefix = Join-Path $repoRoot ('.tmp-ui\easiiot-' + $port + '-' + [DateTime]::UtcNow.Ticks)
$arguments = ($serveArgs | ForEach-Object { ConvertTo-WindowsArgument $_ }) -join ' '
$launched = Start-Process -FilePath $executable -ArgumentList $arguments -WorkingDirectory $repoRoot `
    -WindowStyle Hidden -RedirectStandardOutput "$logPrefix.log" -RedirectStandardError "$logPrefix.error.log" -PassThru
# Cache the process handle while it exists so Windows PowerShell can read the
# exit code even if startup fails before the first health probe completes.
$null = $launched.Handle
Write-Host "Starting Hermetrix (PID $($launched.Id)); log: $logPrefix.error.log"
$deadline = [DateTime]::UtcNow.AddSeconds($StartupTimeoutSec)
try {
    while ([DateTime]::UtcNow -lt $deadline) {
        if ($launched.HasExited) {
            $launched.WaitForExit()
            throw "Harness exited with code $($launched.ExitCode)."
        }
        if (Get-HarnessHealth) {
            $reviewer = Assert-HarnessIdentity
            Write-HarnessReady $reviewer
            return
        }
        Start-Sleep -Milliseconds 250
    }
    throw "Harness did not become ready within $StartupTimeoutSec seconds."
} catch {
    # Clean up only the process created by this invocation. Never stop a reused
    # instance, another listener or any model process.
    if (-not $launched.HasExited) { Stop-Process -Id $launched.Id -ErrorAction SilentlyContinue }
    throw "$($_.Exception.Message) Inspect $logPrefix.error.log."
}
