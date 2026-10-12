[简体中文](../zh-CN/install.md) | **繁體中文** | [English](../en/install.md)

# 安裝

本頁帶你從零裝好一個面板和若干節點。裝好之後各項功能怎麼用，請見[使用指南](guide.md)；升級、備份和排錯請見[維護](maintenance.md)。

> 面板介面目前只有簡體中文。文中「」裡的按鈕和欄位名稱照介面原文顯示，方便對照。

- [1. 準備伺服器和網域](#1-準備伺服器和網域)
- [2. 安裝面板](#2-安裝面板)
- [3. 登記並安裝節點](#3-登記並安裝節點)
- [4. 節點憑證](#4-節點憑證)
- [5. 面板和節點裝在同一台伺服器](#5-面板和節點裝在同一台伺服器)
- [6. 下一步](#6-下一步)

## 1. 準備伺服器和網域

**系統**：Debian 或 Ubuntu，使用 systemd，需要 root 權限。下面的指令都在伺服器的 SSH 終端機裡執行，請先執行 `sudo -i` 切換到 root。

**下載方式**：安裝腳本預設從本專案的 GitHub Release 下載編譯好的程式（支援 amd64 和 arm64），核對 sha256 後安裝，**不在伺服器上編譯**，規格很低的機器也能很快裝好。伺服器只需要能連上 GitHub。

**網域**：下面的範例網域都要換成你自己的。

| 範例 | 解析到 | 用途 |
| --- | --- | --- |
| `panel.example.com` | 面板伺服器 IP | 管理後台、節點連線面板 |
| `sub.example.com` | 面板伺服器 IP | 選用：獨立的訂閱網域 |
| `hk.example.com` | 香港節點伺服器 IP | 用戶端連線節點、節點的 TLS 憑證 |

- 新增 A 紀錄。只有伺服器的 IPv6 確實可用時才新增 AAAA 紀錄，否則有 IPv6 的客戶會連不上。
- 如果網域託管在 Cloudflare，請設為 **DNS only（灰色雲朵）**。這些代理協定不能經過 Cloudflare 的 HTTP 代理。

**連接埠**：

| 伺服器 | 需要開放的連接埠 | 說明 |
| --- | --- | --- |
| 面板 | TCP 80、443 | 自動申請憑證、HTTPS 後台和訂閱。安裝前不能被其他程式（如 Nginx、Caddy）佔用 |
| 節點 | TCP 80 | 預設用 HTTP 驗證申請憑證；改用 DNS 驗證時可不開放 |
| 節點 | 你設定的代理連接埠（TCP / UDP） | 雲端服務商的安全群組和本機防火牆**都要放行** |

節點會主動連線面板，所以節點**不需要**開放任何控制連接埠。檢查連接埠是否被佔用：

```bash
ss -lntp | grep -E ':(80|443)\b'
```

## 2. 安裝面板

在**面板伺服器**上執行：

```bash
apt-get update && apt-get install -y curl
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash
```

依照提示依序填寫：

1. **面板網域**：必須已經解析到這台伺服器；
2. **訂閱網域**（選用）：直接按 Enter 表示訂閱也用面板網域；
3. **Let's Encrypt 電子郵件**（選用）；
4. **管理員帳號和密碼**：密碼至少 12 個字元，輸入時不會顯示。

腳本會下載程式、申請憑證、建立管理員並啟動服務。管理員在服務對外開放之前就已建立，不會有被別人搶先註冊的空檔。

完成後開啟 `https://panel.example.com/login` 登入。檢查服務狀態：

```bash
systemctl is-active skysbx-panel
journalctl -u skysbx-panel -n 50 --no-pager
```

安裝完成後，在伺服器上輸入 `veyra` 可以開啟管理選單：查看狀態、升級、重新啟動、看日誌、解除安裝。選單裡不會顯示密碼、token 之類的敏感資訊。

> 服務和檔案名稱裡的 `skysbx` 是舊名字，為了相容舊版的資料而保留，不影響使用。

### 隱藏後台網址（建議）

登入後進入「设置」，在「面板访问路径」填一個只有你知道的路徑，例如 `/my-panel-2026`，儲存後後台網址就變成：

```text
https://panel.example.com/my-panel-2026
```

直接造訪根網域會回傳 404，也可以設定成跳轉到一個普通網頁。**節點連線和訂閱連結不受影響**，改路徑不會讓節點離線，也不會讓已發出的訂閱失效。

## 3. 登記並安裝節點

### 3.1 在面板裡登記

進入「节点」，填寫後點「新建」：

| 欄位 | 範例 | 說明 |
| --- | --- | --- |
| 名称 | `香港 01` | 支援中文 |
| 客户端连接地址 | `hk.example.com` | 用戶端要連線的位址，不要加 `https://` |
| 国家 | `HK` | 兩位國家代碼 |
| 流量倍率 | `1` | 1 為正常計費，0.5 為半價，0 為不扣流量 |

儲存後會顯示**接入 token**，**只顯示這一次**，請馬上複製。不要把 token 貼到公開的地方；遺失了可以在節點列表點「换 token」重新產生。每台節點使用自己的 token。

### 3.2 在節點伺服器上安裝

`hk.example.com` 要解析到**這台節點伺服器**。在節點伺服器上執行：

```bash
apt-get update && apt-get install -y curl
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash
```

依照提示填寫：

1. **面板網址**：例如 `https://panel.example.com`；
2. **接入 token**：剛才複製的那串，輸入時不會顯示；
3. **節點網域**：使用 AnyTLS、Hysteria2、TUIC 時必須填（需要憑證）；只用 VLESS Reality 或 Shadowsocks 可以直接按 Enter；
4. **憑證電子郵件**（選用）。

回到面板，節點應顯示「在线」。在節點伺服器上檢查：

```bash
systemctl is-active skysbx-node
journalctl -u skysbx-node -n 50 --no-pager
ls -l /opt/skysbx/cert.pem /opt/skysbx/key.pem   # 只有用到憑證時才需要檢查
```

**節點「在线」不代表已經能用**，還需要在面板裡替它新增入站（代理連接埠），請見[使用指南：入站](guide.md#入站)。

## 4. 節點憑證

AnyTLS、Hysteria2、TUIC 需要節點自己的憑證；VLESS Reality 和 Shadowsocks 不需要。

### 預設：HTTP 驗證

安裝時填了節點網域就會自動申請。需要節點的 TCP 80 對外開放且沒被佔用。憑證會自動續期，續期後節點自動重新啟動以載入新憑證，期間連線會中斷一下。

### Cloudflare DNS 驗證（無法使用 TCP 80 時）

網域在 Cloudflare 上時，準備一個**只能編輯這個網域 DNS** 的 API token，然後：

```bash
read -rsp 'Cloudflare DNS API token: ' cf_token; printf '\n'
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- \
  --panel https://panel.example.com --domain hk.example.com \
  --email you@example.com --cf-token "$cf_token"
unset cf_token
```

腳本仍會提示你輸入接入 token。用 DNS 驗證不代表開啟 CDN，節點的解析紀錄仍要設為灰色雲朵。

### 已經有憑證

把完整憑證鏈和私鑰放到預設位置，再略過憑證申請：

```bash
install -d -m 0700 /opt/skysbx
install -m 0644 /你的路徑/fullchain.pem /opt/skysbx/cert.pem
install -m 0600 /你的路徑/privkey.pem /opt/skysbx/key.pem
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash -s -- \
  --panel https://panel.example.com --domain hk.example.com --no-cert
```

憑證的續期和重新載入需要你自己負責。

## 5. 面板和節點裝在同一台伺服器

**選擇這種方式時，用本節取代第 2、3 節。**面板網域（和選用的訂閱網域）都解析到這台伺服器：

```bash
apt-get update && apt-get install -y curl
curl -4 -fL --retry 3 --connect-timeout 15 \
  https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-panel-and-node.sh \
  -o /tmp/veyra-all-install.sh && bash /tmp/veyra-all-install.sh
```

腳本會一步步引導：填面板網域、訂閱網域和電子郵件 → 建立管理員 → 你到面板裡新建節點 → 回到 SSH 貼上接入 token → 完成。

- 節點的「客户端连接地址」和 TLS 入站的 SNI 都填面板網域。節點會直接使用面板的憑證，不再另外申請。
- 面板已經佔用 TCP 80 和 443，代理連接埠要避開，例如 Reality 用 10443、AnyTLS 用 8443。

## 6. 下一步

1. 替節點新增入站：[使用指南：入站](guide.md#入站)
2. 建立使用者、複製訂閱連結給客戶：[使用指南：使用者](guide.md#使用者)
3. 開啟兩步驟驗證，保護後台：[使用指南：設定](guide.md#設定)
