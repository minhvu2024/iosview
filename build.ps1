# Build iosview.exe (UI + Go agent), luôn nén bằng UPX. Requires Go >= 1.24, Node >= 18, upx trong PATH
# (winget install upx  hoặc  choco install upx  hoặc tải https://github.com/upx/upx/releases).
$ErrorActionPreference = "Stop"
if (-not (Get-Command upx -ErrorAction SilentlyContinue)) { throw "Thiếu upx trong PATH" }
Push-Location "$PSScriptRoot\web"
if (-not (Test-Path node_modules)) { npm install }
node build.mjs
Pop-Location
Push-Location $PSScriptRoot
$env:CGO_ENABLED = "0"
if (Test-Path iosview.exe) { Remove-Item iosview.exe -Force }
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=0.9.1" -o iosview.exe ./cmd/iosview
upx --best --lzma -q iosview.exe
upx -t -q iosview.exe
Pop-Location
Write-Host "OK -> iosview.exe (đã nén UPX)"
