[CmdletBinding()]
param(
    [string]$ProjectRoot = 'D:\Jobs\G2Tech\Mr.SB\easiiot_project',
    [string]$BaselineRef = 'd1f9870'
)
$ErrorActionPreference = 'Stop'
$harnessRoot = Split-Path -Parent $PSScriptRoot
$project = (Resolve-Path -LiteralPath $ProjectRoot).Path
$bash = 'C:\Program Files\Git\bin\bash.exe'
if (-not (Test-Path -LiteralPath $bash)) { throw 'Git Bash is required for these repository Docker tests.' }
& docker info --format '{{.ServerVersion}} {{.OSType}}'
if ($LASTEXITCODE -ne 0) { throw 'Docker Linux engine must be ready.' }
$baselineCommit = & git -C $project rev-parse --verify "$BaselineRef^{commit}"
if ($LASTEXITCODE -ne 0) { throw 'Baseline commit is unavailable.' }
$runId = Get-Date -Format 'yyyyMMdd-HHmmss'
$evidence = Join-Path $harnessRoot ".tmp-ui\easiiot-runtime-proof-$runId"
$baseline = Join-Path $evidence 'before'
New-Item -ItemType Directory -Path $evidence | Out-Null
$archive = Join-Path $evidence 'baseline.zip'
& git -C $project archive --format=zip "--output=$archive" $baselineCommit backend frontend/services frontend/package.json
if ($LASTEXITCODE -ne 0) { throw 'Could not export baseline source.' }
Expand-Archive -LiteralPath $archive -DestinationPath $baseline
New-Item -ItemType Directory -Path (Join-Path $baseline 'scripts'), (Join-Path $baseline 'frontend\tests') | Out-Null
foreach ($name in @('postgres_runtime_smoke_test.sh','postgres_integration_test.sh','test_host_compat.sh')) {
    Copy-Item -LiteralPath (Join-Path $project "scripts\$name") -Destination (Join-Path $baseline "scripts\$name")
}
# Hold the frontend and checks identical in both runs; only the backend baseline
# differs. Reuse installed dependencies without downloading or editing them.
Copy-Item -LiteralPath (Join-Path $project 'frontend\services\api.js') -Destination (Join-Path $baseline 'frontend\services\api.js')
Copy-Item -LiteralPath (Join-Path $project 'frontend\tests\api-refresh-runtime.mjs') -Destination (Join-Path $baseline 'frontend\tests\api-refresh-runtime.mjs')
New-Item -ItemType Junction -Path (Join-Path $baseline 'frontend\node_modules') -Target (Join-Path $project 'frontend\node_modules') | Out-Null
$productionHashBefore = (Get-FileHash -LiteralPath (Join-Path $project 'backend\cmd\api\handler\auth.go') -Algorithm SHA256).Hash
$frontendHash = (Get-FileHash -LiteralPath (Join-Path $project 'frontend\services\api.js') -Algorithm SHA256).Hash
$testHash = (Get-FileHash -LiteralPath (Join-Path $project 'frontend\tests\api-refresh-runtime.mjs') -Algorithm SHA256).Hash
function Invoke-RuntimeProof([string]$Root, [string]$Log) {
    Push-Location -LiteralPath $Root
    try {
        & $bash -c 'EASIIOT_FRONTEND_REFRESH_SMOKE=1 bash scripts/postgres_runtime_smoke_test.sh' *> $Log
        return $LASTEXITCODE
    } finally { Pop-Location }
}
Write-Host "Evidence: $evidence"
Write-Host 'BEFORE: run actual frontend against the baseline backend in a fresh isolated Docker stack'
$beforeLog = Join-Path $evidence 'before.txt'
$beforeExit = Invoke-RuntimeProof $baseline $beforeLog
$beforeText = Get-Content -LiteralPath $beforeLog -Raw
Get-Content -LiteralPath $beforeLog -Tail 12 | Write-Host
if ($beforeExit -eq 0 -or $beforeText -notmatch 'OBSERVED: rejected refresh HTTP 400; credentials remaining 3; redirect /' -or $beforeText -notmatch 'real rejected refresh must require login') {
    throw 'Baseline did not reproduce the exact refresh/session failure; do not claim this proof passed.'
}
Write-Host 'AFTER: run the identical frontend/checks against the fixed source in another fresh stack'
$afterLog = Join-Path $evidence 'after.txt'
$afterExit = Invoke-RuntimeProof $project $afterLog
$afterText = Get-Content -LiteralPath $afterLog -Raw
Get-Content -LiteralPath $afterLog -Tail 14 | Write-Host
if ($afterExit -ne 0 -or $afterText -notmatch 'OBSERVED: rejected refresh HTTP 401; credentials remaining 0; redirect /login' -or $afterText -notmatch 'PASS: API auth, worker, scheduler boot, consumer, telemetry, and backup/restore are valid') {
    throw 'Fixed-source outcome did not pass the real runtime checks.'
}
if ($productionHashBefore -ne (Get-FileHash -LiteralPath (Join-Path $project 'backend\cmd\api\handler\auth.go') -Algorithm SHA256).Hash) {
    throw 'Production source changed during reproduction.'
}
foreach ($root in @($baseline,$project)) {
    if ($frontendHash -ne (Get-FileHash -LiteralPath (Join-Path $root 'frontend\services\api.js') -Algorithm SHA256).Hash -or
        $testHash -ne (Get-FileHash -LiteralPath (Join-Path $root 'frontend\tests\api-refresh-runtime.mjs') -Algorithm SHA256).Hash) {
        throw 'Frontend/check drift invalidates the comparison.'
    }
}
$receipt = [ordered]@{
    baseline_commit = $baselineCommit
    baseline_expected_failure_exit = $beforeExit
    fixed_runtime_exit = $afterExit
    before = @{ status = 400; credentials_remaining = 3; redirect = '/' }
    after = @{ status = 401; credentials_remaining = 0; redirect = '/login' }
    production_source_unchanged = $true
    frontend_sha256 = $frontendHash
    runtime_test_sha256 = $testHash
    production_auth_sha256 = $productionHashBefore
    baseline_auth_sha256 = (Get-FileHash -LiteralPath (Join-Path $baseline 'backend\cmd\api\handler\auth.go') -Algorithm SHA256).Hash
    before_log_sha256 = (Get-FileHash -LiteralPath $beforeLog -Algorithm SHA256).Hash
    after_log_sha256 = (Get-FileHash -LiteralPath $afterLog -Algorithm SHA256).Hash
    before_log = $beforeLog
    after_log = $afterLog
}
$receipt | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $evidence 'receipt.json') -Encoding utf8
Write-Host 'PASS: exact live failure reproduced; identical checks pass after the fix; source unchanged.'
