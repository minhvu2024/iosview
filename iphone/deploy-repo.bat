@echo off
setlocal
rem Day len Cloudflare Pages - project iosview - https://iosview.pages.dev/
rem   1. kho Sileo TrollVNC: thu muc repo
rem   deploy-repo.bat             : kho Sileo, kiem tra ban PC local trung ban dang phat
rem   deploy-repo.bat 0.11.11      : kem dung ban PC duoc chi dinh
rem Lan dau: trinh duyet mo ra de dang nhap Cloudflare, bam Allow.
cd /d "%~dp0"

where npx >nul 2>&1
if errorlevel 1 goto nonode

set "IOSVIEW_RELEASE_VERSION=%~1"
if not defined IOSVIEW_RELEASE_VERSION goto sileoOnly
echo [1/3] Chuan bi ban iOSView cho nhan vien...
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference = 'Stop'; $ver = $env:IOSVIEW_RELEASE_VERSION;" ^
  "if ($ver -notmatch '^\d+\.\d+\.\d+
  "New-Item -ItemType Directory -Force repo\pc | Out-Null;" ^
  "Copy-Item $exe.FullName repo\pc\iosview.exe -Force;" ^
  "$h = (Get-FileHash repo\pc\iosview.exe -Algorithm SHA256).Hash.ToLower();" ^
  "$n = (Get-Item repo\pc\iosview.exe).Length;" ^
  "[pscustomobject]@{version=$ver; sha256=$h; size=$n; url='iosview.exe'} | ConvertTo-Json -Compress | Set-Content -Encoding ascii repo\pc\latest.json;" ^
  "Set-Content -Encoding ascii repo\_headers ('/pc/*', '  Cache-Control: no-cache', '/Packages*', '  Cache-Control: no-cache', '/Release', '  Cache-Control: no-cache');" ^
  "Write-Host ('      Ban cho nhan vien: ' + $ver)"
if errorlevel 1 goto failed
goto deploy

:sileoOnly
echo [1/3] Kiem tra ban PC trong thu muc repo trung ban dang phat...
powershell -NoProfile -Command ^
  "$ErrorActionPreference = 'Stop'; $local = Get-Content repo\pc\latest.json -Raw | ConvertFrom-Json;" ^
  "$live = Invoke-RestMethod -TimeoutSec 15 'https://iosview.pages.dev/pc/latest.json';" ^
  "$hash = (Get-FileHash repo\pc\iosview.exe -Algorithm SHA256).Hash;" ^
  "if ($local.version -ne $live.version -or $local.sha256 -ne $live.sha256 -or $local.size -ne $live.size -or $local.url -ne $live.url -or $local.sha256 -ne $hash) { throw 'Ban PC local khac ban dang phat. Chi dinh phien ban neu muon phat hanh PC.' }"
if errorlevel 1 goto failed

:deploy
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
) { throw 'So phien ban phai co dang x.y.z' };" ^
  "$exe = Get-Item -LiteralPath ('..\iosview-' + $ver + '.exe');" ^
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
