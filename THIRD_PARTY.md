# 第三方组件与许可证

## Xray-core

本插件的 Linux 安装包分发未经本项目修改的官方 Xray-core v26.3.27 可执行文件，作为独立进程运行。插件通过标准输入提供 JSON 配置，不将 Xray 作为 Go 模块链接到插件后端。

| 项目 | 信息 |
| --- | --- |
| 上游项目 | [XTLS/Xray-core](https://github.com/XTLS/Xray-core) |
| 固定版本与发布资源 | [v26.3.27 release](https://github.com/XTLS/Xray-core/releases/tag/v26.3.27) |
| 对应版本源码 | [v26.3.27 source tag](https://github.com/XTLS/Xray-core/tree/v26.3.27) |
| 对应版本许可证 | [LICENSE — Mozilla Public License 2.0](https://github.com/XTLS/Xray-core/blob/v26.3.27/LICENSE) |
| MPL-2.0 正文 | [Mozilla Public License, version 2.0](https://www.mozilla.org/MPL/2.0/) |

[构建脚本](./build.py) 下载固定版本的官方 ZIP，并对照脚本内固定的 SHA-256 校验下载内容。Linux amd64 使用 `Xray-linux-64.zip`，Linux arm64 使用 `Xray-linux-arm64-v8a.zip`；`Xray-windows-64.zip` 仅供本地测试内核提取，不进入 Linux 安装包。

打包时，脚本会将官方归档内文件名以 `LICENSE` 或 `NOTICE` 开头的文件保留到安装包的 `licenses/` 目录，并加上 `Xray-` 前缀。分发安装包时应保留这些声明。插件的单节点出站配置不使用 geoip/geosite 数据库，构建脚本也不将它们装入插件包。

Xray-core 自身包含的依赖及其许可证应以该版本上游源码、依赖声明和随附通知为准；本文件不将这些依赖重新授权为插件许可证。分发修改后的 Xray 时，还需按 MPL-2.0 履行适用的源码与通知义务。

## Go、Python 与前端

- 插件后端仅使用 Go 标准库，[go.mod](./go.mod) 未引入外部模块。Go 标准库随工具链提供，其许可见 [Go 官方 LICENSE](https://go.dev/LICENSE)。这不表示独立 Xray 内核没有第三方依赖。
- [构建脚本](./build.py) 仅使用 Python 标准库，不需要 pip 依赖；Python 解释器不随插件安装包分发。Python 许可见 [Python 官方许可说明](https://docs.python.org/3/license.html)。
- 面板使用原生 HTML、CSS、JavaScript，不打包第三方前端框架。

## 插件与内核的许可边界

本项目自有插件代码及文档按根目录 [LICENSE](./LICENSE) 中的 GNU GPL version 3 发布。随包分发、独立运行的 Xray-core 仍按其 MPL-2.0 及适用第三方许可证发布。两者的许可证分别适用于各自组件；插件的 GPL-3.0 声明不替代 Xray 的上游许可证或版权通知。
