[CmdletBinding()]
param(
    [string]$WorkspaceRoot = 'D:\Projects\hermetrix-delivery',
    [string]$DataRoot,
    [string]$ExecutablePath,
    [ValidateNotNullOrEmpty()]
    [string]$ReviewerProviderId = 'provider_0a5ea6ae26e5656aaddf01e318ed60a0',
    [ValidatePattern('^127\.0\.0\.1:[0-9]{1,5}$')]
    [string]$Listen = '127.0.0.1:18804',
    [switch]$Rebuild,
    [switch]$Open,
    [switch]$CheckOnly,
    [switch]$Foreground,
    [ValidateRange(5, 120)]
    [int]$StartupTimeoutSec = 30
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $DataRoot) { $DataRoot = Join-Path $repoRoot '.tmp-ui\delivery-budget-data-20261002' }
if (-not $ExecutablePath) { $ExecutablePath = Join-Path $repoRoot '.tmp-ui\hermetrix-delivery-final.exe' }

# Reuse detached startup and exact process/data/workspace/reviewer checks.
# Opening existing work does not require inference or a different provider.
# Rebuilding a running instance is refused; stop/reconcile it explicitly first.
& (Join-Path $PSScriptRoot 'start-easiiot-harness.ps1') `
    -WorkspaceRoot $WorkspaceRoot -DataRoot $DataRoot -ExecutablePath $ExecutablePath `
    -ReviewerProviderId $ReviewerProviderId -Listen $Listen `
    -Rebuild:$Rebuild -Open:$Open -CheckOnly:$CheckOnly -Foreground:$Foreground `
    -StartupTimeoutSec $StartupTimeoutSec
