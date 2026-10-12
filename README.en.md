[简体中文](README.md) | [繁體中文](README.zh-TW.md) | **English**

# Veyra

A self-hosted proxy panel: one web admin for several proxy servers (nodes), customer accounts, plans and subscription links. For your own use, or for handing out accounts on a small scale.

[![CI](https://github.com/Zayvian/veyra-panel/actions/workflows/ci.yml/badge.svg)](https://github.com/Zayvian/veyra-panel/actions/workflows/ci.yml)

> The panel's interface is in Simplified Chinese only; the documentation explains every label.

## Features

- **Protocols**: VLESS Reality, Hysteria2 (obfs, masquerade, port hopping), AnyTLS, Shadowsocks 2022, TUIC v5
- **Users**: expiry, traffic limits, monthly resets, concurrent-IP limits, per-inbound access, one-click renewal, plan presets
- **Subscriptions**: Clash / mihomo, sing-box, Shadowrocket and plain share links, detected per client; Clash and sing-box come with groups (AI, YouTube, Netflix and more) and mainland-China-direct rules; QR codes and a ready-to-send message; an announcement at the top of every list; inactive accounts see why
- **Nodes**: traffic rates (half price, free…), relaying through another node, per-node traffic statistics
- **No needless disconnects**: adding or removing users, renewals and new passwords are hot-swapped without dropping anyone else; upgrading the panel does not touch customers
- **Security**: a hidden admin path, two-step login, per-user link reissue, an emergency credential rotation for everyone
- **Simple installs**: one command, downloading prebuilt binaries (amd64 / arm64) — nothing is compiled on the server

## Quick start

You need a Debian / Ubuntu server, root, and a domain pointing at it.

**Panel** (on the panel server):

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash
```

**Node** (on each proxy server — first create the node on the panel's 「节点」page and copy its join token):

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash
```

**Upgrade**: the same command followed by `-s -- --upgrade`.

## Documentation

| Document | Covers |
| --- | --- |
| [Installation](docs/en/install.md) | domains and ports, installing the panel and nodes, certificates, panel and node on one server |
| [User guide](docs/en/guide.md) | every page and feature: users, plans, nodes, inbounds, restrictions, settings, subscriptions |
| [Maintenance](docs/en/maintenance.md) | upgrades, backup and restore, lost passwords, uninstalling, troubleshooting |
| [Protocol](protocol/PROTOCOL.md) | the messages between panel and node (for developers, in Chinese) |

## Repository layout

| Directory | Contents | Licence |
| --- | --- | --- |
| [`panel/`](panel/) | the admin panel | AGPL-3.0 |
| [`node/`](node/) | the node, embedding sing-box | GPL-3.0 |
| [`protocol/`](protocol/) | the panel–node protocol and its samples | — |
| [`deploy/`](deploy/) | the shared `veyra` menu and download verification | — |

The node's protocol core is [veyra-core](https://github.com/Zayvian/veyra-core), sing-box with patches such as changing users without a restart. It lives in its own repository, pinned by [`node/core.lock`](node/core.lock).

**Releases**: every merge to `main` that passes the tests is built by CI for linux amd64 / arm64 (the node against the core in `core.lock`) and published as a [Release](https://github.com/Zayvian/veyra-panel/releases) with `sha256sums.txt`. The installers verify a download before installing it.

## Licence

`panel/` is AGPL-3.0 and `node/` is GPL-3.0, each with its own `LICENSE` and `NOTICE`. The panel contains no sing-box code (CI checks this); the two talk only over the network.

Veyra is developed from [kosje/skysbx-panel](https://github.com/kosje/skysbx-panel) and [kosje/skysbx-node](https://github.com/kosje/skysbx-node). The original authorship is retained; it does not imply the original author's involvement or endorsement. Veyra is not affiliated with sing-box — please do not report Veyra problems there.

Questions and suggestions: [Issues](https://github.com/Zayvian/veyra-panel/issues).
