$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$Dist = Join-Path $Root "dist"
$Stage = Join-Path $Dist "v1"
$Zip = Join-Path $Dist "v1-windows-amd64.zip"

New-Item -ItemType Directory -Force $Dist | Out-Null
New-Item -ItemType Directory -Force $Stage | Out-Null
Get-ChildItem -LiteralPath $Stage -Force | Remove-Item -Recurse -Force
if (Test-Path $Zip) {
    Remove-Item -LiteralPath $Zip -Force
}

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
if (-not $env:GOCACHE) {
    $env:GOCACHE = $env:TEMP
}

go build -trimpath -ldflags "-s -w -H=windowsgui" -o (Join-Path $Stage "LineOnePhone.exe") ./cmd/gateway
go build -trimpath -ldflags "-s -w" -o (Join-Path $Stage "phone-cli.exe") ./cmd/phone-cli

Copy-Item -Path (Join-Path $Root "web") -Destination (Join-Path $Stage "web") -Recurse -Force
Copy-Item -Path (Join-Path $Root "docs") -Destination (Join-Path $Stage "docs") -Recurse -Force
if (Test-Path (Join-Path $Stage "docs\edge-pdf-profile")) {
    Remove-Item -LiteralPath (Join-Path $Stage "docs\edge-pdf-profile") -Recurse -Force
}
Copy-Item -Path (Join-Path $Root ".env.example") -Destination (Join-Path $Stage ".env.example") -Force

Compress-Archive -Path $Stage -DestinationPath $Zip -Force
Write-Host "Package created: $Zip"
