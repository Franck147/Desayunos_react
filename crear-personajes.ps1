param(
    [switch]$Demo
)

$backendDir = Join-Path $PSScriptRoot "backend"
$dataDir = Join-Path $PSScriptRoot ".data"
$mode = if ($Demo) { "demo" } else { "sqlite" }

$env:APP_MODE = $mode
$env:DATA_DIR = $dataDir

Push-Location $backendDir
try {
    & ".\desayunos-api.exe" seed-users
} finally {
    Pop-Location
}
