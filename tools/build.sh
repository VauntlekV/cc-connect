#!/bin/bash
# 编译安装当前检出的代码：前端 + 测试 + 二进制。
# 用法：tools/build.sh；安装目录默认 ~/.local/opt/cc-connect，可用 OUT=... 覆盖。
# 装好后自己重启服务（如 systemctl --user restart cc-connect）。
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=${OUT:-$HOME/.local/opt/cc-connect}
(cd web && npm install --no-audit --no-fund && npm run build)
go test -tags no_web ./agent/acp/ ./agent/claudecode/ ./platform/telegram/ ./config/
go test -tags no_web ./core/ -run Relay
mkdir -p "$OUT"
base=$(git rev-parse --short origin/main 2>/dev/null || echo unknown)
go build -ldflags "-s -w -X main.version=main-$base-local -X main.commit=$(git rev-parse --short HEAD) -X main.buildTime=$(date -u +%FT%TZ)" \
  -o "$OUT/cc-connect.new" ./cmd/cc-connect
mv "$OUT/cc-connect.new" "$OUT/cc-connect"
"$OUT/cc-connect" --version 2>/dev/null | head -1
