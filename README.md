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
- 选择 HTTP 或 SOCKS5，经节点实际访问 Google 或 Cloudflare 预设检测地址，查看状态码、耗时和错误；支持 1–30 秒超时与取消。

## ⚡ 快速开始

### 📋 前置要求

- 运行 VoCat 的 Linux amd64 或 arm64 设备。
- 选择与 VoCat 宿主设备架构一致的安装包。运行已构建的安装包不需要 Go、Python 或单独安装 Xray。

### 📦 安装与运行

1. 获取或按「本地开发」构建对应安装包：

   | 宿主架构 | 构建产物 |
   | --- | --- |
   | Linux amd64 / x86_64 | `dist/xray-manager-<version>-linux-amd64.zip` |
   | Linux arm64 / aarch64 | `dist/xray-manager-<version>-linux-arm64.zip` |

2. 在 VoCat 宿主执行：

   ```bash
   sudo vocat develop on
   ```

3. 重启 VoCat，在「系统设置 → 插件」上传对应 ZIP 并启用插件。
4. 打开侧边栏「Xray管理」，点击「新增节点」，在弹窗中粘贴一条节点分享链接；本地端口留空自动分配，或填写 1–65535 的整数。点击「导入并推送到 VoCat」。
5. 前往 VoCat「代理管理」页面，将新上游代理绑定到对应 SIM，并使用 VoCat 的代理探测确认远端连接可用。

## 📖 使用说明

一次导入一个完整的 `vless://`、`vmess://`、`trojan://` 或 `ss://` 分享链接，链接备注用作初始名称，之后可独立重命名。重复导入相同链接会复用已有记录。

| 操作 | 说明 |
| --- | --- |
| 导入并推送到 VoCat | 解析链接、启动节点并写入 VoCat 上游代理列表 |
| 导入 | 解析链接并启动节点，不写入 VoCat 上游代理列表 |
| 启动 | 只启动本地 Xray，VoCat 上游配置及启用状态保持不变 |
| 启动并推送 | 启动已有节点，并创建或更新对应的 VoCat 上游代理 |
| 连接设置 | 修改本地端口、入站模式、UDP、局域网访问和认证 |
| 重命名 | 不修改链接或重启 Xray；同步已有上游名称，保留 ID、凭据、启用状态及 SIM 绑定；宿主可能触发 VoWiFi 重连，同步失败可重试 |
| 节点参数 | 修改远端节点链接并保留独立名称；VLESS 可直接调整常用参数 |
| 检测 | 通过 HTTP 或 SOCKS5 实际访问 Google / Cloudflare 预设目标，查看状态码、耗时和错误 |
| 停止 | 停用 VoCat 上游代理并停止本地 Xray 进程，保留节点记录 |
| 删除 | 删除 VoCat 上游代理及 SIM 绑定，并删除本地节点 |

节点配置由插件单向推送到 VoCat。在 VoCat「代理管理」中直接删除上游代理，不会删除插件中的节点；再次推送可以重建上游代理，但已删除的 SIM 绑定需要手动重新绑定。

## 🧠 功能细节

### 🔌 本地端口

新增节点时，本地端口留空会自动分配，也可以填写 1–65535 的端口。端口会持久化保存，停止或重新启动节点不会主动更换。

已有节点可在「连接设置」中修改端口。修改运行中的节点可能中断现有连接；保存后需要重新推送到 VoCat，以更新上游代理地址。

### 🌐 局域网访问与认证

默认使用 HTTP/SOCKS5 混合入站并监听 `127.0.0.1`。也可以切换为纯 HTTP；由于 VoCat 上游代理使用 SOCKS，纯 HTTP 模式不会推送到 VoCat。

开启「允许局域网访问」后监听 `0.0.0.0`，其他设备可通过 **VoCat 设备的局域网 IP:端口** 访问。插件不会自动修改宿主防火墙规则。

账号密码认证可独立开启。密码不会在节点列表中回显；编辑已有认证时密码留空表示保留原密码。

### 🔗 协议支持

| 协议 | 支持范围 |
| --- | --- |
| VLESS | UUID、Vision、TLS、Reality 等常用参数 |
| VMess | Base64 JSON、AEAD |
| Trojan | URL 分享链接，支持 TLS 等常用配置 |
| Shadowsocks | SIP002，支持常见 AEAD 与 SS2022 加密方式 |

VLESS、VMess 和 Trojan 支持 RAW、WebSocket、gRPC、XHTTP、HTTPUpgrade 和 KCP，并支持 TLS；VLESS 额外支持 Reality。解析器采用白名单校验，不保证兼容所有客户端的私有参数。

不支持订阅 URL、批量订阅导入、Hysteria、TUIC、Shadowsocks SIP002 插件或任意 Xray JSON 配置导入。

### ⚙️ 进程与连接

每个启用节点独立运行一个 Xray 进程。节点显示「运行中」只代表本地进程和代理入口正常，不代表远端节点一定可用；可使用「检测」功能通过 HTTP 或 SOCKS5 实际访问目标网址确认连接。

关闭网页不会停止节点。停止插件或退出 VoCat 会结束 Xray 进程，下次启用插件时会恢复之前启用的节点。内核意外退出后需要手动重新启动，没有自动重启机制。

### 🔒 私有数据与卸载

节点配置保存在 VoCat extensions 根目录下的 `.xray-manager-private/nodes.json`。文件包含节点链接、凭据、端口和启用状态，属于敏感数据，并以明文形式保存。

卸载插件会保留这些数据，重新安装后可以继续读取。若需要彻底清除，请先在面板删除节点并确认 VoCat 上游代理已清理，再手动删除 `.xray-manager-private` 目录。

## 🧱 技术栈

- 后端：Go 1.24 或更新版本，仅使用 Go 标准库，无外部 Go 模块依赖。
- 节点内核：Xray-core v26.3.27，随安装包分发，作为独立进程运行。
- 面板：原生 HTML、CSS、JavaScript，通过 VoCat 同源 API 访问后端和上游代理。
- 构建：Python 3.8 或更新版本，仅使用标准库，无需 pip 安装依赖。
- 目标平台：Linux amd64、Linux arm64；manifest 声明 `process.spawn`、`network.connect`、`proxy.write` 权限。

## 🗂️ 项目结构

```text
vocat-plugin-xray-manager/
├── .github/
│   ├── ISSUE_TEMPLATE/        # 双语 Bug / 功能建议表单与安全报告入口
│   └── workflows/
│       └── release.yml        # 发布 Release 时构建双架构插件包
├── internal/
│   ├── node/                  # 分享链接解析、参数校验和 Xray 配置映射
│   └── engine/                # 节点持久化、端口管理、进程生命周期和连通性检测
├── web/
│   ├── panel.html             # 面板结构
│   ├── panel.css              # 面板样式
│   ├── panel.js               # 面板交互和 API 调用
│   └── options.js             # VLESS 参数编辑器
├── main.go                    # VoCat 后端入口、私有目录定位与 HTTP API
├── go.mod                     # Go 模块定义
├── vocat-plugin.json          # 插件标识、权限与平台命令
├── build.py                  # 内核校验与分架构打包
├── browser-test.cjs          # 浏览器交互回归测试
├── README.md                 # 安装、使用与开发说明
├── THIRD_PARTY.md            # 第三方组件与许可证说明
├── LICENSE                   # 插件 GPL-3.0 许可证
└── SECURITY.md               # 安全报告渠道
```

`.cache/` 和 `dist/` 仅用于本地构建缓存及生成的安装包，不属于源码目录。

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

构建脚本设置 `GOOS=linux`、对应 `GOARCH` 和 `CGO_ENABLED=0`，从官方归档提取 Xray，并保留上游 LICENSE/NOTICE 文件。每个安装包只声明对应的平台命令，生成同名 `.zip.sha256` 校验文件，并检查 ZIP 不超过宿主的 64 MiB 限制。安装包的 `source/` 目录包含对应插件源码；同时生成独立的 `xray-manager-<version>-source.zip`，该源码包用于开发，不用于插件安装。

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
