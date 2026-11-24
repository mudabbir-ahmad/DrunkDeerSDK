# Builds dist\DeerKB.exe and dist\DeerKB-<version>.zip. Run from anywhere:
#   powershell -ExecutionPolicy Bypass -File build.ps1 [-SkipTests]
param([switch]$SkipTests)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$src = Join-Path $root 'deerkb'
$dist = Join-Path $root 'dist'

$version = '0.1.0'
$versionFile = Join-Path $root 'VERSION'
if (Test-Path $versionFile) {
    $version = (Get-Content $versionFile -TotalCount 1).Trim()
}

New-Item -ItemType Directory -Force -Path $dist | Out-Null
$exe = Join-Path $dist 'DeerKB.exe'
$zip = Join-Path $dist "DeerKB-$version.zip"

Push-Location $src
try {
    if (-not $SkipTests) {
        go vet ./...
        if ($LASTEXITCODE -ne 0) { throw "go vet failed" }
        go test ./...
        if ($LASTEXITCODE -ne 0) { throw "go test failed" }
    }

    # Embed the manifest (DPI awareness, Windows 10/11 compatibility) as a resource the linker picks up.
    go run github.com/akavel/rsrc@v0.10.2 -manifest app.manifest -o rsrc_windows_amd64.syso
    if ($LASTEXITCODE -ne 0) { throw "rsrc failed" }

    # -H windowsgui: no console window in the release build.
    go build -trimpath -ldflags "-s -w -H windowsgui -X main.appVersion=$version" -o $exe .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
}
finally {
    Pop-Location
}

$files = @($exe, (Join-Path $src 'NOT-YET-SUPPORTED.md'), (Join-Path $root 'README.md')) |
    Where-Object { Test-Path $_ }
if (Test-Path $zip) { Remove-Item $zip }
Compress-Archive -Path $files -DestinationPath $zip

"{0}: {1:N2} MB" -f $exe, ((Get-Item $exe).Length / 1MB)
"{0}: {1:N2} MB" -f $zip, ((Get-Item $zip).Length / 1MB)
