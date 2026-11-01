package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ───────────────────────────── SDK discovery ─────────────────────────────

func FindSDK() string {
	cands := []string{Cfg.SDKRoot, os.Getenv("ANDROID_SDK_ROOT"), os.Getenv("ANDROID_HOME")}
	if p, err := exec.LookPath("emulator"); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			cands = append(cands, filepath.Dir(filepath.Dir(r)))
		}
	}
	cands = append(cands, filepath.Join(Home, "Android", "Sdk"), "/opt/android-sdk", "/usr/lib/android-sdk")
	for _, c := range cands {
		if c == "" {
			continue
		}
		for _, d := range []string{"emulator", "system-images", "platforms", "platform-tools"} {
			if st, err := os.Stat(filepath.Join(c, d)); err == nil && st.IsDir() {
				return c
			}
		}
	}
	return ""
}

// Bootstrap resolves the SDK, extends PATH and reports whether adb exists.
func Bootstrap() bool {
	SDK = FindSDK()
	if SDK != "" {
		path := os.Getenv("PATH")
		for _, sub := range []string{"emulator", "platform-tools", "cmdline-tools/latest/bin"} {
			p := filepath.Join(SDK, sub)
			if st, err := os.Stat(p); err == nil && st.IsDir() && !strings.Contains(":"+path+":", ":"+p+":") {
				path += ":" + p
			}
		}
		if err := os.Setenv("PATH", path); err != nil {
			Logf("err", "cannot extend PATH with the SDK: %v", err)
		}
	}
	HaveAndroid = Which("android")
	return Which("adb")
}

// ───────────────────────────── AVD data ─────────────────────────────

func AVDDir(name string) string {
	if p := IniGet(filepath.Join(AVDHome, name+".ini"), "path"); p != "" {
		return p
	}
	return filepath.Join(AVDHome, name+".avd")
}

func CfgPath(name string) string { return filepath.Join(AVDDir(name), "config.ini") }

func ShortTag(t string) string {
	switch {
	case strings.HasPrefix(t, "google_apis_playstore"):
		return "play"
	case strings.HasPrefix(t, "google_apis"):
		return "apis"
	case t == "default" || strings.HasPrefix(t, "aosp"):
		return "aosp"
	}
	return t
}

// ImgInfo is what an AVD's config says about its system image.
type ImgInfo struct{ API, Tag, ABI, TagID string }

func (i ImgInfo) String() string {
	if i.API == "?" {
		return "unknown image"
	}
	return fmt.Sprintf("android-%s %s %s", i.API, i.Tag, i.ABI)
}

func (i ImgInfo) Short() string { return i.API + "·" + i.Tag }

func AvdInfo(name string) ImgInfo {
	parts := strings.Split(strings.Trim(IniGet(CfgPath(name), "image.sysdir.1"), "/"), "/")
	if len(parts) < 4 {
		return ImgInfo{"?", "?", "", ""}
	}
	return ImgInfo{strings.TrimPrefix(parts[1], "android-"), ShortTag(parts[2]), parts[3], parts[2]}
}

func ListNames() []string {
	m, _ := filepath.Glob(filepath.Join(AVDHome, "*.ini"))
	var out []string
	for _, f := range m {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".ini"))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func SnapList(name string) []string {
	ents, _ := os.ReadDir(filepath.Join(AVDDir(name), "snapshots"))
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func HostABIs() []string {
	switch m := hostMachine(); m {
	case "x86_64":
		return []string{"x86_64", "x86"}
	case "aarch64", "arm64":
		return []string{"arm64-v8a"}
	}
	return []string{"x86_64"}
}

func hostMachine() string {
	r := Run([]string{"uname", "-m"}, Quiet)
	return r.Text()
}

// ───────────────────────────── scanning ─────────────────────────────

// Emu is an AVD with its live state.
type Emu struct {
	Name   string
	Serial string
	State  string // stopped | booting | running | offline | unauthorized
	Info   ImgInfo
	RSS    int64 // KiB of host memory
	Custom bool
}

func (e Emu) Active() bool { return e.State != "stopped" }
func (e Emu) Online() bool { return e.State == "running" && e.Serial != "" }

// Conn is one adb transport that is not an AVD of ours (USB, Wi-Fi, TLS, unknown emulator, or saved).
type Conn struct {
	Serial    string
	State     string // device | offline | unauthorized | saved ...
	Model     string
	Transport string // usb | wifi | wifi (tls) | emulator | saved
	TID       string
	Addr      string // host:port for saved entries
}

func (c Conn) Online() bool { return c.State == "device" }
func (c Conn) Name() string {
	if c.Model != "" {
		return c.Model
	}
	return c.Serial
}

// Scan is one poll result.
type Scan struct {
	Emus  []Emu
	Conns []Conn
}

type Scanner struct {
	mu     sync.Mutex
	names  map[string]string // serial -> avd name
	booted map[string]bool
}

var Scn = &Scanner{names: map[string]string{}, booted: map[string]bool{}}

var psRE = regexp.MustCompile(`(?:^|[\s/])(?:emulator|qemu-system\S*)\s+(?:.*\s)?-avd\s+(\S+)(?:\s|$)`)

var psLineRE = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(.*)$`)

// PsScan -> {avd name: [nprocs, rss KiB]} for emulator processes on this host.
func PsScan() map[string][2]int64 {
	out := map[string][2]int64{}
	r := Run([]string{"ps", "-eo", "pid=,rss=,args="}, Quiet)
	for _, l := range strings.Split(r.Out, "\n") {
		lm := psLineRE.FindStringSubmatch(l)
		if lm == nil {
			continue
		}
		if m := psRE.FindStringSubmatch(lm[3]); m != nil {
			e := out[m[1]]
			e[0]++
			rss, _ := strconv.ParseInt(lm[2], 10, 64)
			e[1] += rss
			out[m[1]] = e
		}
	}
	return out
}

func (s *Scanner) Scan() Scan {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := ListNames()
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	devs := ParseDevicesL(Run([]string{"adb", "devices", "-l"}, Opt{Quiet: true, Timeout: 8 * time.Second}).Out)
	live := map[string]Device{}
	for _, d := range devs {
		live[d.Serial] = d
	}
	for k := range s.names {
		if _, ok := live[k]; !ok {
			delete(s.names, k)
		}
	}
	for k := range s.booted {
		if _, ok := live[k]; !ok {
			delete(s.booted, k)
		}
	}
	procs := PsScan()
	avdSerial := map[string]string{}
	var extra []Device
	for _, d := range devs {
		if strings.HasPrefix(d.Serial, "emulator-") {
			n := s.names[d.Serial]
			if n == "" && d.State == "device" {
				first := strings.TrimSpace(strings.SplitN(Adb(d.Serial, Opt{Quiet: true, Timeout: 2 * time.Second}, "emu", "avd", "name").Out, "\n", 2)[0])
				if nameSet[first] {
					n = first
					s.names[d.Serial] = n
				}
			}
			if nameSet[n] {
				avdSerial[n] = d.Serial
				continue
			}
		}
		extra = append(extra, d)
	}
	var sc Scan
	for _, n := range names {
		e := Emu{Name: n, Info: AvdInfo(n)}
		e.Custom = e.Info.TagID != "" && !IsOfficialTag(e.Info.TagID)
		if ser := avdSerial[n]; ser != "" {
			e.Serial = ser
			if st := live[ser].State; st != "device" {
				e.State = st
			} else {
				if !s.booted[ser] && Sh(ser, "getprop sys.boot_completed", Opt{Quiet: true, Timeout: 2 * time.Second}).Text() == "1" {
					s.booted[ser] = true
				}
				e.State = map[bool]string{true: "running", false: "booting"}[s.booted[ser]]
			}
		} else if _, ok := procs[n]; ok {
			e.State = "booting"
		} else {
			e.State = "stopped"
		}
		e.RSS = procs[n][1]
		sc.Emus = append(sc.Emus, e)
	}
	have := map[string]bool{}
	for _, d := range extra {
		sc.Conns = append(sc.Conns, Conn{Serial: d.Serial, State: d.State, Model: d.Model, Transport: d.Transport, TID: d.TID})
		have[d.Serial] = true
	}
	for _, c := range LoadConns() {
		if !have[c.Addr] {
			sc.Conns = append(sc.Conns, Conn{Serial: c.Addr, State: "saved", Model: c.Name, Transport: "saved", Addr: c.Addr})
		}
	}
	return sc
}

// Lookup returns the current state of one AVD (fresh scan).
func Lookup(name string) (Emu, bool) {
	for _, e := range Scn.Scan().Emus {
		if e.Name == name {
			return e, true
		}
	}
	return Emu{}, false
}

func ActiveNow(name string) bool { e, ok := Lookup(name); return ok && e.Active() }

// ───────────────────────────── start / stop ─────────────────────────────

func setupDisplay() []string {
	disp := os.Getenv("DISPLAY")
	if r := Run([]string{"pgrep", "-a", "Xwayland"}, Quiet); r.OK() {
		if m := regexp.MustCompile(` (:\d+)`).FindStringSubmatch(r.Out); m != nil {
			disp = m[1]
		}
	}
	if disp == "" {
		disp = ":1"
	}
	if Which("xhost") {
		Run([]string{"xhost", "+SI:localuser:" + os.Getenv("USER")}, Quiet)
	}
	return []string{"DISPLAY=" + disp, "QT_QPA_PLATFORM=xcb"}
}

func LaunchFlags(name string) []string {
	flags := append([]string(nil), Cfg.EmuFlags...)
	extra := ShellSplit(Cfg.AVDFlags[name])
	hasGPU := false
	for _, e := range extra {
		hasGPU = hasGPU || e == "-gpu"
	}
	if gm := IniGet(CfgPath(name), "hw.gpu.mode"); Cfg.DefaultGPU != "" && (gm == "" || gm == "auto") && !hasGPU {
		flags = append(flags, "-gpu", Cfg.DefaultGPU)
	}
	return append(flags, extra...)
}

// DoStart launches the emulator detached; output goes to CacheDir/<name>.log.
func DoStart(name string, extra ...string) (bool, string) {
	if !Which("emulator") {
		return false, "emulator not found — install the SDK 'emulator' package (see: android-tui doctor)"
	}
	MkdirP(CacheDir)
	lp := filepath.Join(CacheDir, name+".log")
	argv := append(append([]string{"emulator", "-avd", name}, LaunchFlags(name)...), extra...)
	Logf("cmd", "$ %s", ShellJoin(argv))
	f, err := os.Create(lp)
	if err != nil {
		return false, err.Error()
	}
	defer func() { _ = f.Close() }() // emulator log file: nothing useful to do if closing fails
	c := exec.Command("setsid", append([]string{"-f"}, argv...)...)
	c.Stdout, c.Stderr = f, f
	c.Env = append(os.Environ(), setupDisplay()...)
	if err := c.Run(); err != nil {
		return false, "could not launch emulator: " + err.Error()
	}
	return true, fmt.Sprintf("starting %s …  (log: %s)", name, lp)
}

func DoStop(e Emu) (bool, string) {
	if e.Serial == "" {
		if _, ok := PsScan()[e.Name]; ok {
			Run([]string{"pkill", "-f", "--", fmt.Sprintf(`(emulator|qemu-system[^ ]*) +(.* )?-avd %s( |$)`, e.Name)}, Opt{})
			return true, "killed half-started " + e.Name
		}
		return true, e.Name + " is not running"
	}
	if HaveAndroid && Run([]string{"android", "emulator", "stop", e.Serial}, Opt{Timeout: 30 * time.Second}).OK() {
		return true, fmt.Sprintf("stopped %s (%s)", e.Name, e.Serial)
	}
	if Adb(e.Serial, Opt{Timeout: 10 * time.Second}, "emu", "kill").OK() {
		return true, fmt.Sprintf("stopped %s (%s) via adb", e.Name, e.Serial)
	}
	return false, fmt.Sprintf("failed to stop %s (%s)", e.Name, e.Serial)
}

func WaitGone(name string) bool {
	for i := 0; i < Cfg.StopTimeout*2; i++ {
		if e, ok := Lookup(name); !ok || !e.Active() {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// StartMode: warm | cold | snap | wipe | headless. Restarts the AVD if it is active.
func StartMode(name, mode, snap string) (bool, string) {
	flags := map[string][]string{"cold": {"-no-snapshot-load"}, "snap": {"-snapshot", snap}, "wipe": {"-wipe-data"},
		"headless": {"-no-window", "-no-audio"}}[mode]
	if e, ok := Lookup(name); ok && e.Active() {
		if ok, msg := DoStop(e); !ok {
			return ok, msg
		}
		if !WaitGone(name) {
			return false, fmt.Sprintf("%s did not shut down in %ds", name, Cfg.StopTimeout)
		}
	}
	return DoStart(name, flags...)
}

func StopAll() (bool, string) {
	n := 0
	for _, e := range Scn.Scan().Emus {
		if e.Active() {
			if ok, _ := DoStop(e); ok {
				n++
			}
		}
	}
	return true, fmt.Sprintf("stopped %d emulator(s)", n)
}

// ───────────────────────────── snapshots ─────────────────────────────

func SaveSnapshot(e Emu, snap string) (bool, string) {
	if e.Serial == "" {
		return false, e.Name + " must be running to save a snapshot"
	}
	if Adb(e.Serial, Opt{Timeout: 120 * time.Second}, "emu", "avd", "snapshot", "save", snap).OK() {
		return true, fmt.Sprintf("saved snapshot '%s' for %s", snap, e.Name)
	}
	return false, "snapshot save failed"
}

func LoadSnapshotLive(e Emu, snap string) (bool, string) {
	r := Adb(e.Serial, Opt{Timeout: 60 * time.Second}, "emu", "avd", "snapshot", "load", snap)
	if r.OK() && !strings.Contains(r.Out, "KO") {
		return true, fmt.Sprintf("loaded '%s' into %s", snap, e.Name)
	}
	return false, "load failed: " + r.Why()
}

func DeleteSnapshot(e Emu, snap string) (bool, string) {
	if e.Serial != "" {
		Adb(e.Serial, Opt{Timeout: 30 * time.Second}, "emu", "avd", "snapshot", "delete", snap)
	} else {
		_ = os.RemoveAll(filepath.Join(AVDDir(e.Name), "snapshots", snap))
	}
	return true, "deleted snapshot " + snap
}

// ───────────────────────────── rename / clone / images ─────────────────────────────

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

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

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

func TagDisplay(t string) string {
	if v, ok := OfficialTags[t]; ok {
		return v
	}
	return t
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
