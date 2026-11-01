#!/bin/sh
# TUI smoke test: drives the real binary in a pty against the fake phone (scripts/demo.sh) and asserts on the rendered screen.
#   make smoke        (needs: python3 + pyte; builds bin/android-tui first)
# Key facts: focus the device pane with `2`, enter the tab pane with `l`, `}`/`{` = next/prev mode, `]`/`[` = next/prev tab in the mode (Ctrl-N/B inside text-input tabs).
set -u
HERE=$(cd "$(dirname "$0")" && pwd); ROOT=$HERE/../..
BIN=${ATUI:-$ROOT/bin/android-tui}; [ -x "$BIN" ] || { echo "build first: make build" >&2; exit 2; }
python3 -c 'import pyte' 2>/dev/null || { echo "need python3 + pyte (pip install pyte)" >&2; exit 2; }
W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
printf '#!/bin/sh\n[ -e "%s/intro" ] || export ATUI_NO_INTRO=1\nexport DEMO_RESET=1 EMU="%s"\nexec sh "%s/scripts/demo.sh"\n' "$W" "$BIN" "$ROOT" > "$W/run.sh"; chmod +x "$W/run.sh"
DEFCOLS=${COLS:-150}; export ATUI_BIN=$W/run.sh ROWS=${ROWS:-40}
mkdir -p "$W/nobin" "$W/nohome"
RUN_COLS=; RUN_BIN=
FAIL=0
run() { # name  steps  expected-regex...
  name=$1; steps=$2; shift 2
  COLS=${RUN_COLS:-$DEFCOLS} ATUI_BIN=${RUN_BIN:-$W/run.sh} timeout 120 python3 "$HERE/pty_drive.py" "$steps" > "$W/out.txt" 2>&1
  good=1
  for re in "$@"; do
    grep -aqE -- "$re" "$W/out.txt" || { echo "FAIL  $name — missing /$re/"; good=0; FAIL=1; }
  done
  [ $good = 1 ] && echo "ok    $name"
}
START='2;w:2;l;w:0.5'
# Modes: Device [Overview Apps Files Screen Shell] · Optimize [Tweaks Debloat Procs Control Journal] · Inspect [Logcat Props System Diff Tools]
# `}` / `{` switch mode (resuming the last tab used there), `]` / `[` cycle tabs inside the mode.
run "starts, lists AVDs and device"        'w:2;snap:a'                              'Pixel_9' 'Pixel_7'
run "overview: profile section"            "${START};snap:a"                         'Profile' 'Class +handheld' 'revertible'
run "modes: title + tab strips"            "${START};snap:a;};w:1;snap:b;};w:1;snap:c" 'Device │ Optimize │ Inspect │ Connect' 'Device › Overview' 'Optimize › Tweaks' 'Overview │ Apps │ Files │ Screen │ Shell' 'Tweaks │ Debloat │ Procs │ Control │ Journal' 'Logcat │ Props │ System │ Diff │ Tools'
run "modes: resume last tab"               "${START};];w:1.5;};w:1;{;w:1.5;snap:a"   'scope user +sort name'
touch "$W/intro"; run "first run: quick-start overlay"   'w:2;snap:a;<ESC>;w:0.5;snap:b'                'Quick start' 'SAFE BY DESIGN'; rm -f "$W/intro"
run "palette: fuzzy subsequence"           "${START};<C-p>;w:0.8;shzk;w:0.8;snap:a"  'Start Shizuku \(adb\)'
run "zoom: Z fills the screen, Esc leaves"  "${START};Z;w:1;snap:a;<ESC>;w:1;snap:b"     'ZOOM · Z or Esc to exit' '\[1\] Emulators'
run "offline: stopped emulator guidance"   "1;w:2;l;w:0.5;];w:1;snap:a"                       'Pixel_9 is stopped' 'cold boot'
RUN_COLS=80; run "narrow 80 cols: lists → main → back" 'w:2;snap:a;2;w:1;l;w:1.5;};w:1;snap:b;<ESC>;w:1;snap:c' '\[1\] Emulators' 'Esc: lists' 'Optimize › Tweaks'; RUN_COLS=
run "revert ALL needs the typed word"      "${START};];w:1.5;d;w:3;};w:1;];];];];w:1.5;R;w:0.8;snap:a;<ENTER>;w:0.5;snap:b;revert;w:0.5;<ENTER>;w:3;snap:c" 'Type REVERT to confirm' 'reverted 1 change'
out=$(HOME="$W/nohome" SDK_ROOT= ANDROID_HOME= ANDROID_SDK_ROOT= PATH="$W/nobin" "$BIN" 2>&1)
if echo "$out" | grep -aq "'adb' not found" && echo "$out" | grep -aq "brew install android-platform-tools"; then echo "ok    no adb: preflight message with install hints"; else echo "FAIL  no adb: preflight message"; echo "$out" | head -3; FAIL=1; fi
run "dashboard: next steps + hotkey"    "${START};snap:a;b;w:2;snap:b"            'Next steps' 'Only packages that exist on this device'
run "apps: disable is journalled"          "${START};];w:1.5;d;w:3;snap:a"           'Disable: com.example.notes +\(journalled'
run "journal: entry, revert, reverted"     "${START};];w:1.5;d;w:3;};w:1;];];];];w:1.5;snap:a;<ENTER>;w:1;<ENTER>;w:1.5;snap:b" 'installed → disabled' '↩ Disable' '0 revertible'
run "undo: Ctrl-Z reverts last change" "${START};];w:1.5;d;w:3;<C-z>;w:1;snap:a;<ENTER>;w:3;snap:b" 'Undo the last change' 'undid 1 change'
run "logcat: preset picker + filter"       "${START};};};w:2;P;w:0.6;<DOWN>;<DOWN>;<ENTER>;w:1;snap:a" 'preset ANRs'
run "tweaks: apply, baseline, journal"     "${START};};w:2.5;<ENTER>;w:1;<ENTER>;w:3;};w:1;];];];w:1;snap:diff;{;w:1;];];];];w:1.5;snap:j" 'auto +[0-9]+ keys' 'Animation speed +setting: global/window_animation_scale +1.0 → 0'
run "control: toggle is journalled"        "${START};};w:1;];];];w:1;<DOWN>;w:0.3;<ENTER>;w:2;snap:a" 'Auto-rotate: on.*journalled'
run "palette: Start Shizuku (fake: absent)" "${START};<C-p>;w:0.8;Shizuku;w:0.8;<ENTER>;w:3;snap:a" 'moe.shizuku.privileged.api\) is not installed'
run "debloat: disable + snapshot"          "${START};};w:1;];w:2;<DOWN>;w:0.3;d;w:1;<ENTER>;w:3;snap:a" 'disable: 1 ok, 0 failed +snapshot:'
run "procs: sort"                          "${START};};w:1;];];w:2;o;w:1;snap:a" 'sorted by memory'
run "screen: capture screenshot"           "${START};];];];w:1.5;c;w:3;snap:a" 'saved .*Pixel_7-[0-9-]+\.png'
run "diff: take snapshot"                  "${START};};};];];];w:1.5;<ENTER>;w:1;<ENTER>;w:2.5;snap:a" 'Saved snapshots \(1\)'
# --- CLI against the fake phone
cli() { name=$1; re=$2; shift 2; out=$(DEMO_RESET= ATUI="$BIN" FAKE=$W/fake HOME=$W/home sh "$ROOT/scripts/demo.sh" "$@" 2>&1); if echo "$out" | grep -aqE -- "$re"; then echo "ok    $name"; else echo "FAIL  $name — missing /$re/"; echo "$out" | tail -3; FAIL=1; fi; }
cli "cli: ls"                              'Pixel_7' ls
cli "cli: uidump -o file"                  'saved .*ui\.txt' uidump TEST-DEVICE-001 -o "$W/ui.txt"
[ -s "$W/ui.txt" ] && echo "ok    cli: uidump file written" || { echo "FAIL  cli: uidump file empty"; FAIL=1; }
cli "cli: shot -o file"                    'saved .*s\.png' shot TEST-DEVICE-001 -o "$W/s.png"
[ "$(head -c 4 "$W/s.png" 2>/dev/null | tail -c 3)" = "PNG" ] && echo "ok    cli: shot is a PNG" || { echo "FAIL  cli: shot not a PNG"; FAIL=1; }
DEMO_RESET= ATUI="$BIN" FAKE=$W/fake HOME=$W/home sh "$ROOT/scripts/demo.sh" shot TEST-DEVICE-001 --stdout 2>/dev/null | head -c 4 | tail -c 3 | grep -q PNG && echo "ok    cli: shot --stdout" || { echo "FAIL  cli: shot --stdout"; FAIL=1; }
[ $FAIL = 0 ] && echo "smoke: all passed" || { echo "smoke: FAILURES"; exit 1; }
