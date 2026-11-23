@echo off
setlocal
rem Day len Cloudflare Pages - project iosview - https://iosview.pages.dev/
rem   1. kho Sileo TrollVNC: thu muc repo
rem   2. ban iOSView cho nhan vien: repo\pc\iosview.exe + repo\pc\latest.json
rem      lay file iosview-x.y.z.exe co so phien ban cao nhat trong D:\CODE\iosview
rem      (bo qua ban thu nhu iosview-0.11.0-poc.exe, iosview-x.y.z-noupx.exe)
rem Lan dau: trinh duyet mo ra de dang nhap Cloudflare, bam Allow.
cd /d "%~dp0"

where npx >nul 2>&1
if errorlevel 1 goto nonode

echo [1/3] Chuan bi ban iOSView cho nhan vien...
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$exe = Get-ChildItem ..\iosview-*.exe | Where-Object { $_.BaseName -match '^iosview-\d+(\.\d+){1,3}$' } | Sort-Object { [version]($_.BaseName -replace '^iosview-','') } | Select-Object -Last 1;" ^
  "if (-not $exe) { Write-Host 'Khong thay iosview-*.exe'; exit 1 };" ^
  "$ver = $exe.BaseName -replace '^iosview-','';" ^
  "New-Item -ItemType Directory -Force repo\pc | Out-Null;" ^
  "Copy-Item $exe.FullName repo\pc\iosview.exe -Force;" ^
  "$h = (Get-FileHash repo\pc\iosview.exe -Algorithm SHA256).Hash.ToLower();" ^
  "$n = (Get-Item repo\pc\iosview.exe).Length;" ^
  "[pscustomobject]@{version=$ver; sha256=$h; size=$n; url='iosview.exe'} | ConvertTo-Json -Compress | Set-Content -Encoding ascii repo\pc\latest.json;" ^
  "Set-Content -Encoding ascii repo\_headers ('/pc/*', '  Cache-Control: no-cache', '/Packages*', '  Cache-Control: no-cache', '/Release', '  Cache-Control: no-cache');" ^
  "Write-Host ('      Ban cho nhan vien: ' + $ver)"
if errorlevel 1 goto failed

echo [2/3] Kiem tra dang nhap Cloudflare...
call npx --yes wrangler@4 whoami | findstr /i "not authenticated" >nul
if not errorlevel 1 call npx --yes wrangler@4 login

echo [3/3] Day len Cloudflare Pages...
call npx --yes wrangler@4 pages deploy repo --project-name iosview --branch main --commit-dirty=true
if errorlevel 1 goto failed

echo.
echo XONG. Kiem tra: https://iosview.pages.dev/pc/latest.json
echo Kho Sileo: https://iosview.pages.dev/
pause
exit /b 0

:nonode
echo Chua co Node.js. Cai ban LTS tai https://nodejs.org roi chay lai file nay.
pause
exit /b 1

:failed
echo.
echo LOI - xem thong bao phia tren, chup man hinh gui lai.
pause
exit /b 1
