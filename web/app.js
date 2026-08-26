const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const view = $("#view");
const titles = { dash: "总览", machines: "机器", images: "镜像", templates: "账号与密钥", install: "装机向导", stress: "硬件压测", jobs: "任务", boot: "网络引导" };
const kickers = {
  dash: "CONTROL / OVERVIEW",
  machines: "INVENTORY / NODES",
  images: "STORAGE / IMAGES",
  templates: "CREDS / TEMPLATES",
  install: "PROVISION / WIZARD",
  stress: "DIAG / STRESS",
  jobs: "PIPELINE / JOBS",
  boot: "NETBOOT / DHCP",
};
let current = "dash";
let cache = { machines: [], images: [], jobs: [], events: [], overview: {}, catalog: [], templates: [] };

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
  if (!n) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0, x = n;
  while (x >= 1024 && i < u.length - 1) { x /= 1024; i++; }
  return (i === 0 ? String(Math.round(x)) : x.toFixed(1)) + " " + u[i];
}
function fmtEta(sec) {
  if (!isFinite(sec) || sec < 0) return "";
  if (sec < 1) return "<1 秒";
  if (sec < 60) return Math.ceil(sec) + " 秒";
  const m = Math.floor(sec / 60);
  const s = Math.round(sec % 60);
  if (m < 60) return s ? m + " 分 " + s + " 秒" : m + " 分";
  const h = Math.floor(m / 60);
  return h + " 小时 " + (m % 60) + " 分";
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
    const [overview, machines, images, jobs, events, health, catalog, templates] = await Promise.all([
      api("/overview"), api("/machines"), api("/images"), api("/jobs"), api("/events"),
      fetch("/api/v1/health").then(r => r.json()).catch(() => ({ ok: false })),
      api("/os-catalog").catch(() => OS_CATALOG),
      api("/templates").catch(() => []),
    ]);
    cache = { overview, machines, images, jobs, events, catalog: catalog || OS_CATALOG, templates: templates || [] };
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
  const fn = { dash: renderDash, machines: renderMachines, images: renderImages, templates: renderTemplates, install: renderInstall, stress: renderStress, jobs: renderJobs, boot: renderBoot }[current];
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
            <td>${hwCell(m)}</td>
            <td class="actions">
              <button class="primary" data-act="install" data-id="${m.id}">装机</button>
              <button data-act="detail" data-id="${m.id}">详情</button>
              <button data-act="detect" data-id="${m.id}">检测</button>
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
      if (b.dataset.act === "install") return startInstall(id);
      if (b.dataset.act === "detect") {
        await api("/machines/" + id + "/detect", { method: "POST" });
        await load(); render();
        return;
      }
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
  const kv = [
    ["品牌", inv.vendor],
    ["型号", inv.product],
    ["序列号", inv.serial],
    ["SKU", inv.sku],
    ["UUID", inv.uuid],
    ["资产标签", inv.asset_tag],
    ["主板", [inv.board_vendor, inv.board_name].filter(Boolean).join(" ")],
    ["主板序列号", inv.board_serial],
    ["BIOS", [inv.bios_vendor, inv.bios_version, inv.bios_date].filter(Boolean).join(" · ")],
    ["来源", inv.detect_source === "redfish" ? "Redfish BMC" : (inv.detect_source === "dmi" ? "RAMOS DMI" : inv.detect_source)],
  ].filter(x => x[1]);
  openModal(`
    <h3>${escapeHtml(m.name)}</h3>
    <p class="hint mono">${escapeHtml(m.mac)} · ${escapeHtml(m.ip || "")} · agent ${escapeHtml(m.agent_version || "-")}</p>
    <div class="actions">
      <button class="primary" id="md-install">装机</button>
      <button id="md-detect">检测硬件</button>
      <button id="ed">编辑 BMC</button>
      <button id="pxe">PXE 引导并重启</button>
      <button id="disk">下次从磁盘启动</button>
      <button class="danger" id="md-del">删除机器</button>
    </div>
    <h4>服务器</h4>
    ${kv.length ? `<dl class="kv">${kv.map(([k, v]) => `<dt>${escapeHtml(k)}</dt><dd>${escapeHtml(v)}</dd>`).join("")}</dl>` : `<div class="hint">还没有品牌/型号/序列号。点「检测硬件」（需 Redfish），或先 PXE 进 RAMOS 让 Agent 上报 DMI。</div>`}
    <h4>CPU / 内存</h4>
    <div class="hint">${escapeHtml(inv.cpu_model || "")} · ${inv.cpus || 0} 核 · ${inv.memory_mb || 0} MB · ${escapeHtml(inv.firmware || "")}</div>
    <h4>磁盘</h4>
    ${(inv.disks || []).map(d => `<div class="hint mono">${escapeHtml(d.path)} ${fmtBytes(d.size_b)} ${escapeHtml(d.model || "")}${d.serial ? " SN " + escapeHtml(d.serial) : ""}</div>`).join("") || "<div class='hint'>-</div>"}
    <h4>网卡</h4>
    ${(inv.nics || []).map(n => `<div class="hint mono">${escapeHtml(n.name)} ${escapeHtml(n.mac)} ${escapeHtml((n.ips||[]).join(", "))}</div>`).join("") || "<div class='hint'>-</div>"}
  `);
  $("#md-install").onclick = () => startInstall(id);
  $("#md-detect").onclick = async () => {
    try {
      await api("/machines/" + id + "/detect", { method: "POST" });
      await load();
      machineDetail(id);
    } catch (e) { alert(e.message); }
  };
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

function productLine(inv) {
  if (!inv) return "";
  const v = (inv.vendor || "").trim();
  const p = (inv.product || "").trim();
  if (v && p) return p.toLowerCase().startsWith(v.toLowerCase()) ? p : v + " " + p;
  return p || v;
}

function hwCell(m) {
  const inv = (m && m.inventory) || {};
  const line = productLine(inv);
  const specs = inv.cpus || inv.memory_mb ? `${inv.cpus || 0}C / ${inv.memory_mb || 0}MB / ${(inv.disks || []).length} disks` : "";
  const top = line ? escapeHtml(line) : (specs ? `<span class="hint">${escapeHtml(specs)}</span>` : `<span class="hint">-</span>`);
  const sn = inv.serial ? `<div class="hint mono">SN ${escapeHtml(inv.serial)}</div>` : "";
  const sub = line && specs ? `<div class="hint">${escapeHtml(specs)}</div>` : "";
  return top + sn + sub;
}

function startInstall(id) {
  closeModal();
  installDraft = blankInstallDraft();
  installDraft.machine_id = id;
  const images = cache.images || [];
  if (images[0]) {
    installDraft.image_id = images[0].id;
    const v = osVersion(images[0].os_family, images[0].os_version);
    if (v && v.default_user) installDraft.username = v.default_user;
    installDraft.partitions = defaultParts(installDraft.firmware, images[0].os_family, images[0].os_version);
  }
  applyMachineDefaults();
  navTo("install");
}

function machineHint(m) {
  if (!m) return "选一台已进入 RAMOS 的机器";
  const inv = m.inventory || {};
  const bits = [];
  const line = productLine(inv);
  if (line) bits.push(line);
  if (inv.serial) bits.push("SN " + inv.serial);
  bits.push(`${inv.cpus || 0} 核 · ${inv.memory_mb || 0} MB · ${(inv.disks || []).length} 块盘`);
  return bits.join(" · ");
}

function setUploadProgress(p) {
  const box = $("#i-progress");
  const fill = $("#i-progress-fill");
  const bar = $("#i-progress-bar");
  const text = $("#i-progress-text");
  const pctEl = $("#i-progress-pct");
  if (!box || !fill || !bar || !text || !pctEl) return;
  if (p.hidden) {
    box.classList.add("hidden");
    return;
  }
  box.classList.remove("hidden");
  const total = p.total || 0;
  const loaded = p.loaded || 0;
  let pct = 0;
  if (p.phase === "inspecting") pct = 100;
  else if (total > 0) pct = Math.min(100, (loaded / total) * 100);
  fill.classList.toggle("indeterminate", p.phase === "inspecting");
  fill.style.width = (p.phase === "inspecting" ? 100 : pct) + "%";
  bar.setAttribute("aria-valuenow", String(Math.round(pct)));
  pctEl.textContent = Math.round(pct) + "%";
  if (p.phase === "ready") {
    text.textContent = "准备上传 · " + fmtBytes(total);
  } else if (p.phase === "inspecting") {
    text.textContent = "上传完成，正在检测分区和引导…";
  } else if (p.phase === "error") {
    text.textContent = p.error || "上传失败";
    pctEl.textContent = "失败";
  } else {
    const parts = [fmtBytes(loaded) + " / " + fmtBytes(total)];
    if (p.speed > 0) parts.push(fmtBytes(p.speed) + "/s");
    if (p.speed > 0 && total > loaded) {
      const eta = fmtEta((total - loaded) / p.speed);
      if (eta) parts.push("剩余 " + eta);
    }
    text.textContent = parts.join(" · ");
  }
}

function uploadControlPlaneImage(file, fields) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/v1/images/upload");
    const t = token();
    if (t) xhr.setRequestHeader("X-API-Token", t);
    let lastT = Date.now();
    let lastB = 0;
    let speed = 0;
    xhr.upload.onprogress = (ev) => {
      const total = ev.lengthComputable ? ev.total : file.size;
      const loaded = ev.loaded;
      const now = Date.now();
      const dt = (now - lastT) / 1000;
      if (dt >= 0.25) {
        speed = (loaded - lastB) / dt;
        lastT = now;
        lastB = loaded;
      }
      setUploadProgress({ loaded, total, speed, phase: "uploading" });
    };
    xhr.upload.onload = () => {
      setUploadProgress({ loaded: file.size, total: file.size, speed: 0, phase: "inspecting" });
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve(xhr.responseText);
      else reject(new Error(xhr.responseText || ("HTTP " + xhr.status)));
    };
    xhr.onerror = () => reject(new Error("网络错误，上传中断"));
    xhr.onabort = () => reject(new Error("已取消"));
    const fd = new FormData();
    fd.append("file", file);
    fd.append("name", fields.name || file.name);
    fd.append("kind", fields.kind || "");
    fd.append("os_family", fields.os_family || "");
    fd.append("os_version", fields.os_version || "");
    setUploadProgress({ loaded: 0, total: file.size, speed: 0, phase: "uploading" });
    xhr.send(fd);
  });
}

function renderImages() {
  view.innerHTML = `
    <div class="row">
      <div class="panel">
        <h3>登记镜像 URL</h3>
        <label>名称</label><input id="i-name" placeholder="Ubuntu 24.04 cloud">
        <div class="row3">
          ${osSelectHTML("ubuntu", "24.04", "i")}
          ${imageKindHTML("i-kind")}
        </div>
        <label>URL</label><input id="i-url" placeholder="https://...img 或 http://本平台/images/...">
        <div class="row"><div><label>SHA256</label><input id="i-sum"></div><div><label></label><button class="primary" id="i-add">登记</button></div></div>
      </div>
      <div class="panel">
        <h3>上传到控制面</h3>
        <p class="hint">大文件建议用 URL 登记。上传前请选好系统和版本，传到本机后会检测分区表和 UEFI/BIOS 引导。</p>
        <div class="row3">
          ${osSelectHTML("ubuntu", "24.04", "u")}
          ${imageKindHTML("u-kind")}
        </div>
        <input type="file" id="i-file">
        <div id="i-file-meta" class="hint mono"></div>
        <div id="i-progress" class="upload-progress hidden">
          <div class="upload-track" id="i-progress-bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="0">
            <i class="upload-fill" id="i-progress-fill"></i>
          </div>
          <div class="upload-meta">
            <span id="i-progress-text">准备上传</span>
            <b id="i-progress-pct">0%</b>
          </div>
        </div>
        <button class="primary" id="i-up" style="margin-top:12px">上传</button>
      </div>
    </div>
    <div class="panel" style="margin-top:14px">
      <table><thead><tr><th>名称</th><th>类型</th><th>大小</th><th>引导</th><th>URL</th><th></th></tr></thead>
      <tbody>${(cache.images||[]).length ? (cache.images||[]).map(i => `<tr>
        <td>${escapeHtml(i.name)}<div class="hint">${escapeHtml(osLabel(i))}</div></td>
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
        name: $("#i-name").value, os_family: $("#i-os").value, os_version: $("#i-osver").value, kind: $("#i-kind").value,
        url: $("#i-url").value, checksum: $("#i-sum").value, checksum_type: "sha256",
      })});
      await load(); render();
    } catch (e) { alert(e.message); }
  };
  $("#i-file").onchange = () => {
    const f = $("#i-file").files[0];
    const meta = $("#i-file-meta");
    if (!f) {
      if (meta) meta.textContent = "";
      setUploadProgress({ hidden: true });
      return;
    }
    if (meta) meta.textContent = f.name + " · " + fmtBytes(f.size);
    setUploadProgress({ loaded: 0, total: f.size, speed: 0, phase: "ready" });
  };
  $("#i-up").onclick = async () => {
    const f = $("#i-file").files[0];
    if (!f) return alert("选择文件");
    const btn = $("#i-up");
    const fileEl = $("#i-file");
    btn.disabled = true;
    if (fileEl) fileEl.disabled = true;
    ["u-os", "u-osver", "u-kind"].forEach(id => { const el = $("#" + id); if (el) el.disabled = true; });
    btn.textContent = "上传中…";
    try {
      await uploadControlPlaneImage(f, {
        name: f.name,
        kind: $("#u-kind").value,
        os_family: $("#u-os").value,
        os_version: $("#u-osver").value,
      });
      await load();
      render();
    } catch (e) {
      setUploadProgress({ loaded: 0, total: f.size, speed: 0, phase: "error", error: e.message });
      btn.disabled = false;
      if (fileEl) fileEl.disabled = false;
      ["u-os", "u-osver", "u-kind"].forEach(id => { const el = $("#" + id); if (el) el.disabled = false; });
      btn.textContent = "上传";
      alert(e.message);
    }
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
  wireOsSelects("i");
  wireOsSelects("u");
}

function wireOsSelects(prefix) {
  const p = prefix || "i";
  const os = $("#" + p + "-os");
  if (!os) return;
  os.onchange = () => {
    const d = osDistro(os.value);
    const ver = $("#" + p + "-osver");
    if (!ver || !d) return;
    ver.innerHTML = (d.versions || []).map(v => `<option value="${escapeHtml(v.id)}">${escapeHtml(v.label)}</option>`).join("");
    if (d.versions && d.versions.length) ver.value = d.versions[d.versions.length - 1].id;
  };
}

const OS_CATALOG = [
  { family: "ubuntu", label: "Ubuntu", versions: [
    { id: "20.04", label: "20.04 LTS", default_user: "root", root_fs: "ext4", net_backend: "netplan" },
    { id: "22.04", label: "22.04 LTS", default_user: "root", root_fs: "ext4", net_backend: "netplan" },
    { id: "24.04", label: "24.04 LTS", default_user: "root", root_fs: "ext4", net_backend: "netplan" },
    { id: "26.04", label: "26.04 LTS", default_user: "root", root_fs: "ext4", net_backend: "netplan" },
  ]},
  { family: "debian", label: "Debian", versions: [
    { id: "11", label: "11 (bullseye)", default_user: "root", root_fs: "ext4", net_backend: "ifupdown" },
    { id: "12", label: "12 (bookworm)", default_user: "root", root_fs: "ext4", net_backend: "ifupdown" },
    { id: "13", label: "13 (trixie)", default_user: "root", root_fs: "ext4", net_backend: "netplan" },
  ]},
  { family: "rocky", label: "Rocky Linux", versions: [
    { id: "8", label: "8", default_user: "root", root_fs: "xfs", net_backend: "ifcfg" },
    { id: "9", label: "9", default_user: "root", root_fs: "xfs", net_backend: "nm" },
    { id: "10", label: "10", default_user: "root", root_fs: "xfs", net_backend: "nm" },
  ]},
  { family: "alma", label: "AlmaLinux", versions: [
    { id: "8", label: "8", default_user: "root", root_fs: "xfs", net_backend: "ifcfg" },
    { id: "9", label: "9", default_user: "root", root_fs: "xfs", net_backend: "nm" },
  ]},
  { family: "centos", label: "CentOS", versions: [
    { id: "7", label: "7", default_user: "root", root_fs: "ext4", net_backend: "ifcfg" },
    { id: "9", label: "Stream 9", default_user: "root", root_fs: "xfs", net_backend: "nm" },
  ]},
  { family: "custom", label: "自定义", versions: [
    { id: "generic", label: "generic", default_user: "root", root_fs: "ext4", net_backend: "netplan" },
  ]},
];
const BOND_MODES = [
  ["802.3ad", "802.3ad (LACP)"],
  ["active-backup", "主备 active-backup"],
  ["balance-tlb", "balance-tlb"],
  ["balance-alb", "balance-alb"],
  ["balance-xor", "balance-xor"],
  ["balance-rr", "balance-rr"],
];
const USER_PRESETS = ["root", "ubuntu", "debian", "rocky", "almalinux", "centos"];
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

function osCatalog() {
  return (cache.catalog && cache.catalog.length) ? cache.catalog : OS_CATALOG;
}
function osDistro(family) {
  return osCatalog().find(d => d.family === family) || osCatalog()[0];
}
function osVersion(family, id) {
  const d = osDistro(family);
  if (!d) return null;
  return (d.versions || []).find(v => v.id === id) || d.versions[d.versions.length - 1];
}
function osLabel(img) {
  if (!img) return "";
  const d = osCatalog().find(x => x.family === img.os_family);
  const v = osVersion(img.os_family, img.os_version);
  if (!d) return [img.os_family, img.os_version].filter(Boolean).join(" ");
  if (v && v.id && v.id !== "generic") return d.label + " " + v.label;
  return d.label;
}
function osSelectHTML(family, version, prefix) {
  const cats = osCatalog();
  const fam = family || "ubuntu";
  const d = osDistro(fam);
  const ver = version || (d && d.versions[d.versions.length - 1].id) || "";
  const p = prefix || "i";
  return `<div><label>系统</label>
    <select id="${p}-os">${cats.map(x => `<option value="${escapeHtml(x.family)}" ${x.family === fam ? "selected" : ""}>${escapeHtml(x.label)}</option>`).join("")}</select>
  </div>
  <div><label>版本</label>
    <select id="${p}-osver">${(d.versions || []).map(v => `<option value="${escapeHtml(v.id)}" ${v.id === ver ? "selected" : ""}>${escapeHtml(v.label)}</option>`).join("")}</select>
  </div>`;
}

function imageKindHTML(id, selected) {
  const cur = selected || "cloud-disk";
  const kinds = [
    ["cloud-disk", "云镜像（整盘 qcow2/raw）"],
    ["cloud-root", "根文件系统镜像"],
    ["raw-disk", "整盘 raw"],
  ];
  return `<div><label>类型</label>
    <select id="${escapeHtml(id)}">${kinds.map(([v, lab]) => `<option value="${v}" ${v === cur ? "selected" : ""}>${lab}</option>`).join("")}</select>
  </div>`;
}

function defaultParts(fw, family, version) {
  const root = (osVersion(family, version) || {}).root_fs || "ext4";
  if (fw === "bios") return [
    { name: "biosboot", size_mb: 1, fs: "biosboot", mount: "", flags: "bios_grub" },
    { name: "root", size_mb: 0, fs: root, mount: "/", flags: "" },
  ];
  return [
    { name: "efi", size_mb: 512, fs: "vfat", mount: "/boot/efi", flags: "esp,boot" },
    { name: "root", size_mb: 0, fs: root, mount: "/", flags: "" },
  ];
}

function blankNic() {
  return { kind: "ethernet", name: "", mac: "", method: "dhcp", ip: "", prefix: "24", gateway: "", dns1: "8.8.8.8", dns2: "", bond_mode: "802.3ad", bond_members: [], vlan_id: "", parent: "" };
}

function blankInstallDraft() {
  return {
    step: 1, machine_id: "", image_id: "", hostname: "", username: "root",
    password: "", timezone: "Asia/Shanghai", firmware: "uefi", disk: "", reboot: true,
    ssh_keys: [""], partitions: defaultParts("uefi", "ubuntu", "24.04"), nics: [blankNic()],
    account_tpl: "", key_tpl: "",
  };
}

function credTemplates(kind) {
  return (cache.templates || []).filter(t => !kind || t.kind === kind);
}

function keyPreview(keys) {
  const k = (keys || []).map(s => String(s).trim()).filter(Boolean);
  if (!k.length) return "—";
  const parts = k[0].split(/\s+/);
  const first = parts.slice(0, 2).join(" ");
  const short = first.length > 42 ? first.slice(0, 42) + "…" : first;
  return k.length > 1 ? short + " · +" + (k.length - 1) : short;
}

function tplChipHTML(list, selectedId) {
  if (!list.length) return `<span class="hint">还没有模板</span>`;
  return list.map(t => `<button type="button" class="tpl-chip ${t.id === selectedId ? "on" : ""}" data-tpl="${escapeHtml(t.id)}">${escapeHtml(t.name || t.id)}</button>`).join("");
}

function ensureInstallDraft() {
  if (installDraft) return;
  installDraft = blankInstallDraft();
  const machines = cache.machines || [];
  const images = cache.images || [];
  if (machines[0]) installDraft.machine_id = machines[0].id;
  if (images[0]) {
    installDraft.image_id = images[0].id;
    const v = osVersion(images[0].os_family, images[0].os_version);
    if (v && v.default_user) installDraft.username = v.default_user;
    installDraft.partitions = defaultParts(installDraft.firmware, images[0].os_family, images[0].os_version);
  }
  applyMachineDefaults();
}

function applyCredTemplate(t) {
  if (!t) return;
  ensureInstallDraft();
  if (t.kind === "account") {
    if (t.username) installDraft.username = t.username;
    installDraft.password = t.password || "";
    installDraft.account_tpl = t.id;
    const keys = (t.ssh_keys || []).map(s => String(s).trim()).filter(Boolean);
    if (keys.length) {
      installDraft.ssh_keys = keys;
      installDraft.key_tpl = t.id;
    }
    return;
  }
  const keys = (t.ssh_keys || []).map(s => String(s).trim()).filter(Boolean);
  installDraft.ssh_keys = keys.length ? keys : [""];
  installDraft.key_tpl = t.id;
}

function quoteTemplate(t) {
  applyCredTemplate(t);
  if (installDraft.machine_id && installDraft.image_id) installDraft.step = 2;
  if (current !== "install") navTo("install");
  else renderInstall();
}

function splitKeyLines(text) {
  return String(text || "").split(/\r?\n/).map(s => s.trim()).filter(s => s && !s.startsWith("#"));
}

function templateForm(t = {}, kind) {
  const k = t.kind || kind || "account";
  const keys = (t.ssh_keys && t.ssh_keys.length) ? t.ssh_keys.join("\n") : "";
  openModal(`
    <h3>${t.id ? "编辑模板" : (k === "key" ? "新建密钥模板" : "新建账号模板")}</h3>
    <label>名称</label><input id="tf-name" value="${escapeHtml(t.name || "")}" placeholder="${k === "key" ? "例如 运维公钥" : "例如 机房 root"}">
    ${k === "account" ? `
      <div class="row">
        <div><label>用户名</label><input id="tf-user" value="${escapeHtml(t.username || "root")}"></div>
        <div><label>密码</label><input id="tf-pass" type="password" placeholder="${t.id ? "留空则不修改" : ""}"></div>
      </div>
    ` : ""}
    <label>SSH 公钥（每行一把）</label>
    <textarea id="tf-keys" placeholder="ssh-ed25519 AAAA...">${escapeHtml(keys)}</textarea>
    <label>备注</label><input id="tf-notes" value="${escapeHtml(t.notes || "")}" placeholder="可选">
    <div class="actions" style="margin-top:14px">
      <button class="primary" id="tf-save">保存</button>
      <button class="ghost" id="tf-close">取消</button>
    </div>`);
  $("#tf-close").onclick = closeModal;
  $("#tf-save").onclick = async () => {
    const name = $("#tf-name").value.trim();
    if (!name) return alert("请填写名称");
    const sshKeys = splitKeyLines($("#tf-keys").value);
    const body = { kind: k, name, notes: $("#tf-notes").value.trim(), ssh_keys: sshKeys };
    if (k === "account") {
      body.username = $("#tf-user").value.trim();
      body.password = $("#tf-pass").value;
      if (!body.username) return alert("请填写用户名");
    } else if (!sshKeys.length) {
      return alert("请填写至少一把公钥");
    }
    try {
      if (t.id) await api("/templates/" + t.id, { method: "PUT", body: JSON.stringify({ ...t, ...body }) });
      else await api("/templates", { method: "POST", body: JSON.stringify(body) });
      closeModal();
      await load();
      render();
    } catch (e) { alert(e.message); }
  };
}

function promptTemplateSave(kind) {
  collectInstallForm();
  const d = installDraft || {};
  const keys = (d.ssh_keys || []).map(s => String(s).trim()).filter(Boolean);
  if (kind === "key" && !keys.length) return alert("请先填写公钥");
  if (kind === "account" && !(d.username || "").trim()) return alert("请先填写用户名");
  openModal(`
    <h3>${kind === "account" ? "保存账号模板" : "保存密钥模板"}</h3>
    <label>名称</label><input id="tpl-name" placeholder="${kind === "account" ? "例如 机房 root" : "例如 运维公钥"}">
    <label>备注</label><input id="tpl-notes" placeholder="可选">
    ${kind === "account" ? `<label class="chk"><input type="checkbox" id="tpl-with-keys" ${keys.length ? "checked" : ""}> 同时保存当前公钥</label>` : ""}
    <div class="actions" style="margin-top:14px">
      <button class="primary" id="tpl-ok">保存</button>
      <button class="ghost" id="tpl-no">取消</button>
    </div>`);
  $("#tpl-no").onclick = closeModal;
  $("#tpl-ok").onclick = async () => {
    const name = $("#tpl-name").value.trim();
    if (!name) return alert("请填写名称");
    const body = { kind, name, notes: $("#tpl-notes").value.trim() };
    if (kind === "account") {
      body.username = d.username;
      body.password = d.password;
      if ($("#tpl-with-keys") && $("#tpl-with-keys").checked) body.ssh_keys = keys;
    } else {
      body.ssh_keys = keys;
    }
    try {
      const saved = await api("/templates", { method: "POST", body: JSON.stringify(body) });
      if (saved && saved.id) {
        if (kind === "account") installDraft.account_tpl = saved.id;
        else installDraft.key_tpl = saved.id;
      }
      closeModal();
      await load();
      renderInstall();
    } catch (e) { alert(e.message); }
  };
}

function renderTemplates() {
  const list = cache.templates || [];
  view.innerHTML = `
    <div class="actions" style="margin-bottom:12px">
      <button class="primary" id="tpl-add-acct">新建账号模板</button>
      <button id="tpl-add-key">新建密钥模板</button>
    </div>
    <p class="hint" style="margin:0 0 12px">账号模板保存用户名和密码（公钥可选）；密钥模板只保存公钥。装机向导第 2 步可一键引用。</p>
    <div class="panel">
      <table>
        <thead><tr><th>名称</th><th>类型</th><th>用户</th><th>密钥</th><th>备注</th><th></th></tr></thead>
        <tbody>${list.length ? list.map(t => `
          <tr>
            <td>${escapeHtml(t.name)}<div class="hint mono">${escapeHtml(t.id)}</div></td>
            <td>${t.kind === "key" ? `<span class="badge">密钥</span>` : `<span class="badge ok">账号</span>`}</td>
            <td class="mono">${t.kind === "account" ? escapeHtml(t.username || "—") : "—"}</td>
            <td class="mono hint">${escapeHtml(keyPreview(t.ssh_keys))}</td>
            <td class="hint">${escapeHtml(t.notes || "")}</td>
            <td class="actions">
              <button class="primary" data-act="quote" data-id="${t.id}">引用到装机</button>
              <button data-act="edit" data-id="${t.id}">编辑</button>
              <button class="danger" data-act="delete" data-id="${t.id}" data-name="${escapeHtml(t.name || t.id)}">删除</button>
            </td>
          </tr>`).join("") : `<tr><td colspan="6" class="empty">NO TEMPLATES · 先建账号或密钥模板</td></tr>`}
        </tbody>
      </table>
    </div>`;
  $("#tpl-add-acct").onclick = () => templateForm({}, "account");
  $("#tpl-add-key").onclick = () => templateForm({}, "key");
  view.onclick = async (ev) => {
    const b = ev.target.closest("button[data-act]");
    if (!b) return;
    const t = list.find(x => x.id === b.dataset.id);
    try {
      if (b.dataset.act === "quote") {
        if (!t) return;
        quoteTemplate(t);
        return;
      }
      if (b.dataset.act === "edit") {
        if (t) templateForm(t, t.kind);
        return;
      }
      if (b.dataset.act === "delete") {
        if (!confirm("删除模板「" + (b.dataset.name || b.dataset.id) + "」？")) return;
        await api("/templates/" + b.dataset.id, { method: "DELETE" });
        await load();
        render();
      }
    } catch (e) { alert(e.message); }
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
  const base = whole
    ? "整盘云镜像，写入后保留镜像内分区，并自动把根分区扩到整盘"
    : "根文件系统镜像，需要在第 3 步指定分区（剩余空间会占满磁盘）";
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
    installDraft.partitions = defaultParts(m.firmware, (selectedImage() || {}).os_family, (selectedImage() || {}).os_version);
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
      kind: $(".n-kind", row)?.value || "ethernet",
      name: $(".n-name", row)?.value || "",
      mac: $(".n-mac", row)?.value || "",
      method: $(".n-method", row)?.value || "dhcp",
      ip: $(".n-ip", row)?.value.trim() || "",
      prefix: $(".n-prefix", row)?.value || "24",
      gateway: $(".n-gw", row)?.value.trim() || "",
      dns1: $(".n-dns1", row)?.value.trim() || "",
      dns2: $(".n-dns2", row)?.value.trim() || "",
      bond_mode: $(".n-bond-mode", row)?.value || "802.3ad",
      bond_members: $$(".n-member:checked", row).map(el => el.value),
      vlan_id: $(".n-vlan", row)?.value || "",
      parent: $(".n-parent", row)?.value || "",
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

function renderNicRow(n, i, invNics, allNics) {
  const kind = n.kind || "ethernet";
  const staticOn = n.method === "static";
  const nicOpts = invNics.map(x => {
    const lab = `${x.name} · ${x.mac || ""} ${x.up ? "· UP" : ""}`.trim();
    return `<option value="${escapeHtml(x.name)}" ${x.name === n.name ? "selected" : ""}>${escapeHtml(lab)}</option>`;
  }).join("");
  const parents = [];
  invNics.forEach(x => { if (x.name && !parents.includes(x.name)) parents.push(x.name); });
  (allNics || []).forEach(x => {
    if ((x.kind === "bond" || x.kind === "ethernet") && x.name && !parents.includes(x.name)) parents.push(x.name);
  });
  const parentOpts = parents.map(p => `<option value="${escapeHtml(p)}" ${p === n.parent ? "selected" : ""}>${escapeHtml(p)}</option>`).join("");
  const members = n.bond_members || [];
  const methodSel = `<select class="n-method">
          <option value="dhcp" ${n.method === "dhcp" || !n.method ? "selected" : ""}>DHCP（自动）</option>
          <option value="static" ${staticOn ? "selected" : ""}>静态地址</option>
          <option value="none" ${n.method === "none" ? "selected" : ""}>不配置地址（给 VLAN/Bond 用）</option>
        </select>`;
  const staticBox = `<div class="static-fields ${staticOn ? "" : "hidden"}">
      <div class="editor-grid">
        <div><label>IP 地址</label><input class="n-ip" value="${escapeHtml(n.ip || "")}" placeholder="10.0.0.20" inputmode="decimal"></div>
        <div><label>前缀长度</label><select class="n-prefix">${opts(PREFIX_OPTS, n.prefix || "24", v => "/" + v)}</select></div>
        <div><label>网关</label><input class="n-gw" value="${escapeHtml(n.gateway || "")}" placeholder="10.0.0.1" inputmode="decimal"></div>
        <div><label>主 DNS</label><input class="n-dns1" value="${escapeHtml(n.dns1 || "")}" placeholder="8.8.8.8"></div>
        <div><label>备 DNS</label><input class="n-dns2" value="${escapeHtml(n.dns2 || "")}" placeholder="1.1.1.1"></div>
      </div>
    </div>`;
  let body = "";
  if (kind === "bond") {
    body = `<div class="editor-grid">
      <div><label>Bond 名称</label><input class="n-name" value="${escapeHtml(n.name || "bond0")}" placeholder="bond0"></div>
      <div><label>模式</label><select class="n-bond-mode">${opts(BOND_MODES, n.bond_mode || "802.3ad")}</select></div>
      <div><label>地址获取</label>${methodSel}</div>
    </div>
    <div class="full" style="margin-top:8px"><label>成员网卡</label>
      <div class="member-list">${invNics.length ? invNics.map(x => `<label><input type="checkbox" class="n-member" value="${escapeHtml(x.name)}" ${members.includes(x.name) ? "checked" : ""}> ${escapeHtml(x.name)}${x.mac ? " · " + escapeHtml(x.mac) : ""}</label>`).join("") : `<span class="hint">机器尚未上报网卡</span>`}</div>
    </div>
    ${staticBox}`;
  } else if (kind === "vlan") {
    body = `<div class="editor-grid">
      <div><label>父接口</label><select class="n-parent">${parentOpts || `<option value="${escapeHtml(n.parent || "eth0")}">${escapeHtml(n.parent || "eth0")}</option>`}</select></div>
      <div><label>VLAN ID</label><input class="n-vlan" type="number" min="1" max="4094" value="${escapeHtml(String(n.vlan_id || ""))}" placeholder="100"></div>
      <div><label>接口名</label><input class="n-name" value="${escapeHtml(n.name || "")}" placeholder="自动 parent.ID"></div>
      <div><label>地址获取</label>${methodSel}</div>
    </div>
    ${staticBox}`;
  } else {
    body = `<div class="editor-grid">
      <div><label>网卡</label>
        <select class="n-name">
          ${invNics.length ? nicOpts : `<option value="${escapeHtml(n.name || "eth0")}">${escapeHtml(n.name || "eth0")}</option>`}
        </select>
        <input type="hidden" class="n-mac" value="${escapeHtml(n.mac || (invNics.find(x => x.name === n.name) || {}).mac || "")}">
      </div>
      <div><label>地址获取</label>${methodSel}</div>
    </div>
    ${staticBox}`;
  }
  return `<div class="editor-item nic-row">
    <div class="editor-grid">
      <div><label>类型</label>
        <select class="n-kind">
          <option value="ethernet" ${kind === "ethernet" ? "selected" : ""}>物理网卡</option>
          <option value="bond" ${kind === "bond" ? "selected" : ""}>Bond</option>
          <option value="vlan" ${kind === "vlan" ? "selected" : ""}>VLAN</option>
        </select>
      </div>
    </div>
    ${body}
    <div class="chk-row"><button type="button" class="ghost danger-lite" data-del-nic="${i}">删除</button></div>
  </div>`;
}

function buildInstallBody() {
  collectInstallForm();
  const d = installDraft;
  const nics = d.nics.filter(n => n.name || n.kind === "vlan" || n.kind === "bond" || n.method === "static").map(n => {
    const dns = [n.dns1, n.dns2].map(s => (s || "").trim()).filter(Boolean);
    const cfg = { kind: n.kind || "ethernet", name: n.name, mac: n.mac, method: n.method || "dhcp" };
    if (n.method === "static") {
      cfg.address = n.ip ? `${n.ip}/${n.prefix || "24"}` : "";
      cfg.gateway = n.gateway;
      cfg.dns = dns;
    }
    if (cfg.kind === "bond") {
      cfg.bond_mode = n.bond_mode || "802.3ad";
      cfg.bond_members = n.bond_members || [];
      if (!cfg.name) cfg.name = "bond0";
    }
    if (cfg.kind === "vlan") {
      cfg.vlan_id = Number(n.vlan_id || 0);
      cfg.parent = n.parent;
      if (!cfg.name && cfg.parent && cfg.vlan_id) cfg.name = cfg.parent + "." + cfg.vlan_id;
    }
    return cfg;
  });
  const inv = machineNics(selectedMachine());
  const have = new Set(nics.filter(x => (x.kind || "ethernet") === "ethernet").map(x => x.name));
  for (const n of [...nics]) {
    if (n.kind !== "bond") continue;
    for (const m of n.bond_members || []) {
      if (!m || have.has(m)) continue;
      const hit = inv.find(x => x.name === m);
      nics.push({ kind: "ethernet", name: m, mac: hit ? hit.mac : "", method: "none" });
      have.add(m);
    }
  }
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
    if (images[0]) {
      installDraft.image_id = images[0].id;
      const v = osVersion(images[0].os_family, images[0].os_version);
      if (v && v.default_user) installDraft.username = v.default_user;
      installDraft.partitions = defaultParts(installDraft.firmware, images[0].os_family, images[0].os_version);
    }
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
          <p class="hint">${machineHint(m)}</p>
        </div>
        <div><label>镜像</label>
          <select id="in-i">${images.length ? images.map(x => `<option value="${x.id}" ${x.id === d.image_id ? "selected" : ""}>${escapeHtml(x.name)} · ${escapeHtml(osLabel(x) || x.kind || "")}</option>`).join("") : `<option value="">（请先在「镜像」登记）</option>`}</select>
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
      <div class="tpl-block">
        <label>账号模板</label>
        <div class="tpl-row">
          ${tplChipHTML(credTemplates("account"), d.account_tpl)}
          <button type="button" class="ghost" id="in-save-acct">保存当前为账号模板</button>
          <button type="button" class="ghost" id="in-manage-tpl">管理模板</button>
        </div>
      </div>
      <div class="row">
        <div><label>登录用户</label>
          <select id="in-user">
            ${opts(USER_PRESETS, userKnown ? d.username : "root")}
            <option value="__custom" ${userKnown ? "" : "selected"}>自定义…</option>
          </select>
          <input id="in-user-custom" class="${userKnown ? "hidden" : ""}" value="${userKnown ? "" : escapeHtml(d.username)}" placeholder="用户名" style="margin-top:8px">
        </div>
        <div><label>登录密码</label><input id="in-pass" type="password" value="${escapeHtml(d.password)}" placeholder="建议同时配置公钥"></div>
      </div>
      <div class="tpl-block">
        <label>密钥模板</label>
        <div class="tpl-row">
          ${tplChipHTML(credTemplates("key"), d.key_tpl)}
          <button type="button" class="ghost" id="in-save-key">保存当前公钥为模板</button>
        </div>
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
      <p class="hint">点模板即可填入。账号模板会写入用户名和密码；若模板带公钥也会一并填入。密钥模板会替换下方公钥列表。</p>
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
        <div class="actions">
          <button type="button" class="ghost" id="in-add-nic">添加网卡</button>
          <button type="button" class="ghost" id="in-add-bond">添加 Bond</button>
          <button type="button" class="ghost" id="in-add-vlan">添加 VLAN</button>
        </div>
      </div>
      <div id="in-nics-box">${(d.nics.length ? d.nics : [blankNic()]).map((n, i) => renderNicRow(n, i, nics, d.nics)).join("")}</div>
      <p class="hint">${nics.length ? "物理网卡按 MAC 绑定，装完后改名为 nic0 / nic1，不沿用 RAMOS 里的 ens* / eth*。" : "尚未上报网卡时默认 DHCP。"} 可组合 Bond 和 VLAN（VLAN 可建在 Bond 上）。配置按镜像系统和版本写入（Ubuntu netplan / Debian ifupdown / Rocky NetworkManager）。</p>
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
  if (isel) isel.onchange = () => {
    collectInstallForm();
    const img = selectedImage();
    const v = img ? osVersion(img.os_family, img.os_version) : null;
    if (v && v.default_user) installDraft.username = v.default_user;
    installDraft.partitions = defaultParts(installDraft.firmware, img && img.os_family, img && img.os_version);
    renderInstall();
  };
  const fw = $("#in-fw");
  if (fw) fw.onchange = () => {
    collectInstallForm();
    const img = selectedImage();
    installDraft.partitions = defaultParts(installDraft.firmware, img && img.os_family, img && img.os_version);
    renderInstall();
  };
  const user = $("#in-user");
  if (user) user.onchange = () => {
    const custom = $("#in-user-custom");
    if (user.value === "__custom") custom.classList.remove("hidden");
    else { custom.classList.add("hidden"); installDraft.username = user.value; }
  };
  view.querySelectorAll("button[data-tpl]").forEach(btn => {
    btn.onclick = () => {
      const t = (cache.templates || []).find(x => x.id === btn.dataset.tpl);
      if (!t) return;
      collectInstallForm();
      applyCredTemplate(t);
      renderInstall();
    };
  });
  const saveAcct = $("#in-save-acct");
  if (saveAcct) saveAcct.onclick = () => promptTemplateSave("account");
  const saveKey = $("#in-save-key");
  if (saveKey) saveKey.onclick = () => promptTemplateSave("key");
  const manageTpl = $("#in-manage-tpl");
  if (manageTpl) manageTpl.onclick = () => { collectInstallForm(); navTo("templates"); };
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
    const img = selectedImage();
    installDraft.partitions = defaultParts(installDraft.firmware, img && img.os_family, img && img.os_version);
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
    const unused = nics.find(x => !installDraft.nics.some(n => n.kind !== "bond" && n.kind !== "vlan" && n.name === x.name));
    installDraft.nics.push(unused ? { ...blankNic(), name: unused.name, mac: unused.mac || "" } : blankNic());
    renderInstall();
  };
  const addBond = $("#in-add-bond");
  if (addBond) addBond.onclick = () => {
    collectInstallForm();
    const used = new Set(installDraft.nics.filter(n => n.kind === "bond").map(n => n.name));
    let name = "bond0";
    for (let i = 0; used.has(name); i++) name = "bond" + i;
    const members = nics.slice(0, 2).map(x => x.name).filter(Boolean);
    installDraft.nics.push({ ...blankNic(), kind: "bond", name, method: "none", bond_members: members });
    renderInstall();
  };
  const addVlan = $("#in-add-vlan");
  if (addVlan) addVlan.onclick = () => {
    collectInstallForm();
    const bond = [...installDraft.nics].reverse().find(n => n.kind === "bond" && n.name);
    const parent = (bond && bond.name) || (nics[0] && nics[0].name) || "eth0";
    installDraft.nics.push({ ...blankNic(), kind: "vlan", parent, vlan_id: "100", method: "static", name: parent + ".100" });
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
  $$(".n-kind").forEach(el => {
    el.onchange = () => {
      collectInstallForm();
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
    for (const n of (body.network && body.network.nics) || []) {
      if (n.kind === "vlan") {
        if (!n.vlan_id || n.vlan_id < 1 || n.vlan_id > 4094) return alert("VLAN ID 需要在 1–4094");
        if (!n.parent) return alert("VLAN 需要选择父接口（物理网卡或 Bond）");
      }
      if (n.kind === "bond" && !(n.bond_members && n.bond_members.length)) return alert("Bond 请至少勾选一块成员网卡");
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
