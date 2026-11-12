import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import net.asivery.AppLoad 1.0
import net.asivery.ApploadUtils

Rectangle {
    id: root
    objectName: "remarkmail"
    width: parent ? parent.width : 1404
    height: parent ? parent.height : 1872
    color: "white"
    focus: true
    signal close

    readonly property real u: width / 1404
    readonly property color ink: "#171717"
    readonly property color muted: "#626262"
    readonly property color rule: "#c7c7c7"
    readonly property real gutter: 52 * u
    property string mode: "loading"
    property string folder: "inbox"
    property string account: ""
    property bool demo: false
    property bool configured: false
    property string notice: ""
    property string mailboxStatus: ""
    property bool busy: false
    property string busyLabel: ""
    property bool searchVisible: false
    property string query: ""
    property string nextPage: ""
    property var conversation: ({ messages: [] })
    property var draft: ({})
    property bool settingDraft: false
    property int revision: 0
    property int savedRevision: 0
    property bool saving: false
    property var saveWaiters: []
    property bool keyboardRequested: false
    property var inputTarget: null
    property bool sendConfirmation: false
    property bool deleteConfirmation: false
    property bool privacyVisible: false
    property real readingScale: 1.0
    property int serial: 0
    property var pending: ({})
    readonly property bool dirty: mode === "compose" && revision > savedRevision
    readonly property bool editable: mode === "compose" && draft.state === "draft" && !busy && !sendConfirmation && !deleteConfirmation && !privacyVisible
    readonly property real keyboardInset: keyboardRequested && Qt.inputMethod.visible ? Math.min(height * 0.65, Qt.inputMethod.keyboardRectangle.height) : 0

    component InkButton: Button {
        id: control
        property bool selected: false
        property bool quiet: false
        implicitHeight: 76 * root.u
        implicitWidth: Math.max(110 * root.u, caption.implicitWidth + 44 * root.u)
        focusPolicy: Qt.TabFocus
        hoverEnabled: false
        padding: 16 * root.u
        contentItem: Text {
            id: caption
            text: control.text
            font.family: "Noto Sans"
            font.pixelSize: 27 * root.u
            font.weight: control.selected ? Font.DemiBold : Font.Medium
            color: !control.enabled ? "#999999" : control.selected || control.down ? "white" : root.ink
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
        }
        background: Rectangle {
            color: control.enabled && (control.selected || control.down) ? root.ink : "white"
            radius: 8 * root.u
            border.color: control.activeFocus ? root.ink : root.rule
            border.width: control.activeFocus ? 3 * root.u : control.quiet ? 0 : root.u
        }
    }

    component MailField: TextField {
        font.family: "Noto Sans"
        font.pixelSize: 30 * root.u
        color: root.ink
        padding: 20 * root.u
        activeFocusOnPress: false
        selectByMouse: true
        background: Rectangle { color: "white"; border.color: parent.activeFocus ? root.ink : root.rule; radius: 6 * root.u }
        onPressed: { root.inputTarget = this; forceActiveFocus(); }
    }

    function rpc(action, params, done, blocking) {
        var id = ++serial;
        var payload = JSON.stringify({id:id, action:action, params:params || {}});
        if (encodeURIComponent(payload).replace(/%[0-9A-F]{2}/gi, "x").length > 180*1024) {
            notice = "This message is too long to save on the tablet. Shorten it before continuing.";
            if (done) Qt.callLater(function(){done(false,null);});
            return;
        }
        pending[id] = { done: done, at: Date.now(), action: action, blocking: blocking !== false };
        if (blocking !== false) { busy = true; busyLabel = action === "send" ? "Sending…" : "Loading…"; }
        appload.sendMessage(100, payload);
    }
    function receive(contents) {
        var response;
        try { response = JSON.parse(contents); } catch (error) { notice = "The mail service returned an unreadable response."; return; }
        var task = pending[response.id];
        if (!task) return;
        delete pending[response.id];
        if (task.blocking) { busy = false; busyLabel = ""; }
        if (!response.ok) notice = response.error || "Could not complete this action.";
        if (task.done) task.done(response.ok, response.result);
    }
    function initialize() {
        rpc("status", {}, function(ok, result) {
            if (!ok) { mode = "setup"; return; }
            configured = result.configured; account = result.email || ""; demo = result.demo;
            mode = configured ? "list" : "setup";
            if (configured) loadMailbox(false);
        });
    }
    function hideKeyboard() { keyboardRequested = false; Qt.inputMethod.commit(); Qt.inputMethod.hide(); }
    function toggleKeyboard() {
        if (keyboardRequested) { hideKeyboard(); return; }
        if (!inputTarget) inputTarget = mode === "compose" ? draftBody : searchBox;
        inputTarget.forceActiveFocus(); keyboardRequested = true;
        Qt.callLater(function() { if (keyboardRequested) Qt.inputMethod.show(); });
    }
    function loadMailbox(append) {
        if (busy) return;
        if (folder === "drafts") { loadDrafts(); return; }
        rpc("list", {folder:folder, query:query, page:append ? nextPage : ""}, function(ok, result) {
            if (!ok) return;
            if (!append) threads.clear();
            for (var i=0; i<result.threads.length; i++) threads.append(result.threads[i]);
            nextPage = result.next || "";
            mailboxStatus = result.cached ? "Saved mailbox · " + formatDate(result.updated) : "Updated " + formatDate(result.updated);
            notice = result.cached ? (result.warning || "Offline. Showing saved messages.") : "";
        });
    }
    function loadDrafts() {
        rpc("draft-list", {}, function(ok, result) {
            if (!ok) return;
            drafts.clear(); for (var i=0; i<result.length; i++) drafts.append(result[i]);
            mailboxStatus = "Drafts saved on this tablet";
        });
    }
    function chooseFolder(value) {
        if (busy) return;
        folder = value; query = ""; searchBox.text = ""; searchVisible = false;
        hideKeyboard(); threads.clear(); drafts.clear(); nextPage = ""; notice = ""; loadMailbox(false);
    }
    function openThread(id) {
        if (busy) return;
        hideKeyboard();
        rpc("thread", {id:id}, function(ok, result) {
            if (!ok) return;
            conversation = result; mode = "thread"; reader.contentY = 0;
            notice = (result.cached ? "Offline copy · Saved " + formatDate(result.updated) : "")
                + (result.shortened ? "  Showing the latest messages. Long content has been shortened; open Gmail for the full conversation." : "");
        });
    }
    function modify(operation) {
        if (busy) return;
        rpc("modify", {id:conversation.id, operation:operation}, function(ok) {
            if (!ok) return;
            if (operation === "archive") { mode = "list"; notice = "Conversation archived."; loadMailbox(false); return; }
            var next = Object.assign({}, conversation);
            if (operation === "read" || operation === "unread") next.unread = operation === "unread";
            if (operation === "star" || operation === "unstar") next.starred = operation === "star";
            conversation = next;
        });
    }
    function applyDraft(value) {
        settingDraft = true; saveTimer.stop(); draft = value;
        revision = value.revision; savedRevision = value.revision;
        draftTo.text = value.to || ""; draftCC.text = value.cc || "";
        draftSubject.text = value.subject || ""; draftBody.text = value.body || "";
        mode = "compose"; composer.contentY = 0; settingDraft = false; inputTarget = draftBody;
        hideKeyboard();
        if (value.state !== "draft") notice = "Delivery uncertain. Check Sent in Gmail before sending another copy.";
    }
    function newDraft(reply) {
        if (busy) return;
        var value = {to:"",cc:"",subject:"",body:"",threadId:"",replyMessageId:"",references:""};
        if (reply && conversation.messages.length) {
            var messages = conversation.messages;
            var last = messages[messages.length-1];
            value.to = last.replyTo || last.from;
            if (last.from.toLowerCase().indexOf(account.toLowerCase()) >= 0) value.to = last.to;
            value.subject = /^re:/i.test(conversation.subject) ? conversation.subject : "Re: " + conversation.subject;
            value.threadId = conversation.id; value.replyMessageId = last.messageId; value.references = last.references || "";
        }
        notice = "";
        rpc("draft-new", {draft:value}, function(ok, result) { if (ok) applyDraft(result); });
    }
    function openDraft(id) { if (!busy) { notice = ""; rpc("draft-load", {id:id}, function(ok, result) { if (ok) applyDraft(result); }); } }
    function editDraft() {
        if (settingDraft || mode !== "compose" || draft.state !== "draft") return;
        revision++; saveTimer.restart();
    }
    function snapshot() {
        return {id:draft.id, to:draftTo.text, cc:draftCC.text, subject:draftSubject.text, body:draftBody.text,
            threadId:draft.threadId || "", replyMessageId:draft.replyMessageId || "", references:draft.references || "", revision:revision};
    }
    function finishSaving(ok) {
        var waiters = saveWaiters; saveWaiters = [];
        for (var i=0; i<waiters.length; i++) waiters[i](ok);
    }
    function saveDraft(done) {
        if (done) saveWaiters.push(done);
        saveTimer.stop();
        if (saving) return;
        if (!dirty) { finishSaving(true); return; }
        saving = true; var sentRevision = revision;
        rpc("draft-save", {draft:snapshot()}, function(ok, result) {
            saving = false;
            if (!ok) { finishSaving(false); return; }
            savedRevision = Math.max(savedRevision, result.revision);
            if (revision > sentRevision) { saveDraft(); return; }
            finishSaving(true);
        }, false);
    }
    function back() {
        if (busy || sendConfirmation || deleteConfirmation) return;
        if (mode === "compose") { saveDraft(function(ok) { if (ok) { hideKeyboard(); mode="list"; chooseFolder("drafts"); } }); }
        else { hideKeyboard(); mode="list"; loadMailbox(false); }
    }
    function requestSend() {
        if (!editable || saving) return;
        Qt.inputMethod.commit();
        saveDraft(function(ok) { if (ok) { hideKeyboard(); sendConfirmation=true; } });
    }
    function confirmSend() {
        if (busy) return;
        sendConfirmation = false;
        rpc("send", {id:draft.id, revision:savedRevision, confirm:true}, function(ok, result) {
            if (ok) { hideKeyboard(); mode="list"; folder="inbox"; query=""; searchBox.text=""; threads.clear(); mailboxStatus="Refresh to check your mailbox"; notice=result.demo ? "Preview complete. No email was sent." : "Email sent."; }
            else { rpc("draft-load", {id:draft.id}, function(loaded, value) { if (loaded) applyDraft(value); }); }
        });
    }
    function deleteDraft() {
        deleteConfirmation = false;
        rpc("draft-delete", {id:draft.id}, function(ok) { if (ok) { hideKeyboard(); mode="list"; chooseFolder("drafts"); } });
    }
    function exitApp() {
        if (busy || sendConfirmation || deleteConfirmation) return;
        if (mode === "compose") saveDraft(function(ok) { if (ok) { hideKeyboard(); close(); } });
        else { hideKeyboard(); close(); }
    }
    function unloading() {
        Qt.inputMethod.commit();
        if (dirty) saveDraft();
        hideKeyboard();
        // Keep the idle backend; AppLoad 0.6.0 can race termination against unloading.
    }
    function formatDate(value) {
        var d = new Date(value); if (isNaN(d.getTime())) return value || "";
        return d.toLocaleDateString(Qt.locale(), "MMM d") + " · " + d.toLocaleTimeString(Qt.locale(), "h:mm ap");
    }
    function sender(value) { return value.replace(/\s*<[^>]+>/g, "").replace(/^"|"$/g, ""); }
    function handleKey(event) {
        var command = event.modifiers & (Qt.ControlModifier | Qt.MetaModifier);
        if (event.key === Qt.Key_Escape) {
            if (busy) return true;
            if (sendConfirmation || deleteConfirmation || privacyVisible) { sendConfirmation=false; deleteConfirmation=false; privacyVisible=false; }
            else if (keyboardRequested) hideKeyboard(); else if (mode === "compose" || mode === "thread") back(); else exitApp();
            return true;
        }
        if (command && event.key === Qt.Key_S && mode === "compose") { saveDraft(); return true; }
        if (command && event.key === Qt.Key_N && mode === "list") { newDraft(false); return true; }
        if (event.key === Qt.Key_F6 && (mode === "compose" || searchVisible)) { toggleKeyboard(); return true; }
        return false;
    }
    Keys.onPressed: function(event) { event.accepted=handleKey(event); }

    AppLoad { id: appload; applicationID: "RemarkMail"; onMessageReceived: function(type, contents) { if (type===101) root.receive(contents); } }
    Connections { target: Qt.inputMethod; function onVisibleChanged() { root.keyboardRequested=Qt.inputMethod.visible && (root.mode==="compose" || root.searchVisible); } }
    Timer { id: saveTimer; interval: 650; onTriggered: root.saveDraft(); }
    Timer {
        interval: 1000; running: true; repeat: true
        onTriggered: {
            for (var key in root.pending) {
                var task=root.pending[key];
                if (Date.now()-task.at < 65000) continue;
                delete root.pending[key]; if (task.blocking) root.busy=false;
                root.notice=task.action==="send" ? "Delivery is uncertain. Check Sent in Gmail before sending another copy." : "The mail service did not respond. Try reopening Mail.";
                if (task.done) task.done(false, null);
            }
        }
    }
    ListModel { id: threads }
    ListModel { id: drafts }
    Component.onCompleted: Qt.callLater(initialize)

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: root.gutter
        anchors.bottomMargin: root.gutter + root.keyboardInset
        spacing: 24 * root.u

        RowLayout {
            Layout.fillWidth: true; spacing: 20 * root.u
            ColumnLayout {
                Layout.fillWidth: true; spacing: 3 * root.u
                Text { text: root.mode==="compose" ? "Write a letter" : "reMail"; font.family:"EB Garamond"; font.pixelSize:76*root.u; color:root.ink }
                Text { text: root.demo ? "PREVIEW · Synthetic mailbox" : root.account || "A little space for your inbox"; font.family:"Noto Sans"; font.pixelSize:24*root.u; color:root.muted; elide:Text.ElideRight; Layout.fillWidth:true }
            }
            InkButton { objectName:"privacyButton"; text:"Privacy"; quiet:true; enabled:!root.busy; onClicked:{root.hideKeyboard();root.privacyVisible=true;} }
            InkButton { objectName:"backButton"; visible: root.mode==="thread" || root.mode==="compose"; text:"Back"; enabled:!root.busy && !root.saving; onClicked:root.back() }
            InkButton { objectName:"closeButton"; text:"Close"; enabled:!root.busy && !root.saving; onClicked:root.exitApp() }
        }
        Rectangle { Layout.fillWidth:true; height:2*root.u; color:root.ink }

        RowLayout {
            visible: root.mode==="list"; Layout.fillWidth:true; spacing:8*root.u
            Repeater {
                model:[{key:"inbox",label:"Inbox"},{key:"starred",label:"Starred"},{key:"sent",label:"Sent"},{key:"all",label:"All mail"},{key:"drafts",label:"Drafts"}]
                InkButton { required property var modelData; text:modelData.label; selected:root.folder===modelData.key; enabled:!root.busy; onClicked:root.chooseFolder(modelData.key) }
            }
            Item { Layout.fillWidth:true }
            InkButton { objectName:"composeButton"; text:"Compose"; selected:true; enabled:!root.busy; onClicked:root.newDraft(false) }
        }
        RowLayout {
            visible: root.mode==="list"; Layout.fillWidth:true; spacing:16*root.u
            Text { text:root.mailboxStatus; color:root.muted; font.family:"Noto Sans"; font.pixelSize:24*root.u; Layout.fillWidth:true; elide:Text.ElideRight }
            InkButton { text:"Search"; quiet:true; visible:root.folder!=="drafts"; enabled:!root.busy; onClicked:{ root.searchVisible=!root.searchVisible; if (!root.searchVisible) root.hideKeyboard(); } }
            InkButton { objectName:"refreshButton"; text:"Refresh"; quiet:true; enabled:!root.busy; onClicked:root.loadMailbox(false) }
        }
        RowLayout {
            visible:root.mode==="list" && root.searchVisible; Layout.fillWidth:true; spacing:12*root.u
            MailField { id:searchBox; objectName:"searchBox"; Layout.fillWidth:true; placeholderText:"Search Gmail"; enabled:!root.busy; onAccepted:{ root.query=text; root.hideKeyboard(); root.loadMailbox(false); } }
            InkButton { text:"Keyboard"; selected:root.keyboardRequested; onClicked:{root.inputTarget=searchBox;root.toggleKeyboard();} }
            InkButton { text:"Find"; enabled:!root.busy; onClicked:{root.query=searchBox.text;root.hideKeyboard();root.loadMailbox(false);} }
        }
        Rectangle {
            visible: root.notice!=="" || root.busy
            Layout.fillWidth:true; implicitHeight:noticeText.implicitHeight+32*root.u
            color:"#f1f1f1"; radius:6*root.u
            Text { id:noticeText; anchors.fill:parent; anchors.margins:16*root.u; text:root.busy ? root.busyLabel : root.notice; wrapMode:Text.Wrap; color:root.ink; font.family:"Noto Sans"; font.pixelSize:25*root.u; textFormat:Text.PlainText }
        }

        Item {
            visible:root.mode==="setup" || root.mode==="loading"; Layout.fillWidth:true; Layout.fillHeight:true
            Column {
                anchors.centerIn:parent; width:parent.width*0.82; spacing:30*root.u
                Text { width:parent.width; text:root.mode==="loading" ? "Opening your inbox…" : "Your inbox, on paper."; font.family:"EB Garamond"; font.pixelSize:64*root.u; color:root.ink; horizontalAlignment:Text.AlignHCenter; wrapMode:Text.Wrap }
                Text { width:parent.width; text:"Connect your Gmail account using the setup tool on your Mac. Once connected, Mail works directly over this tablet’s Wi-Fi."; visible:root.mode==="setup"; font.family:"Noto Sans"; font.pixelSize:31*root.u; color:root.muted; horizontalAlignment:Text.AlignHCenter; wrapMode:Text.Wrap }
                InkButton { anchors.horizontalCenter:parent.horizontalCenter; text:"Check connection"; visible:root.mode==="setup"; enabled:!root.busy; onClicked:root.initialize() }
            }
        }

        ListView {
            id:mailbox; objectName:"mailbox"; visible:root.mode==="list" && root.folder!=="drafts"; Layout.fillWidth:true; Layout.fillHeight:true; clip:true; model:threads; spacing:0
            ScrollBar.vertical:ScrollBar { policy:ScrollBar.AsNeeded }
            delegate:Item {
                id:threadRow
                required property string id
                required property string subject
                required property string from
                required property string snippet
                required property string date
                required property bool unread
                required property bool starred
                required property int count
                width:mailbox.width; height:230*root.u
                Column {
                    anchors.fill:parent; anchors.topMargin:27*root.u; anchors.rightMargin:16*root.u; spacing:11*root.u
                    Row {
                        width:parent.width; spacing:14*root.u
                        Text { text:threadRow.unread ? "●" : "○"; font.pixelSize:23*root.u; color:root.ink }
                        Text { text:root.sender(threadRow.from); width:parent.width-300*root.u; font.family:"Noto Sans"; font.pixelSize:27*root.u; font.weight:threadRow.unread ? Font.DemiBold : Font.Normal; elide:Text.ElideRight; color:root.ink; textFormat:Text.PlainText }
                        Text { text:root.formatDate(threadRow.date).split(" · ")[0]; width:220*root.u; horizontalAlignment:Text.AlignRight; font.family:"Noto Sans"; font.pixelSize:24*root.u; color:root.muted; textFormat:Text.PlainText }
                    }
                    Text { width:parent.width; text:(threadRow.starred ? "★  " : "")+threadRow.subject+(threadRow.count>1 ? "  ("+threadRow.count+")" : ""); font.family:"EB Garamond"; font.pixelSize:43*root.u; font.weight:threadRow.unread ? Font.DemiBold : Font.Normal; elide:Text.ElideRight; color:root.ink; textFormat:Text.PlainText }
                    Text { width:parent.width; text:threadRow.snippet; font.family:"Noto Sans"; font.pixelSize:27*root.u; maximumLineCount:2; elide:Text.ElideRight; wrapMode:Text.Wrap; color:root.muted; textFormat:Text.PlainText }
                }
                Rectangle { anchors.bottom:parent.bottom; width:parent.width; height:root.u; color:root.rule }
                MouseArea { anchors.fill:parent; enabled:!root.busy; onClicked:root.openThread(threadRow.id) }
            }
            Text { anchors.centerIn:parent; visible:threads.count===0 && !root.busy; text:root.query!=="" ? "No matching conversations" : "No conversations to show"; font.family:"EB Garamond"; font.pixelSize:45*root.u; color:root.muted }
            footer:Item { width:mailbox.width; height:root.nextPage!=="" ? 110*root.u : 0; InkButton { anchors.centerIn:parent; text:"Load more"; visible:root.nextPage!==""; enabled:!root.busy; onClicked:root.loadMailbox(true) } }
        }

        ListView {
            id:draftList; visible:root.mode==="list" && root.folder==="drafts"; Layout.fillWidth:true; Layout.fillHeight:true; clip:true; model:drafts
            ScrollBar.vertical:ScrollBar { policy:ScrollBar.AsNeeded }
            delegate:Item {
                id:draftRow
                required property string id
                required property string to
                required property string subject
                required property string body
                required property string state
                width:draftList.width; height:215*root.u
                Column { anchors.fill:parent; anchors.topMargin:30*root.u; spacing:12*root.u
                    Text { text:draftRow.state==="draft" ? "To: "+(draftRow.to || "Add a recipient") : "Check delivery in Gmail"; width:parent.width; elide:Text.ElideRight; font.family:"Noto Sans"; font.pixelSize:25*root.u; color:root.muted; textFormat:Text.PlainText }
                    Text { text:draftRow.subject || "Untitled draft"; width:parent.width; elide:Text.ElideRight; font.family:"EB Garamond"; font.pixelSize:44*root.u; color:root.ink; textFormat:Text.PlainText }
                    Text { text:draftRow.body; width:parent.width; maximumLineCount:1; elide:Text.ElideRight; font.family:"Noto Sans"; font.pixelSize:27*root.u; color:root.muted; textFormat:Text.PlainText }
                }
                Rectangle { anchors.bottom:parent.bottom; width:parent.width; height:root.u; color:root.rule }
                MouseArea { anchors.fill:parent; enabled:!root.busy; onClicked:root.openDraft(draftRow.id) }
            }
            Text { anchors.centerIn:parent; visible:drafts.count===0 && !root.busy; text:"Room for your next letter."; font.family:"EB Garamond"; font.pixelSize:45*root.u; color:root.muted }
        }

        RowLayout {
            visible:root.mode==="thread"; Layout.fillWidth:true; spacing:12*root.u
            InkButton { objectName:"replyButton"; text:"Reply"; selected:true; enabled:!root.busy; onClicked:root.newDraft(true) }
            InkButton { text:root.conversation.starred ? "Unstar" : "Star"; enabled:!root.busy; onClicked:root.modify(root.conversation.starred ? "unstar" : "star") }
            InkButton { text:root.conversation.unread ? "Mark read" : "Mark unread"; enabled:!root.busy; onClicked:root.modify(root.conversation.unread ? "read" : "unread") }
            InkButton { text:"Archive"; enabled:!root.busy; onClicked:root.modify("archive") }
            Item { Layout.fillWidth:true }
            InkButton { text:"A−"; onClicked:root.readingScale=Math.max(0.8,root.readingScale-0.1) }
            InkButton { text:"A+"; onClicked:root.readingScale=Math.min(1.8,root.readingScale+0.1) }
        }
        Flickable {
            id:reader; objectName:"reader"; visible:root.mode==="thread"; Layout.fillWidth:true; Layout.fillHeight:true; contentWidth:width; contentHeight:letter.implicitHeight+50*root.u; clip:true; boundsBehavior:Flickable.StopAtBounds
            ScrollBar.vertical:ScrollBar { policy:ScrollBar.AsNeeded }
            Column {
                id:letter; width:reader.width-24*root.u; spacing:34*root.u
                Text { width:parent.width; text:root.conversation.subject || ""; wrapMode:Text.Wrap; font.family:"EB Garamond"; font.pixelSize:60*root.u; color:root.ink; textFormat:Text.PlainText }
                Repeater {
                    model:root.conversation.messages || []
                    Column {
                        required property var modelData
                        width:letter.width; spacing:22*root.u
                        Rectangle { width:parent.width; height:root.u; color:root.rule }
                        Text { width:parent.width; text:modelData.from; wrapMode:Text.Wrap; font.family:"Noto Sans"; font.pixelSize:28*root.u; font.weight:Font.DemiBold; color:root.ink; textFormat:Text.PlainText }
                        Text { width:parent.width; text:root.formatDate(modelData.date)+"\nTo: "+modelData.to; wrapMode:Text.Wrap; font.family:"Noto Sans"; font.pixelSize:24*root.u; color:root.muted; textFormat:Text.PlainText }
                        Text { width:parent.width; text:modelData.body; wrapMode:Text.Wrap; font.family:"EB Garamond"; font.pixelSize:37*root.u*root.readingScale; lineHeight:1.25; color:root.ink; textFormat:Text.PlainText }
                        Text { width:parent.width; visible:(modelData.attachments || []).length>0; text:"Attachments: "+(modelData.attachments || []).join(", ")+"\nOpen attachments in Gmail on your phone or computer."; wrapMode:Text.Wrap; font.family:"Noto Sans"; font.pixelSize:25*root.u; color:root.muted; textFormat:Text.PlainText }
                    }
                }
            }
        }

        RowLayout {
            visible:root.mode==="compose"; Layout.fillWidth:true; spacing:14*root.u
            Text { objectName:"saveStatus"; text:root.saving ? "Saving…" : root.dirty ? "Unsaved changes" : "Saved on this tablet"; Layout.fillWidth:true; font.family:"Noto Sans"; font.pixelSize:24*root.u; color:root.muted }
            InkButton { objectName:"keyboardButton"; text:"Keyboard"; selected:root.keyboardRequested; enabled:!root.busy; onClicked:root.toggleKeyboard() }
            InkButton { text:"Save"; enabled:root.editable && !root.saving; onClicked:root.saveDraft() }
            InkButton { objectName:"discardButton"; text:"Discard"; enabled:root.editable && !root.saving; onClicked:{root.hideKeyboard();root.deleteConfirmation=true;} }
            InkButton { objectName:"sendButton"; text:"Send"; selected:true; enabled:root.editable && !root.saving; onClicked:root.requestSend() }
        }
        Flickable {
            id:composer; visible:root.mode==="compose"; Layout.fillWidth:true; Layout.fillHeight:true; contentWidth:width; contentHeight:composeColumn.implicitHeight+30*root.u; clip:true; boundsBehavior:Flickable.StopAtBounds
            ScrollBar.vertical:ScrollBar { policy:ScrollBar.AsNeeded }
            function keepCursorVisible() {
                if (!root.inputTarget) return;
                var point=root.inputTarget.mapToItem(composeColumn, 0, root.inputTarget.cursorRectangle ? root.inputTarget.cursorRectangle.y : 0);
                if (point.y-contentY>height-120*root.u) contentY=Math.min(Math.max(0,contentHeight-height),point.y-height+150*root.u);
                if (point.y<contentY) contentY=Math.max(0,point.y-50*root.u);
            }
            onHeightChanged:Qt.callLater(keepCursorVisible)
            Column {
                id:composeColumn; width:composer.width-16*root.u; spacing:18*root.u
                Text { text:"TO"; font.family:"Noto Sans"; font.pixelSize:23*root.u; color:root.muted }
                MailField { id:draftTo; objectName:"draftTo"; width:parent.width; enabled:root.editable; placeholderText:"name@example.com"; inputMethodHints:Qt.ImhEmailCharactersOnly; onTextEdited:root.editDraft() }
                Text { text:"CC"; font.family:"Noto Sans"; font.pixelSize:23*root.u; color:root.muted }
                MailField { id:draftCC; objectName:"draftCC"; width:parent.width; enabled:root.editable; placeholderText:"Optional"; inputMethodHints:Qt.ImhEmailCharactersOnly; onTextEdited:root.editDraft() }
                Text { text:"SUBJECT"; font.family:"Noto Sans"; font.pixelSize:23*root.u; color:root.muted }
                MailField { id:draftSubject; objectName:"draftSubject"; width:parent.width; enabled:root.editable; placeholderText:"A few words"; onTextEdited:root.editDraft() }
                Rectangle { width:parent.width; height:root.u; color:root.rule }
                TextArea {
                    id:draftBody; objectName:"draftBody"; width:parent.width; height:Math.max(550*root.u,contentHeight+56*root.u)
                    enabled:!root.busy; readOnly:!root.editable; activeFocusOnPress:false; selectByMouse:true
                    font.family:"EB Garamond"; font.pixelSize:37*root.u; color:root.ink; wrapMode:TextEdit.Wrap; textFormat:TextEdit.PlainText; padding:14*root.u; placeholderText:"Take your time."
                    onPressed:{root.inputTarget=draftBody;forceActiveFocus();}
                    onTextChanged:root.editDraft()
                    onCursorRectangleChanged:if (activeFocus) Qt.callLater(composer.keepCursorVisible)
                    Keys.onPressed:function(event){event.accepted=root.handleKey(event);}
                    background:Rectangle {color:"white"}
                    DisplayMethodArea { anchors.fill:parent; displayMethod:root.keyboardRequested ? DisplayMethodArea.Animate : DisplayMethodArea.Content }
                }
            }
        }
    }

    Rectangle {
        anchors.fill:parent; visible:root.sendConfirmation || root.deleteConfirmation || root.privacyVisible; color:"#eeffffff"; z:20
        MouseArea {anchors.fill:parent}
        Rectangle {
            anchors.centerIn:parent; width:parent.width-2*root.gutter; height:confirmationContent.implicitHeight+96*root.u; color:"white"; border.color:root.ink; border.width:2*root.u; radius:12*root.u
            Column {
                id:confirmationContent; anchors.centerIn:parent; width:parent.width-96*root.u; spacing:30*root.u
                Text {width:parent.width; text:root.privacyVisible ? "Privacy & support" : root.sendConfirmation ? "Send this email?" : "Discard this draft?"; font.family:"EB Garamond"; font.pixelSize:60*root.u; color:root.ink; wrapMode:Text.Wrap}
                Text {width:parent.width; text:root.privacyVisible ? "reMail connects directly to Gmail to read, send, and organize the mail you choose. Sending always requires confirmation.\n\nMessages and drafts are cached on this tablet. Credentials are stored in a private, unencrypted tablet file; the Mac setup tool uses Keychain. reMail does not send mailbox data to a developer server, advertising service, or AI model.\n\nYou can revoke access in Google Account connections. Revoking access does not delete local cached copies.\n\nRead the full policy on your phone or computer:\nhttps://krupapc.dev/apps/remail/privacy/\n\nSupport: krupa@krupapc.dev" : root.sendConfirmation ? "To: "+draftTo.text+(draftCC.text ? "\nCc: "+draftCC.text : "")+"\n\n"+(draftSubject.text || "(No subject)") : "This removes the draft saved on this tablet."; font.family:"Noto Sans"; font.pixelSize:30*root.u; color:root.muted; wrapMode:Text.Wrap; textFormat:Text.PlainText}
                Row {
                    spacing:20*root.u
                    InkButton {objectName:"cancelConfirmation"; text:root.privacyVisible ? "Done" : "Keep editing"; onClicked:{root.sendConfirmation=false;root.deleteConfirmation=false;root.privacyVisible=false;}}
                    InkButton {objectName:"confirmAction"; visible:!root.privacyVisible; text:root.sendConfirmation ? "Send email" : "Discard draft"; selected:true; onClicked:{if(root.sendConfirmation)root.confirmSend();else root.deleteDraft();}}
                }
            }
        }
    }
}
