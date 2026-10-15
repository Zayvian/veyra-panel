[简体中文](../zh-CN/maintenance.md) | **繁體中文** | [English](../en/maintenance.md)

# 維護

- [1. 升級](#1-升級)
- [2. 備份與還原](#2-備份與還原)
- [3. 忘記密碼、兩步驟驗證、更換 token](#3-忘記密碼兩步驟驗證更換-token)
- [4. 解除安裝](#4-解除安裝)
- [5. 排查問題](#5-排查問題)
- [6. 進階](#6-進階)

---

## 1. 升級

每次有新的提交合併進 `main` 並通過自動測試後，專案會自動發布一個新的 [Release](https://github.com/Zayvian/veyra-panel/releases)（名稱是 `build-日期-提交編號`），裡面是編譯好的面板和節點程式。升級就是下載最新的 Release 並替換程式，**伺服器上不編譯**。

### 升級面板

在面板伺服器上執行：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash -s -- --upgrade
```

面板會重新啟動幾秒鐘，期間後台打不開，**客戶的連線不受影響**。資料庫結構會在啟動時自動升級，使用者、節點、訂閱都會保留。

### 升級節點

在每台節點伺服器上執行：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- --upgrade
```

會從 `/opt/skysbx/node.env` 讀取面板網址和 token，不用重新登記，也不會重新申請憑證。**節點會重新啟動，這台節點上的連線斷一次**，建議一台一台升級。

也可以在伺服器上執行 `veyra`，從選單裡選「升級」。

### 要不要升級節點

每個 Release 都同時包含面板和節點，但**很多版本只改了面板**。節點的版本號是編譯它的那次提交，所以即使節點程式碼沒變，新 Release 裡的節點版本號也會變。請看 Release 對應的提交說明：沒有涉及 `node/` 的修改，節點就不用升級。

面板和節點之間的通訊是向下相容的，新面板配舊節點、舊面板配新節點都能正常運作，可以分開升級。

### 查看版本

```bash
/opt/skysbx/skysbx-panel -version
/opt/skysbx/skysbx-node -version
```

面板的「节点」頁也會顯示每台節點的版本。

### 安裝指定版本（回退）

在指令前加上 `VEYRA_RELEASE=Release 名稱`，例如：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | VEYRA_RELEASE=build-20261012-ce58403 bash -s -- --upgrade
```

只保留最近 10 個 Release。**回退面板時，如果新版本升級過資料庫結構，只換回舊程式可能不行**，要連同升級前的資料庫備份一起還原，請見[備份與還原](#2-備份與還原)。

### 在伺服器上編譯

CPU 不是 amd64 / arm64，或想自己編譯時，加上 `--from-source`。下載不到編譯好的程式時，也會自動改為本機編譯。本機編譯會先安裝 Docker，在規格低的機器上編譯節點可能需要很久。

### 面板和節點在同一台伺服器

```bash
curl -fL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-panel-and-node.sh -o /tmp/veyra-all-install.sh
sh /tmp/veyra-all-install.sh --upgrade
```

會依序升級面板和節點。只有面板有變動時，單獨升級面板即可。

### 新增獨立的訂閱網域

先把新網域解析到面板伺服器，再執行：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash -s -- --upgrade --sub-domain sub.example.com
```

設定後，只有這個網域能存取訂閱，原本用面板網域的訂閱連結會回傳 404。使用者頁複製出來的會是新連結，**已經發出去的舊連結要換成新連結**。

也可以不用命令：在面板「设置 → 更换域名」裡填訂閱網域。這樣舊連結會繼續能用，見[使用指南：更換網域](guide.md#更換網域)。更換面板網域也在那裡。

---

## 2. 備份與還原

### 要備份什麼

面板的全部資料（使用者、節點、訂閱、流量、設定）都在 **`/opt/skysbx/skysbx.db`** 這一個檔案裡。完整備份還包括：

| 檔案 | 內容 |
| --- | --- |
| `/opt/skysbx/skysbx.db`（以及同名的 `-wal`、`-shm`） | 資料庫 |
| `/opt/skysbx/panel.env`、`node.env` | 網域、面板網址、節點 token |
| `/opt/skysbx/certs`、`cert.pem`、`key.pem` | 面板和節點的憑證 |
| `/opt/skysbx/skysbx-panel`、`skysbx-node` | 目前的程式，方便回退 |
| `/etc/systemd/system/skysbx-*.service` | 服務設定 |
| `/etc/letsencrypt`（如果有） | 節點憑證和續期設定 |

備份裡有私鑰、token 和資料庫，**請放在私密的地方**，不要上傳到公開倉庫、群組或 Issues。另外存一份到伺服器以外，升級前尤其要備份。

### 建立備份

用 root 執行。它會短暫停止 Veyra 服務以確保資料庫完整，完成後自動恢復：

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

### 還原

還原會回到備份時的狀態，備份之後新增的使用者和流量紀錄會遺失。

```bash
backup_file=/root/veyra-backups/veyra-20261012-120000.tar.gz   # 換成你的備份檔
sha256sum -c "$backup_file.sha256"
systemctl stop skysbx-panel            # 節點伺服器改為 skysbx-node；同機則兩個都停
saved_dir="/opt/skysbx-before-restore-$(date +%Y%m%d-%H%M%S)"
test ! -e "$saved_dir" && mv /opt/skysbx "$saved_dir"
tar -xzpf "$backup_file" -C /
systemctl daemon-reload
systemctl start skysbx-panel           # 節點伺服器改為 skysbx-node
```

確認還原成功之前，請保留 `/opt/skysbx-before-restore-*`。遷移到新伺服器時，還要調整 DNS、防火牆，並確認憑證續期正常。

---

## 3. 忘記密碼、兩步驟驗證、更換 token

### 重設管理員密碼

在面板伺服器上執行（`admin` 換成你的管理員帳號）：

```bash
read -rsp '新密碼: ' p; printf '\n'
printf '%s' "$p" | /opt/skysbx/skysbx-panel -db /opt/skysbx/skysbx.db -set-admin admin
unset p
```

密碼至少 12 個字元。

### 兩步驟驗證的手機和復原碼都遺失了

```bash
/opt/skysbx/skysbx-panel -db /opt/skysbx/skysbx.db -reset-2fa
```

或執行 `veyra` 選單 → Panel → 7。之後用密碼登入，再到「设置」裡重新開啟。

### 更換節點 token

1. 在面板「节点」頁點這台節點的「换 token」，複製新的 token；
2. 在節點伺服器上編輯 `/opt/skysbx/node.env`，只替換 `SKYSBX_TOKEN=` 後面的值；
3. 執行 `systemctl restart skysbx-node`，確認節點重新上線。

舊 token 立即失效。節點 token 和使用者的訂閱連結是兩回事。

---

## 4. 解除安裝

先想好要不要保留資料：

- `--uninstall`：只刪除程式和服務，**保留**資料庫、設定、憑證。之後用同一個腳本的 `--upgrade` 就能原樣裝回來。
- `--purge`：**完全刪除**，包括資料庫、所有使用者和訂閱、憑證，**無法復原**。

```bash
# 面板
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh -o /root/veyra-panel-install.sh
bash /root/veyra-panel-install.sh --uninstall     # 或 --purge

# 節點
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh -o /root/veyra-node-install.sh
bash /root/veyra-node-install.sh --uninstall      # 或 --purge
```

面板和節點在同一台伺服器時，**先清理節點，再清理面板**。`--purge` 不會刪除 curl、git、nftables、certbot 這類可能被其他程式使用的軟體；Docker 只有確認是安裝腳本自己裝的才會刪除。

---

## 5. 排查問題

先看日誌：

```bash
systemctl status skysbx-panel --no-pager
journalctl -u skysbx-panel -n 100 --no-pager
systemctl status skysbx-node --no-pager
journalctl -u skysbx-node -n 100 --no-pager
```

| 現象 | 先檢查 |
| --- | --- |
| 面板打不開、憑證申請失敗 | 網域解析、TCP 80 / 443 是否放行或被佔用、面板日誌 |
| 訂閱網域首頁顯示 404 | 正常，訂閱網域只開放 `/sub/` 路徑 |
| 節點離線 | 節點能否連到面板的 HTTPS 網址、`node.env` 裡的網址和 token、節點日誌 |
| 節點在線但入站顯示「未生效」 | 連接埠衝突、憑證路徑和 SNI、入站頁上方的錯誤訊息 |
| 入站一直「等待节点应用」 | 節點日誌裡的設定錯誤；節點版本是否太舊 |
| Hysteria2 / TUIC 連不上 | 防火牆是否放行 **UDP**、憑證是否有效；HY2 跳連接埠還要放行整個範圍 |
| HY2 時好時壞、常常逾時 | 客戶的網路可能在干擾 QUIC：新建一個開啟混淆的 HY2 入站 |
| 客戶訂閱裡沒有可用節點 | 使用者是否停用、到期、流量用完；使用者的「可用协议」；節點或入站是否停用 |
| 客戶常常被斷線 | 是否設了較低的同時線上 IP 上限（手機切換網路會換 IP） |
| 改了名稱但用戶端還是舊名稱 | 用戶端需要更新訂閱 |
| 升級時顯示 `compiling here instead` | 沒下載到編譯好的程式，改為本機編譯了。請檢查伺服器能否連上 GitHub |

提問請到 [Issues](https://github.com/Zayvian/veyra-panel/issues)，附上版本、部署方式和去除敏感資訊的日誌。

---

## 6. 進階

### 自訂安裝目錄

所有安裝、升級、備份指令前都加上同一個 `VEYRA_ROOT=/你的目錄`。憑證路徑不會自動修改，要逐一核對 TLS 入站裡的憑證路徑。

### 自己的反向代理

預設安裝由面板自己處理 HTTPS。如果你已有自己的反向代理，可以讓面板只監聽本機：`skysbx-panel --addr 127.0.0.1:8080 --sub-domain sub.example.com --db /opt/skysbx/skysbx.db`，由反向代理負責 TLS，並保留 Host、設定正確的 `X-Forwarded-Proto`。這種方式需要自己維護 systemd 服務檔，安裝腳本升級時不會保留你的自訂參數。

### 開發與建置

倉庫結構、授權和發布流程請見[專案首頁](../../README.zh-TW.md)。本機建置：

```bash
git clone https://github.com/Zayvian/veyra-panel.git
cd veyra-panel/panel && go test ./...          # 面板：Go 1.27
```

節點需要把 [veyra-core](https://github.com/Zayvian/veyra-core) 複製到倉庫旁邊，切換到 `node/core.lock` 指定的版本，並使用 Go 1.26.x（1.27 無法連結 sing-box）：

```bash
git clone --branch "$(sed -n 's/^CORE_REF=//p' veyra-panel/node/core.lock)" https://github.com/Zayvian/veyra-core.git
cd veyra-panel/node
go test -tags 'with_clash_api,with_v2ray_api,with_utls,with_acme,with_quic' ./...
```

面板和節點之間的通訊協定請見 [protocol/PROTOCOL.md](../../protocol/PROTOCOL.md)。
