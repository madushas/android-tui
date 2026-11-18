package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

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
	MkdirPrivate(CacheDir)
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
