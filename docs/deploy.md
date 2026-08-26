# 部署 Rack-auto

这份教程按「第一次把机器装起来」来写。你可以先扫一遍目录，再按自己的环境跳到对应章节。

做完之后，你应该能：在浏览器打开控制台 → 服务器 PXE 进 RAMOS 装 Linux，或进 Windows PE 装 Windows Server → 下发镜像装机。

## 目录

1. [先看清整条链路](#1-先看清整条链路)
2. [你需要准备什么](#2-你需要准备什么)
3. [推荐网络怎么接](#3-推荐网络怎么接)
4. [安装控制面](#4-安装控制面)
5. [单独下载源码配置文件](#5-单独下载源码配置文件)
6. [写配置（最容易写错的地方）](#6-写配置最容易写错的地方)
7. [bootstrap：本机 iPXE 与离线缓存](#7-bootstrap本机-ipxe-与离线缓存)
8. [启动服务](#8-启动服务)
9. [配置 DHCP](#9-配置-dhcp)
10. [第一次装机](#10-第一次装机)
    - [Windows Server 2019–2025](#105-windows-server-2019-2025)
11. [自己用 KVM 做装机镜像](#11-自己用-kvm-做装机镜像)
12. [用 systemd 长期跑](#12-用-systemd-长期跑)
13. [升级控制面（下载二进制覆盖）](#13-升级控制面下载二进制覆盖)
14. [Docker 部署](#14-docker-部署)
15. [自检清单](#15-自检清单)
16. [常见问题](#16-常见问题)

---

## 1. 先看清整条链路

待装机服务器并不是直接去下操作系统 ISO，而是：

```
服务器开机（PXE）
    → DHCP 告诉它：TFTP 在哪、引导文件叫什么
    → TFTP 下载 undionly.kpxe（传统 BIOS）或 ipxe.efi（UEFI）
    → iPXE 再通过 HTTP 找控制面：/ipxe/boot.ipxe
    → 若该 MAC 有 Windows 装机任务：wimboot 加载 WinPE（boot.wim）
         → diskpart 分区 → 把 install.wim 下到磁盘 → DISM Apply-Image → bcdboot
    → 否则进入 RAMOS（内存里的 Ubuntu live-server + Agent）
         → 控制面下发「装哪个镜像、分区、用户、SSH 公钥、网卡」
         → Agent 写盘，写完切回本地磁盘重启
```

所以控制面要同时提供三样东西：**HTTP（Web + iPXE + API）**、**TFTP**、以及一台能应答 PXE 的 **DHCP**（内置或你现有的均可）。

---

## 2. 你需要准备什么

| 准备 | 说明 |
| --- | --- |
| 一台控制面主机 | **强烈建议 Linux**。要绑定 UDP 67/69，Windows 只能先用来看 Web，不适合当 PXE 服务器。 |
| 一块装机网卡 | 和控制面、待装机服务器在**同一二层网段**（同一交换机/VLAN）。 |
| 待装机服务器 | 支持 PXE；有 BMC（IPMI/Redfish）会更省事，没有也可以人肉进 BIOS 选网卡启动。 |
| 出网 | 第一次 `bootstrap` 要下载 Ubuntu 26.04 live-server ISO（约 **2.7GB**）。默认从 Ubuntu 官方 CD 镜像列表取路径并选延迟最低的（不含阿里云）；也可把 `ubuntu_mirror` 钉死。 |
| 内存 | 待装机服务器建议 **≥ 4GB**。PXE 客户端只把 `casper.iso`（squashfs 层，大约几百 MB～1.5GB）拉进内存，不再下 2.7GB 整包。2GB 的虚拟机仍可能 OOM。 |
| 磁盘 | 控制面预留约 **5GB** 给 RAMOS（完整 ISO 缓存 + `casper.iso`）；另外再为 cloud 镜像留空间。 |
| 管理员权限 | 绑定 DHCP/TFTP 特权端口需要 root（或等价能力）。 |

**不需要**事先给每台机器装操作系统。有 BMC 的话，连显示器都可以不用。

控制面磁盘建议预留：**RAMOS 约 5GB**（`live-server.iso` + `casper.iso`），再加上你要存的 cloud 镜像（每张大约 600MB～1GB）。

---

## 3. 推荐网络怎么接

实验室最小接法：一台交换机，控制面和待装机服务器都插在上面。

更稳妥的接法（机房常用）：

```
                    ┌─────────────┐
  办公/管理网 ─────►│  控制面 NIC0 │  SSH、浏览器看 Web
                    │             │
  PXE 装机网  ─────►│  控制面 NIC1 │  DHCP + TFTP + HTTP（public_url）
                    └──────┬──────┘
                           │
                    ┌──────┴──────┐
                    │  接入交换机  │  ← 待装机服务器 PXE 网卡都接这里
                    └─────────────┘

  BMC / IPMI 网 ──► 服务器 iLO/iDRAC（可以和管理网同一段，不要和 PXE 混用也行）
```

请记住三件事：

1. **`public_url` 必须是待装机服务器能访问的地址**，不要填 `127.0.0.1` 或 `localhost`。服务器 PXE 起来后，是它自己去拉内核和 Agent 的。
2. **同一二层不要跑两套 DHCP。** 机房里如果已经有 DHCP，用「沿用现有」；实验室空网段再用内置 DHCP。
3. 内置 DHCP **只绑在你指定的接入网卡上**，不要选到办公网上网的那块网卡。

---

## 4. 安装控制面

三条路，选一条即可。实验室从 **A** 最快；要长期跑选 **A 或 C**。

### A. 用 GitHub Release 二进制（推荐，不用装 Go）

到 [Releases](https://github.com/Songxwn/Rack-auto/releases/latest) 下载对应平台的包。**不要**在控制面主机上 `go build`，安装教程默认走这条路。

`v0.3.0` 压缩包里的文件名带平台后缀（`rackauto-linux-amd64`）；之后的版本会改成包内直接叫 `rackauto`。下面两套名字都写上了：

```bash
sudo mkdir -p /opt/rackauto/{bin,configs,data/agent/x86_64}
cd /tmp
curl -fLO https://github.com/Songxwn/Rack-auto/releases/latest/download/rackauto-linux-amd64.tar.gz
tar -tzf rackauto-linux-amd64.tar.gz
tar -xzf rackauto-linux-amd64.tar.gz

CTRL=$(ls -1 rackauto-linux-amd64 rackauto 2>/dev/null | head -n1)
AGENT=$(ls -1 rackauto-agent-linux-amd64 rackauto-agent 2>/dev/null | head -n1)
if [ -z "$CTRL" ] || [ -z "$AGENT" ]; then
  echo "解压后没有找到二进制，请看 tar -tzf 的输出"; ls -l; exit 1
fi
sudo install -m 0755 "$CTRL" /opt/rackauto/bin/rackauto
sudo install -m 0755 "$AGENT" /opt/rackauto/data/agent/x86_64/rackauto-agent
[ -f winpe-curl.exe ] && sudo install -m 0755 winpe-curl.exe /opt/rackauto/bin/winpe-curl.exe
```

ARM 控制面把 `amd64` 换成 `arm64`，Agent 目录用 `data/agent/aarch64/`。Windows 压缩包是 `.zip`，只适合看 Web，PXE 请换 Linux。

若还要给 ARM 服务器装机，再下一份 `rackauto-linux-arm64.tar.gz`，把其中的 Agent 放到 `data/agent/aarch64/rackauto-agent`。

Release 包里**没有** YAML 和 systemd 单元，接着按 [第 5 节](#5-单独下载源码配置文件) 单独下载即可，不必 `git clone`。以后升级不要重装目录，按 [第 13 节](#13-升级控制面下载二进制覆盖) 下载新包、覆盖二进制即可。

### B. 从源码编译（可选）

只在你要改代码时才需要。必须是 **Go 1.25 工具链**。本机若是 Go 1.26 且报 `nfcSparseValues`，说明安装不完整，用下面的 `GOTOOLCHAIN` 即可，不必先修好 GOROOT。

还要先 `mkdir -p bin`，否则 `go build -o bin/rackauto` 会直接失败。

```bash
git clone https://github.com/Songxwn/Rack-auto.git
cd Rack-auto
export GOTOOLCHAIN=go1.25.3
export GOPROXY=https://goproxy.cn,direct
mkdir -p bin
go build -o bin/rackauto ./cmd/rackauto
go build -o bin/rackauto-agent ./cmd/rackauto-agent
```

也可以 `make build` 或 `bash scripts/build.sh`（Windows：`powershell -File scripts/build.ps1`）。

`bootstrap` 若找不到 `go`、但 `data/agent/` 里已经有 Release 的 Agent，会跳过交叉编译。

### C. Docker

见 [第 14 节](#14-docker-部署)。镜像里已经带好 Linux Agent，仍需要执行一次 `bootstrap` 下载 Ubuntu live-server ISO。

---

## 5. 单独下载源码配置文件

GitHub Release 只含 `rackauto` / `rackauto-agent`。配置示例、systemd 单元、Compose 文件在**源码仓库**里。控制面按第 4 节 A 装二进制时，**不必 clone 整仓，也不必装 Go**，按文件下载即可。

建议和当前二进制同一版：网页左下角或 `curl -sS http://127.0.0.1:8080/api/v1/health` 里的 `version`（例如 `v0.4.14`）。还没装过时用 `main`，拿仓库最新示例。

```bash
# 改成你的版本；追新用 main
VER=main
RAW="https://raw.githubusercontent.com/Songxwn/Rack-auto/${VER}"
# 若 raw.githubusercontent.com 访问失败，可改用：
# RAW="https://cdn.jsdelivr.net/gh/Songxwn/Rack-auto@${VER}"

sudo mkdir -p /opt/rackauto/configs /opt/rackauto/deploy
tmp=$(mktemp)

curl -fL -o "$tmp" "$RAW/configs/rackauto.example.yaml"
sudo install -m 0644 "$tmp" /opt/rackauto/configs/rackauto.example.yaml
# 已有 rackauto.yaml 时不要覆盖（里面是你改过的 public_url / token）
if [ ! -f /opt/rackauto/configs/rackauto.yaml ]; then
  sudo cp /opt/rackauto/configs/rackauto.example.yaml /opt/rackauto/configs/rackauto.yaml
fi

curl -fL -o "$tmp" "$RAW/deploy/rackauto.service"
sudo install -m 0644 "$tmp" /opt/rackauto/deploy/rackauto.service

# 只用 Docker 时再下这一份
curl -fL -o "$tmp" "$RAW/deploy/docker-compose.yml"
sudo install -m 0644 "$tmp" /opt/rackauto/deploy/docker-compose.yml

rm -f "$tmp"
ls -l /opt/rackauto/configs /opt/rackauto/deploy
```

浏览器打开也可以（复制到目标路径即可）：

- [configs/rackauto.example.yaml](https://github.com/Songxwn/Rack-auto/blob/main/configs/rackauto.example.yaml)
- [deploy/rackauto.service](https://github.com/Songxwn/Rack-auto/blob/main/deploy/rackauto.service)
- [deploy/docker-compose.yml](https://github.com/Songxwn/Rack-auto/blob/main/deploy/docker-compose.yml)

想一次拿整个 `configs/` 和 `deploy/`、仍不编译时，用稀疏克隆：

```bash
git clone --depth 1 --filter=blob:none --sparse https://github.com/Songxwn/Rack-auto.git
cd Rack-auto
git sparse-checkout set configs deploy
```

下好后去 [第 6 节](#6-写配置最容易写错的地方) 改 `public_url` 和 `api_token`。systemd 单元在 [第 12 节](#12-用-systemd-长期跑) 安装。升级二进制时**不要**再覆盖已经改过的 `rackauto.yaml`。

---

## 6. 写配置（最容易写错的地方）

```bash
# 已按第 5 节下载到 /opt/rackauto 时，直接编辑：
sudo ${EDITOR:-nano} /opt/rackauto/configs/rackauto.yaml

# 源码目录（clone 或稀疏克隆）里：
cp configs/rackauto.example.yaml configs/rackauto.yaml
```

用编辑器打开，**至少改这两项**：

```yaml
listen: ":8080"
public_url: "http://10.0.0.50:8080"   # 改成控制面在「装机网」上的地址
data_dir: "./data"                    # /opt 安装建议改成 /opt/rackauto/data
api_token: "请换成一段随机字符串"       # 给 Agent / 脚本用；网页改走账号登录
tftp_listen: ":69"
```

怎么查「装机网」地址：

```bash
ip -4 addr
# 或
ip -br a
```

例如接入网卡是 `enp1s0`，地址是 `10.0.0.50/24`，则 `public_url` 写成 `http://10.0.0.50:8080`。

`dhcp` 段可以先保持 `enabled: false`，启动后再到 Web「网络引导」里选网卡、点应用。YAML 里的 DHCP 只是默认值；Web 保存后会写进数据库，优先于 YAML。

---

## 7. bootstrap：本机 iPXE 与离线缓存

iPXE 固件已经打进控制面程序，**PXE 阶段不会访问 boot.ipxe.org**。`serve` 启动时也会自动把 `undionly.kpxe` / `ipxe.efi` 写到 `data/tftp/`。

RAMOS 使用 **Ubuntu 26.04 LTS live-server**（当前最新稳定版 LTS）。第一次需要联网把 ISO 缓存到本机，抽出 `casper/vmlinuz`、`casper/initrd`，再打一份只含 squashfs 层的 `casper.iso`；之后整个装机网可以离线。PXE 客户端只拉 `casper.iso`，不会把 2.7GB 整包灌进内存。

`ubuntu_mirror` 留空（或写成 `auto`）时，bootstrap 会从 [Ubuntu 官方 CD 镜像列表](https://launchpad.net/ubuntu/+cdmirrors) 拉取路径（**不使用阿里云**），再并行探测并选 **HTTP 延迟最低** 且能拿到 `SHA256SUMS` 的那一家。ISO 文件名和校验和以 `releases.ubuntu.com` / `cdimage.ubuntu.com` 为准。想钉死某源时再填完整 URL：

```yaml
bootstrap:
  ubuntu_release: "26.04"
  ubuntu_mirror: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases"
```

若已经有 ISO，不必再下：

```yaml
bootstrap:
  ubuntu_iso: "/path/to/ubuntu-26.04-live-server-amd64.iso"
```

```bash
# 源码目录（有网，只需一次）
./bin/rackauto bootstrap -config configs/rackauto.yaml

# /opt 安装
sudo /opt/rackauto/bin/rackauto bootstrap \
  -config /opt/rackauto/configs/rackauto.yaml \
  -data-dir /opt/rackauto/data

# 之后完全离线再执行（只校验缓存、重装内置 iPXE）
./bin/rackauto bootstrap -offline -config configs/rackauto.yaml
```

它会做：

1. 把内置 iPXE 写到 `data/tftp/`（BIOS：`undionly.kpxe`，UEFI：`ipxe.efi`）
2. 下载 Ubuntu live-server ISO 到 `data/ramos/ubuntu/<arch>/live-server.iso`（约 2.7GB，可续传；这是控制面缓存，机器不拉这个文件）
3. 抽出 `vmlinuz` / `initrd.stock`，打出 `casper.iso`，并把启动脚本接到 `initrd` 后面
4. 若当前目录有源码，交叉编译 Linux Agent 到 `data/agent/`

已经存在且完整的 ISO / 内核会跳过，可以重复执行。下载中断会留着 `.tmp`，下次自动续传。

没有 Go、又没从源码 bootstrap 时，请从对应架构的 Release 包拷入 Agent（文件名带平台后缀）：

```bash
sudo mkdir -p /opt/rackauto/data/agent/x86_64 /opt/rackauto/data/agent/aarch64
sudo install -m 0755 rackauto-agent-linux-amd64 /opt/rackauto/data/agent/x86_64/rackauto-agent
sudo install -m 0755 rackauto-agent-linux-arm64 /opt/rackauto/data/agent/aarch64/rackauto-agent
```

---

## 8. 启动服务

UDP 67（DHCP）和 69（TFTP）是特权端口，**第一次请用 root**：

```bash
sudo ./bin/rackauto serve -config configs/rackauto.yaml
# 或
sudo /opt/rackauto/bin/rackauto serve \
  -config /opt/rackauto/configs/rackauto.yaml \
  -data-dir /opt/rackauto/data
```

看到类似日志就对了：

```
TFTP :69 目录 .../data/tftp
控制台 http://127.0.0.1:8080  版本 v0.2.0
```

若 TFTP 失败，多半是没 root，或本机已有 tftpd 占着 69。

本机浏览器打开 `http://控制面IP:8080`，用 **admin / admin** 登录（右上角「账号」可改用户名和密码）。左下角变成 `CTRL // ONLINE` 就对了。iPXE 拉内核、Agent 报到**不走**这套网页登录。Agent 仍用配置里的 `api_token`（若你设了的话）。

![Rack-auto 控制台总览](images/console-overview.png)

侧栏「总览 / 机器 / 镜像 / 装机 / 压测 / 任务 / 网络引导」对应后面几步；DHCP 还没开时，总览里会显示 `STANDBY`，点「打开 DHCP」会跳到网络引导页。

防火墙放行（按你实际用的工具选一套）：

```bash
# firewalld
sudo firewall-cmd --add-port=8080/tcp --permanent
sudo firewall-cmd --add-port=69/udp --permanent
sudo firewall-cmd --add-port=67/udp --permanent   # 仅在使用内置 DHCP 时
sudo firewall-cmd --reload

# ufw
sudo ufw allow 8080/tcp
sudo ufw allow 69/udp
sudo ufw allow 67/udp
```

云主机安全组同样要放行这些端口，并且 **DHCP/TFTP 通常只能在同二层内网用**，不要指望跨公网 PXE。

---

## 9. 配置 DHCP

打开控制台 **08 网络引导**。上方「控制面地址」应与 `public_url` 一致，不对就改完点保存。

### 方案一：实验室，用内置 DHCP

适合：交换机上**没有**别的 DHCP，你愿意让 Rack-auto 发地址。

1. 勾选「启用内置 DHCP」
2. **接入网卡**选连着装机交换机的那块（不要选上网网卡）
3. 选中后会按网卡 IPv4 自动填网段、next-server、地址池（一般是 `.100–.200`），可再改
4. 点 **保存并应用**
5. 状态应变为「运行中」，总览页 DHCP 显示 `LIVE`

地址池不要覆盖控制面自己的 IP，也不要覆盖网关。

### 方案二：机房已有 DHCP，只改引导项

不要启用内置 DHCP。把现有服务器的 `next-server` 指到控制面，并按固件选文件名。

ISC dhcpd 示例：

```
next-server 10.0.0.50;
if exists user-class and option user-class = "iPXE" {
  filename "boot.ipxe";
} elsif option client-arch != 00:00 {
  filename "ipxe.efi";
} else {
  filename "undionly.kpxe";
}
```

dnsmasq 示例：

```
dhcp-userclass=set:ipxe,iPXE
dhcp-boot=tag:ipxe,boot.ipxe
dhcp-match=set:efi64,option:client-arch,7
dhcp-boot=tag:!ipxe,tag:efi64,ipxe.efi,,10.0.0.50
dhcp-boot=tag:!ipxe,undionly.kpxe,,10.0.0.50
```

iPXE 自己 chainload 时：

```
dhcp
chain http://10.0.0.50:8080/ipxe/boot.ipxe
```

Web 同一页底部会按你当前的 `public_url` 生成一份可复制片段。

---

## 10. 第一次装机

建议第一次用一台**可以重装**的机器，磁盘会被覆盖。

### 10.1 登记机器（两种方式）

**有 BMC：** 打开「机器」→「登记机器 / BMC」。填名称、MAC（PXE 那块网卡）、固件（UEFI 或 BIOS）、BMC 协议与地址。保存后可用「开机 / PXE重启」试一下。配了 **Redfish** 时，点「检测」会从 BMC 读取品牌、型号、序列号。

**没有 BMC：** 到机柜把服务器设为「网卡 PXE 启动」，直接开机。进 RAMOS 后，Agent 会向控制面报到，并带上 DMI（品牌/型号/序列号）。机器列表里会出现它。

列表和详情里都能看到这些信息。点 **装机** 会跳到装机向导并选中这台机器。

### 10.2 准备镜像

「镜像」页先选**系统和版本**（Debian 12/13、Ubuntu 24.04、Rocky 8–10、AlmaLinux 8–10、CentOS Stream 8–10 等），登记 URL 和**本机上传**两栏都可以单独选，再：

- **登记 URL**：例如 Ubuntu 24.04 cloud  
  `https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img`  
  类型选「云镜像（整盘 qcow2/raw）」
- **上传文件**：先在上传栏选系统和版本、镜像类型，再选文件。页面会显示进度、速率和剩余时间；传到控制面后会检测分区表和引导。
- **Windows Server 2019–2025**：类型选「Windows Server ISO」，上传官方 ISO。装机链路与 Linux 完全不同，见 [10.5](#105-windows-server-2019-2025)。
- **自己做镜像**：要用 KVM 装一套再导出 qcow2 时，按 [第 11 节](#11-自己用-kvm-做装机镜像)。BIOS 与 UEFI 各做一张；分区、cloud-init、OpenSSH、扩容工具都必须按那一节的要求。

系统和版本决定装机时怎么写网卡（Ubuntu/Debian 13 用 netplan，Debian 11/12 用 ifupdown，Rocky / AlmaLinux / CentOS Stream 8 用 ifcfg，9+ 用 NetworkManager），以及默认根文件系统（RHEL 系多为 xfs）。装完会把根分区扩到磁盘剩余空间。

待装机服务器也要能访问这个 URL；若只有内网，把镜像传到控制面「上传」，URL 会变成 `http://<public_url>/images/...`。

### 10.3 下发任务

打开「装机」，按三步向导用下拉框和表单填写，不再贴 JSON：

1. 选择机器和镜像，选固件与时区
2. 登录用户默认 `root`（也可改成 ubuntu/debian 等发行版用户），填密码；公钥可「添加」或「导入 .pub 文件」。常用账号和公钥可先在「模板」页保存，第 2 步点模板名称即可填入；也可把当前填写内容保存为新模板。
3. 从机器上报的库存里选择目标磁盘和网卡。根文件系统镜像可在分区表里增删分区（勾选「使用剩余空间」）；整盘云镜像会保留镜像自带分区，并在写盘后把根分区扩到整盘。网卡可选 DHCP / 静态，也可添加 Bond 和 VLAN（VLAN 可以建在 Bond 上）。物理网卡按 **MAC** 写入，装完后名字是 `nic0` / `nic1`，不沿用 RAMOS（Ubuntu live）里的 `ens3` / `enp1s0`。

有 BMC 可在最后一步点「同时 BMC PXE 重启」。到「任务」看进度和日志。成功后机器会切回本地磁盘重启。任务卡在「等待 PXE」或一直 pending 时，可在任务页直接删除；删掉后该 MAC 不再进 WinPE，机器状态回到就绪，然后可以重新下发。

### 10.4 没有 PXE 时的调试

在已经能进系统的 Linux 上（不要在生产盘上乱试）可以手工跑 Agent：

```bash
./rackauto-agent --url http://10.0.0.50:8080 --token <api_token>
```

### 10.5 Windows Server 2019–2025

Windows Server **不能**走 RAMOS / qcow2 / cloud-init。下发任务后，该机器下次 PXE 会加载内置的 **wimboot** 和 ISO 里的 **WinPE `boot.wim`**，在 Windows PE 里用 `diskpart` + `DISM /Apply-Image` + `bcdboot` 装系统，再用 `unattend.xml` 写管理员密码、主机名、时区和可选静态 IP。

**准备镜像**

1. 「镜像」页系统选 **Windows Server**，版本 2019 / 2022 / 2025。
2. 类型选 **Windows Server ISO**，把微软官方 ISO **上传到控制面**（不要只填外网 URL：控制面必须能抽出 `sources/boot.wim`）。`install.wim` 常常大于 4GB、在 UDF 里，控制面按 WIM 头定位后由 WinPE 直接 HTTP 下载，不必再复制一份到磁盘。
3. 点「检测」：应看到 WIM 版本列表（Standard / Datacenter，Core 或带桌面）。
4. 也可以先传一张同代 ISO 抽出 WinPE，再单独登记 `install.wim` / `install.esd`（类型 **Windows install.wim / ESD**）；没有 `boot.wim` 时会尝试借用已有 ISO 抽出的 WinPE。

只支持 **x86_64**。官方 WinPE **没有** `curl.exe` / `certutil` / `bitsadmin`。**v0.4.36+** 会把 Release 里的 `winpe-curl.exe` 经 wimboot 压进 `X:\Windows\System32\curl.exe`（静态 Go 程序，支持装机脚本用到的那几个 curl 参数）。控制面启动时把它拷到 `data/winpe/curl.exe`。升级时请把 `winpe-curl.exe` 放到 `rackauto` 旁边。

**向导差异**

- 登录用户默认 `Administrator`，**密码必填**（写入应答文件）。没有 SSH 公钥。
- 第 3 步选 WIM edition（默认偏向 Standard 带桌面，而不是 Core）、可选产品密钥、目标磁盘、DHCP 或静态 IP。
- **不要**画 Linux 分区，也 **不要** 配 Bond / VLAN。
- 主机名最长 15 个 ASCII 字符。时区 `Asia/Shanghai` 会写成 `China Standard Time`。应答文件语言保持 **en-US**，避免英文 ISO 被强行 zh-CN 后安装器卡住。
- 默认打开远程桌面。

**磁盘号**

WinPE 的 `diskpart` 用 **磁盘序号**，不是 Linux 的 `/dev/sda`。向导里若仍显示 Agent 上报的 `/dev/sda`，会按 `sda→0`、`sdb→1`、`nvme0n1→0` 来映射。没把握时选「磁盘 0」。选错会清错盘。

**装机过程中**

1. 下发任务后状态是「等待 PXE 进入 Windows PE」。不要让这台机器再进 Ubuntu RAMOS。
2. 机器 PXE 后屏幕应出现 Windows PE，而不是 Ubuntu。任务进度：partitioning → downloading_wim → applying_image → bootloader。
3. `install.wim` 会下到 Windows 分区（W:），不要指望塞进 WinPE 的内存盘 X:。5GB+ 的 WIM 需要装机网带宽和磁盘空间。
4. 应用完成后会写 BCD、拷贝 unattend 到 `\Windows\Panther\`，再 `wpeutil reboot`。第一次进系统走 OOBE 无人值守。
5. WinPE 下载阶段需要 **DHCP**；装完后的静态 IP 在 unattend 里按 MAC 生效。

评估版 ISO 可以不填产品密钥。VL / OEM 密钥按你的授权填写。

**不要做的事**

- 不要把 Windows ISO 当成「云镜像 qcow2」登记。
- 不要用 Linux Agent 去写 Windows 盘；领到 Windows 任务时 Agent 会拒绝。
- 不要用 Windows 10/11 客户端 ISO 当 Server（检测会警告没有 Server edition）。
- 本轮不做 sysprep VHDX、ARM64 Windows、拆分的 `install.swm`。

---

## 11. 自己用 KVM 做装机镜像

官方 Ubuntu cloud 镜像可以直接用。要预装驱动、软件或改成自己的发行版时，用 **KVM 装一套，再导出 qcow2**。Rack-auto 把它当「整盘云镜像」写到服务器磁盘，然后扩根分区、注入账号和网卡。

BIOS 和 UEFI **各做一张盘**，装机向导里的固件必须和镜像一致。虚拟磁盘 8～16GB 即可（稀疏 qcow2），真机容量再大也会在写盘后把根分区扩满。

### 11.1 分区必须长这样

不要 swap，不要 LVM，不要加密，不要独立 `/boot`（内核放在根分区）。否则写盘后扩容对不上，检测也会报分区异常。

| 固件 | 分区表 | 分区 | 引导 |
| --- | --- | --- | --- |
| **BIOS** | **MBR（dos）** | **只有 1 个**：整盘一个 Linux 根分区（ext4 或 xfs），**不要 GPT、不要 biosboot** | GRUB 装到磁盘 MBR；该分区必须有 **启动标志**（boot/active） |
| **UEFI** | GPT | **只有 2 个**：① EFI 系统分区（FAT32，约 200～512MB，挂 `/boot/efi`）② 根分区（ext4 或 xfs） | ESP 里要有 `\EFI\BOOT\BOOTX64.EFI`（可用 grub 的 `--removable` 写出） |

BIOS 若用 GPT，安装器通常还会再划一块 biosboot，就不是「一个分区」了。UEFI 不要再加第三个分区。

### 11.2 镜像里必须有的软件

装完系统后、关机导出前，用 root 装好并 **enable**。缺任何一项，写盘后可能没有 SSH、cloud-init 不跑、或根分区不会随磁盘变大。

| 作用 | Debian / Ubuntu | Rocky / Alma / CentOS |
| --- | --- | --- |
| cloud-init | `cloud-init` | `cloud-init` |
| 扩容分区 | `cloud-guest-utils`（`growpart`）+ `e2fsprogs` | `cloud-utils-growpart` + `xfsprogs` + `e2fsprogs` |
| SSH | `openssh-server` | `openssh-server` |

Debian / Ubuntu：

```bash
apt-get update
apt-get install -y cloud-init cloud-guest-utils e2fsprogs openssh-server
systemctl enable ssh
for u in cloud-init-local cloud-init cloud-config cloud-final; do
  systemctl enable "$u" 2>/dev/null || true
done
printf 'datasource_list: [ NoCloud, None ]\n' > /etc/cloud/cloud.cfg.d/90-datasource.cfg
```

Rocky / Alma：

```bash
dnf install -y cloud-init cloud-utils-growpart openssh-server e2fsprogs xfsprogs
systemctl enable sshd
for u in cloud-init-local cloud-init cloud-config cloud-final; do
  systemctl enable "$u" 2>/dev/null || true
done
printf 'datasource_list: [ NoCloud, None ]\n' > /etc/cloud/cloud.cfg.d/90-datasource.cfg
```

再确认：

- `cloud-init` **不要** `apt remove` / `systemctl disable` / `touch /etc/cloud/cloud-init.disabled`。第一次开机要靠它写主机名和用户。
- `sshd` 开机自启。Rack-auto 会再写入 `PermitRootLogin yes`；镜像里有 OpenSSH 即可。
- `/etc/fstab` 里不能有 swap 行。
- 网卡保持 DHCP 即可，不要把 KVM 里的静态 IP 写死；装机时会按 MAC 重写为 `nic0` / `nic1`。

### 11.3 用 virt-install 建虚拟机

在一台支持虚拟化的 Linux 上（不必是 PXE 控制面）：

```bash
# Debian/Ubuntu 宿主
sudo apt-get install -y qemu-kvm libvirt-daemon-system virtinst ovmf
# Rocky 宿主：sudo dnf install -y qemu-kvm libvirt virt-install edk2-ovmf
sudo systemctl enable --now libvirtd
```

把发行版 ISO 放到宿主上。磁盘用 qcow2。`--os-variant` 按系统改（`debian12`、`ubuntu24.04`、`rocky9` 等，`osinfo-query os` 可查；没有对应项就用 `generic`）。

BIOS（SeaBIOS，对应装机向导选 BIOS）：

```bash
sudo virt-install \
  --name ra-debian12-bios \
  --memory 2048 --vcpus 2 \
  --disk path=/var/lib/libvirt/images/ra-debian12-bios.qcow2,size=8,format=qcow2 \
  --cdrom /path/to/debian-12.iso \
  --os-variant debian12 \
  --boot hd,cdrom \
  --network network=default \
  --graphics vnc,listen=0.0.0.0
```

UEFI（OVMF，对应装机向导选 UEFI）：

```bash
sudo virt-install \
  --name ra-debian12-uefi \
  --memory 2048 --vcpus 2 \
  --disk path=/var/lib/libvirt/images/ra-debian12-uefi.qcow2,size=8,format=qcow2 \
  --cdrom /path/to/debian-12.iso \
  --os-variant debian12 \
  --boot uefi \
  --network network=default \
  --graphics vnc,listen=0.0.0.0
```

也可用 `virt-manager` 图形安装，分区规则和第 11.1 节相同。

安装器里选 **手动/自定义分区**，不要「整个磁盘自动」：

1. **BIOS：** 分区表 MBR。只建一个主分区，类型 Linux，挂载 `/`，文件系统 ext4（Rocky 可用 xfs），打上 **boot**。引导器安装位置选 **磁盘**（`/dev/vda`），不是某个分区。
2. **UEFI：** 分区表 GPT。第一块 EFI（FAT32，`/boot/efi`，200～512MB）；第二块根分区挂 `/`。不要 swap。

进系统后按 11.2 装包。UEFI 再执行一次，确保检测能看到 `BOOTX64.EFI`：

```bash
# Debian/Ubuntu
grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=BOOT --removable
update-grub
# Rocky/Alma
dnf install -y grub2-efi-x64 shim-x64
grub2-install --target=x86_64-efi --efi-directory=/boot/efi --boot-directory=/boot --removable
```

v0.4.21 起，即使镜像里只有 `\EFI\debian\grubx64.efi` 这类发行版路径，装机结束时 Agent 也会拷一份到 `\EFI\BOOT\BOOTX64.EFI`，真机固件才能找到。镜像里仍然建议带上 `--removable`。

BIOS 确认启动标志（应看到 `*`）：

```bash
fdisk -l /dev/vda
```

若不用下一节的 `virt-sysprep`，关机前清一次实例状态：

```bash
cloud-init clean --logs
truncate -s 0 /etc/machine-id
rm -f /var/lib/dbus/machine-id
poweroff
```

否则直接 `poweroff`。

### 11.4 导出 qcow2 并上传

可选：清掉本机身份，避免每台装出来的机器 `machine-id` / SSH 主机密钥相同（会保留 cloud-init 软件包）：

```bash
sudo virt-sysprep -a /var/lib/libvirt/images/ra-debian12-uefi.qcow2 \
  --operations machine-id,ssh-hostkeys,net-hostname,net-hwaddr,dhcp-client-state,logfiles,tmp-files,bash-history,cloud-init
```

压缩导出（推荐，上传更快）：

```bash
sudo qemu-img convert -p -c -O qcow2 \
  /var/lib/libvirt/images/ra-debian12-uefi.qcow2 \
  ./debian12-uefi.qcow2
```

BIOS 那张对 `ra-debian12-bios.qcow2` 再转一次。到控制台「镜像」：系统和版本选对，类型选 **云镜像（整盘 qcow2/raw）**，上传。检测应类似：

- BIOS 盘：`bootable: BIOS; root ext4 p1`（或 xfs）
- UEFI 盘：`bootable: UEFI (BOOTX64.EFI); root ext4 p2`

装机时固件与镜像一致。不要拿 BIOS 盘去装 UEFI 机器。

---

## 12. 用 systemd 长期跑

单元文件在源码 `deploy/rackauto.service`。没 clone 时按 [第 5 节](#5-单独下载源码配置文件) 下到 `/opt/rackauto/deploy/`。

```bash
# /opt 安装（第 5 节已下载）：
sudo cp /opt/rackauto/deploy/rackauto.service /etc/systemd/system/rackauto.service

# 或源码目录：
# sudo cp deploy/rackauto.service /etc/systemd/system/rackauto.service

# 按实际路径编辑 ExecStart、WorkingDirectory
sudo systemctl daemon-reload
sudo systemctl enable --now rackauto
sudo journalctl -u rackauto -f
```

---

## 13. 升级控制面（下载二进制覆盖）

升级 = **下载 GitHub Release → 覆盖控制面、Agent，以及 WinPE 用的 `winpe-curl.exe` → 重启进程**。不要在控制面 `go build`，也不要重装整个 `/opt/rackauto`。

配置、数据库、已上传镜像、RAMOS 的 ISO / `casper.iso` **都不要动**。SQLite 打开后会自己加列。

### 要覆盖哪些文件

| 文件 | 作用 |
| --- | --- |
| `rackauto` | 控制面本身（Web、API、PXE、DHCP） |
| `rackauto-agent` | 机器 PXE 进 RAMOS 之后从控制面下载的 Agent |
| `winpe-curl.exe` | 压进 WinPE 的 curl（官方 boot.wim 没有 curl/certutil） |

**都要换。** 只换控制面时，网页版本号会变，装机仍走旧 Agent。Windows 装机还需要 `winpe-curl.exe` 压进 PE。已停在 RAMOS 里的机器要**重新 PXE**，才会下载到新 Agent。

默认安装路径：

```text
/opt/rackauto/bin/rackauto
/opt/rackauto/data/agent/x86_64/rackauto-agent
```

ARM 控制面或 ARM 待装机服务器：Agent 在 `data/agent/aarch64/rackauto-agent`。若两种架构都要装，两份 Agent 都覆盖。

按 [README 最短路径](../README.md) 装在仓库目录时，对应的是 `bin/rackauto` 和 `data/agent/<arch>/rackauto-agent`。

### systemd（推荐）

在**控制面 Linux**上执行。把包名里的 `amd64` 换成你的架构（ARM 控制面用 `arm64`）。

```bash
# 1. 看现在跑的是哪一版（网页左下角也会显示）
curl -sS http://127.0.0.1:8080/api/v1/health
# 期望类似：{"ok":true,"name":"rackauto","version":"v0.4.10"}

# 2. 下载最新包（钉死版本则把 latest 换成 download/v0.4.10）
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
if [ -z "$CTRL" ] || [ -z "$AGENT" ]; then
  echo "解压后没有找到二进制，请看 tar -tzf 的输出"; ls -l; exit 1
fi

# 3. 停服务，覆盖，再启动（不要动 configs/ 和 data/）
sudo systemctl stop rackauto
sudo install -m 0755 "$CTRL" /opt/rackauto/bin/rackauto
sudo install -m 0755 "$AGENT" /opt/rackauto/data/agent/x86_64/rackauto-agent
[ -f winpe-curl.exe ] && sudo install -m 0755 winpe-curl.exe /opt/rackauto/bin/winpe-curl.exe
# 若还要给 ARM 机器装机，再解压 arm64 包：
# sudo install -m 0755 rackauto-agent /opt/rackauto/data/agent/aarch64/rackauto-agent
sudo systemctl start rackauto
sudo systemctl status rackauto --no-pager

# 4. 确认版本号已经变了
curl -sS http://127.0.0.1:8080/api/v1/health
```

`install` 是覆盖目标路径上的文件，不会新建一套目录。正在跑的进程用的是内存里的旧映像，所以**覆盖后必须重启**。

没有 systemd、前台跑的：先停掉 `rackauto serve`（Ctrl+C 或 `kill`），按同样方式 `install` 覆盖，再：

```bash
sudo /opt/rackauto/bin/rackauto serve \
  -config /opt/rackauto/configs/rackauto.yaml \
  -data-dir /opt/rackauto/data
```

### 升级之后还要不要 bootstrap

一般**不用**。`data/ramos/`、`data/tftp/`、已上传镜像都还在。

发行说明里若写了「必须再跑 bootstrap」（例如换成 `casper.iso` 那种引导方式），再执行一次即可：已有完整 ISO 不会重下。

### 机器侧

1. 浏览器强刷控制台，左下角版本应等于新 tag。
2. 下一次装机前让节点重新 PXE（网页里 PXE 引导，或 BMC 下次启动改网卡）。已经停在旧 RAMOS 里的 Agent 不会自动热更新。
3. 打开「镜像」页：v0.4.10 起会检测已上传镜像的分区表和引导。

### Docker

Compose 不是「覆盖宿主机两个文件」，而是更新镜像后重建容器。数据在 volume 里，配置仍是挂进去的 `configs/rackauto.yaml`：

```bash
cd /path/to/Rack-auto
git fetch --tags
git checkout v0.4.10   # 或 git pull 跟 main
cd deploy
docker compose up -d --build
curl -sS http://127.0.0.1:8080/api/v1/health
```

不要在 Windows 宿主机上 `go build`。构建发生在 Docker 里。

---

## 14. Docker 部署

Compose 使用 `network_mode: host`，这样 DHCP/TFTP 才能绑网卡。构建镜像需要仓库源码（Dockerfile 的 context 是仓库根），请 `git clone`。只跑 Release 二进制时不要走 Compose，用第 4 节 A + [第 5 节](#5-单独下载源码配置文件) 即可。

已有源码时：

```bash
cp configs/rackauto.example.yaml configs/rackauto.yaml
# 编辑 public_url、data_dir 可保持默认，容器内会用 -data-dir /var/lib/rackauto
cd deploy
docker compose up -d --build
docker compose exec rackauto rackauto bootstrap -config /etc/rackauto.yaml -data-dir /var/lib/rackauto
```

之后浏览器访问宿主机 `:8080`。内置 DHCP 仍要在 Web 里选**宿主机**网卡名（host 网络下看到的就是宿主机网卡）。

---

## 15. 自检清单

按顺序打勾，卡在哪一步就去下一节对号入座。

- [ ] `curl -sS http://<public_url>/api/v1/health` 返回正常（从**另一台机器**测，不要只在控制面本机测 `127.0.0.1`）
- [ ] `ls data/tftp/` 里有 `undionly.kpxe` 和 `ipxe.efi`
- [ ] `ls data/ramos/ubuntu/x86_64/` 里有 `vmlinuz`、`initrd`、`casper.iso`、`layerfs-path`（`live-server.iso` 是控制面缓存，大约 2.7GB）
- [ ] `ls data/agent/x86_64/rackauto-agent` 文件存在且可执行
- [ ] 控制台左下角是 `CTRL // ONLINE`
- [ ] DHCP：要么内置显示运行中，要么现有 dhcpd/dnsmasq 已改 next-server
- [ ] 服务器 PXE 后能看到 iPXE，而不是一直 `DHCP...`
- [ ] 机器列表出现该节点，或你已手工登记 MAC
- [ ] 自制镜像已按 [第 11 节](#11-自己用-kvm-做装机镜像) 分区（BIOS 一分区 / UEFI 仅 EFI+根），并装好 cloud-init、扩容工具、OpenSSH
- [ ] 装机任务日志里能看到写盘、注入用户，而不是下载内核 404

---

## 16. 常见问题

**装机账号和公钥每次都要手填**  
在「模板」里保存账号（用户名/密码，公钥可选）或密钥（公钥列表）。装机向导第 2 步点模板名称就会填入；也可以把当前填写内容存成新模板。密码存在控制面 SQLite，和 BMC 密码一样靠 API Token 保护。

**打开网页要登录**  
默认账号密码是 **admin / admin**。进控制台后点右上角 **账号** 修改。这只拦住浏览器管理界面；iPXE、`/ramos/`、`/images/` 和 Agent 接口不走网页登录。Agent 若启用了 `api_token`，仍在内核参数里带 Token。

**列表里没有品牌/型号/序列号**  
进 RAMOS 后 Agent 会读 DMI 自动上报。已配 **Redfish** 的机器可在「机器」里点「检测」，不进内存系统也能从 BMC 拉。仅 IPMI 时请先 PXE。点 **装机** 会跳到向导并选中该机。

**如何升级到新版本**  
不要重装、不要编译。按 [第 13 节](#13-升级控制面下载二进制覆盖) 下载 Release，覆盖 `rackauto`、`rackauto-agent` 和 `winpe-curl.exe`，重启控制面。Windows 装机缺 `winpe-curl.exe` 时 PE 里没有 curl。

**镜像要选系统和版本**  
Debian 12 和 Ubuntu 24.04 写网卡的方式不同（ifupdown / netplan），Rocky 8 和 9 也不一样（ifcfg / NetworkManager）。登记或上传时选对系统和版本，装机才会写入对应配置。Bond 和 VLAN（含 Bond 上的 VLAN）在装机向导第 3 步添加。自己用 KVM 导出的 qcow2 见 [第 11 节](#11-自己用-kvm-做装机镜像)。

**自己做的 qcow2 检测失败 / 装完没有 SSH**  
BIOS 必须是 **MBR、只有一个带启动标志的根分区**；UEFI 必须是 **EFI + 根分区**，ESP 里要有 `BOOTX64.EFI`。不要 swap、不要 LVM、不要单独 `/boot`。镜像里要安装并启用 **cloud-init**、**openssh-server**，以及扩容工具（Debian/Ubuntu：`cloud-guest-utils`；Rocky/Alma：`cloud-utils-growpart`）。步骤见 [第 11 节](#11-自己用-kvm-做装机镜像)。

**装完后网卡没地址 / 配置不生效**  
RAMOS 是 Ubuntu live，网卡名常是 `ens3` / `enp1s0`；Debian、Rocky 装完后往往是另一套名字。v0.4.18 起按 MAC 绑定并改名为 `nic0`、`nic1`。请换新的 `rackauto` 和 `rackauto-agent` 后重新 PXE 再装。

**`go build` 失败 / undefined: nfcSparseValues / 找不到 bin/rackauto**  
生产环境请用 Release，不要编译。若一定要编：

1. 先 `mkdir -p bin`，再 `go build -o bin/rackauto ./cmd/rackauto`（目录不存在会直接失败）。
2. 指定工具链，避开损坏的本机 Go 1.26：`export GOTOOLCHAIN=go1.25.3`
3. 国内拉模块：`export GOPROXY=https://goproxy.cn,direct`
4. 或直接 `make build` / `bash scripts/build.sh`

**解压 Release 后没有叫 rackauto 的文件**  
`v0.3.0` 包内是 `rackauto-linux-amd64` 和 `rackauto-agent-linux-amd64`。用 `tar -tzf` 看实际名字，再 `install` 到 `/opt/rackauto/bin/rackauto`。

**iPXE 报 Network unreachable（https://ipxe.org/28086011），随后反复出现 iPXE 画面**  
这是两件事叠在一起：

1. **网关不在 PXE 网段。** 例如地址是 `192.168.177.100/24`，网关却是示例里的 `10.0.0.1`。打开「网络引导」，重新选一次接入网卡（会把网关改成这块网卡的 IP），再点保存并应用。
2. **iPXE 循环。** 已经进入 iPXE 后应下发本机 TFTP 的 `boot.ipxe`，不能再给 `undionly.kpxe`。请更新控制面后重启，并把 `public_url` 改成 PXE 网段地址（例如 `http://192.168.177.1:8080`）。内核和脚本都从控制面拉取，不访问公网。

**PXE 一直 DHCP timeout**  
二层不通、选错接入网卡、或网段里另有 DHCP 在抢。抓包看 UDP 67/68。确认交换机没有 DHCP snooping 拦了控制面。

**能拿到地址，TFTP 失败 / PXE-E32**  
69/udp 没放行，或 `data/tftp` 里没有对应文件。再跑一次 `bootstrap`。部分网卡 UEFI 更吃 `snponly.efi`，可在现有 DHCP 里改 filename 试一下。

**iPXE 出现，但拉 boot.ipxe / 内核失败**  
`public_url` 写成了 `127.0.0.1`，或填了管理网 IP 而 PXE 网卡到不了。用服务器所在网段的地址重设并保存。

**Web 能开，左下角 `CTRL // NO LINK` 或 `OFFLINE`**  
API Token 填错，或页面不是从控制面自己的 HTTP 打开的（不要用 `file://`）。

**DHCP 应用失败：请指定接入网卡 / 绑定 67 失败**  
启用时必须选网卡。67 被 dhcpd/dnsmasq 占用时先停掉旧服务，或改用「方案二」。Linux 请用 root。

**PXE 进了内核随后 Kernel panic / Attempted to kill init**  
紫屏上的 `KERNEL PANIC! Attempted to kill init! exitcode=0x00000100` 是 casper 的 init 退出，不是硬件坏了。常见原因：

1. **旧版把整张 2.7GB ISO 拉进内存。** 请换成 **v0.4.2+**，再跑一次 `bootstrap`（已有 `live-server.iso` 不会重下，会抽出 squashfs 打成 `casper.iso`），然后重启 PXE。
2. **没重新 bootstrap。** 新控制面会请求 `/ramos/ubuntu/<arch>/casper.iso`，没有这个文件就会找不到 live 介质。
3. **机器内存太小。** 建议 ≥ 4GB；2GB 虚拟机仍可能在 wget/`tmpfs` 时 OOM。
4. **`public_url` 对 PXE 网不可达**，或 `casper.iso` 404。串口/屏幕上应能看到 casper 在 HTTP 拉 `casper.iso`。

不要用带分号的 `ds=nocloud-net;s=...`：iPXE 会把 `;` 当成命令分隔符，内核命令行被截断。v0.4.4 用 `autoinstall cloud-config-url=.../user-data`（无分号）让 Subiquity 跑 early-commands 拉起 Agent，而不是语言选择界面。

**PXE 进了 Ubuntu「Welcome / 选择语言」安装界面**  
这是 Subiquity 交互安装，不是 RAMOS。v0.4.3 及更早关掉了 cloud-init 又没带 `autoinstall`，安装器就会停在语言选择，Agent 也不会注册。请换成 **v0.4.4+** 控制面后重新 PXE（不必重下 ISO）。成功时屏幕应停在 early-commands / Agent 日志，控制台出现该 MAC。若仍进语言界面：确认 `public_url` 对机器可达，并 `curl http://<public_url>/ipxe/cidata/<mac>/user-data` 能返回 `autoinstall:`。

**进了 Ubuntu / RAMOS 但控制台没有机器**  
画面停在 `running /bin/bash /tmp/ramos.sh` 是正常的（early-commands 不能返回，否则安装器会继续）。但 Agent 应马上出现在控制台。

- 若一直只有这一行、机器还不出现：多半是旧脚本在启动 Agent 前同步跑 `apt-get` 卡住了。请用 **v0.4.5+**，重启控制面后再 PXE。屏幕上应出现 `download .../rackauto-agent` 和 `starting rackauto-agent`。
- `cannot download rackauto-agent`：把 Release 里的 `rackauto-agent` 拷到控制面 `data/agent/x86_64/rackauto-agent`（ARM 用 `aarch64`）。
- `register: unauthorized`：Agent 带的 `rackauto_token` 和控制面 `api_token` 不一致。网页登录账号拦不住 Agent；对一下配置和内核参数。
- 也可在安装器里切到 tty2（Alt+F2）看 `/var/log/rackauto.log`。

**装机写盘失败 / `qemu-img: executable file not found`**  
v0.4.5 为了先注册把 `apt-get` 放到后台，装机时可能还没装上 `qemu-utils`。请换成 **v0.4.6+** 的 `rackauto-agent`（拷到 `data/agent/x86_64/` 后重新 PXE）：Agent 会内置转换 qcow2，不依赖 `qemu-img`。权宜之计：在 RAMOS 里 `apt-get install -y qemu-utils` 后再从网页重发装机任务。

**装机写盘失败 / 镜像下不下来**  
镜像 URL 机器访问不了（HTTPS 证书、要代理）。改成控制面本地上传。确认类型选对（cloud 的 qcow2 选「云镜像」）。

**装完进不了系统 / UEFI 说硬盘上没有 EFI 文件**  
整盘 qcow2 从虚拟机拷到服务器后，机器固件**没有**虚拟机里的 NVRAM 启动项，只会找 `\EFI\BOOT\BOOTX64.EFI`。Debian/Rocky 安装器常常只把 grub 放在 `\EFI\debian` 或 `\EFI\rocky`，固件就认为 ESP 是空的。请换成 **v0.4.21+** 的 `rackauto` 和 `rackauto-agent`，重新 PXE 后再装一次：Agent 会把 shim/grub 拷到 `\EFI\BOOT\`，并尽量用 efibootmgr 写一条启动项。装机日志里应出现 `UEFI fallback` 和 `ESP file EFI/...`。BMC 下次启动改回 **磁盘 / UEFI**。若开了 Secure Boot，镜像里需要有 shim（官方 cloud 镜像一般有；自制镜像可先关 Secure Boot）。

**装完进不了系统**  
先看镜像页「引导」列：整盘云镜像需要能匹配向导里的固件。BIOS 盘要有 MBR 启动标志；UEFI 盘要有 ESP 和 EFI 加载器。自制镜像按 [第 11 节](#11-自己用-kvm-做装机镜像)。v0.4.10 起上传/检测会解析 GPT 和 EFI 文件；装机时不再用向导分区表覆盖镜像自带的 fstab。若仍起不来：BMC 把下次启动改回磁盘，确认机器固件模式和镜像匹配。

**误把内置 DHCP 开到办公网**  
立刻在「网络引导」点「停止 DHCP」，并在交换机上确认没有别人拿错地址。生产环境优先沿用现有 DHCP。

**Windows 装机进了 Ubuntu RAMOS**  
该 MAC 没有 pending/running 的 Windows 任务，或控制面还是旧版本。确认任务还在「等待 PXE 进入 Windows PE」，并升级到带 WinPE 的 Release 后再 PXE。

**Windows PE 一进就重启**  
官方 ISO 的 `boot.wim` 会启动 Windows 安装程序；找不到光盘就会立刻重启。请用 **v0.4.32+** 的 `rackauto`（不必重跑 bootstrap），任务仍在等待 WinPE 时再 PXE 一次。成功时任务日志里应很快出现 `winpe_started`。

**任务卡死（一直 pending / 等待 PXE / installing 不结束）**  
到「任务」页点删除。任何状态都可以删。删掉 pending/running 的 Windows 任务后，该 MAC 下次 PXE 不再进 WinPE；机器若还停在 installing/stressing 且没有别的进行中任务，会回到 ready。然后重新下发即可。请用 **v0.4.34+**。

**Windows PE 停在命令行，任务一直「等待 PXE 进入 Windows PE」**  
wimboot 只能把文件注到 `X:\Windows\System32`（文件名不能带路径）。旧版把 `startnet.cmd` 写错位置，PE 只跑了自带的 `wpeinit`，装机脚本没启动。请换成 **v0.4.32+** 后再 PXE。若已经停在 `X:\Windows\System32>`，可先执行 `install.cmd` 应急（仍建议升级后重来，旧脚本会去找 `X:\diskpart.txt`）。任务已经废了就先在网页里删掉再重发。

**DISM 错误 13「数据无效」**  
下载到的 `install.wim` 不是完整映像（以前会把 ISO 里偶然出现的 `MSWIM` 字节当成文件结尾，只下一小段）。请用 **v0.4.37+**，覆盖 `rackauto` 和 `winpe-curl.exe` 后**重新检测 ISO**、删掉卡死任务再 PXE。屏幕上应出现 `winpe-curl: wrote … bytes`，大小应是数 GB，不是几 MB。

**Windows PE 起来了但 install.wim 下不下来**  
`public_url` 对 PXE 网不可达，或 ISO 没有在控制面本地。WinPE 用注入的 `curl.exe` 拉 `/images/win/<id>/install.wim`。装机网必须有 DHCP（静态 IP 只在装完后生效）。控制面日志应有 `WinPE curl.exe ...`；浏览器打开 `http://<public_url>/winpe/curl.exe` 应能下载到文件而不是 404。

**Windows PE 报 `'curl.exe' 不是内部或外部命令` / `'certutil.exe' 不是内部或外部命令`**  
官方 boot.wim 本来就没有这些工具。请用 **v0.4.36+**，并把 Release 包里的 `winpe-curl.exe` 装到控制面 `rackauto` 同目录后重启。wimboot 会把它压成 `X:\Windows\System32\curl.exe`。任务废了就先在网页里删掉再重发。

**Windows 装完进不了系统 / 停在 bootmgr**  
向导固件必须和机器一致（UEFI 用 GPT + EFI 分区，BIOS 用 MBR）。看任务是否已经 `bcdboot` 成功。Secure Boot 一般可开（官方 ISO 的 WinPE/Windows 有签名）；若定制 boot.wim 被破坏则先关 Secure Boot。

**检测不到 WIM 版本 / 没有 boot.wim**  
把完整官方 ISO 传到控制面再点「检测」。只传 `install.wim` 时，需要同代 Windows Server ISO 先抽出过 `boot.wim`。`install.wim` >4GB 是正常的，不必先用 7z 手工解包。
