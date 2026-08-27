# UEFI：进 EFI Internal Shell，手工配地址引导进 Rack-auto

DHCP / 自动 PXE 不可用时（拿不到地址、选错网卡、机房 DHCP 不给引导文件），可以进 **UEFI EFI Internal Shell**，再进 **iPXE**，**手工写 IP**，用 HTTP 拉控制面菜单。

BIOS 机器没有 EFI Shell，请用网卡 PXE 或 U 盘里的 `undionly.kpxe`。本文只讲 **UEFI**。

---

## 你需要准备什么

| 项 | 说明 |
|----|------|
| 控制面地址 | PXE 网段上能访问的 `public_url`，例如 `http://192.168.177.1:8080` |
| 本机临时 IP | 与控制面同网段、未被占用，例如 `192.168.177.50` |
| 子网掩码 / 网关 | 例如 `255.255.255.0`，网关一般是控制面或交换机网关 |
| `ipxe.efi` | 控制面 `data/tftp/ipxe.efi`（跑过 `rackauto bootstrap` 或启动过 `serve` 就会有） |
| U 盘（推荐） | FAT32，把 `ipxe.efi` 拷到根目录 |

把下面例子里的地址换成你的环境：

```
控制面: 192.168.177.1:8080
本机:   192.168.177.50 / 255.255.255.0
网关:   192.168.177.1
```

浏览器先确认能打开：

```
http://192.168.177.1:8080/ipxe/boot.ipxe
```

能看到一段 `#ipxe` 脚本即可。

---

## 一、进入 EFI Internal Shell

不同品牌按键不同，常见：

| 品牌 | 进启动菜单 / Setup | Shell 入口 |
|------|-------------------|------------|
| Dell | F12 / F2 | Boot Menu → **EFI Internal Shell** / UEFI Shell |
| HP | F9 / F10 | Boot Option → **Internal Shell** / Embedded UEFI Shell |
| Supermicro / 通用 | F11 / Del / Esc | Boot Override → **UEFI: Built-in EFI Shell** |
| 部分主板 | 先进 Setup | Boot → 打开 **UEFI Shell** / **Internal EFI Shell** |

注意：

1. 固件模式选 **UEFI**，不要 Legacy/CSM。
2. Secure Boot 若拦第三方 `ipxe.efi`，先临时关闭。
3. 若列表里没有 Shell：Setup → Boot 里启用 **EFI Shell**，或从 U 盘启动带 `Shell.efi` 的 EDK2 工具盘。

进 Shell 后大致是：

```
UEFI Interactive Shell v2.x
...
Shell>
```

---

## 二、从 U 盘加载 iPXE（推荐）

1. 把控制面上的 `data/tftp/ipxe.efi` 拷到 FAT32 U 盘根目录，插到待装机服务器。
2. 在 Shell 里刷新盘符：

```
map -r
```

3. 看盘符（常见 `fs0:`、`fs1:`）：

```
map
```

4. 进入 U 盘并确认文件：

```
fs0:
ls
```

看到 `ipxe.efi` 后执行：

```
ipxe.efi
```

若报错，试：

```
\ipxe.efi
```

或换盘符 `fs1:` 再试。

成功后会出现 **iPXE** 提示符（或短暂自动 DHCP，可立刻按 **Ctrl-B** 打断进命令行）：

```
iPXE>
```

接着做下一节「手工配地址」。

---

## 三、在 iPXE 里手工配地址并 chain

在 `iPXE>` 下执行（按你的网段改数字）：

```
ifclose
set net0/ip 192.168.177.50
set net0/netmask 255.255.255.0
set net0/gateway 192.168.177.1
ifopen net0
```

可选：多块网卡时先看接口：

```
ifstat
```

若要用 `net1`，把上面的 `net0` 改成 `net1`。

测连通（有的固件支持）：

```
ping 192.168.177.1
```

拉控制面菜单：

```
chain http://192.168.177.1:8080/ipxe/boot.ipxe
```

成功后应进入 Rack-auto 的 iPXE 菜单 / 自动按 MAC 进 RAMOS 或 WinPE。

---

## 四、已经进了 iPXE（自动 PXE 成功）但 HTTP 错了

网卡 PXE 已经加载了 `ipxe.efi`，只是 DHCP 给的地址或 `public_url` 不对时：

1. 开机看到 iPXE 画面时连按 **Ctrl-B**，进入 `iPXE>`。
2. 直接做[第三节](#三在-ipxe-里手工配地址并-chain)的 `set net0/ip` … `chain http://...`。

不必再进 EFI Shell。

---

## 五、可选：在 EFI Shell 里先配静态 IP（排查用）

部分机器的 Shell 带网络命令，可先确认二层通不通：

```
ifconfig -l
ifconfig -s eth0 static 192.168.177.50 255.255.255.0 192.168.177.1
ping 192.168.177.1
```

接口名可能是 `eth0`、`eth1` 或别的，以 `ifconfig -l` 为准。  
Shell 一般**不能**直接 `chain` 控制面脚本，配完网仍建议[第二节](#二从-u-盘加载-ipxe推荐)加载 `ipxe.efi`，再在 iPXE 里 `chain`。

---

## 六、一条龙命令备忘（复制后改地址）

```
# --- 在 iPXE> 下 ---
ifclose
set net0/ip 192.168.177.50
set net0/netmask 255.255.255.0
set net0/gateway 192.168.177.1
ifopen net0
chain http://192.168.177.1:8080/ipxe/boot.ipxe
```

ARM64 服务器请用控制面 TFTP 里的 `ipxe-arm64.efi`（若有），步骤相同，U 盘文件名改成对应文件。

---

## 常见问题

**Shell 里找不到 U 盘 / `map` 没有 fs0**  
换 USB 口（尽量主板后置）、格式化成 FAT32（不要 exFAT）、再执行 `map -r`。

**`ipxe.efi` 启动被 Secure Boot 拒绝**  
Setup 里临时关闭 Secure Boot，或使用已签名的启动链（机房策略允许时再开）。

**`chain` 失败 / Connection refused / No such file**  
- 控制面是否在跑、`public_url` 端口是否对（默认 `8080`）  
- 本机 IP 是否与控制面同网段、网关是否在同网段（错网关会 `Network unreachable`）  
- 浏览器从别的机器访问 `http://<控制面>:8080/ipxe/boot.ipxe` 是否通  

**多网卡时总不通**  
`ifstat` 看哪个口有链路，对那个口 `ifopen` / 改 `net0`→`net1`，并把网线插在装机用的那块卡上。

**进了菜单又退回 / 循环**  
控制面地址要用 **PXE 网段 IP**，不要用 `127.0.0.1` 或管理网不可达地址。装机任务需按该机 **MAC** 绑定。

---

## 和自动 PXE 的关系

| 方式 | 何时用 |
|------|--------|
| 内置 / 现有 DHCP + TFTP | 正常装机，见 [deploy.md §9](deploy.md#9-配置-dhcp) |
| 本文：Shell → iPXE → 手工 IP | DHCP 坏了、临时验证控制面、或只想指定地址 |

手工引导成功后，说明 HTTP / iPXE / 镜像链路正常；再回去修 DHCP 或「仅响应 PXE」等配置即可恢复全自动。
