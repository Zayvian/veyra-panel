# Veyra Node

Veyra 的节点程序。每台代理服务器安装一份，主动连接面板，接收配置、更新用户、运行代理并上报流量。

[![Node CI](https://github.com/Zayvian/veyra-node/actions/workflows/ci.yml/badge.svg)](https://github.com/Zayvian/veyra-node/actions/workflows/ci.yml)

**新用户先安装 [Veyra Panel](https://github.com/Zayvian/veyra-panel)，再按本页安装节点。** [Veyra Core](https://github.com/Zayvian/veyra-core) 已编译进节点，不单独启动或安装。

## 功能与部署条件

支持 VLESS Reality、AnyTLS、Shadowsocks 2022、Hysteria2、TUIC v5；支持用户热更新、上传/下载统计和会话撤销。Hysteria2 支持 Linux UDP 跳端口；TUIC 当前固定端口。

默认安装器使用 Debian / Ubuntu、systemd 和 root 权限。通过 Docker 编译程序，安装后由 systemd 直接运行。需要访问 GitHub、Go 依赖和 Docker 镜像源。每台主机运行一个受管理的节点实例。

- 节点能访问面板的 HTTPS 地址；不额外开放控制端口。
- 节点域名指向本机，使用直连 DNS，Cloudflare 为灰云。
- 默认节点证书签发需要可达且空闲的 TCP 80；不能提供时看「证书选项」。
- 代理端口同时在云安全组和本机防火墙放行。
- 面板、节点同机时请使用 [同机安装流程](https://github.com/Zayvian/veyra-panel#6-面板与节点同机安装)，避免与面板抢占 80/443。

## 1. 从面板取得 token

登录面板，进入「节点 → 新建」：

| 字段 | 示例 |
| --- | --- |
| 名称 | 香港无限流量 |
| 客户端连接地址 | `hk.example.com`，不带协议前缀 |
| 国家 | `HK` |
| 流量倍率 | `1` 正常，`0.1` 十分之一，`0` 不扣套餐 |

复制创建后只显示一次的接入 token。它属于这台节点，不是用户的订阅 token，不要公开。

## 2. 安装节点

在**节点服务器的 SSH 终端**运行 `sudo -i`，再执行：

```bash
apt-get update && apt-get install -y curl
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh | bash
```

按提示输入面板地址、在面板复制的接入 token、节点域名和证书邮箱。token 输入不显示；填写后才下载节点与配套 core 源码、使用 Go 1.26.5 构建、签发证书并启动服务。普通使用者不用手动安装 Go 或单独克隆 core。

面板已在**同一台服务器**运行时，节点域名填面板域名。安装器会自动复用面板现有证书，不申请新证书，也不会占用面板正在使用的 TCP 80。

**AnyTLS、Hysteria2、TUIC 都需要证书**。只使用 Reality / Shadowsocks 时可省略 `--domain`，交互提示域名时直接回车。安装器允许证书失败后继续启动，所以服务在线不能代替证书检查。

```bash
systemctl is-active skysbx-node
journalctl -u skysbx-node -n 50 --no-pager
# 使用 TLS 协议时还要检查：
ls -l /opt/skysbx/cert.pem /opt/skysbx/key.pem
```

回面板确认节点「在线」。此时还未配置代理端口，继续下一步。

## 3. 添加入站并导入订阅

在面板中点击该节点的「入站」，选择协议、填写唯一名称和端口：

| 协议 | 示例端口 | 必要设置 |
| --- | --- | --- |
| VLESS Reality | TCP 443 | Reality 握手站点；密钥自动生成 |
| AnyTLS | TCP 8443 | 证书、私钥、SNI |
| Shadowsocks 2022 | TCP / UDP 8388 | 密钥自动生成 |
| Hysteria2 | UDP 8443 | 证书、SNI；可选跳跃范围 |
| TUIC v5 | UDP 9443 | 证书、SNI；当前无自动跳端口 |

证书默认 `/opt/skysbx/cert.pem`，私钥默认 `/opt/skysbx/key.pem`，SNI 填匹配的节点域名。普通直连时中转设置留空。放行实际配置的 TCP / UDP 端口，不要把示例端口当成固定要求。

tag 是导出的入站名称，支持中文，例如「香港下载 01」。保存后确认生效；再在面板创建用户、分配入站、复制订阅并导入客户端。完整客户端与套餐操作见 [面板 README](https://github.com/Zayvian/veyra-panel#5-用户倍率和订阅)。

### HY2 跳端口示例

监听 `8443`、范围 `20000-30000`、间隔 `30s`。云安全组放行 UDP 8443 与 UDP 20000–30000，本机 INPUT 防火墙允许实际监听 UDP 8443。安装器提供 nftables 依赖和所需 systemd 能力；规则负责把范围重定向到监听端口，不代替防火墙放行。

仅支持直连 Linux 节点，不与中转同时使用。节点清理的专用表命名为 `skysbx_hop_<16位十六进制>`；不要把自己的防火墙表命名成这个格式。清空面板跳跃范围可关闭，节点退出/重启会清理所属规则。

## 证书选项

### 默认 HTTP 验证

使用安装命令的 `--domain` 和 `--email`，节点 TCP 80 对公网开放且空闲。安装器配置 certbot 续签和证书复制钩子，证书更新时重启节点加载，会短暂中断连接。

### Cloudflare DNS 验证

域名由 Cloudflare 管理且 TCP 80 不可用时，准备仅能编辑该域名 DNS 的 API token，在 root Bash 中交互读取：

```bash
read -rsp 'Cloudflare DNS API token: ' cf_token; printf '\n'
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh | bash -s -- \
  --panel https://panel.example.com --domain hk.example.com \
  --email you@example.com --cf-token "$cf_token"
unset cf_token
```

安装程序仍会提示节点接入 token。DNS 验证不等于开启 CDN，代理记录仍应灰云。Cloudflare 凭据保存在 `/etc/letsencrypt/cloudflare.ini`，不要公开；`--cf-token` 在运行期间作为进程参数传入，应在受信任的主机上执行。

### 已有证书

确认完整证书链和私钥匹配，安装到默认路径：

```bash
install -d -m 0700 /opt/skysbx
install -m 0644 /你的路径/fullchain.pem /opt/skysbx/cert.pem
install -m 0600 /你的路径/privkey.pem /opt/skysbx/key.pem
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh | bash -s -- \
  --panel https://panel.example.com \
  --domain hk.example.com --no-cert
```

自行管理证书续签与重新加载。入站 SNI 与证书一致。面板同机使用共享证书时，按同机流程操作，不能用这段命令随意覆盖共享链接。

## 4. 更新节点与内核

先 [备份当前数据、证书和程序](https://github.com/Zayvian/veyra-panel/blob/main/docs/BACKUP.md)，再在每台节点执行：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh | bash -s -- --upgrade
```

更新从 `/opt/skysbx/node.env` 读取面板地址、token，不必重新登记节点。它会同时拉取配套内核并重新编译，保留证书，不重复签发。更新会重启节点，建议逐台进行。

安装或更新完成后，输入 `veyra` 可打开交互式管理菜单，进行状态查看、升级、重启、日志查看和卸载；Node token 不会显示在菜单中。

旧版迁移：先更新面板、再更新节点、最后使用新协议。若曾设置其他下载源环境变量，可显式指定：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh | \
  VEYRA_REPO=https://github.com/Zayvian/veyra-node.git \
  VEYRA_CORE_REPO=https://github.com/Zayvian/veyra-core.git \
  VEYRA_GH_OWNER=Zayvian VEYRA_REF=main \
  bash -s -- --upgrade
```

自定义目录在每次操作加 `VEYRA_ROOT=实际目录`；旧的 `SKYSBX_ROOT` 仍兼容。手工或容器部署不要直接覆盖原 unit。`node.env` 丢失要先恢复；证书缺失不会因 `--upgrade` 自动补签。更多迁移、回退和 token 替换见 [维护说明](https://github.com/Zayvian/veyra-panel/blob/main/docs/UPGRADE.md)。

## 5. 日常管理与卸载

```bash
/opt/skysbx/skysbx-node --version
systemctl status skysbx-node --no-pager
journalctl -u skysbx-node -n 100 --no-pager
systemctl restart skysbx-node
```

| 问题 | 检查 |
| --- | --- |
| 离线 | 面板 HTTPS 可达性、node.env 的地址和 token、日志 |
| 在线但入站未生效 | 端口冲突、证书、SNI、是否使用匹配的内核 |
| HY2 / TUIC 连接失败 | UDP 规则、证书有效期、客户端支持；HY2 再看 nftables |
| 下载仍扣很多额度 | 面板节点倍率是否保存、客户端是否确实选了这个节点 |
| 升级失败 | 失败发生在下载、编译还是启动；保留日志，不删除数据 |

### 只卸载节点程序，保留凭据以便重装

```bash
curl -4 -fL --retry 3 --connect-timeout 15 \
  https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh \
  -o /root/veyra-node-install.sh && \
bash /root/veyra-node-install.sh --uninstall
```

这会停止并删除 `skysbx-node` 服务和程序，保留 `/opt/skysbx/node.env`、证书及 token。之后运行 `--upgrade` 可以重新装回同一个节点，不需要在面板重新创建节点或更换 token。

### 彻底删除节点

```bash
curl -4 -fL --retry 3 --connect-timeout 15 \
  https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh \
  -o /root/veyra-node-install.sh && \
bash /root/veyra-node-install.sh --purge
```

这会删除节点服务、程序、`node.env`、节点证书、证书续签钩子与安装器的构建缓存；节点随即离线。脚本仅在确认 Docker 是由它安装时才会删除 Docker。`curl`、`git`、`nftables`、`certbot` 等共享系统组件会保留，因此不需要为了卸载 Veyra 而重装 VPS。

面板和节点同机时，先执行本节的 Node `--purge`，确认节点服务已经移除，再在 [Panel 卸载章节](https://github.com/Zayvian/veyra-panel#8-卸载彻底清理与重装)执行 Panel `--purge`。不要将 `--purge` 用于升级或迁移。

## 开发者构建

普通用户使用上面的安装器。开发者需把节点和 core 克隆为同级目录：

```bash
git clone https://github.com/Zayvian/veyra-node.git
git clone https://github.com/Zayvian/veyra-core.git
cd veyra-node
# 使用 Go 1.26.5；-race 检测还需要 C 编译器
go test -race -tags 'with_clash_api,with_v2ray_api,with_utls,with_acme,with_quic' ./...
CGO_ENABLED=0 go build -trimpath \
  -tags 'with_clash_api,with_v2ray_api,with_utls,with_acme,with_quic' \
  -o veyra-node ./cmd/node
```

保留 go.mod 中 core 和 sing-quic 的本地 replace 指令；省略构建标签可能导致功能缺失。不要随意把配套 core 替换为其他内核版本。

## 项目维护

[提交问题](https://github.com/Zayvian/veyra-node/issues)时附程序版本、协议、部署方式和脱敏日志。测试覆盖真实 QUIC 连接、用户热更新、撤销、流量归属与 Linux 构建；实际服务器证书/网络需自行验收。

许可证与来源见 [LICENSE](LICENSE)、[NOTICE](NOTICE)。
