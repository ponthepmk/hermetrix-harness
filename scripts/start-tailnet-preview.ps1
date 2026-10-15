[CmdletBinding()]
param(
    [string]$Domain = 'desktop-nh66lnq.tail0f7f57.ts.net',
    [ValidateRange(1,65535)][int]$PublicPort = 18810,
    [ValidateRange(1,65535)][int]$GatewayPort = 18811,
    [string]$Upstream = 'http://127.0.0.1:18810',
    [Parameter(Mandatory=$true)][string]$Principal
)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$privateDir = Join-Path $repoRoot '.tmp-ui\tailnet-preview'
$executable = Join-Path $repoRoot '.tmp-ui\hermetrix-tailnet.exe'
$null = New-Item -ItemType Directory -Force -Path $privateDir
$account = [Security.Principal.WindowsIdentity]::GetCurrent().Name
& icacls $privateDir /inheritance:r /grant:r "${account}:(OI)(CI)F" 'SYSTEM:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Cannot protect gateway credentials.' }
$tokenFile = Join-Path $privateDir 'access-token.txt'
if (-not (Test-Path -LiteralPath $tokenFile)) {
    $randomBytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($randomBytes) } finally { $rng.Dispose() }
    [IO.File]::WriteAllText($tokenFile, [Convert]::ToBase64String($randomBytes).TrimEnd('=').Replace('+','-').Replace('/','_'))
}
$token = [IO.File]::ReadAllText($tokenFile).Trim()
if ($token.Length -lt 32) { throw 'Gateway token is invalid.' }
$certificate = Join-Path $privateDir 'server.crt'
$privateKey = Join-Path $privateDir 'server.key'
& tailscale cert --cert-file $certificate --key-file $privateKey $Domain
if ($LASTEXITCODE -ne 0) { throw 'Tailscale certificate request failed.' }
$health = Invoke-RestMethod "$Upstream/api/health" -TimeoutSec 3
if ($health.ok -ne $true -or $health.service -ne 'hermetrix-harness') { throw 'Upstream is not healthy Hermetrix.' }
$listener = Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort $GatewayPort -State Listen -ErrorAction SilentlyContinue
if ($listener) {
    $owner = Get-CimInstance Win32_Process -Filter "ProcessId = $($listener.OwningProcess)"
    foreach ($expected in @($executable, $Domain, $Upstream, $Principal, $certificate, $privateKey)) {
        if ($owner.ExecutablePath -ne $executable -or -not $owner.CommandLine.Contains($expected)) { throw 'Gateway port belongs to another configuration; left untouched.' }
    }
} else {
    if (-not (Test-Path -LiteralPath $executable)) { throw 'Build cmd/hermetrix-tailnet before starting.' }
    $previousToken = [Environment]::GetEnvironmentVariable('HERMETRIX_TAILNET_TOKEN', 'Process')
    try {
        $env:HERMETRIX_TAILNET_TOKEN = $token
        $arguments = @('--listen', "127.0.0.1:$GatewayPort", '--upstream', $Upstream, '--public-host', "${Domain}:$PublicPort", '--tls-cert', $certificate, '--tls-key', $privateKey, '--auth-principal', $Principal)
        # All arguments are operator-controlled paths or authorities; quote each
        # value and reject quotes rather than permit ambiguous Windows argv.
        if ($arguments | Where-Object { $_.Contains('"') -or $_.EndsWith('\') }) { throw 'Unsupported quote/trailing backslash in gateway arguments.' }
        $argumentLine = ($arguments | ForEach-Object { '"' + $_ + '"' }) -join ' '
        $gateway = Start-Process -FilePath $executable -ArgumentList $argumentLine -WorkingDirectory $repoRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $privateDir 'gateway.log') -RedirectStandardError (Join-Path $privateDir 'gateway.error.log') -PassThru
        Write-Host "Started authenticated gateway PID $($gateway.Id)."
    } finally { [Environment]::SetEnvironmentVariable('HERMETRIX_TAILNET_TOKEN', $previousToken, 'Process') }
}
$serveText = & tailscale serve status --json
if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect Tailscale Serve.' }
$serveConfig = $serveText | ConvertFrom-Json
$existing = $serveConfig.TCP.PSObject.Properties | Where-Object { $_.Name -eq [string]$PublicPort }
$target = "127.0.0.1:$GatewayPort"
if ($existing -and $existing.Value.TCPForward -ne $target) { throw 'Public port has a different Tailscale route; left untouched.' }
& tailscale serve --bg --yes "--tcp=$PublicPort" "tcp://$target"
if ($LASTEXITCODE -ne 0) { throw 'Cannot enable private Tailscale route.' }
Write-Host "Tailnet URL: https://${Domain}:$PublicPort/"
Write-Host "Login token file: $tokenFile"
