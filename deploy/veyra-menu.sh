#!/usr/bin/env bash
# Veyra's small on-host management menu. Installed as /usr/local/bin/veyra.
set -u

OWNER="Zayvian"
PANEL_INSTALL="https://raw.githubusercontent.com/${OWNER}/veyra-panel/main/install.sh"
NODE_INSTALL="https://raw.githubusercontent.com/${OWNER}/veyra-node/main/install.sh"

if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        exec sudo "$0" "$@"
    fi
    printf '请使用 root 运行，或执行：sudo veyra\n' >&2
    exit 1
fi

panel_root() {
    local root
    root=$(systemctl show skysbx-panel -p WorkingDirectory --value 2>/dev/null || true)
    printf '%s\n' "${root:-/opt/skysbx}"
}

node_root() {
    local root
    root=$(systemctl show skysbx-node -p WorkingDirectory --value 2>/dev/null || true)
    printf '%s\n' "${root:-/opt/skysbx}"
}

has_panel() {
    local root
    root=$(panel_root)
    [ -x "$root/skysbx-panel" ] || [ -f /etc/systemd/system/skysbx-panel.service ]
}

has_node() {
    local root
    root=$(node_root)
    [ -x "$root/skysbx-node" ] || [ -f /etc/systemd/system/skysbx-node.service ]
}

pause() {
    printf '\n按 Enter 返回菜单...'
    read -r _ || true
}

status_panel() {
    local root domain sub
    root=$(panel_root)
    domain=$(sed -n 's/^SKYSBX_DOMAIN=//p' "$root/panel.env" 2>/dev/null | head -1)
    sub=$(sed -n 's/^SKYSBX_SUB_DOMAIN=//p' "$root/panel.env" 2>/dev/null | head -1)
    printf '\nPanel 状态：%s\n' "$(systemctl is-active skysbx-panel 2>/dev/null || true)"
    printf '目录：%s\n' "$root"
    [ -n "$domain" ] && printf '面板：https://%s/login\n' "$domain"
    [ -n "$sub" ] && printf '订阅域名：https://%s\n' "$sub"
}

status_node() {
    local root panel
    root=$(node_root)
    panel=$(sed -n 's/^SKYSBX_PANEL=//p' "$root/node.env" 2>/dev/null | head -1)
    printf '\nNode 状态：%s\n' "$(systemctl is-active skysbx-node 2>/dev/null || true)"
    printf '目录：%s\n' "$root"
    [ -n "$panel" ] && printf '连接面板：%s\n' "$panel"
}

run_installer() {
    local kind=$1 root url file
    shift
    case "$kind" in
        panel)
            root=$(panel_root); url=$PANEL_INSTALL; file=/root/veyra-panel-install.sh ;;
        node)
            root=$(node_root); url=$NODE_INSTALL; file=/root/veyra-node-install.sh ;;
        *) return 1 ;;
    esac
    rm -f "$file"
    if ! curl -4 -fL --retry 3 --connect-timeout 15 "$url" -o "$file"; then
        printf '\n下载安装器失败，请检查网络或 GitHub 连通性。\n' >&2
        return 1
    fi
    VEYRA_ROOT="$root" bash "$file" "$@"
}

confirm() {
    local words=$1 answer
    printf '输入 %s 以确认：' "$words"
    read -r answer || return 1
    [ "$answer" = "$words" ]
}

panel_menu() {
    local choice
    while :; do
        clear 2>/dev/null || true
        printf 'Veyra Panel 管理\n\n'
        status_panel
        cat <<'EOF'

1) 查看状态
2) 升级 Panel（保留全部数据）
3) 重启 Panel
4) 查看实时日志（Ctrl+C 返回）
5) 卸载 Panel 程序，保留数据和证书
6) 彻底删除 Panel、用户、订阅和证书
0) 返回
EOF
        printf '请选择：'
        read -r choice || return
        case "$choice" in
            1) status_panel; pause ;;
            2) run_installer panel --upgrade; pause ;;
            3) systemctl restart skysbx-panel && printf 'Panel 已重启。\n'; pause ;;
            4) journalctl -u skysbx-panel -f --no-pager ;;
            5) confirm 'UNINSTALL PANEL' && run_installer panel --uninstall || printf '已取消。\n'; pause ;;
            6) confirm 'PURGE PANEL' && run_installer panel --purge || printf '已取消。\n'; pause ;;
            0) return ;;
            *) printf '无效选项。\n'; pause ;;
        esac
    done
}

node_menu() {
    local choice
    while :; do
        clear 2>/dev/null || true
        printf 'Veyra Node 管理\n\n'
        status_node
        cat <<'EOF'

1) 查看状态
2) 升级 Node 与内核（保留 token 和证书）
3) 重启 Node
4) 查看实时日志（Ctrl+C 返回）
5) 卸载 Node 程序，保留 token 和证书
6) 彻底删除 Node、token 和证书
0) 返回
EOF
        printf '请选择：'
        read -r choice || return
        case "$choice" in
            1) status_node; pause ;;
            2) run_installer node --upgrade; pause ;;
            3) systemctl restart skysbx-node && printf 'Node 已重启。\n'; pause ;;
            4) journalctl -u skysbx-node -f --no-pager ;;
            5) confirm 'UNINSTALL NODE' && run_installer node --uninstall || printf '已取消。\n'; pause ;;
            6) confirm 'PURGE NODE' && run_installer node --purge || printf '已取消。\n'; pause ;;
            0) return ;;
            *) printf '无效选项。\n'; pause ;;
        esac
    done
}

upgrade_all() {
    has_panel && run_installer panel --upgrade
    has_node && run_installer node --upgrade
}

usage() {
    cat <<'EOF'
用法：veyra [panel|node|status|update]
不带参数时显示交互式管理菜单。
EOF
}

case "${1:-}" in
    panel) panel_menu; exit 0 ;;
    node) node_menu; exit 0 ;;
    status) has_panel && status_panel; has_node && status_node; exit 0 ;;
    update) upgrade_all; exit $? ;;
    -h|--help) usage; exit 0 ;;
    '') ;;
    *) usage; exit 1 ;;
esac

while :; do
    clear 2>/dev/null || true
    printf 'Veyra 管理菜单\n\n'
    has_panel && status_panel
    has_node && status_node
    cat <<'EOF'

1) 管理 Panel
2) 管理 Node
3) 升级本机所有已安装组件
0) 退出
EOF
    printf '请选择：'
    read -r choice || exit 0
    case "$choice" in
        1) has_panel && panel_menu || { printf '未检测到 Panel。\n'; pause; } ;;
        2) has_node && node_menu || { printf '未检测到 Node。\n'; pause; } ;;
        3) upgrade_all; pause ;;
        0) exit 0 ;;
        *) printf '无效选项。\n'; pause ;;
    esac
done
