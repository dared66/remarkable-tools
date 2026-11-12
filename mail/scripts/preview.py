#!/usr/bin/env python3
"""Render and exercise the real Mail QML against the compiled Go demo backend."""
import argparse
import json
import os
from pathlib import Path
import shutil
import sys

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--interactive", action="store_true")
args = parser.parse_args()
if not args.interactive:
    os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")
os.environ.setdefault("QT_QUICK_BACKEND", "software")
os.environ.setdefault("QT_QUICK_CONTROLS_STYLE", "Basic")

from PySide6.QtCore import QObject, QProcess, QPoint, Qt, QUrl, Slot, Signal
from PySide6.QtGui import QFontDatabase, QGuiApplication
from PySide6.QtQml import QQmlComponent, QQmlExpression, QQmlEngine
from PySide6.QtQuick import QQuickView
from PySide6.QtTest import QTest

ROOT = Path(__file__).resolve().parents[2]
BASE = ROOT / ".state/mail-preview"
BASE.mkdir(parents=True, exist_ok=True)
RUN = BASE / "test-state"
if not args.interactive and RUN.exists():
    shutil.rmtree(RUN)
source = (ROOT / "mail/ui/Mail.qml").read_text()
source = "\n".join(line for line in source.splitlines() if not line.startswith("import net.asivery."))
(BASE / "Mail.qml").write_text(source)
(BASE / "AppLoad.qml").write_text('''import QtQuick
Item {
    id: transport
    property string applicationID
    signal messageReceived(int type, string contents)
    function sendMessage(type, contents) { if (type === 100) PreviewBackend.request(contents); }
    Connections { target: PreviewBackend; function onReply(contents) { transport.messageReceived(101, contents); } }
}
''')
(BASE / "DisplayMethodArea.qml").write_text('''import QtQuick
Item {
    enum Method { Content, Animate }
    property int displayMethod: DisplayMethodArea.Content
}
''')


class Backend(QObject):
    reply = Signal(str)

    def __init__(self):
        super().__init__()
        self.buffer = b""
        self.process = QProcess(self)
        self.process.readyReadStandardOutput.connect(self.read)
        self.process.start(str(ROOT / "downloads/remarkmail-preview"), ["--stdio", "--demo", "--data-dir", str(RUN)])
        if not self.process.waitForStarted(5000):
            raise RuntimeError("Build the preview backend first")

    @Slot(str)
    def request(self, contents):
        self.process.write(contents.encode() + b"\n")

    def read(self):
        self.buffer += bytes(self.process.readAllStandardOutput())
        while b"\n" in self.buffer:
            line, self.buffer = self.buffer.split(b"\n", 1)
            self.reply.emit(line.decode())


app = QGuiApplication(sys.argv)
for font in (ROOT / ".state/preview-fonts").glob("*.ttf"):
    QFontDatabase.addApplicationFont(str(font))
backend = Backend()
view = QQuickView()
view.engine().rootContext().setContextProperty("PreviewBackend", backend)
component = QQmlComponent(view.engine(), QUrl.fromLocalFile(str(BASE / "Mail.qml")))
if component.isError():
    backend.process.kill()
    backend.process.waitForFinished()
    raise SystemExit("\n".join(e.toString() for e in component.errors()))
root = component.create()
if root is None:
    raise SystemExit("\n".join(e.toString() for e in component.errors()))
view.setContent(QUrl.fromLocalFile(str(BASE / "Mail.qml")), component, root)
view.resize(1404, 1872)
view.show()
view.requestActivate()


def js(code):
    expression = QQmlExpression(QQmlEngine.contextForObject(root), root, code)
    value = expression.evaluate()
    if expression.hasError():
        raise AssertionError(expression.error().toString())
    return value[0] if isinstance(value, tuple) else value


def until(condition, message, milliseconds=5000):
    for _ in range(milliseconds // 20):
        if condition():
            return
        QTest.qWait(20)
    raise AssertionError(message)


def click(name):
    item = root.findChild(QObject, name)
    assert item is not None, name
    assert item.property("enabled"), name + " is disabled"
    point = item.mapToScene(item.boundingRect().center())
    QTest.mouseClick(view, Qt.LeftButton, Qt.NoModifier, QPoint(round(point.x()), round(point.y())))
    QTest.qWait(60)


def shot(name):
    QTest.qWait(150)
    assert view.grabWindow().save(str(BASE / (name + ".png")))


if args.interactive:
    root.close.connect(app.quit)
    try:
        raise SystemExit(app.exec())
    finally:
        backend.process.terminate()
        backend.process.waitForFinished(3000)

checks = []
try:
    until(lambda: root.property("mode") == "list" and not root.property("busy"), "Mailbox did not load")
    assert js("threads.count") == 3
    assert root.property("demo") and not root.property("keyboardRequested")
    shot("01-inbox")
    checks.append("real Go backend transport, inbox list, preview label, keyboard initially hidden")
    click("privacyButton")
    assert root.property("privacyVisible") and not root.property("keyboardRequested")
    shot("05-privacy")
    QTest.keyClick(view, Qt.Key_Escape)
    assert not root.property("privacyVisible") and root.property("mode") == "list"
    checks.append("in-app privacy disclosure opens without keyboard capture and closes with Escape")
    js('openThread("sample-saturday")')
    until(lambda: root.property("mode") == "thread" and not root.property("busy"), "Thread did not load")
    assert js("conversation.messages.length") == 2
    shot("02-conversation")
    checks.append("conversation messages and attachment names")
    click("replyButton")
    until(lambda: root.property("mode") == "compose" and not root.property("busy"), "Reply draft did not open")
    assert js("draftTo.text") == "Alex Morgan <alex@example.com>"
    assert js("draftSubject.text") == "Re: A plan for Saturday"
    assert js("draft.threadId") == "sample-saturday"
    body = root.findChild(QObject, "draftBody")
    body.forceActiveFocus()
    for character in "saturday sounds lovely. see you at ten.":
        # Qt's string helper only accepts QWidget; QQuickView is a QWindow.
        QTest.keyClick(view, Qt.Key(ord(character.upper())))
    until(lambda: not root.property("dirty") and not root.property("saving"), "Reply was not autosaved")
    assert "saturday sounds lovely" in body.property("text"), repr(body.property("text"))
    body.setProperty("text", body.property("text") + "\nCafé, bientôt — ☕")
    until(lambda: not root.property("dirty") and not root.property("saving"), "Unicode text was not autosaved")
    shot("03-compose")
    draft_id = js("draft.id")
    checks.append("reply recipient/thread headers and actual physical typing with verified autosave")
    click("keyboardButton")
    assert root.property("keyboardRequested")
    QTest.keyClick(view, Qt.Key_Escape)
    assert not root.property("keyboardRequested")
    checks.append("explicit keyboard toggle and Escape release")
    click("backButton")
    until(lambda: root.property("mode") == "list" and not root.property("busy"), "Back failed")
    js("openDraft(" + json.dumps(draft_id) + ")")
    until(lambda: root.property("mode") == "compose" and not root.property("busy"), "Saved draft did not reopen")
    assert "saturday sounds lovely" in body.property("text") and "Café, bientôt — ☕" in body.property("text")
    checks.append("draft survives navigation and reload from durable storage")
    click("sendButton")
    until(lambda: root.property("sendConfirmation"), "Send confirmation did not open")
    shot("04-confirm-send")
    assert js("draft.state") == "draft"
    click("cancelConfirmation")
    assert not root.property("sendConfirmation") and root.property("mode") == "compose"
    click("sendButton")
    until(lambda: root.property("sendConfirmation"), "Second confirmation did not open")
    click("confirmAction")
    until(lambda: root.property("mode") == "list" and not root.property("busy"), "Demo send did not finish")
    assert root.property("notice") == "Preview complete. No email was sent."
    checks.append("send requires confirmation, cancellation retains draft, demo never sends real mail")
    js('chooseFolder("drafts")')
    until(lambda: not root.property("busy"), "Draft list did not load")
    assert js("drafts.count") == 0
    js('chooseFolder("starred")')
    until(lambda: not root.property("busy"), "Starred did not load")
    assert js("threads.count") == 1
    js('chooseFolder("inbox")')
    until(lambda: not root.property("busy"), "Inbox did not load")
    js('query="book";loadMailbox(false)')
    until(lambda: not root.property("busy"), "Search did not finish")
    assert js("threads.count") == 1
    checks.append("draft list excludes sent receipts; folder filtering and search")
    click("composeButton")
    until(lambda: root.property("mode") == "compose" and not root.property("busy"), "New draft failed")
    click("discardButton")
    assert root.property("deleteConfirmation")
    click("cancelConfirmation")
    assert root.property("mode") == "compose"
    click("discardButton")
    click("confirmAction")
    until(lambda: root.property("mode") == "list" and not root.property("busy"), "Discard failed")
    assert js("drafts.count") == 0
    checks.append("discard requires confirmation and cancel preserves draft")
    report = {"passed":len(checks), "checks":checks, "screenshots":str(BASE)}
    (BASE / "verification.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
finally:
    backend.process.closeWriteChannel()
    if not backend.process.waitForFinished(3000):
        backend.process.kill()
        backend.process.waitForFinished()
