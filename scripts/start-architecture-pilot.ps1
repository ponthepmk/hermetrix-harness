[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ConfigPath,
    [switch]$CheckOnly,
    [switch]$Open
)

$ErrorActionPreference = 'Stop'
$pilotConfig = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
$pilotRepo = Split-Path -Parent $PSScriptRoot
$pilotExecutable = [IO.Path]::GetFullPath($pilotConfig.executable)
$pilotData = [IO.Path]::GetFullPath($pilotConfig.data_root)
$pilotWorkspace = (Resolve-Path -LiteralPath $pilotConfig.workspace_root).Path
$pilotListen = [string]$pilotConfig.listen
if ($pilotListen -notmatch '^127\.0\.0\.1:([0-9]{1,5})$') { throw 'The pilot must use an explicit IPv4 loopback address.' }
$pilotPort = [int]$Matches[1]
if ($pilotPort -lt 1 -or $pilotPort -gt 65535) { throw 'Invalid pilot port.' }
foreach ($pilotFile in @($pilotExecutable, (Join-Path $pilotData 'hermetrix.db'), $pilotConfig.whisper_cli, $pilotConfig.whisper_model)) {
    if (-not (Test-Path -LiteralPath $pilotFile -PathType Leaf)) { throw "Required pilot file is missing: $pilotFile" }
}
if (-not $pilotConfig.reviewer_provider_id -or -not $pilotConfig.brain_server_name -or -not $pilotConfig.brain_project) {
    throw 'The prepared pilot must pin its reviewer and Project Brain scope.'
}
$pilotVoiceURL = [uri]$pilotConfig.whisper_server
if ($pilotVoiceURL.Scheme -ne 'http' -or $pilotVoiceURL.Host -ne '127.0.0.1' -or $pilotVoiceURL.AbsolutePath -ne '/') {
    throw 'Whisper must use an explicit loopback HTTP address.'
}
$pilotURL = "http://$pilotListen"
# Bind ownership remains observable even when the Windows provider reports State=0.
# Process/data/reviewer identity and HTTP health are still checked before reuse.
$pilotConnection = Get-NetTCPConnection -LocalPort $pilotPort -ErrorAction SilentlyContinue |
    Where-Object { $_.RemotePort -eq 0 -and $_.OwningProcess -gt 0 } | Select-Object -First 1
if ($pilotConnection) {
    $pilotOwner = Get-CimInstance Win32_Process -Filter "ProcessId = $($pilotConnection.OwningProcess)"
    $pilotDataPattern = '(?:^|\s)--data\s+(?:"' + [regex]::Escape($pilotData) + '"|' + [regex]::Escape($pilotData) + ')(?=\s|$)'
    $pilotReviewerPattern = '(?:^|\s)--reviewer-provider-id\s+' + [regex]::Escape([string]$pilotConfig.reviewer_provider_id) + '(?=\s|$)'
    if ($pilotOwner.ExecutablePath -ne $pilotExecutable -or $pilotOwner.CommandLine -notmatch $pilotDataPattern -or $pilotOwner.CommandLine -notmatch $pilotReviewerPattern) {
        throw 'This port belongs to another executable, data directory or reviewer. It was left untouched.'
    }
    $pilotHealth = Invoke-RestMethod "$pilotURL/api/health" -TimeoutSec 3
    if (-not $pilotHealth.ok -or $pilotHealth.service -ne 'hermetrix-harness') { throw 'The existing pilot is not healthy.' }
    Write-Output "Pilot running: $pilotURL/ (PID $($pilotOwner.ProcessId))"
    if ($Open) { try { Start-Process $pilotURL } catch { Write-Warning "Pilot is running; open $pilotURL/ manually." } }
    return
}
if ($CheckOnly) {
    Write-Output "Pilot artifacts verified. No application was started. URL when started: $pilotURL/"
    return
}

function ConvertTo-PilotArgument([string]$Value) {
    if ($Value.Length -gt 0 -and $Value -notmatch '[\s"]') { return $Value }
    '"' + [regex]::Replace([regex]::Replace($Value, '(\\*)"', '$1$1\"'), '(\\+)$', '$1$1') + '"'
}

# Only the child receives this local voice configuration. No credentials are
# copied from Codex and no historical Goal or installed application is opened.
$pilotVoiceKeys = @('HERMETRIX_WHISPER_CLI','HERMETRIX_WHISPER_MODEL','HERMETRIX_WHISPER_SERVER','HERMETRIX_WHISPER_DEVICE')
$pilotPreviousVoice = @{}
foreach ($pilotKey in $pilotVoiceKeys) { $pilotPreviousVoice[$pilotKey] = [Environment]::GetEnvironmentVariable($pilotKey, 'Process') }
try {
    $env:HERMETRIX_WHISPER_CLI = $pilotConfig.whisper_cli
    $env:HERMETRIX_WHISPER_MODEL = $pilotConfig.whisper_model
    $env:HERMETRIX_WHISPER_SERVER = $pilotConfig.whisper_server
    $env:HERMETRIX_WHISPER_DEVICE = 'CUDA'
    $pilotArgs = @('serve','--listen',$pilotListen,'--data',$pilotData,'--workspace',$pilotWorkspace,
        '--reviewer-provider-id',$pilotConfig.reviewer_provider_id,
        '--project-brain-server',$pilotConfig.brain_server_name,'--project-brain-project',$pilotConfig.brain_project)
    $pilotLog = Join-Path (Split-Path -Parent $pilotExecutable) ('pilot-' + [DateTime]::UtcNow.Ticks)
    $pilotProcess = Start-Process -FilePath $pilotExecutable -ArgumentList (($pilotArgs | ForEach-Object { ConvertTo-PilotArgument $_ }) -join ' ') `
        -WorkingDirectory $pilotRepo -WindowStyle Hidden -RedirectStandardOutput "$pilotLog.out.log" -RedirectStandardError "$pilotLog.err.log" -PassThru
    $null = $pilotProcess.Handle
} finally {
    foreach ($pilotKey in $pilotVoiceKeys) { [Environment]::SetEnvironmentVariable($pilotKey, $pilotPreviousVoice[$pilotKey], 'Process') }
}
$pilotDeadline = [DateTime]::UtcNow.AddSeconds(30)
try {
    while ([DateTime]::UtcNow -lt $pilotDeadline) {
        if ($pilotProcess.HasExited) { throw 'Pilot exited during startup.' }
        try {
            $pilotHealth = Invoke-RestMethod "$pilotURL/api/health" -TimeoutSec 1
            if ($pilotHealth.ok -and $pilotHealth.service -eq 'hermetrix-harness') {
                Write-Output "Pilot ready: $pilotURL/ (PID $($pilotProcess.Id)); logs: $pilotLog.err.log"
                Write-Output 'Project Brain still needs its own project-scoped read credential and discovery in MCP Settings.'
                if ($Open) { try { Start-Process $pilotURL } catch { Write-Warning "Pilot is ready; open $pilotURL/ manually." } }
                return
            }
        } catch { }
        Start-Sleep -Milliseconds 250
    }
    throw 'Pilot did not become healthy within 30 seconds.'
} catch {
    if (-not $pilotProcess.HasExited) { Stop-Process -Id $pilotProcess.Id -ErrorAction SilentlyContinue }
    throw "$($_.Exception.Message) Inspect $pilotLog.err.log."
}
