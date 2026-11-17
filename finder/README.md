# reFinder for reMarkable 2

reFinder is an offline AppLoad app for reMarkable 2 running firmware 3.29.1.0. Choose **My files** for native notebooks, PDFs and EPUBs, **Obsidian** for the vaults under `/home/root/reMarkdown`, or **Device files** for the filesystem starting at `/`.

Tap a folder to enter it. **Search** finds names in the current folder and its descendants. **Back** restores the preceding results and scroll position. Sort by name or modification time and toggle hidden files in the filesystem views. Device searches skip virtual system trees and do not follow directory symlinks.

Tap a document to see its location, type, modification date and available cached page images. reFinder displays PNG/JPEG images and up to 20 KiB of UTF-8 text, including Markdown. Notebook writing and full document reading use the tablet's native My files. **Copy original to Downloads** makes a copy of a PDF or EPUB in `/home/root/Downloads`. The copy contains the original source document. Handwriting and annotations belong to the native notebook. An existing destination file is preserved.

## Install

Connect the paired reMarkable 2 to the Mac by USB and wake it. Double-click `install.command` in this directory or run the following from the project root.

```sh
./finder/install.command
```

Open the tablet's **AppLoad → Reload → reFinder**. The USB installer expects your own SSH configuration at `.state/ssh_config` in the repository root, with a `remarkable-usb` host pointing to the tablet and a paired SSH key. Verify the tablet’s host key before installation; this file and your keys stay private and are never included in a package. It verifies the package checksums and runs a document-list check with the ARM binary before installing. A previous reFinder version is preserved beside the AppLoad directory on update. Firmware and Xochitl continue running during installation.

For manual installation with an existing AppLoad setup, extract `downloads/RemarkFinder-rm2.tar.gz` and place the entire `RemarkFinder` directory under `/home/root/xovi/exthome/appload/`, preserving the executable bit on `backend/entry`.

## Build and check

Use Go 1.25 or later and Python 3 with PySide6. From the repository root, create the build environment:

```sh
python3 -m venv .state/ui-preview-env
.state/ui-preview-env/bin/python -m pip install -r finder/requirements-build.txt
```

The build finds Go and `pyside6-rcc` on `PATH`, beside the Python interpreter, or in `.state/toolchain/go` and `.state/ui-preview-env`. `REFINDER_GO` and `REFINDER_RCC` can select explicit tool paths.

```sh
.state/ui-preview-env/bin/python finder/scripts/build.py
(cd finder && go test ./...)
.state/ui-preview-env/bin/python finder/scripts/preview.py
```

The build produces `downloads/RemarkFinder-rm2.tar.gz` and a host-native backend. The preview drives the QML through touch events using disposable notebooks and vault files.

The launcher name and app heading are **reFinder**. The internal AppLoad ID and package directory remain `RemarkFinder` for in-place upgrades from Finder. Source and packages use GPL-3.0-or-later; see [LICENSE](LICENSE).
