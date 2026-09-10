# skysbx-panel

由 **kosje** 开发，我二次开发的代理管理面板：管理节点、用户、套餐额度、节点倍率与订阅。

[![Panel CI](https://github.com/zayvian-lee/skysbx-panel/actions/workflows/ci.yml/badge.svg)](https://github.com/zayvian-lee/skysbx-panel/actions/workflows/ci.yml)

| 组件 | 作用 | 安装位置 |
| --- | --- | --- |
| **本仓库：面板** | 管理用户、配置节点、生成订阅、计算扣量 | 面板服务器 |
| [skysbx-node](https://github.com/zayvian-lee/skysbx-node) | 运行代理、接收配置、上报流量 | 每台代理服务器安装一份 |
| [skysbx-core](https://github.com/zayvian-lee/skysbx-core) | 节点内置协议内核 | 不单独安装；随节点编译、更新 |

支持 VLESS Reality、AnyTLS、Shadowsocks 2022、Hysteria2、TUIC v5。Hysteria2 支持 UDP 端口跳跃，**TUIC 当前使用固定端口**。支持中文节点名称、0–100 倍流量计费和独立 HTTPS 订阅域名。

## 阅读顺序

1. [准备服务器和域名](#1-准备服务器和域名)
2. [安装面板](#2-安装面板)
3. [登记并安装节点](#3-登记并安装节点)
4. [配置协议和端口](#4-配置协议和端口)
5. [用户、倍率和订阅](#5-用户倍率和订阅)
6. [面板与节点同机安装](#6-面板与节点同机安装)
7. [更新与旧版迁移](#7-更新与旧版迁移)
8. [日常检查和故障排查](#8-日常检查和故障排查)

## 1. 准备服务器和域名

安装脚本面向 **Debian / Ubuntu + systemd**。以下命令在服务器 SSH 终端执行，先运行 `sudo -i` 切换到 root。首次构建需要访问 GitHub、Go 依赖源、Docker 镜像源；脚本会安装构建依赖，服务运行本身不在 Docker 内。

本文默认面板、节点分开部署。同机部署请直接看第 6 节。下面的示例域名必须换成自己的真实域名：

| 示例 | DNS 解析到哪里 | 用途 |
| --- | --- | --- |
| `panel.example.com` | 面板服务器 IP | 管理员登录、节点连接面板 |
| `sub.example.com` | 面板服务器 IP | 可选的独立订阅域名 |
| `hk.example.com` | 香港节点服务器 IP | 客户端连接节点、节点 TLS 证书 |

添加 DNS A 记录；仅当服务器 IPv6 确实可达时才添加 AAAA。本文采用直连方式，Cloudflare 设为 **DNS only / 灰云**。

| 服务器 | 需要的端口 | 说明 |
| --- | --- | --- |
| 面板 | TCP 80、443 | 自动证书、HTTPS 管理和订阅；安装前不能被其他程序占用 |
| 节点 | TCP 80 | 默认 HTTP 证书验证使用；DNS 验证可不开放 |
| 节点 | 配置的代理 TCP / UDP 端口 | 同时检查云安全组和本机防火墙，具体见第 4 节 |

节点主动连接面板的 HTTPS 地址，不需要额外开放节点控制端口。检查监听占用：

```bash
ss -lntp | grep -E ':(80|443)\b'
```

已有 Nginx、Caddy 等占用端口时，先规划部署方式；默认安装脚本让面板直接使用 80 / 443。

## 2. 安装面板

**在面板服务器执行：**

```bash
apt-get update && apt-get install -y curl
curl -fL https://raw.githubusercontent.com/zayvian-lee/skysbx-panel/main/install.sh -o /tmp/skysbx-panel-install.sh
sh /tmp/skysbx-panel-install.sh \
  --domain panel.example.com \
  --sub-domain sub.example.com \
  --email you@example.com
```

不需要独立订阅域名时，删除 `--sub-domain sub.example.com` 这一行，订阅将使用面板域名。

安装过程提示创建管理员账号、密码，随后编译程序、申请证书、启动服务。管理员在服务开放前创建；更新不会重新创建管理员。首次构建可能需要数分钟，取决于服务器和下载速度。

完成后访问 `https://panel.example.com/login`，使用刚创建的账号登录。检查：

```bash
systemctl is-active skysbx-panel
curl -I https://panel.example.com/login
journalctl -u skysbx-panel -n 50 --no-pager
```

服务应为 `active`，浏览器证书应有效。独立订阅域名的 `/login` 返回 404 是正常行为，它只开放 `/sub/`；完整订阅链接在创建用户后获得。

## 3. 登记并安装节点

### 3.1 在面板里登记

进入「节点」，新建记录，例如：

| 字段 | 示例 |
| --- | --- |
| 名称 | 香港无限流量 |
| 客户端连接地址 | `hk.example.com`，不带 `https://` |
| 国家 | `HK` |
| 流量倍率 | 普通节点填 `1`，免扣套餐节点填 `0` |

保存后复制 **接入 token**，它只显示一次。不要把 token 放进公开仓库或截图；丢失时在节点列表「换 token」，再更新服务器配置。每台节点使用独立 token。

### 3.2 在对应节点服务器安装

下面的 `hk.example.com` 应解析到**这台节点服务器**。token 由安装程序交互提示输入，粘贴上一步的值：

```bash
apt-get update && apt-get install -y curl
curl -fL https://raw.githubusercontent.com/zayvian-lee/skysbx-node/main/install.sh -o /tmp/skysbx-node-install.sh
sh /tmp/skysbx-node-install.sh \
  --panel https://panel.example.com \
  --domain hk.example.com \
  --email you@example.com
```

安装会拉取节点和配套内核源码、编译程序、申请节点证书并启动 `skysbx-node`。**AnyTLS、Hysteria2、TUIC 都需要节点证书**。只用 Reality 或 Shadowsocks 时可省略 `--domain`，在域名提示处回车。

回到面板确认节点显示「在线」，并检查：

```bash
systemctl is-active skysbx-node
journalctl -u skysbx-node -n 50 --no-pager
ls -l /opt/skysbx/cert.pem /opt/skysbx/key.pem
```

最后一条仅对使用 TLS 证书的节点适用。**在线不代表代理已可用**：还需添加入站。证书申请失败时节点服务可能仍在线，必须修复证书后再使用 TLS 协议。

无法用 TCP 80 申请证书或已有自己的证书时，见 [节点证书选项](https://github.com/zayvian-lee/skysbx-node#证书选项)。

## 4. 配置协议和端口

在节点列表点击「入站」，按需添加协议。**每个入站对应客户端中的一个代理条目**。

| 协议 | 示例端口 | 防火墙放行 | 主要设置 |
| --- | --- | --- | --- |
| VLESS Reality | `443` | TCP 443 | Reality 握手站点，先保留面板默认值 |
| AnyTLS | `8443` | TCP 8443 | 证书、私钥、SNI |
| Shadowsocks 2022 | `8388` | TCP 和 UDP 8388 | 密钥自动生成 |
| Hysteria2 | `8443` | UDP 8443 | 证书、SNI，可选跳端口范围 |
| TUIC v5 | `9443` | UDP 9443 | 证书、SNI；当前固定端口 |

按实际配置放行，表中只是示例。同一 TCP 端口不能被两个监听服务占用。同机面板已经占用 TCP 443 时，Reality 可以改成 TCP 10443。不要关闭整个防火墙来代替放行所需端口。

- **tag / 入站名称**：填客户端要显示的名称，例如 `香港下载 01`，支持中文，不追加用户名和流量；同一面板内名称必须唯一。
- **已有记录改名**：用户和节点点击行尾「编辑 → 名称 → 保存」；入站点击「节点 → 入站数量 → 编辑 → 入站名称 → 保存」。用户名支持 1–32 位英文字母、数字、点、横线、下划线，并以字母或数字开头；节点和入站名称支持中文。改名保留用户凭据、订阅 token 和已用流量。入站改名会重建相关监听，客户端更新订阅后显示新名称。节点改名会自动重新生成该节点所有入站名称（包括自定义名称），需要自定义时先改节点名称，再改各入站名称。
- **证书、私钥路径**：默认是节点上的 `/opt/skysbx/cert.pem`、`/opt/skysbx/key.pem`，通常留空使用默认值。
- **SNI**：与节点证书匹配，如 `hk.example.com`，与订阅域名无关。
- **站内中转、外部中转地址**：普通直连节点保持为空，不要填写订阅域名。

### Hysteria2 跳端口

示例：监听 `8443`，范围 `20000-30000`，间隔 `30s`。

1. 节点为 Linux，已安装 `nftables`，systemd 服务具有 `CAP_NET_ADMIN`；安装脚本会配置这些依赖。
2. 云安全组放行 UDP 8443、UDP 20000–30000；本机 INPUT 防火墙允许实际监听 UDP 8443。
3. 保存后，节点把跳跃范围重定向到监听端口。规则只负责重定向，不代替安全组或防火墙放行。
4. 客户端刷新订阅。Mihomo / sing-box 完整配置包含间隔；URI 分享链接导入使用客户端支持的间隔设置。

范围可写 `20000-30000,40000`，不能重叠或与其他入站、中转端口冲突。清空范围关闭跳端口。**当前仅支持直连，不与中转同时使用**；每台主机运行一个受面板管理的节点实例。

保存后检查入站是否生效。「未生效」时先查看错误和节点日志。修改端口、证书、跳跃范围等会重建节点配置，现有连接可能中断。

## 5. 用户、倍率和订阅

进入「用户」新建用户。用户名使用英文字母、数字等界面允许的字符；**中文支持针对节点和入站名称，用户名称规则未改动**。

1. 填到期日、流量上限、同时在线 IP 限制；空额度或 `0` 表示不限流量。
2. 套餐 200 G，在「流量上限 GiB」填 `200`。本项目一个套餐单位按 1024³ 字节计算，订阅 GB 状态使用相同换算。
3. 月度套餐选择流量重置日；不重置表示持续累计。重置不会延长到期时间。
4. 在用户的入站分配功能中选择可用入站。选择「全部入站（含以后新建）」表示不限；选择「仅允许勾选项」则只开放所选入站，新建入站不会自动加入。顶部可「全选当前入站 / 清空勾选」，每台服务器可「全选 / 取消 / 仅选此节点」。例如只开放日本：在日本那一组点击「仅选此节点 → 保存」。清空勾选用于重新选择，不能直接保存为空；需要全部禁用时停用用户。

**订阅输出顺序**：在「节点 → 编辑 → 订阅排序」给每台服务器设置数字，例如香港 `10`、日本 `20`、其他服务器 `30`、`40`。数字越小越靠前，默认 `0`（因此也会排在 `10` 前面），同值按节点创建顺序。服务器内部顺序在「入站 → 编辑 → 节点内排序」设置，同值按入站名称排列。排序对所有用户和导出格式生效，但仍按各用户权限过滤；只改排序不会重启节点。客户端更新订阅后查看，若客户端自行按延迟或名称排序，请切回订阅原始顺序。
5. 复制该用户订阅链接，导入客户端并刷新。

### 倍率扣量

在「节点 → 编辑」设置，对该节点全部用户和入站生效：

| 倍率 | 实际上传 + 下载共 100 GB，套餐扣多少 |
| --- | --- |
| `1` | 100 GB |
| `0.1` | 10 GB |
| `0` | 0 GB |
| `2` | 200 GB |

支持 0–100，最多三位小数。实际流量仍写入历史；用户额度、超额停用、订阅用量使用计费流量。修改倍率从新收到的上报开始生效，不重算历史，不重启节点。

**0 倍率只是不扣额度**：用户已停用、到期或在其他节点耗尽套餐，仍不能连接。中转使用最终认证用户的落地节点倍率。月度/手动重置一起清除当前周期计费上传、下载和小数余额。

### 客户端格式

| 客户端 | 如何导入 |
| --- | --- |
| v2rayN | 新建订阅分组，粘贴链接并更新；可指定 `?format=base64` |
| Clash Party / Mihomo | 导入远程订阅；可指定 `?format=clash` |
| Shadowrocket | 添加 Subscribe，粘贴链接并更新；隐藏 User-Agent 时指定 `?format=shadowrocket` |
| sing-box | 远程配置使用 `?format=singbox` |
| 浏览器 | 打开订阅网页；必要时指定 `?format=html` |

已有查询参数时使用 `&format=...`，否则使用 `?format=...`。客户端内核需支持所选协议，原版旧 Clash 不在兼容范围内。

导出只显示入站名称。Shadowrocket 专用格式显示「上传：1.00 GB | 下载：2.00 GB | 总量：200.00 GB」，无限套餐显示「不限」。其他格式保留标准字节响应头，由客户端决定显示单位。具体 iOS 版本效果需在设备刷新验证。

## 6. 面板与节点同机安装

**选择同机方案时，用本节代替第 2、3 节的安装命令。** 面板和可选订阅域名解析到同一台服务器：

```bash
apt-get update && apt-get install -y curl
curl -fL https://raw.githubusercontent.com/zayvian-lee/skysbx-panel/main/install-panel-and-node.sh -o /tmp/skysbx-all-install.sh
sh /tmp/skysbx-all-install.sh \
  --domain panel.example.com \
  --sub-domain sub.example.com \
  --email you@example.com
```

流程：创建管理员 → 面板上线 → 浏览器登录并新建节点 → 回终端粘贴 token → 节点安装完成。

节点连接地址、TLS 入站 SNI 都填 `panel.example.com`。同机脚本让节点复用面板证书，不再单独申请；**不要传另一个节点域名或让 certbot 抢占 TCP 80**。代理 TCP 端口避开面板的 80 / 443，例如 Reality 10443、AnyTLS 8443。

同机证书使用文件链接。面板续签后确认节点加载了有效证书，必要时执行 `systemctl restart skysbx-node`，会短暂中断连接。

## 7. 更新与旧版迁移

**更新前先按 [备份与恢复](docs/BACKUP.md) 备份**。包括数据库、配置、证书和当前程序；不要在数据库运行时只复制主文件、遗漏 WAL。

| 当前情况 | 更新方式 |
| --- | --- |
| 本项目普通面板功能更新 | 根据提交/发布说明只更新面板 |
| 旧版没有 HY2 / TUIC 或跳端口支持 | 先更新面板，再更新每台节点，最后新增协议 |
| 通过本项目同机脚本安装 | 可用同机更新命令更新两个服务 |
| 同一项目的旧仓库版本、默认目录和服务 | 备份后执行本项目更新命令，切换源码来源 |
| 自定义路径、容器安装或其他面板 | 不直接套用，先看 [迁移说明](docs/UPGRADE.md) |

### 更新面板：在面板服务器执行

```bash
curl -fL https://raw.githubusercontent.com/zayvian-lee/skysbx-panel/main/install.sh -o /tmp/skysbx-panel-install.sh
sh /tmp/skysbx-panel-install.sh --upgrade
```

脚本读取 `/opt/skysbx/panel.env` 的域名，更早版本可从 systemd unit 读取。读取失败时显式补上 `--domain panel.example.com --email you@example.com`。

增加独立订阅域名：先配置 DNS，再执行：

```bash
sh /tmp/skysbx-panel-install.sh --upgrade --sub-domain sub.example.com
```

原 token 保留。配置独立订阅域名后，只有该域名能访问订阅，旧面板域名及其他域名的订阅路径返回 404；面板管理功能照常使用。复制的新链接使用订阅域名；客户端已有旧 URL 不会自动改变，必须手动替换后才能更新订阅。未配置独立订阅域名时，仍使用面板域名订阅。

### 更新节点：在每台节点服务器执行

```bash
curl -fL https://raw.githubusercontent.com/zayvian-lee/skysbx-node/main/install.sh -o /tmp/skysbx-node-install.sh
sh /tmp/skysbx-node-install.sh --upgrade
```

读取 `/opt/skysbx/node.env` 的面板地址和 token，不必删除重建节点。**更新节点会同时编译配套内核**，不单独安装 core。节点会重启，造成短暂断连。

### 同机更新

```bash
curl -fL https://raw.githubusercontent.com/zayvian-lee/skysbx-panel/main/install-panel-and-node.sh -o /tmp/skysbx-all-install.sh
sh /tmp/skysbx-all-install.sh --upgrade
```

依次更新面板和节点、重新连接共享证书。只有面板变化时可只运行面板更新。

### 旧版数据如何处理

新版启动自动迁移数据库，保留管理员、用户、节点、凭据、订阅 token 和已用额度。旧节点默认 `1 倍率`。**不要通过删除节点、重装系统或 `--purge` 来升级**。

旧版只有当前周期流量合计，迁移时延续旧订阅口径计入下载；升级后分别累计，下一次重置后完全按新口径显示。不要为了改变显示而重置用户套餐额度。

升级后检查服务 `active`、节点在线、原用户与额度、入站生效状态；刷新订阅并实测连接。出现问题保留日志，按 [迁移与回退说明](docs/UPGRADE.md) 操作。回退数据库版本时不能只替换旧二进制。

## 8. 日常检查和故障排查

在安装了对应组件的服务器上执行：

```bash
/opt/skysbx/skysbx-panel --version
/opt/skysbx/skysbx-node --version
systemctl status skysbx-panel --no-pager
systemctl status skysbx-node --no-pager
journalctl -u skysbx-panel -n 100 --no-pager
journalctl -u skysbx-node -n 100 --no-pager
```

| 现象 | 先检查 |
| --- | --- |
| 面板打不开、证书失败 | DNS A/AAAA、TCP 80/443 放行及占用、面板日志 |
| 订阅域名首页 404 | 正常，使用用户的完整 `/sub/token` 链接 |
| 节点离线 | 节点能否访问面板 HTTPS、token、节点日志 |
| 在线但入站未生效 | 证书路径/SNI、端口冲突、节点和内核版本 |
| HY2 / TUIC 无法连接 | UDP 防火墙、证书、客户端内核；HY2 再看跳跃范围 |
| 订阅没有可用节点 | 用户停用/到期/超额、入站分配、节点或入站停用 |
| 改名后仍是旧名 | 刷新订阅，检查是否使用静态导入条目 |
| 小火箭显示原始字节 | 升级面板，使用 `?format=shadowrocket` 并更新订阅 |
| 构建下载失败 | 检查失败的 GitHub / Go 模块 / Docker 源；不要删除数据库 |

管理员密码恢复、token 替换、自定义路径、卸载与重装见 [维护说明](docs/UPGRADE.md)。备份含敏感凭据，不要提交到 GitHub。

## 项目维护

问题与建议提交到 [本项目 Issues](https://github.com/zayvian-lee/skysbx-panel/issues)，附版本、部署方式和脱敏日志。自动检查覆盖测试、并发检测、脚本语法和 Linux 构建；真实 DNS、证书、防火墙和客户端仍需部署验收。

许可证见 [LICENSE](LICENSE)，代码来源与致谢见 [NOTICE.md](NOTICE.md)。
