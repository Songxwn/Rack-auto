FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /out/rackauto ./cmd/rackauto \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /out/agent-x86_64 ./cmd/rackauto-agent \
 && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /out/agent-aarch64 ./cmd/rackauto-agent

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata ipmitool wimlib
WORKDIR /var/lib/rackauto
COPY --from=build /out/rackauto /usr/local/bin/rackauto
COPY --from=build /out/agent-x86_64 /var/lib/rackauto/agent/x86_64/rackauto-agent
COPY --from=build /out/agent-aarch64 /var/lib/rackauto/agent/aarch64/rackauto-agent
COPY configs/rackauto.example.yaml /etc/rackauto.yaml
EXPOSE 8080/tcp 69/udp 67/udp
ENV RACKAUTO_CONFIG=/etc/rackauto.yaml
ENTRYPOINT ["rackauto"]
CMD ["serve", "-config", "/etc/rackauto.yaml", "-data-dir", "/var/lib/rackauto"]
