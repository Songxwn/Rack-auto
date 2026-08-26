const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const view = $("#view");
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
let cache = { machines: [], images: [], jobs: [], events: [], overview: {}, catalog: [], templates: [], kmsKeys: [] };
let authed = false;

function pageTitle(name) {
  return t("title." + name);
}

async function api(path, opts = {}) {
  const headers = { ...(opts.headers || {}) };
  if (!headers["Content-Type"] && !(opts.body instanceof FormData)) headers["Content-Type"] = "application/json";
  const res = await fetch("/api/v1" + path, { ...opts, headers });
  const text = await res.text();
  if (res.status === 401) {
    showLogin(path === "/login" ? (text || t("login.bad")) : t("login.need"));
    throw new Error(text || t("login.need"));
  }
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
  if (sec < 1) return t("eta.lt1s");
  if (sec < 60) return t("eta.sec", { n: Math.ceil(sec) });
  const m = Math.floor(sec / 60);
  const s = Math.round(sec % 60);
  if (m < 60) return s ? t("eta.minsec", { m, s }) : t("eta.min", { n: m });
  const h = Math.floor(m / 60);
  return t("eta.hour", { h, m: m % 60 });
}
function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function navTo(name) {
  current = name;
  $$("#nav button").forEach(b => b.classList.toggle("active", b.dataset.view === name));
  $("#title").textContent = pageTitle(name);
  $("#kicker").textContent = kickers[name] || "";
  render();
}
$$("#nav button").forEach(b => b.addEventListener("click", () => navTo(b.dataset.view)));
$("#refresh").addEventListener("click", () => { if (authed) load().then(render); });

async function load() {
  try {
    const [overview, machines, images, jobs, events, health, catalog, templates, kmsKeys] = await Promise.all([
      api("/overview"), api("/machines"), api("/images"), api("/jobs"), api("/events"),
      fetch("/api/v1/health").then(r => r.json()).catch(() => ({ ok: false })),
      api("/os-catalog").catch(() => OS_CATALOG),
      api("/templates").catch(() => []),
      api("/windows/kms-keys").catch(() => KMS_KEYS),
    ]);
    cache = { overview, machines, images, jobs, events, catalog: catalog || OS_CATALOG, templates: templates || [], kmsKeys: kmsKeys || KMS_KEYS };
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
  const label = (v && String(v).trim()) || "dev";
  const side = $("#ctrl-ver");
  const head = $("#app-ver");
  if (side) side.textContent = label;
  if (head) head.textContent = label;
  document.title = t("doc.title", { v: label });
}

async function removeMachine(id, label) {
  const name = label || id;
  if (!confirm(t("m.delConfirm", { name }))) return false;
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
      <button class="ghost" id="go-boot">${t("dash.openDhcp")}</button>
    </div>
    <div class="panel telemetry">
      <h3>TELEMETRY</h3>
      ${(cache.events || []).map(e => `<div class="event"><span class="t mono">${escapeHtml(e.created_at)}</span><span class="badge ${e.level === "error" ? "bad" : e.level === "warn" ? "warn" : "ok"}">${escapeHtml(e.level)}</span><span>${escapeHtml(e.message)}</span></div>`).join("") || `<div class='empty'>${t("dash.empty")}</div>`}
    </div>`;
  $("#go-boot").onclick = () => navTo("boot");
}

function renderMachines() {
  view.innerHTML = `
    <div class="actions" style="margin-bottom:12px">
      <button class="primary" id="add-m">${t("m.add")}</button>
    </div>
    <div class="panel">
      <table>
        <thead><tr><th>${t("m.col.name")}</th><th>${t("m.col.mac")}</th><th>${t("m.col.status")}</th><th>${t("m.col.fw")}</th><th>${t("m.col.bmc")}</th><th>${t("m.col.hw")}</th><th></th></tr></thead>
        <tbody>${(cache.machines || []).length ? (cache.machines || []).map(m => `
          <tr>
            <td>${escapeHtml(m.name)}<div class="hint mono">${escapeHtml(m.id)}</div></td>
            <td class="mono">${escapeHtml(m.mac)}<div>${escapeHtml(m.ip || "")}</div></td>
            <td>${badge(m.status)}<div class="hint">${escapeHtml(m.boot_mode || "")}</div></td>
            <td>${escapeHtml(m.firmware || "-")}</td>
            <td>${escapeHtml(m.bmc_type || "-")}<div class="hint">${escapeHtml(m.bmc_address || "")}</div></td>
            <td>${hwCell(m)}</td>
            <td class="actions">
              <button class="primary" data-act="install" data-id="${m.id}">${t("m.install")}</button>
              <button data-act="detail" data-id="${m.id}">${t("m.detail")}</button>
              <button data-act="detect" data-id="${m.id}">${t("m.detect")}</button>
              <button data-act="pxe" data-id="${m.id}">${t("m.pxe")}</button>
              <button data-act="on" data-id="${m.id}">${t("m.on")}</button>
              <button data-act="off" data-id="${m.id}">${t("m.off")}</button>
              <button data-act="cycle" data-id="${m.id}">${t("m.cycle")}</button>
              <button class="danger" data-act="delete" data-id="${m.id}" data-name="${escapeHtml(m.name || m.mac || m.id)}">${t("btn.delete")}</button>
            </td>
          </tr>`).join("") : `<tr><td colspan="7" class="empty">${t("m.empty")}</td></tr>`}
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
    <h3>${m.id ? t("m.edit") : t("m.register")}</h3>
    <div class="row">
      <div><label>${t("m.name")}</label><input id="f-name" value="${escapeHtml(m.name || "")}"></div>
      <div><label>${t("m.mac")}</label><input id="f-mac" value="${escapeHtml(m.mac || "")}" placeholder="aa:bb:cc:dd:ee:ff"></div>
    </div>
    <div class="row3">
      <div><label>${t("m.fw")}</label><select id="f-fw"><option value="uefi">UEFI</option><option value="bios">${t("m.fw.bios")}</option></select></div>
      <div><label>${t("m.boot")}</label><select id="f-boot"><option value="ramos">RAMOS</option><option value="pxe">PXE</option><option value="disk">${t("m.boot.disk")}</option></select></div>
      <div><label>${t("m.bmcProto")}</label><select id="f-bmc"><option value="ipmi">IPMI</option><option value="redfish">Redfish</option></select></div>
    </div>
    <div class="row3">
      <div><label>${t("m.bmcAddr")}</label><input id="f-addr" value="${escapeHtml(m.bmc_address || "")}" placeholder="${t("ph.bmc")}"></div>
      <div><label>${t("m.port")}</label><input id="f-port" value="${m.bmc_port || 623}"></div>
      <div><label>${t("m.user")}</label><input id="f-user" value="${escapeHtml(m.bmc_username || "")}"></div>
    </div>
    <label>${t("m.pass")}</label><input id="f-pass" type="password" placeholder="${m.id ? t("m.passKeep") : ""}">
    <label><input type="checkbox" id="f-insecure" ${m.bmc_insecure ? "checked" : ""}> ${t("m.insecure")}</label>
    <div class="actions" style="margin-top:14px">
      <button class="primary" id="f-save">${t("btn.save")}</button>
      <button class="ghost" id="f-close">${t("btn.cancel")}</button>
      ${m.id ? `<button class="danger" id="f-del">${t("btn.delete")}</button>` : ""}
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
    [t("kv.vendor"), inv.vendor],
    [t("kv.product"), inv.product],
    [t("kv.serial"), inv.serial],
    ["SKU", inv.sku],
    ["UUID", inv.uuid],
    [t("kv.asset"), inv.asset_tag],
    [t("kv.board"), [inv.board_vendor, inv.board_name].filter(Boolean).join(" ")],
    [t("kv.boardSn"), inv.board_serial],
    ["BIOS", [inv.bios_vendor, inv.bios_version, inv.bios_date].filter(Boolean).join(" · ")],
    [t("kv.source"), inv.detect_source === "redfish" ? "Redfish BMC" : (inv.detect_source === "dmi" ? "RAMOS DMI" : inv.detect_source)],
  ].filter(x => x[1]);
  openModal(`
    <h3>${escapeHtml(m.name)}</h3>
    <p class="hint mono">${escapeHtml(m.mac)} · ${escapeHtml(m.ip || "")} · agent ${escapeHtml(m.agent_version || "-")}</p>
    <div class="actions">
      <button class="primary" id="md-install">${t("m.install")}</button>
      <button id="md-detect">${t("m.detectHw")}</button>
      <button id="ed">${t("m.editBmc")}</button>
      <button id="pxe">${t("m.pxeBoot")}</button>
      <button id="disk">${t("m.nextDisk")}</button>
      <button class="danger" id="md-del">${t("m.del")}</button>
    </div>
    <h4>${t("m.server")}</h4>
    ${kv.length ? `<dl class="kv">${kv.map(([k, v]) => `<dt>${escapeHtml(k)}</dt><dd>${escapeHtml(v)}</dd>`).join("")}</dl>` : `<div class="hint">${t("m.noInv")}</div>`}
    <h4>${t("m.cpuMem")}</h4>
    <div class="hint">${escapeHtml(inv.cpu_model || "")} · ${t("m.cores", { n: inv.cpus || 0 })} · ${inv.memory_mb || 0} MB · ${escapeHtml(inv.firmware || "")}</div>
    <h4>${t("m.disks")}</h4>
    ${(inv.disks || []).map(d => `<div class="hint mono">${escapeHtml(d.path)} ${fmtBytes(d.size_b)} ${escapeHtml(d.model || "")}${d.serial ? " SN " + escapeHtml(d.serial) : ""}</div>`).join("") || "<div class='hint'>-</div>"}
    <h4>${t("m.nics")}</h4>
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
  if (!m) return t("m.hintPick");
  const inv = m.inventory || {};
  const bits = [];
  const line = productLine(inv);
  if (line) bits.push(line);
  if (inv.serial) bits.push("SN " + inv.serial);
  bits.push(t("m.hintInv", { cpu: inv.cpus || 0, mem: inv.memory_mb || 0, disks: (inv.disks || []).length }));
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
    text.textContent = t("up.ready", { size: fmtBytes(total) });
  } else if (p.phase === "inspecting") {
    text.textContent = t("up.inspect");
  } else if (p.phase === "error") {
    text.textContent = p.error || t("up.fail");
    pctEl.textContent = t("up.failed");
  } else {
    const parts = [fmtBytes(loaded) + " / " + fmtBytes(total)];
    if (p.speed > 0) parts.push(fmtBytes(p.speed) + "/s");
    if (p.speed > 0 && total > loaded) {
      const eta = fmtEta((total - loaded) / p.speed);
      if (eta) parts.push(t("up.eta", { eta }));
    }
    text.textContent = parts.join(" · ");
  }
}

function uploadControlPlaneImage(file, fields) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/v1/images/upload");
    xhr.withCredentials = true;
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
      if (xhr.status === 401) {
        showLogin(t("login.need"));
        reject(new Error(t("login.need")));
        return;
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(xhr.responseText);
      else reject(new Error(xhr.responseText || ("HTTP " + xhr.status)));
    };
    xhr.onerror = () => reject(new Error(t("net.err")));
    xhr.onabort = () => reject(new Error(t("net.abort")));
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
        <h3>${t("img.urlTitle")}</h3>
        <label>${t("img.name")}</label><input id="i-name" placeholder="Ubuntu 24.04 cloud">
        <div class="row3">
          ${osSelectHTML("ubuntu", "24.04", "i")}
          ${imageKindHTML("i-kind")}
        </div>
        <label>${t("img.url")}</label><input id="i-url" placeholder="${t("ph.imgUrl")}">
        <div class="row"><div><label>SHA256</label><input id="i-sum"></div><div><label></label><button class="primary" id="i-add">${t("img.register")}</button></div></div>
      </div>
      <div class="panel">
        <h3>${t("img.uploadTitle")}</h3>
        <p class="hint">${t("img.hint")}</p>
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
            <span id="i-progress-text">${t("up.prepare")}</span>
            <b id="i-progress-pct">0%</b>
          </div>
        </div>
        <button class="primary" id="i-up" style="margin-top:12px">${t("up.btn")}</button>
      </div>
    </div>
    <div class="panel" style="margin-top:14px">
      <table><thead><tr><th>${t("img.name")}</th><th>${t("img.col.kind")}</th><th>${t("img.col.size")}</th><th>${t("img.col.boot")}</th><th>URL</th><th></th></tr></thead>
      <tbody>${(cache.images||[]).length ? (cache.images||[]).map(i => `<tr>
        <td>${escapeHtml(i.name)}<div class="hint">${escapeHtml(osLabel(i))}</div></td>
        <td>${escapeHtml(i.kind)}</td><td>${fmtBytes(i.size_b || (i.inspect && i.inspect.virtual_size_b) || 0)}</td>
        <td>${inspectBadge(i)}<div class="hint">${escapeHtml((i.inspect && i.inspect.message) || "")}</div></td>
        <td class="mono hint">${escapeHtml(i.url)}</td>
        <td class="actions">
          <button data-inspect="${i.id}">${t("img.inspect")}</button>
          <button class="danger" data-del="${i.id}">${t("btn.delete")}</button>
        </td>
      </tr>`).join("") : `<tr><td colspan="6" class="empty">${t("img.empty")}</td></tr>`}</tbody></table>
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
    if (!f) return alert(t("img.choose"));
    const btn = $("#i-up");
    const fileEl = $("#i-file");
    btn.disabled = true;
    if (fileEl) fileEl.disabled = true;
    ["u-os", "u-osver", "u-kind"].forEach(id => { const el = $("#" + id); if (el) el.disabled = true; });
    btn.textContent = t("up.busy");
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
      btn.textContent = t("up.btn");
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
    if (!confirm(t("img.delConfirm"))) return;
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
    const kind = $("#" + p + "-kind");
    if (kind && os.value === "windows") kind.value = "windows-iso";
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
    { id: "10", label: "10", default_user: "root", root_fs: "xfs", net_backend: "nm" },
  ]},
  { family: "centos", label: "CentOS", versions: [
    { id: "7", label: "7", default_user: "root", root_fs: "ext4", net_backend: "ifcfg" },
    { id: "8", label: "Stream 8", default_user: "root", root_fs: "xfs", net_backend: "ifcfg" },
    { id: "9", label: "Stream 9", default_user: "root", root_fs: "xfs", net_backend: "nm" },
    { id: "10", label: "Stream 10", default_user: "root", root_fs: "xfs", net_backend: "nm" },
  ]},
  { family: "windows", label: "Windows Server", versions: [
    { id: "2019", label: "2019", default_user: "Administrator", root_fs: "ntfs", net_backend: "windows" },
    { id: "2022", label: "2022", default_user: "Administrator", root_fs: "ntfs", net_backend: "windows" },
    { id: "2025", label: "2025", default_user: "Administrator", root_fs: "ntfs", net_backend: "windows" },
  ]},
  { family: "custom", label: "custom", versions: [
    { id: "generic", label: "generic", default_user: "root", root_fs: "ext4", net_backend: "netplan" },
  ]},
];
// Official Microsoft GVLK fallback (same as GET /api/v1/windows/kms-keys).
const KMS_KEYS = [
  { id: "2019-standard", version: "2019", edition: "standard", label: "Windows Server 2019 Standard", key: "N69G4-B89J2-4G8F4-WWYCC-J464C" },
  { id: "2019-datacenter", version: "2019", edition: "datacenter", label: "Windows Server 2019 Datacenter", key: "WMDGN-G9PQG-XVVXX-R3X43-63DFG" },
  { id: "2019-essentials", version: "2019", edition: "essentials", label: "Windows Server 2019 Essentials", key: "WVDHN-86M7X-466P6-VHXV7-YY726" },
  { id: "2022-standard", version: "2022", edition: "standard", label: "Windows Server 2022 Standard", key: "VDYBN-27WPP-V4HQT-9VMD4-VMK7H" },
  { id: "2022-datacenter", version: "2022", edition: "datacenter", label: "Windows Server 2022 Datacenter", key: "WX4NM-KYWYW-QJJR4-XV3QB-6VM33" },
  { id: "2022-datacenter-azure", version: "2022", edition: "datacenter-azure", label: "Windows Server 2022 Datacenter: Azure Edition", key: "NTBV8-9K7Q8-V27C6-M2BTV-KHMXV" },
  { id: "2025-standard", version: "2025", edition: "standard", label: "Windows Server 2025 Standard", key: "TVRH6-WHNXV-R9WG3-9XRFY-MY832" },
  { id: "2025-datacenter", version: "2025", edition: "datacenter", label: "Windows Server 2025 Datacenter", key: "D764K-2NDRG-47T6Q-P8T8W-YP6DF" },
  { id: "2025-datacenter-azure", version: "2025", edition: "datacenter-azure", label: "Windows Server 2025 Datacenter: Azure Edition", key: "XGN3F-F394H-FD2MY-PP6FD-8MCRC" },
];
const DEFAULT_KMS_HOST = "kms.songxwn.com";
const BOND_MODES = [
  ["802.3ad", "802.3ad (LACP)"],
  ["active-backup", "active-backup"],
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
  const dlab = d ? (d.family === "custom" ? t("os.custom") : d.label) : "";
  if (!d) return [img.os_family, img.os_version].filter(Boolean).join(" ");
  if (v && v.id && v.id !== "generic") return dlab + " " + v.label;
  return dlab;
}
function osSelectHTML(family, version, prefix) {
  const cats = osCatalog();
  const fam = family || "ubuntu";
  const d = osDistro(fam);
  const ver = version || (d && d.versions[d.versions.length - 1].id) || "";
  const p = prefix || "i";
  const lab = x => x.family === "custom" ? t("os.custom") : x.label;
  return `<div><label>${t("img.os")}</label>
    <select id="${p}-os">${cats.map(x => `<option value="${escapeHtml(x.family)}" ${x.family === fam ? "selected" : ""}>${escapeHtml(lab(x))}</option>`).join("")}</select>
  </div>
  <div><label>${t("img.ver")}</label>
    <select id="${p}-osver">${(d.versions || []).map(v => `<option value="${escapeHtml(v.id)}" ${v.id === ver ? "selected" : ""}>${escapeHtml(v.label)}</option>`).join("")}</select>
  </div>`;
}

function imageKindHTML(id, selected) {
  const cur = selected || "cloud-disk";
  const kinds = [
    ["cloud-disk", t("img.kind.cloudDisk")],
    ["cloud-root", t("img.kind.cloudRoot")],
    ["raw-disk", t("img.kind.rawDisk")],
    ["windows-iso", t("img.kind.winIso")],
    ["windows-wim", t("img.kind.winWim")],
  ];
  return `<div><label>${t("img.kind")}</label>
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
    account_tpl: "", key_tpl: "", wim_index: 0, product_key: "",
    key_mode: "none", kms_key_id: "", kms_host: "", remove_defender: false,
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
  if (!list.length) return `<span class="hint">${t("tpl.none")}</span>`;
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

function templateForm(tpl = {}, kind) {
  const k = tpl.kind || kind || "account";
  const keys = (tpl.ssh_keys && tpl.ssh_keys.length) ? tpl.ssh_keys.join("\n") : "";
  openModal(`
    <h3>${tpl.id ? t("tpl.edit") : (k === "key" ? t("tpl.newKey") : t("tpl.newAcct"))}</h3>
    <label>${t("tpl.name")}</label><input id="tf-name" value="${escapeHtml(tpl.name || "")}" placeholder="${k === "key" ? t("tpl.phKey") : t("tpl.phAcct")}">
    ${k === "account" ? `
      <div class="row">
        <div><label>${t("tpl.user")}</label><input id="tf-user" value="${escapeHtml(tpl.username || "root")}"></div>
        <div><label>${t("tpl.pass")}</label><input id="tf-pass" type="password" placeholder="${tpl.id ? t("m.passKeep") : ""}"></div>
      </div>
    ` : ""}
    <label>${t("tpl.keys")}</label>
    <textarea id="tf-keys" placeholder="ssh-ed25519 AAAA...">${escapeHtml(keys)}</textarea>
    <label>${t("tpl.notes")}</label><input id="tf-notes" value="${escapeHtml(tpl.notes || "")}" placeholder="${t("tpl.notesPh")}">
    <div class="actions" style="margin-top:14px">
      <button class="primary" id="tf-save">${t("btn.save")}</button>
      <button class="ghost" id="tf-close">${t("btn.cancel")}</button>
    </div>`);
  $("#tf-close").onclick = closeModal;
  $("#tf-save").onclick = async () => {
    const name = $("#tf-name").value.trim();
    if (!name) return alert(t("tpl.needName"));
    const sshKeys = splitKeyLines($("#tf-keys").value);
    const body = { kind: k, name, notes: $("#tf-notes").value.trim(), ssh_keys: sshKeys };
    if (k === "account") {
      body.username = $("#tf-user").value.trim();
      body.password = $("#tf-pass").value;
      if (!body.username) return alert(t("tpl.needUser"));
    } else if (!sshKeys.length) {
      return alert(t("tpl.needKey"));
    }
    try {
      if (tpl.id) await api("/templates/" + tpl.id, { method: "PUT", body: JSON.stringify({ ...tpl, ...body }) });
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
  if (kind === "key" && !keys.length) return alert(t("tpl.needKeyFirst"));
  if (kind === "account" && !(d.username || "").trim()) return alert(t("tpl.needUser"));
  openModal(`
    <h3>${kind === "account" ? t("tpl.saveAcct") : t("tpl.saveKey")}</h3>
    <label>${t("tpl.name")}</label><input id="tpl-name" placeholder="${kind === "account" ? t("tpl.phAcct") : t("tpl.phKey")}">
    <label>${t("tpl.notes")}</label><input id="tpl-notes" placeholder="${t("tpl.notesPh")}">
    ${kind === "account" ? `<label class="chk"><input type="checkbox" id="tpl-with-keys" ${keys.length ? "checked" : ""}> ${t("tpl.withKeys")}</label>` : ""}
    <div class="actions" style="margin-top:14px">
      <button class="primary" id="tpl-ok">${t("btn.save")}</button>
      <button class="ghost" id="tpl-no">${t("btn.cancel")}</button>
    </div>`);
  $("#tpl-no").onclick = closeModal;
  $("#tpl-ok").onclick = async () => {
    const name = $("#tpl-name").value.trim();
    if (!name) return alert(t("tpl.needName"));
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
      <button class="primary" id="tpl-add-acct">${t("tpl.newAcct")}</button>
      <button id="tpl-add-key">${t("tpl.newKey")}</button>
    </div>
    <p class="hint" style="margin:0 0 12px">${t("tpl.pageHint")}</p>
    <div class="panel">
      <table>
        <thead><tr><th>${t("tpl.name")}</th><th>${t("tpl.col.kind")}</th><th>${t("tpl.col.user")}</th><th>${t("tpl.col.keys")}</th><th>${t("tpl.col.notes")}</th><th></th></tr></thead>
        <tbody>${list.length ? list.map(row => `
          <tr>
            <td>${escapeHtml(row.name)}<div class="hint mono">${escapeHtml(row.id)}</div></td>
            <td>${row.kind === "key" ? `<span class="badge">${t("tpl.kind.key")}</span>` : `<span class="badge ok">${t("tpl.kind.acct")}</span>`}</td>
            <td class="mono">${row.kind === "account" ? escapeHtml(row.username || "—") : "—"}</td>
            <td class="mono hint">${escapeHtml(keyPreview(row.ssh_keys))}</td>
            <td class="hint">${escapeHtml(row.notes || "")}</td>
            <td class="actions">
              <button class="primary" data-act="quote" data-id="${row.id}">${t("tpl.quote")}</button>
              <button data-act="edit" data-id="${row.id}">${t("btn.edit")}</button>
              <button class="danger" data-act="delete" data-id="${row.id}" data-name="${escapeHtml(row.name || row.id)}">${t("btn.delete")}</button>
            </td>
          </tr>`).join("") : `<tr><td colspan="6" class="empty">${t("tpl.empty")}</td></tr>`}
        </tbody>
      </table>
    </div>`;
  $("#tpl-add-acct").onclick = () => templateForm({}, "account");
  $("#tpl-add-key").onclick = () => templateForm({}, "key");
  view.onclick = async (ev) => {
    const b = ev.target.closest("button[data-act]");
    if (!b) return;
    const tpl = list.find(x => x.id === b.dataset.id);
    try {
      if (b.dataset.act === "quote") {
        if (!tpl) return;
        quoteTemplate(tpl);
        return;
      }
      if (b.dataset.act === "edit") {
        if (tpl) templateForm(tpl, tpl.kind);
        return;
      }
      if (b.dataset.act === "delete") {
        if (!confirm(t("tpl.delConfirm", { name: b.dataset.name || b.dataset.id }))) return;
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
function isWindowsImage(img) {
  if (!img) return false;
  if (img.kind === "windows-iso" || img.kind === "windows-wim") return true;
  if (img.inspect && img.inspect.windows) return true;
  const f = String(img.os_family || "").toLowerCase();
  return f === "windows" || f.startsWith("windows");
}
function wimImages(img) {
  return (img && img.inspect && img.inspect.wim_images) || [];
}
function defaultWIMIndex(img) {
  const list = wimImages(img);
  if (!list.length) return 1;
  const score = im => {
    const n = ((im.name || "") + " " + (im.flags || "") + " " + (im.edition || "")).toUpperCase();
    let s = 0;
    if (n.includes("SERVER")) s += 10;
    if (n.includes("STANDARD")) s += 5;
    if (n.includes("DATACENTER")) s += 3;
    if (n.includes("CORE")) s -= 4;
    return s;
  };
  let best = list[0];
  for (const im of list) if (score(im) > score(best)) best = im;
  return best.index || 1;
}

function kmsKeyList() {
  return (cache.kmsKeys && cache.kmsKeys.length) ? cache.kmsKeys : KMS_KEYS;
}

function matchKMSKeyID(img, wimIndex) {
  const list = wimImages(img);
  const wim = list.find(x => Number(x.index) === Number(wimIndex)) || list[0] || {};
  let ver = String((img && img.os_version) || "").trim();
  const blob = ((wim.name || "") + " " + (wim.description || "")).toUpperCase();
  if (!ver) {
    if (blob.includes("2025")) ver = "2025";
    else if (blob.includes("2022")) ver = "2022";
    else if (blob.includes("2019")) ver = "2019";
  }
  const n = ((wim.name || "") + " " + (wim.description || "") + " " + (wim.flags || "") + " " + (wim.edition || "")).toUpperCase();
  let ed = "standard";
  if (n.includes("AZURE")) ed = "datacenter-azure";
  else if (n.includes("ESSENTIAL")) ed = "essentials";
  else if (n.includes("DATACENTER")) ed = "datacenter";
  const id = ver + "-" + ed;
  if (kmsKeyList().some(k => k.id === id)) return id;
  const fb = ver + "-standard";
  if (kmsKeyList().some(k => k.id === fb)) return fb;
  return "2022-standard";
}

function inspectBadge(img) {
  const inx = img && img.inspect;
  if (!inx || !inx.status || inx.status === "skipped") {
    return `<span class="badge">${t("insp.none")}</span>`;
  }
  if (inx.status === "error") {
    return `<span class="badge bad">${t("insp.bad")}</span>`;
  }
  if (isWindowsImage(img)) {
    const n = (inx.wim_images || []).length;
    return `<span class="badge ok">${n ? "WIM ×" + n : "WinPE"}</span>`;
  }
  if (img.kind === "cloud-root" && inx.root_fs && !inx.boot_uefi && !inx.boot_bios) {
    return `<span class="badge ok">rootfs ${escapeHtml(inx.root_fs)}</span>`;
  }
  const bits = [];
  if (inx.boot_uefi) bits.push("UEFI");
  if (inx.boot_bios) bits.push("BIOS");
  if (!bits.length) return `<span class="badge warn">${t("insp.noboot")}</span>`;
  return `<span class="badge ${inx.status === "warn" ? "warn" : "ok"}">${bits.join(" / ")}</span>`;
}

function imageHint(img, firmware) {
  if (!img) return t("img.hintNeed");
  if (isWindowsImage(img)) {
    const inx = img.inspect;
    if (!inx || inx.status === "skipped") return t("img.hintWinSkip");
    if (inx.status === "error") return t("img.hintFail", { msg: inx.message || "" });
    return t("img.hintWinOk") + (inx.message ? " " + inx.message : "");
  }
  const whole = isWholeDiskImage(img);
  const inx = img.inspect;
  const base = whole ? t("img.hintWhole") : t("img.hintRoot");
  if (!inx || inx.status === "skipped") {
    return base + t("img.hintNoInspect");
  }
  if (inx.status === "error") return t("img.hintFail", { msg: inx.message || "" });
  if (whole && firmware === "bios" && !inx.boot_bios) return t("img.hintNoBios");
  if (whole && firmware !== "bios" && !inx.boot_uefi) return t("img.hintNoUefi");
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
  if (g("in-wim")) installDraft.wim_index = Number(g("in-wim").value || 0);
  if (g("in-key-mode")) installDraft.key_mode = g("in-key-mode").value || "none";
  if (g("in-kms-id")) installDraft.kms_key_id = g("in-kms-id").value;
  if (g("in-pkey")) installDraft.product_key = g("in-pkey").value.trim();
  if (g("in-kms-host")) installDraft.kms_host = g("in-kms-host").value.trim();
  if (g("in-rm-defender")) installDraft.remove_defender = g("in-rm-defender").checked;
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
    return `<i class="seg${i % 3}" style="flex:${pct}" title="${escapeHtml(p.name || "part")} ${p.size_mb ? p.size_mb + " MB" : t("in.rest")}"></i>`;
  }).join("")}</div>`;
}

function renderPartRow(p, i) {
  const rest = !p.size_mb;
  const flags = String(p.flags || "");
  return `<div class="editor-item part-row">
    <div class="editor-grid">
      <div><label>${t("in.partName")}</label><input class="p-name" value="${escapeHtml(p.name || "")}" placeholder="root"></div>
      <div><label>${t("in.fs")}</label><select class="p-fs">${opts(FS_OPTS, p.fs)}</select></div>
      <div><label>${t("in.mount")}</label><select class="p-mount">
        <option value="" ${!p.mount ? "selected" : ""}>${t("in.nomount")}</option>
        ${opts(MOUNT_OPTS, p.mount)}
      </select></div>
      <div><label>${t("in.size")}</label>
        <input class="p-size" type="number" min="0" value="${p.size_mb || ""}" ${rest ? "disabled" : ""} placeholder="512">
      </div>
    </div>
    <div class="chk-row">
      <label><input type="checkbox" class="p-rest" ${rest ? "checked" : ""}> ${t("in.useRest")}</label>
      <label><input type="checkbox" class="f-esp" ${flags.includes("esp") ? "checked" : ""}> ESP</label>
      <label><input type="checkbox" class="f-bios" ${flags.includes("bios_grub") ? "checked" : ""}> BIOS GRUB</label>
      <label><input type="checkbox" class="f-boot" ${flags.includes("boot") ? "checked" : ""}> boot</label>
      <button type="button" class="ghost danger-lite" data-del-part="${i}">${t("btn.delete")}</button>
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
          <option value="dhcp" ${n.method === "dhcp" || !n.method ? "selected" : ""}>${t("in.dhcp")}</option>
          <option value="static" ${staticOn ? "selected" : ""}>${t("in.static")}</option>
          <option value="none" ${n.method === "none" ? "selected" : ""}>${t("in.noaddr")}</option>
        </select>`;
  const staticBox = `<div class="static-fields ${staticOn ? "" : "hidden"}">
      <div class="editor-grid">
        <div><label>${t("in.ip")}</label><input class="n-ip" value="${escapeHtml(n.ip || "")}" placeholder="10.0.0.20" inputmode="decimal"></div>
        <div><label>${t("in.prefix")}</label><select class="n-prefix">${opts(PREFIX_OPTS, n.prefix || "24", v => "/" + v)}</select></div>
        <div><label>${t("in.gw")}</label><input class="n-gw" value="${escapeHtml(n.gateway || "")}" placeholder="10.0.0.1" inputmode="decimal"></div>
        <div><label>${t("in.dns1")}</label><input class="n-dns1" value="${escapeHtml(n.dns1 || "")}" placeholder="8.8.8.8"></div>
        <div><label>${t("in.dns2")}</label><input class="n-dns2" value="${escapeHtml(n.dns2 || "")}" placeholder="1.1.1.1"></div>
      </div>
    </div>`;
  let body = "";
  if (kind === "bond") {
    body = `<div class="editor-grid">
      <div><label>${t("in.bondName")}</label><input class="n-name" value="${escapeHtml(n.name || "bond0")}" placeholder="bond0"></div>
      <div><label>${t("in.mode")}</label><select class="n-bond-mode">${opts(BOND_MODES, n.bond_mode || "802.3ad")}</select></div>
      <div><label>${t("in.addrGet")}</label>${methodSel}</div>
    </div>
    <div class="full" style="margin-top:8px"><label>${t("in.members")}</label>
      <div class="member-list">${invNics.length ? invNics.map(x => `<label><input type="checkbox" class="n-member" value="${escapeHtml(x.name)}" ${members.includes(x.name) ? "checked" : ""}> ${escapeHtml(x.name)}${x.mac ? " · " + escapeHtml(x.mac) : ""}</label>`).join("") : `<span class="hint">${t("in.noNicInv")}</span>`}</div>
    </div>
    ${staticBox}`;
  } else if (kind === "vlan") {
    body = `<div class="editor-grid">
      <div><label>${t("in.parent")}</label><select class="n-parent">${parentOpts || `<option value="${escapeHtml(n.parent || "eth0")}">${escapeHtml(n.parent || "eth0")}</option>`}</select></div>
      <div><label>${t("in.vlanId")}</label><input class="n-vlan" type="number" min="1" max="4094" value="${escapeHtml(String(n.vlan_id || ""))}" placeholder="100"></div>
      <div><label>${t("in.ifname")}</label><input class="n-name" value="${escapeHtml(n.name || "")}" placeholder="${t("in.ifAuto")}"></div>
      <div><label>${t("in.addrGet")}</label>${methodSel}</div>
    </div>
    ${staticBox}`;
  } else {
    body = `<div class="editor-grid">
      <div><label>${t("in.nic")}</label>
        <select class="n-name">
          ${invNics.length ? nicOpts : `<option value="${escapeHtml(n.name || "eth0")}">${escapeHtml(n.name || "eth0")}</option>`}
        </select>
        <input type="hidden" class="n-mac" value="${escapeHtml(n.mac || (invNics.find(x => x.name === n.name) || {}).mac || "")}">
      </div>
      <div><label>${t("in.addrGet")}</label>${methodSel}</div>
    </div>
    ${staticBox}`;
  }
  return `<div class="editor-item nic-row">
    <div class="editor-grid">
      <div><label>${t("in.kind")}</label>
        <select class="n-kind">
          <option value="ethernet" ${kind === "ethernet" ? "selected" : ""}>${t("in.phys")}</option>
          <option value="bond" ${kind === "bond" ? "selected" : ""}>Bond</option>
          <option value="vlan" ${kind === "vlan" ? "selected" : ""}>VLAN</option>
        </select>
      </div>
    </div>
    ${body}
    <div class="chk-row"><button type="button" class="ghost danger-lite" data-del-nic="${i}">${t("btn.delete")}</button></div>
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
  const win = isWindowsImage(selectedImage());
  const netNics = win ? nics.filter(n => (n.kind || "ethernet") === "ethernet") : nics;
  return {
    machine_id: d.machine_id, image_id: d.image_id, hostname: d.hostname, username: d.username,
    password: d.password, timezone: d.timezone, firmware: d.firmware,
    ssh_keys: win ? [] : (d.ssh_keys || []).map(s => s.trim()).filter(Boolean),
    disk: d.disk, partitions: win ? [] : d.partitions, network: { nics: netNics }, reboot: d.reboot,
    wim_index: win ? Number(d.wim_index || 0) : undefined,
    product_key: win && d.key_mode === "custom" ? (d.product_key || "") : undefined,
    kms_key_id: win && d.key_mode === "kms" ? (d.kms_key_id || "") : undefined,
    kms_host: win ? (d.kms_host || "") : undefined,
    remove_defender: win ? !!d.remove_defender : undefined,
    enable_rdp: win ? true : undefined,
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
  const win = isWindowsImage(img);
  const userPresets = win ? ["Administrator"] : USER_PRESETS;
  const userKnown = userPresets.includes(d.username);
  const step = d.step;
  const wimList = wimImages(img);
  const wimIdx = d.wim_index || defaultWIMIndex(img);
  const step2Linux = `
      <div class="tpl-block">
        <label>${t("in.acctTpl")}</label>
        <div class="tpl-row">
          ${tplChipHTML(credTemplates("account"), d.account_tpl)}
          <button type="button" class="ghost" id="in-save-acct">${t("in.saveAcct")}</button>
          <button type="button" class="ghost" id="in-manage-tpl">${t("in.manageTpl")}</button>
        </div>
      </div>
      <div class="row">
        <div><label>${t("in.loginUser")}</label>
          <select id="in-user">
            ${opts(userPresets, userKnown ? d.username : "root")}
            <option value="__custom" ${userKnown ? "" : "selected"}>${t("in.custom")}</option>
          </select>
          <input id="in-user-custom" class="${userKnown ? "hidden" : ""}" value="${userKnown ? "" : escapeHtml(d.username)}" placeholder="${t("in.username")}" style="margin-top:8px">
        </div>
        <div><label>${t("in.loginPass")}</label><input id="in-pass" type="password" value="${escapeHtml(d.password)}" placeholder="${t("in.passHintKey")}"></div>
      </div>
      <div class="tpl-block">
        <label>${t("in.keyTpl")}</label>
        <div class="tpl-row">
          ${tplChipHTML(credTemplates("key"), d.key_tpl)}
          <button type="button" class="ghost" id="in-save-key">${t("in.saveKey")}</button>
        </div>
      </div>
      <label>${t("in.ssh")}</label>
      <div id="in-keys-box" class="editor-list">
        ${(d.ssh_keys.length ? d.ssh_keys : [""]).map((k, i) => `
          <div class="key-row">
            <input class="ssh-key" value="${escapeHtml(k)}" placeholder="${t("ph.ssh")}">
            <button type="button" class="ghost danger-lite" data-del-key="${i}">${t("btn.delete")}</button>
          </div>`).join("")}
      </div>
      <div class="actions" style="margin-top:10px">
        <button type="button" id="in-add-key">${t("in.addKey")}</button>
        <button type="button" id="in-import-key">${t("in.importKey")}</button>
        <input type="file" id="in-key-file" class="hidden" accept=".pub,text/plain">
      </div>
      <p class="hint">${t("in.step2Hint")}</p>
    `;
  const step2Win = `
      <div class="tpl-block">
        <label>${t("in.acctTpl")}</label>
        <div class="tpl-row">
          ${tplChipHTML(credTemplates("account"), d.account_tpl)}
          <button type="button" class="ghost" id="in-save-acct">${t("in.saveAcct")}</button>
          <button type="button" class="ghost" id="in-manage-tpl">${t("in.manageTpl")}</button>
        </div>
      </div>
      <div class="row">
        <div><label>${t("in.loginUser")}</label>
          <select id="in-user">
            ${opts(userPresets, userKnown ? d.username : "Administrator")}
            <option value="__custom" ${userKnown ? "" : "selected"}>${t("in.custom")}</option>
          </select>
          <input id="in-user-custom" class="${userKnown ? "hidden" : ""}" value="${userKnown ? "" : escapeHtml(d.username)}" placeholder="${t("in.username")}" style="margin-top:8px">
        </div>
        <div><label>${t("in.loginPass")}</label><input id="in-pass" type="password" value="${escapeHtml(d.password)}" placeholder="${t("in.passHintWin")}"></div>
      </div>
      <p class="hint">${t("in.winHint")}</p>
    `;
  const step3Win = `
      <div class="row">
        <div><label>${t("in.wim")}</label>
          <select id="in-wim">
            ${wimList.length ? wimList.map(x => `<option value="${x.index}" ${Number(wimIdx) === Number(x.index) ? "selected" : ""}>${escapeHtml(String(x.index))} · ${escapeHtml(x.name || x.edition || x.flags || ("Image " + x.index))}</option>`).join("") : `<option value="${wimIdx}">${wimIdx}</option>`}
          </select>
          <p class="hint">${t("in.wimHint")}</p>
        </div>
        <div><label>${t("in.keyMode")}</label>
          <select id="in-key-mode">
            <option value="none" ${d.key_mode === "none" || !d.key_mode ? "selected" : ""}>${t("in.keyNone")}</option>
            <option value="custom" ${d.key_mode === "custom" ? "selected" : ""}>${t("in.keyCustom")}</option>
            <option value="kms" ${d.key_mode === "kms" ? "selected" : ""}>${t("in.keyKMS")}</option>
          </select>
        </div>
      </div>
      <div class="row ${d.key_mode === "custom" ? "" : "hidden"}" id="in-pkey-row">
        <div style="flex:1"><label>${t("in.pkey")}</label>
          <input id="in-pkey" value="${escapeHtml(d.product_key || "")}" placeholder="${t("in.pkeyPh")}">
        </div>
      </div>
      <div class="row ${d.key_mode === "kms" ? "" : "hidden"}" id="in-kms-row">
        <div style="flex:1"><label>${t("in.kmsKey")}</label>
          <select id="in-kms-id">
            ${kmsKeyList().map(k => `<option value="${escapeHtml(k.id)}" ${(d.kms_key_id || matchKMSKeyID(img, wimIdx)) === k.id ? "selected" : ""}>${escapeHtml(k.label)}</option>`).join("")}
          </select>
          <p class="hint">${t("in.kmsKeyHint")}</p>
        </div>
      </div>
      <div class="row">
        <div style="flex:1"><label>${t("in.kmsHost")}</label>
          <input id="in-kms-host" value="${escapeHtml(d.kms_host || "")}" placeholder="${t("in.kmsHostPh")}">
          <p class="hint">${t("in.kmsHostHint")}</p>
        </div>
      </div>
      <label class="chk"><input type="checkbox" id="in-rm-defender" ${d.remove_defender ? "checked" : ""}> ${t("in.rmDefender")}</label>
      <p class="hint">${t("in.rmDefenderHint")}</p>
      <div><label>${t("in.disk")}</label>
        <select id="in-disk">
          <option value="" ${!d.disk ? "selected" : ""}>${t("in.disk0")}</option>
          ${disks.map(x => `<option value="${escapeHtml(x.path)}" ${x.path === d.disk ? "selected" : ""}>${escapeHtml(x.path)} · ${fmtBytes(x.size_b)} · ${escapeHtml(x.model || "")}</option>`).join("")}
        </select>
        <p class="hint">${t("in.diskWinHint")}</p>
      </div>
      <div class="editor-head" style="margin-top:18px">
        <h4>${t("in.nic")}</h4>
        <div class="actions">
          <button type="button" class="ghost" id="in-add-nic">${t("in.addNic")}</button>
        </div>
      </div>
      <div id="in-nics-box">${(d.nics.length ? d.nics.filter(n => (n.kind || "ethernet") === "ethernet") : [blankNic()]).map((n, i) => renderNicRow({ ...n, kind: "ethernet" }, i, nics, d.nics)).join("")}</div>
      <p class="hint">${t("in.winNetHint")}</p>
    `;
  const step3Linux = `
      <div><label>${t("in.disk")}</label>
        <select id="in-disk">
          <option value="" ${!d.disk ? "selected" : ""}>${t("in.autoDisk")}</option>
          ${disks.map(x => `<option value="${escapeHtml(x.path)}" ${x.path === d.disk ? "selected" : ""}>${escapeHtml(x.path)} · ${fmtBytes(x.size_b)} · ${escapeHtml(x.model || "")}</option>`).join("")}
        </select>
        <p class="hint">${disks.length ? t("in.diskFromAgent") : t("in.diskNoInv")}</p>
      </div>
      ${whole ? `<p class="hint">${t("in.wholeHint")}</p>` : `
      <div class="editor-head">
        <h4>${t("in.parts")}</h4>
        <button type="button" class="ghost" id="in-reset-parts">${t("in.resetParts")}</button>
      </div>
      ${partBar(d.partitions, (disks.find(x => x.path === d.disk) || disks[0] || {}).size_b)}
      <div id="in-parts-box">${d.partitions.map((p, i) => renderPartRow(p, i)).join("")}</div>
      <button type="button" id="in-add-part" style="margin-top:8px">${t("in.addPart")}</button>
      `}
      <div class="editor-head" style="margin-top:18px">
        <h4>${t("in.nic")}</h4>
        <div class="actions">
          <button type="button" class="ghost" id="in-add-nic">${t("in.addNic")}</button>
          <button type="button" class="ghost" id="in-add-bond">${t("in.addBond")}</button>
          <button type="button" class="ghost" id="in-add-vlan">${t("in.addVlan")}</button>
        </div>
      </div>
      <div id="in-nics-box">${(d.nics.length ? d.nics : [blankNic()]).map((n, i) => renderNicRow(n, i, nics, d.nics)).join("")}</div>
      <p class="hint">${nics.length ? t("in.netHintInv") : t("in.netHintDhcp")}${t("in.netHintBond")}</p>
    `;
  const stepBody = step === 1 ? `
      <div class="row">
        <div><label>${t("in.machine")}</label>
          <select id="in-m">${machines.length ? machines.map(x => `<option value="${x.id}" ${x.id === d.machine_id ? "selected" : ""}>${escapeHtml(x.name)} · ${escapeHtml(x.mac)}</option>`).join("") : `<option value="">${t("in.noMachine")}</option>`}</select>
          <p class="hint">${machineHint(m)}</p>
        </div>
        <div><label>${t("in.image")}</label>
          <select id="in-i">${images.length ? images.map(x => `<option value="${x.id}" ${x.id === d.image_id ? "selected" : ""}>${escapeHtml(x.name)} · ${escapeHtml(osLabel(x) || x.kind || "")}</option>`).join("") : `<option value="">${t("in.noImage")}</option>`}</select>
          <p class="hint">${imageHint(img, d.firmware)}</p>
        </div>
      </div>
      <div class="row3">
        <div><label>${t("in.host")}</label><input id="in-host" value="${escapeHtml(d.hostname)}" placeholder="${win ? t("in.hostPh") : "node-01"}"></div>
        <div><label>${t("m.fw")}</label>
          <select id="in-fw">
            <option value="uefi" ${d.firmware === "uefi" ? "selected" : ""}>UEFI</option>
            <option value="bios" ${d.firmware === "bios" ? "selected" : ""}>${t("m.fw.bios")}</option>
          </select>
        </div>
        <div><label>${t("in.tz")}</label><select id="in-tz">${opts(TIMEZONES, d.timezone)}</select></div>
      </div>
      <label class="chk"><input type="checkbox" id="in-reboot" ${d.reboot ? "checked" : ""}> ${t("in.reboot")}</label>
    ` : step === 2 ? (win ? step2Win : step2Linux) : (win ? step3Win : step3Linux);

  view.innerHTML = `
    <div class="panel">
      <div class="steps">
        <span data-step="1" class="${step === 1 ? "on" : ""}">${t("in.step1")}</span>
        <span data-step="2" class="${step === 2 ? "on" : ""}">${win ? t("in.step2win") : t("in.step2")}</span>
        <span data-step="3" class="${step === 3 ? "on" : ""}">${win ? t("in.step3win") : t("in.step3")}</span>
      </div>
      ${stepBody}
      <div class="actions" style="margin-top:18px">
        ${step > 1 ? `<button type="button" id="in-prev">${t("in.prev")}</button>` : ""}
        ${step < 3 ? `<button type="button" class="primary" id="in-next">${t("in.next")}</button>` : `
          <button type="button" class="primary" id="in-go">${t("in.go")}</button>
          <button type="button" id="in-pxe">${t("in.pxe")}</button>`}
      </div>
    </div>`;

  const goStep = n => { collectInstallForm(); installDraft.step = n; renderInstall(); };
  $$(".steps span[data-step]").forEach(el => {
    el.onclick = () => goStep(Number(el.dataset.step));
  });
  const next = $("#in-next");
  if (next) next.onclick = () => {
    collectInstallForm();
    if (step === 1 && (!installDraft.machine_id || !installDraft.image_id)) return alert(t("in.needBoth"));
    if (step === 2 && isWindowsImage(selectedImage()) && !(installDraft.password || "").trim()) {
      return alert(t("in.needWinPass"));
    }
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
    installDraft.wim_index = isWindowsImage(img) ? defaultWIMIndex(img) : 0;
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
  const keyMode = $("#in-key-mode");
  if (keyMode) keyMode.onchange = () => {
    collectInstallForm();
    if (installDraft.key_mode === "kms") {
      if (!installDraft.kms_key_id) {
        installDraft.kms_key_id = matchKMSKeyID(selectedImage(), installDraft.wim_index || defaultWIMIndex(selectedImage()));
      }
      if (!installDraft.kms_host) installDraft.kms_host = DEFAULT_KMS_HOST;
    }
    renderInstall();
  };
  const wimSel = $("#in-wim");
  if (wimSel) wimSel.onchange = () => {
    collectInstallForm();
    if (installDraft.key_mode === "kms") {
      installDraft.kms_key_id = matchKMSKeyID(selectedImage(), installDraft.wim_index);
    }
    renderInstall();
  };
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
    if (!body.machine_id || !body.image_id) return alert(t("in.needBoth"));
    const img = selectedImage();
    const win = isWindowsImage(img);
    if (win) {
      if (!(body.password || "").trim()) return alert(t("in.needWinPass"));
    } else if (img && img.inspect) {
      const inx = img.inspect;
      if (inx.status === "error") return alert(t("in.imgFail", { msg: inx.message || t("insp.bad") }));
      if (isWholeDiskImage(img)) {
        if (body.firmware === "bios" && inx.status !== "skipped" && !inx.boot_bios) {
          return alert(t("in.noBios"));
        }
        if (body.firmware !== "bios" && inx.status !== "skipped" && !inx.boot_uefi) {
          return alert(t("in.noUefi"));
        }
        const disks = machineDisks(selectedMachine());
        const disk = body.disk ? disks.find(x => x.path === body.disk) : disks.slice().sort((a, b) => (b.size_b || 0) - (a.size_b || 0))[0];
        if (disk && disk.size_b && inx.virtual_size_b && inx.virtual_size_b > disk.size_b) {
          return alert(t("in.tooBig"));
        }
      }
    }
    if (!win && !isWholeDiskImage(img)) {
      const roots = (body.partitions || []).filter(p => p.mount === "/");
      if (roots.length !== 1) return alert(t("in.needRoot"));
    }
    for (const n of (body.network && body.network.nics) || []) {
      if (n.kind === "vlan") {
        if (!n.vlan_id || n.vlan_id < 1 || n.vlan_id > 4094) return alert(t("in.badVlan"));
        if (!n.parent) return alert(t("in.needParent"));
      }
      if (n.kind === "bond" && !(n.bond_members && n.bond_members.length)) return alert(t("in.needMember"));
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
      <p class="hint">${t("st.hint")}</p>
      <label>${t("st.machine")}</label>
      <select id="st-m">${(cache.machines||[]).map(m => `<option value="${m.id}">${escapeHtml(m.name)}</option>`).join("")}</select>
      <div class="row3" style="margin-top:8px">
        <label><input type="checkbox" class="tg" value="cpu" checked> CPU</label>
        <label><input type="checkbox" class="tg" value="memory" checked> ${t("st.mem")}</label>
        <label><input type="checkbox" class="tg" value="disk" checked> ${t("st.disk")}</label>
        <label><input type="checkbox" class="tg" value="network" checked> ${t("st.net")}</label>
      </div>
      <div class="row3">
        <div><label>${t("st.dur")}</label><input id="st-d" value="60"></div>
        <div><label>${t("st.cpu")}</label><input id="st-c" value="0"></div>
        <div><label>${t("st.memPct")}</label><input id="st-mem" value="50"></div>
      </div>
      <div class="row">
        <div><label>${t("st.diskFile")}</label><input id="st-path" placeholder="/tmp/stress.bin"></div>
        <div><label>${t("st.diskMb")}</label><input id="st-ds" value="512"></div>
      </div>
      <button class="primary" id="st-go" style="margin-top:12px">${t("st.go")}</button>
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
  const nameOf = (id) => {
    const m = (cache.machines || []).find(x => x.id === id);
    return m ? (m.name || m.mac || id) : id;
  };
  view.innerHTML = `
    <div class="panel">
      <table>
        <thead><tr><th>${t("j.col.job")}</th><th>${t("j.col.machine")}</th><th>${t("j.col.status")}</th><th>${t("j.col.prog")}</th><th>${t("j.col.msg")}</th><th></th></tr></thead>
        <tbody>${(cache.jobs||[]).length ? (cache.jobs||[]).map(j => `<tr>
          <td>${escapeHtml(j.type)}<div class="hint mono">${escapeHtml(j.id)}</div></td>
          <td>${escapeHtml(nameOf(j.machine_id))}<div class="hint mono">${escapeHtml(j.machine_id)}</div></td>
          <td>${badge(j.status)}</td>
          <td class="mono">${j.progress || 0}%<div class="prog"><i style="width:${Math.max(0, Math.min(100, j.progress || 0))}%"></i></div></td>
          <td>${escapeHtml(j.message || "")}</td>
          <td class="actions">
            <button data-act="log" data-id="${escapeHtml(j.id)}">${t("j.log")}</button>
            <button class="danger" data-act="delete" data-id="${escapeHtml(j.id)}" data-type="${escapeHtml(j.type)}">${t("btn.delete")}</button>
          </td>
        </tr>`).join("") : `<tr><td colspan="6" class="empty">${t("j.empty")}</td></tr>`}</tbody>
      </table>
    </div>`;
  view.onclick = async (ev) => {
    const b = ev.target.closest("button[data-act]");
    if (!b) return;
    const id = b.dataset.id;
    try {
      if (b.dataset.act === "delete") {
        if (!confirm(t("j.delConfirm", { type: b.dataset.type || "", id }))) return;
        await api("/jobs/" + id, { method: "DELETE" });
        await load();
        render();
        return;
      }
      if (b.dataset.act !== "log") return;
      const j = await api("/jobs/" + id);
      openModal(`<h3>${escapeHtml(j.type)} ${badge(j.status)}</h3>
        <p class="hint">${escapeHtml(j.message || "")}</p>
        ${j.result ? `<pre class="log">${escapeHtml(JSON.stringify(j.result, null, 2))}</pre>` : ""}
        <pre class="log">${escapeHtml(j.logs || t("j.nolog"))}</pre>`);
    } catch (e) { alert(e.message); }
  };
}

async function renderBoot() {
  let settings = {};
  try { settings = await api("/settings"); } catch (e) { view.innerHTML = `<div class="panel">${escapeHtml(e.message)}</div>`; return; }
  const dhcp = settings.dhcp || {};
  const st = settings.dhcp_status || {};
  const nics = settings.nics || [];
  const statusBadge = st.running
    ? `<span class="badge ok">${t("boot.running")}</span>`
    : (st.error ? `<span class="badge bad">${t("boot.fail")}</span>` : `<span class="badge">${t("boot.stopped")}</span>`);
  view.innerHTML = `
    <div class="panel">
      <h3>${t("boot.urlTitle")}</h3>
      <p class="hint">${t("boot.urlHint")}</p>
      <div class="row">
        <div><label>Public URL</label><input id="b-url" value="${escapeHtml(settings.public_url || "")}"></div>
        <div><label>${t("boot.token")}</label><input id="b-tok" type="password" placeholder="${t("boot.tokenPh")}"></div>
      </div>
      <p class="hint">${t("boot.tokenHint")}</p>
      <button class="primary" id="b-save" style="margin-top:10px">${t("btn.save")}</button>
    </div>
    <div class="panel" style="margin-top:14px">
      <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
        <h3 style="margin:0">${t("boot.dhcpTitle")}</h3>
        <div>${statusBadge} <span class="hint">${st.running ? escapeHtml(st.interface || "") + " · " + escapeHtml(st.listen || "") : escapeHtml(st.error || t("boot.idle"))}</span></div>
      </div>
      <p class="hint">${t("boot.dhcpHint")}</p>
      <label><input type="checkbox" id="d-on" ${dhcp.enabled ? "checked" : ""}> ${t("boot.enable")}</label>
      <label>${t("boot.if")}</label>
      <select id="d-if">
        <option value="">${t("boot.pickIf")}</option>
        ${nics.map(n => {
          const ips = (n.ipv4 || []).map(x => x.cidr).join(", ") || t("boot.noip");
          const sel = n.name === dhcp.interface ? "selected" : "";
          return `<option value="${escapeHtml(n.name)}" ${sel} data-nic="${encodeURIComponent(JSON.stringify(n))}">${escapeHtml(n.name)} · ${escapeHtml(ips)} ${n.up ? "· UP" : "· DOWN"}</option>`;
        }).join("")}
      </select>
      <p class="hint" id="d-if-hint"></p>
      <div class="row3">
        <div><label>${t("boot.subnet")}</label><input id="d-subnet" value="${escapeHtml(dhcp.subnet || "")}" placeholder="10.0.0.0/24"></div>
        <div><label>${t("boot.gw")}</label><input id="d-gw" value="${escapeHtml(dhcp.router || "")}" placeholder="10.0.0.1"></div>
        <div><label>${t("boot.next")}</label><input id="d-next" value="${escapeHtml(dhcp.next_server || "")}" placeholder="${t("boot.if")} IPv4"></div>
      </div>
      <div class="row3">
        <div><label>${t("boot.poolStart")}</label><input id="d-start" value="${escapeHtml(dhcp.range_start || "")}"></div>
        <div><label>${t("boot.poolEnd")}</label><input id="d-end" value="${escapeHtml(dhcp.range_end || "")}"></div>
        <div><label>${t("boot.dns")}</label><input id="d-dns" value="${escapeHtml(dhcp.dns || "8.8.8.8")}" placeholder="8.8.8.8,1.1.1.1"></div>
      </div>
      <div class="row">
        <div><label>${t("boot.lease")}</label><input id="d-lease" value="${dhcp.lease_sec || 3600}"></div>
        <div><label>${t("boot.listen")}</label><input id="d-listen" value="${escapeHtml(dhcp.listen_addr || "0.0.0.0:67")}"></div>
      </div>
      <div class="actions" style="margin-top:14px">
        <button class="primary" id="d-apply">${t("boot.apply")}</button>
        <button id="d-stop">${t("boot.stop")}</button>
      </div>
    </div>
    <div class="panel" style="margin-top:14px">
      <h3>${t("boot.snipTitle")}</h3>
      <p class="hint">${t("boot.snipHint")}</p>
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
      <p class="hint">${t("boot.tftp", { addr: escapeHtml(settings.tftp_listen || "") })}</p>
    </div>`;
  const fillFromNic = (nic) => {
    const a = (nic.ipv4 || [])[0];
    const hint = $("#d-if-hint");
    if (!a) {
      hint.textContent = t("boot.noIfIp");
      return;
    }
    hint.textContent = t("boot.fillHint", { cidr: a.cidr });
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
      alert(t("boot.saved"));
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
  m.innerHTML = `<div class="sheet">${html}<div class="actions" style="margin-top:12px"><button class="ghost" id="modal-x">${t("btn.close")}</button></div></div>`;
  $("#modal-x").onclick = closeModal;
  m.onclick = (e) => { if (e.target === m) closeModal(); };
}
function closeModal() { $("#modal").classList.add("hidden"); $("#modal").innerHTML = ""; }

function setWho(name) {
  const el = $("#who");
  if (el) el.textContent = name || "—";
}

function showLogin(msg) {
  authed = false;
  const box = $("#login");
  if (box) box.classList.remove("hidden");
  const err = $("#login-err");
  if (err && msg) err.textContent = String(msg).replace(/\s+$/, "");
}

function hideLogin() {
  const box = $("#login");
  if (box) box.classList.add("hidden");
  const err = $("#login-err");
  if (err) err.textContent = "";
}

async function doLogin() {
  const username = ($("#login-user") && $("#login-user").value.trim()) || "";
  const password = ($("#login-pass") && $("#login-pass").value) || "";
  const err = $("#login-err");
  if (err) err.textContent = "";
  try {
    const res = await fetch("/api/v1/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
    const text = await res.text();
    if (!res.ok) {
      if (res.status === 401) throw new Error(t("login.bad"));
      if (res.status === 429) throw new Error(t("login.locked"));
      throw new Error(text || res.statusText);
    }
    const out = text ? JSON.parse(text) : {};
    if ($("#login-pass")) $("#login-pass").value = "";
    setWho(out.username || username);
    authed = true;
    hideLogin();
    await load();
    render();
  } catch (e) {
    if (err) err.textContent = e.message || t("login.fail");
  }
}

function accountForm() {
  const cur = ($("#who") && $("#who").textContent !== "—" ? $("#who").textContent : "admin") || "admin";
  openModal(`
    <h3>${t("acc.title")}</h3>
    <p class="hint">${t("acc.hint")}</p>
    <label>${t("acc.user")}</label><input id="acc-user" value="${escapeHtml(cur)}" autocomplete="username">
    <label>${t("acc.cur")}</label><input id="acc-cur" type="password" autocomplete="current-password">
    <div class="row">
      <div><label>${t("acc.new")}</label><input id="acc-new" type="password" placeholder="${t("acc.keep")}" autocomplete="new-password"></div>
      <div><label>${t("acc.new2")}</label><input id="acc-new2" type="password" placeholder="${t("acc.keep")}" autocomplete="new-password"></div>
    </div>
    <div class="actions" style="margin-top:14px">
      <button class="primary" id="acc-save">${t("btn.save")}</button>
      <button class="ghost" id="acc-close">${t("btn.cancel")}</button>
    </div>`);
  $("#acc-close").onclick = closeModal;
  $("#acc-save").onclick = async () => {
    const username = $("#acc-user").value.trim();
    const currentPassword = $("#acc-cur").value;
    const password = $("#acc-new").value;
    const password2 = $("#acc-new2").value;
    if (!currentPassword) return alert(t("acc.needCur"));
    if (!username) return alert(t("acc.needUser"));
    if (password !== password2) return alert(t("acc.mismatch"));
    try {
      const out = await api("/account", { method: "PUT", body: JSON.stringify({ username, current_password: currentPassword, password }) });
      setWho((out && out.username) || username);
      closeModal();
      alert(t("acc.ok"));
    } catch (e) { alert(e.message); }
  };
}

async function boot() {
  try { applyChrome(); } catch (e) {}
  fetch("/api/v1/health").then(r => r.json()).then(h => {
    if (h && h.version) {
      setVersion(h.version);
      const v = $("#login-ver");
      if (v) v.textContent = h.version;
    }
  }).catch(() => {});
  try {
    const sess = await fetch("/api/v1/session").then(r => r.json());
    if (sess && sess.authenticated) {
      setWho(sess.username);
      authed = true;
      hideLogin();
      await load();
      render();
      return;
    }
  } catch (e) {}
  showLogin();
}

if ($("#login-go")) $("#login-go").onclick = doLogin;
if ($("#login-pass")) $("#login-pass").addEventListener("keydown", e => { if (e.key === "Enter") doLogin(); });
if ($("#login-user")) $("#login-user").addEventListener("keydown", e => { if (e.key === "Enter") { const p = $("#login-pass"); if (p) p.focus(); } });
if ($("#account")) $("#account").onclick = () => { if (authed) accountForm(); };
if ($("#logout")) $("#logout").onclick = async () => {
  try { await fetch("/api/v1/logout", { method: "POST" }); } catch (e) {}
  closeModal();
  setWho("—");
  showLogin();
};

function tickClock() {
  const el = $("#clock");
  if (!el) return;
  el.textContent = new Date().toISOString().replace("T", " ").slice(0, 19) + " Z";
}
tickClock();
setInterval(tickClock, 1000);

boot();
setInterval(() => {
  if (!authed) return;
  load().then(() => { if (["dash", "jobs", "machines"].includes(current)) render(); });
}, 8000);
