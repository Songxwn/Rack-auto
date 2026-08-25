# Rack-auto

裸金属服务器自动化装机平台：iPXE 网络引导（传统 BIOS + UEFI）→ 内存中的 RAMOS → 控制面下发镜像与配置 → IPMI / Redfish 开关机与引导。

[![ci](https://github.com/Songxwn/Rack-auto/actions/workflows/ci.yml/badge.svg)](https://github.com/Songxwn/Rack-auto/actions/workflows/ci.yml)

## 能做什么

- **iPXE 启动**：TFTP 提供 `undionly.kpxe` / `ipxe.efi`，再 HTTP chainload 到按 MAC 生成的脚本
- **BIOS 与 UEFI**：按 DHCP option 93 选择引导文件；装机可选 GPT+ESP 或 bios_grub
- **RAMOS**：Alpine 内核 + initramfs 在内存运行，加载 overlay 后启动 `rackauto-agent`
- **装机配置**：SSH 公钥、用户密码、磁盘分区、网卡（DHCP/静态）写入 cloud-init / netplan / interfaces
- **压测**：CPU、内存、硬盘、到控制面的网络吞吐
- **BMC**：IPMI 2.0（lanplus）与 Redfish 设置 PXE/磁盘引导、开机、关机、重启

```
DHCP/TFTP ──► iPXE (BIOS 或 UEFI)
                 │
                 ▼
            RAMOS (内存 Alpine + Agent)
                 │  HTTP API
                 ▼
         Rack-auto 控制面 (Web + SQLite)
                 │
                 ├── 下发装机 / 压测任务
                 └── IPMI / Redfish → BMC 开关机、指定引导
```

## 快速开始

需要 Go 1.25+。控制面建议跑在 Linux 上（TFTP/DHCP 特权端口）；Windows 可先跑 Web 与 Agent 联调。

```bash
git clone https://github.com/Songxwn/Rack-auto.git
cd Rack-auto
cp configs/rackauto.example.yaml configs/rackauto.yaml
# 把 public_url 改成机器能访问的地址，不要用 127.0.0.1
go build -o bin/rackauto ./cmd/rackauto
./bin/rackauto bootstrap -config configs/rackauto.yaml
./bin/rackauto serve -config configs/rackauto.yaml
```

浏览器打开 `http://<控制面>:8080`。

`bootstrap` 会：

1. 下载 iPXE 到 `data/tftp/`
2. 下载 Alpine LTS 内核/initramfs/modloop 到 `data/ramos/<arch>/`
3. 交叉编译 Linux `rackauto-agent` 到 `data/agent/`

### 现有 DHCP

把 next-server 指到控制面，BIOS `filename undionly.kpxe`，UEFI `filename ipxe.efi`。iPXE 随后请求 `/ipxe/boot.ipxe`。

也可以在 Web「网络引导」里启用内置 DHCP，并指定一块**接入网卡**（只在该网卡上提供 PXE 地址）。需要 root/管理员权限绑定 UDP 67。

### 装机流程

1. 在「机器」里登记 BMC（IPMI 或 Redfish），或让服务器先 PXE 进 RAMOS 自动发现
2. 在「镜像」登记 Ubuntu/Debian 等 cloud 镜像 URL，或上传文件
3. 「装机」选择机器与镜像，填写用户、密码、SSH 公钥、分区、网卡
4. 可选「同时 BMC PXE 重启」——设置一次 PXE 引导并电源循环
5. Agent 在内存系统里分区、写镜像、注入配置；完成后把引导切回本地磁盘并重启

Agent 也可在任意 Linux 上手工运行（用于无 PXE 的调试）：

```bash
./rackauto-agent --url http://10.0.0.1:8080 --token <token>
```

## 配置

见 `configs/rackauto.example.yaml`。常用项：

| 项 | 说明 |
| --- | --- |
| `listen` | HTTP 控制台与 iPXE/API |
| `public_url` | 服务器 PXE 后访问控制面的 URL |
| `api_token` | 非空则 API 需要 `X-API-Token` |
| `tftp_listen` | 默认 `:69` |
| `dhcp.enabled` | 内置 DHCP，也可在 Web 管理 |
| `dhcp.interface` | DHCP / PXE 接入网卡 |

## API 摘要

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/health` | 健康检查 |
| CRUD | `/api/v1/machines` | 机器与 BMC |
| POST | `/api/v1/machines/{id}/power` | `on/off/cycle/reset/soft` |
| POST | `/api/v1/machines/{id}/boot` | PXE/磁盘/光盘，BIOS 或 UEFI |
| POST | `/api/v1/jobs/install` | 装机任务 |
| POST | `/api/v1/jobs/stress` | 压测任务 |
| GET | `/api/v1/nics` | 控制面主机网卡 |
| POST | `/api/v1/dhcp/apply` | 保存并应用 DHCP（含接入网卡） |
| POST | `/api/v1/dhcp/stop` | 停止内置 DHCP |
| GET | `/ipxe/boot.ipxe` | iPXE 入口 |

## 开发

```bash
go test ./...
go build ./cmd/rackauto
go build ./cmd/rackauto-agent
```

GitHub Actions 在打 `v*` 标签时编译 linux/darwin/windows 多架构并发布 Release。

## License

MIT
