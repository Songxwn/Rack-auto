# 固定 Go 1.25 工具链，避开本机 Go 1.26 缺表无法编译的问题。
$env:GOTOOLCHAIN = if ($env:GOTOOLCHAIN) { $env:GOTOOLCHAIN } else { "go1.25.3" }
if (-not $env:GOPROXY) { $env:GOPROXY = "https://goproxy.cn,direct" }
New-Item -ItemType Directory -Force -Path bin | Out-Null
go build -trimpath -ldflags "-s -w" -o bin/rackauto.exe ./cmd/rackauto
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go build -trimpath -ldflags "-s -w" -o bin/rackauto-agent.exe ./cmd/rackauto-agent
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "ok: bin/rackauto.exe  bin/rackauto-agent.exe"
