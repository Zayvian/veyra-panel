**简体中文** | [繁體中文](../zh-TW/install.md) | [English](../en/install.md)

# 安装

本页带你从零装好一个面板和若干节点。装好之后各个功能怎么用，见[使用指南](guide.md)；升级、备份和排错见[维护](maintenance.md)。

- [1. 准备服务器和域名](#1-准备服务器和域名)
- [2. 安装面板](#2-安装面板)
- [3. 登记并安装节点](#3-登记并安装节点)
- [4. 节点证书](#4-节点证书)
- [5. 面板和节点装在同一台服务器](#5-面板和节点装在同一台服务器)
- [6. 下一步](#6-下一步)

## 1. 准备服务器和域名

**系统**：Debian 或 Ubuntu，使用 systemd，需要 root 权限。下面的命令都在服务器的 SSH 终端里执行，先运行 `sudo -i` 切换到 root。

**下载方式**：安装脚本默认从本项目的 GitHub Release 下载编译好的程序（支持 amd64 和 arm64），核对 sha256 后安装，**不在服务器上编译**，配置很低的机器也能很快装好。服务器只需要能访问 GitHub。

**域名**：下面的示例域名都要换成你自己的。

| 示例 | 解析到 | 用途 |
| --- | --- | --- |
| `panel.example.com` | 面板服务器 IP | 管理后台、节点连接面板 |
| `sub.example.com` | 面板服务器 IP | 可选：单独的订阅域名 |
| `hk.example.com` | 香港节点服务器 IP | 客户端连接节点、节点的 TLS 证书 |

- 添加 A 记录。只有服务器的 IPv6 确实能用时才添加 AAAA 记录，否则有 IPv6 的客户会连不上。
- 如果域名托管在 Cloudflare，请设为 **DNS only（灰云）**。这些代理协议不能经过 Cloudflare 的 HTTP 代理。

**端口**：

| 服务器 | 需要开放的端口 | 说明 |
| --- | --- | --- |
| 面板 | TCP 80、443 | 自动申请证书、HTTPS 后台和订阅。安装前不能被其他程序（如 Nginx、Caddy）占用 |
| 节点 | TCP 80 | 默认用 HTTP 验证申请证书；改用 DNS 验证时可不开放 |
| 节点 | 你配置的代理端口（TCP / UDP） | 云服务商的安全组和本机防火墙**都要放行** |

节点会主动连接面板，所以节点**不需要**开放任何控制端口。检查端口是否被占用：

```bash
ss -lntp | grep -E ':(80|443)\b'
```

## 2. 安装面板

在**面板服务器**上执行：

```bash
apt-get update && apt-get install -y curl
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash
```

按提示依次填写：

1. **面板域名**：必须已经解析到这台服务器；
2. **订阅域名**（可选）：直接回车表示订阅也用面板域名；
3. **Let's Encrypt 邮箱**（可选）；
4. **管理员用户名和密码**：密码至少 12 位，输入时不显示。

脚本会下载程序、申请证书、创建管理员并启动服务。管理员在服务对外开放之前就已经创建好，不存在被别人抢先注册的空档。

完成后打开 `https://panel.example.com/login` 登录。检查服务状态：

```bash
systemctl is-active skysbx-panel
journalctl -u skysbx-panel -n 50 --no-pager
```

安装完成后，在服务器上输入 `veyra` 可以打开管理菜单：查看状态、升级、重启、看日志、卸载。菜单里不会显示密码、token 之类的敏感信息。

> 服务和文件名里的 `skysbx` 是旧名字，为了兼容旧版本的数据而保留，不影响使用。

### 隐藏后台地址（推荐）

登录后进入「设置」，在「面板访问路径」填一个只有你知道的路径，例如 `/my-panel-2026`，保存后后台地址就变成：

```text
https://panel.example.com/my-panel-2026
```

直接访问根域名会返回 404，也可以设置成跳转到一个普通网页。**节点连接和订阅链接不受影响**，改路径不会让节点离线、也不会让已发出的订阅失效。

## 3. 登记并安装节点

### 3.1 在面板里登记

进入「节点」，填写后点「新建」：

| 字段 | 示例 | 说明 |
| --- | --- | --- |
| 名称 | `香港 01` | 支持中文 |
| 客户端连接地址 | `hk.example.com` | 客户端连接的地址，不要带 `https://` |
| 国家 | `HK` | 两位国家代码 |
| 流量倍率 | `1` | 1 为正常计费，0.5 为半价，0 为不扣流量 |

保存后会显示**接入 token**，**只显示这一次**，请马上复制。不要把 token 发到公开的地方；丢了可以在节点列表点「换 token」重新生成。每台节点用自己的 token。

### 3.2 在节点服务器上安装

`hk.example.com` 要解析到**这台节点服务器**。在节点服务器上执行：

```bash
apt-get update && apt-get install -y curl
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash
```

按提示填写：

1. **面板地址**：例如 `https://panel.example.com`；
2. **接入 token**：刚才复制的那串，输入时不显示；
3. **节点域名**：用 AnyTLS、Hysteria2、TUIC 时必须填（需要证书）；只用 VLESS Reality 或 Shadowsocks 可以直接回车；
4. **证书邮箱**（可选）。

回到面板，节点应显示「在线」。在节点服务器上检查：

```bash
systemctl is-active skysbx-node
journalctl -u skysbx-node -n 50 --no-pager
ls -l /opt/skysbx/cert.pem /opt/skysbx/key.pem   # 只有用到证书时才需要检查
```

**节点「在线」不代表已经能用**，还需要在面板里给它添加入站（代理端口），见[使用指南：入站](guide.md#入站)。

## 4. 节点证书

AnyTLS、Hysteria2、TUIC 需要节点自己的证书；VLESS Reality 和 Shadowsocks 不需要。

### 默认：HTTP 验证

安装时填了节点域名就会自动申请。要求节点的 TCP 80 对公网开放且没被占用。证书会自动续期，续期后节点自动重启加载新证书，期间连接会断一下。

### Cloudflare DNS 验证（TCP 80 不能用时）

域名在 Cloudflare 上时，准备一个**只能编辑这个域名 DNS** 的 API token，然后：

```bash
read -rsp 'Cloudflare DNS API token: ' cf_token; printf '\n'
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- \
  --panel https://panel.example.com --domain hk.example.com \
  --email you@example.com --cf-token "$cf_token"
unset cf_token
```

脚本仍然会提示你输入接入 token。用 DNS 验证不代表开启 CDN，节点的解析记录仍然要设为灰云。

### 已经有证书

把完整证书链和私钥放到默认位置，再跳过证书申请：

```bash
install -d -m 0700 /opt/skysbx
install -m 0644 /你的路径/fullchain.pem /opt/skysbx/cert.pem
install -m 0600 /你的路径/privkey.pem /opt/skysbx/key.pem
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- \
  --panel https://panel.example.com --domain hk.example.com --no-cert
```

证书的续期和重新加载需要你自己负责。

## 5. 面板和节点装在同一台服务器

**选择这种方式时，用本节代替第 2、3 节。**面板域名（和可选的订阅域名）都解析到这台服务器：

```bash
apt-get update && apt-get install -y curl
curl -4 -fL --retry 3 --connect-timeout 15 \
  https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-panel-and-node.sh \
  -o /tmp/veyra-all-install.sh && bash /tmp/veyra-all-install.sh
```

脚本会一步步引导：填面板域名、订阅域名和邮箱 → 创建管理员 → 你去面板里新建节点 → 回到 SSH 粘贴接入 token → 完成。

- 节点的「客户端连接地址」和 TLS 入站的 SNI 都填面板域名。节点会直接用面板的证书，不再另外申请。
- 面板已经占用了 TCP 80 和 443，代理端口要避开，例如 Reality 用 10443、AnyTLS 用 8443。

## 6. 下一步

1. 在节点里添加入站：[使用指南：入站](guide.md#入站)
2. 创建用户、复制订阅链接发给客户：[使用指南：用户](guide.md#用户)
3. 开启两步验证，保护后台：[使用指南：设置](guide.md#设置)
