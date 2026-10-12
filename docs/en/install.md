[简体中文](../zh-CN/install.md) | [繁體中文](../zh-TW/install.md) | **English**

# Installation

This page takes you from nothing to a working panel and nodes. For how to use each feature afterwards, see the [User guide](guide.md); for upgrades, backups and troubleshooting, see [Maintenance](maintenance.md).

> The panel's interface is in Simplified Chinese only. Button and field names are quoted here exactly as they appear on screen, followed by their meaning.

- [1. Servers and domains](#1-servers-and-domains)
- [2. Install the panel](#2-install-the-panel)
- [3. Register and install a node](#3-register-and-install-a-node)
- [4. Node certificates](#4-node-certificates)
- [5. Panel and node on the same server](#5-panel-and-node-on-the-same-server)
- [6. Next steps](#6-next-steps)

## 1. Servers and domains

**System**: Debian or Ubuntu with systemd, and root access. Run the commands below in the server's SSH terminal after `sudo -i`.

**How it installs**: the scripts download prebuilt binaries (amd64 and arm64) from this project's GitHub Releases, verify their sha256, and install them. **Nothing is compiled on the server**, so even a very small machine installs quickly. The server only needs to reach GitHub.

**Domains** — replace the examples with your own:

| Example | Points to | Used for |
| --- | --- | --- |
| `panel.example.com` | the panel server | the admin UI; nodes connect here |
| `sub.example.com` | the panel server | optional: a separate subscription domain |
| `hk.example.com` | the Hong Kong node | clients connect here; the node's TLS certificate |

- Add A records. Add an AAAA record only if the server's IPv6 actually works; otherwise clients with IPv6 will fail to connect.
- If the domain is on Cloudflare, set it to **DNS only (grey cloud)**. These proxy protocols cannot pass through Cloudflare's HTTP proxy.

**Ports**:

| Server | Open | Why |
| --- | --- | --- |
| Panel | TCP 80, 443 | certificates, the HTTPS admin UI and subscriptions. Nothing else (Nginx, Caddy…) may use them |
| Node | TCP 80 | the default HTTP certificate challenge; not needed with DNS validation |
| Node | the proxy ports you configure (TCP / UDP) | open them in **both** the cloud provider's security group and the host firewall |

Nodes connect out to the panel, so a node needs **no** inbound control port. To see what is using 80/443:

```bash
ss -lntp | grep -E ':(80|443)\b'
```

## 2. Install the panel

On the **panel server**:

```bash
apt-get update && apt-get install -y curl
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash
```

Answer the prompts:

1. **Panel domain** — must already resolve to this server;
2. **Subscription domain** (optional) — press Enter to serve subscriptions from the panel domain;
3. **Let's Encrypt email** (optional);
4. **Administrator username and password** — at least 12 characters, not echoed.

The script downloads the binary, obtains a certificate, creates the administrator and starts the service. The administrator exists before the service is reachable, so nobody can claim the panel first.

Then open `https://panel.example.com/login` and sign in. To check the service:

```bash
systemctl is-active skysbx-panel
journalctl -u skysbx-panel -n 50 --no-pager
```

Run `veyra` on the server for a management menu: status, upgrade, restart, logs, uninstall. It never shows passwords or tokens.

> `skysbx` in service and file names is the project's former name, kept so existing installations keep their data.

### Hide the admin URL (recommended)

After signing in, open 「设置」(Settings) and set 「面板访问路径」(admin path) to something only you know, such as `/my-panel-2026`. The admin UI then lives at:

```text
https://panel.example.com/my-panel-2026
```

The bare domain returns 404, or redirects to a page of your choice. **Node connections and subscription links are unaffected**: changing the path neither disconnects nodes nor breaks subscriptions already handed out.

## 3. Register and install a node

### 3.1 Register it in the panel

Open 「节点」(Nodes), fill in the form and click 「新建」(Create):

| Field | Example | Meaning |
| --- | --- | --- |
| 名称 (name) | `Hong Kong 01` | any language |
| 客户端连接地址 (client address) | `hk.example.com` | what clients connect to; no `https://` |
| 国家 (country) | `HK` | two-letter country code |
| 流量倍率 (traffic rate) | `1` | 1 bills normally, 0.5 half, 0 not at all |

After saving, the panel shows the node's **join token once**. Copy it now and keep it private; if it is lost, use 「换 token」(new token) in the node list. Each node has its own token.

### 3.2 Install on the node server

`hk.example.com` must resolve to **this node server**. On it, run:

```bash
apt-get update && apt-get install -y curl
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash
```

Answer the prompts:

1. **Panel URL**, e.g. `https://panel.example.com`;
2. **Join token** — the one you just copied (not echoed);
3. **Node domain** — required for AnyTLS, Hysteria2 and TUIC (they need a certificate); press Enter if you only use VLESS Reality or Shadowsocks;
4. **Certificate email** (optional).

Back in the panel, the node should show 「在线」(online). On the node:

```bash
systemctl is-active skysbx-node
journalctl -u skysbx-node -n 50 --no-pager
ls -l /opt/skysbx/cert.pem /opt/skysbx/key.pem   # only when using a certificate
```

**Online does not mean usable yet**: add inbounds (proxy ports) to it in the panel — see [User guide: Inbounds](guide.md#inbounds).

## 4. Node certificates

AnyTLS, Hysteria2 and TUIC need a certificate on the node; VLESS Reality and Shadowsocks do not.

### Default: HTTP validation

Obtained automatically when you give a node domain at install. The node's TCP 80 must be reachable and free. Renewal is automatic, and the node restarts to load the new certificate, briefly dropping its connections.

### Cloudflare DNS validation (when TCP 80 is not available)

With the domain on Cloudflare, create an API token that can **only edit DNS for that zone**, then:

```bash
read -rsp 'Cloudflare DNS API token: ' cf_token; printf '\n'
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- \
  --panel https://panel.example.com --domain hk.example.com \
  --email you@example.com --cf-token "$cf_token"
unset cf_token
```

The script still asks for the join token. DNS validation does not mean using the CDN — keep the node's record grey-clouded.

### Bringing your own certificate

Put the full chain and the key in the default location and skip issuance:

```bash
install -d -m 0700 /opt/skysbx
install -m 0644 /your/path/fullchain.pem /opt/skysbx/cert.pem
install -m 0600 /your/path/privkey.pem /opt/skysbx/key.pem
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- \
  --panel https://panel.example.com --domain hk.example.com --no-cert
```

Renewal and reloading are then up to you.

## 5. Panel and node on the same server

**Use this section instead of sections 2 and 3.** The panel domain (and the optional subscription domain) point to this server:

```bash
apt-get update && apt-get install -y curl
curl -4 -fL --retry 3 --connect-timeout 15 \
  https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-panel-and-node.sh \
  -o /tmp/veyra-all-install.sh && bash /tmp/veyra-all-install.sh
```

It walks you through it: panel domain, subscription domain and email → the administrator → you create the node in the panel → paste its join token back in SSH → done.

- Use the panel domain as the node's client address and as the SNI of TLS inbounds. The node reuses the panel's certificate rather than requesting another.
- The panel already holds TCP 80 and 443, so give proxies other ports, e.g. Reality on 10443 and AnyTLS on 8443.

## 6. Next steps

1. Add inbounds to the node: [User guide: Inbounds](guide.md#inbounds)
2. Create users and send them their subscription link: [User guide: Users](guide.md#users)
3. Turn on two-step login for the admin UI: [User guide: Settings](guide.md#settings)
