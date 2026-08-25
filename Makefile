# Rack-auto 测试
.PHONY: all test build agent bootstrap

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

all: test build

test:
	go test ./...

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/rackauto ./cmd/rackauto
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/rackauto-agent ./cmd/rackauto-agent

agent:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o data/agent/x86_64/rackauto-agent ./cmd/rackauto-agent
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o data/agent/aarch64/rackauto-agent ./cmd/rackauto-agent

bootstrap: build
	./bin/rackauto bootstrap
