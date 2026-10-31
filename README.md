**简体中文** | [繁體中文](README.zh-TW.md) | [English](README.en.md)

# Veyra

自建的代理管理面板：用一个网页后台管理多台代理服务器（节点）、客户账号、套餐和订阅链接。适合自己用，也适合小规模地给别人开账号。

[![CI](https://github.com/Zayvian/veyra-panel/actions/workflows/ci.yml/badge.svg)](https://github.com/Zayvian/veyra-panel/actions/workflows/ci.yml)

## 功能

- **协议**：VLESS Reality、VLESS WS（套 CDN）、Hysteria2（混淆、伪装网站、跳端口）、AnyTLS、Shadowsocks 2022、TUIC v5
- **用户**：到期时间、流量上限、每月自动重置、同时在线 IP 限制、按入站分配权限、一键续费、套餐模板
- **订阅**：Clash / mihomo、sing-box、Shadowrocket、通用分享链接，自动识别客户端；Clash 和 sing-box 自带分组（AI、YouTube、Netflix 等）和国内直连规则；二维码和可直接发给客户的话术；订阅公告；账号失效时显示原因
- **节点**：流量倍率（如 0.5 倍、不扣流量）、站内中转、每台节点的流量统计
- **不断线**：增删用户、续费、改密码都是热更新，不会断开其他人的连接；升级面板也不影响客户
- **安全**：管理后台可隐藏路径、两步验证、单个用户重置链接、紧急时全员更换密码
- **安装简单**：一条命令安装，直接下载编译好的程序（amd64 / arm64），服务器上不编译

## 快速开始

需要 Debian / Ubuntu 服务器、root 权限，以及解析到服务器的域名。

**面板**（在面板服务器上）：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash
```

**节点**（在每台代理服务器上，先在面板「节点」页新建节点、复制接入 token）：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash
```

**升级**：同样的命令后面加 `-s -- --upgrade`。

## 文档

| 文档 | 内容 |
| --- | --- |
| [安装](docs/zh-CN/install.md) | 准备域名和端口、安装面板和节点、证书、面板和节点同机 |
| [使用指南](docs/zh-CN/guide.md) | 每个页面、每个功能怎么用：用户、套餐、节点、入站、限制、设置、订阅 |
| [维护](docs/zh-CN/maintenance.md) | 升级、备份恢复、忘记密码、卸载、排查问题 |
| [通信协议](protocol/PROTOCOL.md) | 面板和节点之间的消息格式（开发者） |

## 仓库结构

| 目录 | 内容 | 许可证 |
| --- | --- | --- |
| [`panel/`](panel/) | 管理面板 | AGPL-3.0 |
| [`node/`](node/) | 节点程序，内置 sing-box | GPL-3.0 |
| [`protocol/`](protocol/) | 面板和节点之间的协议说明和样例 | — |
| [`deploy/`](deploy/) | 共用的 `veyra` 管理菜单和下载校验脚本 | — |

节点内置的协议内核是 [veyra-core](https://github.com/Zayvian/veyra-core)：加了「改用户不用重启」等补丁的 sing-box。它单独一个仓库，由 [`node/core.lock`](node/core.lock) 锁定版本。

**发布**：每次合并到 `main` 并通过测试后，CI 会按 `core.lock` 编译面板和节点的 linux amd64 / arm64 版本，发布为一个新的 [Release](https://github.com/Zayvian/veyra-panel/releases)，附带 `sha256sums.txt`。安装脚本下载后会先校验再安装。

## 许可证

`panel/` 使用 AGPL-3.0，`node/` 使用 GPL-3.0，各自的 `LICENSE` 和 `NOTICE` 在各自目录里。面板不包含任何 sing-box 代码（CI 会检查），两者只通过网络消息通信。

本项目基于 [kosje/skysbx-panel](https://github.com/kosje/skysbx-panel) 和 [kosje/skysbx-node](https://github.com/kosje/skysbx-node) 开发，保留原作者署名，不代表原作者参与或认可本版本。与 sing-box 项目无关，请不要把本项目的问题报告给 sing-box。

问题和建议请提交到 [Issues](https://github.com/Zayvian/veyra-panel/issues)。
