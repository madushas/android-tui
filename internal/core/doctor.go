package core

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// DoctorLine is one doctor finding. Kind: h (header) | ok | warn | bad | txt.
type DoctorLine struct{ Kind, Text string }

// AdbInstallHint tells the user how to get adb on each platform (shown by the preflight check and the TUI).
const AdbInstallHint = "Arch: pacman -S android-tools · Debian/Ubuntu: apt install adb · Fedora: dnf install android-tools · macOS: brew install android-platform-tools" +
	" · or download platform-tools from developer.android.com/tools/releases/platform-tools and set SDK_ROOT"

func Doctor() []DoctorLine {
	var L []DoctorLine
	add := func(k, f string, a ...any) { L = append(L, DoctorLine{k, fmt.Sprintf(f, a...)}) }
	add("h", "Tools")
	if Which("emulator") {
		v := regexp.MustCompile(`version [0-9.]+`).FindString(Run([]string{"emulator", "-version"}, Opt{Quiet: true, Timeout: 10 * time.Second}).Out)
		add("ok", "emulator found  %s", v)
	} else {
		add("bad", "emulator not found (install the 'emulator' SDK package)")
	}
	if Which("adb") {
		v := strings.SplitN(Run([]string{"adb", "version"}, Quiet).Text(), "\n", 2)[0]
		add("ok", "adb found  %s", v)
	} else {
		add("bad", "adb not found — %s", AdbInstallHint)
	}
	if HaveAndroid {
		add("ok", "android CLI: %s", strings.SplitN(Run([]string{"android", "--version"}, Quiet).Text(), "\n", 2)[0])
	} else {
		add("warn", "android CLI not found — create/update features disabled")
	}
	if SDK != "" {
		add("ok", "SDK: %s", SDK)
	} else {
		add("warn", "SDK root not found (set SDK_ROOT in config) — image features disabled")
	}
	for _, t := range [][2]string{{"scrcpy", "screen mirroring + control"}, {"wl-copy", "clipboard on Wayland"}, {"xdg-open", "open screenshots"}} {
		k, s := "warn", "missing"
		if Which(t[0]) {
			k, s = "ok", "found"
		}
		add(k, "%s: %s  (%s)", t[0], s, t[1])
	}
	add("h", "Virtualization")
	if hostMachine() == "x86_64" {
		if exists("/dev/kvm") {
			if f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0); err == nil {
				_ = f.Close()
				add("ok", "/dev/kvm is accessible")
			} else {
				add("bad", "/dev/kvm exists but you can't use it — run: sudo usermod -aG kvm $USER  (then log out/in)")
			}
		} else {
			add("bad", "/dev/kvm missing — enable VT-x/AMD-V in firmware and load kvm_intel/kvm_amd")
		}
	} else {
		add("ok", "non-x86 host: KVM check skipped")
	}
	if Cfg.DefaultGPU == "swiftshader_indirect" || Cfg.DefaultGPU == "guest" {
		add("warn", "DEFAULT_GPU=%s is deprecated on recent emulators and can crash custom ROMs — try DEFAULT_GPU=\"host\" or per-AVD flags (-gpu host -feature -Vulkan)", Cfg.DefaultGPU)
	}
	add("h", "Display")
	if xw := Run([]string{"pgrep", "-a", "Xwayland"}, Quiet).Out; xw != "" {
		add("ok", "Xwayland running (%s)", regexp.MustCompile(` (:\d+)`).FindString(xw))
	} else {
		add("warn", "Xwayland not running — the emulator window needs it on Wayland (falls back to DISPLAY)")
	}
	if Which("xhost") {
		add("ok", "xhost available")
	} else {
		add("warn", "xhost missing (pacman -S xorg-xhost)")
	}
	add("txt", "    session: %s", os.Getenv("XDG_SESSION_TYPE"))
	add("h", "AVDs")
	var st syscall.Statfs_t
	if syscall.Statfs(AVDHome, &st) == nil {
		free := st.Bavail * uint64(st.Bsize) >> 30
		k := "ok"
		if free < 10 {
			k = "warn"
		}
		add(k, "%dG free on the AVD volume", free)
	}
	names := ListNames()
	for _, n := range names {
		d, c := AVDDir(n), CfgPath(n)
		switch {
		case !exists(d):
			add("bad", "%s: .avd directory missing (orphaned .ini — delete it)", n)
		case !exists(c):
			add("bad", "%s: config.ini missing", n)
		default:
			s := IniGet(c, "image.sysdir.1")
			if SDK != "" && s != "" && !exists(SDK+"/"+s) && !exists(s) {
				add("bad", "%s: system image not installed (%s) — fix: change system image", n, s)
			} else {
				add("ok", "%s", n)
			}
		}
	}
	if len(names) == 0 {
		add("warn", "no AVDs yet — create one with n")
	}
	adbVersionCheck(add)
	return L
}

// adbVersionCheck reports whether this adb can do QR pairing (it needs `adb mdns services`, platform-tools 30.0.0+).
func adbVersionCheck(add func(k, f string, a ...any)) {
	out := Run([]string{"adb", "version"}, Quiet).Out
	if out == "" {
		return
	}
	v, ok := parseAdbVersion(out)
	switch {
	case !ok:
		add("warn", "adb version not recognised — QR pairing needs platform-tools 30.0.0 or newer")
	case v < 30:
		add("bad", "adb %d is too old for QR pairing (needs platform-tools 30.0.0 or newer)", v)
	default:
		add("ok", "adb %d supports QR pairing (mDNS)", v)
	}
}
