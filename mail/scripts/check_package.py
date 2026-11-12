#!/usr/bin/env python3
"""Verify the installation archive, ARM executable, and embedded frontend."""
import hashlib
import json
from pathlib import Path
import struct
import tarfile

from PySide6.QtCore import QFile, QIODevice, QResource

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = ROOT / "downloads/mail-package/RemarkMail"
hashes = json.loads((PACKAGE.parent / "SHA256SUMS.json").read_text())
expected = {"icon.png", "manifest.json", "resources.rcc", "backend/entry", "THIRD_PARTY_NOTICES.txt", "COPYING"}
assert set(hashes) == expected
with tarfile.open(ROOT / "downloads/RemarkMail-rm2.tar.gz") as archive:
    assert {m.name for m in archive.getmembers()} == {"RemarkMail/" + name for name in expected}
    for name in expected:
        data = (PACKAGE / name).read_bytes()
        assert hashlib.sha256(data).hexdigest() == hashes[name]
        assert archive.extractfile("RemarkMail/" + name).read() == data
    assert archive.getmember("RemarkMail/backend/entry").mode & 0o111
binary = (PACKAGE / "backend/entry").read_bytes()
assert binary[:4] == b"\x7fELF" and binary[4:6] == bytes((1, 1))
assert struct.unpack_from("<H", binary, 18)[0] == 40  # EM_ARM
assert QResource.registerResource(str(PACKAGE / "resources.rcc"))
frontend = QFile(":/ui/Mail.qml")
assert frontend.open(QIODevice.ReadOnly)
assert bytes(frontend.readAll()) == (ROOT / "mail/ui/Mail.qml").read_bytes()
manifest = json.loads((PACKAGE / "manifest.json").read_text())
assert manifest["id"] == "RemarkMail" and manifest["entry"] == "/ui/Mail.qml"
report = {"archive_hashes":True, "expected_files_only":True, "arm32_little_endian":True,
    "backend_executable":True, "embedded_frontend_matches_source":True, "manifest":True}
report_path = ROOT / ".state/mail-preview/package-verification.json"
report_path.parent.mkdir(parents=True, exist_ok=True)
report_path.write_text(json.dumps(report, indent=2) + "\n")
print(json.dumps(report, indent=2))
