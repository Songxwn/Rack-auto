GOTOOLCHAIN ?= go1.25.3
export GOTOOLCHAIN
# 国内拉模块失败时取消下一行注释：
# export GOPROXY ?= https://goproxy.cn,direct

.PHONY: build test

build:
	mkdir -p bin
	go build -trimpath -ldflags "-s -w" -o bin/rackauto ./cmd/rackauto
	go build -trimpath -ldflags "-s -w" -o bin/rackauto-agent ./cmd/rackauto-agent

test:
	go test ./...
