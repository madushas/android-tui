#!/bin/sh
# Map every APK in an extracted firmware tree to its REAL package id (read from the manifest, not guessed from the file name).
#   sh scripts/dev/ota-pkgnames.sh <extracted-root> > packages-real.txt
# Output, one line per APK:   package:<path relative to root>=<package id>
# Needs aapt2 (Android SDK build-tools) or aapt on PATH. Overlays (/overlay/) are kept but flagged "# overlay".
set -e
ROOT=${1:?usage: ota-pkgnames.sh <extracted-root>}
if command -v aapt2 >/dev/null 2>&1; then
  pkg() { aapt2 dump packagename "$1" 2>/dev/null | tail -n1; }
elif command -v aapt >/dev/null 2>&1; then
  pkg() { aapt dump badging "$1" 2>/dev/null | sed -n "s/^package: name='\([^']*\)'.*/\1/p" | head -n1; }
else
  echo "need aapt2 or aapt (Android SDK build-tools)" >&2; exit 1
fi
find "$ROOT" -type f -name '*.apk' | sort | while IFS= read -r f; do
  id=$(pkg "$f")
  rel=${f#"$ROOT"}
  case "$rel" in */overlay/*) note=" # overlay" ;; *) note="" ;; esac
  if [ -n "$id" ]; then echo "package:$rel=$id$note"; else echo "package:$rel=?  # manifest unreadable (split/encrypted?)" >&2; fi
done
