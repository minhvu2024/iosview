#!/usr/bin/env bash
# Build UI + Windows exe from Linux/macOS (cross-compile), luôn nén bằng UPX.
set -euo pipefail
cd "$(dirname "$0")"
command -v upx >/dev/null || { echo "Thiếu upx (apt install upx-ucl / brew install upx)"; exit 1; }
[ -d web/node_modules ] || (cd web && npm install)
(cd web && node build.mjs)
rm -f iosview.exe
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=0.9.1" -o iosview.exe ./cmd/iosview
upx --best --lzma -q iosview.exe
upx -t -q iosview.exe
echo "OK -> iosview.exe ($(du -h iosview.exe | cut -f1), đã nén UPX)"
