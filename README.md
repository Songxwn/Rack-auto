# Rack-auto

把机房里的裸金属，从「按开机键」变成「在网页里点一下」。

服务器 PXE 网络启动 → 内存里跑一套 RAMOS（Ubuntu live-server + Agent）装 Linux，或进入 Windows PE 装 Windows Server 2019–2025 → 控制面下发镜像、账号、分区和网卡 → 需要时再用 IPMI / Redfish 开关机。传统 BIOS 和 UEFI 都支持。

[![ci](https://github.com/Songxwn/Rack-auto/actions/workflows/ci.yml/badge.svg)](https://github.com/Songxwn/Rack-auto/actions/workflows/ci.yml)
[最新版本](https://github.com/Songxwn/Rack-auto/releases/latest)

![Rack-auto 控制台总览](docs/images/console-overview.png)

控制台是深色机房 HUD：左侧 01–08 导航，总览上能看到节点、在线 Agent、镜像、进行中的任务，以及 DHCP 与上报事件。账号和 SSH 公钥可存成模板，装机时一键引用。

**详细逐步教程（网络怎么接、DHCP、第一次装机、排错）请看 [docs/deploy.md](docs/deploy.md)。** 下面是最短路径，方便你先把控制台跑起来。

## 它怎么工作

```
DHCP / TFTP  ──►  iPXE（BIOS：undionly.kpxe  /  UEFI：ipxe.efi）
                       │
                       ├── Linux ──► RAMOS（内存 Ubuntu + Agent）──► 写 qcow2 / cloud-init
                       └── Windows Server ──► WinPE（wimboot + boot.wim）──► DISM / bcdboot
                       │
                       ▼
           Rack-auto 控制面（Web + SQLite）
```

## 五分钟上手

控制面请用 **Linux**（要占用 UDP 67 / 69）。**请直接用 GitHub Release 二进制，不要自己 `go build`。** 源码编译容易被本机 Go 版本坑到（见下方「开发」）。

### 1. 安装 Release

到 [Releases](https://github.com/Songxwn/Rack-auto/releases/latest) 下载对应包。当前 `v0.3.0` 解压后文件名带平台后缀：

```bash
# Linux x86_64 示例；ARM 把 amd64 换成 arm64
mkdir -p bin data/agent/x86_64
curl -fLO https://github.com/Songxwn/Rack-auto/releases/latest/download/rackauto-linux-amd64.tar.gz
tar -tzf rackauto-linux-amd64.tar.gz    # 先看里面叫什么
tar -xzf rackauto-linux-amd64.tar.gz
# v0.3.0：rackauto-linux-amd64 / rackauto-agent-linux-amd64
# 以后版本包内可能直接是 rackauto / rackauto-agent
install -m 0755 rackauto-linux-amd64 bin/rackauto 2>/dev/null || install -m 0755 rackauto bin/rackauto
install -m 0755 rackauto-agent-linux-amd64 data/agent/x86_64/rackauto-agent 2>/dev/null \
  || install -m 0755 rackauto-agent data/agent/x86_64/rackauto-agent
[ -f winpe-curl.exe ] && install -m 0755 winpe-curl.exe bin/winpe-curl.exe
```

更完整的 `/opt/rackauto` 安装见 [docs/deploy.md](docs/deploy.md#4-安装控制面)。配置文件不在 Release 包里，按 [单独下载源码配置文件](docs/deploy.md#5-单独下载源码配置文件) 从 GitHub 拉取 YAML 和 systemd 单元即可，不必 clone。

### 升级（覆盖二进制）

不要 `go build`。到 [Releases](https://github.com/Songxwn/Rack-auto/releases/latest) 下载对应包，**覆盖**控制面、Agent 和 `winpe-curl.exe` 后重启：

```bash
# systemd 安装在 /opt/rackauto 时；ARM 把 amd64 换成 arm64
cd /tmp
curl -fLO https://github.com/Songxwn/Rack-auto/releases/latest/download/rackauto-linux-amd64.tar.gz
tar -tzf rackauto-linux-amd64.tar.gz
tar -xzf rackauto-linux-amd64.tar.gz
CTRL=
AGENT=
[ -f rackauto ] && CTRL=rackauto
[ -z "$CTRL" ] && [ -f rackauto-linux-amd64 ] && CTRL=rackauto-linux-amd64
[ -f rackauto-agent ] && AGENT=rackauto-agent
[ -z "$AGENT" ] && [ -f rackauto-agent-linux-amd64 ] && AGENT=rackauto-agent-linux-amd64
sudo systemctl stop rackauto
sudo install -m 0755 "$CTRL" /opt/rackauto/bin/rackauto
sudo install -m 0755 "$AGENT" /opt/rackauto/data/agent/x86_64/rackauto-agent
[ -f winpe-curl.exe ] && sudo install -m 0755 winpe-curl.exe /opt/rackauto/bin/winpe-curl.exe
sudo systemctl start rackauto
curl -sS http://127.0.0.1:8080/api/v1/health
```

配置、数据库、镜像、ISO 都不要动。控制面、Agent、`winpe-curl.exe` 都要换；已在 RAMOS 里的机器需重新 PXE 才能拿到新 Agent。逐步说明（含文件名带平台后缀的旧包、ARM、Docker）见 [升级教程](docs/deploy.md#13-升级控制面下载二进制覆盖)。

### 2. 写一份配置

Release 安装没有 example 文件时，按 [单独下载源码配置文件](docs/deploy.md#5-单独下载源码配置文件) 拉取，不要 clone 整仓：

```bash
# 已有源码目录时：
cp configs/rackauto.example.yaml configs/rackauto.yaml

# /opt 安装（第 5 节已下载 example）时：
# sudo nano /opt/rackauto/configs/rackauto.yaml
```

打开文件，把 `public_url` 改成**待装机服务器能访问的控制面地址**，例如 `http://10.0.0.50:8080`。不要填 `127.0.0.1`：PXE 起来之后，是服务器自己来拉内核的。

建议同时设一个 `api_token`，给 **RAMOS Agent 和脚本** 用（写在配置里即可，网页不再填）。打开网页用账号登录，默认 **admin / admin**，进控制台后点右上角「账号」改掉。

### 3. 准备引导文件

iPXE 已内置。Ubuntu 26.04 live-server ISO 第一次需要联网缓存（约 2.7GB，不必装 Go）。bootstrap 会再打一份只含 squashfs 的 `casper.iso`，PXE 机器只拉这一份：

```bash
./bin/rackauto bootstrap -config configs/rackauto.yaml
```

详见 [部署教程](docs/deploy.md#7-bootstrap本机-ipxe-与离线缓存)。

### 4. 启动

DHCP / TFTP 需要特权端口，请用 root：

```bash
sudo ./bin/rackauto serve -config configs/rackauto.yaml
```

浏览器打开 `http://<控制面IP>:8080`，用 **admin / admin** 登录。左下角变成 `CTRL // ONLINE` 就对了。登录后请立刻改密码。

### 5. 让服务器能 PXE

- **实验室空网段：** 打开「网络引导」，选中连交换机的**接入网卡**，启用内置 DHCP，点保存并应用。同一二层不要再开别的 DHCP。
- **机房已有 DHCP：** 不要开内置。把 `next-server` 指到控制面；BIOS 用 `undionly.kpxe`，UEFI 用 `ipxe.efi`。页面底部有可复制的 dhcpd / dnsmasq 片段。

接着在「机器」里登记 BMC（或让服务器 PXE 一次自动报到），在「镜像」登记 cloud 镜像 URL 或上传自己用 KVM 导出的 qcow2（[怎么做镜像](docs/deploy.md#11-自己用-kvm-做装机镜像)），Windows Server 则上传官方 ISO（[Windows Server 装机](docs/deploy.md#105-windows-server-2019-2025)），到「装机」填用户后下发。逐步点击说明在 [第一次装机](docs/deploy.md#10-第一次装机)。

## 常用配置

完整示例见 `configs/rackauto.example.yaml`。Web「网络引导」里改的 DHCP 会存进数据库，优先于 YAML。

| 项 | 说明 |
| --- | --- |
| `listen` | Web / iPXE / API，默认 `:8080` |
| `public_url` | 服务器 PXE 后访问控制面的 URL，必须是对端可达地址 |
| `api_token` | 给 Agent / 脚本用（`X-API-Token`）。网页管理改走账号登录，不拦 iPXE |
| `data_dir` | 数据库、TFTP、镜像、Agent 存放目录 |
| `tftp_listen` | 默认 `:69` |
| `dhcp.enabled` | 内置 DHCP，也可只在网页里开关 |
| `dhcp.interface` | 只在这块接入网卡上应答 PXE |
| `bootstrap.ubuntu_release` | RAMOS 用的 Ubuntu LTS，默认 `26.04` |
| `bootstrap.ubuntu_mirror` | 空/`auto` 则从 Ubuntu 官方 CD 镜像列表取路径并选延迟最低的（不含阿里云）；也可钉死 URL |

## 长期运行

- systemd 单元示例：[deploy/rackauto.service](deploy/rackauto.service)（无源码时按 [第 5 节](docs/deploy.md#5-单独下载源码配置文件) 下载）
- Docker（host 网络）需要 clone 源码：先 `cp configs/rackauto.example.yaml configs/rackauto.yaml` 并改好 `public_url`，再 `cd deploy && docker compose up -d --build`，然后在容器里跑一次 `bootstrap`。细节在 [Docker 部署](docs/deploy.md#14-docker-部署)。

## API 摘要

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/health` | 健康检查（含 `version`） |
| POST | `/api/v1/login` | Web 登录（默认 admin / admin） |
| GET | `/api/v1/session` | 当前登录状态 |
| PUT | `/api/v1/account` | 修改控制台用户名/密码 |
| CRUD | `/api/v1/machines` | 机器与 BMC |
| POST | `/api/v1/machines/{id}/power` | `on/off/cycle/reset/soft` |
| POST | `/api/v1/machines/{id}/boot` | PXE/磁盘/光盘，BIOS 或 UEFI |
| POST | `/api/v1/machines/{id}/detect` | 检测品牌/型号/序列号（Redfish 或已上报的 DMI） |
| GET | `/api/v1/os-catalog` | 镜像系统/版本（网卡与分区后端） |
| GET | `/api/v1/windows/kms-keys` | Windows Server 2019–2025 内置 KMS 客户端密钥 (GVLK) |
| CRUD | `/api/v1/templates` | 账号 / SSH 密钥模板（装机一键引用） |
| POST | `/api/v1/images/upload/init` | 大文件分片上传：创建会话 |
| PUT | `/api/v1/images/upload/{id}/chunk` | 上传分片（`X-Upload-Offset`） |
| POST | `/api/v1/images/upload/{id}/complete` | 合并分片并检测镜像 |
| POST | `/api/v1/images/upload` | 小文件整包 multipart（兼容） |
| POST | `/api/v1/images/{id}/inspect` | 检测已上传镜像的分区表和引导 |
| POST | `/api/v1/jobs/install` | 装机任务 |
| POST | `/api/v1/jobs/stress` | 压测任务 |
| DELETE | `/api/v1/jobs/{id}` | 删除任务（含卡死的 pending/running，解除 WinPE 绑定） |
| GET | `/api/v1/nics` | 控制面主机网卡 |
| POST | `/api/v1/dhcp/apply` | 保存并应用 DHCP（含接入网卡） |
| POST | `/api/v1/dhcp/stop` | 停止内置 DHCP |
| GET | `/ipxe/boot.ipxe` | iPXE 入口 |
| GET | `/winpe/wimboot` | Windows PE 加载器（无需登录） |
| GET | `/winpe/curl.exe` | 注入 WinPE 的 curl（无需登录） |
| GET | `/ipxe/windows/{mac}/...` | WinPE 脚本 / unattend（无需登录） |

## 开发

源码要求 **Go 1.25**。不要用本机已经损坏的 Go 1.26 直接 `go build`（常见报错 `undefined: nfcSparseValues`）。也不要 `go build -o bin/rackauto` 却忘了建 `bin/`。

Linux / macOS：

```bash
git clone https://github.com/Songxwn/Rack-auto.git
cd Rack-auto
export GOTOOLCHAIN=go1.25.3
export GOPROXY=https://goproxy.cn,direct   # 国内建议加上
mkdir -p bin
go build -o bin/rackauto ./cmd/rackauto
go build -o bin/rackauto-agent ./cmd/rackauto-agent
```

或 `make build` / `bash scripts/build.sh`。Windows 用 `powershell -File scripts/build.ps1`。

```bash
go test ./...
```

GitHub Actions 在推送 `v*` 标签时用 Go 1.25 编译 **linux amd64 / arm64** 并发布 Release（不编 Mac / Windows）。生产环境请用 Release，不要在控制面主机上编译。

## License

MIT
