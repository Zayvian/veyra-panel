# 备份与恢复

适用于默认数据目录 `/opt/skysbx`、systemd 部署。自定义目录必须对应修改路径。备份在安装了组件的**每台服务器**分别执行：面板备份不能替代节点证书、token 的备份。

## 备份什么

| 文件/目录 | 内容 |
| --- | --- |
| `/opt/skysbx/skysbx.db` 及可能存在的 WAL/SHM | 用户、节点、订阅、额度、配置 |
| `/opt/skysbx/panel.env`、`node.env` | 域名、连接地址、节点 token |
| `/opt/skysbx/certs`、`cert.pem`、`key.pem` | 面板和节点证书 |
| `/opt/skysbx/skysbx-panel`、`skysbx-node` | 当前可回退的程序 |
| `/etc/systemd/system/skysbx-*.service` | 启动方式和路径 |
| `/etc/letsencrypt`（如有） | 独立节点的证书和续签配置 |

备份包包含私钥、token 和数据库。用私有存储保管，不要上传公开仓库、聊天群或 Issues。另存一份到服务器之外，记录备份日期及程序版本。

## 创建一致性备份

下面在 **root 的 Bash** 中执行。它短暂停止当前正在运行的 skysbx 服务，完成后恢复原先正在运行的服务。同机部署会短暂中断代理连接。使用停服归档避免遗漏正在写入的 SQLite WAL。

```bash
bash <<'BACKUP'
set -euo pipefail
umask 077
backup_dir=/root/skysbx-backups
mkdir -p "$backup_dir"
backup_file="$backup_dir/skysbx-$(date +%Y%m%d-%H%M%S).tar.gz"
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

构建缓存可以重新下载，不包含在备份中。自定义 systemd drop-in、外部证书路径、反向代理配置需要自行追加备份；上述脚本不会自动发现它们。

## 恢复与回退

恢复会覆盖备份对应的数据库、程序、配置和证书；备份之后的用户变动、流量和配置不会出现在旧数据库中。先保留故障现场的新备份，再执行回退，防止丢失排查资料。

1. 确认归档来自可信来源，路径符合当前主机，校验 sha256。
2. 停止本机 skysbx 服务；独立节点使用 certbot 时，避免续签任务同时改写证书。
3. 把当前数据目录移动到一个**不存在的新目录**保留，不把新旧数据库的 WAL 混用。
4. 恢复同一备份内的数据库、程序和 unit，重新加载 systemd。
5. 只启动这台主机实际安装的服务，检查日志、登录、用户额度和一条实际连接。

默认安装示例（替换备份文件路径，在 root Bash 中逐步执行）：

```bash
backup_file=/root/skysbx-backups/skysbx-20260909-120000.tar.gz
sha256sum -c "$backup_file.sha256"
tar -tzf "$backup_file"
# 先确认校验成功，归档内容是预期的 opt/skysbx、相关 unit 和证书目录。
```

然后按本机组件停止服务。面板主机停止 `skysbx-panel`，节点主机停止 `skysbx-node`，同机两者都停止：

```bash
systemctl stop skysbx-panel
# 本机也有节点时再执行：systemctl stop skysbx-node
saved_dir="/opt/skysbx-before-restore-$(date +%Y%m%d-%H%M%S)"
test ! -e "$saved_dir" && mv /opt/skysbx "$saved_dir"
tar -xzpf "$backup_file" -C /
systemctl daemon-reload
systemctl start skysbx-panel
# 本机也有节点时再执行：systemctl start skysbx-node
```

上例是面板恢复；**独立节点把停/启服务改为 `skysbx-node`**。只恢复与该主机相符的备份。同机两个服务都要停止后才移动共享目录；确认恢复有效前保留 `skysbx-before-restore-*`。

迁移到新服务器时，还需安装运行所需依赖、恢复证书续签任务、调整 DNS/防火墙，并核对 unit 的路径。这份归档不是整个操作系统镜像。
