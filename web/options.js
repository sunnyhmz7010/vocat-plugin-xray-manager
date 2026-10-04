"use strict";
// 两个入口共用同一链接编辑器；表单直接修改链接，不产生第二份参数配置。
const parameterFields = [
  ["type", "传输方式", ["", "tcp", "raw", "ws", "grpc", "xhttp", "kcp", "httpupgrade"]],
  ["security", "传输安全", ["", "none", "tls", "reality"]],
  ["flow", "VLESS Flow", ["", "xtls-rprx-vision", "xtls-rprx-vision-udp443"]],
  ["packetEncoding", "数据包封装", ["", "none", "xudp"]],
  ["encryption", "VLESS encryption", null, "none 或服务端提供的加密参数"],
  ["sni", "服务器名称（SNI）", null, "与服务端证书匹配"],
  ["fp", "TLS 指纹", null, "例如 chrome、firefox、safari"],
  ["alpn", "TLS ALPN", null, "h2,http/1.1；Reality 不支持"],
  ["pbk", "Reality 公钥", null, "服务端提供的公钥"],
  ["sid", "Reality Short ID", null, "十六进制，可为空"],
  ["spx", "Reality Spider X", null, "/"],
  ["pqv", "Reality ML-DSA-65 验证公钥", null, "可选"],
  ["path", "传输路径", null, "/"],
  ["host", "传输 Host", null, "按服务端配置填写"],
  ["serviceName", "gRPC 服务名", null, ""],
  ["authority", "gRPC Authority", null, ""],
  ["mode", "传输模式", ["", "auto", "packet-up", "stream-up", "stream-one", "gun", "multi"]],
  ["headerType", "传输头类型", ["", "none", "http", "srtp", "utp", "wechat-video", "dtls", "wireguard"]],
  ["seed", "KCP Seed", null, "与服务端一致；留空不设置"],
  ["ed", "WS / HTTPUpgrade Early Data", null, "最大字节数"],
  ["pcs", "TLS 证书 SHA-256 固定值", null, "多个值以逗号分隔"],
  ["vcn", "TLS 证书验证名称", null, "多个名称以逗号分隔"],
  ["downloadSettings", "XHTTP 独立下载连接（JSON）", "textarea", '{"address":"download.example.com","port":443,"network":"xhttp","security":"tls","tlsSettings":{"serverName":"download.example.com"},"xhttpSettings":{"path":"/"}}'],
  ["extra", "XHTTP 其他高级设置（JSON）", "textarea", "如 headers、xmux 等，不需要重复填写下载连接"]
];
class LinkEditor {
  constructor(inputID, mountID) {
    this.input = $(inputID); this.controls = new Map();
    this.details = element("details", "", "parameter-editor");
    this.details.append(element("summary", "VLESS 参数"));
    this.message = element("p", "粘贴 VLESS 链接后可展开调整。其他协议请直接编辑分享链接。", "hint");
    this.details.append(this.message);
    this.grid = element("div", "", "parameter-grid");
    for (const [key, title, choices, placeholder] of parameterFields) {
      const field = element("div", "", choices === "textarea" ? "wide" : "");
      const label = element("label", title); label.htmlFor = `${mountID}-${key}`;
      const control = document.createElement(Array.isArray(choices) ? "select" : choices === "textarea" ? "textarea" : "input");
      control.id = label.htmlFor;
      if (Array.isArray(choices)) for (const value of choices) { const option = element("option", value || "使用默认 / 不设置"); option.value = value; control.append(option); }
      else { if (choices !== "textarea") control.type = "text"; control.placeholder = placeholder || ""; control.autocomplete = "off"; control.spellcheck = false; }
      control.addEventListener("input", () => {
        control.setCustomValidity("");
        try {
          const url = new URL(this.input.value.trim()); if (url.protocol !== "vless:") return;
          if (key === "extra" || key === "downloadSettings") {
            const current = readExtra(url);
            const value = control.value.trim() ? readJSONObject(control.value) : undefined;
            let extra;
            if (key === "downloadSettings") {
              extra = current;
              if (value) extra.downloadSettings = value; else delete extra.downloadSettings;
            } else {
              extra = value || {};
              if (!Object.hasOwn(extra, "downloadSettings") && current.downloadSettings !== undefined) extra.downloadSettings = current.downloadSettings;
            }
            if (Object.keys(extra).length) url.searchParams.set("extra", JSON.stringify(extra)); else url.searchParams.delete("extra");
            this.input.value = url.href;
            this.loadExtra(url, key);
          } else {
            if (control.value === "") url.searchParams.delete(key); else url.searchParams.set(key, control.value);
            this.input.value = url.href;
          }
          if (![...this.controls.values()].some(item => item.validity.customError)) this.message.textContent = "下方选择会更新上面的分享链接；点击保存后才生效。服务端参数必须匹配。";
        } catch {
          control.setCustomValidity("请检查分享链接和 JSON：高级设置必须是有效对象。");
          this.message.textContent = "当前输入尚未写入链接，请修正 JSON；原链接和其他参数已保留。";
        }
      });
      if (key === "fp") {
        const list = document.createElement("datalist"); list.id = `${mountID}-fingerprints`;
        for (const value of ["chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized", "randomizednoalpn"]) { const option = document.createElement("option"); option.value=value; list.append(option); }
        control.setAttribute("list", list.id); field.append(list);
      }
      this.controls.set(key, control); field.append(label, control); this.grid.append(field);
    }
    this.details.append(this.grid, element("p", "支持的参数会转换为 Xray 配置。未知字段和不适用的组合会明确报错，不会自动丢弃。此内核已移除跳过证书验证，不能启用 allowInsecure；可使用证书固定值或验证名称。packet 封装与 Reality ALPN 无对应实现。KCP seed/头部自动转换为核心封装；XHTTP 下载连接留空表示关闭，启用时必须显式填写 address、port 和 network=xhttp，其他参数不会从上传连接继承。独立下载不能配合 stream-one。", "hint"));
    $(mountID).append(this.details); this.input.addEventListener("input", () => this.load()); this.load();
  }
  load() {
    let url; try { url = new URL(this.input.value.trim()); } catch {}
    const enabled = url?.protocol === "vless:";
    this.grid.hidden = !enabled;
    this.message.textContent = enabled ? "下方选择会更新上面的分享链接；点击保存后才生效。服务端参数必须匹配。" : "VLESS 链接可展开调整参数；其他协议请直接编辑分享链接。";
    for (const control of this.controls.values()) control.setCustomValidity("");
    if (!enabled) { for (const control of this.controls.values()) control.value = ""; return; }
    for (const [key, control] of this.controls) {
      if (key === "extra" || key === "downloadSettings") continue;
      const value = url.searchParams.get(key) || "";
      if (control.tagName === "SELECT" && !Array.from(control.options).some(o => o.value === value)) { const option = element("option", value); option.value = value; control.append(option); }
      control.value = value;
    }
    this.loadExtra(url);
  }
  loadExtra(url, editingKey) {
    try {
      const extra = readExtra(url), download = extra.downloadSettings;
      delete extra.downloadSettings;
      if (editingKey !== "downloadSettings" && !this.controls.get("downloadSettings").validity.customError) this.controls.get("downloadSettings").value = download === undefined ? "" : JSON.stringify(download, null, 2);
      if (editingKey !== "extra" && !this.controls.get("extra").validity.customError) this.controls.get("extra").value = Object.keys(extra).length ? JSON.stringify(extra, null, 2) : "";
      // 粘贴完整 extra 对象时把下载配置移至其唯一编辑入口。
      if (editingKey === "extra" && Object.hasOwn(readJSONObject(this.controls.get("extra").value || "{}"), "downloadSettings")) this.controls.get("extra").value = Object.keys(extra).length ? JSON.stringify(extra, null, 2) : "";
    } catch {
      if (!editingKey) { this.controls.get("extra").value = url.searchParams.get("extra") || ""; this.controls.get("downloadSettings").value = ""; }
      this.message.textContent = "链接的 extra JSON 无效，请直接修正分享链接中的 extra。其他参数已保留。";
    }
  }
}
function readJSONObject(text) {
  const value = JSON.parse(text);
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("JSON must be an object");
  return value;
}
function readExtra(url) { return readJSONObject(url.searchParams.get("extra") || "{}"); }
const importEditor = new LinkEditor("link", "import-parameters");
const connectionEditor = new LinkEditor("connection-link", "connection-parameters");
function updateMode(prefix) {
  const http = $(`${prefix}-mode`).value === "http";
  $(`${prefix}-udp`).disabled = busy || http;
  $(`${prefix}-mode-hint`).textContent = http ? "纯 HTTP 仅使用 TCP。切换后会停用此节点对应的 VoCat 上游，保留绑定。" : "SOCKS UDP 可独立关闭；HTTP 请求仍使用 TCP。";
  if (prefix === "import") $("import-button").textContent = http ? "导入并启动 HTTP 代理" : "导入并推送到 VoCat";
}
for (const prefix of ["import", "node"]) { $(`${prefix}-mode`).addEventListener("change", () => updateMode(prefix)); updateMode(prefix); }
let connectionID = "";
async function openConnection(node) {
  await run(async () => {
    const saved = await request(`${BACKEND}/nodes/${node.id}/connection`);
    connectionID = node.id; $("connection-name").textContent = node.name;
    $("connection-link").value = saved.link; connectionEditor.load(); notice("");
    $("connection-dialog").showModal(); $("connection-link").focus();
  });
}
$("connection-form").addEventListener("submit", event => {
  event.preventDefault(); const id = connectionID, link = $("connection-link").value.trim();
  run(async () => {
    await request(`${BACKEND}/nodes/${id}/connection`, "PUT", {link});
    const saved = await request(`${BACKEND}/nodes/${id}/connection`);
    if (saved.link !== link) throw new Error("保存回读未匹配，请刷新后重新检查节点参数。");
    $("connection-dialog").close(); $("connection-link").value = ""; connectionEditor.load();
    notice("节点参数已保存，本地端口和节点 ID 保留。");
  });
});
for (const id of ["close-connection", "cancel-connection"]) $(id).addEventListener("click", () => { if (!busy) $("connection-dialog").close(); });
$("connection-dialog").addEventListener("cancel", event => { if (busy) event.preventDefault(); });
$("connection-dialog").addEventListener("close", () => { $("connection-link").value = ""; connectionEditor.load(); });
let probeID = "", probeController;
function openProbe(node) {
  $("probe-protocol").value = "http";
  $("probe-protocol").querySelector('option[value="socks5"]').disabled = node.inbound_mode === "http";
  probeID = node.id; $("probe-name").textContent = node.name; $("probe-result").textContent = ""; $("probe-result").className = "";
  $("probe-dialog").showModal();
}
$("probe-target").addEventListener("change", () => { if ($("probe-target").value !== "custom") $("probe-url").value = $("probe-target").value; else $("probe-url").focus(); });
$("probe-url").addEventListener("input", () => { $("probe-target").value = "custom"; });
$("probe-form").addEventListener("submit", async event => {
  event.preventDefault(); if (busy) return;
  const url = $("probe-url").value.trim(), timeout_seconds = Number($("probe-timeout").value);
  setBusy(true); $("cancel-probe").disabled = false; $("close-probe").disabled = false;
  $("probe-result").className = ""; $("probe-result").textContent = "正在通过节点代理访问目标…";
  probeController = new AbortController();
  try {
    const result = await request(`${BACKEND}/nodes/${probeID}/probe`, "POST", {url, timeout_seconds, proxy_protocol: $("probe-protocol").value}, probeController.signal);
    $("probe-result").className = result.ok ? "" : "error";
    $("probe-result").textContent = `${result.ok ? "代理访问成功" : "检测未通过"} · ${result.latency_ms} ms${result.status_code ? ` · HTTP ${result.status_code}` : ""}${result.error ? `\n${result.error}` : ""}`;
  } catch (error) { if (error.name !== "AbortError") { $("probe-result").className = "error"; $("probe-result").textContent = error.message; } }
  finally { probeController = undefined; setBusy(false); }
});
for (const id of ["close-probe", "cancel-probe"]) $(id).addEventListener("click", () => { probeController?.abort(); $("probe-dialog").close(); });
$("probe-dialog").addEventListener("cancel", () => probeController?.abort());
