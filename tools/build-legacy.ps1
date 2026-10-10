param(
    [string]$GoExe = "$env:USERPROFILE\sdk\go1.20.14\bin\go.exe"
)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path -Parent $PSScriptRoot)
if (-not (Test-Path -LiteralPath $GoExe)) { throw "Укажите -GoExe с путём к Go 1.20.14" }
$version = & $GoExe version
if ($version -notmatch 'go1\.20\.14 ') { throw "Legacy-сборка требует Go 1.20.14. Найдено: $version" }
$env:GOCACHE = Join-Path (Get-Location) '.tools\gocache'
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
Write-Host 'Running tests...'
& $GoExe test ./...
if ($LASTEXITCODE -ne 0) { throw 'Тесты не прошли' }
New-Item -ItemType Directory -Force dist | Out-Null
foreach ($arch in @('386', 'amd64')) {
    $env:GOARCH = $arch
    $suffix = if ($arch -eq '386') { 'x86' } else { 'amd64' }
    Write-Host "Building $suffix..."
    & $GoExe build -trimpath -ldflags '-H=windowsgui' -o "dist\es-reports-$suffix.exe" ./cmd/es-reports
    if ($LASTEXITCODE -ne 0) { throw "Ошибка сборки $arch" }
    & $GoExe build -trimpath -o "dist\es-reports-cli-$suffix.exe" ./cmd/es-reports
    if ($LASTEXITCODE -ne 0) { throw "Ошибка CLI-сборки $arch" }
}
Get-ChildItem dist\*.exe | Select-Object Name,Length
