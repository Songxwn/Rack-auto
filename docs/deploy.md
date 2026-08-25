# 部署 Rack-auto

这份教程按「第一次把机器装起来」来写。你可以先扫一遍目录，再按自己的环境跳到对应章节。

做完之后，你应该能：在浏览器打开控制台 → 服务器 PXE 进内存系统（RAMOS）→ 下发镜像装机。

## 目录

1. [先看清整条链路](#1-先看清整条链路)
2. [你需要准备什么](#2-你需要准备什么)
3. [推荐网络怎么接](#3-推荐网络怎么接)
4. [安装控制面](#4-安装控制面)
5. [写配置（最容易写错的地方）](#5-写配置最容易写错的地方)
6. [bootstrap：下载引导文件](#6-bootstrap下载引导文件)
7. [启动服务](#7-启动服务)
8. [配置 DHCP](#8-配置-dhcp)
9. [第一次装机](#9-第一次装机)
10. [用 systemd 长期跑](#10-用-systemd-长期跑)
11. [Docker 部署](#11-docker-部署)
12. [自检清单](#12-自检清单)
13. [常见问题](#13-常见问题)

---

## 1. 先看清整条链路

待装机服务器并不是直接去下操作系统 ISO，而是：

```
服务器开机（PXE）
    → DHCP 告诉它：TFTP 在哪、引导文件叫什么
    → TFTP 下载 undionly.kpxe（传统 BIOS）或 ipxe.efi（UEFI）
    → iPXE 再通过 HTTP 找控制面：/ipxe/boot.ipxe
    → 进入 RAMOS（内存里的 Alpine + Agent）
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
| 出网 | 第一次 `bootstrap` 要下载 iPXE 和 Alpine 内核（约几十到上百 MB）。 |
| 管理员权限 | 绑定 DHCP/TFTP 特权端口需要 root（或等价能力）。 |

**不需要**事先给每台机器装操作系统。有 BMC 的话，连显示器都可以不用。

控制面磁盘建议预留空间：引导文件不大，**镜像**才占地方（一张 Ubuntu cloud 镜像大约 600MB～1GB，按你要存几份算）。

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

### A. 用 GitHub Release 二进制（推荐）

到 [Releases](https://github.com/Songxwn/Rack-auto/releases) 下载对应平台的包，例如 Linux x86_64：

```bash
sudo mkdir -p /opt/rackauto/{bin,configs,data}
cd /tmp
curl -fLO https://github.com/Songxwn/Rack-auto/releases/latest/download/rackauto-linux-amd64.tar.gz
tar -tzf rackauto-linux-amd64.tar.gz   # 里面一般是 rackauto-linux-amd64、rackauto-agent-linux-amd64
tar -xzf rackauto-linux-amd64.tar.gz
sudo install -m 0755 rackauto-linux-amd64 /opt/rackauto/bin/rackauto
sudo mkdir -p /opt/rackauto/data/agent/x86_64
sudo install -m 0755 rackauto-agent-linux-amd64 /opt/rackauto/data/agent/x86_64/rackauto-agent
```

ARM 控制面把上面的 `amd64` 换成 `arm64`，Agent 目录用 `data/agent/aarch64/`。Windows 控制面只能预览 Web，PXE 请换 Linux。

Release 里的 Agent 只覆盖**当前这个 CPU 架构**。若控制面是 x86_64、还要给 ARM 服务器装机，再下一份 `rackauto-linux-arm64.tar.gz`，把其中的 `rackauto-agent-linux-arm64` 放到 `data/agent/aarch64/rackauto-agent`。

从**源码**执行 `bootstrap` 会自动交叉编译两个架构，不必手工拷。

### B. 从源码编译

需要 [Go 1.25+](https://go.dev/dl/)。

```bash
git clone https://github.com/Songxwn/Rack-auto.git
cd Rack-auto
go build -o bin/rackauto ./cmd/rackauto
```

后面的配置文件可以用仓库里的 `configs/rackauto.example.yaml`。`bootstrap` 会顺带交叉编译 Linux Agent。

### C. Docker

见 [第 11 节](#11-docker-部署)。镜像里已经带好 Linux Agent，仍需要执行一次 `bootstrap` 下载 iPXE / Alpine。

---

## 5. 写配置（最容易写错的地方）

```bash
# 源码目录里：
cp configs/rackauto.example.yaml configs/rackauto.yaml

# 或 Release 安装到 /opt/rackauto 时：
sudo cp configs/rackauto.example.yaml /opt/rackauto/configs/rackauto.yaml
# （若没有 clone，可从仓库复制 example：https://github.com/Songxwn/Rack-auto/blob/main/configs/rackauto.example.yaml）
```

用编辑器打开，**至少改这两项**：

```yaml
listen: ":8080"
public_url: "http://10.0.0.50:8080"   # 改成控制面在「装机网」上的地址
data_dir: "./data"                    # /opt 安装建议改成 /opt/rackauto/data
api_token: "请换成一段随机字符串"       # 建议打开；Web 右上角 Token 填同一个
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

## 6. bootstrap：下载引导文件

控制面第一次启动前执行一次（需要访问 `boot.ipxe.org` 和 `dl-cdn.alpinelinux.org`）：

```bash
# 源码目录
./bin/rackauto bootstrap -config configs/rackauto.yaml

# /opt 安装
sudo /opt/rackauto/bin/rackauto bootstrap \
  -config /opt/rackauto/configs/rackauto.yaml \
  -data-dir /opt/rackauto/data
```

它会做三件事：

1. 把 `undionly.kpxe`、`ipxe.efi`、`snponly.efi` 放到 `data/tftp/`
2. 把 Alpine LTS 内核 / initramfs / modloop 放到 `data/ramos/x86_64` 与 `data/ramos/aarch64`
3. 若当前目录能找到源码，再交叉编译 Linux Agent 到 `data/agent/`

已经存在且大于 1KB 的文件会跳过，可以重复执行。某次下载失败时，日志会打 `!`，修好网络后再跑一遍即可。

没有 Go、又没从源码 bootstrap 时，请从对应架构的 Release 包拷入 Agent（文件名带平台后缀）：

```bash
sudo mkdir -p /opt/rackauto/data/agent/x86_64 /opt/rackauto/data/agent/aarch64
sudo install -m 0755 rackauto-agent-linux-amd64 /opt/rackauto/data/agent/x86_64/rackauto-agent
sudo install -m 0755 rackauto-agent-linux-arm64 /opt/rackauto/data/agent/aarch64/rackauto-agent
```

---

## 7. 启动服务

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

本机浏览器打开 `http://控制面IP:8080`。若配置了 `api_token`，在右上角 **API TOKEN** 里填同一串，失焦即会记住。左下角变成 `CTRL // ONLINE` 就对了：

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

## 8. 配置 DHCP

打开控制台 **07 网络引导**。上方「控制面地址」应与 `public_url` 一致，不对就改完点保存。

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
if option client-arch != 00:00 {
  filename "ipxe.efi";
} else {
  filename "undionly.kpxe";
}
```

dnsmasq 示例：

```
dhcp-match=set:efi64,option:client-arch,7
dhcp-boot=tag:efi64,ipxe.efi,,10.0.0.50
dhcp-boot=undionly.kpxe,,10.0.0.50
```

iPXE 自己 chainload 时：

```
dhcp
chain http://10.0.0.50:8080/ipxe/boot.ipxe
```

Web 同一页底部会按你当前的 `public_url` 生成一份可复制片段。

---

## 9. 第一次装机

建议第一次用一台**可以重装**的机器，磁盘会被覆盖。

### 9.1 登记机器（两种方式）

**有 BMC：** 打开「机器」→「登记机器 / BMC」。填名称、MAC（PXE 那块网卡）、固件（UEFI 或 BIOS）、BMC 协议与地址。保存后可用「开机 / PXE重启」试一下。

**没有 BMC：** 到机柜把服务器设为「网卡 PXE 启动」，直接开机。进 RAMOS 后，Agent 会向控制面报到，机器列表里会出现它。

### 9.2 准备镜像

「镜像」页可以：

- **登记 URL**（推荐）：例如 Ubuntu 24.04 cloud  
  `https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img`  
  类型选「云镜像（整盘 qcow2/raw）」
- **上传文件**：大文件更建议 URL，让 RAMOS 自己去拉

待装机服务器也要能访问这个 URL；若只有内网，把镜像传到控制面「上传」，URL 会变成 `http://<public_url>/images/...`。

### 9.3 下发任务

打开「装机」：

1. 选机器和镜像
2. 主机名、用户名、登录密码、SSH 公钥（强烈建议填公钥，密码可作兜底）
3. 固件与分区一般保持默认（UEFI：ESP + 根分区；BIOS：bios_grub + 根分区）
4. 网卡 JSON 默认 DHCP 即可；静态地址示例：

```json
{"nics":[{"name":"eth0","method":"static","address":"10.0.0.20/24","gateway":"10.0.0.1","dns":["8.8.8.8"]}]}
```

5. 点「下发装机任务」。有 BMC 可同时点「BMC PXE 重启」

到「任务」看进度和日志。成功后机器会切回本地磁盘重启。用你填的用户 + 密钥/密码 SSH 进去，即告完成。

### 9.4 没有 PXE 时的调试

在已经能进系统的 Linux 上（不要在生产盘上乱试）可以手工跑 Agent：

```bash
./rackauto-agent --url http://10.0.0.50:8080 --token <api_token>
```

---

## 10. 用 systemd 长期跑

仓库提供了 [deploy/rackauto.service](../deploy/rackauto.service) 示例。

```bash
sudo cp deploy/rackauto.service /etc/systemd/system/rackauto.service
# 按实际路径编辑 ExecStart、WorkingDirectory
sudo systemctl daemon-reload
sudo systemctl enable --now rackauto
sudo journalctl -u rackauto -f
```

---

## 11. Docker 部署

Compose 使用 `network_mode: host`，这样 DHCP/TFTP 才能绑网卡。请先在仓库根目录准备好配置：

```bash
cp configs/rackauto.example.yaml configs/rackauto.yaml
# 编辑 public_url、data_dir 可保持默认，容器内会用 -data-dir /var/lib/rackauto
cd deploy
docker compose up -d --build
docker compose exec rackauto rackauto bootstrap -config /etc/rackauto.yaml -data-dir /var/lib/rackauto
```

之后浏览器访问宿主机 `:8080`。内置 DHCP 仍要在 Web 里选**宿主机**网卡名（host 网络下看到的就是宿主机网卡）。

---

## 12. 自检清单

按顺序打勾，卡在哪一步就去下一节对号入座。

- [ ] `curl -sS http://<public_url>/api/v1/health` 返回正常（从**另一台机器**测，不要只在控制面本机测 `127.0.0.1`）
- [ ] `ls data/tftp/` 里有 `undionly.kpxe` 和 `ipxe.efi`
- [ ] `ls data/ramos/x86_64/` 里有 `vmlinuz-lts`、`initramfs-lts`
- [ ] `ls data/agent/x86_64/rackauto-agent` 文件存在且可执行
- [ ] 控制台左下角是 `CTRL // ONLINE`
- [ ] DHCP：要么内置显示运行中，要么现有 dhcpd/dnsmasq 已改 next-server
- [ ] 服务器 PXE 后能看到 iPXE，而不是一直 `DHCP...`
- [ ] 机器列表出现该节点，或你已手工登记 MAC
- [ ] 装机任务日志里能看到写盘、注入用户，而不是下载内核 404

---

## 13. 常见问题

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

**进了 Alpine / RAMOS 但控制台没有机器**  
Agent 连不上 `public_url`，或 Token 不一致。看 RAMOS 控制台滚动日志。

**装机写盘失败 / qemu-img**  
镜像 URL 机器访问不了（HTTPS 证书、要代理）。改成控制面本地上传。确认类型选对（cloud 的 qcow2 选「云镜像」）。

**装完进不了系统**  
固件选错（机器是 BIOS 却按 UEFI 分区，或反过来）。BMC 里把下次启动改回磁盘后再试。

**误把内置 DHCP 开到办公网**  
立刻在「网络引导」点「停止 DHCP」，并在交换机上确认没有别人拿错地址。生产环境优先沿用现有 DHCP。
