#!/usr/bin/env python3
"""Exercise reFinder's QML with its real desktop backend and disposable tablet files."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")
os.environ.setdefault("QT_QUICK_BACKEND", "software")
os.environ.setdefault("QT_QUICK_CONTROLS_STYLE", "Basic")
from PySide6.QtCore import QObject, QPoint, Qt, QTimer, QUrl, Signal, Slot
from PySide6.QtGui import QFontDatabase, QGuiApplication
from PySide6.QtQml import QQmlEngine, QQmlExpression
from PySide6.QtQuick import QQuickView
from PySide6.QtTest import QTest

PROJECT = Path(__file__).resolve().parents[2]
UI = PROJECT / "finder/ui"
PDF = "d2f42efe-d8a4-4262-8f6c-c7843277c152"
FOLDER = "5c145444-443e-4c72-890a-9f26a481a64d"
PREVIEW = PROJECT / ".state/finder-preview"
PREVIEW.mkdir(parents=True, exist_ok=True)

class Backend(QObject):
    reply = Signal(str)

    def __init__(self, device):
        super().__init__()
        self.process = subprocess.Popen([PROJECT / "downloads/finder-preview", "--stdio", "--device-root", device],
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)

    @Slot(str)
    def request(self, body):
        self.process.stdin.write(body + "\n")
        self.process.stdin.flush()
        response = self.process.stdout.readline()
        if not response:
            raise RuntimeError("reFinder exited")
        QTimer.singleShot(0, lambda: self.reply.emit(response))


def run(work):
    device = work / "device"
    store = device / "home/root/.local/share/remarkable/xochitl"
    store.mkdir(parents=True)
    def put(name, data):
        (store / name).write_text(json.dumps(data))
    def record(id_, name, kind, parent=""):
        put(id_ + ".metadata", dict(visibleName=name, type=kind, parent=parent, lastModified="1794528000000"))
    record(FOLDER, "Kitchen", "CollectionType")
    record(PDF, "Pastry formulas", "DocumentType", FOLDER)
    for i, name in enumerate(["Baking notes", "Field notebook", "Project sketches", "Reading list"]):
        record(f"00000000-0000-0000-0000-{i+1:012x}", name, "DocumentType")
    put(PDF + ".content", {"fileType":"pdf", "pageCount":2, "pages":["page-one"]})
    (store / (PDF + ".pdf")).write_bytes(b"%PDF-1.7")
    thumbs = store / (PDF + ".thumbnails")
    thumbs.mkdir()
    shutil.copyfile(UI / "icon.png", thumbs / "page-one.png")
    vault = device / "home/root/reMarkdown/Culinary/Recipes"
    vault.mkdir(parents=True)
    (vault / "Lemon cake.md").write_text("Lemon zest and olive oil.\n")
    app = QGuiApplication(sys.argv)
    for f in (PROJECT / ".state/preview-fonts").glob("*.ttf"):
        QFontDatabase.addApplicationFont(str(f))
    # Supply just the launcher transport; load the shipped UI without rewriting it.
    module = work / "net/asivery/AppLoad"
    module.mkdir(parents=True)
    (module / "qmldir").write_text("module net.asivery.AppLoad\nAppLoad 1.0 AppLoad.qml\n")
    (module / "AppLoad.qml").write_text("""import QtQuick
Item {
    id: self
    property string applicationID
    signal messageReceived(int type, string contents)
    function sendMessage(type, contents) { PreviewBackend.request(contents) }
    Connections { target: PreviewBackend; function onReply(contents) { self.messageReceived(101, contents) } }
}""")
    backend = Backend(device)
    try:
        view = QQuickView()
        view.setResizeMode(QQuickView.SizeRootObjectToView)
        view.engine().addImportPath(str(work))
        view.engine().rootContext().setContextProperty("PreviewBackend", backend)
        view.setSource(QUrl.fromLocalFile(str(UI / "Finder.qml")))
        if view.status() != QQuickView.Ready:
            raise AssertionError([e.toString() for e in view.errors()])
        root = view.rootObject()
        view.resize(1404,1872)
        view.show()
        def wait():
            QTest.qWait(90)
            assert not root.property("busy")
        def js(code):
            exp = QQmlExpression(QQmlEngine.contextForObject(root), root, code)
            val, _ = exp.evaluate()
            assert not exp.hasError(), exp.error().toString()
            wait()
            return val
        def tap(item):
            p = item.mapToScene(item.boundingRect().center())
            QTest.mouseClick(view, Qt.LeftButton, Qt.NoModifier, QPoint(round(p.x()),round(p.y())))
            wait()
        def button(name):
            item = root.findChild(QObject, name)
            assert item.isVisible() and item.isEnabled(), name
            tap(item)
        def row(i):
            cell = js(f"rows.itemAtIndex({i})")
            assert cell, i
            tap(cell)
        wait()
        assert js("list.total") == 5
        shot = view.grabWindow()
        assert not shot.isNull()
        assert shot.scaled(700,936,Qt.KeepAspectRatio,Qt.SmoothTransformation).save(str(PREVIEW / "reFinder.png"))
        row(0)
        assert js("list.title") == "Kitchen"
        row(0)
        assert js("document.pages") == 2 and js("document.images") == 1
        for _ in range(30):
            if js("previewImage.status === Image.Ready"):
                break
        else:
            raise AssertionError("Cached preview did not load")
        button("exportBtn")
        assert "Copied to" in root.property("notice")
        button("exportBtn")
        assert "already exists" in root.property("notice")
        button("backBtn")
        button("backBtn")
        assert js("list.title") == "My files"
        button("searchBtn")
        root.findChild(QObject,"queryField").setProperty("text","pastry")
        button("findBtn")
        assert js("list.total") == 1 and js("list.items[0].location") == "My files / Kitchen"
        button("vaultBtn")
        button("searchBtn")
        root.findChild(QObject,"queryField").setProperty("text","lemon")
        button("findBtn")
        row(0)
        assert js("document.text") == "Lemon zest and olive oil.\n"
        button("backBtn")
        assert js("query") == "lemon"
        button("deviceBtn")
        row(0)
        assert js("list.location") == "/home"
        button("backBtn")
        assert js("list.title") == "Device files"
        print("Passed touch navigation, recursive search, text and PNG previews, export, collision handling and back navigation.")
        view.close()
    finally:
        backend.process.stdin.close()
        backend.process.wait(timeout=5)


if __name__ == "__main__":
    with tempfile.TemporaryDirectory() as tmp:
        run(Path(tmp))
