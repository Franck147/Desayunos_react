param(
    [switch]$Demo,
    [switch]$Produccion
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$backendDir = Join-Path $root "backend"
$frontendDir = Join-Path $root "frontend"
$dataDir = Join-Path $root ".data"
$goExe = Join-Path $root ".tools\go\bin\go.exe"

$mode = if ($Demo) { "demo" } else { "sqlite" }

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "  Desayunos - Modo: $mode" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan

# Compilar backend si go está disponible
if (Test-Path $goExe) {
    Write-Host "Compilando backend con Go..." -ForegroundColor Gray
    Push-Location $backendDir
    try {
        & $goExe build -o desayunos-api.exe .
    } finally {
        Pop-Location
    }
}

$env:APP_MODE = $mode
$env:DATA_DIR = $dataDir
$env:PORT = "8080"
$env:APP_ORIGIN = "http://127.0.0.1:3000"
$env:COOKIE_SECURE = "false"
$env:API_URL = "http://127.0.0.1:8080"

Write-Host "Iniciando Backend API en http://127.0.0.1:8080..." -ForegroundColor Cyan
$backendProc = Start-Process -FilePath (Join-Path $backendDir "desayunos-api.exe") -WorkingDirectory $backendDir -PassThru -NoNewWindow

Start-Sleep -Seconds 1

try {
    Write-Host "Iniciando Frontend Next.js en http://127.0.0.1:3000..." -ForegroundColor Green
    Write-Host "Presiona Ctrl+C para detener ambos servicios.`n" -ForegroundColor Yellow
    Push-Location $frontendDir
    if ($Produccion) {
        npm.cmd run start
    } else {
        npm.cmd run dev
    }
} finally {
    Pop-Location
    if ($backendProc -and -not $backendProc.HasExited) {
        Write-Host "`nDeteniendo Backend API..." -ForegroundColor Yellow
        Stop-Process -Id $backendProc.Id -Force -ErrorAction SilentlyContinue
    }
    Write-Host "Servicios detenidos." -ForegroundColor Gray
}
