#!/usr/bin/env bash
# Build UI + Windows exe from Linux/macOS (cross-compile), luôn nén bằng UPX.
# File ra luôn có số phiên bản: iosview-<VERSION>.exe
set -euo pipefail
cd "$(dirname "$0")"
VERSION=0.10.35
TAGS=""
# WAILS=1 ./build.sh — bản Wails v3 (0.11.0 PoC), cửa sổ frameless + Mica; cần Go >= 1.26
if [ "${WAILS:-}" = "1" ]; then VERSION=0.11.14; TAGS="wails,production"; fi
OUT="iosview-$VERSION.exe"
command -v upx >/dev/null || { echo "Thiếu upx (apt install upx-ucl / brew install upx)"; exit 1; }
[ -d web/node_modules ] || (cd web && npm install)
(cd web && node build.mjs)
TMP="${OUT%.exe}.build.exe"
trap 'rm -f "$TMP"' EXIT
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -tags "$TAGS" -ldflags "-s -w -H=windowsgui -X main.version=$VERSION" -o "$TMP" ./cmd/iosview
upx --best --lzma -q "$TMP"
upx -t -q "$OUT"
echo "OK -> $OUT ($(du -h "$OUT" | cut -f1), đã nén UPX)"
