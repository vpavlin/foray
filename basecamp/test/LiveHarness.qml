import QtQuick

// The Foray view against a real `foray serve` on loopback: foray_core's calls
// are answered by HTTP to 127.0.0.1, shrooms_core's status by a stand-in.
// Run by basecamp/test/live.sh, which starts the service.
Item {
    id: top
    width: 1400; height: 860
    property string base: "http://127.0.0.1:" + Qt.application.arguments[Qt.application.arguments.length - 2]
    property string outDir: Qt.application.arguments[Qt.application.arguments.length - 1]

    function http(method, path, body) {
        var x = new XMLHttpRequest()
        x.open(method, base + path, false)
        if (body) x.setRequestHeader("Content-Type", "application/json")
        x.send(body || null)
        return x.responseText || "{}"
    }
    QtObject {
        id: fake
        property var callModule: function (module, method, args) {
            if (module === "shrooms_core" && method === "status")
                return JSON.stringify({ name: "laptop", meshes: [{ label: "home", overlay: "fd00::1" }], peers: [] })
            if (module !== "foray_core") return ""
            if (method === "find" || method === "found")
                return JSON.stringify([{ address: "fd00::1", peer: "laptop", mesh: "home", reply: JSON.parse(top.http("GET", "/v1/lists")) }])
            if (method === "get") return top.http("GET", args[1])
            if (method === "send") return top.http(args[1], args[2], args[3])
            if (method === "getPref") return ""
            return JSON.stringify({ ok: true })
        }
    }
    Main { id: view; anchors.fill: parent; bridge: fake }

    function shot(name, next) {
        top.grabToImage(function (r) { r.saveToFile(top.outDir + "/" + name); console.error("SHOT " + name); next() })
    }
    Timer { id: settle; interval: 700; property var next: null; onTriggered: if (next) next() }
    Timer {
        interval: 2500; running: true
        onTriggered: {
            console.error("LISTS=" + view.lists.length + " CURRENT=" + (view.current ? view.current.name : "-") + " ITEMS=" + view.items.length)
            var tires = view.items.filter(function (i) { return i.title === "Buy winter tires" })[0]
            view.selectedId = tires ? tires.id : ""
            settle.next = function () { top.shot("foray-list.png", function () {
                // Tick one done, through the view, and check the service took it.
                var milk = view.items.filter(function (i) { return i.title === "Milk and bread" })[0]
                view.write("PATCH", "/items/" + milk.id, { state: "done" })
                console.error("DONE=" + JSON.parse(top.http("GET", "/v1/lists/home")).items.filter(function (i) { return i.state === "done" }).length)
                Qt.quit()
            }) }
            settle.restart()
        }
    }
}
