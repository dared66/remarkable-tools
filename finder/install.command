#!/bin/sh
# Install the locally built reFinder package using this project's paired USB host.
set -eu
cd "$(dirname "$0")/.."
archive=downloads/RemarkFinder-rm2.tar.gz
test -s "$archive" || { echo "Build reFinder first with python3 finder/scripts/build.py."; exit 1; }
ssh -F .state/ssh_config -o ConnectTimeout=5 remarkable-usb '
set -eu
test "$(uname -m)" = armv7l || { echo "This package requires reMarkable 2 ARMv7."; exit 1; }
apps=/home/root/xovi/exthome/appload
test -d "$apps" || { echo "AppLoad was not found."; exit 1; }
staging=$(mktemp -d "$apps/../.finder-install.XXXXXX")
trap '\''rmdir "$staging" 2>/dev/null || true'\'' EXIT
tar -xzf - -C "$staging"
cd "$staging/RemarkFinder"
sha256sum -c SHA256SUMS
test -x backend/entry
# Exercise the ARM binary on the actual document store; do not print document names.
probe=$(printf "%s\n" '\''{"id":1,"action":"list","place":"library"}'\'' | ./backend/entry --stdio)
case "$probe" in *\"data\":*) ;; *) echo "reFinder could not read My files."; exit 1;; esac
cd "$apps"
exec 9>"$apps/../.finder-install.lock"
flock -n 9 || { echo "Another reFinder installation is in progress."; exit 1; }
if [ -d RemarkFinder ] && cmp -s RemarkFinder/SHA256SUMS "$staging/RemarkFinder/SHA256SUMS" && (cd RemarkFinder && sha256sum -c SHA256SUMS >/dev/null); then
    echo "reFinder is already installed and verified."
    rm -r "$staging/RemarkFinder"
    exit 0
fi
if [ -e RemarkFinder ]; then
    backup=$(mktemp -d "$apps/../.finder-previous.XXXXXX")
    mv RemarkFinder "$backup/"
    if ! mv "$staging/RemarkFinder" RemarkFinder; then
        mv "$backup/RemarkFinder" RemarkFinder
        exit 1
    fi
    echo "Previous reFinder preserved in $backup"
else
    mv "$staging/RemarkFinder" RemarkFinder
fi
echo "reFinder installed. Open AppLoad, tap Reload, then reFinder."
' < "$archive"
