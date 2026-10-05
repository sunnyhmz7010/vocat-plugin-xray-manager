"use strict";
const BACKEND = "/api/extensions/xray-manager/backend";
const $ = (id) => document.getElementById(id);
let busy = false;
let nodes = [];
let upstreams = [];
let upstreamKnown = false;
let editingPortID = "";
let editingPasswordSet = false;
function csrf() {
  const part = document.cookie.split("; ").find((value) => value.startsWith("vocat_csrf="));
  return part ? decodeURIComponent(part.slice(11)) : "";
}
async function request(path, method = "GET", body, signal) {
  const headers = { Accept: "application/json" };
  if (method !== "GET") { headers["Content-Type"] = "application/json"; headers["X-CSRF-Token"] = csrf(); }
  const response = await fetch(path, { method, headers, signal, credentials: "same-origin", body: body === undefined ? undefined : JSON.stringify(body) });
  let payload;
  try { payload = await response.json(); } catch { throw new Error(`请求失败（HTTP ${response.status}），请检查插件是否已启用。`); }
  if (!response.ok) throw new Error(response.status === 401 ? "登录已过期，请重新登录 VoCat。" : payload.error?.message || `HTTP ${response.status}`);
  return payload.data;
}
function notice(text, error = false) { for (const id of ["notice", "form-notice", "port-notice", "connection-notice", "rename-notice"]) { $(id).textContent = text; $(id).className = error ? "error" : ""; } }
function setBusy(value) {
  busy = value;
  document.querySelectorAll("button").forEach((item) => { item.disabled = value; });
  document.querySelectorAll("select").forEach((item) => { item.disabled = value; });
  document.querySelectorAll("input, textarea").forEach((item) => { if (item.type === "checkbox") item.disabled = value; else item.readOnly = value; });
  if (typeof updateMode === "function") { updateMode("import"); updateMode("node"); }
}
function toggleAuth(prefix, canKeep = false) {
  const enabled = $(`${prefix}-auth`).checked;
  $(`${prefix}-auth-fields`).hidden = !enabled;
  $(`${prefix}-username`).required = enabled;
  $(`${prefix}-password`).required = enabled && !canKeep;
}
function connectionSettings(prefix, port, canKeep = false) {
  const auth = $(`${prefix}-auth`).checked;
  const username = auth ? $(`${prefix}-username`).value : "";
  const password = auth ? $(`${prefix}-password`).value : "";
  const bytes = (value) => new TextEncoder().encode(value).length;
  if (auth && (!username || bytes(username) > 255 || bytes(password) > 255 || (!password && !canKeep))) throw new Error("请输入账号和密码，各限 1–255 字节。");
  if (auth && username !== username.trim()) throw new Error("账号首尾不能包含空白字符。");
  if (auth && username.includes(":")) throw new Error("账号不能包含冒号，HTTP 认证使用冒号分隔账号和密码。");
  if (auth && password === "********") throw new Error("密码不能是八个星号，该值是 VoCat 的密码保留标记。");
  const result = { port, inbound_mode: $(`${prefix}-mode`).value, udp_enabled: $(`${prefix}-udp`).checked, allow_lan: $(`${prefix}-lan`).checked, auth_enabled: auth, username };
  if (!auth || password || !canKeep) result.password = password;
  return result;
}
function element(tag, text, className) { const item = document.createElement(tag); item.textContent = text; if (className) item.className = className; return item; }
// Fluent UI System Icons，来自 VoCat 使用的同一图标包（MIT）。
const iconPaths = {"AddRegular": ["M10 2.5c.28 0 .5.22.5.5v6.5H17a.5.5 0 0 1 0 1h-6.5V17a.5.5 0 0 1-1 0v-6.5H3a.5.5 0 0 1 0-1h6.5V3c0-.28.22-.5.5-.5Z"], "PlayRegular": ["M17.22 8.69a1.5 1.5 0 0 1 0 2.62l-10 5.5A1.5 1.5 0 0 1 5 15.5v-11A1.5 1.5 0 0 1 7.22 3.2l10 5.5Zm-.48 1.75a.5.5 0 0 0 0-.88l-10-5.5A.5.5 0 0 0 6 4.5v11c0 .38.4.62.74.44l10-5.5Z"], "EditRegular": ["M17.18 2.93a2.97 2.97 0 0 0-4.26-.06l-9.37 9.38c-.33.33-.56.74-.66 1.2l-.88 3.94a.5.5 0 0 0 .6.6l3.93-.87c.46-.1.9-.34 1.23-.68l9.36-9.36a2.97 2.97 0 0 0 .05-4.15Zm-3.55.65a1.97 1.97 0 1 1 2.8 2.8l-.68.66-2.8-2.79.68-.67Zm-1.38 1.38 2.8 2.8-7.99 7.97c-.2.2-.46.35-.74.41l-3.16.7.7-3.18c.07-.27.2-.51.4-.7l8-8Z"], "ArrowSyncRegular": ["M11.41 3.64a.5.5 0 0 0 0-.71L9.3.8a.5.5 0 0 0-.7.7l1 1a7.5 7.5 0 0 0-4.08 13.5.5.5 0 0 0 .6-.8A6.5 6.5 0 0 1 10.14 3.5L8.59 5.04a.5.5 0 0 0 .7.7l2.12-2.11ZM8.6 16.36a.5.5 0 0 0 0 .71l2.12 2.12a.5.5 0 0 0 .7-.7l-1-1a7.5 7.5 0 0 0 4.07-13.5.5.5 0 1 0-.59.8A6.5 6.5 0 0 1 9.86 16.5l1.55-1.55a.5.5 0 1 0-.7-.7l-2.12 2.11Z"], "PauseRegular": ["M5 2a2 2 0 0 0-2 2v12c0 1.1.9 2 2 2h2a2 2 0 0 0 2-2V4a2 2 0 0 0-2-2H5ZM4 4a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V4Zm9-2a2 2 0 0 0-2 2v12c0 1.1.9 2 2 2h2a2 2 0 0 0 2-2V4a2 2 0 0 0-2-2h-2Zm-1 2a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1h-2a1 1 0 0 1-1-1V4Z"], "DeleteRegular": ["M8.5 4h3a1.5 1.5 0 0 0-3 0Zm-1 0a2.5 2.5 0 0 1 5 0h5a.5.5 0 0 1 0 1h-1.05l-1.2 10.34A3 3 0 0 1 12.27 18H7.73a3 3 0 0 1-2.98-2.66L3.55 5H2.5a.5.5 0 0 1 0-1h5ZM5.74 15.23A2 2 0 0 0 7.73 17h4.54a2 2 0 0 0 1.99-1.77L15.44 5H4.56l1.18 10.23ZM8.5 7.5c.28 0 .5.22.5.5v6a.5.5 0 0 1-1 0V8c0-.28.22-.5.5-.5ZM12 8a.5.5 0 0 0-1 0v6a.5.5 0 0 0 1 0V8Z"], "DismissRegular": ["m4.09 4.22.06-.07a.5.5 0 0 1 .63-.06l.07.06L10 9.29l5.15-5.14a.5.5 0 0 1 .63-.06l.07.06c.18.17.2.44.06.63l-.06.07L10.71 10l5.14 5.15c.18.17.2.44.06.63l-.06.07a.5.5 0 0 1-.63.06l-.07-.06L10 10.71l-5.15 5.14a.5.5 0 0 1-.63.06l-.07-.06a.5.5 0 0 1-.06-.63l.06-.07L9.29 10 4.15 4.85a.5.5 0 0 1-.06-.63l.06-.07-.06.07Z"]};
// 连通性检测的脉冲线图标，沿用 20px 画布和 currentColor。
iconPaths.Pulse = ["M1.5 9.5h4l2-6a.5.5 0 0 1 .95 0l4 12 2-6a.5.5 0 0 1 .47-.34h3.58v1h-3.22l-2.36 7.16a.5.5 0 0 1-.95 0l-4-12-1.64 4.98a.5.5 0 0 1-.48.34H1.5Z"];
function icon(name) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 20 20"); svg.setAttribute("class", "button-icon"); svg.setAttribute("aria-hidden", "true"); svg.setAttribute("focusable", "false");
  for (const d of iconPaths[name]) { const path = document.createElementNS(svg.namespaceURI, "path"); path.setAttribute("d", d); svg.append(path); }
  return svg;
}
function button(text, action, className = "secondary") {
  const item = element("button", text, className);
  const symbol = text === "停止" ? "PauseRegular" : text === "启动并推送" || text === "启动" ? "PlayRegular" : text === "连接设置" || text === "节点参数" || text === "重命名" ? "EditRegular" : text === "检测" ? "Pulse" : text.includes("删除") ? "DeleteRegular" : text === "推送到 VoCat 代理管理" ? "ArrowSyncRegular" : null;
  if (symbol) item.prepend(icon(symbol));
  if (text === "停止") item.className = "warning";
  if (text === "启动并推送") item.className = "success";
  item.type = "button"; item.disabled = busy; item.addEventListener("click", action); return item;
}
for (const [id, symbol, text] of [["add-node", "AddRegular", "新增节点"], ["refresh", "ArrowSyncRegular", "刷新状态"], ["close-dialog", "DismissRegular", ""], ["close-port", "DismissRegular", ""]]) {
  $(id).replaceChildren(icon(symbol)); if (text) $(id).append(document.createTextNode(text));
}
async function refresh() {
  // 节点列表是插件的权威状态；VoCat 列表失败时仍显示节点并明确推送状态未知。
  nodes = await request(`${BACKEND}/nodes`);
  upstreamKnown = false;
  try { upstreams = await request("/api/upstream-proxies"); upstreamKnown = true; } catch (error) { $("load-state").textContent = `节点已读取；VoCat 上游列表读取失败：${error.message}`; }
  if (upstreamKnown) $("load-state").textContent = nodes.length ? "" : "还没有节点，粘贴链接即可开始。";
  render();
}
function render() {
  $("nodes").replaceChildren();
  for (const node of nodes) {
    const card = element("tr", "", "node");
    const nameCell = element("td", "");
    nameCell.append(element("div", node.name, "node-name"), element("div", node.protocol.toUpperCase(), "protocol"));
    const stateCell = element("td", "");
    stateCell.append(element("span", node.running ? "运行中" : node.enabled ? "启动失败" : "已停止", `badge${node.running ? " running" : ""}`));
    if (node.error) stateCell.append(element("p", node.error, "error"));
    const upstream = upstreamKnown ? upstreams.find((item) => item.id === node.id) : undefined;
    const sync = node.inbound_mode === "http" ? (upstream?.enabled ? "请停用不兼容的上游" : "纯 HTTP · 不推送") : !upstreamKnown ? "状态未知" : upstream ? (upstream.addr !== node.addr ? "地址不一致" : upstream.username !== (node.auth_enabled ? node.username : "") ? "认证不一致" : upstream.enabled ? "已添加" : "已停用") : "未推送";
    const addressCell = element("td", "");
    addressCell.append(element("div", node.addr, "address"), element("div", `${node.allow_lan ? '局域网可访问' : '仅本机'} · ${node.auth_enabled ? '账号认证' : '无认证'}`, "access-mode"));
    addressCell.append(element("div", `${node.inbound_mode === "http" ? "HTTP / CONNECT" : "HTTP / SOCKS5"} · ${node.udp_enabled === false || node.inbound_mode === "http" ? "TCP" : "TCP + UDP"}`, "access-mode"));
    card.append(nameCell, addressCell, stateCell, element("td", sync, "push-state"));
    const actionCell = element("td", "");
    const actions = element("div", "", "actions");
    if (!node.running) actions.append(button("启动", () => run(async () => {
      await request(`${BACKEND}/nodes/${node.id}/start`, "POST");
      notice("本地代理已启动，VoCat 上游配置未更新。");
    })));
    if (node.inbound_mode !== "http") actions.append(button(node.running ? "推送到 VoCat 代理管理" : "启动并推送", () => run(async () => {
      await request(`${BACKEND}/nodes/${node.id}/start`, "POST");
      await syncNode(node.id);
      notice("节点已启动并写入 VoCat。可在代理页面绑定 SIM 和探测连接。");
    })));
    actions.append(button("重命名", () => openRename(node)));
    actions.append(button("节点参数", () => openConnection(node)));
    if (node.running) actions.append(button("检测", () => openProbe(node)));
    if (node.running || node.enabled) actions.append(button("停止", () => run(async () => {
      // 先更新宿主；若宿主拒绝操作，保留正在工作的内核。
      const list = await request("/api/upstream-proxies");
      const owned = ownedUpstream(list, node.id);
      if (owned) await request(`/api/upstream-proxies/${node.id}`, "PATCH", { enabled: false });
      try { await request(`${BACKEND}/nodes/${node.id}/stop`, "POST"); }
      catch (error) { throw new Error(`VoCat 上游已停用，但本地停止失败：${error.message} 请重试停止。`); }
      notice("已停用 VoCat 上游条目并停止节点。");
    })));
    actions.append(button("连接设置", () => {
      editingPortID = node.id;
      $("port-node-name").textContent = node.name;
      $("node-port").value = node.addr.split(":").pop();
      $("node-mode").value = node.inbound_mode || "mixed";
      $("node-udp").checked = node.udp_enabled !== false;
      updateMode("node");
      $("node-lan").checked = !!node.allow_lan;
      $("node-auth").checked = !!node.auth_enabled;
      $("node-username").value = node.username || "";
      $("node-password").value = "";
      editingPasswordSet = !!node.auth_enabled && !!node.password_set;
      $("node-password").placeholder = editingPasswordSet ? "留空保留现有密码" : "请输入密码";
      $("password-hint").textContent = editingPasswordSet ? "留空保留现有密码；填写新密码即可替换。" : "账号和密码各限 255 字节。";
      toggleAuth("node", editingPasswordSet);
      notice(""); $("port-dialog").showModal(); $("node-port").focus();
    }));
    actions.append(button("删除", () => {
      if (card.querySelector(".confirm")) return;
      const confirm = element("div", "", "confirm");
      confirm.append(element("p", "删除将同时移除 VoCat 中的上游代理及其绑定。此操作无法撤销。"));
      confirm.append(button("确认删除", () => run(async () => {
        const list = await request("/api/upstream-proxies");
        const owned = ownedUpstream(list, node.id);
        if (owned) await request(`/api/upstream-proxies/${node.id}`, "DELETE");
        try { await request(`${BACKEND}/nodes/${node.id}`, "DELETE"); }
        catch (error) { throw new Error(`VoCat 上游已不存在，原有 SIM 绑定已移除；本地节点清理失败：${error.message} 请重试删除，或重新推送并手动绑定 SIM。`); }
        notice("节点及 VoCat 上游条目已删除。");
      }), "danger"), button("取消", () => confirm.remove()));
      actionCell.append(confirm);
    }, "danger"));
    actionCell.prepend(actions); card.append(actionCell); $("nodes").append(card);
  }
}
function ownedUpstream(list, id) {
  const existing = list.find((item) => item.id === id);
  const local = nodes.find((item) => item.id === id);
  if (existing && (!local || (![local.addr, ...(local.previous_addrs || [])].includes(existing.addr)) || (!["", local.username || "", ...(local.previous_usernames || [])].includes(existing.username)))) throw new Error("VoCat 中存在同 ID 的其他代理，已保留原条目；请先解决 ID 冲突。");
  return existing;
}
async function syncNode(id) {
  const config = await request(`${BACKEND}/nodes/${id}/export`, "POST");
  const list = await request("/api/upstream-proxies");
  const existing = ownedUpstream(list, id);
  try {
    await request(existing ? `/api/upstream-proxies/${id}` : "/api/upstream-proxies", existing ? "PUT" : "POST", config);
    const saved = (await request("/api/upstream-proxies")).find((item) => item.id === id);
    if (!saved || saved.addr !== config.addr || !saved.enabled || saved.username !== config.username) throw new Error("回读配置与预期不一致");
  } catch (error) { throw new Error(`节点已保留，但 VoCat 推送未确认完成：${error.message} 请点击“推送到 VoCat 代理管理”重试。`); }
}
async function run(action) {
  if (busy) return;
  setBusy(true); notice("正在处理…");
  try { await action(); } catch (error) { notice(error.message, true); }
  try { await refresh(); } catch (error) { $("load-state").textContent = `状态读取失败：${error.message}`; }
  setBusy(false);
}
$("import-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const link = $("link").value.trim();
  if (!link) return;
  const pushToVocat = event.submitter?.id !== "import-local-button";
  const port = $("import-port").value === "" ? 0 : Number($("import-port").value);
  if (!Number.isInteger(port) || port < 0 || port > 65535) { notice("请输入 1 到 65535 的整数端口，或留空自动分配。", true); return; }
  let settings;
  try { settings = connectionSettings("import", port); } catch (error) { notice(error.message, true); return; }
  run(async () => {
    const node = await request(`${BACKEND}/nodes`, "POST", { link, ...settings });
    nodes = [...nodes.filter((item) => item.id !== node.id), node];
    if (!node.running) await request(`${BACKEND}/nodes/${node.id}/start`, "POST");
    try { if (pushToVocat && node.inbound_mode !== "http") await syncNode(node.id); } catch (error) { throw new Error(`节点“${node.name}”已保存。${error.message}`); }
    $("link").value = "";
    $("import-form").reset();
    toggleAuth("import"); updateMode("import"); importEditor.load();
    $("import-dialog").close();
    notice(!pushToVocat ? `“${node.name}”已导入并启动，监听 ${node.addr}，未推送到 VoCat。` : node.inbound_mode === "http" ? `“${node.name}”已启动为 HTTP 代理，监听 ${node.addr}，未推送到 VoCat。` : `“${node.name}”已添加到 VoCat，监听 ${node.addr}。请在代理页面选择 SIM 绑定。`);
  });
});
let renamingID = "";
function openRename(node) {
  renamingID = node.id;
  $("rename-name").value = node.name;
  notice("");
  $("rename-dialog").showModal();
  $("rename-name").focus(); $("rename-name").select();
}
$("rename-form").addEventListener("submit", event => {
  event.preventDefault();
  const id = renamingID, name = $("rename-name").value.trim();
  if (!name || new TextEncoder().encode(name).length > 255 || /[\u0000-\u001f\u007f-\u009f]/.test(name)) {
    notice("请输入非空名称，最多 255 UTF-8 字节，不能包含控制字符。", true); return;
  }
  run(async () => {
    await request(`${BACKEND}/nodes/${id}/name`, "PUT", {name});
    const local = (await request(`${BACKEND}/nodes`)).find(item => item.id === id);
    if (!local || local.name !== name) throw new Error("本地名称回读未匹配，请刷新确认后重试。");
    nodes = nodes.map(item => item.id === id ? local : item);
    let existing;
    try {
      existing = ownedUpstream(await request("/api/upstream-proxies"), id);
      if (existing && existing.name !== name) {
        // PATCH 只支持启用状态，改名须 PUT；显式保留所有原字段。
        await request(`/api/upstream-proxies/${id}`, "PUT", {id, name, addr:existing.addr, username:existing.username, password:existing.password, enabled:existing.enabled});
        const saved = (await request("/api/upstream-proxies")).find(item => item.id === id);
        if (!saved || saved.name !== name || saved.addr !== existing.addr || saved.username !== existing.username || saved.enabled !== existing.enabled || saved.password !== existing.password) throw new Error("上游名称或原配置回读未匹配");
      }
    } catch (error) {
      throw new Error(`本地已重命名，但 VoCat 名称同步未确认：${error.message} 输入已保留，可再次保存重试。`);
    }
    $("rename-dialog").close();
    notice(existing ? "节点与 VoCat 上游已重命名，原绑定和启用状态保留。" : "节点已重命名；尚未推送到 VoCat，未创建上游。");
  });
});
for (const id of ["close-rename", "cancel-rename"]) $(id).addEventListener("click", () => { if (!busy) $("rename-dialog").close(); });
$("rename-dialog").addEventListener("cancel", event => { if (busy) event.preventDefault(); });
$("port-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const port = Number($("node-port").value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) { notice("请输入 1 到 65535 的整数端口。", true); return; }
  const id = editingPortID;
  let settings;
  try { settings = connectionSettings("node", port, editingPasswordSet); } catch (error) { notice(error.message, true); return; }
  run(async () => {
    let disabledUpstream = false;
    if (settings.inbound_mode === "http") {
      const owned = ownedUpstream(await request("/api/upstream-proxies"), id);
      if (owned?.enabled) { await request(`/api/upstream-proxies/${id}`, "PATCH", {enabled:false}); disabledUpstream = true; }
    }
    let updated;
    try { updated = await request(`${BACKEND}/nodes/${id}/settings`, "PUT", settings); } catch(error) { throw new Error(`${disabledUpstream ? "VoCat 上游已停用；" : ""}${error.message}`); }
    $("node-password").value = "";
    $("port-dialog").close();
    if (updated.inbound_mode === "http") { notice(`HTTP 设置已保存，监听 ${updated.addr}。对应 VoCat 上游已停用，绑定保留。`); return; }
    notice(`连接设置已保存，VoCat 本机地址为 ${updated.addr}。${updated.running ? '请点击“推送到 VoCat 代理管理”更新地址和凭据。' : '节点仍处于停止状态；点击“启动并推送”可启用新端口并更新上游地址。'}`);
  });
});
$("import-auth").addEventListener("change", () => toggleAuth("import"));
$("node-auth").addEventListener("change", () => toggleAuth("node", editingPasswordSet));
for (const id of ["close-port", "cancel-port"]) $(id).addEventListener("click", () => { if (!busy) $("port-dialog").close(); });
$("port-dialog").addEventListener("cancel", (event) => { if (busy) event.preventDefault(); });
$("add-node").addEventListener("click", () => { notice(""); $("import-dialog").showModal(); $("link").focus(); });
for (const id of ["close-dialog", "cancel-import"]) $(id).addEventListener("click", () => { if (!busy) $("import-dialog").close(); });
$("import-dialog").addEventListener("cancel", (event) => { if (busy) event.preventDefault(); });
// iframe 不会继承宿主的深色主题类；仅观察同源宿主，不修改宿主页面。
const darkPreference = matchMedia("(prefers-color-scheme: dark)");
function applyTheme() {
  let dark = darkPreference.matches;
  try { if (window.parent !== window) dark = window.parent.document.documentElement.classList.contains("dark"); } catch {}
  document.documentElement.classList.toggle("dark", dark);
}
try { if (window.parent !== window) new MutationObserver(applyTheme).observe(window.parent.document.documentElement, { attributes: true, attributeFilter: ["class"] }); } catch {}
darkPreference.addEventListener("change", applyTheme);
applyTheme();
$("refresh").addEventListener("click", () => run(async () => { notice(""); }));
refresh().catch((error) => { $("load-state").textContent = `读取失败：${error.message}`; });
