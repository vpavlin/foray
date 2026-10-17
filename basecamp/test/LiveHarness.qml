import QtQuick

// The Foray view against a real `foray serve` on loopback: foray_core's calls
// are answered by HTTP to 127.0.0.1, shrooms_core's status by a stand-in.
// Run by basecamp/test/live.sh, which starts the service.
Item {
    id: top
    width: 1400; height: 860
    property string base: "http://127.0.0.1:" + Qt.application.arguments[Qt.application.arguments.length - 2]
    readonly property var demoQr: ["1111111010111111", "1000001001100001", "1011101110101101", "1011101001001101", "1000001011000001", "1111111010111111"]
    property string outDir: Qt.application.arguments[Qt.application.arguments.length - 1]

    property var keys: []
    function http(method, path, body, key) {
        var x = new XMLHttpRequest()
        x.open(method, base + path, false)
        if (body) x.setRequestHeader("Content-Type", "application/json")
        if (key) x.setRequestHeader("Idempotency-Key", key)
        x.send(body || null)
        return x.responseText || "{}"
    }
    // The core's prefs, kept here; and the list's machine not answering.
    property var prefs: ({})
    property bool down: false
    readonly property string refused: JSON.stringify({ error: "cannot reach fd00::1: Connection refused" })
    QtObject {
        id: fake
        property var callModule: function (module, method, args) {
            if (module === "shrooms_core" && method === "status")
                return JSON.stringify({ name: "laptop", meshes: [{ label: "home", overlay: "fd00::1" }], peers: [] })
            if (module !== "foray_core") return ""
            if (method === "find" || method === "found")
                return JSON.stringify([{ address: "fd00::1", peer: "laptop", mesh: "home", reply: JSON.parse(top.http("GET", "/v1/lists")) }])
            if (method === "get") return top.down ? top.refused : top.http("GET", args[1])
            if (method === "send") return top.down ? top.refused : top.http(args[1], args[2], args[3])
            if (method === "sendOnce") { top.keys.push(args[4]); return top.down ? top.refused : top.http(args[1], args[2], args[3], args[4]) }
            if (method === "getPref") return top.prefs[args[0]] || ""
            if (method === "setPref") { top.prefs[args[0]] = args[1]; return JSON.stringify({ ok: true }) }
            return JSON.stringify({ ok: true })
        }
    }
    Main { id: view; anchors.fill: parent; bridge: fake }

    function count(title) { return JSON.parse(top.http("GET", "/v1/lists/home")).items.filter(function (i) { return i.title === title }).length }
    // Kept, caught up, and written while the list's machine does not answer
    // (design §7.1).
    function offline() {
        var key = view.current.key
        var kept = JSON.parse(top.prefs.kept || "{}")[key]
        console.error("KEPT items=" + (kept ? kept.items.length === view.items.length : false) + " seq=" + (kept ? kept.seq === view.seq : false)
                      + " sort=" + view.pref("sort") + " lists=" + JSON.parse(top.prefs.lists || "[]").length)
        // Another device adds one: caught up from the changes, not read whole.
        var before = view.items.length
        top.http("POST", "/v1/lists/home/items", JSON.stringify({ title: "Eggs" }))
        view.pollChanges()
        console.error("CAUGHTUP=" + (view.items.length === before + 1) + " " + (view.items.filter(function (i) { return i.title === "Eggs" }).length === 1))
        // Its machine goes away: the list stays, a write waits.
        top.down = true
        view.openList(view.current)
        var shown = view.items.length
        var r = view.write("POST", "/items", { title: "Offline milk" })
        console.error("OUTBOX shown=" + (shown === before + 1) + " waits=" + (r && r.waiting) + " n=" + view.waitingHere()
                      + " kept=" + JSON.parse(top.prefs.outbox || "[]").length + " service=" + top.count("Offline milk") + " said=" + JSON.stringify(view.said))
        // A refusal is not kept: only no answer is.
        top.down = false
        var bad = view.write("PATCH", "/items/nonexistent", { state: "done" })
        console.error("REFUSED kept=" + view.outbox.length + " said=" + (view.said !== ""))
        // It answers again: sent, oldest first, and the list read.
        view.pollChanges()
        // The resend carried the key the first try had; sent once more with
        // it (its answer lost), the service applies it once.
        var k = top.keys.filter(function (x, i) { return top.keys.indexOf(x) !== i })
        top.http("POST", "/v1/lists/home/items", JSON.stringify({ title: "Offline milk" }), k[0])
        console.error("ONCE resentWithSameKey=" + (k.length > 0) + " service=" + top.count("Offline milk"))
        console.error("SENT service=" + top.count("Offline milk") + " left=" + view.outbox.length + " shown=" + view.items.filter(function (i) { return i.title === "Offline milk" }).length)
        Qt.quit()
    }

    function shot(name, next) {
        top.grabToImage(function (r) { r.saveToFile(top.outDir + "/" + name); console.error("SHOT " + name); next() })
    }
    Timer { id: settle; interval: 700; property var next: null; onTriggered: if (next) next() }
    Timer {
        interval: 2500; running: true
        onTriggered: {
            console.error("ROWS0=" + view.rows.map(function (r) { return r.header ? "[" + r.label + "]" : r.item.title }).join(" | "))
            console.error("LISTS=" + view.lists.length + " CURRENT=" + (view.current ? view.current.name : "-") + " ITEMS=" + view.items.length)
            var tires = view.items.filter(function (i) { return i.title === "Buy winter tires" })[0]
            view.selectedId = tires ? tires.id : ""
            settle.next = function () { top.shot("foray-list.png", function () {
                // Tick one done, through the view, and check the service took it.
                var milk = view.items.filter(function (i) { return i.title === "Milk and bread" })[0]
                view.write("PATCH", "/items/" + milk.id, { state: "done" })
                console.error("DONE=" + JSON.parse(top.http("GET", "/v1/lists/home")).items.filter(function (i) { return i.state === "done" }).length)
                view.pairing = { link: "foray://add?address=fd7b::1&list=home", qr: JSON.parse(top.http("GET", "/v1/lists/home")).items.length ? top.demoQr : [] }
                settle.next = function () { top.shot("foray-pair.png", function () {
                    // About us: saved through the view, read back from the service.
                    view.pairing = null
                    view.write("PUT", "/context", { place: "Prague, Czechia", language: "English", currency: "CZK",
                                                    notes: "Car: 2019 Octavia, tyres 205/55 R16.\nWe shop at Albert and Rohlik.",
                                                    helpers: view.parseHelpers("duet/duet — research\njimmy-crib/vpavlin: Logos apps, Scala\nnot a helper") })
                    var c = JSON.parse(top.http("GET", "/v1/lists/home/context"))
                    console.error("CONTEXT=" + c.place + "|" + c.currency + "|" + (c.notes.indexOf("Octavia") >= 0) + "|" + JSON.stringify(c.helpers))
                    view.editingContext = true
                    settle.next = function () { top.shot("foray-about.png", function () {
                        view.editingContext = false
                        view.setView("title", "tag")
                        console.error("ROWS=" + view.rows.map(function (r) { return r.header ? "[" + r.label + "]" : r.item.title }).join(" | "))
                        settle.next = function () { top.shot("foray-sorted.png", function () { view.setView("due", "state"); top.offline() }) }
                        settle.restart()
                    }) }
                    settle.restart()
                }) }
                settle.restart()
            }) }
            settle.restart()
        }
    }
}
