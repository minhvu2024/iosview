# Build iosview-<VERSION>.exe (UI + Go agent), luôn nén bằng UPX. Requires Go >= 1.26, Node >= 18, upx trong PATH
# (winget install upx  hoặc  choco install upx  hoặc tải https://github.com/upx/upx/releases).
param([switch]$Wails)   # -Wails: bản Wails v3 (0.11.0 PoC), cửa sổ frameless + Mica; cần Go >= 1.26 (go tự tải toolchain)
$ErrorActionPreference = "Stop"
$Version = "0.10.35"
$Tags = ""
if ($Wails) { $Version = "0.11.14"; $Tags = "wails,production" }
$Out = "iosview-$Version.exe"
if (-not (Get-Command upx -ErrorAction SilentlyContinue)) { throw "Thiếu upx trong PATH" }
Push-Location "$PSScriptRoot\web"
try {
    if (-not (Test-Path node_modules)) {
        npm ci
        if ($LASTEXITCODE -ne 0) { throw "Không cài được dependency UI" }
    }
    node build.mjs
    if ($LASTEXITCODE -ne 0) { throw "Build UI thất bại" }
} finally { Pop-Location }
Push-Location $PSScriptRoot
$env:CGO_ENABLED = "0"
$TempOut = "iosview-$Version.build.exe"
try {
    go build -trimpath -tags "$Tags" -ldflags "-s -w -H=windowsgui -X main.version=$Version" -o $TempOut ./cmd/iosview
    if ($LASTEXITCODE -ne 0) { throw "Build Go thất bại" }
    upx --best --lzma -q $TempOut
    if ($LASTEXITCODE -ne 0) { throw "Nén UPX thất bại" }
    upx -t -q $TempOut
    if ($LASTEXITCODE -ne 0) { throw "Kiểm tra UPX thất bại" }
    Move-Item -LiteralPath $TempOut -Destination $Out -Force
} finally {
    if (Test-Path -LiteralPath $TempOut) { Remove-Item -LiteralPath $TempOut -Force }
    Pop-Location
}
Write-Host "OK -> $Out (đã nén UPX)"
