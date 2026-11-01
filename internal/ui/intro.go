package ui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/madushas/android-tui/internal/core"
)

const introText = ` Welcome to android-tui — emulators, devices and safe optimisation in one place.

 MOVE        1 / 2 / 3  focus emulators · adb connections · snapshots      → or l  enter the main panel      Esc  back
 MODES       { }  switch mode: Device · Optimize · Inspect                 [ ]  switch tab inside the mode
 EVERYWHERE  :  or Ctrl-P  action palette (fuzzy search, remembers what you use)      ?  full help      q  quit
 UNDO        Ctrl-Z  undo the last change on the selected device

 SAFE BY DESIGN
   · every change is journalled with the value it replaced (Optimize › Journal) and can be reverted
   · an automatic 'auto' settings snapshot is taken before the first change on a device
   · Debloat only disables packages for user 0 (restorable); wipe/delete ask you to confirm

 FIRST STEPS
   1. connect a phone (USB debugging, or Connect › QR pairing) or start an emulator
   2. select it, press → — the Overview shows 'Next steps' with one-key shortcuts
   3. Optimize › Tweaks lists only what fits this device (brand, SoC, Android version)

 Press Esc to close. This screen appears once (delete ` + "`intro.seen`" + ` in the data dir to see it again).`

// maybeIntro shows the quick-start overlay on the very first run (flag file in the data dir).
func (m *Model) maybeIntro() {
	if os.Getenv("ATUI_NO_INTRO") != "" {
		return
	}
	flag := filepath.Join(core.DataDir, "intro.seen")
	if _, err := os.Stat(flag); err == nil {
		return
	}
	core.MkdirP(core.DataDir)
	_ = os.WriteFile(flag, []byte("1\n"), 0o644)
	m.push(newViewer("Quick start", strings.Split(introText, "\n")))
}
