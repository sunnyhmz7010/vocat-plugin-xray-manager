<div align="center">
  <h1>VoCat插件-Xray管理</h1>
</div>

<p align="center">
  <a href="https://github.com/sunnyhmz7010/vocat-plugin-xray-manager/releases"><img src="https://img.shields.io/github/v/release/sunnyhmz7010/vocat-plugin-xray-manager?label=Release&color=3b82f6" alt="Release" /></a>
  <img src="https://img.shields.io/badge/Xray-v26.3.27-8b5cf6" alt="Xray v26.3.27" />
  <a href="https://github.com/sunnyhmz7010/vocat-plugin-xray-manager/blob/main/LICENSE"><img src="https://img.shields.io/github/license/sunnyhmz7010/vocat-plugin-xray-manager?color=10b981" alt="License" /></a>
</p>

---

## ✨ 为什么做这个插件

VoCat 的上游代理需要 SOCKS 地址，而常见节点通常以 VLESS、VMess、Trojan 或 Shadowsocks 分享链接提供。这个插件把两者连接起来：粘贴一条链接，由内置 Xray 创建本地 HTTP/SOCKS5 混合服务，并将本地地址写入 VoCat 上游代理列表，然后在 VoCat 中绑定 SIM。

安装包内置 Xray v26.3.27，无需安装 Clash，也无需手工维护另一套代理客户端配置。

## 🚀 核心能力

- 导入 VLESS、VMess AEAD、Trojan 和 Shadowsocks SIP002 单节点链接。
- 按解析器支持的组合使用 RAW、WebSocket、gRPC、XHTTP、HTTPUpgrade、KCP、TLS 与 Reality，具体限制见下文。
- 最多保存 64 个节点；每个启用节点独立运行一个 Xray 进程，并使用默认监听 `127.0.0.1`、无账号密码的 HTTP/SOCKS5 混合服务，可选择纯 HTTP、局域网访问及账号密码认证，混合模式的 UDP 可独立关闭。
- 自动分配或自定义 `127.0.0.1` 上的端口，并持久化保存；支持修改已有节点端口。
- 在侧边栏管理节点的启动、停止、推送与删除；关闭页面后内核继续运行。
- 在新增弹窗和「节点参数」中配置 VLESS 参数；修改链接时保留节点 ID、本地端口及 SIM 绑定。
- 选择 HTTP 或 SOCKS5，经节点实际访问检测网址，查看状态码、耗时和错误；支持自定义地址、1–30 秒超时与取消。

## ⚡ 快速开始

### 📋 前置要求

- 运行 VoCat 的 Linux amd64 或 arm64 设备，以及可管理插件和上游代理的账号。
- VoCat 需具备插件后端、侧边栏贡献与上游代理管理接口。本项目按 VoCat `master` 的 `2bb43de6b8c4de70e2f0d10ca6d6b8adac38a9cf` 插件接口适配，不据此承诺某个已发布版本兼容，也不代表已完成真机验收。
- 选择与 **VoCat 宿主设备** 架构一致的安装包。运行已构建的安装包不需要 Go、Python 或单独安装 Xray。

### 📦 安装与运行

1. 获取或按「本地开发」构建对应安装包：

   | 宿主架构 | 构建产物 |
   | --- | --- |
   | Linux amd64 / x86_64 | `dist/xray-manager-1.1.0-linux-amd64.zip` |
   | Linux arm64 / aarch64 | `dist/xray-manager-1.1.0-linux-arm64.zip` |

2. 在 VoCat 宿主执行：

   ```bash
   sudo vocat develop on
   ```

3. 重启 VoCat，在「系统设置 → 插件」上传对应 ZIP 并启用插件。
4. 打开侧边栏「Xray管理」，点击「新增节点」，在弹窗中粘贴一条节点分享链接；本地端口留空自动分配，或填写 1–65535 的整数。点击「导入并推送到 VoCat」。
5. 前往 VoCat「代理」页面，将新上游代理绑定到对应 SIM，并使用 VoCat 的代理探测确认远端连接可用。

页面路由为 `/extensions/xray-manager/panel`。

界面采用与 VoCat 代理管理一致的工具栏、紧凑表格和新增弹窗布局，跟随同源宿主的深浅主题；手机端表格可横向滚动。操作失败时弹窗保留输入，成功导入后关闭弹窗。

插件名称、侧边栏与页面标题统一为「Xray管理」。插件 ID 为 `xray-manager`，页面 ID 为 `panel`；不修改 VoCat，宿主仍会显示「Xray管理 · 版本号」副标题。

## 📖 使用说明

一次导入一个完整的 `vless://`、`vmess://`、`trojan://` 或 `ss://` 分享链接。链接备注用作节点名称。重复导入相同链接会复用已有记录；不同链接文本可能生成不同记录。

| 操作或状态 | 行为 |
| --- | --- |
| 导入并推送到 VoCat | 解析链接、启动内核、保存节点，再写入宿主上游列表；SIM 绑定由用户在代理页面完成 |
| 内核运行中 | 本地进程及代理就绪检查通过，不表示远端节点可达 |
| 推送到 VoCat 代理管理 / 启动并推送 | 启动节点并创建或更新上游地址和启用状态，按当前认证设置写入或清空 SOCKS 账号密码 |
| 连接设置 | 修改端口、入站模式、UDP、局域网访问及账号密码认证；保留节点 ID 和启停状态，保存后需重新推送以更新 VoCat 地址和凭据 |
| 节点参数 | 修改远端链接；VLESS 可展开表单调整参数，失败保留输入；成功重启运行中的节点，保留本地 ID 与端口 |
| 检测 | 选择 HTTP/SOCKS5 代理协议、目标和超时；请求通过节点出站，显示 HTTP 状态码及响应耗时 |
| 停止 | 先停用宿主上游，再停止本地内核；保留节点记录 |
| 删除 | 确认后先删除宿主上游及其 SIM 绑定，再删除本地节点并停止内核 |
| 刷新状态 | 重新读取本地节点与 VoCat 上游状态 |

节点向 VoCat 代理管理单向推送配置。在宿主代理管理中删除上游条目，不会删除本地节点或停止内核；再次推送会重建上游，但需重新绑定 SIM。要一并删除节点和内核，请在Xray管理页面执行删除。

本地节点和宿主上游通过不同 API 操作，没有跨 API 事务。导入后推送失败时，节点可能已保存并运行，按提示点击「推送到 VoCat 代理管理」重试。删除时也可能出现宿主条目已移除、本地清理失败的情况；面板会明确提示。**重新推送只能重建上游，不能恢复已删除的 SIM 绑定，必须手动重新绑定。**

若 VoCat 上游列表读取失败，表格会显示「状态未知」。若存在同 ID 但地址既不是当前地址也不在本节点历史地址内，或用户名既不是当前账号也不在本节点历史账号内的上游条目，插件会保留该条目并报冲突，请先在宿主中核实处理。

## 🧠 功能细节

### 🔌 自定义本地端口

新增节点时，「本地端口」留空表示自动分配，也可填写 1–65535 的整数。插件检查 TCP，混合模式启用 UDP 时也检查 UDP；其他已保存节点的端口即使停用也会被保留。低位端口是否可用取决于宿主进程权限。重复导入同链接但填写不同端口时会提示在「连接设置」中修改端口。

已有节点点击「连接设置」修改端口、访问模式或账号密码。不同端口变更会先启动候选服务并保存，再关闭旧进程；同端口更改认证或监听范围需要先停止旧服务，失败时保留旧配置并尝试恢复旧服务，恢复失败会明确报错。修改运行中节点会中断现有连接；停用节点修改后仍停用。

保存后需点击「推送到 VoCat 代理管理」或「启动并推送」更新宿主地址和凭据，否则宿主可能暂时无法连接。推送更新原上游 ID，保留已有 SIM 绑定；若推送失败，可直接重试，输入及本地配置不会被删除。历史端口随配置保存，以便重启后仍能识别并更新原条目。

### 🌐 局域网访问与认证

默认模式为 HTTP/SOCKS5 混合，同一端口同时接受 HTTP、CONNECT、SOCKS5。可选择纯 HTTP（HTTP/CONNECT、仅 TCP）供其他客户端使用；VoCat 上游只支持 SOCKS，所以纯 HTTP 不推送到 VoCat，切换时会先停用原上游并保留绑定。两边不具备事务，若停用成功而本地修改失败，界面会明确提示，需重新推送以恢复宿主上游。当前核心没有禁用 HTTP 回落的纯 SOCKS 入站选项。

「允许局域网访问」默认关闭。开启后监听 `0.0.0.0`，其他设备使用 **VoCat 设备的局域网 IP:端口**；VoCat 自身仍使用 `127.0.0.1:端口`。插件不修改宿主防火墙规则。LAN 模式的 UDP ASSOCIATE 使用连接的实际本地地址，依据 [Xray v26.3.27 SOCKS 实现](https://raw.githubusercontent.com/XTLS/Xray-core/v26.3.27/proxy/socks/protocol.go)。

「账号密码认证」可独立开启。账号和密码各为 1–255 UTF-8 字节；首次开启必须填写密码，编辑已有认证时密码留空表示保留。密码不在节点列表返回或回显，只在私有配置中保存并在用户推送时导出给 VoCat。关闭认证会清空本地凭据，重新推送后也清空宿主凭据。为兼容 VoCat，账号首尾不能有空白，账号不能包含 HTTP Basic 分隔符冒号，密码不能是八个星号（宿主的保留标记）。

局域网访问与账号密码认证默认关闭。修改设置时保存之前使用的用户名与端口，以便重新推送时识别原代理条目，更新原 ID 并保留 SIM 绑定。

### 🔗 协议与参数边界

解析器采用白名单，拒绝未知参数、重复查询参数及无效组合，不会静默丢弃不认识的选项。支持范围以 [节点解析实现](./internal/node/node.go) 为准。

| 协议 | 支持范围 |
| --- | --- |
| VLESS | UUID 凭据，`encryption=none` 或核心支持的 `mlkem768x25519plus` 参数；支持 `xtls-rprx-vision`、`xtls-rprx-vision-udp443`，flow 仅可与 RAW + TLS/Reality 组合 |
| VMess | Base64 JSON 分享格式，版本为空或 `2`，仅 AEAD（`aid` 为空或 `0`）；加密支持 `auto`、`aes-128-gcm`、`chacha20-poly1305`、`none`、`zero` |
| Trojan | URL 密码凭据；未指定 `security` 时默认 TLS |
| Shadowsocks | SIP002 userinfo 的明文或 Base64 凭据；支持 `aes-128-gcm`、`aes-256-gcm`、`chacha20-ietf-poly1305` 与三种 `2022-blake3-*` 方法；SS2022 校验密钥长度和编码 |

VLESS、VMess 和 Trojan 支持 RAW（`tcp`）、WS（`websocket`）、gRPC、XHTTP（`splithttp`）、HTTPUpgrade、KCP（`mkcp`）。RAW 可选 `headerType=http` 并填写 `path`/`host`；WS/HTTPUpgrade 支持 `path`/`host`/`ed`；gRPC 支持 `serviceName`、`authority`，mode 为 `gun` 或 `multi`；XHTTP 支持 `path`/`host`，mode 为 `auto`、`packet-up`、`stream-up`、`stream-one`，以及已校验的传输 `extra` JSON。KCP 支持 `seed` 与 `headerType=none/srtp/utp/wechat-video/dtls/wireguard`，插件会转成当前核心的 `finalmask`，不再写入已移除的 KCP 字段。有 seed 时使用 `mkcp-aes128gcm`；显式选择头部但不提供 seed 时使用 `mkcp-original`。两个参数都省略时保留当前核心原生 KCP；头部 `none` 和省略参数并非同一种封装，需要与服务器一致。

TLS 支持 `sni`、核心已知指纹 `fp`、`alpn`，证书固定值 `pcs`（别名 `pinnedPeerCertSha256` / `pinSHA256`）与验证名称 `vcn`（别名 `verifyPeerCertByName`）。固定核心自 2026-06-01 起已移除 `allowInsecure=true`，因此不能提供有效的跳过证书校验开关；旧 `verifyPeerCertInNames` 同样已移除。证书固定值使用 SHA-256 十六进制摘要，多个值以逗号分隔。依据 [固定核心传输配置](https://github.com/XTLS/Xray-core/blob/v26.3.27/infra/conf/transport_internet.go)。

Reality 仅支持 VLESS 的 RAW/XHTTP/gRPC，需要 32 字节 Base64 公钥 `pbk`；`sid` 最多 16 位、偶数长度十六进制。支持 `spx` 和可选的 `pqv` / `mldsa65Verify` 验证公钥。Reality 无 ALPN 配置字段，提供时明确拒绝；未指定指纹时使用 `chrome`。

VLESS `packetEncoding` 提供 `none`（核心默认行为）与 `xudp`（显式配置 XUDP，关闭 TCP mux）选项。`packet` 无对应 Xray 实现，明确拒绝。高级参数不是任意 Xray 出站 JSON 透传；XHTTP `extra` 接纳传输字段和已校验的 `downloadSettings`，不接受 `sockopt`、本地证书文件、递归下载设置或替换出站。主流分享字段支持范围以 [参数映射实现](./internal/node/options.go) 与测试为准，不能保证所有客户端私有字段均兼容。

在「VLESS 参数」中填写「XHTTP 独立下载连接（JSON）」即可启用，清空即可关闭；「其他高级设置」与下载配置分别编辑，但都写回同一分享链接的 `extra`。下载 `address`、整数 `port` 和 `network=xhttp/splithttp` 必须显式提供，**不继承上传连接配置**。`security` 省略时为 `none`，下载路径省略时为 `/`。上传模式不能选 `stream-one`。

```json
{
  "address": "download.example.com",
  "port": 443,
  "network": "xhttp",
  "security": "tls",
  "tlsSettings": {
    "serverName": "download.example.com",
    "fingerprint": "chrome",
    "alpn": ["h2"]
  },
  "xhttpSettings": {
    "path": "/download",
    "host": "download.example.com"
  }
}
```

下载连接支持独立 TLS 或 Reality 设置。JSON 中使用核心字段名，如 `fingerprint`、`mldsa65Verify`，不能使用分享链接别名 `fp`、`pqv`。下载模式由核心固定为 stream-down，`xhttpSettings.mode` 只接纳省略或 `auto`；嵌套 `extra` 与同级传输选项不能混写，以免被核心覆盖。上传、下载入口必须回源到共享 XHTTP 会话的服务端，两个互不关联的 XHTTP listener 无法配对。配置结构依据 [Xray StreamConfig](https://github.com/XTLS/Xray-core/blob/v26.3.27/infra/conf/transport_internet.go)、[下载拨号实现](https://github.com/XTLS/Xray-core/blob/v26.3.27/transport/internet/splithttp/dialer.go) 与 [KCP 封装管理器](https://github.com/XTLS/Xray-core/blob/v26.3.27/transport/internet/finalmask/finalmask.go)。

测试已覆盖 KCP 六种头部选择与有/无 seed 的真实转发、错误 seed 拒绝，以及 XHTTP 两个回环 HTTP 入口回源同一服务端的上下行分离（明文和 TLS 下载，约 120 KB 上传与响应逐字节一致）。下载 Reality/ML-DSA 配置通过固定核心检查；其实际握手、HTTP/3 下载及外部 CDN 行为尚未验收。

不支持订阅 URL、批量订阅导入、Hysteria、TUIC、Shadowsocks SIP002 插件或任意 Xray JSON 配置导入。Xray 内核自身支持的功能不等于本插件允许的分享链接参数。

### ⚙️ 进程与连接

每个节点首次导入时使用指定端口，留空则自动分配可用的 TCP/UDP 端口，随后持久化复用；停止或重新启用不会主动换端口。若端口被其他程序占用，启动会失败并提示错误。

启动会进行 Xray 配置检查、本地进程存活和代理就绪检查，**运行中不代表远端可用**。点击「检测」，可选择通过混合端口的 HTTP 或 SOCKS5 通道（纯 HTTP 仅 HTTP），访问 Google、Cloudflare 或自定义 HTTP/HTTPS 地址。探测仅拨号本地代理，无环境代理、无 direct 回退，不跟随重定向；2xx/3xx 表示收到成功/跳转响应，不代表所有网站可用。耗时覆盖代理连接、目标响应及最多 64 KiB 响应体读取，非 ICMP 延迟。生成的 Xray 配置只有该节点出站。

关闭网页不影响内核运行。停止插件或退出 VoCat 会结束其内核；下次启用插件时恢复保存为 `enabled` 的节点。单个节点启动失败不会阻止其他节点或面板启动。内核意外退出后需点击「启动并推送」重试，没有自动重启机制。

### 🔒 私有数据与卸载

宿主传入的插件数据目录必须符合 `extensions/xray-manager/data` 布局。插件将配置保存在 **同一个 extensions 根目录** 下的 `.xray-manager-private/nodes.json`，不放在已安装插件的静态资源目录中。实际绝对路径取决于 VoCat 的 extensions 根目录；不要按当前工作目录猜测路径。

私有目录权限为 `0700`，保存文件权限为 `0600`。文件包含原始节点链接、节点凭据、端口和启用状态，属于敏感数据；它是明文配置，并非加密存储。配置通过标准输入传给 Xray，不写入进程参数或临时内核配置文件，内核日志输出关闭。

卸载插件会保留私有数据，重新安装并启用时会读取并恢复节点。需要彻底清除时：先在面板逐一删除全部节点并确认宿主上游已清理，再停止插件，确认宿主 extensions 根目录后，手动删除其中的 `.xray-manager-private` 目录。直接删除私有文件不会替你清理宿主上游或 SIM 绑定。

## 🧱 技术栈

- 后端：Go 1.24 或更新版本，仅使用 Go 标准库，无外部 Go 模块依赖。
- 节点内核：Xray-core v26.3.27，随安装包分发，作为独立进程运行。
- 面板：原生 HTML、CSS、JavaScript，通过 VoCat 同源 API 访问后端和上游代理。
- 构建：Python 3.8 或更新版本，仅使用标准库，无需 pip 安装依赖。
- 目标平台：Linux amd64、Linux arm64；manifest 声明 `process.spawn`、`network.connect`、`proxy.write` 权限。

## 🗂️ 项目结构

```text
vocat-plugin-xray-manager/
├── main.go                    # VoCat 后端入口、私有目录定位与 HTTP API
├── go.mod                     # Go 版本与模块声明
├── internal/
│   ├── node/                  # 分享链接解析与单元测试
│   └── engine/                # 节点持久化、Xray 生命周期与测试
├── web/                       # 侧边栏面板
├── vocat-plugin.json          # 插件标识、权限与平台命令
├── build.py                   # 内核校验与分架构打包
├── README.md                  # 安装、使用与开发说明
├── THIRD_PARTY.md             # 第三方组件与许可证说明
├── LICENSE                    # 插件 GPL-3.0 许可证
├── SECURITY.md                # 安全报告渠道
├── .cache/                    # 构建时生成的内核下载缓存及中间产物
└── dist/                      # 构建时生成的安装包与 SHA-256 文件
```

## 👨‍💻 本地开发

### 🧰 环境

安装 Go ≥ 1.24、Python ≥ 3.8，并在本项目根目录执行命令。Python 脚本使用赋值表达式，不兼容 Python 3.7。首次构建需要访问 GitHub 官方 Xray release 下载地址；无需 Node.js、npm 或 pip。

### ⚙️ 命令

构建两个 Linux 架构：

```bash
python3 build.py --arch all
```

仅构建指定架构：

```bash
python3 build.py --arch amd64
python3 build.py --arch arm64
```

已缓存内核时可禁止下载；缓存仍需通过固定 SHA-256 校验：

```bash
python3 build.py --arch all --offline
```

Go 源码检查与单元测试可使用：

```bash
go vet ./...
go test ./...
```

真实内核测试（本地回环 TCP/UDP 转发、生命周期、各协议配置校验）需要提供 Xray 可执行文件：

```bash
XRAY_TEST_BINARY=/absolute/path/to/xray go test ./... -count=1
```

未设置该变量时，真实内核测试会跳过。可选浏览器回归使用 `node browser-test.cjs`，需已有 Playwright；可以用 `PLAYWRIGHT_MODULE` 指定已有模块路径、`CHROMIUM_EXECUTABLE` 指定 Chromium 路径。浏览器测试模拟 VoCat API，覆盖错误恢复和状态一致性，不替代目标设备验收。

构建脚本设置 `GOOS=linux`、对应 `GOARCH` 和 `CGO_ENABLED=0`，从官方归档提取 Xray，并保留上游 LICENSE/NOTICE 文件。每个安装包只声明对应的平台命令，生成同名 `.zip.sha256` 校验文件，并检查 ZIP 不超过宿主的 64 MiB 限制。安装包的 `source/` 目录包含对应插件源码；同时生成独立的 `xray-manager-1.1.0-source.zip`，该源码包用于开发，不用于插件安装。

脚本的 `windows-test` 选项仅提取 Windows 测试内核，不生成 VoCat Windows 插件安装包。修改固定内核版本时，需要同时核实官方资源名、更新固定哈希并复核协议兼容性；不要跳过校验。

以上命令为开发入口，不代表已经在当前环境执行或完成目标设备验收。后端需由 VoCat 插件管理器提供环境变量启动，不是通用独立代理服务。

## 🔐 安全报告

如果发现安全问题，请不要公开披露细节。请优先参考仓库中的 [SECURITY.md](./SECURITY.md) 提交安全报告。

## 📄 许可证

本项目基于 [GPL-3.0](./LICENSE) 开源。

随包分发的独立 Xray-core 内核采用 MPL-2.0，与插件自身的 GPL-3.0 许可证不同。版本、源码和上游许可证见 [THIRD_PARTY.md](./THIRD_PARTY.md)。

<div align="center">
  <sub>Built with ❤️ by Sunny</sub>
</div>
