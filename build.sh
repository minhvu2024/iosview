#!/usr/bin/env bash
# Build UI + Windows exe from Linux/macOS (cross-compile), luôn nén bằng UPX.
# File ra luôn có số phiên bản: iosview-<VERSION>.exe
set -euo pipefail
cd "$(dirname "$0")"
VERSION=0.10.8
OUT="iosview-$VERSION.exe"
command -v upx >/dev/null || { echo "Thiếu upx (apt install upx-ucl / brew install upx)"; exit 1; }
[ -d web/node_modules ] || (cd web && npm install)
(cd web && node build.mjs)
rm -f "$OUT"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$VERSION" -o "$OUT" ./cmd/iosview
upx --best --lzma -q "$OUT"
upx -t -q "$OUT"
echo "OK -> $OUT ($(du -h "$OUT" | cut -f1), đã nén UPX)"
