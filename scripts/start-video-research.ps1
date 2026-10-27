param(
    [string]$PythonPath = "$env:USERPROFILE/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/python.exe",
    [string]$NodePath = "$env:USERPROFILE/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe",
    [string]$LlamaServerPath = 'D:/llm-server/Bonsai-demo/bin/cuda/llama-server.exe',
    [string]$ModelsDirectory = 'D:/llm-server/Hermetrix-research/models',
    [int]$Port = 18814,
    [int]$VisionPort = 8089,
    [switch]$Install
)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
$taskSource = Join-Path $PSScriptRoot 'video-research'
$taskRuntime = Join-Path $taskRoot '.tmp-ui/youtube-research-20261013'
$taskPackages = Join-Path $taskRuntime 'python-packages'
$taskData = Join-Path $taskRuntime 'jobs'
foreach ($taskExecutable in @($PythonPath, $NodePath, $LlamaServerPath)) {
    if (!(Test-Path -LiteralPath $taskExecutable -PathType Leaf)) { throw "Missing runtime: $taskExecutable" }
}
New-Item -ItemType Directory -Path $taskRuntime -Force | Out-Null
if ($Install) {
    & $PythonPath -m pip install --target $taskPackages --upgrade -r (Join-Path $taskSource 'requirements.txt') --disable-pip-version-check
    if ($LASTEXITCODE) { throw 'Python dependency installation failed' }
    & $PythonPath (Join-Path $taskSource 'download_models.py') --models-dir $ModelsDirectory
    if ($LASTEXITCODE) { throw 'Model download or integrity check failed' }
}
$taskHashInput = [System.Collections.Generic.List[byte]]::new()
foreach ($taskName in @('server.py', 'pipeline.py', 'public_web.py', 'report.py')) {
    $taskHashInput.AddRange([IO.File]::ReadAllBytes((Join-Path $taskSource $taskName)))
}
$taskSHA = [Security.Cryptography.SHA256]::Create()
$taskRevision = [BitConverter]::ToString($taskSHA.ComputeHash($taskHashInput.ToArray())).Replace('-', '').ToLowerInvariant()
$taskSHA.Dispose()
$taskExisting = Get-NetTCPConnection -State Listen -LocalAddress 127.0.0.1 -LocalPort $Port -ErrorAction SilentlyContinue
if ($taskExisting) {
    $taskHealth = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/health" -TimeoutSec 4
    if ($taskHealth.service -ne 'hermetrix-video-research' -or $taskHealth.code_revision -ne $taskRevision) {
        throw "Port $Port is still running an older or different service (PID $($taskExisting.OwningProcess)). Close that exact service manually, then run this launcher again. No process was stopped."
    }
    Write-Output "Research already ready: http://127.0.0.1:$Port/"
    return
}
$taskVision = Get-NetTCPConnection -State Listen -LocalAddress 127.0.0.1 -LocalPort $VisionPort -ErrorAction SilentlyContinue
if ($taskVision) {
    $taskModels = Invoke-RestMethod -Uri "http://127.0.0.1:$VisionPort/v1/models" -TimeoutSec 4
    if (@($taskModels.data | Where-Object { $_.id -eq 'hermetrix-vision' }).Count -ne 1) { throw "Vision port $VisionPort belongs to a different model" }
    $taskVisionCommandLine = (Get-CimInstance Win32_Process -Filter "ProcessId=$($taskVision.OwningProcess)").CommandLine
    if ($taskVisionCommandLine -notmatch '(?:--cache-ram|-cram)\s+"?0"?(?:\s|$)') {
        throw "Existing vision PID $($taskVision.OwningProcess) still has an unqualified RAM-cache setting. Close that exact owned vision service manually before using this bounded launcher. No process was stopped."
    }
} else {
    $taskWeights = Join-Path $ModelsDirectory 'qwen3-vl-4b/Qwen3VL-4B-Instruct-Q4_K_M.gguf'
    $taskProjector = Join-Path $ModelsDirectory 'qwen3-vl-4b/mmproj-Qwen3VL-4B-Instruct-Q8_0.gguf'
    if (!(Test-Path -LiteralPath $taskWeights) -or !(Test-Path -LiteralPath $taskProjector)) { throw 'Run with -Install to prepare the models first' }
    $taskVisionProcess = Start-Process -FilePath $LlamaServerPath -ArgumentList @('-m', "`"$taskWeights`"", '--mmproj', "`"$taskProjector`"", '--no-mmproj-offload', '-ngl', '8', '--no-kv-offload', '--cache-ram', '0', '--no-cache-idle-slots', '--fit', 'off', '-c', '16384', '-np', '1', '-t', '6', '--image-min-tokens', '256', '--image-max-tokens', '512', '--host', '127.0.0.1', '--port', "$VisionPort", '--alias', 'hermetrix-vision', '--jinja', '--cors-origins', "http://127.0.0.1:$Port", '--no-cors-credentials') -WindowStyle Hidden -RedirectStandardOutput (Join-Path $taskRuntime 'launcher-vision-out.txt') -RedirectStandardError (Join-Path $taskRuntime 'launcher-vision-error.txt') -PassThru
    $taskVisionProcess.Id | Set-Content -LiteralPath (Join-Path $taskRuntime 'launcher-vision-pid.txt')
    $taskReady = $false
    for ($taskAttempt = 0; $taskAttempt -lt 40; $taskAttempt++) {
        try { $taskReady = (Invoke-RestMethod -Uri "http://127.0.0.1:$VisionPort/health" -TimeoutSec 1).status -eq 'ok' } catch {}
        if ($taskReady) { break }
        Start-Sleep -Milliseconds 500
    }
    if (!$taskReady) { throw 'Vision model is still loading; inspect launcher-vision-error.txt before trying again' }
}
$taskFFmpeg = (Get-Command ffmpeg -ErrorAction Stop).Source
$taskArguments = @("`"$(Join-Path $taskSource 'server.py')`"", '--port', "$Port", '--data', "`"$taskData`"", '--packages', "`"$taskPackages`"", '--whisper-model', "`"$(Join-Path $ModelsDirectory 'whisper-turbo')`"", '--vision-url', "http://127.0.0.1:$VisionPort", '--ffmpeg', "`"$taskFFmpeg`"", '--node', "`"$NodePath`"")
$taskProcess = Start-Process -FilePath $PythonPath -ArgumentList $taskArguments -WindowStyle Hidden -RedirectStandardOutput (Join-Path $taskRuntime 'launcher-service-out.txt') -RedirectStandardError (Join-Path $taskRuntime 'launcher-service-error.txt') -PassThru
$taskProcess.Id | Set-Content -LiteralPath (Join-Path $taskRuntime 'launcher-service-pid.txt')
Write-Output "Research starting: http://127.0.0.1:$Port/ (PID $($taskProcess.Id))"
Write-Output "MCP endpoint: http://127.0.0.1:$Port/mcp; trust annotations only for this reviewed local read-only implementation."
