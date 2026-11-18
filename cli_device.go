package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/madushas/android-tui/internal/core"
)

// deviceVerbs act on a running device (an adb connection or a running emulator).
var deviceVerbs = map[string]bool{"profile": true, "tweaks": true, "apply": true, "revert": true, "journal": true, "uidump": true, "snapshot": true,
	"diff": true, "shell": true, "logcat": true, "shot": true, "install": true, "uninstall": true, "props": true, "pkgs": true, "push": true, "pull": true, "adb": true}

// runDeviceCmd runs a verb that talks to a device through adb. name is the device name the user typed, s its adb serial.
func runDeviceCmd(cmd, name, s string, rest []string) int {
	switch cmd {
	case "profile", "tweaks", "apply", "revert", "journal", "uidump", "snapshot", "diff":
		return cliSuite(cmd, s, rest)
	case "shell":
		return runExt("adb", "-s", s, "shell")
	case "logcat":
		return runExt("adb", "-s", s, "logcat")
	case "shot":
		f := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("%s-%s.png", core.SafeName(name), time.Now().Format("20060102-150405")))
		toStdout := false
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "-", "--stdout":
				toStdout = true // raw PNG on stdout: android-tui shot <dev> --stdout > x.png
			case "-o":
				if i+1 < len(rest) {
					f = rest[i+1]
					i++
				}
			default:
				f = rest[i]
			}
		}
		b, err := core.Binary([]string{"adb", "-s", s, "exec-out", "screencap", "-p"}, 30*time.Second)
		if err != nil || len(b) < 100 {
			return fail("screenshot failed: %v", err)
		}
		if toStdout {
			_, _ = os.Stdout.Write(b)
			return 0
		}
		core.MkdirP(filepath.Dir(f))
		if err := core.WritePrivate(f, b); err != nil {
			return fail("%v", err)
		}
		fmt.Println("saved", f)
		return 0
	case "install":
		if len(rest) == 0 {
			return fail("install needs an APK path")
		}
		p := core.NormPath(rest[0])
		files := []string{p}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			files, _ = filepath.Glob(filepath.Join(p, "*.apk"))
		}
		if len(files) == 0 {
			return fail("no APK at %s", p)
		}
		flags := rest[1:]
		if len(flags) == 0 {
			flags = []string{"-r"}
		}
		verb := "install"
		if len(files) > 1 {
			verb = "install-multiple"
		}
		return runExt(append(append([]string{"adb", "-s", s, verb}, flags...), files...)...)
	case "uninstall":
		if len(rest) == 0 {
			return fail("uninstall needs a package")
		}
		return runExt(append([]string{"adb", "-s", s, "uninstall"}, rest...)...)
	case "props":
		for _, kv := range core.ParseProps(core.Sh(s, "getprop", core.Quiet).Out) {
			if len(rest) == 0 || strings.Contains(strings.ToLower(kv.K+"="+kv.V), strings.ToLower(rest[0])) {
				fmt.Printf("%s=%s\n", kv.K, kv.V)
			}
		}
		return 0
	case "pkgs":
		for _, p := range core.ParsePM(core.Sh(s, core.PMScript, core.Opt{Quiet: true, Timeout: 40 * time.Second}).Out) {
			if !p.System && !p.Removed && (len(rest) == 0 || strings.Contains(strings.ToLower(p.Name), strings.ToLower(rest[0]))) {
				fmt.Println(p.Name)
			}
		}
		return 0
	case "push", "pull":
		if len(rest) != 2 {
			return fail("usage: android-tui %s <dev> <src> <dst>", cmd)
		}
		return runExt("adb", "-s", s, cmd, rest[0], rest[1])
	case "adb":
		return runExt(append([]string{"adb", "-s", s}, rest...)...)
	}
	return 0
}
