#!/bin/sh
# Try android-tui with no phone and no SDK: a fake adb / emulator / android CLI plus two AVDs, a custom-ROM zip and a few files, settings/device_config/appops state.
#   sh scripts/demo.sh            → launches ./android-tui   (or: ATUI=/path/to/android-tui sh scripts/demo.sh ls)
set -e
HERE=$(cd "$(dirname "$0")" && pwd)
export FAKE=${FAKE:-/tmp/emu-demo}
[ -n "$DEMO_RESET" ] && rm -rf "$FAKE"
[ -f "$FAKE/props.tsv" ] && SEEDED=1
mkdir -p "$FAKE/avd" "$FAKE/home/Downloads" "$FAKE/sdcard/Download" "$FAKE/sdcard/DCIM" "$FAKE/sdk/platform-tools" \
  "$FAKE/sdk/system-images/android-36/google_apis_playstore/x86_64" "$FAKE/sdk/system-images/android-35/google_apis/x86_64"
for n in Pixel_9 Tablet_A; do
  mkdir -p "$FAKE/avd/$n.avd/snapshots/clean" "$FAKE/avd/$n.avd/snapshots/default_boot"
  echo x > "$FAKE/avd/$n.avd/snapshots/clean/snapshot.pb"; head -c 300000 /dev/zero > "$FAKE/avd/$n.avd/snapshots/default_boot/ram.bin"
  printf 'avd.ini.encoding=UTF-8\npath=%s/avd/%s.avd\npath.rel=avd/%s.avd\ntarget=android-36\n' "$FAKE" "$n" "$n" > "$FAKE/avd/$n.ini"
  printf 'AvdId=%s\navd.ini.displayname=%s\nhw.device.name=pixel_9\nhw.ramSize=2048\nhw.cpu.ncore=4\nhw.gpu.mode=auto\nhw.lcd.density=420\nhw.lcd.width=1080\nhw.lcd.height=2400\ndisk.dataPartition.size=6G\nimage.sysdir.1=system-images/android-36/google_apis_playstore/x86_64/\ntag.id=google_apis_playstore\nabi.type=x86_64\n' "$n" "$n" > "$FAKE/avd/$n.avd/config.ini"
done
echo "# demo file" > "$FAKE/sdcard/notes.txt"; head -c 2048 /dev/zero > "$FAKE/sdcard/Download/big.bin"; touch "$FAKE/home/Downloads/demo.apk"
# a tiny SDK-layout "custom ROM" zip you can import from the Images tab
T=$(mktemp -d); mkdir "$T/x86_64"
printf 'Pkg.Desc=Demo ROM\nAndroidVersion.ApiLevel=36\nSystemImage.Abi=x86_64\nSystemImage.TagId=demo\nSystemImage.TagDisplay=Demo ROM\n' > "$T/x86_64/source.properties"
head -c 2000000 /dev/zero > "$T/x86_64/system.img"; echo r > "$T/x86_64/ramdisk.img"; echo k > "$T/x86_64/kernel-ranchu"
(cd "$T" && python3 -c "import zipfile,os;z=zipfile.ZipFile('$FAKE/demo-rom.zip','w',zipfile.ZIP_DEFLATED);[z.write('x86_64/'+f) for f in os.listdir('x86_64')]")
# fake device tools. Guard: `ln -sfn` onto an existing real dir would create a link INSIDE it (recursive dev/dev/dev…)
if [ "$FAKE" != "$HERE/fake" ]; then
  [ -L "$FAKE/dev" ] || rm -rf "$FAKE/dev"
  ln -sfn "$HERE/fake/dev" "$FAKE/dev"
fi
[ -z "$SEEDED" ] && {
# state of the fake phone (a ZTE handheld on a Unisoc T606; DEMO_FORM=tv turns it into a Google TV box)
printf '%s\n' "${DEMO_FORM:-handheld}" > "$FAKE/form"
printf 'global/window_animation_scale\t1.0\nglobal/adb_enabled\t1\nglobal/stay_on_while_plugged_in\t0\nsecure/match_content_frame_rate\t1\nsystem/sound_effects_enabled\t1\nsystem/unisoc_fling_friction_slope\t0.6\n' > "$FAKE/settings.tsv"
printf 'ro.product.model\tBlade A73\nro.product.brand\tZTE\nro.product.manufacturer\tZTE\nro.product.device\tblade\nro.board.platform\tums9230\nro.soc.model\tT606\nro.hardware\tums9230_1h10\nro.build.version.release\t14\nro.build.version.sdk\t34\nro.build.display.id\tBLADE-14.0.1\nro.build.type\tuser\nro.build.fingerprint\tZTE/blade/blade:14/UP1A/1:user/release-keys\nro.build.characteristics\tdefault\npersist.sys.locale\ten-US\nro.kernel.qemu\t0\npersist.sys.apr.enabled\t1\npersist.sys.apr.mode\t0\npersist.sys.ztelog.enable\ttrue\npersist.unipnp.report.enable\t1\n' > "$FAKE/props.tsv"
printf 'com.example.notes\tinstalled\tu\norg.fake.browser\tinstalled\tu\ndev.wirity.demo\tinstalled\tu\ncom.android.settings\tinstalled\ts\ncom.android.printspooler\tinstalled\ts\ncom.google.android.feedback\tinstalled\ts\ncom.zte.zdm\tinstalled\ts\ncom.zte.aiengine\tinstalled\ts\ncom.google.android.marvin.talkback\tinstalled\ts\ncom.android.egg\tinstalled\ts\ncom.google.android.katniss\tinstalled\ts\ncom.google.android.backdrop\tinstalled\ts\n' > "$FAKE/pkgs.tsv"
: > "$FAKE/dc.tsv"
}
export XDG_DATA_HOME="$FAKE/home/.local/share"
export PATH="$HERE/fake/bin:$PATH" ANDROID_AVD_HOME="$FAKE/avd" ANDROID_SDK_ROOT="$FAKE/sdk" HOME="$FAKE/home" XDG_CACHE_HOME="$FAKE/home/.cache" XDG_CONFIG_HOME="$FAKE/home/.config"
echo "demo environment in $FAKE — import $FAKE/demo-rom.zip from the Images tab" >&2
exec "${ATUI:-${EMU:-$HERE/../android-tui}}" "$@"
