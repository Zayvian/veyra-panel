# 面板 ↔ 节点控制协议

节点主动连接面板的 `wss://<面板>/api/v1/node/connect`，用 `Authorization: Bearer <join token>` 认证。之后双方在这条 WebSocket 上互发 JSON 文本帧。节点不需要开放任何控制端口。

两边各自定义了一份消息结构体：面板在 `panel/internal/hub/proto.go`，节点在 `node/internal/proto/proto.go`。两边**故意不共用 Go 包**：面板和节点的许可证不同，它们之间的约定只有线上的 JSON 格式。保证两边一致的是 `testdata/` 里的样例。

## 信封

每一帧都是一个信封：

| 字段 | 类型 | 说明 |
|---|---|---|
| `t` | string | 消息类型，见下表 |
| `id` | uint64，可省略 | 命令和回复用它对应；单向上报时省略 |
| `d` | object，可省略 | 消息内容，结构由 `t` 决定 |

## 消息

| `t` | 方向 | `d` 的结构 | 说明 |
|---|---|---|---|
| `hello` | 节点 → 面板 | `version` `os` `arch` `hostname` `singbox_version` | 连接后第一条消息。面板收到后立即下发 `config` 和 `users` |
| `config` | 面板 → 节点 | `config`：完整的 sing-box 配置，入站里不带用户 | 节点重建 sing-box 实例。配置和上次完全相同时节点不重建 |
| `users` | 面板 → 节点 | `by_tag`：入站 tag → 用户列表；`stats_users`：统计白名单；`ip_limits`：用户名 → 同时在线 IP 上限（可省略） | 全量替换，不是增量。节点热替换用户，不断开其他连接 |
| `ok` | 节点 → 面板 | 无 | 对某个 `id` 的命令执行成功 |
| `error` | 节点 → 面板 | `msg` | 对某个 `id` 的命令执行失败，节点继续运行旧配置 |
| `state` | 节点 → 面板 | `inbounds`：实际在运行的入站 tag；`error`（可省略） | 每次尝试应用配置后发送，说明节点实际在跑什么 |
| `stats` | 节点 → 面板 | `users`：用户名 → `{up, down}`；`system`（可省略） | 距上次上报的**增量**字节数，不是累计值 |
| `online` | 节点 → 面板 | `users`；`ips`（可省略）；`activity`（可省略） | 当前在线用户、各自的来源 IP 数和连接形态（只有计数，不含目标地址） |
| `ping` | 面板 → 节点 | 无 | 保活 |
| `pong` | 节点 → 面板 | 无 | 回应 `ping`，`id` 相同 |

用户条目的字段：`name`，以及按协议使用的 `uuid`、`flow`（VLESS）或 `password`（AnyTLS、Shadowsocks 等）。

## 修改协议的规矩

1. **先改 `testdata/` 里的样例**，再改两边的结构体。
2. 面板的 `panel/internal/hub` 和节点的 `node/internal/proto` 各有一个测试：把每份样例解析成自己的结构体，再编码回 JSON，结果必须和样例完全一致。字段改名、漏字段、`omitempty` 写错，都会让其中一边的测试失败。
3. 两边测试都通过之后才能合并。
4. 新字段要能被旧版本忽略：旧节点不认识的字段会被丢弃，旧面板同理。需要双方都支持才能启用的功能，要先在 `hello` 里协商。
