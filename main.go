// android-tui — Android device & emulator optimisation, diagnostics and management: Bubble Tea TUI + scriptable CLI.
package main

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
	"github.com/madushas/android-tui/internal/ui"
)

const usage = `android-tui %s — Android emulators & adb connections

Usage: android-tui [-y] [command] [args]

  (none)                      interactive TUI (lazygit/btop style)
  ls                          emulators and adb connections, listed separately

  start|stop|restart <avd>    stop also accepts 'all'
  cold|headless|wipe <avd>    cold boot · no window · factory reset
  snaps <avd>                 list snapshots        snap <avd> <name>   boot from one
  save <avd> <name>           save a snapshot of a running AVD
  new                         create-AVD wizard (android CLI)
  rm <avd>                    delete an AVD (asks you to type its name)
  image <avd> <image-path>    change the system image (wipes user data)
  raw <avd>                   open config.ini in $EDITOR
  set <avd> key=value ...     set config.ini keys (scripting)
  rename <avd> <new>          clone <avd> <new> [--with-snapshots]

  images                      installed system images (official + custom ROMs)
  import <zip|url>            install a ROM/system image (AOSP, LineageOS emu_img_zip, /e/OS…)
  mkavd <name> <image> [1-4]  create an AVD from an installed image (profile 1-4)
  rootavd <avd> [--script P] [--status|--restore]  power users: root an AVD with your own copy of rootAVD
  flags <avd> [flags...]      show / set per-AVD emulator launch flags

  connect <host[:port]>       adb connect
  pair [host:port code]       pair by code, or with no args: QR code pairing (Android 11+)

  ── optimisation suite ──
  profile <dev>               what android-tui knows about the device (form factor, SoC, API, privilege)
  tweaks <dev>                catalog filtered for this device, with current state
  apply <dev> <id> [on|off|value]   apply a tweak (journalled)       revert <dev> all|<id>
  journal <dev>               every change made, with the value it replaced
  uidump <dev> [--md|--json|--full] [-o file]  current screen's view hierarchy as text
  snapshot <dev> [label]      save settings+device_config        diff <dev> [snapshot.json]  compare with now

  install <dev> <apk|dir> [flags]   uninstall <dev> <pkg>   shot <dev> [file|-o file|--stdout]
  props <dev> [filter]        pkgs <dev> [filter]       push|pull <dev> <src> <dst>
  adb <dev> <adb args...>     shell <dev>      logcat <dev>

  update [check|all|emulator|cli]   prune   doctor   config
  -y, --yes                   assume "yes" to confirmations

<dev> is an AVD name, an adb serial, or any unique case-insensitive substring.`

var assumeYes bool

func main() {
	var args []string
	for _, a := range os.Args[1:] {
		if a == "-y" || a == "--yes" {
			assumeYes = true
		} else {
			args = append(args, a)
		}
	}
	os.Exit(run(args))
}

func runTUI() int {
	if !isTTY() || fi(os.Stdin) {
		sc := core.Scn.Scan()
		printList(sc)
		return 0
	}
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if core.Cfg.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	if _, err := tea.NewProgram(ui.New(), opts...).Run(); err != nil {
		return fail("%v", err)
	}
	return 0
}
