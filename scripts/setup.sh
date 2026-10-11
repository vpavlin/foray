#!/usr/bin/env bash
# Sets up a Foray list and its session on the machine that keeps it, as the
# user whose shrooms-agent runs the session. Safe to run again.
#
#   scripts/setup.sh --list home --mesh home --title Home \
#       [--model venice/deepseek-v4-1-flash] [--suggest-from laptop/shrooms] [--no-session]
#
# What it does:
#   ~/.local/bin/foray                      the binary (built here if Go is present)
#   ~/.config/foray/config.json             the list, added or updated
#   ~/foray-<list>/                         the session's directory: AGENTS.md, .pi/settings.json,
#                                           bin/foray (for a cage), and the list's socket
#   ~/.pi/agent/mcp.json                    a "foray" entry (only the list's session may use it)
#   ~/.config/systemd/user/foray.service    the service, enabled and (re)started
#   the session foray-<list>, created on this machine's shrooms-agent, limited to the list's mesh
set -euo pipefail
cd "$(dirname "$0")/.."

list="" mesh="" title="" model="venice/deepseek-v4-1-flash" suggest="" session=1
while [ $# -gt 0 ]; do
    case "$1" in
        --list) list=$2; shift 2 ;;
        --mesh) mesh=$2; shift 2 ;;
        --title) title=$2; shift 2 ;;
        --model) model=$2; shift 2 ;;
        --suggest-from) suggest=$2; shift 2 ;;
        --no-session) session=0; shift ;;
        *) echo "unknown: $1" >&2; exit 2 ;;
    esac
done
[ -n "$list" ] && [ -n "$mesh" ] || { echo "usage: $0 --list NAME --mesh LABEL [--title T]" >&2; exit 2; }
title=${title:-$list}
name="foray-$list"
dir="$HOME/$name"
cfg="$HOME/.config/foray/config.json"

echo "==> the binary"
mkdir -p "$HOME/.local/bin" "$dir/bin" "$dir/.pi"
if command -v go >/dev/null; then
    go build -o "$HOME/.local/bin/foray" ./cmd/foray
fi
[ -x "$HOME/.local/bin/foray" ] || { echo "no foray binary: install Go, or put one in ~/.local/bin" >&2; exit 1; }
cp "$HOME/.local/bin/foray" "$dir/bin/foray"

echo "==> the list ($list on mesh $mesh)"
mkdir -p "$(dirname "$cfg")"
python3 - "$cfg" "$list" "$mesh" "$title" "$name" "$dir/.foray.sock" "$suggest" "$session" <<'PY'
import json, os, sys
path, lst, mesh, title, name, sock, suggest, session = sys.argv[1:]
c = json.load(open(path)) if os.path.exists(path) else {}
lists = [l for l in c.get("lists", []) if l["name"] != lst]
old = next((l for l in c.get("lists", []) if l["name"] == lst), {})
allow = sorted(set(old.get("suggest_from", []) + ([s for s in suggest.split(",") if s] if suggest else [])))
entry = {"name": lst, "title": title, "mesh": mesh, "socket": sock, "suggest_from": allow}
if session == "1":
    entry["session"] = name
lists.append(entry)
c["lists"] = lists
tmp = path + ".tmp"
json.dump(c, open(tmp, "w"), indent=2)
os.replace(tmp, path)
PY

echo "==> the session's directory"
cp session/AGENTS.md "$dir/AGENTS.md"
provider=${model%%/*}; mid=${model#*/}
python3 - "$dir/.pi/settings.json" "$provider" "$mid" <<'PY'
import json, sys
path, provider, model = sys.argv[1:]
json.dump({"defaultProvider": provider, "defaultModel": model}, open(path, "w"), indent=2)
PY
if ! python3 -c "import json,os,sys; m=json.load(open(os.path.expanduser('~/.pi/agent/models.json'))); sys.exit(0 if '$provider' in (m.get('providers') or m) else 1)" 2>/dev/null; then
    echo "   NOTE: pi has no '$provider' provider in ~/.pi/agent/models.json — add it (with its key) before the session can think"
fi

echo "==> the session's tools (pi)"
python3 - "$HOME/.pi/agent/mcp.json" "$dir/bin/foray" "$list" "$dir/.foray.sock" <<'PY'
import json, os, sys
path, binp, lst, sock = sys.argv[1:]
c = json.load(open(path)) if os.path.exists(path) else {"mcpServers": {}}
c.setdefault("mcpServers", {})["foray-" + lst] = {
    "command": binp, "args": ["mcp", "--list", lst, "--socket", sock],
    "description": "The household's Foray list: only its own session may use these tools."}
os.makedirs(os.path.dirname(path), exist_ok=True)
json.dump(c, open(path, "w"), indent=2)
PY

echo "==> the service"
mkdir -p "$HOME/.config/systemd/user"
cat > "$HOME/.config/systemd/user/foray.service" <<UNIT
[Unit]
Description=Foray: the household's lists on the mesh
After=network-online.target

[Service]
ExecStart=%h/.local/bin/foray serve
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
UNIT
systemctl --user daemon-reload
systemctl --user enable --now foray.service >/dev/null
systemctl --user restart foray.service
sleep 1
curl -sf "http://127.0.0.1:7389/v1/lists" >/dev/null && echo "   serving" || { echo "   not answering: journalctl --user -u foray" >&2; exit 1; }

if [ "$session" = 1 ]; then
    echo "==> the session $name (pi, limited to mesh $mesh)"
    addr=$(curl -s --unix-socket /run/shrooms/shrooms.sock http://x/status |
        python3 -c "import json,sys; print([m['overlay'] for m in json.load(sys.stdin)['meshes'] if m['label']=='$mesh'][0])" 2>/dev/null || true)
    if [ -z "$addr" ]; then
        echo "   this machine is not on mesh $mesh yet: the session is created once it is (run this again)"
    else
        agent="http://[$addr]:7387"
        if curl -sf "$agent/v1/sessions" | python3 -c "import json,sys; sys.exit(0 if any(s['name']=='$name' for s in json.load(sys.stdin)) else 1)"; then
            echo "   exists"
        else
            curl -sf -X POST "$agent/v1/sessions" -H 'Content-Type: application/json' \
                -d "{\"name\":\"$name\",\"dir\":\"$dir\",\"harness\":\"pi\",\"keep_running\":true}" >/dev/null && echo "   created"
        fi
        curl -sf -X PATCH "$agent/v1/sessions/$name" -H 'Content-Type: application/json' \
            -d "{\"meshes\":[\"$mesh\"]}" >/dev/null && echo "   limited to $mesh" || echo "   NOTE: could not limit it to $mesh — do it in Shrooms Agents (meshes)"
    fi
fi
echo "done."
