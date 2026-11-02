[CmdletBinding()]
param()
$ErrorActionPreference='Stop'
$repo=Split-Path -Parent $PSScriptRoot
$exe=& (Join-Path $PSScriptRoot 'build-development-release.ps1')
if($LASTEXITCODE -ne 0){throw 'Release build failed'}
$exe=($exe | Select-Object -Last 1).ToString()
$releaseRoot=Split-Path -Parent $exe
$packageRoot=Join-Path $releaseRoot 'package'
New-Item -ItemType Directory -Path $packageRoot | Out-Null
Copy-Item -LiteralPath $exe -Destination (Join-Path $packageRoot 'hermetrix.exe')
foreach($name in @('manifest.json','source-inputs.json')){Copy-Item -LiteralPath (Join-Path $releaseRoot $name) -Destination $packageRoot}
# Repository-bound launchers stay in the checkout; copying one would silently
# change its workspace/data boundary when launched from the extracted package.
@'
# Hermetrix development candidate

This is a review package, not an installer or production approval.
Run hermetrix.exe version to inspect the binary identity.
Use the development launcher from the original checkout for an isolated server.
Read docs/PLATFORM-RELEASE-STATUS.json and DEVELOPMENT-RELEASE-RUNBOOK.md first.
No database, credential vault, production config, tests or raw test data is included.
'@ | Set-Content -LiteralPath (Join-Path $packageRoot 'README.md') -Encoding utf8
$docs=Join-Path $packageRoot 'docs';New-Item -ItemType Directory -Path $docs | Out-Null
foreach($name in @('AUDIT-REPORT.md','G0-IMPLEMENTATION-PLAN.md','PLATFORM-THREAT-MODEL.md','DEVELOPMENT-RELEASE-RUNBOOK.md','PLATFORM-RELEASE-STATUS.json')){
    $source=Join-Path (Join-Path $repo 'docs') $name
    if(-not(Test-Path -LiteralPath $source)){throw "Missing release document: $name"}
    Copy-Item -LiteralPath $source -Destination $docs
}
$status=Get-Content -LiteralPath (Join-Path $docs 'PLATFORM-RELEASE-STATUS.json') -Raw | ConvertFrom-Json
if($status.production_ready -ne $false){throw 'This development packager cannot attest production qualification'}
Copy-Item -LiteralPath (Join-Path $repo 'docs\api\platform-additions.openapi.json') -Destination $docs
Copy-Item -LiteralPath (Join-Path $repo 'docs\api\route-inventory.json') -Destination $docs
$files=@(Get-ChildItem -LiteralPath $packageRoot -File -Recurse | ForEach-Object{[ordered]@{path=[IO.Path]::GetRelativePath($packageRoot,$_.FullName);sha256=(Get-FileHash -LiteralPath $_.FullName).Hash.ToLowerInvariant()}})
$files | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $packageRoot 'checksums.json') -Encoding utf8
$archive=Join-Path $releaseRoot 'hermetrix-development-candidate.zip'
Compress-Archive -Path (Join-Path $packageRoot '*') -DestinationPath $archive
Write-Output $archive
