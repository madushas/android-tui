#!/bin/sh
# Stand-in for the third-party rootAVD script, for the FAKE demo/smoke environment only. It "patches" the ramdisk it is given
# (relative to the working directory, like the real script is invoked) and exercises the interactive hand-over.
#   ANDROID_TUI_ROOTAVD=$PWD/scripts/fake/rootAVD.sh make demo
[ -f "$1" ] || { echo "[fake rootAVD] ramdisk not found: $1"; exit 1; }
echo "[fake rootAVD] patching $1 (cwd=$PWD, ANDROID_HOME=$ANDROID_HOME)"
printf '%s' "-patched" >> "$1"
echo "[fake rootAVD] done — cold-boot the AVD"
