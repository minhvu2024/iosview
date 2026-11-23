# Build iosview-cd (CoreDevice sidecar for iPhone iOS 17.4+ without jailbreak)
# into .\iosview-cd\iosview-cd.exe, next to iosview-<ver>.exe.
#
#   cd D:\CODE\iosview
#   .\sidecar\build-sidecar.ps1
#
# Needs Python 3.13, 3.12 or 3.11 for Windows (python.org). Python 3.14 does
# NOT work yet: pymobiledevice3 -> pyimg4 pins pylzss==0.3.4, which has no
# prebuilt wheel for 3.14 (pip would need Visual C++ to compile it).
# A folder build (onedir) starts faster and trips antivirus less than a
# single-file exe.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$venv = Join-Path $root ".stage\cd-venv"
$ok = @("3.13", "3.12", "3.11")

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what loi (ma $LASTEXITCODE)" }
}

# Pick a supported Python through the py launcher.
$pyver = $null
foreach ($v in $ok) {
    & py "-$v" -c "import sys" 2>$null
    if ($LASTEXITCODE -eq 0) { $pyver = $v; break }
}
if (-not $pyver) {
    Write-Host ""
    Write-Host "Khong tim thay Python 3.13 / 3.12 / 3.11." -ForegroundColor Red
    Write-Host "Python 3.14 chua dung duoc. Cai them Python 3.13 (64-bit) tu python.org"
    Write-Host "(de nguyen 3.14 cung duoc), roi chay lai script nay."
    exit 1
}
Write-Host "Dung Python $pyver"

# Recreate the venv if it is missing or was made with another Python.
$vpy = "$venv\Scripts\python.exe"
if (Test-Path $vpy) {
    $have = & $vpy -c "import sys; print('%d.%d' % sys.version_info[:2])"
    if ($have -ne $pyver) {
        Write-Host "Venv cu dung Python $have, tao lai..."
        Remove-Item -Recurse -Force $venv
    }
}
if (-not (Test-Path $vpy)) {
    & py "-$pyver" -m venv $venv; Check "Tao venv"
}

& $vpy -m pip install --upgrade pip; Check "Nang cap pip"
# --only-binary: never try to compile C extensions (no Visual C++ needed).
& $vpy -m pip install --upgrade --only-binary=:all: -r "$PSScriptRoot\requirements.txt" pyinstaller
Check "Cai pymobiledevice3 / pyinstaller"

# --noupx: UPX on the bundled DLLs/.pyd (ffmpeg, cryptography...) is slow and
# can break them at runtime; only the small launcher exe is packed below.
& "$venv\Scripts\pyinstaller.exe" --noconfirm --clean --onedir --console --noupx `
    --name iosview-cd `
    --distpath $root --workpath "$root\.stage\cd-build" --specpath "$root\.stage" `
    --collect-all pymobiledevice3 `
    --collect-all pmd_pytcp `
    --collect-all pmd_net_addr `
    --collect-all developer_disk_image `
    --collect-all pytun_pmd3 `
    --copy-metadata pymobiledevice3 `
    "$PSScriptRoot\iosview_cd.py"
Check "PyInstaller"

if (Get-Command upx -ErrorAction SilentlyContinue) {
    upx -q --lzma "$root\iosview-cd\iosview-cd.exe" | Out-Null
}

Write-Host ""
Write-Host "Xong: $root\iosview-cd\iosview-cd.exe" -ForegroundColor Green
Write-Host "Chep ca thu muc iosview-cd\ canh iosview-<phien ban>.exe tren may nhan vien."
