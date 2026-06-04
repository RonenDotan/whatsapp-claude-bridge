#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MCP_DIR="$SCRIPT_DIR/../whatsapp-mcp-server"

component="${1:-all}"

usage() {
    cat <<'EOF'
Usage: start.sh [component]

  start.sh             Restart all components
  start.sh signal      Restart signal-cli only
  start.sh whatsapp    Restart whatsapp-mcp-server only
  start.sh bridge      Restart Go bridge only
  start.sh --help      Show this help

Components:
  signal    signal-cli daemon  (TCP 0.0.0.0:7583)
  whatsapp  whatsapp-mcp-server (python main.py)
  bridge    whatsapp-bridge
EOF
}

if [[ "$component" == "help" || "$component" == "--help" || "$component" == "-h" ]]; then
    usage
    exit 0
fi

find_signal_cli() {
    local candidate
    local extracted_dir

    if candidate="$(command -v signal-cli 2>/dev/null)"; then
        echo "$candidate"
        return 0
    fi

    if extracted_dir="$(ls -td "${TMPDIR:-/tmp}"/signal-cli-*-extracted 2>/dev/null | head -1)" &&
        [[ -x "$extracted_dir/bin/signal-cli" ]]; then
        echo "$extracted_dir/bin/signal-cli"
        return 0
    fi

    for candidate in \
        "$HOME/.local/bin/signal-cli" \
        /usr/local/bin/signal-cli \
        /opt/homebrew/bin/signal-cli \
        "${TMPDIR:-/tmp}"/signal-cli
    do
        if [[ -x "$candidate" ]]; then
            echo "$candidate"
            return 0
        fi
    done

    return 1
}

kill_by_pattern() {
    pkill -f "$1" 2>/dev/null || true
}

if [[ "$component" == "signal" || "$component" == "all" ]]; then
    SIGNAL_CLI="$(find_signal_cli)" || { echo "[ERROR] Cannot find signal-cli. Install it or add to PATH."; exit 1; }
fi

restart_signal_cli() {
    local SIGNAL_CLI_PATH="$SIGNAL_CLI"

    kill_by_pattern signal-cli
    sleep 1
    nohup "$SIGNAL_CLI_PATH" daemon --tcp 0.0.0.0:7583 >/dev/null 2>&1 &
    echo "[OK] signal-cli started (PID $!)"
}

restart_whatsapp_mcp() {
    kill_by_pattern main.py
    sleep 1
    (
        cd "$MCP_DIR"
        nohup .venv/bin/python main.py >/dev/null 2>&1 &
        echo "[OK] whatsapp-mcp started (PID $!)"
    )
}

restart_bridge() {
    kill_by_pattern whatsapp-bridge
    sleep 1

    if command -v curl >/dev/null 2>&1; then
        latest_whatsmeow="$(
            curl -fsSL https://proxy.golang.org/go.mau.fi/whatsmeow/@latest |
                python3 -c 'import json, sys; print(json.load(sys.stdin)["Version"])'
        )"
        current_whatsmeow="$(grep 'go.mau.fi/whatsmeow' "$SCRIPT_DIR/go.mod" | awk '{print $2}')"

        if [[ "$latest_whatsmeow" != "$current_whatsmeow" ]]; then
            (cd "$SCRIPT_DIR" && go get go.mau.fi/whatsmeow@latest && go build -o whatsapp-bridge .)
        fi
    else
        echo "[WARN] curl not found; skipping whatsmeow update check"
    fi

    if [[ ! -x "$SCRIPT_DIR/whatsapp-bridge" ]]; then
        (cd "$SCRIPT_DIR" && go build -o whatsapp-bridge .)
    fi

    nohup "$SCRIPT_DIR/whatsapp-bridge" >> "$SCRIPT_DIR/bridge.log" 2>> "$SCRIPT_DIR/bridge.err" &
    echo "[OK] bridge started (PID $!)"
}

case "$component" in
  signal)   restart_signal_cli ;;
  whatsapp) restart_whatsapp_mcp ;;
  bridge)   restart_bridge ;;
  all)      restart_signal_cli; restart_whatsapp_mcp; restart_bridge ;;
  *)        echo "Unknown component: $component"; echo "Run: ./start.sh --help"; exit 1 ;;
esac
