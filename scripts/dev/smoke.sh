#!/bin/sh
# TUI smoke test: drives the real binary in a pty against the fake phone (scripts/demo.sh) and asserts on the rendered screen.
#   make smoke        (needs: python3 + pyte; builds bin/android-tui first)
# Key facts: focus the device pane with `2`, enter the tab pane with `l`, `}`/`{` = next/prev mode, `]`/`[` = next/prev tab in the mode (Ctrl-N/B inside text-input tabs).
set -u
HERE=$(cd "$(dirname "$0")" && pwd); ROOT=$HERE/../..
BIN=${ATUI:-$ROOT/bin/android-tui}; [ -x "$BIN" ] || { echo "build first: make build" >&2; exit 2; }
python3 -c 'import pyte' 2>/dev/null || { echo "need python3 + pyte (pip install pyte)" >&2; exit 2; }
# The fake emulator detaches like a real one and would outlive a run (making the next run see the AVD as running).
stop_fakes() { pkill -f "scripts/fake/bin/[e]mulator" 2>/dev/null || true; }
stop_fakes
W=$(mktemp -d); trap 'stop_fakes; rm -rf "$W"' EXIT
printf '#!/bin/sh\n[ -e "%s/intro" ] || export ATUI_NO_INTRO=1\nunset ANDROID_TUI_ROOTAVD\nexport DEMO_RESET=1 EMU="%s"\nexec sh "%s/scripts/demo.sh"\n' "$W" "$BIN" "$ROOT" > "$W/run.sh"; chmod +x "$W/run.sh"
DEFCOLS=${COLS:-150}; DEFROWS=${ROWS:-40}; export ATUI_BIN=$W/run.sh
mkdir -p "$W/nobin" "$W/nohome"
RUN_COLS=; RUN_ROWS=; RUN_BIN=
printf '#!/bin/sh\n[ -e "%s/intro" ] || export ATUI_NO_INTRO=1\nexport ANDROID_TUI_ROOTAVD="%s/scripts/fake/rootAVD.sh"\nexport DEMO_RESET=1 EMU="%s"\nexec sh "%s/scripts/demo.sh"\n' "$W" "$ROOT" "$BIN" "$ROOT" > "$W/run-rootavd.sh"; chmod +x "$W/run-rootavd.sh"
FAIL=0
run() { # name  steps  expected-regex...
  name=$1; steps=$2; shift 2
  [ -z "${SMOKE_ONLY:-}" ] || echo "$name" | grep -qE "$SMOKE_ONLY" || return 0   # SMOKE_ONLY='regex' runs just the matching scenarios
  COLS=${RUN_COLS:-$DEFCOLS} ROWS=${RUN_ROWS:-$DEFROWS} ATUI_BIN=${RUN_BIN:-$W/run.sh} timeout 120 python3 "$HERE/pty_drive.py" "$steps" > "$W/out.txt" 2>&1
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
run "diff: take snapshot"                  "${START};};};];];];w:1.5;<ENTER>;w:1;<ENTER>;w:2.5;snap:a" 'Saved settings snapshots \(1\)'
# --- mouse + keyboard: context menus, double-click, wheel, buttons (see internal/ui/input.go)
run "ctx: right-click an app, click an item"  "${START};];w:1.5;rclick@com.example.notes;w:0.8;snap:a;click@Disable;w:3;snap:b" 'Pull APK to computer' 'Mark / unmark all' 'Disable: com.example.notes +\(journalled'
run "ctx: '.' opens it, a letter picks an item" "${START};];w:1.5;.;w:0.8;snap:a;d;w:3;snap:b" 'Force stop' 'Disable: com.example.notes +\(journalled'
run "ctx: Ctrl-O works inside the Shell tab"   "${START};];];];];w:1;<C-o>;w:0.8;snap:a" 'Actions palette…' 'Zoom main panel'
run "ctx: emulator list right-click"           "1;w:2;rclick@Pixel_9|L;w:0.8;snap:a" 'Start headless' 'Factory reset…' 'Edit hardware…'
run "ctx: confirm dialog has clickable buttons" "${START};];w:1.5;rclick@com.example.notes;w:0.8;click@Clear data;w:1;snap:a" '\[ Yes  y \]' '\[ No  n \]'
run "mouse: palette — double-click runs an action" "${START};<C-p>;w:0.8;shizuku;w:0.8;dclick@Start Shizuku;w:3;snap:a" 'moe.shizuku.privileged.api\) is not installed'
run "mouse: palette — wheel moves 3 rows a notch"  "${START};<C-p>;w:0.8;snap:a;wheeldown@Actions;w:0.4;wheeldown@Actions;w:0.5;snap:b" ' 1/[0-9]+ ─' ' 7/[0-9]+ ─'
run "mouse: double-click a connection opens it" "2;w:2;dclick@Pixel_7|L;w:1;b;w:2;snap:a" 'Only packages that exist on this device'
run "mouse: double-click a log line shows it"   "${START};};};w:2;dclick@message number 6;w:1;snap:a" 'Logcat line'
RUN_ROWS=18; run "scroll: Overview keys + wheel"  "${START};w:3;snap:a;<PGDN>;w:0.4;snap:b;<END>;w:0.4;snap:c;<HOME>;w:0.3;wheeldown:75,10;w:0.4;snap:d" '↓ [0-9]+ more' '↑ [0-9]+ line\(s\) above'; RUN_ROWS=
# a right-click on every one of the 15 mode tabs (reached by clicking modes and tabs) must open a menu
steps="${START}"
add() { steps="$steps;click@$1;w:1.2;rclick:75,10;w:0.6;snap:t$2;<ESC>;w:0.3"; }
steps="$steps;w:1;rclick:75,10;w:0.6;snap:t0;<ESC>;w:0.3"
add Apps 1; add Files 2; add Screen 3; add Shell 4
add Optimize 5; add Debloat 6; add Procs 7; add Control 8; add Journal 9
add Inspect 10; add Logcat 11; add Props 12; add System 13; add Diff 14; add Tools 15
timeout 200 python3 "$HERE/pty_drive.py" "$steps" > "$W/out.txt" 2>&1
n=$(grep -ac 'Actions palette…' "$W/out.txt")
if [ "$n" -ge 15 ]; then echo "ok    ctx: right-click menu on all 15 tabs ($n menus)"; else echo "FAIL  ctx: right-click menu on all 15 tabs — only $n menus opened"; FAIL=1; fi
# --- rootAVD (optional power-user feature): unconfigured help, typed confirmation, hand-over of the terminal, verified result
run "rootAVD: not configured → explains the setup"  "1;w:2;l;w:0.5;<C-p>;w:0.8;rootavd;w:0.8;<ENTER>;w:1;snap:a" 'power users' 'ROOTAVD=' 'does not bundle'
RUN_BIN=$W/run-rootavd.sh; run "rootAVD: root, confirm, run, verify, protect" "1;w:2;s;w:7;<C-p>;w:0.8;root avd;w:0.8;<ENTER>;w:1;snap:a;<ENTER>;w:0.5;snap:b;root;w:0.4;<ENTER>;w:3;snap:c;<ENTER>;w:2.5;snap:d;<C-p>;w:0.8;restore stock;w:0.8;<ENTER>;w:1.5;snap:e" 'Type ROOT to confirm' 'Also affected' '\[fake rootAVD\] patching' 'ramdisk patched' 'stop Pixel_9 first'; RUN_BIN=; stop_fakes
# --- CLI against the fake phone
cli() { name=$1; re=$2; shift 2; out=$(DEMO_RESET= ANDROID_TUI_ROOTAVD= ATUI="$BIN" FAKE=$W/fake HOME=$W/home sh "$ROOT/scripts/demo.sh" "$@" 2>&1); if echo "$out" | grep -aqE -- "$re"; then echo "ok    $name"; else echo "FAIL  $name — missing /$re/"; echo "$out" | tail -3; FAIL=1; fi; }
cli "cli: rootavd --status (nothing backed up yet)" 'status:   no backup yet' rootavd Pixel_9 --status
cli "cli: rootavd refuses a stopped emulator"    'start Pixel_9 first' rootavd Pixel_9 --script "$ROOT/scripts/fake/rootAVD.sh"
cli "cli: rootavd explains an unconfigured setup" 'rootAVD is not configured' rootavd Pixel_9
cli "cli: ls"                              'Pixel_7' ls
cli "cli: uidump -o file"                  'saved .*ui\.txt' uidump TEST-DEVICE-001 -o "$W/ui.txt"
[ -s "$W/ui.txt" ] && echo "ok    cli: uidump file written" || { echo "FAIL  cli: uidump file empty"; FAIL=1; }
cli "cli: shot -o file"                    'saved .*s\.png' shot TEST-DEVICE-001 -o "$W/s.png"
[ "$(head -c 4 "$W/s.png" 2>/dev/null | tail -c 3)" = "PNG" ] && echo "ok    cli: shot is a PNG" || { echo "FAIL  cli: shot not a PNG"; FAIL=1; }
DEMO_RESET= ATUI="$BIN" FAKE=$W/fake HOME=$W/home sh "$ROOT/scripts/demo.sh" shot TEST-DEVICE-001 --stdout 2>/dev/null | head -c 4 | tail -c 3 | grep -q PNG && echo "ok    cli: shot --stdout" || { echo "FAIL  cli: shot --stdout"; FAIL=1; }
[ $FAIL = 0 ] && echo "smoke: all passed" || { echo "smoke: FAILURES"; exit 1; }
