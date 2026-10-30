[简体中文](../zh-CN/guide.md) | [繁體中文](../zh-TW/guide.md) | **English**

# User guide

Every feature, page by page, in the order of the panel's top navigation. Not installed yet? Start with [Installation](install.md).

> The panel's interface is in Simplified Chinese only. Button and field names are quoted here exactly as they appear on screen, followed by their meaning.

- [Dashboard](#dashboard)
- [Users](#users)
- [Plans](#plans)
- [Nodes](#nodes)
- [Inbounds](#inbounds)
- [Restrictions](#restrictions)
- [Settings](#settings)
- [Subscriptions and clients](#subscriptions-and-clients)
- [What disconnects people](#what-disconnects-people)

---

## Dashboard

「概览」, the first page:

- **active users / total, online now, nodes online / total, inbounds, total traffic**;
- 「需要处理」(**needs attention**): users about to expire, recently expired or nearly out of traffic — click to jump to the users page;
- a **14-day traffic chart**;
- **per-node traffic**: status, this period's upload and download, the next statistics reset, the last 14 days and all time, busiest first.

---

## Users

「用户」. A user is one customer account with one subscription link.

### Creating a user

| Field | Meaning |
| --- | --- |
| 名称 (name) | 1–32 characters: letters (any script), digits, dot, dash, underscore, starting with a letter or digit. Traffic is counted against this name on every node |
| 套餐 (plan) | pick a [plan](#plans) to fill the fields below; you can still change them |
| 有效天数 (days) | expires this many days from now. If 到期 (expiry) is also set, the expiry wins |
| 到期 (expiry) | an exact expiry time, in the server's time zone. Blank means it never expires |
| 流量上限 GiB (traffic limit) | blank or 0 means unlimited. 1 GiB = 1024³ bytes |
| 同时在线 IP (concurrent IPs) | how many different source addresses may use the account at once; blank means unlimited — see below |
| 流量重置 (traffic reset) | for monthly plans: the day used traffic goes back to zero — 「按创建日」(the day it was created), 「每月 N 号」(the Nth), or 「每月最后一天」(the last day) |
| 重置时间 (reset time) | the time of day the reset happens |
| 备注 (note) | visible only to you |

Days 29–31 fall back to the last day of shorter months, so February is never skipped. When an expiry and a reset land on the same moment, the expiry wins: a reset never revives an expired account.

Once an account expires its used traffic is cleared automatically as well, with or without a reset day, so a renewal starts from zero; the traffic history is kept. An account with neither an expiry nor a reset day is cleared monthly on the anniversary of its creation (created 14 March 09:30 means the 14th of every month at 09:30).

### The user list

Each row shows:

- **Status**: `正常` (active); `已停用` (disabled by you); `已失效` (expired or out of traffic). `在线` (online) is added while the user has a live connection.
- 「可用协议」(**allowed inbounds**): `全部` (all) or e.g. `3 / 8`. Click to change it — see below.
- **IP**: addresses online now / the limit, red when over.
- **Expiry**, **used**, **limit**: under the limit, the percentage used and the next automatic reset.

Links under the name:

- 「活动」(**activity**): how the account has been used recently — see below.
- 「打开订阅」/「复制链接」: open / copy the subscription link.
- 「二维码 / 话术」(**QR code / message**): the subscription QR code, and a ready-to-send message with the link, expiry, traffic and how to import it. Edit it if you like, then 「复制话术」(copy).

Buttons at the end of the row:

| Button | Does |
| --- | --- |
| 编辑 (edit) | change name, expiry, limits, reset day and note. Used traffic is not edited here |
| 续费 (renew) | see below |
| 停用 / 启用 (disable / enable) | disabled users cannot connect; their subscription says the account is disabled |
| 清零 (reset usage) | set used traffic back to zero |
| 重置链接 (new link) | give this user a new subscription link and new passwords — see below |
| 删除 (delete) | delete the user; the subscription stops working at once |

### Needs attention

Above the list, enabled users who need you soon are listed, most urgent first, each with a renew button:

- expiring within 7 days;
- expired within the last 30 days;
- at 90% or more of their traffic (monthly plans show when they reset).

Users you disabled yourself are not listed.

### Renewing

Click 「续费」, enter a number of days (or pick a plan), choose whether to reset used traffic, and confirm:

- New expiry = **the later of the current expiry and now** + the days. Renewing early keeps the remaining days; renewing after expiry starts from now, without giving back the lapsed days.
- Renewing also **enables** the account.
- An account with no expiry gets none; only the traffic reset applies.
- The subscription link does not change; the customer does nothing.

### New link

「重置链接」is for **one user's link having leaked or been resold**:

1. the subscription link is replaced, and the old one stops working at once;
2. the connection passwords are replaced too, so everything imported from the old link stops connecting.

Send the **new link** to the user and have them delete the old subscription and **import the new one** (refreshing the old one cannot work: that link is gone). Nobody else is affected.

> To have a customer simply refresh their node list, **don't** use this — ask them to update the subscription in their client.

### Allowed inbounds

Click 「全部」or `3 / 8` in the user's row:

- 「全部入站（含以后新建）」— **all inbounds, including future ones**;
- 「仅允许勾选项」— **only the ticked inbounds**; new inbounds are not added automatically.

At the top you can select all / clear; per node, 「全选 / 取消 / 仅选此节点」(all / none / only this node). An empty selection cannot be saved — to block a user entirely, disable them.

### Concurrent IPs

Counted by **distinct source address**, not connections (one device opens dozens).

- Over the limit, the **newest address is cut off**; whoever was already connected keeps working.
- An address keeps its slot for **one minute** after its last connection closes, so a locked screen or a reconnect does not lose it; switching from Wi-Fi to mobile data costs at most about a minute.
- **Each node counts on its own**: a user limited to 2 may use 2 addresses on each of two nodes. The user list shows the sum across nodes, which can therefore exceed the limit.
- Connections through a relay all appear to come from the relay, so the limit is looser there, never stricter.

### Activity

Hour by hour, the **shape** of the user's usage: concurrent connections, distinct destinations, distinct destination ports and source addresses — each the peak within the hour.

**Which sites a user visits is not recorded** — the node does not report it and the panel does not store it. The shape is enough: browsing touches a handful of hosts; BitTorrent connects to hundreds of different peers at once; a long bulk download is few destinations with a lot of traffic.

---

## Plans

「套餐」: presets for what you sell, e.g. "Monthly 100G / 3 IPs / $5".

| Field | Meaning |
| --- | --- |
| 名称 (name) | e.g. "Monthly 100G" — spaces and any script allowed |
| 有效天数 (days) | blank or 0 means no expiry |
| traffic limit, concurrent IPs, traffic reset | as for users |
| 备注 (note) | e.g. the price; shown in the dropdown |

Picking a plan when creating or renewing a user fills the form, which you can still edit. **Users keep no link to the plan**: changing or deleting a plan **never affects accounts already sold**.

---

## Nodes

「节点」. Each proxy server is a node; see [Installation](install.md#3-register-and-install-a-node) to add one.

| Field | Meaning |
| --- | --- |
| 名称 (name) | any language. Renaming a node regenerates the names of all its inbounds |
| 客户端连接地址 (client address) | what subscriptions tell clients to connect to (domain or IP). Moving servers is a DNS change, with no new subscriptions |
| 国家 (country) | two-letter code |
| 流量倍率 (traffic rate) | how much of a plan's traffic is charged — see below |
| 订阅排序 (subscription order) | smaller numbers come first in subscriptions (on edit) |
| 节点统计重置 (statistics reset) | when the dashboard's "this period" counter for the node resets each month. Statistics only — plans and history are untouched |

**Traffic rate**, for a customer using 100 GB on the node:

| Rate | Charged to the plan |
| --- | --- |
| `1` | 100 GB |
| `0.5` | 50 GB |
| `0` | 0 GB (free — but a disabled, expired or exhausted account still cannot connect) |
| `2` | 200 GB |

0 to 100, up to three decimals. Actual traffic is still recorded; a new rate applies from then on, without recalculating history or restarting the node.

Also in the list:

- **Status**: `在线` (online) / `离线` (offline) / `已停用` (disabled).
- 「入站」(**inbounds**): their number; click to manage them.
- 「版本」(**version**): node version / core version.
- 「换 token」(**new token**): issues a new join token and invalidates the old one. Put it in `/opt/skysbx/node.env` on the node (the `SKYSBX_TOKEN=` line) and run `systemctl restart skysbx-node`.
- 「停用」(**disable**): the node stops accepting connections and leaves subscriptions.

---

## Inbounds

An inbound is one proxy port on a node, and one entry in a client's node list. Click a node's inbound count to manage them.

### Protocols

| Protocol | Example port | Firewall | You fill in | Good for |
| --- | --- | --- | --- | --- |
| VLESS Reality | 443 | TCP | the Reality handshake site | **the main one** — most robust, no certificate |
| Hysteria2 | 8443 | UDP | certificate, SNI; optional obfs, masquerade, port hopping | fast; turn on obfs where QUIC is interfered with |
| AnyTLS | 8443 | TCP | certificate, SNI | a fallback |
| Shadowsocks 2022 | 8388 | TCP and UDP | nothing (keys are generated) | a fallback with the widest client support |
| TUIC v5 | 9443 | UDP | certificate, SNI | rarely needed: a subset of Hysteria2 (no obfs, masquerade or port hopping) |

Give every node at least one **TCP** protocol (VLESS Reality) and one **UDP** protocol (Hysteria2), so whichever way a customer's network misbehaves, one of them works.

All keys, short ids and Shadowsocks server keys are generated for you.

### Common fields

- **tag** (the inbound's name): what clients display, e.g. "Hong Kong 01". Any script; unique across the panel.
- 「端口」(**port**): one TCP port can serve only one inbound per node. Sharing a host with the panel, keep away from 80 and 443.
- 「证书路径 / 私钥路径」(**certificate / key path**): files on the node; blank means `/opt/skysbx/cert.pem` and `key.pem`, where the installer puts them.
- **SNI**: the domain the node's certificate is for, usually the node domain.
- 「节点内排序」(**order within the node**, on edit): smaller first. Changing only the order does not restart the node.

### VLESS Reality: the handshake site

Reality borrows a real website's TLS handshake. Pick one from the 「常用站点…」(common sites) dropdown beside the field, or type `host:port`.

「检测」(**check**) confirms from the panel that the site does TLS 1.3 with HTTP/2 and a valid certificate. Passing means it works from the panel, not necessarily from everywhere your users are; failing means it will not work anywhere.

### Hysteria2: obfs and masquerade

They guard against different things:

- **Obfs (salamander)** wraps every packet so the traffic **no longer looks like QUIC**. Use it when Hysteria2 is flaky or times out while Reality works — the customer's network is throttling or interfering with QUIC. The password is generated and put in subscriptions; Clash Verge / mihomo, sing-box and Shadowrocket support it.
- **Masquerade** is what a browser or a prober sees when it connects to the port: a real website (e.g. bing.com) instead of an error that gives the proxy away. Server-side only; clients are unaffected. Pick from the dropdown, click 「随机」(random), or type any https URL; different inbounds should use different sites.

**Only one of them is in effect at a time**: with obfs on, a prober cannot even complete the handshake, so it never reaches the masquerade site. Leaving a masquerade set does no harm and takes over if you later turn obfs off.

| Situation | Choose |
| --- | --- |
| Hysteria2 often times out, Reality is fine | obfs on |
| Hysteria2 works; you just want it harder to identify | obfs off, set a masquerade site |

### Hysteria2 port hopping

E.g. listen on `8443`, hop range `20000-30000`, interval `30s`. Clients keep switching ports within the range, which defeats throttling of a single UDP port.

1. Allow UDP 8443 and UDP 20000–30000 in the security group, and UDP 8443 in the host firewall;
2. the node redirects the whole range to the listening port (with nftables, which the installer sets up);
3. customers update their subscription.

Ranges like `20000-30000,40000` are accepted; they may not overlap other inbounds or relay ports. Clear the range to turn hopping off. Direct Linux nodes only; not combined with a relay.

### Relays

Two ways, pick one:

- 「站内中转」(**relay through another node**): pick another node the panel manages; the panel opens a forwarding port on it that passes traffic through unchanged. Nothing is decrypted or authenticated there, so traffic accounting, IP limits and activity are unaffected. Changing the relay rebuilds only the relay node.
- 「外部中转地址」(**external relay address**): for a relay the panel does not manage (realm, nginx stream…), enter `relay.example.net:443`, including the relay's listening port.

### Status

| Status | Meaning |
| --- | --- |
| `已生效` (in effect) | the node runs this inbound with the current settings |
| `确认中…` (confirming) | the settings were just sent; the page refreshes until the node confirms |
| `等待节点应用` (waiting for the node) | the node is still on older settings and has not caught up |
| `跑的是旧配置` (running the old config) | the node refused the latest configuration and still runs the previous one; the reason is shown at the top |
| `未生效` (not in effect) | the node is not running this inbound; see the error at the top and the node's log |
| `节点离线` (node offline) | the node is not connected, so nothing can be confirmed |

### Changing an inbound

- Adding, changing or disabling any inbound **rebuilds that node's configuration, dropping every connection on it once**.
- **Changing the handshake site, switching obfs, or changing the port cuts customers off until they update their subscription.** For an inbound in use, add a new one instead, run both for a week or two, and disable the old one once most customers have refreshed.
- The masquerade site, certificate paths, name and order can change without affecting clients.

---

## Restrictions

「限制」: rules for **every node and every user**.

- 「禁止 BT / PT」(**block BitTorrent**): stops plaintext BitTorrent, uTP and UDP trackers — most BitTorrent traffic — but not encrypted BitTorrent over TCP. It makes BitTorrent hard to use, not impossible.
- 「禁止测速站点」(**block speed tests**): blocks common speed-test sites by domain.
- 「自定义域名黑名单」(**domain blocklist**): one per line, matched as a suffix; `example.com` also blocks `a.b.example.com`.

Saving pushes a new configuration to **every node**, dropping all connections once. With any of these on, nodes inspect the first bytes of each connection to tell its protocol and domain.

There is no switch for "heavy downloading": it is a matter of volume, managed with each user's traffic limit and concurrent IPs.

---

## Settings

「设置」.

### Admin path and root redirect

See [Installation: hide the admin URL](install.md#hide-the-admin-url-recommended).

### Subscription announcement

「订阅公告」: one line per entry (up to 5 lines of 60 characters), shown **at the top of every customer's node list** — e.g. "📢 Website: example.com", "Renewals: contact …". Save it empty to turn it off.

Each line is a placeholder entry that cannot connect, there only to be read. In Clash and sing-box, announcements sit in their own "公告" group at the top and are **never in a group that carries traffic**, so customers cannot connect to one by mistake. Customers see a new announcement after updating their subscription. A line with the same name as a group (such as "AI" or "节点选择") is skipped.

### Changing the domain

「更换域名」 (under 设置): enter the new panel domain, and optionally a subscription domain, then click 「更换域名」.

1. **First** point the new domain's A record at the panel server. The panel checks the DNS and will not save if the name does not resolve to this server. Tick 「跳过解析检查」 only behind a CDN or if the check is wrong;
2. After saving, the panel restarts by itself and comes up on the new domain within a few seconds to a couple of minutes (it obtains a certificate first). Until the new domain works, the old address still lets you sign in;
3. Nodes switch to the new domain by themselves and write it into their own configuration, so **there is no need to log in to the node servers**. Nodes must be on this version or newer; upgrade older ones first;
4. The node on the panel server itself, if its address is the panel domain, moves to the new domain and the new certificate. Customers using its AnyTLS, Hysteria2 or TUIC need to **update their subscription once**;
5. Subscription links copied from now on use the new domain. **The old domain stays in service**, so links already handed out keep working.

Once every node has moved (the page shows how many still connect through the old domain) and customers have their new links, click 「停用旧域名」. Links on the old domain then stop working and the panel stops renewing its certificate. Keeping an old domain that is blocked or has expired does no harm.

### Two-step login

「两步验证」: signing in then needs, besides the password, the 6-digit code from an authenticator app (Google Authenticator, Microsoft Authenticator, 1Password…).

1. Click 「开启两步验证」(turn on) and scan the QR code with the app;
2. enter the 6-digit code to confirm. Nothing changes until a correct code is entered, so a bad scan cannot lock you out;
3. **keep the 10 recovery codes** shown — they appear only once. Each can replace a code once if you lose your phone.

Turning it off or generating new recovery codes needs a current code. **If the phone and the recovery codes are both lost**, on the panel server run:

```bash
/opt/skysbx/skysbx-panel -db /opt/skysbx/skysbx.db -reset-2fa
```

or `veyra` → Panel → 7.

### Force everyone to update

「强制全员更新订阅」: an emergency button, **not for routine use**. Type 「全部断连」 to confirm.

- every user's connection passwords are replaced; **subscription links stay the same**;
- every node rebuilds, dropping all connections;
- customers recover by updating their subscription. Many clients update automatically (the panel suggests every 12 hours), so customers who do not update by hand may be offline for up to about 12 hours — tell them beforehand.

For: suspecting a node server has been compromised, removing a node you no longer trust, suspecting configurations have leaked widely.

| Situation | Use |
| --- | --- |
| **one** user's link leaked | 「重置链接」on the users page |
| **everyone's** configuration may have leaked | force everyone to update |
| a customer just needs a fresh node list | neither — they update the subscription themselves |

---

## Subscriptions and clients

### Importing

Paste the user's subscription link into the client as a subscription and update it. The panel detects the client and picks a format; a parameter forces one:

| Client | Import as | Force the format |
| --- | --- | --- |
| Clash Verge / Clash Party / mihomo | remote subscription | `?format=clash` |
| sing-box | remote profile | `?format=singbox` |
| Shadowrocket | subscription | `?format=shadowrocket` |
| v2rayN and others | subscription group | `?format=base64` |

Opened in a browser, a subscription is a block of Base64 text. That is expected.

### What a subscription contains

- every inbound the user may use, ordered by the nodes' subscription order and each node's inbound order;
- in Clash and sing-box formats, a ready-made set of groups and routing rules, see [Groups and routing](#groups-and-routing) below;
- used traffic, total traffic and expiry, which most clients display (Shadowrocket as "upload / download / total / expiry");
- the [announcement](#subscription-announcement), first, if you set one.

A disabled, expired or exhausted account still gets a valid subscription, holding one entry that states the reason (e.g. "expired on 2026-11-10, please renew", in Chinese) followed by the announcement — so the customer knows to contact you rather than assuming their app is broken.

### Groups and routing

Clash and sing-box subscriptions come with these groups, each of which the customer can switch in their client. Group names are shown as they appear (in Chinese):

| Group | Default | Traffic it carries |
| --- | --- | --- |
| 公告 (announcement) | — | Only present when an announcement is set; for reading only |
| 节点选择 (node selection) | First node | The main switch, listing every node |
| AI | Follows 节点选择 | ChatGPT, Claude, Gemini and similar |
| YouTube | Follows 节点选择 | |
| Netflix | Follows 节点选择 | |
| 谷歌 (Google) | Follows 节点选择 | Search, Gmail, Google Play and so on |
| 苹果 (Apple) | Direct | App Store, iCloud, system updates |
| 微软 (Microsoft) | Direct | Windows Update, Office, Bing and so on |
| 漏网之鱼 (everything else) | Follows 节点选择 | Any traffic none of the above matched |

- **Mainland China sites and IP addresses always go direct**: not through a node, and not counted against the plan. Local network addresses go direct too. GitHub follows 节点选择.
- There is no automatic selection: if a node fails, nothing switches over; the customer picks another one.
- Each group chooses its own node for its own traffic. With AI on a US node, Netflix on Singapore and everything else on Hong Kong, all three are in use at once; traffic is counted on each node at that node's multiplier and taken from the same plan.
- The rules come from [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat). Clients download the rule files from the panel, under `/sub/rules/` on the subscription address, and refresh them daily; the panel syncs them from GitHub every 12 hours. Customers who cannot reach GitHub are unaffected, and there are no lists for you to maintain.
- Shadowrocket and v2rayN use share links, which cannot carry groups or rules; routing there follows the client's own settings (v2rayN has a built-in "bypass mainland" mode; Shadowrocket can add a rule configuration separately).

### Security

A subscription link is a password: Base64 is not encryption, and anyone with the link can import it. Do not share it publicly; if it leaks, use 「重置链接」.

### A separate subscription domain

You can set a separate subscription domain (e.g. `sub.example.com`) at install. Subscriptions are then served only there, and every other path on it (including `/login`) returns 404; the admin UI stays on the panel domain.

---

## What disconnects people

| Action | Effect |
| --- | --- |
| adding / editing / disabling users, renewing, resetting usage, new links | nothing restarts; everyone else stays connected |
| a node's rate, client address, order or statistics reset | no effect on connections |
| renaming a node (which renames its inbounds) | every connection on that node drops once |
| adding / changing / disabling an inbound | every connection on that node drops once |
| saving the Restrictions page | every connection on every node drops once |
| force everyone to update | all connections drop until customers update |
| upgrading the panel | the admin UI is down for a few seconds; **customers are unaffected** |
| upgrading a node | that node's connections drop once |
