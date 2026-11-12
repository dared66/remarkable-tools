#!/usr/bin/env python3
"""Build a self-contained ARMv7 AppLoad package and a native preview backend."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import sys

PROJECT = Path(__file__).resolve().parents[2]
SOURCE = PROJECT / "mail"
OUTPUT = PROJECT / "downloads/mail-package"
PACKAGE = OUTPUT / "RemarkMail"


def main():
    go = os.environ.get("REMARKMAIL_GO") or shutil.which("go") or str(PROJECT / ".state/toolchain/go/bin/go")
    rcc = Path(os.environ.get("REMARKMAIL_RCC") or shutil.which("pyside6-rcc") or (Path(sys.executable).parent / "pyside6-rcc" if (Path(sys.executable).parent / "pyside6-rcc").is_file() else None) or PROJECT / ".state/ui-preview-env/bin/pyside6-rcc")
    qt_python = os.environ.get("REMARKMAIL_PYTHON") or sys.executable
    if not Path(go).is_file() or not rcc.is_file():
        raise SystemExit("Install Go and the repository's PySide6 preview environment first. See mail/README.md.")
    (PACKAGE / "backend").mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOCACHE=os.environ.get("GOCACHE", str(PROJECT / ".state/go-cache")), GOMODCACHE=os.environ.get("GOMODCACHE", str(PROJECT / ".state/go-modules")), CGO_ENABLED="0")
    args = [go, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags=-s -w"]
    subprocess.run(args + ["-o", str(PROJECT / "downloads/remarkmail-preview"), "./cmd/remarkmail"], cwd=SOURCE, env=env, check=True)
    subprocess.run(args + ["-o", str(PACKAGE / "backend/entry"), "./cmd/remarkmail"], cwd=SOURCE, env=dict(env, GOOS="linux", GOARCH="arm", GOARM="7"), check=True)
    qrc = OUTPUT / "resources.qrc"
    qrc.write_text('<RCC><qresource prefix="/ui"><file alias="Mail.qml">' + str(SOURCE / "ui/Mail.qml") + '</file></qresource></RCC>')
    subprocess.run([str(rcc), "--binary", str(qrc), "-o", str(PACKAGE / "resources.rcc")], check=True)
    manifest = {"id":"RemarkMail", "name":"reMail", "loadsBackend":True, "entry":"/ui/Mail.qml", "canHaveMultipleFrontends":False, "supportsScaling":True, "aspectRatio":"original"}
    (PACKAGE / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    # A simple envelope icon, drawn as native vector geometry before PNG export.
    # Icon rendering happens in the same PySide environment as resource compilation.
    subprocess.run([qt_python, str(Path(__file__).with_name("icon.py")), str(PACKAGE / "icon.png")], check=True, env=dict(os.environ, QT_QPA_PLATFORM="offscreen"))
    module_dirs = subprocess.check_output([go, "list", "-m", "-f", "{{.Dir}}", "golang.org/x/net", "golang.org/x/text"], cwd=SOURCE, env=env, text=True).splitlines()
    goroot = subprocess.check_output([go, "env", "GOROOT"], env=env, text=True).strip()
    notices = [("Go standard library/runtime", Path(goroot) / "LICENSE")]
    notices.extend((Path(directory).name, Path(directory) / "LICENSE") for directory in module_dirs)
    (PACKAGE / "THIRD_PARTY_NOTICES.txt").write_text("\n\n".join(name + "\n\n" + path.read_text() for name, path in notices))
    (PACKAGE / "COPYING").write_text((PROJECT / "LICENSE").read_text())
    expected = {"icon.png", "manifest.json", "resources.rcc", "backend/entry", "THIRD_PARTY_NOTICES.txt", "COPYING"}
    actual = {str(p.relative_to(PACKAGE)) for p in PACKAGE.rglob("*") if p.is_file()}
    if actual != expected:
        raise SystemExit("Unexpected files in the package directory. Move them out before packaging.")
    archive = PROJECT / "downloads/RemarkMail-rm2.tar.gz"
    with tarfile.open(archive, "w:gz") as tf:
        for name in sorted(expected):
            tf.add(PACKAGE / name, arcname="RemarkMail/" + name)
    hashes = {str(p.relative_to(PACKAGE)): hashlib.sha256(p.read_bytes()).hexdigest() for p in PACKAGE.rglob("*") if p.is_file()}
    (OUTPUT / "SHA256SUMS.json").write_text(json.dumps(hashes, indent=2) + "\n")
    print(json.dumps({"package":str(archive), "sha256":hashlib.sha256(archive.read_bytes()).hexdigest(), "files":hashes}, indent=2))


if __name__ == "__main__":
    main()
