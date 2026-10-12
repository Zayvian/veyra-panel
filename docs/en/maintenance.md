[简体中文](../zh-CN/maintenance.md) | [繁體中文](../zh-TW/maintenance.md) | **English**

# Maintenance

- [1. Upgrading](#1-upgrading)
- [2. Backup and restore](#2-backup-and-restore)
- [3. Lost password, two-step login, node tokens](#3-lost-password-two-step-login-node-tokens)
- [4. Uninstalling](#4-uninstalling)
- [5. Troubleshooting](#5-troubleshooting)
- [6. Advanced](#6-advanced)

---

## 1. Upgrading

Every change merged into `main` that passes the automated tests is published as a new [Release](https://github.com/Zayvian/veyra-panel/releases) (named `build-<date>-<commit>`) with prebuilt panel and node binaries. Upgrading downloads the latest release and swaps the binary — **nothing is compiled on the server**.

### The panel

On the panel server:

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash -s -- --upgrade
```

The panel restarts; the admin UI is unavailable for a few seconds, and **customers' connections are unaffected**. The database schema is migrated on start; users, nodes and subscriptions are kept.

### A node

On each node server:

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- --upgrade
```

The panel URL and token are read from `/opt/skysbx/node.env`, so there is nothing to re-register and no certificate is requested again. **The node restarts, dropping its connections once** — upgrade nodes one at a time.

`veyra` on the server offers the same upgrade from its menu.

### Does the node need upgrading?

Each release contains both binaries, but **many releases only change the panel**. A node binary's version is the commit it was built from, so it changes with every release even when the node's code did not. Check the commits behind a release: if nothing under `node/` changed, nodes need no upgrade.

The panel–node protocol is backward compatible both ways — a newer panel with older nodes and the reverse both work — so they can be upgraded separately.

### Versions

```bash
/opt/skysbx/skysbx-panel -version
/opt/skysbx/skysbx-node -version
```

The panel's 「节点」(Nodes) page also shows each node's version.

### A specific version (rolling back)

Prefix the command with `VEYRA_RELEASE=<release name>`:

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | VEYRA_RELEASE=build-20261012-ce58403 bash -s -- --upgrade
```

Only the 10 most recent releases are kept. **Rolling the panel back across a database migration needs the database backup from before the upgrade**, not just the old binary — see [Backup and restore](#2-backup-and-restore).

### Compiling on the server

Add `--from-source` to build on the server instead — for CPUs other than amd64/arm64, or if you prefer. It also happens automatically when no binary can be downloaded. This installs Docker first, and building the node on a small machine can take a long time.

### Panel and node on one server

```bash
curl -fL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-panel-and-node.sh -o /tmp/veyra-all-install.sh
sh /tmp/veyra-all-install.sh --upgrade
```

This upgrades both. If only the panel changed, upgrading just the panel is enough.

### Adding a separate subscription domain

Point the new domain at the panel server, then:

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash -s -- --upgrade --sub-domain sub.example.com
```

From then on subscriptions are served only on that domain, and links using the panel domain return 404. The users page copies the new links; **links already handed out must be replaced**.

---

## 2. Backup and restore

### What to back up

All of the panel's data — users, nodes, subscriptions, traffic, settings — is in one file, **`/opt/skysbx/skysbx.db`**. A complete backup also covers:

| File | Contents |
| --- | --- |
| `/opt/skysbx/skysbx.db` (and its `-wal`, `-shm`) | the database |
| `/opt/skysbx/panel.env`, `node.env` | domains, panel URL, node token |
| `/opt/skysbx/certs`, `cert.pem`, `key.pem` | panel and node certificates |
| `/opt/skysbx/skysbx-panel`, `skysbx-node` | the current binaries, for rolling back |
| `/etc/systemd/system/skysbx-*.service` | service definitions |
| `/etc/letsencrypt` (if present) | node certificates and renewal |

Backups contain private keys, tokens and the database: **keep them private** — not in public repositories, chats or issues. Keep a copy off the server, and always back up before upgrading.

### Making a backup

As root. It briefly stops Veyra so the database is consistent, then starts it again:

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

### Restoring

Restoring returns to the moment of the backup; users and traffic added since are lost.

```bash
backup_file=/root/veyra-backups/veyra-20261012-120000.tar.gz   # your backup
sha256sum -c "$backup_file.sha256"
systemctl stop skysbx-panel            # skysbx-node on a node; both on a shared host
saved_dir="/opt/skysbx-before-restore-$(date +%Y%m%d-%H%M%S)"
test ! -e "$saved_dir" && mv /opt/skysbx "$saved_dir"
tar -xzpf "$backup_file" -C /
systemctl daemon-reload
systemctl start skysbx-panel           # skysbx-node on a node
```

Keep `/opt/skysbx-before-restore-*` until you are sure the restore worked. Moving to a new server also means updating DNS and the firewall, and checking certificate renewal.

---

## 3. Lost password, two-step login, node tokens

### Resetting the administrator password

On the panel server (replace `admin` with your administrator's name):

```bash
read -rsp 'New password: ' p; printf '\n'
printf '%s' "$p" | /opt/skysbx/skysbx-panel -db /opt/skysbx/skysbx.db -set-admin admin
unset p
```

At least 12 characters.

### Lost both the phone and the recovery codes

```bash
/opt/skysbx/skysbx-panel -db /opt/skysbx/skysbx.db -reset-2fa
```

or `veyra` → Panel → 7. Sign in with the password and turn it on again under 「设置」(Settings).

### Replacing a node's token

1. On the panel's 「节点」(Nodes) page, click the node's 「换 token」(new token) and copy it;
2. on the node, edit `/opt/skysbx/node.env` and replace only the value after `SKYSBX_TOKEN=`;
3. run `systemctl restart skysbx-node` and check the node comes back online.

The old token stops working at once. A node's token has nothing to do with users' subscription links.

---

## 4. Uninstalling

Decide whether to keep the data:

- `--uninstall` removes the program and service but **keeps** the database, configuration and certificates. `--upgrade` with the same script brings it back as it was.
- `--purge` **removes everything**, including the database, every user and subscription, and certificates. **There is no undo.**

```bash
# panel
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh -o /root/veyra-panel-install.sh
bash /root/veyra-panel-install.sh --uninstall     # or --purge

# node
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh -o /root/veyra-node-install.sh
bash /root/veyra-node-install.sh --uninstall      # or --purge
```

On a shared host, **remove the node first, then the panel**. `--purge` leaves shared software such as curl, git, nftables and certbot alone, and removes Docker only if the installer itself installed it.

---

## 5. Troubleshooting

Start with the logs:

```bash
systemctl status skysbx-panel --no-pager
journalctl -u skysbx-panel -n 100 --no-pager
systemctl status skysbx-node --no-pager
journalctl -u skysbx-node -n 100 --no-pager
```

| Symptom | Check first |
| --- | --- |
| panel unreachable, certificate fails | DNS, TCP 80/443 open and free, the panel log |
| the subscription domain's front page is 404 | expected — it serves only `/sub/` |
| node offline | can the node reach the panel over HTTPS; the URL and token in `node.env`; the node log |
| node online but an inbound shows 「未生效」(not in effect) | port conflicts, certificate path and SNI, the error at the top of the inbounds page |
| an inbound stuck at 「等待节点应用」(waiting for the node) | configuration errors in the node log; an outdated node |
| Hysteria2 / TUIC cannot connect | **UDP** allowed in the firewall, a valid certificate; for hopping, the whole range allowed |
| Hysteria2 flaky, frequent timeouts | the customer's network may interfere with QUIC: add a Hysteria2 inbound with obfs on |
| a customer's subscription has no usable nodes | is the user disabled, expired, out of traffic; their allowed inbounds; disabled nodes or inbounds |
| customers keep getting disconnected | a low concurrent-IP limit (phones change address when switching networks) |
| a renamed entry still shows the old name | the client needs to update the subscription |
| an upgrade prints `compiling here instead` | no binary could be downloaded, so it compiled locally — check the server can reach GitHub |

Ask in [Issues](https://github.com/Zayvian/veyra-panel/issues) with the version, how it is deployed, and logs with secrets removed.

---

## 6. Advanced

### A custom install directory

Prefix every install, upgrade and backup command with the same `VEYRA_ROOT=/your/dir`. Certificate paths in inbounds are not rewritten — check each TLS inbound.

### Your own reverse proxy

By default the panel terminates HTTPS itself. Behind your own reverse proxy, run it on loopback — `skysbx-panel --addr 127.0.0.1:8080 --sub-domain sub.example.com --db /opt/skysbx/skysbx.db` — and let the proxy handle TLS, preserve Host, and set `X-Forwarded-Proto`. You then maintain the systemd unit yourself; the installer's upgrade does not keep custom arguments.

### Development

Repository layout, licences and releases: see the [project page](../../README.en.md). Building locally:

```bash
git clone https://github.com/Zayvian/veyra-panel.git
cd veyra-panel/panel && go test ./...          # panel: Go 1.27
```

The node needs [veyra-core](https://github.com/Zayvian/veyra-core) cloned beside the repository at the version in `node/core.lock`, and Go 1.26.x (1.27 cannot link sing-box):

```bash
git clone --branch "$(sed -n 's/^CORE_REF=//p' veyra-panel/node/core.lock)" https://github.com/Zayvian/veyra-core.git
cd veyra-panel/node
go test -tags 'with_clash_api,with_v2ray_api,with_utls,with_acme,with_quic' ./...
```

The panel–node protocol is described in [protocol/PROTOCOL.md](../../protocol/PROTOCOL.md) (in Chinese).
