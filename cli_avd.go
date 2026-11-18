package main

import (
	"fmt"
	"strings"

	"github.com/madushas/android-tui/internal/core"
)

// runAVDCmd runs a verb that manages an emulator (AVD): lifecycle, snapshots, hardware config, rename/clone/delete, rootAVD.
func runAVDCmd(cmd string, d *dev, s string, rest []string, sc core.Scan) int {
	switch cmd {
	case "rootavd":
		return cliRootAVD(*d.emu, sc.Emus, rest)
	case "start":
		if d.emu.Active() {
			return done(true, d.emu.Name+" is already "+d.emu.State)
		}
		return done(core.StartMode(d.emu.Name, "warm", ""))
	case "stop":
		if d.emu == nil {
			return fail("stop only works on emulators")
		}
		return done(core.DoStop(*d.emu))
	case "restart":
		return done(core.StartMode(d.emu.Name, "warm", ""))
	case "cold", "headless", "wipe":
		if cmd == "wipe" && !confirm("Factory-reset "+d.emu.Name+"? All data on it will be erased.") {
			return 1
		}
		return done(core.StartMode(d.emu.Name, cmd, ""))
	case "snaps":
		for _, n := range core.SnapList(d.emu.Name) {
			fmt.Println(n)
		}
		return 0
	case "snap":
		if len(rest) == 0 {
			return fail("snap needs a snapshot name")
		}
		return done(core.StartMode(d.emu.Name, "snap", rest[0]))
	case "save":
		if len(rest) == 0 {
			return fail("save needs a snapshot name")
		}
		return done(core.SaveSnapshot(*d.emu, rest[0]))
	case "rm", "delete":
		if core.ActiveNow(d.emu.Name) {
			core.DoStop(*d.emu)
			core.WaitGone(d.emu.Name)
		}
		if !assumeYes && ask("Type the AVD name ("+d.emu.Name+") to confirm: ") != d.emu.Name {
			return fail("cancelled — nothing was deleted")
		}
		return done(core.DeleteAVD(d.emu.Name))
	case "image":
		if len(rest) == 0 {
			return fail("usage: android-tui image <avd> <system-images/android-N/tag/abi>   (see: android-tui images)")
		}
		if core.ActiveNow(d.emu.Name) {
			return fail("%s is running — stop it first", d.emu.Name)
		}
		if !confirm("This ERASES the user data of " + d.emu.Name + ". Continue?") {
			return 1
		}
		return done(core.SetImage(d.emu.Name, rest[0]))
	case "raw":
		ed := core.PickEditor()
		if ed == "" {
			return fail("no editor found — set $EDITOR")
		}
		c := core.CfgPath(d.emu.Name)
		_ = core.CopyFile(c, c+".emu-bak")
		return runExt(append(core.ShellSplit(ed), c)...)
	case "set":
		if core.ActiveNow(d.emu.Name) {
			return fail("%s is running — stop it first", d.emu.Name)
		}
		for _, kv := range rest {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return fail("expected key=value, got '%s'", kv)
			}
			core.ApplyHW(d.emu.Name, k, v)
			fmt.Printf("  %s=%s\n", k, v)
		}
		return 0
	case "rename":
		if len(rest) == 0 {
			return fail("rename needs a new name")
		}
		return done(core.RenameAVD(d.emu.Name, rest[0]))
	case "clone":
		if len(rest) == 0 {
			return fail("clone needs a new name")
		}
		with := false
		for _, r := range rest[1:] {
			with = with || r == "--with-snapshots"
		}
		return done(core.CloneAVD(d.emu.Name, rest[0], with))
	}
	return 0
}
