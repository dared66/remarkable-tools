#!/usr/bin/env python3
"""Build reFinder for the desktop preview and the reMarkable 2 AppLoad launcher."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import sys
import tempfile
from xml.etree import ElementTree as ET

SOURCE = Path(__file__).resolve().parents[1]
PROJECT = SOURCE.parent
OUT = PROJECT / "downloads"


def main():
    OUT.mkdir(exist_ok=True)
    go = os.environ.get("REFINDER_GO") or shutil.which("go") or str(PROJECT / ".state/toolchain/go/bin/go")
    rcc = os.environ.get("REFINDER_RCC") or shutil.which("pyside6-rcc") or str(Path(sys.executable).parent / "pyside6-rcc")
    if not Path(rcc).is_file():
        rcc = str(PROJECT / ".state/ui-preview-env/bin/pyside6-rcc")
    if not Path(go).is_file() or not Path(rcc).is_file():
        raise SystemExit("Install Go and finder/requirements-build.txt first. See finder/README.md.")
    env = dict(os.environ, GOCACHE=str(PROJECT / ".state/go-cache"), CGO_ENABLED="0", GOTOOLCHAIN="local")
    build = [go, "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w"]
    subprocess.run(build + ["-o", OUT / "finder-preview", "."], cwd=SOURCE, env=env, check=True)
    archive = OUT / "RemarkFinder-rm2.tar.gz"
    with tempfile.TemporaryDirectory(dir=OUT) as work:
        base = Path(work)
        package = base / "RemarkFinder"
        (package / "backend").mkdir(parents=True)
        subprocess.run(build + ["-o", package / "backend/entry", "."], cwd=SOURCE,
                       env=dict(env, GOOS="linux", GOARCH="arm", GOARM="7"), check=True)
        resources = ET.Element("RCC")
        parent = ET.SubElement(resources, "qresource", prefix="/ui")
        ET.SubElement(parent, "file", alias="Finder.qml").text = str(SOURCE / "ui/Finder.qml")
        ET.ElementTree(resources).write(base / "resources.qrc")
        subprocess.run([rcc, "--binary", base / "resources.qrc", "-o", package / "resources.rcc"], check=True)
        # Preserve the original ID so upgrades replace Finder in place.
        manifest = dict(id="RemarkFinder", name="reFinder", loadsBackend=True, entry="/ui/Finder.qml",
                        canHaveMultipleFrontends=False, supportsScaling=True, aspectRatio="original")
        (package / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        goroot = subprocess.check_output([go, "env", "GOROOT"], env=env, text=True).strip()
        shutil.copyfile(Path(goroot) / "LICENSE", package / "Go-LICENSE.txt")
        shutil.copyfile(SOURCE / "ui/icon.png", package / "icon.png")
        shutil.copyfile(SOURCE / "LICENSE", package / "COPYING")
        sums = [f"{hashlib.sha256(file.read_bytes()).hexdigest()}  {file.relative_to(package)}"
                for file in sorted(package.rglob("*")) if file.is_file()]
        (package / "SHA256SUMS").write_text("\n".join(sums) + "\n")
        temp_archive = base / archive.name
        with tarfile.open(temp_archive, "w:gz") as tar:
            tar.add(package, arcname=package.name)
        temp_archive.replace(archive)
    print(archive)


if __name__ == "__main__":
    main()
