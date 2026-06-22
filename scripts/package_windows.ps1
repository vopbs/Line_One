$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$Dist = Join-Path $Root "dist"
$PackageName = "line-one-webrtc-sip"
$Stage = Join-Path $Dist $PackageName
$Zip = Join-Path $Dist "$PackageName-windows-amd64.zip"

New-Item -ItemType Directory -Force $Dist | Out-Null
if (Test-Path $Stage) {
    Remove-Item -LiteralPath $Stage -Recurse -Force
}
if (Test-Path $Zip) {
    Remove-Item -LiteralPath $Zip -Force
}
New-Item -ItemType Directory -Force $Stage | Out-Null

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

go build -trimpath -ldflags "-s -w -H=windowsgui" -o (Join-Path $Stage "LineOnePhone.exe") ./cmd/gateway
go build -trimpath -ldflags "-s -w" -o (Join-Path $Stage "phone-cli.exe") ./cmd/phone-cli

Copy-Item -Path (Join-Path $Root "web") -Destination (Join-Path $Stage "web") -Recurse
Copy-Item -Path (Join-Path $Root "docs") -Destination (Join-Path $Stage "docs") -Recurse
if (Test-Path (Join-Path $Stage "docs\edge-pdf-profile")) {
    Remove-Item -LiteralPath (Join-Path $Stage "docs\edge-pdf-profile") -Recurse -Force
}
Copy-Item -Path (Join-Path $Root ".env.example") -Destination (Join-Path $Stage ".env.example")

Compress-Archive -Path $Stage -DestinationPath $Zip -Force
Write-Host "Package created: $Zip"
