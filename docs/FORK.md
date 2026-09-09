# zayvian-lee 二次开发版

本项目基于 [kosje/skysbx-panel](https://github.com/kosje/skysbx-panel)，保留上游提交历史与 AGPL-3.0 许可证。

配套仓库：[面板](https://github.com/zayvian-lee/skysbx-panel)、[节点](https://github.com/zayvian-lee/skysbx-node)、[内核](https://github.com/zayvian-lee/skysbx-core)。三个组件应一起升级，旧节点不支持新的用户更新接口与跳端口字段。

## 第一版功能

- 独立 HTTPS 订阅域名：管理页、复制链接、订阅页面使用统一的订阅域名；订阅 token 保留。
- Hysteria2：证书、SNI、用户认证、用户流量归属，以及可选 UDP 端口跳跃。
- TUIC v5：UUID + 密码、TLS、BBR、用户热更新。当前使用固定端口。
- sing-box JSON、Mihomo YAML、Base64 分享链接均支持新增协议。
- QUIC 用户更新保留未变化用户的会话；撤销、改名或更换认证凭据会关闭对应旧会话。

## 节点倍率与订阅名称

在「节点」的新建或编辑表单设置「流量倍率」，同一节点的所有入站、用户使用相同倍率。支持 0–100，最多三位小数，旧节点升级后默认 1 倍。

| 倍率 | 实际上传与下载合计 100 GB 的套餐扣量 |
| --- | --- |
| 1 | 100 GB |
| 0.1 | 10 GB |
| 0 | 0 GB，适合无限流量下载节点 |
| 2 | 200 GB |

实际流量保留在历史、节点使用记录中；用户已用额度、超额判断和订阅流量为倍率后的计费量。倍率以面板收到每次上报时的设置为准，不重算历史，不重启节点。中转由最终认证用户的落地节点上报，使用该落地节点的倍率。0 倍率不绕过账号停用、到期或已耗尽额度的限制。月度重置和手动重置会一起清零计费上传、下载和小数余额。

节点名称和入站名称（tag）支持中文。导出的每个代理只显示填写的入站名称，不再追加用户名、用量或到期时间；例如填写「香港下载 01」，导出即为「香港下载 01」。未填写入站名称时仍自动生成协议与节点名称组合；修改节点名称仍按原机制重新生成其入站 tag，并重建节点配置。

Shadowrocket 订阅自动添加 GB 状态文字，例如「上传：1.00 GB | 下载：2.00 GB | 总量：200.00 GB」，无限套餐显示「总量：不限」。隐藏客户端 User-Agent 时，请在订阅链接后加 `?format=shadowrocket`（已有查询参数时用 `&format=shadowrocket`）。订阅网页也使用 GB。与套餐输入保持一致，显示的 GB 按 1024³ 字节换算。

标准 `Subscription-Userinfo` 响应头仍使用整数**字节**，并分别下发计费上传和下载；其他客户端如何显示单位由客户端控制。Shadowrocket 状态采用 Base64 内容内的 `STATUS=` 行，格式参考 [Xboard 的 Shadowrocket 实现](https://github.com/cedar2025/Xboard/blob/master/app/Protocols/Shadowrocket.php)。实际 iOS 客户端显示仍需刷新订阅验证。

本次只需升级面板；数据库自动迁移，原订阅 token 和已用额度保留。旧版只保存当前周期合计，因此升级时历史已用量延续旧订阅口径计入下载；升级后的流量分别累计，下一次重置后完全按新口径显示。

## 独立订阅域名

将 `panel.example.com` 和 `sub.example.com` 的 DNS 解析到面板服务器，然后安装：

```sh
wget -qO- https://raw.githubusercontent.com/zayvian-lee/skysbx-panel/main/install.sh |
  sh -s -- --domain panel.example.com --sub-domain sub.example.com --email you@example.com
```

面板使用内置 ACME 分别管理两个域名的证书。需要域名直连以及可用的 80/443 端口。订阅域名只提供 `/sub/` 路径，管理页面仍使用面板域名。旧域名上的订阅路径继续可用，便于已有客户端迁移。

已有安装可通过 `deploy/install-panel.sh --upgrade --sub-domain sub.example.com` 设置；后续升级从 `/opt/skysbx/panel.env` 保留此配置。不设置时维持原有同域名行为。订阅域名与节点 TLS/SNI 是分别配置的，换订阅域名不需要更换节点证书。

反向代理自行终止 TLS 时，二进制也可单独传入 `--sub-domain sub.example.com`；代理需为新域名提供证书并保留 Host。此模式不启用面板的 `--domain` 自动证书选项。

## Hysteria2 端口跳跃

新增入站时选择 `hysteria2`，填写节点证书对应的 SNI。示例：监听端口 `8443`、跳跃范围 `20000-30000,40000`、间隔 `30s`。

- 当前只支持直连 Linux 节点。清空跳跃范围即可关闭；与中转同时使用会被拒绝。
- 范围必须在 1–65535 内且不重叠；面板检查与本节点其他入站、中转端口的冲突。
- 节点需要 `nftables` 与 `CAP_NET_ADMIN`；安装脚本会安装依赖，现有 systemd 配置已包含所需能力。
- 云安全组需要放行跳跃范围及实际监听 UDP 端口；本机 INPUT 防火墙也需允许实际监听端口。自动创建的规则只负责端口重定向。
- IPv4/IPv6 共用 `inet` NAT 规则。每个入站使用独立 `skysbx_hop_<hash>` 表；退出时清理，节点重启时清除遗留规则。该命名空间专用于一台主机上的单个 skysbx-node 实例。
- 修改范围、监听端口、证书会重建节点配置；仅修改跳跃间隔只影响订阅，下次刷新订阅生效。

## 客户端

| 客户端/格式 | 配置方式与限制 |
| --- | --- |
| v2rayN | Base64 分享链接；hy2 用 `mport` 传递端口范围。使用支持对应协议的内核版本。 |
| Clash Party / Mihomo | `?format=clash`，hy2 使用 `ports` 和 `hop-interval`。原版旧 Clash 不在本版兼容目标内。 |
| sing-box | `?format=singbox`，hy2 使用 `server_ports` 与 `hop_interval`。 |
| Shadowrocket | 使用 Base64 分享链接；具体版本的跳端口和导入效果需在设备上实测。 |

分享链接没有统一的跳跃间隔字段，因此 Base64/URI 导入使用客户端自己的间隔设置；面板自定义间隔保证在 sing-box/Mihomo 完整配置中下发。TUIC 自动跳端口不在本版范围内，不会向订阅写入未经支持的字段。

## 开发与验证

面板：`go test -race ./...`。Linux CI 同时检查安装脚本语法和二进制构建。节点仓库包含真实 QUIC 连接、用户顺序调整、流量归属、撤销会话测试；内核仓库包含认证表并发测试与隔离网络空间内的 nftables 生命周期测试。

CI 与本地回环测试不等于真实服务器验收。首次部署仍需验证 DNS、证书签发、云防火墙和实际客户端连接；Windows 上不会执行 Linux nftables 集成测试。
