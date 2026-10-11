# Veyra

自建代理面板：一个面板管理多台节点、用户、套餐和订阅。面板和节点都在这个仓库里。

[![CI](https://github.com/Zayvian/veyra-panel/actions/workflows/ci.yml/badge.svg)](https://github.com/Zayvian/veyra-panel/actions/workflows/ci.yml)

| 目录 | 是什么 | 装在哪里 | 许可证 |
| --- | --- | --- | --- |
| [`panel/`](panel/) | 管理面板：用户、节点、订阅、计费 | 面板服务器 | AGPL-3.0（[`panel/LICENSE`](panel/LICENSE)） |
| [`node/`](node/) | 节点：内置 sing-box，接收面板下发的配置并上报流量 | 每台代理服务器 | GPL-3.0（[`node/LICENSE`](node/LICENSE)） |
| [`protocol/`](protocol/) | 面板和节点之间的控制协议说明和样例 | — | 随两边 |
| [`deploy/`](deploy/) | 两边共用的 `veyra` 管理菜单和下载校验脚本 | — | — |

节点内置的协议内核是 [veyra-core](https://github.com/Zayvian/veyra-core)（打了热更新补丁的 sing-box，GPL-3.0）。它单独一个仓库，用 [`node/core.lock`](node/core.lock) 锁定具体版本。

## 安装

面板（在面板服务器上）：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash
```

节点（在每台代理服务器上，先在面板里登记节点拿到 token）：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash
```

升级都是同一条命令加 `-s -- --upgrade`。安装和升级默认下载 [Release](https://github.com/Zayvian/veyra-panel/releases) 里 CI 编译好的程序，核对 sha256 后安装，**服务器上不编译**。加 `--from-source` 才在服务器上编译。

面板和节点装在同一台机器上、卸载、证书、故障排查等，见 [面板说明](panel/README.md) 和 [节点说明](node/README.md)。

旧的节点安装地址 `https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh` 会自动转到新地址，已经装好的节点照常升级。

## 发布

每次合并到 `main` 并通过测试后，CI 会自动：

1. 按 `node/core.lock` 检出 veyra-core；
2. 编译面板和节点的 linux amd64 / arm64 版本；
3. 发布为一个新的 Release（`build-日期-提交号`），附带 `sha256sums.txt`。

只保留最近 10 个 Release。

## 许可证

两个目录的许可证不同：`panel/` 是 AGPL-3.0，`node/` 是 GPL-3.0，各自的 `LICENSE` 和 `NOTICE` 在各自目录里。面板不链接任何 sing-box 代码，CI 里有一条检查保证这一点；两边只通过 [`protocol/`](protocol/PROTOCOL.md) 描述的 JSON 消息通信。
