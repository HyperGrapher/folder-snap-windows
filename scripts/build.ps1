param(
    [ValidateSet('Debug', 'Release')]
    [string]$Configuration = 'Release'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$outputDir = Join-Path $projectRoot 'bin'
New-Item -ItemType Directory -Force -Path $outputDir | Out-Null

$resourceScript = Join-Path $projectRoot 'assets\foldersnap.rc'
$resourceObject = Join-Path $projectRoot 'cmd\foldersnap\foldersnap_windows_amd64.syso'
$windres = Get-Command windres -ErrorAction SilentlyContinue
if (-not $windres) { throw 'windres is required to embed the FolderSnap icon.' }
& $windres.Source --target=pe-x86-64 --input-format=rc --output-format=coff -i $resourceScript -o $resourceObject
if ($LASTEXITCODE -ne 0) { throw "windres failed with exit code $LASTEXITCODE" }

$arguments = @('build', '-trimpath', '-o', (Join-Path $outputDir 'FolderSnap.exe'))
if ($Configuration -eq 'Release') {
    # go-fltk is static, but MinGW's C++ runtime is dynamic unless the external
    # linker is told to include libgcc/libstdc++ as well.
    # libfltk_png.a uses setjmp/longjmp. Newer MinGW distributions no longer
    # pull libmingwex in implicitly for a static cgo link, so keep it explicit
    # to make CI and local release toolchains behave consistently.
    $arguments += @('-ldflags', '-H=windowsgui -s -w -linkmode external -extldflags "-static -static-libgcc -static-libstdc++ -lmingwex"')
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
