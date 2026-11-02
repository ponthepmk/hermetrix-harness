[CmdletBinding()]
param()
$ErrorActionPreference='Stop'
$repo=Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $repo
try {
    $commit=(& git rev-parse HEAD).Trim()
    if($LASTEXITCODE -ne 0){throw 'Source identity unavailable'}
    $dirty=if(@(& git status --porcelain=v1).Count){'true'}else{'false'}
    $out=Join-Path $repo ('.hermetrix-g1-staging\macbook-'+[guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $out|Out-Null
    $oldOS=$env:GOOS;$oldArch=$env:GOARCH;$oldCGO=$env:CGO_ENABLED
    try {
        $env:GOOS='darwin';$env:GOARCH='arm64';$env:CGO_ENABLED='0'
        & go build -trimpath -buildvcs=false -ldflags "-X hermetrix-harness/internal/releaseinfo.SourceCommit=$commit -X hermetrix-harness/internal/releaseinfo.SourceDirty=$dirty" -o (Join-Path $out 'hermetrix') ./cmd/hermetrix
        if($LASTEXITCODE -ne 0){throw 'Apple Silicon build failed'}
    } finally {$env:GOOS=$oldOS;$env:GOARCH=$oldArch;$env:CGO_ENABLED=$oldCGO}
    $binary=Join-Path $out 'hermetrix'
    $inspection=& go version -m $binary
    if($LASTEXITCODE -ne 0 -or -not($inspection -match 'GOARCH=arm64') -or -not($inspection -match 'GOOS=darwin')){throw 'Unexpected Mac executable target'}
    [ordered]@{format='hermetrix-macbook-candidate/v1';source_commit=$commit;source_dirty=$dirty;sha256=(Get-FileHash $binary).Hash.ToLowerInvariant();target='darwin/arm64';hardware='Apple Silicon M3/M4';native_execution_verified=$false;production_ready=$false;provider_dependency='Bonsai is an optional independent provider on the PC'}|ConvertTo-Json|Set-Content (Join-Path $out 'manifest.json') -Encoding utf8
    Copy-Item -LiteralPath (Join-Path $repo 'docs\MAC-PC-READINESS.md') -Destination $out
    @'
Apple Silicon development candidate, not a verified native Mac release.
After copying to an approved development folder on the Mac:
  chmod +x ./hermetrix
  ./hermetrix version
  ./hermetrix preflight --data ./development-data
Do not use an installed data directory or migrate user data without approval.
Use scripts/start-development-harness.sh from the source checkout to initialize
isolated staging. Configure Bonsai PC separately through provider settings.
Native microphone, speech, command sandbox and cross-machine acceptance remain required.
'@|Set-Content (Join-Path $out 'README.txt') -Encoding utf8
    # tar preserves a Unix executable mode when chmod is applied on the Mac;
    # never imply Windows-created mode bits are sufficient for Gatekeeper.
    & tar -czf (Join-Path $out 'hermetrix-macbook-arm64.tar.gz') -C $out hermetrix manifest.json MAC-PC-READINESS.md README.txt
    if($LASTEXITCODE -ne 0){throw 'Mac archive packaging failed'}
    Write-Output (Join-Path $out 'hermetrix-macbook-arm64.tar.gz')
} finally {Pop-Location}
