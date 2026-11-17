import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import net.asivery.AppLoad 1.0

Rectangle {
    id: root
    width: parent ? parent.width : 1404
    height: parent ? parent.height : 1872
    color: "white"
    focus: true
    signal close

    readonly property real u: width / 1404
    readonly property color ink: "#191919"
    readonly property color muted: "#535353"
    readonly property color line: "#bdbdbd"
    property string place: "library"
    property string folder: ""
    property string query: ""
    property string sortBy: "name"
    property bool hiddenFiles: false
    property bool searching: false
    property var history: []
    property var list: ({title:"My files", location:"My files", items:[], total:0, offset:0})
    property var document: null
    property string notice: ""
    property int serial: 0
    property bool busy: false
    property var callback: null
    property bool keyboard: false

    component Label: Text {
        color: root.ink
        font.family: "Noto Sans"
        font.pixelSize: 29 * root.u
        textFormat: Text.PlainText
        verticalAlignment: Text.AlignVCenter
    }

    component Action: Button {
        id: control
        property bool selected: false
        implicitHeight: 76 * root.u
        implicitWidth: Math.max(108 * root.u, caption.implicitWidth + 42 * root.u)
        padding: 12 * root.u
        focusPolicy: Qt.TabFocus
        contentItem: Label {
            id: caption
            text: control.text
            font.pixelSize: 26 * root.u
            color: !control.enabled ? "#858585" : control.selected || control.down ? "white" : root.ink
            horizontalAlignment: Text.AlignHCenter
        }
        background: Rectangle {
            color: control.selected || control.down ? root.ink : "white"
            border.color: control.activeFocus ? root.ink : root.line
            border.width: control.activeFocus ? 3 * root.u : root.u
            radius: 8 * root.u
        }
    }

    component Glyph: Canvas {
        id: glyph
        property bool folderShape: false
        implicitWidth: 50 * root.u
        implicitHeight: 50 * root.u
        onFolderShapeChanged: requestPaint()
        onPaint: {
            var p = getContext("2d")
            p.reset()
            p.scale(width/48, height/48)
            p.lineWidth = 2.6
            p.lineJoin = "round"
            p.strokeStyle = "#282828"
            p.beginPath()
            if (folderShape) {
                p.moveTo(3,12); p.lineTo(16,12); p.lineTo(20,16); p.lineTo(44,16)
                p.lineTo(44,38); p.lineTo(3,38); p.closePath()
                p.moveTo(3,20); p.lineTo(44,20)
            } else {
                p.moveTo(10,4); p.lineTo(28,4); p.lineTo(38,14); p.lineTo(38,44)
                p.lineTo(10,44); p.closePath(); p.moveTo(28,4); p.lineTo(28,14)
                p.lineTo(38,14); p.moveTo(16,23); p.lineTo(32,23); p.moveTo(16,29); p.lineTo(30,29)
            }
            p.stroke()
        }
    }

    function send(action, attrs, done) {
        if (busy) return
        var msg = Object.assign({id:++serial, action:action, place:place, path:folder,
                                 query:query, sort:sortBy, hidden:hiddenFiles}, attrs)
        callback = done
        notice = ""
        busy = true
        timeout.restart()
        transport.sendMessage(100, JSON.stringify(msg))
    }
    function received(payload) {
        var r
        try { r = JSON.parse(payload) } catch (e) { return }
        if (!busy || serial !== r.id) return
        timeout.stop()
        busy = false
        var done = callback
        callback = null
        if (r.error) { notice = r.error; return }
        done(r.data)
    }
    function hideKeys() {
        keyboard = false
        Qt.inputMethod.commit()
        Qt.inputMethod.hide()
        root.forceActiveFocus()
    }
    function showKeys() {
        searchBox.forceActiveFocus()
        keyboard = true
        Qt.callLater(function() { if (keyboard) Qt.inputMethod.show() })
    }
    function snapshot() {
        return {place:place, path:folder, query:query, sort:sortBy, hidden:hiddenFiles,
                offset:list.offset, scroll:rows.contentY, searching:searching}
    }
    function showList(state, trail, refresh) {
        hideKeys()
        send("list", Object.assign({}, state, {refresh:!!refresh}), function(data) {
            document = null
            place = state.place
            folder = state.path
            query = state.query
            sortBy = state.sort
            hiddenFiles = state.hidden
            searching = !!state.searching
            searchBox.text = query
            history = trail
            list = data
            rows.positionViewAtBeginning()
            if (state.scroll > 0) Qt.callLater(function() {
                rows.contentY = Math.max(rows.originY, Math.min(state.scroll, rows.contentHeight - rows.height + rows.originY))
            })
        })
    }
    function selectPlace(p, path) {
        showList({place:p, path:path || "", query:"", sort:"name", hidden:false, offset:0}, [], true)
    }
    function update(changes, refresh) {
        showList(Object.assign(snapshot(), {scroll:0}, changes), history, refresh)
    }
    function openItem(item) {
        if (busy) return
        hideKeys()
        if (item.folder) {
            var state = Object.assign(snapshot(), {path:item.path, query:"", searching:false, offset:0, scroll:0})
            showList(state, history.concat([snapshot()]), false)
        } else {
            inspect(item.path, 0)
        }
    }
    function inspect(path, index) {
        send("info", {path:path, offset:index}, function(data) {
            document = data
            textScroll.contentY = 0
        })
    }
    function back() {
        if (busy) return
        hideKeys()
        notice = ""
        if (document) { document = null; return }
        if (searching) { update({query:"", searching:false, offset:0}); return }
        if (history.length) showList(history[history.length-1], history.slice(0,-1), false)
    }
    function toggleSearch() {
        if (searching) { update({query:"", searching:false, offset:0}); return }
        searching = true
        searchBox.text = query
        showKeys()
    }
    function searchNow() {
        if (busy) return
        Qt.inputMethod.commit()
        update({query:searchBox.text.trim(), offset:0, searching:true})
    }
    function copyOriginal() {
        send("export", {path:document.path}, function(destination) { notice = "Copied to " + destination })
    }
    function dateLabel(value) {
        return value ? new Date(value).toLocaleDateString(Qt.locale(), "MMM d, yyyy") : ""
    }
    function sizeLabel(n) {
        if (!n) return "0 B"
        var units = ["B","KiB","MiB","GiB"], i=0
        while (n >= 1024 && i<units.length-1) { n/=1024; ++i }
        return (i && n < 10 ? n.toFixed(1) : Math.round(n)) + " " + units[i]
    }
    function exitApp() { hideKeys(); close() }
    function unloading() { hideKeys() }
    Keys.onEscapePressed: function(event) {
        if (keyboard) hideKeys()
        else if (document || searching || history.length) back()
        else exitApp()
        event.accepted = true
    }
    AppLoad {
        id: transport
        applicationID: "RemarkFinder"
        onMessageReceived: function(type, contents) { if (type === 101) root.received(contents) }
    }
    Timer {
        id: timeout
        interval: 45000
        onTriggered: {
            root.busy = false
            root.callback = null
            root.notice = "reFinder did not receive a reply. Tap Refresh to try again."
        }
    }
    Component.onCompleted: Qt.callLater(function(){ root.selectPlace("library") })

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 50 * root.u
        anchors.bottomMargin: 40 * root.u + (root.keyboard && Qt.inputMethod.visible ? Math.min(root.height*0.55, Qt.inputMethod.keyboardRectangle.height) : 0)
        spacing: 22 * root.u

        RowLayout {
            Layout.fillWidth: true
            spacing: 16 * root.u
            Label {
                text: "reFinder"
                font.family: "EB Garamond"
                font.pixelSize: 80 * root.u
                Layout.fillWidth: true
            }
            Action { text:"Back"; objectName:"backBtn"; visible: root.document !== null || root.history.length > 0 || root.searching; enabled: !root.busy; onClicked: root.back() }
            Action { text:"Close"; onClicked: root.exitApp() }
        }
        Rectangle { Layout.fillWidth:true; implicitHeight:2*root.u; color:root.ink }

        RowLayout {
            visible: root.document === null
            spacing: 12 * root.u
            Action { text:"My files"; objectName:"libraryBtn"; selected: root.place === "library"; enabled: !root.busy; onClicked: root.selectPlace("library") }
            Action { text:"Obsidian"; objectName:"vaultBtn"; selected: root.place === "vaults"; enabled: !root.busy; onClicked: root.selectPlace("vaults") }
            Action { text:"Device files"; objectName:"deviceBtn"; selected: root.place === "device"; enabled: !root.busy; onClicked: root.selectPlace("device") }
            Item { Layout.fillWidth:true }
            Action { text:"Search"; objectName:"searchBtn"; selected:root.searching; enabled:!root.busy; onClicked:root.toggleSearch() }
        }
        Label {
            Layout.fillWidth:true
            text: root.document ? root.document.name : root.list.title
            font.family:"EB Garamond"
            font.pixelSize:60*root.u
            maximumLineCount:2; wrapMode:Text.Wrap; elide:Text.ElideRight
        }
        Label {
            Layout.fillWidth:true
            text: root.document ? root.document.location : root.list.location
            visible: root.document !== null || text !== root.list.title
            font.pixelSize:24*root.u; color:root.muted
            elide:Text.ElideMiddle
        }

        RowLayout {
            visible:root.searching && root.document === null
            Layout.fillWidth:true
            TextField {
                id:searchBox; objectName:"queryField"
                Layout.fillWidth:true; implicitHeight:76*root.u; padding:16*root.u
                font.family:"Noto Sans"; font.pixelSize:29*root.u
                color:root.ink; placeholderTextColor:root.muted
                placeholderText: "Search names in this folder and its subfolders"
                activeFocusOnPress:false; selectByMouse:true
                onPressed: root.showKeys()
                onAccepted: root.searchNow()
                background:Rectangle {color:"white";radius:7*root.u;border.color:root.ink}
            }
            Action {text:"Find"; objectName:"findBtn"; enabled:!root.busy; onClicked:root.searchNow()}
            Action {text:"Clear"; enabled:!root.busy; onClicked:root.update({query:"", searching:false, offset:0})}
        }
        RowLayout {
            visible:root.document === null; Layout.fillWidth:true; spacing:12*root.u
            Label {
                Layout.fillWidth:true; font.pixelSize:25*root.u; color:root.muted
                text: root.busy ? "Loading…" : root.list.total + (root.list.total === 1 ? " item" : " items")
            }
            Action { text: root.sortBy === "name" ? "Sort: Name" : "Sort: Modified"; enabled:!root.busy; onClicked:root.update({sort:root.sortBy==="name"?"recent":"name",offset:0}) }
            Action { text:"Hidden files"; visible:root.place!=="library"; selected:root.hiddenFiles; enabled:!root.busy; onClicked:root.update({hidden:!root.hiddenFiles,offset:0}) }
            Action { text:"Refresh"; objectName:"refreshBtn"; enabled:!root.busy; onClicked:root.update({},true) }
        }
        Label {
            Layout.fillWidth:true; font.pixelSize:26*root.u
            text:root.notice || (!root.document && root.list.warning) || ""
            visible:text.length>0
            wrapMode:Text.Wrap
        }
        Rectangle { Layout.fillWidth:true; implicitHeight:root.u; color:root.line }
        Item {
            Layout.fillWidth:true; Layout.fillHeight:true
            visible:root.document===null
            ListView {
                id:rows
                anchors.fill:parent; clip:true
                boundsBehavior:Flickable.StopAtBounds
                model:root.list.items
                ScrollBar.vertical:ScrollBar {}
                delegate:Item {
                    id:cell
                    required property var modelData
                    width:rows.width
                    height:116*root.u
                    RowLayout {
                        anchors.fill:parent; anchors.leftMargin:9*root.u; anchors.rightMargin:24*root.u; spacing:24*root.u
                        Glyph {folderShape:cell.modelData.folder}
                        ColumnLayout {
                            Layout.fillWidth:true; spacing:3*root.u
                            Label {text:cell.modelData.name; Layout.fillWidth:true; font.pixelSize:34*root.u; elide:Text.ElideRight}
                            Label {text:cell.modelData.kind + (cell.modelData.location ? "  ·  "+cell.modelData.location : ""); Layout.fillWidth:true; elide:Text.ElideMiddle; color:root.muted; font.pixelSize:23*root.u}
                        }
                        Label {text:root.dateLabel(cell.modelData.modified); color:root.muted; font.pixelSize:23*root.u}
                        Label {text:"›"; font.pixelSize:38*root.u}
                    }
                    Rectangle {anchors.bottom:parent.bottom; width:parent.width; height:root.u; color:root.line}
                    MouseArea {anchors.fill:parent; enabled:!root.busy; onClicked:root.openItem(cell.modelData)}
                }
            }
            Label {
                anchors.centerIn:parent; visible:rows.count===0 && !root.busy && !root.notice
                text:root.query ? "No matching names" : "This folder is empty"
            }
        }
        RowLayout {
            visible:root.document===null && root.list.total>32
            Layout.fillWidth:true
            Action {text:"Previous"; enabled:!root.busy && root.list.offset>0; onClicked:root.update({offset:root.list.offset-32})}
            Label {Layout.fillWidth:true; horizontalAlignment:Text.AlignHCenter; text:(root.list.offset+1)+"–"+(root.list.offset+root.list.items.length)+" of "+root.list.total}
            Action {text:"Next"; enabled:!root.busy && root.list.offset+32<root.list.total; onClicked:root.update({offset:root.list.offset+32})}
        }
        ColumnLayout {
            visible:root.document!==null; Layout.fillWidth:true; Layout.fillHeight:true; spacing:18*root.u
            Label {
                Layout.fillWidth:true; color:root.muted; font.pixelSize:25*root.u
                text:root.document ? [root.document.kind, root.document.pages ? root.document.pages+" pages" : "",
                    root.document.size || root.place!=="library" ? root.sizeLabel(root.document.size) : "", root.dateLabel(root.document.modified)].filter(Boolean).join("  ·  ") : ""
            }
            Label {Layout.fillWidth:true; text:root.document ? root.document.note || "" : ""; visible:text.length>0; wrapMode:Text.Wrap; font.pixelSize:28*root.u}
            Flickable {
                id:textScroll; visible:root.document!==null && root.document.preview==="text"
                Layout.fillWidth:true; Layout.fillHeight:true; clip:true
                contentWidth:width; contentHeight:bodyText.implicitHeight
                boundsBehavior:Flickable.StopAtBounds; ScrollBar.vertical:ScrollBar {}
                Label {id:bodyText; width:parent.width; text:root.document ? root.document.text || "" : ""; font.family:"Noto Sans Mono"; font.pixelSize:28*root.u; wrapMode:Text.WrapAnywhere}
            }
            Image {
                id:previewImage
                visible:root.document!==null && root.document.preview==="image"
                Layout.fillWidth:true; Layout.fillHeight:true; fillMode:Image.PreserveAspectFit
                source: root.document ? root.document.image || "" : ""; asynchronous:true; cache:false
                sourceSize.width:1404; sourceSize.height:1872
            }
            Label {visible:previewImage.visible && previewImage.status===Image.Error; text:"The preview image could not be loaded."}
            Item {visible:root.document!==null && root.document.preview==="info"; Layout.fillHeight:true}
            RowLayout {
                Layout.fillWidth:true; spacing:16*root.u
                Action {text:"Previous preview"; visible:root.document!==null && root.document.images>1; enabled:!root.busy && root.document!==null && root.document.imageIndex>0; onClicked:root.inspect(root.document.path,root.document.imageIndex-1)}
                Action {text:"Next preview"; visible:root.document!==null && root.document.images>1; enabled:!root.busy && root.document!==null && root.document.imageIndex+1<root.document.images; onClicked:root.inspect(root.document.path,root.document.imageIndex+1)}
                Item { Layout.fillWidth:true }
                Action {text:"Copy original to Downloads"; objectName:"exportBtn"; visible:root.document!==null && !!root.document.exportable; enabled:!root.busy; onClicked:root.copyOriginal()}
            }
        }
    }
}
