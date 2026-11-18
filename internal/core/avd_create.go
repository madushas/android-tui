package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Official struct{ Pkg, Label, API string }

// OfficialImages lists downloadable images for this host via the android CLI.
func OfficialImages() ([]Official, bool) {
	r := Run([]string{"android", "sdk", "list", "--all", "system-images/*"}, Opt{Timeout: 2 * time.Minute})
	if !r.OK() {
		return nil, false
	}
	have := map[string]bool{}
	for _, i := range ScanImages() {
		have[i.Rel] = true
	}
	abis := map[string]bool{}
	for _, a := range HostABIs() {
		abis[a] = true
	}
	var out []Official
	for _, l := range strings.Split(r.Out, "\n") {
		f := strings.Fields(l)
		if len(f) == 0 || !strings.HasPrefix(f[0], "system-images/") {
			continue
		}
		p := strings.Split(f[0], "/")
		if len(p) >= 4 && abis[p[3]] && !have[f[0]] {
			out = append(out, Official{f[0], TagLabel(p[2]), strings.TrimPrefix(p[1], "android-")})
		}
	}
	return out, true
}

// ───────────────────────────── AVD creation / flags ─────────────────────────────

type HWProfile struct {
	Name         string
	W, H, D, RAM int
	Cores        int
	Dev          string
}

var HWProfiles = []HWProfile{
	{"Phone  (1080x2400 · 420dpi)", 1080, 2400, 420, 2048, 4, "pixel_6"},
	{"Small phone  (720x1280 · 320dpi)", 720, 1280, 320, 1536, 2, "Nexus 5"},
	{"Tablet  (2560x1600 · 320dpi)", 2560, 1600, 320, 3072, 4, "pixel_c"},
	{"TV 1080p  (1920x1080 · 213dpi)", 1920, 1080, 213, 2048, 4, "tv_1080p"},
}

// CreateAVDManual writes an AVD directly — works for custom ROM tags `android emulator create` doesn't know.
func CreateAVDManual(name, rel string, p HWProfile) (bool, string) {
	if !ValidName(name) {
		return false, "invalid name (letters, digits, . _ - only)"
	}
	if exists(filepath.Join(AVDHome, name+".ini")) || exists(filepath.Join(AVDHome, name+".avd")) {
		return false, fmt.Sprintf("'%s' already exists", name)
	}
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) < 4 || !exists(filepath.Join(SDK, rel)) {
		return false, "image not found: " + rel
	}
	api, tag, abi := strings.TrimPrefix(parts[1], "android-"), parts[2], parts[3]
	d := filepath.Join(AVDHome, name+".avd")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return false, err.Error()
	}
	arch := map[string]string{"arm64-v8a": "arm64", "armeabi-v7a": "arm"}[abi]
	if arch == "" {
		arch = abi
	}
	orient := "portrait"
	if p.W > p.H {
		orient = "landscape"
	}
	b2s := func(b bool) string { return map[bool]string{true: "yes", false: "no"}[b] }
	cfg := [][2]string{
		{"AvdId", name}, {"PlayStore.enabled", fmt.Sprint(strings.Contains(tag, "playstore"))}, {"abi.type", abi},
		{"avd.ini.displayname", name}, {"avd.ini.encoding", "UTF-8"}, {"disk.dataPartition.size", "6G"},
		{"fastboot.chosenSnapshotFile", ""}, {"fastboot.forceChosenSnapshotBoot", "no"}, {"fastboot.forceColdBoot", "no"},
		{"fastboot.forceFastBoot", "yes"}, {"hw.accelerometer", "yes"}, {"hw.arc", "false"}, {"hw.audioInput", "yes"},
		{"hw.battery", "yes"}, {"hw.camera.back", "emulated"}, {"hw.camera.front", "emulated"}, {"hw.cpu.arch", arch},
		{"hw.cpu.ncore", fmt.Sprint(p.Cores)}, {"hw.device.manufacturer", "Generic"}, {"hw.device.name", p.Dev},
		{"hw.dPad", b2s(strings.Contains(tag, "tv"))}, {"hw.gps", "yes"}, {"hw.gpu.enabled", "yes"}, {"hw.gpu.mode", "auto"},
		{"hw.initialOrientation", orient}, {"hw.keyboard", "yes"}, {"hw.lcd.density", fmt.Sprint(p.D)}, {"hw.lcd.height", fmt.Sprint(p.H)},
		{"hw.lcd.width", fmt.Sprint(p.W)}, {"hw.mainKeys", "no"}, {"hw.ramSize", fmt.Sprint(p.RAM)}, {"hw.sdCard", "yes"},
		{"hw.sensors.orientation", "yes"}, {"hw.sensors.proximity", "yes"}, {"hw.trackBall", "no"},
		{"image.sysdir.1", strings.Trim(rel, "/") + "/"}, {"runtime.network.latency", "none"}, {"runtime.network.speed", "full"},
		{"sdcard.size", "512M"}, {"showDeviceFrame", "no"}, {"tag.display", TagDisplay(tag)}, {"tag.id", tag}, {"vm.heapSize", "256"},
	}
	var sb strings.Builder
	for _, kv := range cfg {
		sb.WriteString(kv[0] + "=" + kv[1] + "\n")
	}
	if err := os.WriteFile(filepath.Join(d, "config.ini"), []byte(sb.String()), 0o644); err != nil {
		return false, err.Error()
	}
	ini := fmt.Sprintf("avd.ini.encoding=UTF-8\npath=%s\npath.rel=avd/%s.avd\ntarget=android-%s\n", d, name, api)
	if err := os.WriteFile(filepath.Join(AVDHome, name+".ini"), []byte(ini), 0o644); err != nil {
		return false, err.Error()
	}
	return true, fmt.Sprintf("created AVD %s from %s · API %s · %s", name, TagLabel(tag), api, abi)
}

// FlagPresets are one-tap emulator launch flags.
var FlagPresets = [][2]string{
	{"-gpu host", "Hardware GPU (host) — recommended for custom ROMs"},
	{"-feature -Vulkan", "Disable Vulkan (fixes many graphics crashes)"},
	{"-writable-system", "Writable /system (ROM work; then adb root + remount)"},
	{"-no-audio", "No audio"},
	{"-no-boot-anim", "Skip boot animation"},
	{"-no-snapshot", "Disable snapshots (always cold, never saves)"},
	{"-dns-server 1.1.1.1,8.8.8.8", "Custom DNS servers"},
	{"-netspeed lte", "Throttle network to LTE"},
	{"-camera-back webcam0", "Host webcam as back camera"},
	{"-memory 4096", "4 GB RAM override"},
}

// SetAVDFlags rewrites the AVD_FLAGS[name] line of the config file (backup kept once).
func SetAVDFlags(name, flags string) (bool, string) {
	flags = strings.Join(strings.Fields(flags), " ")
	if strings.ContainsAny(flags, "\"$`\\\n") {
		return false, `flags may not contain " $ ` + "`" + ` or backslashes`
	}
	MkdirPrivate(ConfDir)
	text := ConfigTemplate
	if b, err := os.ReadFile(ConfFile); err == nil {
		text = string(b)
		if !exists(ConfFile + ".emu-bak") {
			_ = WritePrivate(ConfFile+".emu-bak", b)
		}
	}
	pat := regexp.MustCompile(`^\s*AVD_FLAGS\[` + regexp.QuoteMeta(name) + `\]=`)
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if !pat.MatchString(l) {
			lines = append(lines, l)
		}
	}
	if flags != "" {
		lines = append(lines, fmt.Sprintf(`AVD_FLAGS[%s]="%s"`, name, flags))
		Cfg.AVDFlags[name] = flags
	} else {
		delete(Cfg.AVDFlags, name)
	}
	if err := WritePrivate(ConfFile, []byte(strings.Join(lines, "\n")+"\n")); err != nil {
		return false, err.Error()
	}
	if flags == "" {
		flags = "(none)"
	}
	return true, fmt.Sprintf("launch flags for %s: %s", name, flags)
}
