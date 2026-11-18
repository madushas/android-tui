package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/madushas/android-tui/internal/core"
)

// verbs are the commands that act on a named AVD or device; avdOnly are those that make sense only for emulators;
// anything else is treated as a legacy `android-tui <avd> [emulator flags]` start.
var (
	verbs = map[string]bool{"start": true, "stop": true, "restart": true, "cold": true, "headless": true, "wipe": true, "snaps": true, "snap": true, "save": true,
		"rm": true, "delete": true, "image": true, "raw": true, "set": true, "rename": true, "clone": true, "shot": true, "shell": true, "logcat": true,
		"rootavd": true, "profile": true, "tweaks": true, "apply": true, "revert": true, "journal": true, "uidump": true, "snapshot": true, "diff": true,
		"install": true, "uninstall": true, "props": true, "pkgs": true, "push": true, "pull": true, "adb": true}
	avdOnly = map[string]bool{"start": true, "restart": true, "cold": true, "headless": true, "wipe": true, "snaps": true, "snap": true, "save": true, "rm": true,
		"delete": true, "image": true, "raw": true, "set": true, "rename": true, "clone": true, "rootavd": true}
)

func run(args []string) int {
	core.MigrateLegacy()
	core.LoadConfig()
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	if code, handled := runInfoCmd(cmd, args); handled {
		return code
	}
	if !core.Bootstrap() {
		if cmd == "doctor" {
			return doctor()
		}
		return fail("'adb' not found.\n     " + core.AdbInstallHint + "\n     Run 'android-tui doctor' for details.")
	}
	if code, handled := runHostCmd(cmd, args); handled {
		return code
	}
	return runTargetCmd(cmd, args)
}

// runInfoCmd handles commands that need neither adb nor the SDK (help, version, config…).
func runInfoCmd(cmd string, args []string) (int, bool) {
	switch cmd {
	case "-h", "--help", "help":
		fmt.Printf(usage+"\n", core.Version)
		return 0, true
	case "__show":
		if len(args) < 2 {
			return 1, true
		}
		return showImage(args[1]), true
	case "-V", "--version", "version":
		fmt.Println("android-tui", core.Version)
		return 0, true
	case "_names":
		fmt.Println(strings.Join(core.ListNames(), "\n"))
		return 0, true
	case "config":
		core.MkdirPrivate(core.ConfDir)
		if _, err := os.Stat(core.ConfFile); err != nil {
			_ = core.WritePrivate(core.ConfFile, []byte(core.ConfigTemplate))
		}
		ed := core.PickEditor()
		if ed == "" {
			return fail("no editor found — set $EDITOR"), true
		}
		return runExt(append(core.ShellSplit(ed), core.ConfFile)...), true
	}
	return 0, false
}

// runHostCmd handles commands about the host machine and the SDK (doctor, images, import, mkavd, connect, pair…).
func runHostCmd(cmd string, args []string) (int, bool) {
	switch cmd {
	case "":
		return runTUI(), true
	case "doctor":
		return doctor(), true
	case "images":
		imgs := core.ScanImages()
		for _, i := range imgs {
			used := ""
			if len(i.Used) > 0 {
				used = "used by " + strings.Join(i.Used, ", ")
			}
			fmt.Printf("%-62s %-26s %7s  %s\n", i.Rel, core.TagLabel(i.Tag), core.Human(float64(core.DirSize(filepath.Join(core.SDK, i.Rel)))), used)
		}
		if len(imgs) == 0 {
			return fail("no system images found (SDK_ROOT set?)"), true
		}
		return 0, true
	case "import":
		return cliImport(args[1:]), true
	case "mkavd":
		if len(args) < 3 {
			return fail("usage: android-tui mkavd <name> <system-images/android-N/tag/abi> [profile 1-4]"), true
		}
		p := core.HWProfiles[0]
		if len(args) > 3 {
			if n := int(args[3][0] - '0'); n >= 1 && n <= len(core.HWProfiles) {
				p = core.HWProfiles[n-1]
			}
		}
		return done(core.CreateAVDManual(args[1], strings.TrimRight(args[2], "/"), p)), true
	case "flags":
		if len(args) < 2 {
			return fail("usage: android-tui flags <avd> [flags...]"), true
		}
		if len(args) == 2 {
			fmt.Println(orDash(core.Cfg.AVDFlags[args[1]]))
			return 0, true
		}
		return done(core.SetAVDFlags(args[1], strings.Join(args[2:], " "))), true
	case "connect":
		if len(args) < 2 {
			return fail("usage: android-tui connect host[:port]"), true
		}
		return done(core.AdbConnect(args[1])), true
	case "pair":
		if len(args) >= 3 {
			return done(core.AdbPair(args[1], args[2])), true
		}
		return cliQR(), true
	case "new", "create":
		if msg := core.NeedAndroid(); msg != "" {
			return fail("%s", msg), true
		}
		return runExt("android", "emulator", "create"), true
	case "prune", "update":
		if msg := core.NeedAndroid(); msg != "" {
			return fail("%s", msg), true
		}
		return cliUpdate(args[1:], cmd), true
	}
	return 0, false
}

// runTargetCmd resolves the AVD or device named in the arguments and dispatches the verb to the right handler.
func runTargetCmd(cmd string, args []string) int {
	sc := core.Scn.Scan()
	if cmd == "ls" || cmd == "list" {
		printList(sc)
		return 0
	}
	if cmd == "stop" && len(args) > 1 && args[1] == "all" {
		return done(core.StopAll())
	}
	if !verbs[cmd] {
		// legacy: `emu Pixel_9 [emulator flags]`
		d := resolve(sc, cmd)
		if d == nil || d.emu == nil {
			return 1
		}
		if d.emu.Active() {
			return done(true, d.emu.Name+" is already "+d.emu.State)
		}
		return done(core.DoStart(d.emu.Name, args[1:]...))
	}
	if len(args) < 2 {
		return fail("%s needs an AVD or device name", cmd)
	}
	d := resolve(sc, args[1])
	if d == nil {
		return 1
	}
	rest := args[2:]
	if avdOnly[cmd] && d.emu == nil {
		return fail("%s only works on emulators (AVDs)", cmd)
	}
	if !avdOnly[cmd] && cmd != "stop" && !d.online() {
		return fail("%s is not online", args[1])
	}
	s := d.serial()
	if deviceVerbs[cmd] {
		return runDeviceCmd(cmd, args[1], s, rest)
	}
	return runAVDCmd(cmd, d, s, rest, sc)
}
