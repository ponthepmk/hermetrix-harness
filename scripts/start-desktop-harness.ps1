$ErrorActionPreference = 'Stop'
$desktopRepo = Split-Path -Parent $PSScriptRoot
$desktopData = Join-Path $desktopRepo '.tmp-ui/desktop-preview-data-20261002'
$desktopExe = Join-Path $desktopRepo '.tmp-ui/hermetrix-desktop-final.exe'
$desktopUrl = 'http://127.0.0.1:18806'
$desktopConnection = Get-NetTCPConnection -LocalPort 18806 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
if ($desktopConnection) {
  $desktopProcess = Get-Process -Id $desktopConnection.OwningProcess
  if ($desktopProcess.Path -ne $desktopExe) { throw 'Port 18806 belongs to another process; it was left untouched.' }
  Invoke-RestMethod "$desktopUrl/api/health" -TimeoutSec 3 | Out-Null
  Write-Output "Desktop control ready: $desktopUrl/ (existing instance reused)"
  return
}
if (-not (Test-Path -LiteralPath $desktopData)) { New-Item -ItemType Directory -Path $desktopData -Force | Out-Null }
Push-Location $desktopRepo
try {
  go build -o $desktopExe ./cmd/hermetrix
  if ($LASTEXITCODE -ne 0) { throw 'Desktop harness build failed; no server was started.' }
} finally { Pop-Location }
$desktopProcess = Start-Process -FilePath $desktopExe -ArgumentList @('serve','--listen','127.0.0.1:18806','--data',$desktopData) -WorkingDirectory $desktopRepo -WindowStyle Hidden -RedirectStandardOutput (Join-Path $desktopRepo '.tmp-ui/desktop-final.out.log') -RedirectStandardError (Join-Path $desktopRepo '.tmp-ui/desktop-final.err.log') -PassThru
for ($desktopPoll = 0; $desktopPoll -lt 40; $desktopPoll++) {
  if ($desktopProcess.HasExited) { throw 'Desktop harness stopped; inspect .tmp-ui/desktop-final.err.log.' }
  try {
    Invoke-RestMethod "$desktopUrl/api/health" -TimeoutSec 1 | Out-Null
    Write-Output "Desktop control ready: $desktopUrl/ (18801/18804 and their data preserved)"
    return
  } catch {}
  Start-Sleep -Milliseconds 250
}
throw 'Desktop harness startup did not become healthy; inspect its logs.'
