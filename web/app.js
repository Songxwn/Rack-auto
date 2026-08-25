const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const view = $("#view");
const titles = { dash: "总览", machines: "机器", images: "镜像", install: "装机向导", stress: "硬件压测", jobs: "任务", boot: "网络引导" };
let current = "dash";
let cache = { machines: [], images: [], jobs: [], events: [], overview: {} };

function token() { return $("#token").value.trim() || localStorage.getItem("rackauto_token") || ""; }
$("#token").value = localStorage.getItem("rackauto_token") || "";
$("#token").addEventListener("change", () => localStorage.setItem("rackauto_token", $("#token").value.trim()));

async function api(path, opts = {}) {
  const headers = { ...(opts.headers || {}) };
  if (!headers["Content-Type"] && !(opts.body instanceof FormData)) headers["Content-Type"] = "application/json";
  const t = token();
  if (t) headers["X-API-Token"] = t;
  const res = await fetch("/api/v1" + path, { ...opts, headers });
  const text = await res.text();
  if (!res.ok) throw new Error(text || res.statusText);
  return text ? JSON.parse(text) : null;
}

function badge(st) {
  const map = {
    discovered: "warn", ready: "ok", installing: "warn", stressing: "warn",
    provisioned: "ok", offline: "", error: "bad",
    pending: "warn", running: "warn", succeeded: "ok", failed: "bad", cancelled: "",
  };
  return `<span class="badge ${map[st] || ""}">${st || "-"}</span>`;
}
function fmtBytes(n) {
  if (!n) return "0";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0, x = n;
  while (x >= 1024 && i < u.length - 1) { x /= 1024; i++; }
  return x.toFixed(1) + " " + u[i];
}
function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function navTo(name) {
  current = name;
  $$("#nav button").forEach(b => b.classList.toggle("active", b.dataset.view === name));
  $("#title").textContent = titles[name];
  render();
}
$$("#nav button").forEach(b => b.addEventListener("click", () => navTo(b.dataset.view)));
$("#refresh").addEventListener("click", () => load().then(render));

async function load() {
  try {
    const [overview, machines, images, jobs, events, health] = await Promise.all([
      api("/overview"), api("/machines"), api("/images"), api("/jobs"), api("/events"),
      fetch("/api/v1/health").then(r => r.json()).catch(() => ({ ok: false })),
    ]);
    cache = { overview, machines, images, jobs, events };
    $("#health").textContent = health.ok ? "控制面 · 在线" : "控制面 · 离线";
  } catch (e) {
    $("#health").textContent = "控制面 · " + e.message;
  }
}

function render() {
  const fn = { dash: renderDash, machines: renderMachines, images: renderImages, install: renderInstall, stress: renderStress, jobs: renderJobs, boot: renderBoot }[current];
  fn();
}

function renderDash() {
  const o = cache.overview || {};
  view.innerHTML = `
    <div class="cards">
      <div class="card"><div class="k">机器</div><div class="v">${o.machines || 0}</div></div>
      <div class="card"><div class="k">在线 Agent</div><div class="v led">${o.online || 0}</div></div>
      <div class="card"><div class="k">镜像</div><div class="v">${o.images || 0}</div></div>
      <div class="card"><div class="k">运行中任务</div><div class="v">${o.running || 0}</div></div>
    </div>
    <div class="panel" style="margin-top:18px">
      <h3>最近事件</h3>
      ${(cache.events || []).map(e => `<div class="hint"><span class="mono">${escapeHtml(e.created_at)}</span> · ${escapeHtml(e.level)} · ${escapeHtml(e.message)}</div>`).join("") || "<div class='hint'>暂无事件</div>"}
    </div>`;
}

function renderMachines() {
  view.innerHTML = `
    <div class="actions" style="margin-bottom:12px">
      <button class="primary" id="add-m">登记机器 / BMC</button>
    </div>
    <div class="panel">
      <table>
        <thead><tr><th>名称</th><th>MAC / IP</th><th>状态</th><th>固件</th><th>BMC</th><th>硬件</th><th></th></tr></thead>
        <tbody>${(cache.machines || []).map(m => `
          <tr>
            <td>${escapeHtml(m.name)}<div class="hint mono">${escapeHtml(m.id)}</div></td>
            <td class="mono">${escapeHtml(m.mac)}<div>${escapeHtml(m.ip || "")}</div></td>
            <td>${badge(m.status)}<div class="hint">${escapeHtml(m.boot_mode || "")}</div></td>
            <td>${escapeHtml(m.firmware || "-")}</td>
            <td>${escapeHtml(m.bmc_type || "-")}<div class="hint">${escapeHtml(m.bmc_address || "")}</div></td>
            <td class="hint">${m.inventory ? `${m.inventory.cpus}C / ${m.inventory.memory_mb}MB / ${(m.inventory.disks||[]).length} disks` : "-"}</td>
            <td class="actions">
              <button data-act="detail" data-id="${m.id}">详情</button>
              <button data-act="pxe" data-id="${m.id}">PXE重启</button>
              <button data-act="on" data-id="${m.id}">开机</button>
              <button data-act="off" data-id="${m.id}">关机</button>
              <button data-act="cycle" data-id="${m.id}">重启</button>
            </td>
          </tr>`).join("")}
        </tbody>
      </table>
    </div>`;
  $("#add-m").onclick = () => machineForm();
  view.onclick = async (ev) => {
    const b = ev.target.closest("button[data-act]");
    if (!b) return;
    const id = b.dataset.id;
    try {
      if (b.dataset.act === "detail") return machineDetail(id);
      if (b.dataset.act === "pxe") await api(`/machines/${id}/pxe-install`, { method: "POST" });
      else await api(`/machines/${id}/power`, { method: "POST", body: JSON.stringify({ action: b.dataset.act }) });
      await load(); render();
    } catch (e) { alert(e.message); }
  };
}

function machineForm(m = {}) {
  openModal(`
    <h3>${m.id ? "编辑机器" : "登记机器"}</h3>
    <div class="row">
      <div><label>名称</label><input id="f-name" value="${escapeHtml(m.name || "")}"></div>
      <div><label>MAC</label><input id="f-mac" value="${escapeHtml(m.mac || "")}" placeholder="aa:bb:cc:dd:ee:ff"></div>
    </div>
    <div class="row3">
      <div><label>固件</label><select id="f-fw"><option value="uefi">UEFI</option><option value="bios">传统 BIOS</option></select></div>
      <div><label>引导模式</label><select id="f-boot"><option value="ramos">RAMOS</option><option value="pxe">PXE</option><option value="disk">本地磁盘</option></select></div>
      <div><label>BMC 协议</label><select id="f-bmc"><option value="ipmi">IPMI</option><option value="redfish">Redfish</option></select></div>
    </div>
    <div class="row3">
      <div><label>BMC 地址</label><input id="f-addr" value="${escapeHtml(m.bmc_address || "")}" placeholder="192.168.1.100 或 https://bmc/redfish/v1"></div>
      <div><label>端口</label><input id="f-port" value="${m.bmc_port || 623}"></div>
      <div><label>用户名</label><input id="f-user" value="${escapeHtml(m.bmc_username || "")}"></div>
    </div>
    <label>密码</label><input id="f-pass" type="password" placeholder="${m.id ? "留空则不修改" : ""}">
    <label><input type="checkbox" id="f-insecure" ${m.bmc_insecure ? "checked" : ""}> Redfish 跳过 TLS 校验</label>
    <div class="actions" style="margin-top:14px">
      <button class="primary" id="f-save">保存</button>
      <button class="ghost" id="f-close">取消</button>
      ${m.id ? `<button class="danger" id="f-del">删除</button>` : ""}
    </div>`);
  $("#f-fw").value = m.firmware || "uefi";
  $("#f-boot").value = m.boot_mode || "ramos";
  $("#f-bmc").value = m.bmc_type || "ipmi";
  $("#f-close").onclick = closeModal;
  $("#f-save").onclick = async () => {
    const body = {
      name: $("#f-name").value, mac: $("#f-mac").value, firmware: $("#f-fw").value, boot_mode: $("#f-boot").value,
      bmc_type: $("#f-bmc").value, bmc_address: $("#f-addr").value, bmc_port: Number($("#f-port").value || 0),
      bmc_username: $("#f-user").value, bmc_password: $("#f-pass").value, bmc_insecure: $("#f-insecure").checked,
      status: m.status || "ready",
    };
    try {
      if (m.id) await api("/machines/" + m.id, { method: "PUT", body: JSON.stringify({ ...m, ...body }) });
      else await api("/machines", { method: "POST", body: JSON.stringify(body) });
      closeModal(); await load(); render();
    } catch (e) { alert(e.message); }
  };
  const del = $("#f-del");
  if (del) del.onclick = async () => {
    if (!confirm("删除这台机器？")) return;
    await api("/machines/" + m.id, { method: "DELETE" });
    closeModal(); await load(); render();
  };
}

async function machineDetail(id) {
  const m = cache.machines.find(x => x.id === id);
  if (!m) return;
  const inv = m.inventory || {};
  openModal(`
    <h3>${escapeHtml(m.name)}</h3>
    <p class="hint mono">${escapeHtml(m.mac)} · ${escapeHtml(m.ip || "")} · agent ${escapeHtml(m.agent_version || "-")}</p>
    <div class="actions">
      <button id="ed">编辑 BMC</button>
      <button id="pxe">PXE 引导并重启</button>
      <button id="disk">下次从磁盘启动</button>
    </div>
    <h4>CPU / 内存</h4>
    <div class="hint">${escapeHtml(inv.cpu_model || "")} · ${inv.cpus || 0} 核 · ${inv.memory_mb || 0} MB · ${escapeHtml(inv.firmware || "")}</div>
    <h4>磁盘</h4>
    ${(inv.disks || []).map(d => `<div class="hint mono">${escapeHtml(d.path)} ${fmtBytes(d.size_b)} ${escapeHtml(d.model || "")}</div>`).join("") || "<div class='hint'>-</div>"}
    <h4>网卡</h4>
    ${(inv.nics || []).map(n => `<div class="hint mono">${escapeHtml(n.name)} ${escapeHtml(n.mac)} ${escapeHtml((n.ips||[]).join(", "))}</div>`).join("") || "<div class='hint'>-</div>"}
  `);
  $("#ed").onclick = () => machineForm(m);
  $("#pxe").onclick = async () => { await api(`/machines/${id}/pxe-install`, { method: "POST" }); closeModal(); };
  $("#disk").onclick = async () => {
    await api(`/machines/${id}/boot`, { method: "POST", body: JSON.stringify({ device: "disk", firmware: m.firmware, persistent: true }) });
    closeModal();
  };
}

function renderImages() {
  view.innerHTML = `
    <div class="row">
      <div class="panel">
        <h3>登记镜像 URL</h3>
        <label>名称</label><input id="i-name" placeholder="Ubuntu 24.04 cloud">
        <div class="row">
          <div><label>系统</label><select id="i-os"><option>ubuntu</option><option>debian</option><option>rocky</option><option>centos</option><option>custom</option></select></div>
          <div><label>类型</label><select id="i-kind">
            <option value="cloud-disk">云镜像（整盘 qcow2/raw）</option>
            <option value="cloud-root">根文件系统镜像</option>
            <option value="raw-disk">整盘 raw</option>
          </select></div>
        </div>
        <label>URL</label><input id="i-url" placeholder="https://...img 或 http://本平台/images/...">
        <div class="row"><div><label>SHA256</label><input id="i-sum"></div><div><label></label><button class="primary" id="i-add">登记</button></div></div>
      </div>
      <div class="panel">
        <h3>上传到控制面</h3>
        <p class="hint">大文件建议用 URL 登记。上传后由本机 HTTP 提供给 RAMOS。</p>
        <input type="file" id="i-file">
        <button class="primary" id="i-up" style="margin-top:12px">上传</button>
      </div>
    </div>
    <div class="panel" style="margin-top:14px">
      <table><thead><tr><th>名称</th><th>类型</th><th>大小</th><th>URL</th><th></th></tr></thead>
      <tbody>${(cache.images||[]).map(i => `<tr>
        <td>${escapeHtml(i.name)}<div class="hint">${escapeHtml(i.os_family||"")}</div></td>
        <td>${escapeHtml(i.kind)}</td><td>${fmtBytes(i.size_b)}</td>
        <td class="mono hint">${escapeHtml(i.url)}</td>
        <td><button class="danger" data-del="${i.id}">删除</button></td>
      </tr>`).join("")}</tbody></table>
    </div>`;
  $("#i-add").onclick = async () => {
    try {
      await api("/images", { method: "POST", body: JSON.stringify({
        name: $("#i-name").value, os_family: $("#i-os").value, kind: $("#i-kind").value,
        url: $("#i-url").value, checksum: $("#i-sum").value, checksum_type: "sha256",
      })});
      await load(); render();
    } catch (e) { alert(e.message); }
  };
  $("#i-up").onclick = async () => {
    const f = $("#i-file").files[0];
    if (!f) return alert("选择文件");
    const fd = new FormData();
    fd.append("file", f);
    fd.append("name", f.name);
    fd.append("kind", $("#i-kind").value);
    fd.append("os_family", $("#i-os").value);
    try {
      const headers = {};
      const t = token(); if (t) headers["X-API-Token"] = t;
      const res = await fetch("/api/v1/images/upload", { method: "POST", body: fd, headers });
      if (!res.ok) throw new Error(await res.text());
      await load(); render();
    } catch (e) { alert(e.message); }
  };
  view.onclick = async (ev) => {
    const id = ev.target.dataset.del;
    if (!id) return;
    if (!confirm("删除镜像？")) return;
    await api("/images/" + id, { method: "DELETE" });
    await load(); render();
  };
}

function defaultParts(fw) {
  if (fw === "bios") return [
    { name: "biosboot", size_mb: 1, fs: "biosboot", mount: "", flags: "bios_grub" },
    { name: "root", size_mb: 0, fs: "ext4", mount: "/", flags: "" },
  ];
  return [
    { name: "efi", size_mb: 512, fs: "vfat", mount: "/boot/efi", flags: "esp,boot" },
    { name: "root", size_mb: 0, fs: "ext4", mount: "/", flags: "" },
  ];
}

function renderInstall() {
  const machines = cache.machines || [];
  const images = cache.images || [];
  view.innerHTML = `
    <div class="panel">
      <div class="steps"><span class="on">1 机器/镜像</span><span>2 账号与密钥</span><span>3 磁盘与网卡</span></div>
      <div class="row">
        <div><label>机器</label><select id="in-m">${machines.map(m => `<option value="${m.id}">${escapeHtml(m.name)} (${escapeHtml(m.mac)})</option>`).join("")}</select></div>
        <div><label>镜像</label><select id="in-i">${images.map(i => `<option value="${i.id}">${escapeHtml(i.name)}</option>`).join("")}</select></div>
      </div>
      <div class="row3">
        <div><label>主机名</label><input id="in-host" placeholder="node-01"></div>
        <div><label>用户名</label><input id="in-user" value="ubuntu"></div>
        <div><label>固件</label><select id="in-fw"><option value="uefi">UEFI</option><option value="bios">传统 BIOS</option></select></div>
      </div>
      <div class="row">
        <div><label>登录密码</label><input id="in-pass" type="password"></div>
        <div><label>时区</label><input id="in-tz" value="Asia/Shanghai"></div>
      </div>
      <label>SSH 公钥（每行一个）</label>
      <textarea id="in-keys" placeholder="ssh-ed25519 AAAA..."></textarea>
      <label>目标磁盘（空则自动选最大盘）</label>
      <input id="in-disk" placeholder="/dev/sda 或 /dev/nvme0n1">
      <label>分区（size_mb=0 表示剩余空间）</label>
      <textarea id="in-parts"></textarea>
      <label>网卡 JSON</label>
      <textarea id="in-nics">{"nics":[{"name":"eth0","method":"dhcp"}]}</textarea>
      <p class="hint">method 可为 dhcp 或 static。静态示例：{"nics":[{"mac":"aa:bb:...","name":"eth0","method":"static","address":"10.0.0.20/24","gateway":"10.0.0.1","dns":["8.8.8.8"]}]}</p>
      <label><input type="checkbox" id="in-reboot" checked> 装完重启并切到本地磁盘引导</label>
      <div class="actions" style="margin-top:14px">
        <button class="primary" id="in-go">下发装机任务</button>
        <button id="in-pxe">同时 BMC PXE 重启</button>
      </div>
    </div>`;
  const syncParts = () => { $("#in-parts").value = JSON.stringify(defaultParts($("#in-fw").value), null, 2); };
  $("#in-fw").onchange = syncParts; syncParts();
  const go = async (pxe) => {
    const m = $("#in-m").value, img = $("#in-i").value;
    if (!m || !img) return alert("需要机器和镜像");
    let partitions, network;
    try { partitions = JSON.parse($("#in-parts").value); network = JSON.parse($("#in-nics").value); }
    catch (e) { return alert("分区或网卡 JSON 无效: " + e.message); }
    const body = {
      machine_id: m, image_id: img, hostname: $("#in-host").value, username: $("#in-user").value,
      password: $("#in-pass").value, timezone: $("#in-tz").value, firmware: $("#in-fw").value,
      ssh_keys: $("#in-keys").value.split("\n").map(s => s.trim()).filter(Boolean),
      disk: $("#in-disk").value, partitions, network, reboot: $("#in-reboot").checked,
    };
    try {
      await api("/jobs/install", { method: "POST", body: JSON.stringify(body) });
      if (pxe) await api(`/machines/${m}/pxe-install`, { method: "POST" });
      navTo("jobs"); await load(); render();
    } catch (e) { alert(e.message); }
  };
  $("#in-go").onclick = () => go(false);
  $("#in-pxe").onclick = () => go(true);
}

function renderStress() {
  view.innerHTML = `
    <div class="panel">
      <p class="hint">在 RAMOS 内存系统中对 CPU、内存、磁盘、到控制面的网络做压测。机器需已 PXE 进入 Agent。</p>
      <label>机器</label>
      <select id="st-m">${(cache.machines||[]).map(m => `<option value="${m.id}">${escapeHtml(m.name)}</option>`).join("")}</select>
      <div class="row3" style="margin-top:8px">
        <label><input type="checkbox" class="tg" value="cpu" checked> CPU</label>
        <label><input type="checkbox" class="tg" value="memory" checked> 内存</label>
        <label><input type="checkbox" class="tg" value="disk" checked> 硬盘</label>
        <label><input type="checkbox" class="tg" value="network" checked> 网络</label>
      </div>
      <div class="row3">
        <div><label>时长（秒）</label><input id="st-d" value="60"></div>
        <div><label>CPU 线程（0=全部）</label><input id="st-c" value="0"></div>
        <div><label>内存占用 %</label><input id="st-mem" value="50"></div>
      </div>
      <div class="row">
        <div><label>磁盘测试文件</label><input id="st-path" placeholder="/tmp/stress.bin"></div>
        <div><label>磁盘测试大小 MB</label><input id="st-ds" value="512"></div>
      </div>
      <button class="primary" id="st-go" style="margin-top:12px">开始压测</button>
    </div>`;
  $("#st-go").onclick = async () => {
    const targets = $$(".tg:checked").map(x => x.value);
    try {
      await api("/jobs/stress", { method: "POST", body: JSON.stringify({
        machine_id: $("#st-m").value, targets, duration_sec: Number($("#st-d").value),
        cpu_workers: Number($("#st-c").value), memory_percent: Number($("#st-mem").value),
        disk_path: $("#st-path").value, disk_size_mb: Number($("#st-ds").value),
      })});
      navTo("jobs"); await load(); render();
    } catch (e) { alert(e.message); }
  };
}

function renderJobs() {
  view.innerHTML = `
    <div class="panel">
      <table>
        <thead><tr><th>任务</th><th>机器</th><th>状态</th><th>进度</th><th>信息</th><th></th></tr></thead>
        <tbody>${(cache.jobs||[]).map(j => `<tr>
          <td>${escapeHtml(j.type)}<div class="hint mono">${escapeHtml(j.id)}</div></td>
          <td class="mono">${escapeHtml(j.machine_id)}</td>
          <td>${badge(j.status)}</td>
          <td>${j.progress || 0}%</td>
          <td>${escapeHtml(j.message || "")}</td>
          <td><button data-j="${j.id}">日志</button></td>
        </tr>`).join("")}</tbody>
      </table>
    </div>`;
  view.onclick = async (ev) => {
    const id = ev.target.dataset.j;
    if (!id) return;
    const j = await api("/jobs/" + id);
    openModal(`<h3>${escapeHtml(j.type)} ${badge(j.status)}</h3>
      <p class="hint">${escapeHtml(j.message || "")}</p>
      ${j.result ? `<pre class="log">${escapeHtml(JSON.stringify(j.result, null, 2))}</pre>` : ""}
      <pre class="log">${escapeHtml(j.logs || "暂无日志")}</pre>`);
  };
}

async function renderBoot() {
  let settings = {};
  try { settings = await api("/settings"); } catch {}
  view.innerHTML = `
    <div class="panel">
      <h3>控制面地址</h3>
      <p class="hint">iPXE / RAMOS 必须能访问这个 URL。请填物理机或交换机可达的地址，不要用 127.0.0.1。</p>
      <div class="row">
        <div><label>Public URL</label><input id="b-url" value="${escapeHtml(settings.public_url || "")}"></div>
        <div><label>API Token</label><input id="b-tok" type="password" placeholder="留空不修改"></div>
      </div>
      <button class="primary" id="b-save" style="margin-top:10px">保存</button>
      <h3>现有 DHCP 配置片段</h3>
      <p class="hint">BIOS 用 undionly.kpxe，UEFI 用 ipxe.efi。iPXE 会 chainload 到下方脚本。</p>
      <pre class="log"># ISC dhcpd
next-server ${escapeHtml((settings.public_url || "http://10.0.0.1:8080").replace(/^https?:\/\//,"").split(":")[0])};
if option client-arch != 00:00 { filename "ipxe.efi"; } else { filename "undionly.kpxe"; }

# dnsmasq
dhcp-match=set:efi64,option:client-arch,7
dhcp-boot=tag:efi64,ipxe.efi
dhcp-boot=undionly.kpxe
dhcp-option=66,${escapeHtml((settings.public_url || "").replace(/^https?:\/\//,"").split(":")[0])}

# iPXE 嵌入脚本 / 手工
dhcp
chain ${escapeHtml(settings.public_url || "http://10.0.0.1:8080")}/ipxe/boot.ipxe
</pre>
      <p class="hint">内置 DHCP：${settings.dhcp_enabled ? "已启用" : "未启用"} · TFTP ${escapeHtml(settings.tftp_listen || "")}</p>
      <p class="hint">首次部署请在控制面主机执行 <span class="mono">rackauto bootstrap</span>，下载 iPXE、Alpine RAMOS 内核并编译 Linux Agent。</p>
    </div>`;
  $("#b-save").onclick = async () => {
    try {
      await api("/settings", { method: "PUT", body: JSON.stringify({ public_url: $("#b-url").value, api_token: $("#b-tok").value }) });
      alert("已保存");
    } catch (e) { alert(e.message); }
  };
}

function openModal(html) {
  const m = $("#modal");
  m.classList.remove("hidden");
  m.innerHTML = `<div class="sheet">${html}<div class="actions" style="margin-top:12px"><button class="ghost" id="modal-x">关闭</button></div></div>`;
  $("#modal-x").onclick = closeModal;
  m.onclick = (e) => { if (e.target === m) closeModal(); };
}
function closeModal() { $("#modal").classList.add("hidden"); $("#modal").innerHTML = ""; }

load().then(render);
setInterval(() => { load().then(() => { if (["dash","jobs","machines"].includes(current)) render(); }); }, 8000);
