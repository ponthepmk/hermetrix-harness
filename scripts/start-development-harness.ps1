[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$ExecutablePath,
    [string]$DataRoot,
    [ValidatePattern('^127\.0\.0\.1:[0-9]{1,5}$')][string]$Listen = '127.0.0.1:18962',
    [switch]$CheckOnly,
    [switch]$Initialize,
    [switch]$AllowUnisolatedCommands
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$staging = [IO.Path]::GetFullPath((Join-Path $repo '.hermetrix-g1-staging'))
if (-not $DataRoot) { $DataRoot = Join-Path $staging 'runtime' }
$data = [IO.Path]::GetFullPath($DataRoot)
if (-not $data.StartsWith($staging + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'This launcher only accepts G1 staging data; installed data is not allowed' }
# Do not traverse links into another installation.
for ($ancestor = $data; $ancestor -and $ancestor.Length -ge $staging.Length; $ancestor = Split-Path -Parent $ancestor) {
    if ((Test-Path -LiteralPath $ancestor) -and ((Get-Item -LiteralPath $ancestor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Linked staging data is not allowed' }
}
$exe = (Resolve-Path -LiteralPath $ExecutablePath).Path
$reportText = & $exe preflight --data $data
if ($LASTEXITCODE -ne 0) { throw 'Read-only preflight failed; nothing started' }
$report = ($reportText -join "`n") | ConvertFrom-Json
if ($CheckOnly) { $reportText; return }
if ($report.data.state -eq 'not_initialized') {
    if (-not $Initialize) { throw 'New staging data requires -Initialize' }
} elseif (-not $report.data.ready_without_migration) { throw 'An isolated migration/restore check is required before starting this data' }
$port = [int]($Listen.Split(':')[1])
if ($port -lt 1 -or $port -gt 65535) { throw 'Invalid port' }
if (Get-NetTCPConnection -LocalAddress '127.0.0.1' -LocalPort $port -ErrorAction SilentlyContinue | Where-Object { $_.RemotePort -eq 0 }) { throw 'Port occupied; existing process left untouched' }
# No provider, Brain, token or installed configuration is inherited as a command
# argument. Missing inference is handled by the app, not by a launcher hard gate.
$previousSandboxPolicy = $env:HERMETRIX_REQUIRE_OS_SANDBOX
try {
    $env:HERMETRIX_REQUIRE_OS_SANDBOX = if ($AllowUnisolatedCommands) { '0' } else { '1' }
    & $exe serve --data $data --workspace $repo --listen $Listen
    if ($LASTEXITCODE -ne 0) { throw 'Staging server exited with an error' }
} finally { $env:HERMETRIX_REQUIRE_OS_SANDBOX = $previousSandboxPolicy }
