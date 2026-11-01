#!/bin/sh
# Collect READ-ONLY diagnostics from a connected Android device so android-tui's parsers, packs and
# capability rules can be tested against real data. Nothing is changed on the device.
# usage: sh scripts/collect-device-info.sh [serial] [--logcat]     → writes device-dump-<model>-<date>.tar.gz
# Review the folder before sharing: serials/MACs/IPs/e-mails are redacted, but on-screen text (uidump) and logcat may hold private info.
set -u
SER=""; LOGCAT=0
for a in "$@"; do case "$a" in --logcat) LOGCAT=1;; *) SER="$a";; esac; done
A="adb"; [ -n "$SER" ] && A="adb -s $SER"
$A get-state >/dev/null 2>&1 || { echo "no device (adb devices)"; exit 1; }
MODEL=$($A shell getprop ro.product.model | tr -d '\r' | tr ' /' '__'); D="device-dump-${MODEL:-unknown}-$(date +%Y%m%d)"
mkdir -p "$D"; sh_() { $A shell "$1" 2>&1 | tr -d '\r'; }
echo "collecting into $D …"
sh_ 'getprop' > "$D/getprop.txt"
sh_ 'pm list features' > "$D/features.txt"
sh_ 'pm list packages -f -i --show-versioncode -U' > "$D/packages.txt"
sh_ 'pm list packages -d' > "$D/packages-disabled.txt"
sh_ 'pm list packages -s' > "$D/packages-system.txt"
sh_ 'pm list packages -u' > "$D/packages-all.txt"
for ns in system secure global; do sh_ "settings list $ns" > "$D/settings-$ns.txt"; done
sh_ 'device_config list' > "$D/device_config.txt"
sh_ 'id; (timeout 3 su -c id 2>&1 | head -n1)' > "$D/privilege.txt"
sh_ 'wm size; wm density' > "$D/wm.txt"
sh_ 'cmd package query-activities --brief -a android.intent.action.MAIN -c android.intent.category.HOME' > "$D/home-activities.txt"
sh_ 'cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.HOME' > "$D/home-current.txt"
sh_ 'dumpsys deviceidle whitelist' > "$D/doze-whitelist.txt"
sh_ 'dumpsys battery' > "$D/battery.txt"
sh_ 'dumpsys activity | grep -i -E "freezer|phantom" | head -n 40' > "$D/freezer-phantom.txt"
sh_ 'dumpsys meminfo | head -n 500' > "$D/meminfo.txt"
sh_ 'top -b -n 1 -m 60' > "$D/top.txt"
sh_ 'ps -A -o PID,USER,RSS,NAME | head -n 80' > "$D/ps.txt"
sh_ 'cmd appops get com.android.settings | head -n 40' > "$D/appops-sample.txt"
sh_ 'am get-standby-bucket com.android.settings' > "$D/bucket-sample.txt"
sh_ 'cmd jobscheduler help | head -n 30' > "$D/jobscheduler-help.txt"
sh_ 'cmd package help | head -n 80' > "$D/cmd-package-help.txt"
sh_ 'sm help 2>&1 | head -n 30' > "$D/sm-help.txt"
sh_ 'ls /storage/emulated/0/Android/data/moe.shizuku.privileged.api 2>&1; ps -A | grep -c shizuku_server' > "$D/shizuku.txt"
# tweak keys android-tui touches (current values)
{ for k in activity_manager/max_phantom_processes activity_manager/use_freezer; do echo "device_config $k = $($A shell device_config get ${k%%/*} ${k##*/} | tr -d '\r')"; done
  for k in global/cached_apps_freezer global/settings_enable_monitor_phantom_procs global/window_animation_scale global/force_resizable_activities global/enable_freeform_support secure/back_gesture_inset_scale_left secure/match_content_frame_rate secure/match_content_dynamic_range global/hdr_conversion_mode system/sound_effects_enabled; do echo "settings $k = $($A shell settings get ${k%%/*} ${k##*/} | tr -d '\r')"; done; } > "$D/tweak-keys.txt"
$A shell 'uiautomator dump /data/local/tmp/uidump.xml >/dev/null 2>&1; cat /data/local/tmp/uidump.xml; rm -f /data/local/tmp/uidump.xml' 2>&1 | tr -d '\r' > "$D/uidump.xml"
[ "$LOGCAT" = 1 ] && $A logcat -d -v threadtime 2>&1 | tail -n 400 > "$D/logcat-tail.txt"
# redact
SERIAL=$($A get-serialno | tr -d '\r')
for f in "$D"/*; do
  sed -i -E "s/${SERIAL}/<SERIAL>/g; s/([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}/<MAC>/g; s/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/<EMAIL>/g; s/\b([0-9]{1,3}\.){3}[0-9]{1,3}\b/<IP>/g" "$f"
done
sed -i -E '/serialno|imei|meid|\.mac|wifi\.address|bluetooth_address|android_id|device_name|ro\.boot\.(hardware\.sku|serial)|persist\.radio\.(imei|meid)|account/Id' "$D"/getprop.txt "$D"/settings-*.txt "$D"/device_config.txt
tar czf "$D.tar.gz" "$D" && echo "done → $D.tar.gz  (open the folder and skim it before sharing)"
