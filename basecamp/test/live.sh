#!/usr/bin/env bash
# Runs the Foray view against a real service on loopback, with a demo list, and
# photographs it: basecamp/test/out/foray-list.png.
set -euo pipefail
cd "$(dirname "$0")/../.."
QML=${QML:-$(command -v qml6 || command -v qml || ls -d /nix/store/*-qtdeclarative-*/bin/qml 2>/dev/null | sort -V | tail -1)}
QMLDIR=$(dirname "$(dirname "$QML")")/lib/qt-6/qml
work=$(mktemp -d); trap 'kill $(jobs -p) 2>/dev/null; rm -rf "$work"' EXIT
port=7398
go build -o "$work/foray" ./cmd/foray
cat > "$work/config.json" <<CFG
{"port": $port, "socket": "/nonexistent", "data": "$work/data",
 "lists": [{"name": "home", "title": "Home", "mesh": "home", "session": "foray-home"}]}
CFG
# A service with no shrooms daemon: loopback only, which is all this needs.
"$work/foray" serve --config "$work/config.json" >"$work/log" 2>&1 &
for _ in $(seq 50); do curl -sf "http://127.0.0.1:$port/v1/lists" >/dev/null && break; sleep 0.1; done
add() { curl -s -X POST "http://127.0.0.1:$port/v1/lists/home/items" -H 'X-Foray-As: phone.home' -d "$1" >/dev/null; }
add '{"title":"Buy winter tires","due":"'"$(date -d '+5 days' +%F)"'","who":"Václav","tags":["car"],"notes":"205/55 R16, the Octavia"}'
add '{"title":"Milk and bread"}'
add '{"title":"Book the tire change","due":"'"$(date -d '+9 days' +%F)"'"}'
add '{"title":"Call the plumber about the bathroom tap","state":"waiting","who":"Jana"}'
add '{"title":"Renew the car insurance","due":"'"$(date -d '-1 days' +%F)"'"}'
id=$(curl -s "http://127.0.0.1:$port/v1/lists/home" | python3 -c "import json,sys;print([i['id'] for i in json.load(sys.stdin)['items'] if i['title']=='Buy winter tires'][0])")
curl -s -X POST "http://127.0.0.1:$port/v1/lists/home/items/$id/comments" -H 'X-Foray-As: foray-home' -d '{"text":"**Two questions:**\n\n- Summer or *all-season*?\n- Should I look for a garage that stores them over winter? See [tyre storage](https://example.com).\n\n![tracker](http://127.0.0.1:9/pixel.png) <b>raw</b>"}' >/dev/null
curl -s -X POST "http://127.0.0.1:$port/v1/lists/home/items/$id/help" -H 'X-Foray-As: foray-home' -d '{"task":"k11/research:1","worker":"k11/research","kind":"compare","state":"completed","ask":"205/55 R16 winter tyres under 120 EUR each","answer":"Three fit the budget: Nokian WR Snowproof (114), Michelin Alpin 6 (119), Continental WinterContact TS 870 (117). Nokian is quietest in tests."}' >/dev/null
curl -s -X PATCH "http://127.0.0.1:$port/v1/lists/home/items/$id" -H 'X-Foray-As: foray-home' -d '{"summary":"**Nokian WR Snowproof**, 205/55 R16 — the quietest of three under 3 000 Kč each.\n\n- **Buy**: [pneu-shop.cz](https://example.com/nokian) 2 850 Kč\n- **Fitting**: [Pneuservis Slavkov](https://example.com/servis), ~1 200 Kč\n\nSources: [Auto Bild test 2026](https://example.com/test)"}' >/dev/null
pid=$(curl -s "http://127.0.0.1:$port/v1/lists/home" | python3 -c "import json,sys;print([i['id'] for i in json.load(sys.stdin)['items'] if i['title'].startswith('Call the plumber')][0])")
curl -s -X POST "http://127.0.0.1:$port/v1/lists/home/items/$pid/comments" -H 'X-Foray-As: foray-home' -d '{"text":"Which day suits you, Tuesday or Thursday?"}' >/dev/null
mkdir -p basecamp/test/out
cp basecamp/view/Main.qml basecamp/test/LiveHarness.qml "$work/"
QT_QPA_PLATFORM=offscreen QT_ASSUME_STDERR_HAS_CONSOLE=1 QML_XHR_ALLOW_FILE_READ=1 QML_IMPORT_PATH="$QMLDIR" QML2_IMPORT_PATH="$QMLDIR" \
  timeout 60 "$QML" -I "$work" "$work/LiveHarness.qml" "$port" "$PWD/basecamp/test/out" 2>&1 | grep -E '^qml: |Error' || true
