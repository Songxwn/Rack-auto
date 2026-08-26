const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const view = $("#view");
const titles = { dash: "总览", machines: "机器", images: "镜像", install: "装机向导", stress: "硬件压测", jobs: "任务", boot: "网络引导" };
const kickers = {
  dash: "CONTROL / OVERVIEW",
  machines: "INVENTORY / NODES",
  images: "STORAGE / IMAGES",
  install: "PROVISION / WIZARD",
  stress: "DIAG / STRESS",
  jobs: "PIPELINE / JOBS",
  boot: "NETBOOT / DHCP",
};
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
  $("#kicker").textContent = kickers[name] || "";
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
    setHealth(health.ok, health.ok ? "CTRL // ONLINE" : "CTRL // OFFLINE");
    if (health.version) setVersion(health.version);
  } catch (e) {
    setHealth(false, "CTRL // NO LINK");
  }
}

function setHealth(ok, text) {
  $("#health").textContent = text;
  const led = $("#health-led");
  if (!led) return;
  led.classList.toggle("on", !!ok);
  led.classList.toggle("off", !ok);
}

function setVersion(v) {
  const t = (v && String(v).trim()) || "dev";
  const side = $("#ctrl-ver");
  const head = $("#app-ver");
  if (side) side.textContent = t;
  if (head) head.textContent = t;
  document.title = "Rack-auto " + t + " · 裸金属装机";
}

async function removeMachine(id, label) {
  const name = label || id;
  if (!confirm("删除机器「" + name + "」？其任务记录也会一并删除。")) return false;
  await api("/machines/" + id, { method: "DELETE" });
  closeModal();
  await load();
  render();
  return true;
}

function render() {
  view.onclick = null;
  const fn = { dash: renderDash, machines: renderMachines, images: renderImages, install: renderInstall, stress: renderStress, jobs: renderJobs, boot: renderBoot }[current];
  view.classList.remove("in");
  fn();
  void view.offsetWidth;
  view.classList.add("in");
}

function renderDash() {
  const o = cache.overview || {};
  view.innerHTML = `
    <div class="cards">
      <div class="card"><div class="k">NODES</div><div class="v">${o.machines || 0}</div><div class="bar"></div></div>
      <div class="card"><div class="k">AGENTS ONLINE</div><div class="v led-num">${o.online || 0}</div><div class="bar"></div></div>
      <div class="card"><div class="k">IMAGES</div><div class="v">${o.images || 0}</div><div class="bar"></div></div>
      <div class="card"><div class="k">JOBS LIVE</div><div class="v">${o.running || 0}</div><div class="bar"></div></div>
    </div>
    <div class="panel hud-strip">
      <div>DHCP UPLINK　${o.dhcp_running ? `<span class="badge ok">LIVE · ${escapeHtml(o.dhcp_interface || "")}</span>` : `<span class="badge">STANDBY</span>`}</div>
      <button class="ghost" id="go-boot">打开 DHCP</button>
    </div>
    <div class="panel telemetry">
      <h3>TELEMETRY</h3>
      ${(cache.events || []).map(e => `<div class="event"><span class="t mono">${escapeHtml(e.created_at)}</span><span class="badge ${e.level === "error" ? "bad" : e.level === "warn" ? "warn" : "ok"}">${escapeHtml(e.level)}</span><span>${escapeHtml(e.message)}</span></div>`).join("") || "<div class='empty'>NO SIGNAL · 等待节点上报</div>"}
    </div>`;
  $("#go-boot").onclick = () => navTo("boot");
}

function renderMachines() {
  view.innerHTML = `
    <div class="actions" style="margin-bottom:12px">
      <button class="primary" id="add-m">登记机器 / BMC</button>
    </div>
    <div class="panel">
      <table>
        <thead><tr><th>名称</th><th>MAC / IP</th><th>状态</th><th>固件</th><th>BMC</th><th>硬件</th><th></th></tr></thead>
        <tbody>${(cache.machines || []).length ? (cache.machines || []).map(m => `
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
              <button class="danger" data-act="delete" data-id="${m.id}" data-name="${escapeHtml(m.name || m.mac || m.id)}">删除</button>
            </td>
          </tr>`).join("") : `<tr><td colspan="7" class="empty">NO NODES · 尚未登记机器</td></tr>`}
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
      if (b.dataset.act === "delete") {
        await removeMachine(id, b.dataset.name || id);
        return;
      }
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
    try { await removeMachine(m.id, m.name || m.mac || m.id); }
    catch (e) { alert(e.message); }
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
      <button class="danger" id="md-del">删除机器</button>
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
  $("#md-del").onclick = async () => {
    try { await removeMachine(id, m.name || m.mac || id); }
    catch (e) { alert(e.message); }
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
        <p class="hint">大文件建议用 URL 登记。上传到本机后会检测分区表和 UEFI/BIOS 引导。</p>
        <input type="file" id="i-file">
        <button class="primary" id="i-up" style="margin-top:12px">上传</button>
      </div>
    </div>
    <div class="panel" style="margin-top:14px">
      <table><thead><tr><th>名称</th><th>类型</th><th>大小</th><th>引导</th><th>URL</th><th></th></tr></thead>
      <tbody>${(cache.images||[]).length ? (cache.images||[]).map(i => `<tr>
        <td>${escapeHtml(i.name)}<div class="hint">${escapeHtml(i.os_family||"")}</div></td>
        <td>${escapeHtml(i.kind)}</td><td>${fmtBytes(i.size_b || (i.inspect && i.inspect.virtual_size_b) || 0)}</td>
        <td>${inspectBadge(i)}<div class="hint">${escapeHtml((i.inspect && i.inspect.message) || "")}</div></td>
        <td class="mono hint">${escapeHtml(i.url)}</td>
        <td class="actions">
          <button data-inspect="${i.id}">检测</button>
          <button class="danger" data-del="${i.id}">删除</button>
        </td>
      </tr>`).join("") : `<tr><td colspan="6" class="empty">NO IMAGES · 登记 URL 或上传镜像</td></tr>`}</tbody></table>
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
    const inspectId = ev.target.dataset.inspect;
    if (inspectId) {
      try {
        await api("/images/" + inspectId + "/inspect", { method: "POST" });
        await load(); render();
      } catch (e) { alert(e.message); }
      return;
    }
    const id = ev.target.dataset.del;
    if (!id) return;
    if (!confirm("删除镜像？")) return;
    await api("/images/" + id, { method: "DELETE" });
    await load(); render();
  };
}

const USER_PRESETS = ["ubuntu", "debian", "rocky", "centos", "root"];
const TIMEZONES = [
  "Asia/Shanghai", "Asia/Hong_Kong", "Asia/Singapore", "Asia/Tokyo", "Asia/Seoul",
  "UTC", "Europe/London", "Europe/Berlin", "America/New_York", "America/Los_Angeles",
  "Australia/Sydney",
];
const FS_OPTS = [
  ["ext4", "ext4"], ["xfs", "xfs"], ["vfat", "EFI / FAT32"], ["swap", "swap"], ["biosboot", "BIOS boot"],
];
const MOUNT_OPTS = ["/", "/boot", "/boot/efi", "/home", "/var", "/tmp"];
const PREFIX_OPTS = ["8", "16", "24", "25", "26", "27", "28"];

let installDraft = null;

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

function blankNic() {
  return { name: "", mac: "", method: "dhcp", ip: "", prefix: "24", gateway: "", dns1: "8.8.8.8", dns2: "" };
}

function blankInstallDraft() {
  return {
    step: 1, machine_id: "", image_id: "", hostname: "", username: "ubuntu",
    password: "", timezone: "Asia/Shanghai", firmware: "uefi", disk: "", reboot: true,
    ssh_keys: [""], partitions: defaultParts("uefi"), nics: [blankNic()],
  };
}

function opts(list, selected, labelFn) {
  return list.map(v => {
    const val = Array.isArray(v) ? v[0] : v;
    const lab = Array.isArray(v) ? v[1] : (labelFn ? labelFn(v) : v);
    return `<option value="${escapeHtml(val)}" ${val === selected ? "selected" : ""}>${escapeHtml(lab)}</option>`;
  }).join("");
}

function selectedMachine() {
  return (cache.machines || []).find(m => m.id === installDraft.machine_id);
}

function selectedImage() {
  return (cache.images || []).find(i => i.id === installDraft.image_id);
}

function machineDisks(m) {
  return ((m && m.inventory && m.inventory.disks) || []).filter(d => d.path);
}

function machineNics(m) {
  return ((m && m.inventory && m.inventory.nics) || []).filter(n => n.name && !String(n.name).startsWith("lo"));
}

function isWholeDiskImage(img) {
  return img && (img.kind === "cloud-disk" || img.kind === "raw-disk");
}

function inspectBadge(img) {
  const inx = img && img.inspect;
  if (!inx || !inx.status || inx.status === "skipped") {
    return `<span class="badge">未检测</span>`;
  }
  if (inx.status === "error") {
    return `<span class="badge bad">不可启动</span>`;
  }
  if (img.kind === "cloud-root" && inx.root_fs && !inx.boot_uefi && !inx.boot_bios) {
    return `<span class="badge ok">rootfs ${escapeHtml(inx.root_fs)}</span>`;
  }
  const bits = [];
  if (inx.boot_uefi) bits.push("UEFI");
  if (inx.boot_bios) bits.push("BIOS");
  if (!bits.length) return `<span class="badge warn">无引导</span>`;
  return `<span class="badge ${inx.status === "warn" ? "warn" : "ok"}">${bits.join(" / ")}</span>`;
}

function imageHint(img, firmware) {
  if (!img) return "先在镜像页登记或上传";
  const whole = isWholeDiskImage(img);
  const inx = img.inspect;
  const base = whole ? "整盘云镜像，写入后保留镜像内分区" : "根文件系统镜像，需要在第 3 步指定分区";
  if (!inx || inx.status === "skipped") {
    return base + "。未检测引导，建议先在镜像页点「检测」。";
  }
  if (inx.status === "error") return "检测失败：" + (inx.message || "");
  if (whole && firmware === "bios" && !inx.boot_bios) return "该镜像不能 BIOS 启动，请改选 UEFI 或换镜像。";
  if (whole && firmware !== "bios" && !inx.boot_uefi) return "该镜像不能 UEFI 启动，请改选 BIOS 或换镜像。";
  return base + (inx.message ? "。" + inx.message : "");
}

function applyMachineDefaults() {
  const m = selectedMachine();
  if (!m) return;
  if (m.firmware && m.firmware !== installDraft.firmware) {
    installDraft.firmware = m.firmware;
    installDraft.partitions = defaultParts(m.firmware);
  }
  if (!installDraft.hostname) installDraft.hostname = m.name || "";
  const nics = machineNics(m);
  if (nics.length && !installDraft.nics.some(n => n.name || n.mac)) {
    installDraft.nics = [{ ...blankNic(), name: nics[0].name, mac: nics[0].mac || "" }];
  }
}

function collectInstallForm() {
  if (!installDraft) return;
  const g = id => document.getElementById(id);
  if (g("in-m")) installDraft.machine_id = g("in-m").value;
  if (g("in-i")) installDraft.image_id = g("in-i").value;
  if (g("in-host")) installDraft.hostname = g("in-host").value.trim();
  if (g("in-user")) {
    const v = g("in-user").value;
    if (v !== "__custom") installDraft.username = v;
  }
  if (g("in-user-custom") && !g("in-user-custom").classList.contains("hidden")) {
    installDraft.username = g("in-user-custom").value.trim() || installDraft.username;
  }
  if (g("in-pass")) installDraft.password = g("in-pass").value;
  if (g("in-tz")) installDraft.timezone = g("in-tz").value;
  if (g("in-fw")) installDraft.firmware = g("in-fw").value;
  if (g("in-disk")) installDraft.disk = g("in-disk").value;
  if (g("in-reboot")) installDraft.reboot = g("in-reboot").checked;
  if (g("in-keys-box")) {
    const keys = $$(".ssh-key").map(el => el.value.trim()).filter(Boolean);
    installDraft.ssh_keys = keys.length ? keys : [""];
  }
  const partRows = $$(".part-row");
  if (partRows.length) {
    installDraft.partitions = partRows.map(row => {
      const flags = [];
      if ($(".f-esp", row)?.checked) flags.push("esp");
      if ($(".f-bios", row)?.checked) flags.push("bios_grub");
      if ($(".f-boot", row)?.checked) flags.push("boot");
      return {
        name: $(".p-name", row).value.trim(),
        size_mb: $(".p-rest", row)?.checked ? 0 : Number($(".p-size", row).value || 0),
        fs: $(".p-fs", row).value,
        mount: $(".p-mount", row).value,
        flags: flags.join(","),
      };
    });
  }
  const nicRows = $$(".nic-row");
  if (nicRows.length) {
    installDraft.nics = nicRows.map(row => ({
      name: $(".n-name", row).value,
      mac: $(".n-mac", row)?.value || "",
      method: $(".n-method", row).value,
      ip: $(".n-ip", row)?.value.trim() || "",
      prefix: $(".n-prefix", row)?.value || "24",
      gateway: $(".n-gw", row)?.value.trim() || "",
      dns1: $(".n-dns1", row)?.value.trim() || "",
      dns2: $(".n-dns2", row)?.value.trim() || "",
    }));
  }
}

function partBar(parts, diskBytes) {
  const fixed = parts.filter(p => p.size_mb > 0).reduce((s, p) => s + p.size_mb, 0);
  const total = diskBytes ? Math.max(diskBytes / 1048576, fixed + 1) : Math.max(fixed + 1024, 1);
  return `<div class="part-bar">${parts.map((p, i) => {
    const mb = p.size_mb > 0 ? p.size_mb : Math.max(total - fixed, 1);
    const pct = Math.max(8, Math.min(80, (mb / total) * 100));
    return `<i class="seg${i % 3}" style="flex:${pct}" title="${escapeHtml(p.name || "part")} ${p.size_mb ? p.size_mb + " MB" : "剩余"}"></i>`;
  }).join("")}</div>`;
}

function renderPartRow(p, i) {
  const rest = !p.size_mb;
  const flags = String(p.flags || "");
  return `<div class="editor-item part-row">
    <div class="editor-grid">
      <div><label>名称</label><input class="p-name" value="${escapeHtml(p.name || "")}" placeholder="root"></div>
      <div><label>文件系统</label><select class="p-fs">${opts(FS_OPTS, p.fs)}</select></div>
      <div><label>挂载点</label><select class="p-mount">
        <option value="" ${!p.mount ? "selected" : ""}>不挂载</option>
        ${opts(MOUNT_OPTS, p.mount)}
      </select></div>
      <div><label>大小 (MB)</label>
        <input class="p-size" type="number" min="0" value="${p.size_mb || ""}" ${rest ? "disabled" : ""} placeholder="512">
      </div>
    </div>
    <div class="chk-row">
      <label><input type="checkbox" class="p-rest" ${rest ? "checked" : ""}> 使用剩余空间</label>
      <label><input type="checkbox" class="f-esp" ${flags.includes("esp") ? "checked" : ""}> ESP</label>
      <label><input type="checkbox" class="f-bios" ${flags.includes("bios_grub") ? "checked" : ""}> BIOS GRUB</label>
      <label><input type="checkbox" class="f-boot" ${flags.includes("boot") ? "checked" : ""}> boot</label>
      <button type="button" class="ghost danger-lite" data-del-part="${i}">删除</button>
    </div>
  </div>`;
}

function renderNicRow(n, i, invNics) {
  const staticOn = n.method === "static";
  const nicOpts = invNics.map(x => {
    const lab = `${x.name} · ${x.mac || ""} ${x.up ? "· UP" : ""}`.trim();
    return `<option value="${escapeHtml(x.name)}" ${x.name === n.name ? "selected" : ""}>${escapeHtml(lab)}</option>`;
  }).join("");
  return `<div class="editor-item nic-row">
    <div class="editor-grid">
      <div><label>网卡</label>
        <select class="n-name">
          ${invNics.length ? nicOpts : `<option value="${escapeHtml(n.name || "eth0")}">${escapeHtml(n.name || "eth0")}</option>`}
        </select>
        <input type="hidden" class="n-mac" value="${escapeHtml(n.mac || (invNics.find(x => x.name === n.name) || {}).mac || "")}">
      </div>
      <div><label>地址获取</label>
        <select class="n-method">
          <option value="dhcp" ${n.method !== "static" ? "selected" : ""}>DHCP（自动）</option>
          <option value="static" ${staticOn ? "selected" : ""}>静态地址</option>
        </select>
      </div>
    </div>
    <div class="static-fields ${staticOn ? "" : "hidden"}">
      <div class="editor-grid">
        <div><label>IP 地址</label><input class="n-ip" value="${escapeHtml(n.ip || "")}" placeholder="10.0.0.20" inputmode="decimal"></div>
        <div><label>前缀长度</label><select class="n-prefix">${opts(PREFIX_OPTS, n.prefix || "24", v => "/" + v)}</select></div>
        <div><label>网关</label><input class="n-gw" value="${escapeHtml(n.gateway || "")}" placeholder="10.0.0.1" inputmode="decimal"></div>
        <div><label>主 DNS</label><input class="n-dns1" value="${escapeHtml(n.dns1 || "")}" placeholder="8.8.8.8"></div>
        <div><label>备 DNS</label><input class="n-dns2" value="${escapeHtml(n.dns2 || "")}" placeholder="1.1.1.1"></div>
      </div>
    </div>
    <div class="chk-row"><button type="button" class="ghost danger-lite" data-del-nic="${i}">删除此网卡</button></div>
  </div>`;
}

function buildInstallBody() {
  collectInstallForm();
  const d = installDraft;
  const nics = d.nics.filter(n => n.name || n.method === "static").map(n => {
    const dns = [n.dns1, n.dns2].map(s => (s || "").trim()).filter(Boolean);
    const cfg = { name: n.name, mac: n.mac, method: n.method };
    if (n.method === "static") {
      cfg.address = n.ip ? `${n.ip}/${n.prefix || "24"}` : "";
      cfg.gateway = n.gateway;
      cfg.dns = dns;
    }
    return cfg;
  });
  return {
    machine_id: d.machine_id, image_id: d.image_id, hostname: d.hostname, username: d.username,
    password: d.password, timezone: d.timezone, firmware: d.firmware,
    ssh_keys: (d.ssh_keys || []).map(s => s.trim()).filter(Boolean),
    disk: d.disk, partitions: d.partitions, network: { nics }, reboot: d.reboot,
  };
}

function renderInstall() {
  const machines = cache.machines || [];
  const images = cache.images || [];
  if (!installDraft) {
    installDraft = blankInstallDraft();
    if (machines[0]) installDraft.machine_id = machines[0].id;
    if (images[0]) installDraft.image_id = images[0].id;
    applyMachineDefaults();
  }
  if (installDraft.machine_id && !machines.some(m => m.id === installDraft.machine_id) && machines[0]) {
    installDraft.machine_id = machines[0].id;
    applyMachineDefaults();
  }
  const d = installDraft;
  const m = selectedMachine();
  const img = selectedImage();
  const disks = machineDisks(m);
  const nics = machineNics(m);
  const whole = isWholeDiskImage(img);
  const userKnown = USER_PRESETS.includes(d.username);
  const step = d.step;
  const stepBody = step === 1 ? `
      <div class="row">
        <div><label>机器</label>
          <select id="in-m">${machines.length ? machines.map(x => `<option value="${x.id}" ${x.id === d.machine_id ? "selected" : ""}>${escapeHtml(x.name)} · ${escapeHtml(x.mac)}</option>`).join("") : `<option value="">（还没有注册的机器）</option>`}</select>
          <p class="hint">${m && m.inventory ? `${m.inventory.cpus || 0} 核 · ${m.inventory.memory_mb || 0} MB · ${(m.inventory.disks || []).length} 块盘` : "选一台已进入 RAMOS 的机器"}</p>
        </div>
        <div><label>镜像</label>
          <select id="in-i">${images.length ? images.map(x => `<option value="${x.id}" ${x.id === d.image_id ? "selected" : ""}>${escapeHtml(x.name)} · ${escapeHtml(x.kind || "")}</option>`).join("") : `<option value="">（请先在「镜像」登记）</option>`}</select>
          <p class="hint">${imageHint(img, d.firmware)}</p>
        </div>
      </div>
      <div class="row3">
        <div><label>主机名</label><input id="in-host" value="${escapeHtml(d.hostname)}" placeholder="node-01"></div>
        <div><label>固件</label>
          <select id="in-fw">
            <option value="uefi" ${d.firmware === "uefi" ? "selected" : ""}>UEFI</option>
            <option value="bios" ${d.firmware === "bios" ? "selected" : ""}>传统 BIOS</option>
          </select>
        </div>
        <div><label>时区</label><select id="in-tz">${opts(TIMEZONES, d.timezone)}</select></div>
      </div>
      <label class="chk"><input type="checkbox" id="in-reboot" ${d.reboot ? "checked" : ""}> 装完重启并切到本地磁盘引导</label>
    ` : step === 2 ? `
      <div class="row">
        <div><label>登录用户</label>
          <select id="in-user">
            ${opts(USER_PRESETS, userKnown ? d.username : "ubuntu")}
            <option value="__custom" ${userKnown ? "" : "selected"}>自定义…</option>
          </select>
          <input id="in-user-custom" class="${userKnown ? "hidden" : ""}" value="${userKnown ? "" : escapeHtml(d.username)}" placeholder="用户名" style="margin-top:8px">
        </div>
        <div><label>登录密码</label><input id="in-pass" type="password" value="${escapeHtml(d.password)}" placeholder="建议同时配置公钥"></div>
      </div>
      <label>SSH 公钥</label>
      <div id="in-keys-box" class="editor-list">
        ${(d.ssh_keys.length ? d.ssh_keys : [""]).map((k, i) => `
          <div class="key-row">
            <input class="ssh-key" value="${escapeHtml(k)}" placeholder="ssh-ed25519 或 ssh-rsa 开头的公钥">
            <button type="button" class="ghost danger-lite" data-del-key="${i}">删除</button>
          </div>`).join("")}
      </div>
      <div class="actions" style="margin-top:10px">
        <button type="button" id="in-add-key">添加公钥</button>
        <button type="button" id="in-import-key">导入 .pub 文件</button>
        <input type="file" id="in-key-file" class="hidden" accept=".pub,text/plain">
      </div>
      <p class="hint">可添加多把钥匙，或导入 id_ed25519.pub。密码可作兜底。</p>
    ` : `
      <div><label>目标磁盘</label>
        <select id="in-disk">
          <option value="" ${!d.disk ? "selected" : ""}>自动选择最大磁盘</option>
          ${disks.map(x => `<option value="${escapeHtml(x.path)}" ${x.path === d.disk ? "selected" : ""}>${escapeHtml(x.path)} · ${fmtBytes(x.size_b)} · ${escapeHtml(x.model || "")}</option>`).join("")}
        </select>
        <p class="hint">${disks.length ? "来自 Agent 上报的库存" : "机器尚未上报磁盘时，将自动选最大盘"}</p>
      </div>
      ${whole ? `<p class="hint">当前镜像是整盘镜像，写入后保留镜像内分区，无需再画分区表。</p>` : `
      <div class="editor-head">
        <h4>分区方案</h4>
        <button type="button" class="ghost" id="in-reset-parts">按固件恢复默认</button>
      </div>
      ${partBar(d.partitions, (disks.find(x => x.path === d.disk) || disks[0] || {}).size_b)}
      <div id="in-parts-box">${d.partitions.map((p, i) => renderPartRow(p, i)).join("")}</div>
      <button type="button" id="in-add-part" style="margin-top:8px">添加分区</button>
      `}
      <div class="editor-head" style="margin-top:18px">
        <h4>网卡</h4>
        <button type="button" class="ghost" id="in-add-nic">添加网卡</button>
      </div>
      <div id="in-nics-box">${(d.nics.length ? d.nics : [blankNic()]).map((n, i) => renderNicRow(n, i, nics)).join("")}</div>
      <p class="hint">${nics.length ? "网卡列表来自机器上报，静态地址只需再填 IP / 网关 / DNS。" : "尚未上报网卡时默认 DHCP。"}</p>
    `;

  view.innerHTML = `
    <div class="panel">
      <div class="steps">
        <span data-step="1" class="${step === 1 ? "on" : ""}">1 机器 / 镜像</span>
        <span data-step="2" class="${step === 2 ? "on" : ""}">2 账号与密钥</span>
        <span data-step="3" class="${step === 3 ? "on" : ""}">3 磁盘与网卡</span>
      </div>
      ${stepBody}
      <div class="actions" style="margin-top:18px">
        ${step > 1 ? `<button type="button" id="in-prev">上一步</button>` : ""}
        ${step < 3 ? `<button type="button" class="primary" id="in-next">下一步</button>` : `
          <button type="button" class="primary" id="in-go">下发装机任务</button>
          <button type="button" id="in-pxe">同时 BMC PXE 重启</button>`}
      </div>
    </div>`;

  const goStep = n => { collectInstallForm(); installDraft.step = n; renderInstall(); };
  $$(".steps span[data-step]").forEach(el => {
    el.onclick = () => goStep(Number(el.dataset.step));
  });
  const next = $("#in-next");
  if (next) next.onclick = () => {
    collectInstallForm();
    if (step === 1 && (!installDraft.machine_id || !installDraft.image_id)) return alert("请选择机器和镜像");
    installDraft.step = step + 1;
    renderInstall();
  };
  const prev = $("#in-prev");
  if (prev) prev.onclick = () => goStep(step - 1);

  const msel = $("#in-m");
  if (msel) msel.onchange = () => {
    collectInstallForm();
    installDraft.nics = [blankNic()];
    installDraft.disk = "";
    applyMachineDefaults();
    renderInstall();
  };
  const isel = $("#in-i");
  if (isel) isel.onchange = () => { collectInstallForm(); renderInstall(); };
  const fw = $("#in-fw");
  if (fw) fw.onchange = () => {
    collectInstallForm();
    installDraft.partitions = defaultParts(installDraft.firmware);
    renderInstall();
  };
  const user = $("#in-user");
  if (user) user.onchange = () => {
    const custom = $("#in-user-custom");
    if (user.value === "__custom") custom.classList.remove("hidden");
    else { custom.classList.add("hidden"); installDraft.username = user.value; }
  };
  const addKey = $("#in-add-key");
  if (addKey) addKey.onclick = () => { collectInstallForm(); installDraft.ssh_keys.push(""); renderInstall(); };
  const importKey = $("#in-import-key");
  const keyFile = $("#in-key-file");
  if (importKey && keyFile) {
    importKey.onclick = () => keyFile.click();
    keyFile.onchange = async () => {
      const f = keyFile.files[0];
      if (!f) return;
      const text = await f.text();
      collectInstallForm();
      const lines = text.split(/\r?\n/).map(s => s.trim()).filter(s => s && !s.startsWith("#"));
      installDraft.ssh_keys = [...(installDraft.ssh_keys || []).filter(Boolean), ...lines];
      if (!installDraft.ssh_keys.length) installDraft.ssh_keys = [""];
      renderInstall();
    };
  }
  view.querySelectorAll("[data-del-key]").forEach(btn => {
    btn.onclick = () => {
      collectInstallForm();
      installDraft.ssh_keys.splice(Number(btn.dataset.delKey), 1);
      if (!installDraft.ssh_keys.length) installDraft.ssh_keys = [""];
      renderInstall();
    };
  });
  const addPart = $("#in-add-part");
  if (addPart) addPart.onclick = () => {
    collectInstallForm();
    installDraft.partitions.push({ name: "data", size_mb: 0, fs: "ext4", mount: "/home", flags: "" });
    renderInstall();
  };
  const resetParts = $("#in-reset-parts");
  if (resetParts) resetParts.onclick = () => {
    collectInstallForm();
    installDraft.partitions = defaultParts(installDraft.firmware);
    renderInstall();
  };
  view.querySelectorAll("[data-del-part]").forEach(btn => {
    btn.onclick = () => {
      collectInstallForm();
      installDraft.partitions.splice(Number(btn.dataset.delPart), 1);
      renderInstall();
    };
  });
  $$(".p-rest").forEach(el => {
    el.onchange = () => {
      const size = $(".p-size", el.closest(".part-row"));
      if (size) size.disabled = el.checked;
    };
  });
  const addNic = $("#in-add-nic");
  if (addNic) addNic.onclick = () => {
    collectInstallForm();
    const unused = nics.find(x => !installDraft.nics.some(n => n.name === x.name));
    installDraft.nics.push(unused ? { ...blankNic(), name: unused.name, mac: unused.mac || "" } : blankNic());
    renderInstall();
  };
  view.querySelectorAll("[data-del-nic]").forEach(btn => {
    btn.onclick = () => {
      collectInstallForm();
      installDraft.nics.splice(Number(btn.dataset.delNic), 1);
      if (!installDraft.nics.length) installDraft.nics = [blankNic()];
      renderInstall();
    };
  });
  $$(".n-method").forEach(el => {
    el.onchange = () => {
      const box = $(".static-fields", el.closest(".nic-row"));
      if (box) box.classList.toggle("hidden", el.value !== "static");
    };
  });
  $$(".n-name").forEach(el => {
    el.onchange = () => {
      const mac = $(".n-mac", el.closest(".nic-row"));
      const hit = nics.find(x => x.name === el.value);
      if (mac) mac.value = hit ? (hit.mac || "") : "";
    };
  });

  const submit = async (pxe) => {
    const body = buildInstallBody();
    if (!body.machine_id || !body.image_id) return alert("请选择机器和镜像");
    const img = selectedImage();
    if (img && img.inspect) {
      const inx = img.inspect;
      if (inx.status === "error") return alert("镜像检测失败：" + (inx.message || "不可启动"));
      if (isWholeDiskImage(img)) {
        if (body.firmware === "bios" && inx.status !== "skipped" && !inx.boot_bios) {
          return alert("该镜像没有 BIOS 引导，请改选 UEFI 或更换镜像");
        }
        if (body.firmware !== "bios" && inx.status !== "skipped" && !inx.boot_uefi) {
          return alert("该镜像没有 UEFI ESP，请改选 BIOS 或更换镜像");
        }
        const disks = machineDisks(selectedMachine());
        const disk = body.disk ? disks.find(x => x.path === body.disk) : disks.slice().sort((a, b) => (b.size_b || 0) - (a.size_b || 0))[0];
        if (disk && disk.size_b && inx.virtual_size_b && inx.virtual_size_b > disk.size_b) {
          return alert("镜像虚拟容量大于目标磁盘");
        }
      }
    }
    if (!isWholeDiskImage(img)) {
      const roots = (body.partitions || []).filter(p => p.mount === "/");
      if (roots.length !== 1) return alert("请恰好指定一个挂载为 / 的根分区");
    }
    try {
      await api("/jobs/install", { method: "POST", body: JSON.stringify(body) });
      if (pxe) await api(`/machines/${body.machine_id}/pxe-install`, { method: "POST" });
      installDraft = null;
      navTo("jobs"); await load(); render();
    } catch (e) { alert(e.message); }
  };
  const go = $("#in-go");
  if (go) go.onclick = () => submit(false);
  const pxeBtn = $("#in-pxe");
  if (pxeBtn) pxeBtn.onclick = () => submit(true);
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
        <tbody>${(cache.jobs||[]).length ? (cache.jobs||[]).map(j => `<tr>
          <td>${escapeHtml(j.type)}<div class="hint mono">${escapeHtml(j.id)}</div></td>
          <td class="mono">${escapeHtml(j.machine_id)}</td>
          <td>${badge(j.status)}</td>
          <td class="mono">${j.progress || 0}%<div class="prog"><i style="width:${Math.max(0, Math.min(100, j.progress || 0))}%"></i></div></td>
          <td>${escapeHtml(j.message || "")}</td>
          <td><button data-j="${j.id}">日志</button></td>
        </tr>`).join("") : `<tr><td colspan="6" class="empty">NO JOBS · 装机与压测任务会出现在这里</td></tr>`}</tbody>
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
  try { settings = await api("/settings"); } catch (e) { view.innerHTML = `<div class="panel">${escapeHtml(e.message)}</div>`; return; }
  const dhcp = settings.dhcp || {};
  const st = settings.dhcp_status || {};
  const nics = settings.nics || [];
  const statusBadge = st.running
    ? `<span class="badge ok">运行中</span>`
    : (st.error ? `<span class="badge bad">失败</span>` : `<span class="badge">已停止</span>`);
  view.innerHTML = `
    <div class="panel">
      <h3>控制面地址</h3>
      <p class="hint">iPXE / RAMOS 必须能访问这个 URL。请填物理机或交换机可达的地址，不要用 127.0.0.1。</p>
      <div class="row">
        <div><label>Public URL</label><input id="b-url" value="${escapeHtml(settings.public_url || "")}"></div>
        <div><label>API Token</label><input id="b-tok" type="password" placeholder="留空不修改"></div>
      </div>
      <button class="primary" id="b-save" style="margin-top:10px">保存</button>
    </div>
    <div class="panel" style="margin-top:14px">
      <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
        <h3 style="margin:0">内置 DHCP 服务器</h3>
        <div>${statusBadge} <span class="hint">${st.running ? escapeHtml(st.interface || "") + " · " + escapeHtml(st.listen || "") : escapeHtml(st.error || "未监听")}</span></div>
      </div>
      <p class="hint">选择连接装机交换机 / PXE 网段的<strong>接入网卡</strong>，DHCP 只在这块网卡上应答。需要绑定 UDP 67，Linux 请用 root，Windows 请以管理员运行。</p>
      <label><input type="checkbox" id="d-on" ${dhcp.enabled ? "checked" : ""}> 启用内置 DHCP</label>
      <label>接入网卡</label>
      <select id="d-if">
        <option value="">（选择网卡）</option>
        ${nics.map(n => {
          const ips = (n.ipv4 || []).map(x => x.cidr).join(", ") || "无 IPv4";
          const sel = n.name === dhcp.interface ? "selected" : "";
          return `<option value="${escapeHtml(n.name)}" ${sel} data-nic="${encodeURIComponent(JSON.stringify(n))}">${escapeHtml(n.name)} · ${escapeHtml(ips)} ${n.up ? "· UP" : "· DOWN"}</option>`;
        }).join("")}
      </select>
      <p class="hint" id="d-if-hint"></p>
      <div class="row3">
        <div><label>网段</label><input id="d-subnet" value="${escapeHtml(dhcp.subnet || "")}" placeholder="10.0.0.0/24"></div>
        <div><label>网关</label><input id="d-gw" value="${escapeHtml(dhcp.router || "")}" placeholder="10.0.0.1"></div>
        <div><label>next-server（TFTP）</label><input id="d-next" value="${escapeHtml(dhcp.next_server || "")}" placeholder="接入网卡 IPv4"></div>
      </div>
      <div class="row3">
        <div><label>地址池起始</label><input id="d-start" value="${escapeHtml(dhcp.range_start || "")}"></div>
        <div><label>地址池结束</label><input id="d-end" value="${escapeHtml(dhcp.range_end || "")}"></div>
        <div><label>DNS</label><input id="d-dns" value="${escapeHtml(dhcp.dns || "8.8.8.8")}" placeholder="8.8.8.8,1.1.1.1"></div>
      </div>
      <div class="row">
        <div><label>租约（秒）</label><input id="d-lease" value="${dhcp.lease_sec || 3600}"></div>
        <div><label>监听地址</label><input id="d-listen" value="${escapeHtml(dhcp.listen_addr || "0.0.0.0:67")}"></div>
      </div>
      <div class="actions" style="margin-top:14px">
        <button class="primary" id="d-apply">保存并应用</button>
        <button id="d-stop">停止 DHCP</button>
      </div>
    </div>
    <div class="panel" style="margin-top:14px">
      <h3>沿用现有 DHCP 时的配置片段</h3>
      <p class="hint">若不启用内置 DHCP：先下发本机 TFTP 上的 undionly.kpxe / ipxe.efi；客户端已成为 iPXE 后再给 <span class="mono">boot.ipxe</span>（仍走本机 TFTP，不访问公网）。</p>
      <pre class="log"># ISC dhcpd
next-server ${escapeHtml((settings.public_url || "http://10.0.0.1:8080").replace(/^https?:\/\//,"").split("/")[0].split(":")[0])};
if exists user-class and option user-class = "iPXE" {
  filename "boot.ipxe";
} elsif option client-arch != 00:00 {
  filename "ipxe.efi";
} else {
  filename "undionly.kpxe";
}

# dnsmasq
dhcp-userclass=set:ipxe,iPXE
dhcp-boot=tag:ipxe,boot.ipxe
dhcp-match=set:efi64,option:client-arch,7
dhcp-boot=tag:!ipxe,tag:efi64,ipxe.efi
dhcp-boot=tag:!ipxe,undionly.kpxe
</pre>
      <p class="hint">TFTP ${escapeHtml(settings.tftp_listen || "")} · 首次部署请执行 <span class="mono">rackauto bootstrap</span></p>
    </div>`;
  const fillFromNic = (nic) => {
    const a = (nic.ipv4 || [])[0];
    const hint = $("#d-if-hint");
    if (!a) {
      hint.textContent = "这块网卡没有 IPv4。请先给接入网卡配上 PXE 网段地址，或手工填写网段 / next-server。";
      return;
    }
    hint.textContent = `将按 ${a.cidr} 填写网段、网关与地址池（可再改）。网关必须和地址池同一网段。`;
    $("#d-subnet").value = a.network;
    $("#d-next").value = a.address;
    $("#d-gw").value = a.address;
    $("#d-start").value = a.pool_start || "";
    $("#d-end").value = a.pool_end || "";
  };
  $("#d-if").onchange = () => {
    const opt = $("#d-if").selectedOptions[0];
    if (!opt || !opt.dataset.nic) { $("#d-if-hint").textContent = ""; return; }
    try { fillFromNic(JSON.parse(decodeURIComponent(opt.dataset.nic))); } catch {}
  };
  if (dhcp.interface) {
    const opt = [...$("#d-if").options].find(o => o.value === dhcp.interface);
    if (opt && opt.dataset.nic && !dhcp.subnet) {
      try { fillFromNic(JSON.parse(decodeURIComponent(opt.dataset.nic))); } catch {}
    }
  }
  const collectDHCP = () => ({
    enabled: $("#d-on").checked,
    interface: $("#d-if").value,
    subnet: $("#d-subnet").value.trim(),
    router: $("#d-gw").value.trim(),
    next_server: $("#d-next").value.trim(),
    range_start: $("#d-start").value.trim(),
    range_end: $("#d-end").value.trim(),
    dns: $("#d-dns").value.trim(),
    lease_sec: Number($("#d-lease").value || 3600),
    listen_addr: $("#d-listen").value.trim() || "0.0.0.0:67",
  });
  $("#b-save").onclick = async () => {
    try {
      await api("/settings", { method: "PUT", body: JSON.stringify({ public_url: $("#b-url").value, api_token: $("#b-tok").value }) });
      alert("已保存");
    } catch (e) { alert(e.message); }
  };
  $("#d-apply").onclick = async () => {
    try {
      await api("/dhcp/apply", { method: "POST", body: JSON.stringify(collectDHCP()) });
      await load(); render();
    } catch (e) { alert(e.message); }
  };
  $("#d-stop").onclick = async () => {
    try {
      await api("/dhcp/stop", { method: "POST", body: "{}" });
      await load(); render();
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

function tickClock() {
  const el = $("#clock");
  if (!el) return;
  el.textContent = new Date().toISOString().replace("T", " ").slice(0, 19) + " Z";
}
tickClock();
setInterval(tickClock, 1000);

load().then(render);
setInterval(() => { load().then(() => { if (["dash","jobs","machines"].includes(current)) render(); }); }, 8000);
