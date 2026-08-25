#!/usr/bin/env bash
set -euo pipefail
# 固定用 Go 1.25 工具链，避开部分 Go 1.26 安装缺 unicode/norm 表导致无法编译。
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.3}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
mkdir -p bin
go build -trimpath -ldflags "-s -w" -o bin/rackauto ./cmd/rackauto
go build -trimpath -ldflags "-s -w" -o bin/rackauto-agent ./cmd/rackauto-agent
echo "ok: bin/rackauto  bin/rackauto-agent"
