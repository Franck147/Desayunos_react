param(
    [Parameter(Mandatory=$true)]
    [string]$Email,
    [Parameter(Mandatory=$true)]
    [string]$Nombre,
    [Parameter(Mandatory=$true)]
    [string]$Telefono,
    [string]$Rol = "ADMIN",
    [switch]$Demo
)

$backendDir = Join-Path $PSScriptRoot "backend"
$dataDir = Join-Path $PSScriptRoot ".data"
$mode = if ($Demo) { "demo" } else { "sqlite" }

$secPass = Read-Host -Prompt "Ingrese la contraseña para $Email" -AsSecureString
$bstr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($secPass)
$plainPass = [System.Runtime.InteropServices.Marshal]::PtrToStringAuto($bstr)
[System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)

$env:APP_MODE = $mode
$env:DATA_DIR = $dataDir
$env:ADMIN_PASSWORD = $plainPass

Push-Location $backendDir
try {
    & ".\desayunos-api.exe" user -email $Email -name $Nombre -phone $Telefono -role $Rol
} finally {
    $env:ADMIN_PASSWORD = ""
    Pop-Location
}
