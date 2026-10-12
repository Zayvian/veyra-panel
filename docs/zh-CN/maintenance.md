**简体中文** | [繁體中文](../zh-TW/maintenance.md) | [English](../en/maintenance.md)

# 维护

- [1. 升级](#1-升级)
- [2. 备份与恢复](#2-备份与恢复)
- [3. 忘记密码、两步验证、换 token](#3-忘记密码两步验证换-token)
- [4. 卸载](#4-卸载)
- [5. 排查问题](#5-排查问题)
- [6. 进阶](#6-进阶)

---

## 1. 升级

每次有新提交合并进 `main` 并通过自动测试后，项目会自动发布一个新的 [Release](https://github.com/Zayvian/veyra-panel/releases)（名字是 `build-日期-提交号`），里面是编译好的面板和节点程序。升级就是下载最新的 Release 并替换程序，**服务器上不编译**。

### 升级面板

在面板服务器上执行：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash -s -- --upgrade
```

面板重启几秒钟，期间后台打不开，**客户的连接不受影响**。数据库结构会在启动时自动升级，用户、节点、订阅都会保留。

### 升级节点

在每台节点服务器上执行：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- --upgrade
```

会从 `/opt/skysbx/node.env` 读取面板地址和 token，不用重新登记，也不会重新申请证书。**节点会重启，这台节点上的连接断一次**，建议一台一台升级。

也可以在服务器上运行 `veyra`，从菜单里选「升级」。

### 要不要升级节点

每个 Release 都同时包含面板和节点，但**很多版本只改了面板**。节点的版本号是编译它的那次提交，所以即使节点代码没变，新 Release 里的节点版本号也会变。看 Release 对应的提交说明：没有涉及 `node/` 的改动，节点就不用升。

面板和节点之间的通信是向下兼容的，新面板配旧节点、旧面板配新节点都能正常工作，可以分开升级。

### 查看版本

```bash
/opt/skysbx/skysbx-panel -version
/opt/skysbx/skysbx-node -version
```

面板的「节点」页也会显示每台节点的版本。

### 装指定版本（回退）

在命令前加 `VEYRA_RELEASE=Release 名称`，例如：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | VEYRA_RELEASE=build-20261012-ce58403 bash -s -- --upgrade
```

只保留最近 10 个 Release。**回退面板时，如果新版本升级过数据库结构，只换回旧程序可能不行**，要连同升级前的数据库备份一起恢复，见[备份与恢复](#2-备份与恢复)。

### 在服务器上编译

CPU 不是 amd64 / arm64，或者想自己编译时，加 `--from-source`。下载不到编译好的程序时，也会自动改为本机编译。本机编译会先安装 Docker，在配置低的机器上编译节点可能要很久。

### 面板和节点在同一台服务器

```bash
curl -fL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-panel-and-node.sh -o /tmp/veyra-all-install.sh
sh /tmp/veyra-all-install.sh --upgrade
```

会依次升级面板和节点。只有面板有变化时，单独升级面板即可。

### 添加单独的订阅域名

先把新域名解析到面板服务器，再执行：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash -s -- --upgrade --sub-domain sub.example.com
```

设置后，只有这个域名能访问订阅，原来用面板域名的订阅链接会返回 404。用户页复制出来的会是新链接，**已经发出去的旧链接要换成新链接**。

---

## 2. 备份与恢复

### 要备份什么

面板的全部数据（用户、节点、订阅、流量、设置）都在 **`/opt/skysbx/skysbx.db`** 这一个文件里。完整备份还包括：

| 文件 | 内容 |
| --- | --- |
| `/opt/skysbx/skysbx.db`（以及同名的 `-wal`、`-shm`） | 数据库 |
| `/opt/skysbx/panel.env`、`node.env` | 域名、面板地址、节点 token |
| `/opt/skysbx/certs`、`cert.pem`、`key.pem` | 面板和节点的证书 |
| `/opt/skysbx/skysbx-panel`、`skysbx-node` | 当前的程序，方便回退 |
| `/etc/systemd/system/skysbx-*.service` | 服务配置 |
| `/etc/letsencrypt`（如有） | 节点证书和续期配置 |

备份里有私钥、token 和数据库，**放在私密的地方**，不要传到公开仓库、群聊或 Issues。另存一份到服务器以外，升级前尤其要备份。

### 创建备份

用 root 执行。它会短暂停止 Veyra 服务以保证数据库完整，完成后自动恢复：

```bash
bash <<'BACKUP'
set -euo pipefail
umask 077
backup_dir=/root/veyra-backups
mkdir -p "$backup_dir"
backup_file="$backup_dir/veyra-$(date +%Y%m%d-%H%M%S).tar.gz"
active_services=()
for svc in skysbx-panel skysbx-node; do
  if systemctl is-active --quiet "$svc"; then active_services+=("$svc"); fi
done
restore_services() {
  for svc in "${active_services[@]}"; do systemctl start "$svc" || true; done
}
trap restore_services EXIT
for svc in "${active_services[@]}"; do systemctl stop "$svc"; done
backup_paths=(opt/skysbx)
for item in etc/systemd/system/skysbx-panel.service etc/systemd/system/skysbx-node.service etc/letsencrypt; do
  if [ -e "/$item" ]; then backup_paths+=("$item"); fi
done
tar --exclude='opt/skysbx/build' --exclude='opt/skysbx/go-mod-cache' \
  --exclude='opt/skysbx/go-build-cache' \
  -czpf "$backup_file" -C / "${backup_paths[@]}"
tar -tzf "$backup_file" >/dev/null
sha256sum "$backup_file" > "$backup_file.sha256"
printf 'Backup: %s\n' "$backup_file"
BACKUP
```

### 恢复

恢复会回到备份时的状态，备份之后新增的用户和流量记录会丢失。

```bash
backup_file=/root/veyra-backups/veyra-20261012-120000.tar.gz   # 换成你的备份文件
sha256sum -c "$backup_file.sha256"
systemctl stop skysbx-panel            # 节点服务器改为 skysbx-node；同机两个都停
saved_dir="/opt/skysbx-before-restore-$(date +%Y%m%d-%H%M%S)"
test ! -e "$saved_dir" && mv /opt/skysbx "$saved_dir"
tar -xzpf "$backup_file" -C /
systemctl daemon-reload
systemctl start skysbx-panel           # 节点服务器改为 skysbx-node
```

确认恢复成功之前，保留 `/opt/skysbx-before-restore-*`。迁移到新服务器时，还要调整 DNS、防火墙，并确认证书续期正常。

---

## 3. 忘记密码、两步验证、换 token

### 重设管理员密码

在面板服务器上执行（`admin` 换成你的管理员用户名）：

```bash
read -rsp '新密码: ' p; printf '\n'
printf '%s' "$p" | /opt/skysbx/skysbx-panel -db /opt/skysbx/skysbx.db -set-admin admin
unset p
```

密码至少 12 位。

### 两步验证的手机和恢复码都丢了

```bash
/opt/skysbx/skysbx-panel -db /opt/skysbx/skysbx.db -reset-2fa
```

或者运行 `veyra` 菜单 → Panel → 7。之后用密码登录，再到「设置」里重新开启。

### 更换节点 token

1. 面板「节点」页点这台节点的「换 token」，复制新 token；
2. 在节点服务器上编辑 `/opt/skysbx/node.env`，只替换 `SKYSBX_TOKEN=` 后面的值；
3. 执行 `systemctl restart skysbx-node`，确认节点重新在线。

旧 token 立即失效。节点 token 和用户的订阅链接是两回事。

---

## 4. 卸载

先想好要不要保留数据：

- `--uninstall`：只删除程序和服务，**保留**数据库、配置、证书。以后用同一脚本的 `--upgrade` 就能原样装回来。
- `--purge`：**彻底删除**，包括数据库、所有用户和订阅、证书，**无法恢复**。

```bash
# 面板
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh -o /root/veyra-panel-install.sh
bash /root/veyra-panel-install.sh --uninstall     # 或 --purge

# 节点
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh -o /root/veyra-node-install.sh
bash /root/veyra-node-install.sh --uninstall      # 或 --purge
```

面板和节点在同一台服务器时，**先清理节点，再清理面板**。`--purge` 不会删除 curl、git、nftables、certbot 这类可能被其他程序使用的软件；Docker 只有确认是安装脚本自己装的才会删除。

---

## 5. 排查问题

先看日志：

```bash
systemctl status skysbx-panel --no-pager
journalctl -u skysbx-panel -n 100 --no-pager
systemctl status skysbx-node --no-pager
journalctl -u skysbx-node -n 100 --no-pager
```

| 现象 | 先检查 |
| --- | --- |
| 面板打不开、证书申请失败 | 域名解析、TCP 80 / 443 是否放行或被占用、面板日志 |
| 订阅域名首页显示 404 | 正常，订阅域名只开放 `/sub/` 路径 |
| 节点离线 | 节点能否访问面板的 HTTPS 地址、`node.env` 里的地址和 token、节点日志 |
| 节点在线但入站显示「未生效」 | 端口冲突、证书路径和 SNI、入站页顶部的错误信息 |
| 入站一直「等待节点应用」 | 节点日志里的配置错误；节点版本是否太旧 |
| Hysteria2 / TUIC 连不上 | 防火墙是否放行了 **UDP**、证书是否有效；HY2 跳端口还要放行整个端口范围 |
| HY2 时好时坏、经常超时 | 客户的网络可能在干扰 QUIC：新建一个开混淆的 HY2 入站 |
| 客户订阅里没有可用节点 | 用户是否停用、到期、流量用完；用户的「可用协议」；节点或入站是否停用 |
| 客户经常被断开 | 是否设了较低的同时在线 IP 上限（手机切换网络会换 IP） |
| 改了名字但客户端还是旧名 | 客户端需要更新订阅 |
| 升级时显示 `compiling here instead` | 没下载到编译好的程序，改为本机编译了。检查服务器能否访问 GitHub |

提问时请到 [Issues](https://github.com/Zayvian/veyra-panel/issues)，附上版本、部署方式和去掉敏感信息的日志。

---

## 6. 进阶

### 自定义安装目录

所有安装、升级、备份命令前加同一个 `VEYRA_ROOT=/你的目录`。证书路径不会自动改，要逐个核对 TLS 入站里的证书路径。

### 自己的反向代理

默认安装由面板自己处理 HTTPS。如果你已有自己的反向代理，可以让面板只监听本机：`skysbx-panel --addr 127.0.0.1:8080 --sub-domain sub.example.com --db /opt/skysbx/skysbx.db`，由反向代理负责 TLS，并保留 Host、设置正确的 `X-Forwarded-Proto`。这种方式需要自己维护 systemd 服务文件，安装脚本的升级不会保留你的自定义参数。

### 开发与构建

仓库结构、许可证和发布流程见[项目首页](../../README.md)。本地构建：

```bash
git clone https://github.com/Zayvian/veyra-panel.git
cd veyra-panel/panel && go test ./...          # 面板：Go 1.27
```

节点需要把 [veyra-core](https://github.com/Zayvian/veyra-core) 克隆到仓库旁边，检出 `node/core.lock` 写的版本，并用 Go 1.26.x（1.27 无法链接 sing-box）：

```bash
git clone --branch "$(sed -n 's/^CORE_REF=//p' veyra-panel/node/core.lock)" https://github.com/Zayvian/veyra-core.git
cd veyra-panel/node
go test -tags 'with_clash_api,with_v2ray_api,with_utls,with_acme,with_quic' ./...
```

面板和节点之间的通信协议见 [protocol/PROTOCOL.md](../../protocol/PROTOCOL.md)。
