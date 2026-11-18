package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func retarget(nn, nd string) {
	ini := filepath.Join(AVDHome, nn+".ini")
	_ = IniSet(ini, "path", nd)
	IniSetIf(ini, "path.rel", "avd/"+nn+".avd")
	c := filepath.Join(nd, "config.ini")
	IniSetIf(c, "AvdId", nn)
	IniSetIf(c, "avd.ini.displayname", nn)
	_ = os.RemoveAll(filepath.Join(nd, "snapshots", "default_boot"))
	locks, _ := filepath.Glob(filepath.Join(nd, "*.lock"))
	for _, l := range locks {
		_ = os.RemoveAll(l)
	}
}

func RenameAVD(o, nn string) (bool, string) {
	if !ValidName(nn) {
		return false, "invalid name (letters, digits, . _ - only)"
	}
	if ActiveNow(o) {
		return false, o + " is running — stop it first"
	}
	if exists(filepath.Join(AVDHome, nn+".ini")) || exists(filepath.Join(AVDHome, nn+".avd")) {
		return false, fmt.Sprintf("'%s' already exists", nn)
	}
	od := AVDDir(o)
	nd := filepath.Join(filepath.Dir(od), nn+".avd")
	if err := os.Rename(od, nd); err != nil {
		return false, "rename failed: " + err.Error()
	}
	if err := os.Rename(filepath.Join(AVDHome, o+".ini"), filepath.Join(AVDHome, nn+".ini")); err != nil {
		return false, "rename failed: " + err.Error()
	}
	retarget(nn, nd)
	_ = os.Rename(filepath.Join(CacheDir, o+".log"), filepath.Join(CacheDir, nn+".log"))
	return true, fmt.Sprintf("renamed %s → %s (quick-boot snapshot discarded; first boot will be cold)", o, nn)
}

func CloneAVD(s, nn string, withSnaps bool) (bool, string) {
	if !ValidName(nn) {
		return false, "invalid name (letters, digits, . _ - only)"
	}
	if ActiveNow(s) {
		return false, s + " is running — stop it first"
	}
	if exists(filepath.Join(AVDHome, nn+".ini")) || exists(filepath.Join(AVDHome, nn+".avd")) {
		return false, fmt.Sprintf("'%s' already exists", nn)
	}
	od := AVDDir(s)
	nd := filepath.Join(filepath.Dir(od), nn+".avd")
	if r := Run([]string{"cp", "-a", "--reflink=auto", od, nd}, Opt{Timeout: time.Hour}); !r.OK() {
		_ = os.RemoveAll(nd)
		return false, "copy failed (disk full?)"
	}
	if b, err := os.ReadFile(filepath.Join(AVDHome, s+".ini")); err == nil {
		_ = os.WriteFile(filepath.Join(AVDHome, nn+".ini"), b, 0o644)
	}
	if !withSnaps {
		_ = os.RemoveAll(filepath.Join(nd, "snapshots"))
	}
	retarget(nn, nd)
	return true, fmt.Sprintf("cloned %s → %s", s, nn)
}

func WipeUserdataFiles(name string, deep bool) {
	d := AVDDir(name)
	_ = os.RemoveAll(filepath.Join(d, "snapshots"))
	for _, pat := range []string{"userdata-qemu.img*", "cache.img*", "encryptionkey.img"} {
		m, _ := filepath.Glob(filepath.Join(d, pat))
		for _, f := range m {
			_ = os.Remove(f)
		}
	}
	if deep {
		_ = os.Remove(filepath.Join(d, "userdata.img"))
	}
	locks, _ := filepath.Glob(filepath.Join(d, "*.lock"))
	for _, l := range locks {
		_ = os.RemoveAll(l)
	}
}

// SetImage points an AVD at another installed system image and wipes its data.
func SetImage(name, rel string) (bool, string) {
	rel = strings.TrimRight(rel, "/")
	c := CfgPath(name)
	if !exists(c) {
		return false, name + ": config.ini not found"
	}
	if !exists(filepath.Join(SDK, rel)) {
		return false, "image not installed: " + rel
	}
	p := strings.Split(rel, "/")
	if len(p) < 4 {
		return false, "bad image path"
	}
	api, tag, abi := p[1], p[2], p[3]
	_ = CopyFile(c, c+".emu-bak")
	_ = IniSet(c, "image.sysdir.1", rel+"/")
	_ = IniSet(c, "tag.id", tag)
	_ = IniSet(c, "tag.display", TagDisplay(tag))
	_ = IniSet(c, "abi.type", abi)
	arch := map[string]string{"arm64-v8a": "arm64", "armeabi-v7a": "arm"}[abi]
	if arch == "" {
		arch = abi
	}
	_ = IniSet(c, "hw.cpu.arch", arch)
	_ = IniSet(c, "PlayStore.enabled", fmt.Sprint(strings.Contains(tag, "playstore")))
	if n := strings.TrimPrefix(api, "android-"); regexp.MustCompile(`^\d+$`).MatchString(n) {
		IniSetIf(c, "image.androidVersion.api", n)
	}
	IniSetIf(filepath.Join(AVDHome, name+".ini"), "target", api)
	WipeUserdataFiles(name, true)
	return true, fmt.Sprintf("%s now uses %s · %s · %s  (user data wiped; backup: config.ini.emu-bak)", name, api, tag, abi)
}

// ───────────────────────────── hardware fields ─────────────────────────────

type HWField struct{ Label, Key, Kind string }

var HWFields = []HWField{
	{"RAM (MB)", "hw.ramSize", "int"}, {"VM heap (MB)", "vm.heapSize", "int"}, {"CPU cores", "hw.cpu.ncore", "int"},
	{"Internal storage (e.g. 8G)", "disk.dataPartition.size", "size:wipe"}, {"SD card (e.g. 512M)", "sdcard.size", "size"},
	{"GPU mode", "hw.gpu.mode", "enum:auto,host,swiftshader_indirect,angle_indirect,guest"},
	{"Host keyboard", "hw.keyboard", "enum:yes,no"}, {"Screen density (dpi)", "hw.lcd.density", "int"},
	{"Screen width (px)", "hw.lcd.width", "int"}, {"Screen height (px)", "hw.lcd.height", "int"},
	{"Back camera", "hw.camera.back", "enum:none,emulated,webcam0"}, {"Front camera", "hw.camera.front", "enum:none,emulated,webcam0"},
	{"Always cold boot", "fastboot.forceColdBoot", "enum:yes,no"}, {"Show device frame", "showDeviceFrame", "enum:yes,no"},
}

var sizeRE = regexp.MustCompile(`^\d+[KMG]?$`)

// ValidateHW checks a user-entered value for a field kind.
func ValidateHW(kind, val string) string {
	switch {
	case kind == "int" && !regexp.MustCompile(`^\d+$`).MatchString(val):
		return "expected a number"
	case strings.HasPrefix(kind, "size") && !sizeRE.MatchString(val):
		return "expected e.g. 4096M or 8G"
	}
	return ""
}

// ApplyHW writes one config.ini key (backing the file up first).
func ApplyHW(name, key, val string) {
	c := CfgPath(name)
	if !exists(c + ".emu-bak") {
		_ = CopyFile(c, c+".emu-bak")
	}
	_ = IniSet(c, key, val)
	switch key {
	case "hw.gpu.mode":
		_ = IniSet(c, "hw.gpu.enabled", "yes")
	case "sdcard.size":
		_ = IniSet(c, "hw.sdCard", "yes")
	}
	_ = os.RemoveAll(filepath.Join(AVDDir(name), "snapshots", "default_boot"))
}

func DeleteAVD(name string) (bool, string) {
	p := AVDDir(name)
	if HaveAndroid {
		if !Run([]string{"android", "emulator", "remove", name}, Opt{}).OK() {
			Run([]string{"android", "emulator", "remove", "--force", name}, Opt{})
		}
	}
	_ = os.Remove(filepath.Join(AVDHome, name+".ini"))
	if p != "" && p != "/" {
		_ = os.RemoveAll(p)
	}
	_ = os.Remove(filepath.Join(CacheDir, name+".log"))
	if exists(filepath.Join(AVDHome, name+".ini")) || exists(p) {
		return false, "failed to delete " + name
	}
	return true, "deleted " + name
}
