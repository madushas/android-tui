package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/madushas/android-tui/internal/core"
)

const rootavdUsage = `usage: android-tui rootavd <avd> [--script PATH] [--status | --restore] [-- extra rootAVD arguments]
  Power users: patches the AVD's ramdisk.img with Magisk by running your own copy of the third-party rootAVD script
  (not bundled). The script path comes from --script, ANDROID_TUI_ROOTAVD or ROOTAVD in the config.
  android-tui keeps a one-time backup of the stock ramdisk; --restore puts it back, --status shows where things stand.
  The ramdisk belongs to the SYSTEM IMAGE: every AVD using the same image is affected.`

// cliRootAVD implements `android-tui rootavd`. emus are the current emulator states (used to refuse unsafe actions).
func cliRootAVD(e core.Emu, emus []core.Emu, rest []string) int {
	var script string
	var restore, status bool
	var extra []string
	for i := 0; i < len(rest); i++ {
		switch a := rest[i]; {
		case a == "--":
			extra, i = rest[i+1:], len(rest)
		case a == "--restore":
			restore = true
		case a == "--status":
			status = true
		case a == "--script" && i+1 < len(rest):
			script = rest[i+1]
			i++
		case strings.HasPrefix(a, "--script="):
			script = strings.TrimPrefix(a, "--script=")
		default:
			return fail("rootavd: unknown option %q\n%s", a, rootavdUsage)
		}
	}
	plan, err := core.PlanRootAVD(e.Name)
	if err != nil {
		return fail("%v", err)
	}
	if status {
		fmt.Printf("avd:      %s\nramdisk:  %s\nbackup:   %s\nstatus:   %s\n", e.Name, plan.Ramdisk, plan.Backup, plan.Status())
		if len(plan.Shared) > 0 {
			fmt.Printf("also used by: %s\n", strings.Join(plan.Shared, ", "))
		}
		return 0
	}
	if restore {
		if plan.Status() == "no backup yet" {
			return fail("nothing to restore: no android-tui backup of the stock ramdisk exists for this image")
		}
		for _, x := range emus {
			if x.Active() && (x.Name == e.Name || slices.Contains(plan.Shared, x.Name)) {
				return fail("stop %s first: it boots from this image", x.Name)
			}
		}
		if !confirm("Restore the stock ramdisk for " + plan.Rel + "? (undoes rootAVD for every AVD using this image)") {
			return 1
		}
		return done(plan.Restore())
	}
	if script == "" {
		script = core.RootAVDScript()
	}
	if script == "" {
		return fail("rootAVD is not configured.\n%s", rootavdUsage)
	}
	if err := plan.UseScript(script); err != nil {
		return fail("%v", err)
	}
	if !e.Online() {
		return fail("start %s first: rootAVD patches the ramdisk through adb while the emulator runs", e.Name)
	}
	warn := "Patch the ramdisk of " + e.Name + " with rootAVD? This rewrites " + plan.Rel + "."
	if len(plan.Shared) > 0 {
		warn += " Also affected (same image): " + strings.Join(plan.Shared, ", ") + "."
	}
	if !confirm(warn) {
		return 1
	}
	before, note, err := plan.Prepare()
	if err != nil {
		return fail("%v", err)
	}
	if note != "" {
		fmt.Println(note)
	}
	cmd := plan.Command(extra, false)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	runErr := cmd.Run()
	return done(plan.Finish(before, runErr))
}
