[简体中文](README.md) | **繁體中文** | [English](README.en.md)

# Veyra

自架的代理管理面板：用一個網頁後台管理多台代理伺服器（節點）、客戶帳號、方案和訂閱連結。適合自己使用，也適合小規模地替別人開帳號。

[![CI](https://github.com/Zayvian/veyra-panel/actions/workflows/ci.yml/badge.svg)](https://github.com/Zayvian/veyra-panel/actions/workflows/ci.yml)

> 面板介面目前只有簡體中文。

## 功能

- **協定**：VLESS Reality、Hysteria2（混淆、偽裝網站、跳連接埠）、AnyTLS、Shadowsocks 2022、TUIC v5
- **使用者**：到期時間、流量上限、每月自動重設、同時線上 IP 限制、依入站分配權限、一鍵續費、方案範本
- **訂閱**：Clash / mihomo、sing-box、Shadowrocket、通用分享連結，自動辨識用戶端；QR Code 和可直接傳給客戶的說明文字；訂閱公告；帳號失效時顯示原因
- **節點**：流量倍率（例如 0.5 倍、不扣流量）、站內中轉、每台節點的流量統計
- **不斷線**：增刪使用者、續費、改密碼都是熱更新，不會中斷其他人的連線；升級面板也不影響客戶
- **安全**：管理後台可隱藏路徑、兩步驟驗證、單一使用者重設連結、緊急時全員更換密碼
- **安裝簡單**：一行指令安裝，直接下載編譯好的程式（amd64 / arm64），伺服器上不編譯

## 快速開始

需要 Debian / Ubuntu 伺服器、root 權限，以及解析到伺服器的網域。

**面板**（在面板伺服器上）：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash
```

**節點**（在每台代理伺服器上，先在面板「节点」頁新建節點、複製接入 token）：

```bash
curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash
```

**升級**：同樣的指令後面加上 `-s -- --upgrade`。

## 文件

| 文件 | 內容 |
| --- | --- |
| [安裝](docs/zh-TW/install.md) | 準備網域和連接埠、安裝面板和節點、憑證、面板和節點同機 |
| [使用指南](docs/zh-TW/guide.md) | 每個頁面、每項功能怎麼用：使用者、套餐、節點、入站、限制、設定、訂閱 |
| [維護](docs/zh-TW/maintenance.md) | 升級、備份還原、忘記密碼、解除安裝、排查問題 |
| [通訊協定](protocol/PROTOCOL.md) | 面板和節點之間的訊息格式（開發者，簡體中文） |

## 倉庫結構

| 目錄 | 內容 | 授權 |
| --- | --- | --- |
| [`panel/`](panel/) | 管理面板 | AGPL-3.0 |
| [`node/`](node/) | 節點程式，內建 sing-box | GPL-3.0 |
| [`protocol/`](protocol/) | 面板和節點之間的協定說明和範例 | — |
| [`deploy/`](deploy/) | 共用的 `veyra` 管理選單和下載校驗腳本 | — |

節點內建的協定核心是 [veyra-core](https://github.com/Zayvian/veyra-core)：加了「改使用者不用重新啟動」等修補的 sing-box。它是獨立的倉庫，由 [`node/core.lock`](node/core.lock) 鎖定版本。

**發布**：每次合併到 `main` 並通過測試後，CI 會依 `core.lock` 編譯面板和節點的 linux amd64 / arm64 版本，發布為新的 [Release](https://github.com/Zayvian/veyra-panel/releases)，附帶 `sha256sums.txt`。安裝腳本下載後會先校驗再安裝。

## 授權

`panel/` 使用 AGPL-3.0，`node/` 使用 GPL-3.0，各自的 `LICENSE` 和 `NOTICE` 在各自的目錄裡。面板不包含任何 sing-box 程式碼（CI 會檢查），兩者只透過網路訊息通訊。

本專案基於 [kosje/skysbx-panel](https://github.com/kosje/skysbx-panel) 和 [kosje/skysbx-node](https://github.com/kosje/skysbx-node) 開發，保留原作者署名，不代表原作者參與或認可本版本。與 sing-box 專案無關，請不要把本專案的問題回報給 sing-box。

問題和建議請提交到 [Issues](https://github.com/Zayvian/veyra-panel/issues)。
