Write-Host "Verificando backend (pruebas unitarias)..." -ForegroundColor Cyan
Push-Location (Join-Path $PSScriptRoot "backend")
& "..\.tools\go\bin\go.exe" test -v ./...
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    exit $LASTEXITCODE
}
& ".\desayunos-api.exe" check
Pop-Location

Write-Host "`nVerificando frontend (TypeScript)..." -ForegroundColor Cyan
Push-Location (Join-Path $PSScriptRoot "frontend")
npm.cmd run typecheck
Pop-Location
Write-Host "`nVerificación completa exitosa." -ForegroundColor Green
