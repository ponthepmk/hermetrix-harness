[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $repo
try {
    $commit = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot identify source commit' }
    $dirty = if (@(& git status --porcelain=v1 --untracked-files=all).Count) { 'true' } else { 'false' }
    $files = @(& git ls-files --cached --others --exclude-standard -- cmd internal assets go.mod go.sum scripts/build-development-release.ps1 |
        Where-Object { $_ -notmatch '(_test\.go$|\.test\.[jm]s$)' -and (Test-Path -LiteralPath $_ -PathType Leaf) } | Sort-Object -Unique)
    if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate build inputs' }
    $entries = @($files | ForEach-Object {
        if (-not (Test-Path -LiteralPath $_ -PathType Leaf)) { throw "Missing build input: $_" }
        [ordered]@{ path = $_; sha256 = (Get-FileHash -LiteralPath $_ -Algorithm SHA256).Hash.ToLowerInvariant() }
    })
    $treeText = ($entries | ForEach-Object { $_.path + "`0" + $_.sha256 }) -join "`n"
    $hash = [Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($treeText))
    $tree = [Convert]::ToHexString($hash).ToLowerInvariant()
    $out = Join-Path $repo ('.hermetrix-g1-staging\release-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $out | Out-Null
    $exe = Join-Path $out 'hermetrix.exe'
    $prefix = 'hermetrix-harness/internal/releaseinfo'
    & go build -trimpath -buildvcs=false -ldflags "-X $prefix.SourceCommit=$commit -X $prefix.SourceTreeSHA256=$tree -X $prefix.SourceDirty=$dirty" -o $exe ./cmd/hermetrix
    if ($LASTEXITCODE -ne 0) { throw 'Development release build failed' }
    $manifestText = & $exe version
    if ($LASTEXITCODE -ne 0) { throw 'Release cannot report its version' }
    $manifest = ($manifestText -join "`n") | ConvertFrom-Json
    $actualHash = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($manifest.release.executable_sha256 -ne $actualHash -or $manifest.release.source_tree_sha256 -ne $tree) { throw 'Release identity mismatch' }
    # Refuse to label a build with a tree changed while the compiler was reading.
    foreach ($entry in $entries) {
        if ((Get-FileHash -LiteralPath $entry.path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $entry.sha256) { throw 'Source changed during build; release is not qualified' }
    }
    $afterFiles = @(& git ls-files --cached --others --exclude-standard -- cmd internal assets go.mod go.sum scripts/build-development-release.ps1 |
        Where-Object { $_ -notmatch '(_test\.go$|\.test\.[jm]s$)' -and (Test-Path -LiteralPath $_ -PathType Leaf) } | Sort-Object -Unique)
    if ((Compare-Object $files $afterFiles) -or (& git rev-parse HEAD).Trim() -ne $commit) { throw 'Build input set changed during build' }
    $manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $out 'manifest.json') -Encoding utf8
    $entries | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $out 'source-inputs.json') -Encoding utf8
    Write-Output $exe
} finally { Pop-Location }
