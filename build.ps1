# Build iosview-<VERSION>.exe (UI + Go agent), luôn nén bằng UPX. Requires Go >= 1.24, Node >= 18, upx trong PATH
# (winget install upx  hoặc  choco install upx  hoặc tải https://github.com/upx/upx/releases).
$ErrorActionPreference = "Stop"
$Version = "0.10.8"
$Out = "iosview-$Version.exe"
if (-not (Get-Command upx -ErrorAction SilentlyContinue)) { throw "Thiếu upx trong PATH" }
Push-Location "$PSScriptRoot\web"
if (-not (Test-Path node_modules)) { npm install }
node build.mjs
Pop-Location
Push-Location $PSScriptRoot
$env:CGO_ENABLED = "0"
if (Test-Path $Out) { Remove-Item $Out -Force }
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$Version" -o $Out ./cmd/iosview
upx --best --lzma -q $Out
upx -t -q $Out
Pop-Location
Write-Host "OK -> $Out (đã nén UPX)"
