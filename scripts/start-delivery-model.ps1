param()
$ErrorActionPreference = 'Stop'
$deliveryRepoRoot = Split-Path -Parent $PSScriptRoot
$deliveryModelBinary = 'D:/llm-server/Bonsai-demo/bin/cuda/llama-server.exe'
$deliveryModelWeights = 'D:/llm-server/Bonsai-demo/models/bonsai2-gguf/27B/Ternary-Bonsai-2-27B-PQ2_0.gguf'
try {
  $deliveryExistingHealth = Invoke-RestMethod 'http://127.0.0.1:8088/health' -TimeoutSec 2
  if ($deliveryExistingHealth.status -eq 'ok') { Write-Output 'Existing local model ready: http://127.0.0.1:8088'; return }
} catch {}
if (Get-NetTCPConnection -LocalPort 8088 -State Listen -ErrorAction SilentlyContinue) { throw '8088 is occupied or loading; no additional model was started.' }
if (-not (Test-Path -LiteralPath $deliveryModelBinary) -or -not (Test-Path -LiteralPath $deliveryModelWeights)) { throw 'The existing Bonsai binary or weights are missing; no download or installation was attempted.' }
$deliveryModelProcess = Start-Process -FilePath $deliveryModelBinary -ArgumentList @('-m',$deliveryModelWeights,'-ngl','999','-c','65536','-fa','on','-ctk','q8_0','-ctv','q8_0','--jinja','--host','127.0.0.1','--port','8088','--slots') -WindowStyle Hidden -WorkingDirectory $deliveryRepoRoot -RedirectStandardOutput (Join-Path $deliveryRepoRoot '.tmp-ui/delivery-model-restored.log') -RedirectStandardError (Join-Path $deliveryRepoRoot '.tmp-ui/delivery-model-restored-error.log') -PassThru
$deliveryModelProcess.Id | Set-Content (Join-Path $deliveryRepoRoot '.tmp-ui/delivery-model-restored.pid')
$deliveryModelDeadline = (Get-Date).AddSeconds(50)
do {
  if ($deliveryModelProcess.HasExited) { throw 'Local model exited during loading; inspect saved logs.' }
  try {
    $deliveryModelHealth = Invoke-RestMethod 'http://127.0.0.1:8088/health' -TimeoutSec 2
    if ($deliveryModelHealth.status -eq 'ok') { Write-Output 'Local model ready: http://127.0.0.1:8088'; return }
  } catch {}
  Start-Sleep -Milliseconds 500
} while ((Get-Date) -lt $deliveryModelDeadline)
throw 'Model has not become healthy yet; its PID and logs were preserved. Do not start another instance.'
