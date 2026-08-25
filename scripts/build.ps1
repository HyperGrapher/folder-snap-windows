param(
    [ValidateSet('Debug', 'Release')]
    [string]$Configuration = 'Release'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$outputDir = Join-Path $projectRoot 'bin'
New-Item -ItemType Directory -Force -Path $outputDir | Out-Null

$arguments = @('build', '-trimpath', '-o', (Join-Path $outputDir 'FolderSnap.exe'))
if ($Configuration -eq 'Release') {
    # go-fltk is static, but MinGW's C++ runtime is dynamic unless the external
    # linker is told to include libgcc/libstdc++ as well.
    $arguments += @('-ldflags', '-H=windowsgui -s -w -linkmode external -extldflags "-static -static-libgcc -static-libstdc++"')
}
$arguments += './cmd/foldersnap'

Push-Location $projectRoot
try {
    & go @arguments
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
    Write-Host "Built $(Join-Path $outputDir 'FolderSnap.exe')"
} finally {
    Pop-Location
}
