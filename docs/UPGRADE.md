# 更新、旧版迁移与维护

正常安装流程见 [README](../README.md)。本页适用于 Veyra 及其旧版兼容 systemd 部署，不是其他面板数据库的导入工具。

## 一、先确认当前安装

在对应服务器检查（不要公开输出中的 token 或私钥）：

```bash
systemctl cat skysbx-panel
systemctl cat skysbx-node
ls -ld /opt/skysbx
/opt/skysbx/skysbx-panel --version
/opt/skysbx/skysbx-node --version
```

未安装的服务/文件报不存在是正常的。记录实际 `WorkingDirectory`、`ExecStart`、`EnvironmentFile`、数据库路径。默认 unit 分别为 `skysbx-panel.service`、`skysbx-node.service`。

| 检查结果 | 操作 |
| --- | --- |
| 默认 systemd + `/opt/skysbx` | 按本页升级 |
| `SKYSBX_ROOT` 自定义目录 | 每次安装、升级、备份都使用同一个实际目录 |
| 手工运行、容器部署、反向代理模式 | 使用原部署方式替换程序，保留参数；不要用默认脚本覆盖自定义 unit |
| 其他面板、不同数据库结构 | 无直接升级保证，先单独做数据迁移方案 |

## 二、从旧版迁移到本项目

1. 按 [备份说明](BACKUP.md) 在面板和每台节点制作备份，并另存到服务器之外。
2. 检查是否设置过 `SKYSBX_REPO`、`SKYSBX_FORK`、`SKYSBX_GH_OWNER`、`SKYSBX_REF`。旧的环境覆盖值会改变实际下载源。
3. 使用下面显式指定本项目来源的命令，先更新面板，再逐台更新节点。
4. 确认原节点在线、原协议正常后，再配置 HY2 / TUIC 等新功能。
5. 刷新客户端订阅，检查名称、额度和一条实际连接；原 token 不应变化。

### 面板服务器

```bash
curl -fL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh -o /tmp/veyra-panel-install.sh
VEYRA_REPO=https://github.com/Zayvian/veyra-panel.git \
VEYRA_GH_OWNER=Zayvian VEYRA_REF=main \
sh /tmp/veyra-panel-install.sh --upgrade
```

域名读取失败时加 `--domain 你的面板域名 --email 你的邮箱`。旧版没有独立订阅域名也可以升级，仍按同域名运行；新增订阅域名则补 `--sub-domain 你的订阅域名`，先完成 DNS。

### 每台节点服务器

```bash
curl -fL https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh -o /tmp/veyra-node-install.sh
VEYRA_REPO=https://github.com/Zayvian/veyra-node.git \
VEYRA_CORE_REPO=https://github.com/Zayvian/veyra-core.git \
VEYRA_GH_OWNER=Zayvian VEYRA_REF=main \
sh /tmp/veyra-node-install.sh --upgrade
```

节点与内核从配套仓库重新编译。`--upgrade` 从 `node.env` 保留面板地址、token，并跳过重新签证书；不要把节点从面板删除重建。证书缺失时需要先恢复证书，升级不会自动补签。

旧目录没有 `panel.env` 时，面板可从旧 unit 读取域名；如果连 unit 都没有，显式提供域名。节点升级必须能读取原 `node.env`，缺失时先恢复文件或使用原节点的有效 token 重新接入。

### 数据与兼容边界

- 升级会新增节点和入站排序字段，原记录默认 `0`，保留原订阅的节点创建顺序和节点内名称顺序。只需升级面板，排序无需更新节点机。
- 用户入站分配增加批量操作和明确的开放范围。原权限保留；新界面的「全选当前入站」只选已有入站，自动开放未来入站需选择「全部入站（含以后新建）」。空勾选不能保存，避免意外解除限制。
- 数据库启动时自动应用迁移，保留已有用户、订阅、节点、已用额度。
- 原节点倍率为 `1`，不会自动免扣历史流量。
- 旧版当前周期只有总量，升级时先计入下载，之后分别累计计费上传和下载。
- 新功能涉及节点协议接口时，节点需要一起更新；仅倍率、中文名称、订阅文案修改只需更新面板。
- 更新会重启对应服务。逐台更新节点可缩小断连范围，但不是无中断升级。
- 自动化测试覆盖代码和迁移样例，不代表能直接兼容任何来源的改版数据库。

## 三、自定义目录与版本

如果原数据目录为 `/srv/skysbx`，在已下载的入口脚本上使用相同值，例如：

```bash
SKYSBX_ROOT=/srv/skysbx sh /tmp/skysbx-panel-install.sh --upgrade
```

该参数控制安装位置，不保证自动改写已有入站中的证书路径。自定义目录时核对面板里每个 TLS 入站的证书、私钥路径。不要误用默认目录创建一个空白面板。

默认构建 `main`。`SKYSBX_REF` 支持存在的分支或标签：先确认仓库有该名称再指定，不要把它当任意提交 SHA 使用。节点入口同时用此名称拉取节点和 core，因此两仓库必须存在匹配的分支/标签；同机入口还支持分别设置 `SKYSBX_REF` 和 `SKYSBX_NODE_REF`。

没有预编译发布包时，脚本仍从源码构建，不能保证离线升级。维护者在确认版本后可建立一致的版本标签；本文不虚构尚未发布的版本号。

## 四、如何回退

回退以更新前备份为准，见 [恢复步骤](BACKUP.md)。恢复**同一备份中的程序、数据库和配置**；不要仅换回旧程序而继续使用新版数据库。回退会回到备份时的数据，升级后新增的账号和用量需要另行核对。

构建阶段失败时先检查服务是否仍为原版本运行；不要执行 `--purge`。如果脚本已经安装新程序并重启失败，保留日志和当前数据，再从备份恢复。

## 五、管理员密码与节点 token

### 重设管理员密码

默认路径，在 root Bash 执行；密码通过标准输入传递：

```bash
read -rsp '新管理员密码: ' admin_password; printf '\n'
printf '%s' "$admin_password" | /opt/skysbx/skysbx-panel \
  --db /opt/skysbx/skysbx.db --set-admin admin
unset admin_password
```

将 `admin` 换成目标管理员名，密码须满足程序校验。自定义数据库路径必须对应调整。

### 更换节点 token

在面板「节点 → 换 token」得到新值，然后在对应节点服务器编辑：

```bash
nano /opt/skysbx/node.env
chmod 600 /opt/skysbx/node.env
systemctl restart skysbx-node
```

只替换 `SKYSBX_TOKEN=` 后的值，不改变其他字段；检查节点恢复在线。旧 token 立即失效。更换订阅 token 和更换节点 token 是不同操作。

## 六、卸载与重装

先下载 README 中的最新入口脚本。在安装了对应组件的主机执行：

```bash
# 面板：移除服务和程序，保留数据库、域名配置、证书
sh /tmp/skysbx-panel-install.sh --uninstall
# 节点：移除服务和程序，保留 node.env、证书
sh /tmp/skysbx-node-install.sh --uninstall
```

仅执行你确实想卸载的组件。保留的数据可以通过相应脚本 `--upgrade` 重装。`--purge` 是永久清理，会删除对应数据和证书；并可能移除该脚本安装的 Docker。**它不是升级、修复连接或回退的步骤**，同机部署尤其要考虑共享依赖。

## 七、独立订阅域名与反向代理

配置了独立订阅域名的安装，升级后只允许该域名访问 `/sub/`。旧面板域名的订阅请求返回 404，不会自动跳转；用户 token 和面板管理入口保留。请把客户端订阅地址换成面板重新复制的新链接。未配置独立订阅域名的安装不受影响。已经导入客户端的节点配置不会因旧订阅入口关闭而自动失效。

默认安装使用内置 HTTPS。已有自管反向代理时，二进制支持 `--addr 127.0.0.1:8080 --sub-domain sub.example.com --db ...`；由代理终止 TLS、提供证书，保留 Host 并设置正确的 `X-Forwarded-Proto`，此模式不启用 `--domain` 自动证书。需维护自己的 unit，默认安装器不会保留自定义 unit 配置。

要关闭已配置的独立订阅域名，需要一致修改 `/opt/skysbx/panel.env` 的 `SKYSBX_SUB_DOMAIN=` 和 unit 中 `--sub-domain` 的值，重载 systemd 后重启面板；只传空参数不会覆盖升级脚本回读的旧值。修改前备份，已有订阅 URL 仍需迁移。
