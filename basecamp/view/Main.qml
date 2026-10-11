import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// Foray in Basecamp: the household's lists, from the machines of the meshes
// this device is on.
//
// Written for the people who use the list, not for whoever runs the mesh: no
// addresses or mesh words in the main flow. The lists are found by asking the
// peers shrooms_core knows about (foray_core does the asking, since a view may
// not open a connection), and kept current by polling each list's changes.
Item {
    id: root
    anchors.fill: parent

    // --- palette: the Shrooms one, warmer for a household -----------------------
    readonly property color cVoid: "#0b0d0f"
    readonly property color cPanel: "#14181c"
    readonly property color cRaised: "#1b2026"
    readonly property color cLine: "#262d35"
    readonly property color cAsh: "#7d8791"
    readonly property color cBone: "#e2e7eb"
    readonly property color cPhosphor: "#35f0a0"
    readonly property color cAmber: "#f0b429"
    readonly property color cRust: "#e05252"
    readonly property color cViolet: "#9a7bff"
    readonly property color cSky: "#5aa9ff"

    property var bridge: typeof logos !== "undefined" ? logos : null
    readonly property bool haveCore: !!bridge && !!bridge.callModule

    function call(module, method, args) {
        if (!haveCore) return null
        var t = ""
        try { t = String(bridge.callModule(module, method, args || []) || "").trim() } catch (e) { return null }
        for (var i = 0; i < 2 && t.charAt(0) === '"'; i++) {
            try { t = String(JSON.parse(t)).trim() } catch (e2) { break }
        }
        if (t.charAt(0) !== "{" && t.charAt(0) !== "[") return null
        try { return JSON.parse(t) } catch (e3) { return null }
    }

    // --- finding the lists --------------------------------------------------------
    //
    // Every list on every machine that answered, as {key, address, machine, mesh,
    // name, title, session, counts}. A list is named by its machine and name
    // together, so two machines may each keep a "home".
    property var lists: []
    property string problem: ""

    function peersArg() {
        var st = call("shrooms_core", "status", [])
        if (!st || st.error) {
            root.problem = st && st.error ? "Shrooms is not running here: " + st.error
                                          : "Foray needs the Shrooms app installed in Basecamp."
            return ""
        }
        root.problem = ""
        var out = []
        // This machine may keep lists too.
        var ms = st.meshes || []
        for (var i = 0; i < ms.length; i++)
            if (ms[i].overlay && ms[i].disabled !== true) out.push((st.name || "this machine") + "|" + ms[i].label + "|" + ms[i].overlay)
        var ps = st.peers || []
        for (var j = 0; j < ps.length; j++)
            if (ps[j].online !== false && ps[j].overlay) out.push(ps[j].name + "|" + (ps[j].mesh || "") + "|" + ps[j].overlay)
        return out.join(";")
    }

    function readFound(found) {
        var out = []
        for (var i = 0; i < (found || []).length; i++) {
            var f = found[i], ls = (f.reply && f.reply.lists) || []
            for (var j = 0; j < ls.length; j++)
                out.push({ key: f.address + "/" + ls[j].name, address: f.address, machine: f.reply.machine || f.peer,
                           mesh: ls[j].mesh || f.mesh, name: ls[j].name, title: ls[j].title || ls[j].name,
                           session: ls[j].session || "", counts: ls[j].counts || {} })
        }
        out.sort(function (a, b) { return a.mesh === b.mesh ? (a.title < b.title ? -1 : 1) : (a.mesh < b.mesh ? -1 : 1) })
        return out
    }

    function findLists() {
        var arg = peersArg()
        if (arg === "") return
        var f = call("foray_core", "find", [arg])
        if (f === null) { root.problem = "Foray's core module is missing: install foray_core and restart Basecamp."; return }
        root.lists = readFound(f)
    }
    function pollFound() {
        var f = call("foray_core", "found", [])
        if (f !== null) {
            root.lists = readFound(f)
            if (current === null && lists.length > 0) {
                var want = String(call("foray_core", "getPref", ["last_list"]) || "")
                var pick = lists[0]
                for (var i = 0; i < lists.length; i++) if (lists[i].key === want) pick = lists[i]
                openList(pick)
            }
        }
    }

    // --- the open list --------------------------------------------------------------
    property var current: null      // an entry of lists
    property var items: []
    property var asks: []
    property int seq: 0
    property string selectedId: ""
    property string said: ""
    // The list's pairing link and its QR, while it is shown.
    property var pairing: null
    // What the household tells its assistant about itself (GET .../context).
    property var context: ({})
    property bool editingContext: false

    function listPath(sub) { return "/v1/lists/" + encodeURIComponent(current.name) + (sub || "") }

    function openList(l) {
        root.current = l
        root.selectedId = ""
        root.items = []
        root.seq = 0
        call("foray_core", "setPref", ["last_list", l.key])
        reloadList()
    }
    function reloadList() {
        if (current === null) return
        var d = call("foray_core", "get", [current.address, listPath("")])
        if (!d || d.error) { root.said = d ? d.error : "no answer"; return }
        root.items = d.items || []
        root.asks = d.asks || []
        root.context = d.context || {}
        root.seq = d.list ? d.list.seq : 0
    }
    function pollChanges() {
        if (current === null) return
        var d = call("foray_core", "get", [current.address, listPath("/changes?since=" + root.seq)])
        if (d && !d.error && d.seq !== root.seq) reloadList()
    }
    function write(method, sub, body) {
        var d = call("foray_core", "send", [current.address, method, listPath(sub), JSON.stringify(body)])
        if (!d || d.error) { root.said = d ? d.error : "no answer"; return null }
        root.said = ""
        reloadList()
        return d
    }

    function itemById(id) {
        for (var i = 0; i < items.length; i++) if (items[i].id === id) return items[i]
        return null
    }
    readonly property var selected: selectedId === "" ? null : itemById(selectedId)

    // What shows, in order: what to do (soonest due first), waiting, the inbox,
    // and what is done, folded.
    property bool showDone: false
    function dueKey(it) { return it.due ? it.due : "9999" }
    function section(state) {
        var out = items.filter(function (it) { return it.state === state })
        out.sort(function (a, b) { return dueKey(a) < dueKey(b) ? -1 : (dueKey(a) > dueKey(b) ? 1 : 0) })
        return out
    }
    readonly property var rows: {
        var out = []
        function add(state, label, folded) {
            var s = section(state)
            if (s.length === 0) return
            out.push({ header: true, label: label, count: s.length, state: state })
            if (!folded) for (var i = 0; i < s.length; i++) out.push({ header: false, item: s[i] })
        }
        add("inbox", "New", false)
        add("open", "To do", false)
        add("waiting", "Waiting", false)
        add("done", "Done", !showDone)
        return out
    }
    readonly property int pendingSuggestions: items.filter(function (it) { return it.state === "suggested" }).length

    // "today", "tomorrow", "Tue 14 Oct", from a due date or date-time.
    function dueText(d) {
        if (!d) return ""
        var t = new Date(d.length === 10 ? d + "T12:00:00" : d)
        if (isNaN(t.getTime())) return d
        var today = new Date(); today.setHours(12, 0, 0, 0)
        var day = new Date(t.getTime()); day.setHours(12, 0, 0, 0)
        var diff = Math.round((day - today) / 86400000)
        var s = diff === 0 ? "today" : diff === 1 ? "tomorrow" : diff === -1 ? "yesterday"
              : day.toLocaleDateString(Qt.locale(), "ddd d MMM")
        if (d.length > 10) s += " " + t.toLocaleTimeString(Qt.locale(), "HH:mm")
        return s
    }
    function overdue(it) {
        if (!it.due || it.state === "done" || it.state === "dropped") return false
        var t = new Date(it.due.length === 10 ? it.due + "T23:59:59" : it.due)
        return t < new Date()
    }
    // Who said it, as a person would read it: the session is "the assistant".
    function byText(by) {
        if (!by) return ""
        if (current && by === current.session) return "assistant"
        return String(by).split(".")[0]
    }

    Timer { interval: 900; running: true; repeat: false; onTriggered: root.findLists() }
    Timer { interval: 20000; running: true; repeat: true; onTriggered: root.findLists() }
    Timer { interval: 1200; running: true; repeat: true; onTriggered: { root.pollFound() } }
    Timer { interval: 3000; running: root.current !== null; repeat: true; onTriggered: root.pollChanges() }

    // --- the page ------------------------------------------------------------------
    Rectangle { anchors.fill: parent; color: cVoid }

    RowLayout {
        anchors.fill: parent
        anchors.margins: 18
        spacing: 16

        // The lists, by mesh.
        ColumnLayout {
            Layout.preferredWidth: 230
            Layout.maximumWidth: 230
            Layout.fillWidth: false
            Layout.fillHeight: true
            spacing: 8
            Text { text: "Foray"; color: cBone; font.pixelSize: 24; font.weight: Font.DemiBold }
            Text {
                visible: root.problem !== ""
                text: root.problem
                color: cAmber; font.pixelSize: 12; wrapMode: Text.WordWrap
                Layout.fillWidth: true
            }
            Text {
                visible: root.problem === "" && root.lists.length === 0
                text: "Looking for lists on your machines…"
                color: cAsh; font.pixelSize: 12; wrapMode: Text.WordWrap
                Layout.fillWidth: true
            }
            Repeater {
                model: root.lists
                delegate: Rectangle {
                    Layout.fillWidth: true
                    implicitHeight: listCol.implicitHeight + 18
                    radius: 10
                    color: root.current && root.current.key === modelData.key ? cRaised : cPanel
                    border.color: root.current && root.current.key === modelData.key ? cPhosphor : cLine
                    ColumnLayout {
                        id: listCol
                        anchors.fill: parent; anchors.margins: 9
                        spacing: 2
                        Text { text: modelData.title; color: cBone; font.pixelSize: 16 }
                        Text {
                            text: (modelData.counts.open || 0) + " to do"
                                  + (modelData.counts.inbox ? " · " + modelData.counts.inbox + " new" : "")
                                  + " · " + modelData.mesh
                            color: cAsh; font.pixelSize: 11
                        }
                    }
                    MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.openList(modelData) }
                }
            }
            Item { Layout.fillHeight: true }
        }

        // The list.
        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 10
            visible: root.current !== null

            RowLayout {
                Layout.fillWidth: true
                Text { text: root.current ? root.current.title : ""; color: cBone; font.pixelSize: 22 }
                Item { Layout.fillWidth: true }
                Text {
                    visible: root.current !== null && root.current.session !== ""
                    text: (root.context.place || root.context.notes) ? "about us" : "tell the assistant about you"
                    color: (root.context.place || root.context.notes) ? cSky : cAmber; font.pixelSize: 12
                    MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.editingContext = true }
                }
                Text {
                    text: "add a phone"
                    color: cSky; font.pixelSize: 12
                    MouseArea {
                        anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            var p = root.call("foray_core", "get", [root.current.address, root.listPath("/pair")])
                            root.pairing = (p && !p.error) ? p : null
                            if (!p || p.error) root.said = p ? p.error : "no answer"
                        }
                    }
                }
                Text {
                    visible: root.pendingSuggestions > 0
                    text: root.pendingSuggestions + " suggestion" + (root.pendingSuggestions > 1 ? "s" : "") + " with the assistant"
                    color: cViolet; font.pixelSize: 11
                }
            }

            // Somebody's agent wants to suggest things.
            Repeater {
                model: root.asks
                delegate: Rectangle {
                    Layout.fillWidth: true
                    implicitHeight: askRow.implicitHeight + 14
                    radius: 8; color: cPanel; border.color: cAmber
                    RowLayout {
                        id: askRow
                        anchors.fill: parent; anchors.margins: 7
                        Text {
                            text: modelData.from + " wants to suggest things for this list" + (modelData.title ? " — “" + modelData.title + "”" : "")
                            color: cBone; font.pixelSize: 12; wrapMode: Text.WordWrap
                            Layout.fillWidth: true
                        }
                        Text { text: "allow"; color: cPhosphor; font.pixelSize: 12
                               MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.write("POST", "/allow", { from: modelData.from }) } }
                        Text { text: "ignore"; color: cAsh; font.pixelSize: 12
                               MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.write("POST", "/ignore", { from: modelData.from }) } }
                    }
                }
            }

            // Add, in one line.
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: 44; radius: 10
                color: cPanel; border.color: addField.activeFocus ? cPhosphor : cLine
                RowLayout {
                    anchors.fill: parent; anchors.leftMargin: 12; anchors.rightMargin: 8
                    TextInput {
                        id: addField
                        objectName: "addField"
                        Layout.fillWidth: true
                        color: cBone; font.pixelSize: 15; clip: true
                        verticalAlignment: TextInput.AlignVCenter
                        onAccepted: if (text.trim() !== "") { root.write("POST", "/items", { title: text, due: addDue.text.trim() }); text = ""; addDue.text = "" }
                        Text { visible: !parent.text; text: "Add something…"; color: cAsh; font.pixelSize: 15; anchors.verticalCenter: parent.verticalCenter }
                    }
                    TextInput {
                        id: addDue
                        Layout.preferredWidth: 110
                        color: cBone; font.pixelSize: 13; clip: true
                        Text { visible: !parent.text; text: "due 2026-10-20"; color: cLine; font.pixelSize: 13 }
                        onAccepted: addField.accepted()
                    }
                    Text { text: "Add"; color: cPhosphor; font.pixelSize: 14
                           MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: addField.accepted() } }
                }
            }
            Text { visible: root.said !== ""; text: root.said; color: cRust; font.pixelSize: 12 }

            ListView {
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: 6
                model: root.rows
                delegate: Item {
                    width: ListView.view.width
                    implicitHeight: modelData.header ? head.implicitHeight + 14 : card.implicitHeight
                    RowLayout {
                        id: head
                        visible: modelData.header === true
                        anchors.left: parent.left; anchors.bottom: parent.bottom
                        Text { text: modelData.label || ""; color: cAsh; font.pixelSize: 13; font.weight: Font.DemiBold }
                        Text { text: String(modelData.count || ""); color: cLine; font.pixelSize: 12 }
                        Text {
                            visible: modelData.state === "done"
                            text: root.showDone ? "hide" : "show"
                            color: cAsh; font.pixelSize: 11; font.underline: true
                            MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.showDone = !root.showDone }
                        }
                    }
                    Rectangle {
                        id: card
                        visible: modelData.header !== true
                        width: parent.width
                        readonly property var it: modelData.item || ({})
                        implicitHeight: cardRow.implicitHeight + 18
                        radius: 10
                        color: root.selectedId === it.id ? cRaised : cPanel
                        border.color: root.selectedId === it.id ? cPhosphor : cLine
                        RowLayout {
                            id: cardRow
                            anchors.fill: parent; anchors.margins: 9
                            spacing: 10
                            // The tick: done, or not.
                            Rectangle {
                                width: 20; height: 20; radius: 10
                                color: card.it.state === "done" ? cPhosphor : "transparent"
                                border.color: card.it.state === "done" ? cPhosphor : cAsh
                                Text { anchors.centerIn: parent; visible: card.it.state === "done"; text: "✓"; color: cVoid; font.pixelSize: 13 }
                                MouseArea {
                                    anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                                    onClicked: root.write("PATCH", "/items/" + card.it.id, { state: card.it.state === "done" ? "open" : "done" })
                                }
                            }
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: 2
                                Text {
                                    text: card.it.title || ""
                                    color: card.it.state === "done" ? cAsh : cBone
                                    font.pixelSize: 15; font.strikeout: card.it.state === "done"
                                    elide: Text.ElideRight; Layout.fillWidth: true
                                }
                                Text {
                                    readonly property var bits: {
                                        var b = []
                                        if (card.it.due) b.push((root.overdue(card.it) ? "overdue · " : "") + root.dueText(card.it.due))
                                        if (card.it.who) b.push(card.it.who)
                                        if ((card.it.help || []).length) {
                                            var h = card.it.help[card.it.help.length - 1]
                                            b.push(h.state === "completed" ? "answer ready" : h.state === "working" ? "assistant on it"
                                                   : h.state === "failed" ? "help failed" : "help: " + h.state)
                                        }
                                        if ((card.it.comments || []).length) b.push(card.it.comments.length + " note" + (card.it.comments.length > 1 ? "s" : ""))
                                        for (var i = 0; i < (card.it.tags || []).length; i++) b.push("#" + card.it.tags[i])
                                        return b
                                    }
                                    visible: bits.length > 0
                                    text: bits.join(" · ")
                                    color: root.overdue(card.it) ? cAmber : cAsh
                                    font.pixelSize: 11
                                }
                            }
                        }
                        MouseArea {
                            anchors.fill: parent; anchors.leftMargin: 34
                            cursorShape: Qt.PointingHandCursor
                            onClicked: root.selectedId = card.it.id
                        }
                    }
                }
            }
        }

        // One item.
        Rectangle {
            Layout.preferredWidth: 380
            Layout.fillHeight: true
            visible: root.selected !== null
            radius: 12; color: cPanel; border.color: cLine
            Flickable {
                anchors.fill: parent; anchors.margins: 14
                contentHeight: detail.implicitHeight
                clip: true
                ColumnLayout {
                    id: detail
                    width: parent.width
                    spacing: 10
                    readonly property var it: root.selected || ({})

                    TextInput {
                        Layout.fillWidth: true
                        text: detail.it.title || ""
                        color: cBone; font.pixelSize: 19; wrapMode: TextInput.Wrap
                        onEditingFinished: if (text.trim() !== "" && text !== detail.it.title) root.write("PATCH", "/items/" + detail.it.id, { title: text })
                    }
                    Text {
                        text: "added by " + root.byText(detail.it.added_by)
                              + (detail.it.suggested_by ? " · suggested by " + detail.it.suggested_by : "")
                        color: cAsh; font.pixelSize: 11
                    }

                    // State, in words.
                    Flow {
                        Layout.fillWidth: true
                        spacing: 8
                        Repeater {
                            model: [["open", "to do"], ["waiting", "waiting"], ["done", "done"], ["dropped", "drop"]]
                            delegate: Rectangle {
                                radius: 12
                                implicitHeight: 26; implicitWidth: stLabel.implicitWidth + 22
                                color: detail.it.state === modelData[0] ? cRaised : "transparent"
                                border.color: detail.it.state === modelData[0] ? cPhosphor : cLine
                                Text { id: stLabel; anchors.centerIn: parent; text: modelData[1]; color: cBone; font.pixelSize: 12 }
                                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                                            onClicked: root.write("PATCH", "/items/" + detail.it.id, { state: modelData[0] }) }
                            }
                        }
                    }

                    GridLayout {
                        columns: 2; columnSpacing: 10; rowSpacing: 6
                        Layout.fillWidth: true
                        Text { text: "due"; color: cAsh; font.pixelSize: 12 }
                        TextInput {
                            Layout.fillWidth: true; color: cBone; font.pixelSize: 13
                            text: detail.it.due || ""
                            onEditingFinished: if (text !== (detail.it.due || "")) root.write("PATCH", "/items/" + detail.it.id, { due: text.trim() })
                            Text { visible: !parent.text; text: "2026-10-20, or 2026-10-20T09:00:00+02:00"; color: cLine; font.pixelSize: 12 }
                        }
                        Text { text: "who"; color: cAsh; font.pixelSize: 12 }
                        TextInput {
                            Layout.fillWidth: true; color: cBone; font.pixelSize: 13
                            text: detail.it.who || ""
                            onEditingFinished: if (text !== (detail.it.who || "")) root.write("PATCH", "/items/" + detail.it.id, { who: text.trim() })
                            Text { visible: !parent.text; text: "anyone"; color: cLine; font.pixelSize: 12 }
                        }
                        Text { text: "tags"; color: cAsh; font.pixelSize: 12 }
                        TextInput {
                            Layout.fillWidth: true; color: cBone; font.pixelSize: 13
                            text: (detail.it.tags || []).join(", ")
                            onEditingFinished: root.write("PATCH", "/items/" + detail.it.id, { tags: text.split(",") })
                        }
                    }
                    TextEdit {
                        Layout.fillWidth: true
                        text: detail.it.notes || ""
                        color: cBone; font.pixelSize: 13; wrapMode: TextEdit.Wrap
                        onActiveFocusChanged: if (!activeFocus && text !== (detail.it.notes || "")) root.write("PATCH", "/items/" + detail.it.id, { notes: text })
                        Text { visible: !parent.text; text: "Notes"; color: cLine; font.pixelSize: 13 }
                    }

                    // Help from the assistant.
                    Text { visible: (root.current && root.current.session !== ""); text: "Ask the assistant to"; color: cAsh; font.pixelSize: 12; Layout.topMargin: 6 }
                    Flow {
                        visible: root.current && root.current.session !== ""
                        Layout.fillWidth: true; spacing: 8
                        Repeater {
                            model: [["find out", "find out"], ["compare", "compare options"], ["draft", "draft a message"], ["plan", "plan the steps"]]
                            delegate: Rectangle {
                                radius: 12; implicitHeight: 26; implicitWidth: helpLabel.implicitWidth + 22
                                color: "transparent"; border.color: cViolet
                                Text { id: helpLabel; anchors.centerIn: parent; text: modelData[1]; color: cViolet; font.pixelSize: 12 }
                                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                                            onClicked: root.write("POST", "/items/" + detail.it.id + "/ask", { kind: modelData[0], text: askText.text.trim() }) }
                            }
                        }
                    }
                    TextInput {
                        id: askText
                        visible: root.current && root.current.session !== ""
                        Layout.fillWidth: true; color: cBone; font.pixelSize: 12
                        Text { visible: !parent.text; text: "…what exactly (optional)"; color: cLine; font.pixelSize: 12 }
                    }
                    Repeater {
                        model: detail.it.help || []
                        delegate: Rectangle {
                            Layout.fillWidth: true
                            implicitHeight: helpCol.implicitHeight + 14
                            radius: 8; color: cRaised; border.color: modelData.state === "completed" ? cPhosphor : cLine
                            ColumnLayout {
                                id: helpCol
                                anchors.fill: parent; anchors.margins: 7
                                spacing: 3
                                Text { text: (modelData.kind || "help") + " · " + modelData.state; color: cAsh; font.pixelSize: 11 }
                                Text { visible: !!modelData.ask; text: modelData.ask || ""; color: cAsh; font.pixelSize: 12; wrapMode: Text.WordWrap; Layout.fillWidth: true }
                                TextEdit { visible: !!modelData.answer; text: modelData.answer || ""; readOnly: true; selectByMouse: true
                                           color: cBone; font.pixelSize: 13; wrapMode: TextEdit.Wrap; Layout.fillWidth: true }
                            }
                        }
                    }

                    // The conversation on the item.
                    Text { visible: (detail.it.comments || []).length > 0; text: "Notes"; color: cAsh; font.pixelSize: 12; Layout.topMargin: 6 }
                    Repeater {
                        model: detail.it.comments || []
                        delegate: ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 1
                            Text { text: root.byText(modelData.by); color: modelData.by === (root.current ? root.current.session : "") ? cViolet : cAsh; font.pixelSize: 11 }
                            Text { text: modelData.text; color: cBone; font.pixelSize: 13; wrapMode: Text.WordWrap; Layout.fillWidth: true }
                        }
                    }
                    Rectangle {
                        Layout.fillWidth: true
                        implicitHeight: 36; radius: 8; color: cRaised; border.color: cLine
                        TextInput {
                            id: reply
                            anchors.fill: parent; anchors.leftMargin: 10; anchors.rightMargin: 10
                            verticalAlignment: TextInput.AlignVCenter
                            color: cBone; font.pixelSize: 13; clip: true
                            onAccepted: if (text.trim() !== "") { root.write("POST", "/items/" + detail.it.id + "/comments", { text: text }); text = "" }
                            Text { visible: !parent.text; text: "Add a note or answer the assistant…"; color: cAsh; font.pixelSize: 13; anchors.verticalCenter: parent.verticalCenter }
                        }
                    }
                }
            }
        }
    }

    // A phone scans this to find the list: the Foray app, on a phone that is
    // on the same Shrooms mesh.
    Rectangle {
        anchors.fill: parent
        visible: root.pairing !== null
        color: "#cc000000"
        MouseArea { anchors.fill: parent; onClicked: root.pairing = null }
        Rectangle {
            anchors.centerIn: parent
            width: 360; height: 470; radius: 14
            color: cPanel; border.color: cLine
            ColumnLayout {
                anchors.fill: parent; anchors.margins: 20
                spacing: 12
                Text { text: "Add a phone to " + (root.current ? root.current.title : ""); color: cBone; font.pixelSize: 17 }
                Text {
                    Layout.fillWidth: true; wrapMode: Text.WordWrap
                    text: "On the phone: install Shrooms and join this mesh, then open Foray and scan this."
                    color: cAsh; font.pixelSize: 12
                }
                Canvas {
                    id: pairQr
                    Layout.alignment: Qt.AlignHCenter
                    width: 280; height: 280
                    readonly property var rows: root.pairing ? root.pairing.qr : []
                    onRowsChanged: requestPaint()
                    onPaint: {
                        var ctx = getContext("2d")
                        ctx.reset()
                        ctx.fillStyle = "#ffffff"; ctx.fillRect(0, 0, width, height)
                        var n = rows.length
                        if (!n) return
                        var cell = Math.floor(width / (n + 8)), off = Math.floor((width - cell * n) / 2)
                        ctx.fillStyle = "#000000"
                        for (var y = 0; y < n; y++)
                            for (var x = 0; x < n; x++)
                                if (rows[y].charAt(x) === "1") ctx.fillRect(off + x * cell, off + y * cell, cell, cell)
                    }
                }
                TextEdit {
                    Layout.fillWidth: true; readOnly: true; selectByMouse: true; wrapMode: TextEdit.WrapAnywhere
                    text: root.pairing ? root.pairing.link : ""
                    color: cAsh; font.pixelSize: 10
                }
                Text { text: "close"; color: cAsh; font.pixelSize: 12; Layout.alignment: Qt.AlignHCenter
                       MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.pairing = null } }
            }
        }
    }

    // About us: the list's context, which the assistant reads before it
    // triages or asks another agent for help.
    Rectangle {
        anchors.fill: parent
        visible: root.editingContext
        color: "#cc000000"
        // Edits that were cancelled go: the fields start from the list's.
        onVisibleChanged: if (visible) {
            ctxPlace.text = root.context.place || ""
            ctxLanguage.text = root.context.language || ""
            ctxCurrency.text = root.context.currency || ""
            ctxNotes.text = root.context.notes || ""
            root.said = ""
        }
        MouseArea { anchors.fill: parent; onClicked: root.editingContext = false }
        Rectangle {
            anchors.centerIn: parent
            width: Math.min(parent.width - 40, 520); height: Math.min(parent.height - 40, 560); radius: 14
            color: cPanel; border.color: cLine
            MouseArea { anchors.fill: parent }
            ColumnLayout {
                anchors.fill: parent; anchors.margins: 20
                spacing: 10
                Text { text: "About us"; color: cBone; font.pixelSize: 17 }
                Text {
                    Layout.fillWidth: true; wrapMode: Text.WordWrap
                    text: "The assistant reads this before it helps, and passes on what a request needs — so prices come in your currency and places near you. Everybody on the list can change it."
                    color: cAsh; font.pixelSize: 12
                }
                GridLayout {
                    Layout.fillWidth: true
                    columns: 2; columnSpacing: 10; rowSpacing: 8
                    Text { text: "place"; color: cAsh; font.pixelSize: 12 }
                    Rectangle {
                        Layout.fillWidth: true; implicitHeight: 30; radius: 6; color: cRaised; border.color: cLine
                        TextInput { id: ctxPlace; anchors.fill: parent; anchors.margins: 7; color: cBone; font.pixelSize: 13; clip: true
                                    text: root.context.place || ""
                                    Text { visible: !parent.text; text: "Prague, Czechia"; color: cLine; font.pixelSize: 13 } }
                    }
                    Text { text: "language"; color: cAsh; font.pixelSize: 12 }
                    Rectangle {
                        Layout.fillWidth: true; implicitHeight: 30; radius: 6; color: cRaised; border.color: cLine
                        TextInput { id: ctxLanguage; anchors.fill: parent; anchors.margins: 7; color: cBone; font.pixelSize: 13; clip: true
                                    text: root.context.language || ""
                                    Text { visible: !parent.text; text: "what the assistant answers in"; color: cLine; font.pixelSize: 13 } }
                    }
                    Text { text: "currency"; color: cAsh; font.pixelSize: 12 }
                    Rectangle {
                        Layout.fillWidth: true; implicitHeight: 30; radius: 6; color: cRaised; border.color: cLine
                        TextInput { id: ctxCurrency; anchors.fill: parent; anchors.margins: 7; color: cBone; font.pixelSize: 13; clip: true
                                    text: root.context.currency || ""
                                    Text { visible: !parent.text; text: "CZK"; color: cLine; font.pixelSize: 13 } }
                    }
                }
                Text { text: "anything else worth knowing"; color: cAsh; font.pixelSize: 12 }
                Rectangle {
                    Layout.fillWidth: true; Layout.fillHeight: true; radius: 6; color: cRaised; border.color: cLine
                    Flickable {
                        id: ctxFlick
                        anchors.fill: parent; anchors.margins: 8; clip: true
                        contentHeight: ctxNotes.paintedHeight
                        TextEdit {
                            id: ctxNotes
                            width: ctxFlick.width
                            text: root.context.notes || ""
                            color: cBone; font.pixelSize: 13; wrapMode: TextEdit.Wrap
                            Text { visible: !parent.text; width: parent.width; wrapMode: Text.WordWrap; color: cLine; font.pixelSize: 13
                                   text: "The car: 2019 Octavia, tyres 205/55 R16\nWe shop at …\nThe kids: …" }
                        }
                    }
                }
                Text {
                    visible: !!root.context.by
                    text: "last changed by " + root.byText(root.context.by || "")
                    color: cAsh; font.pixelSize: 11
                }
                Text { visible: root.said !== ""; text: root.said; color: cRust; font.pixelSize: 12; Layout.fillWidth: true; wrapMode: Text.WordWrap }
                RowLayout {
                    Layout.fillWidth: true
                    Item { Layout.fillWidth: true }
                    Text { text: "cancel"; color: cAsh; font.pixelSize: 13
                           MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.editingContext = false } }
                    Text { text: "Save"; color: cPhosphor; font.pixelSize: 14; Layout.leftMargin: 16
                           MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                                       onClicked: {
                                           var d = root.write("PUT", "/context", { place: ctxPlace.text, language: ctxLanguage.text,
                                                                                    currency: ctxCurrency.text, notes: ctxNotes.text })
                                           if (d) root.editingContext = false
                                       } } }
                }
            }
        }
    }
}
