package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type action struct {
	group, title, key, req string
	run                    func(m *Model) tea.Cmd
}

// unmet returns why an action isn't available for the current target ("" = available).
func (m *Model) unmet(req string) string {
	t := m.tgt()
	switch req {
	case "any", "":
		return ""
	case "emu":
		if !t.isEmu() {
			return "select an emulator"
		}
	case "stopped":
		if !t.isEmu() || t.emu.State != "stopped" {
			return "the emulator must be stopped"
		}
	case "active":
		if !t.isEmu() || !t.emu.Active() {
			return "the emulator isn't running"
		}
	case "online":
		if !t.online() {
			return "the target must be booted and online"
		}
	case "console":
		if !t.console() {
			return "needs a running emulator"
		}
	}
	return ""
}

func (m *Model) act(label string, fn func() (bool, string)) tea.Cmd {
	return m.job(label, false, func() cont {
		ok, msg := fn()
		return func(m *Model) tea.Cmd { m.report(ok, msg); return m.refreshNow() }
	})
}

func (m *Model) startEmu(name, mode string) tea.Cmd {
	return m.act("start "+name, func() (bool, string) { return core.StartMode(name, mode, "") })
}

func (m *Model) emuTarget() *core.Emu {
	if t := m.tgt(); t.isEmu() {
		e := *t.emu
		return &e
	}
	return nil
}

func (m *Model) screenSize() (int, int) {
	var w, h int
	if n, _ := fmt.Sscanf(regexp.MustCompile(`\d+x\d+`).FindString(m.devInfo()["wm"]), "%dx%d", &w, &h); n == 2 {
		return w, h
	}
	return 1080, 1920
}

var keycodes = [][2]string{{"Home", "KEYCODE_HOME"}, {"Back", "KEYCODE_BACK"}, {"Recents", "KEYCODE_APP_SWITCH"}, {"Power", "KEYCODE_POWER"},
	{"Volume up", "KEYCODE_VOLUME_UP"}, {"Volume down", "KEYCODE_VOLUME_DOWN"}, {"Mute", "KEYCODE_VOLUME_MUTE"}, {"Enter", "KEYCODE_ENTER"},
	{"Tab", "KEYCODE_TAB"}, {"Escape", "KEYCODE_ESCAPE"}, {"Delete", "KEYCODE_DEL"}, {"Menu", "KEYCODE_MENU"}, {"Search", "KEYCODE_SEARCH"},
	{"D-pad up", "KEYCODE_DPAD_UP"}, {"D-pad down", "KEYCODE_DPAD_DOWN"}, {"D-pad left", "KEYCODE_DPAD_LEFT"}, {"D-pad right", "KEYCODE_DPAD_RIGHT"},
	{"D-pad center", "KEYCODE_DPAD_CENTER"}, {"Wake up", "KEYCODE_WAKEUP"}, {"Sleep", "KEYCODE_SLEEP"}, {"Notifications", "KEYCODE_NOTIFICATION"},
	{"Camera", "KEYCODE_CAMERA"}, {"Play/pause", "KEYCODE_MEDIA_PLAY_PAUSE"}}

func (m *Model) actions() []action {
	A := func(g, t, k, r string, f func(m *Model) tea.Cmd) action { return action{g, t, k, r, f} }
	sh := func(label, script string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.adbDo(label, script, nil) }
	}
	view := func(title, script string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.showAdb(title, script) }
	}
	goTab := func(id string) func(m *Model) tea.Cmd { return func(m *Model) tea.Cmd { m.gotoTab(id); return nil } }
	apps := m.tabs["apps"].(*appsTab)
	conn := m.tabs["connect"].(*connectTab)
	imgs := m.tabs["images"].(*imagesTab)
	L := []action{
		A("Emulator", "Start", "s", "stopped", func(m *Model) tea.Cmd { return m.startEmu(m.emuTarget().Name, "warm") }),
		A("Emulator", "Start headless (no window)", "H", "stopped", func(m *Model) tea.Cmd { return m.startEmu(m.emuTarget().Name, "headless") }),
		A("Emulator", "Stop", "x", "active", func(m *Model) tea.Cmd { return m.stopEmu() }),
		A("Emulator", "Restart", "r", "active", func(m *Model) tea.Cmd { return m.startEmu(m.emuTarget().Name, "warm") }),
		A("Emulator", "Cold boot", "c", "emu", func(m *Model) tea.Cmd { return m.startEmu(m.emuTarget().Name, "cold") }),
		A("Emulator", "Wipe data & boot (factory reset)", "W", "emu", func(m *Model) tea.Cmd { return m.wipeEmu() }),
		A("Emulator", "Stop all emulators", "", "any", func(m *Model) tea.Cmd { return m.act("stop all", core.StopAll) }),
		A("Emulator", "New AVD…", "n", "any", func(m *Model) tea.Cmd { return m.newAVD() }),
		A("Emulator", "Edit hardware (AVD tab)", "e", "emu", goTab("avd")),
		A("Emulator", "Edit config.ini in $EDITOR", "E", "stopped", func(m *Model) tea.Cmd { return m.rawEdit() }),
		A("Emulator", "Change system image…", "i", "stopped", func(m *Model) tea.Cmd { return m.changeImage(m.emuTarget().Name) }),
		A("Emulator", "Launch flags (GPU, Vulkan, writable system…)", "F", "emu", func(m *Model) tea.Cmd { return m.editFlags(m.emuTarget().Name) }),
		A("Emulator", "Rename…", "R", "stopped", func(m *Model) tea.Cmd { return m.renameEmu() }),
		A("Emulator", "Clone…", "C", "stopped", func(m *Model) tea.Cmd { return m.cloneEmu() }),
		A("Emulator", "Delete…", "d", "emu", func(m *Model) tea.Cmd { return m.deleteEmu() }),
		A("Emulator", "View emulator log", "o", "emu", func(m *Model) tea.Cmd { return m.emuLog() }),
		A("Emulator", "Open AVD folder", "", "emu", func(m *Model) tea.Cmd {
			if !core.OpenPath(core.AVDDir(m.emuTarget().Name)) {
				m.say("xdg-open missing")
			}
			return nil
		}),
		A("Emulator", "Emulator console command…", "", "console", func(m *Model) tea.Cmd {
			m.ask("Emulator console command  (e.g. 'help', 'avd snapshot list')", "help", func(m *Model, v string) tea.Cmd {
				if v == "" {
					return nil
				}
				return m.showHost("emu "+v, append([]string{"adb", "-s", m.tgt().serial(), "emu"}, strings.Fields(v)...)...)
			})
			return nil
		}),
		A("Emulator", "Virtual sensors (accelerometer, light, proximity…)", "", "console", func(m *Model) tea.Cmd { return m.sensors() }),
		A("ROMs", "Open the Images tab (system images & custom ROMs)", "", "any", goTab("images")),
		A("ROMs", "Import ROM from a local .zip…", "", "any", imgs.importLocal),
		A("ROMs", "Import ROM from a URL…", "", "any", imgs.importURL),
		A("ROMs", "Guide: custom & open-source ROMs", "", "any", func(m *Model) tea.Cmd { m.viewText("Custom ROMs", strings.Split(romGuide, "\n")); return nil }),
		A("Install", "Install APK…", "I", "online", func(m *Model) tea.Cmd { return m.installAPK("") }),
		A("Install", "Install APK from a URL…", "", "online", func(m *Model) tea.Cmd { return m.installURL() }),
		A("Install", "Uninstall package…", "", "online", func(m *Model) tea.Cmd {
			m.ask("Package to uninstall (e.g. com.example.app)", "", func(m *Model, p string) tea.Cmd {
				if p == "" {
					return nil
				}
				m.confirm("Uninstall "+p+"?", true, func(m *Model) tea.Cmd {
					return m.adbDo("uninstall "+p, "pm uninstall "+core.Quote(p), func(m *Model) tea.Cmd { apps.Reload(); return nil })
				})
				return nil
			})
			return nil
		}),
		A("Install", "Extract all user APKs to a folder", "", "online", func(m *Model) tea.Cmd { return m.extractAPKs() }),
		A("Capture", "Screenshot (save)", "S", "online", func(m *Model) tea.Cmd { return m.screenshot("save") }),
		A("Capture", "Screenshot → clipboard", "", "online", func(m *Model) tea.Cmd { return m.screenshot("clip") }),
		A("Capture", "Screenshot & open", "", "online", func(m *Model) tea.Cmd { return m.screenshot("open") }),
		A("Capture", "Screen record start / stop (max 3 min)", "", "online", func(m *Model) tea.Cmd { return m.record() }),
		A("Capture", "UI layout dump", "", "online", func(m *Model) tea.Cmd { return m.layoutDump() }),
		A("Capture", "Mirror & control with scrcpy", "M", "online", func(m *Model) tea.Cmd { return m.scrcpy() }),
		A("Input", "Press key…  (home, back, power, volume…)", "", "online", func(m *Model) tea.Cmd {
			var names []string
			for _, k := range keycodes {
				names = append(names, fmt.Sprintf("%-16s %s", k[0], k[1]))
			}
			m.choose("Press key", names, func(m *Model, i int) tea.Cmd {
				return m.adbDo("key "+keycodes[i][0], "input keyevent "+keycodes[i][1], nil)
			})
			return nil
		}),
		A("Input", "Type text…", "", "online", func(m *Model) tea.Cmd {
			m.ask("Text to type on the device (ASCII only)", "", func(m *Model, v string) tea.Cmd {
				if v == "" {
					return nil
				}
				return m.adbDo("type text", "input text "+core.Quote(strings.ReplaceAll(v, " ", "%s")), nil)
			})
			return nil
		}),
		A("Input", "Tap x y…", "", "online", func(m *Model) tea.Cmd {
			w, h := m.screenSize()
			m.ask(fmt.Sprintf("Tap at x y (screen is %dx%d)", w, h), "", func(m *Model, v string) tea.Cmd {
				if f := regexp.MustCompile(`[ ,]+`).Split(strings.TrimSpace(v), -1); len(f) == 2 {
					return m.adbDo("tap", "input tap "+f[0]+" "+f[1], nil)
				}
				m.say("expected: x y")
				return nil
			})
			return nil
		}),
		A("Input", "Wake & unlock", "", "online", func(m *Model) tea.Cmd {
			w, h := m.screenSize()
			return m.adbDo("wake & unlock", fmt.Sprintf("input keyevent KEYCODE_WAKEUP; wm dismiss-keyguard 2>/dev/null; input swipe %d %d %d %d 200", w/2, h*8/10, w/2, h*2/10), nil)
		}),
		A("Input", "Open notification shade", "", "online", sh("notifications", "cmd statusbar expand-notifications")),
		A("Input", "Open quick settings", "", "online", sh("quick settings", "cmd statusbar expand-settings")),
		A("Input", "Close shade", "", "online", sh("close shade", "cmd statusbar collapse")),
	}
	for _, d := range []string{"up", "down", "left", "right"} {
		d := d
		L = append(L, A("Input", "Swipe "+d, "", "online", func(m *Model) tea.Cmd {
			w, h := m.screenSize()
			cx, cy := w/2, h/2
			c := map[string][4]int{"up": {cx, h * 3 / 4, cx, h / 4}, "down": {cx, h / 4, cx, h * 3 / 4}, "left": {w * 8 / 10, cy, w * 2 / 10, cy}, "right": {w * 2 / 10, cy, w * 8 / 10, cy}}[d]
			return m.adbDo("swipe "+d, fmt.Sprintf("input swipe %d %d %d %d 250", c[0], c[1], c[2], c[3]), nil)
		}))
	}
	L = append(L,
		A("Input", "Open URL / deep link…", "", "online", func(m *Model) tea.Cmd {
			m.ask("Open URL or deep link", "https://", func(m *Model, u string) tea.Cmd {
				if u == "" || u == "https://" {
					return nil
				}
				return m.adbDo("open url", "am start -a android.intent.action.VIEW -d "+core.Quote(u), nil)
			})
			return nil
		}),
		A("Input", "Start activity (am start)…", "", "online", func(m *Model) tea.Cmd {
			m.ask("am start arguments", "-n com.example/.MainActivity", func(m *Model, v string) tea.Cmd { return m.adbDo("am start", "am start "+v, nil) })
			return nil
		}),
		A("Input", "Send broadcast (am broadcast)…", "", "online", func(m *Model) tea.Cmd {
			m.ask("am broadcast arguments", "-a android.intent.action.BOOT_COMPLETED", func(m *Model, v string) tea.Cmd { return m.adbDo("am broadcast", "am broadcast "+v, nil) })
			return nil
		}),
		A("Inspect", "Current foreground activity", "", "online", view("Foreground", "dumpsys window | grep -E 'mCurrentFocus|mFocusedApp|mFocusedWindow'; dumpsys activity activities | grep -E 'topResumedActivity|ResumedActivity' | head -n 5")),
		A("Inspect", "Processes (top snapshot)", "", "online", view("top", "top -b -n 1 -m 30")),
		A("Inspect", "Memory summary (meminfo)", "", "online", view("meminfo", "dumpsys meminfo | head -n 80")),
		A("Inspect", "Battery (dumpsys battery)", "", "online", view("battery", "dumpsys battery")),
		A("Inspect", "dumpsys <service>…", "", "online", func(m *Model) tea.Cmd { return m.dumpsysPicker() }),
		A("Inspect", "System features (pm list features)", "", "online", view("features", "pm list features")),
		A("Inspect", "Network interfaces & routes", "", "online", view("network", "ip addr; echo; ip route")),
		A("Inspect", "adb devices -l", "", "any", func(m *Model) tea.Cmd { return m.showHost("adb devices -l", "adb", "devices", "-l") }),
		A("System", "Reboot…", "", "online", func(m *Model) tea.Cmd {
			m.confirm("Reboot "+m.tgt().name()+"?", false, func(m *Model) tea.Cmd { return m.hostAdb("reboot", "reboot") })
			return nil
		}),
		A("System", "adb root", "", "online", func(m *Model) tea.Cmd { return m.hostAdb("adb root", "root") }),
		A("System", "adb unroot", "", "online", func(m *Model) tea.Cmd { return m.hostAdb("adb unroot", "unroot") }),
		A("System", "Start Shizuku (adb)", "", "online", func(m *Model) tea.Cmd { return m.startShizuku() }),
		A("System", "adb remount", "", "online", func(m *Model) tea.Cmd { return m.hostAdb("adb remount", "remount") }),
		A("System", "Bugreport → file", "", "online", func(m *Model) tea.Cmd {
			core.MkdirP(core.Cfg.PullDir)
			m.push(newTask("adb bugreport", [][]string{{"adb", "-s", m.tgt().serial(), "bugreport", core.Cfg.PullDir}}, nil))
			return nil
		}),
		A("Files", "Pull a path from the device…", "", "online", func(m *Model) tea.Cmd {
			m.ask("Remote path to pull", "/sdcard/", func(m *Model, r string) tea.Cmd {
				if r != "" {
					core.MkdirP(core.Cfg.PullDir)
					m.push(newTask("Pull — "+r, [][]string{{"adb", "-s", m.tgt().serial(), "pull", r, core.Cfg.PullDir + "/"}}, nil))
				}
				return nil
			})
			return nil
		}),
		A("Files", "Push a local file…", "", "online", func(m *Model) tea.Cmd {
			m.askPath("Local file or folder to push", "", "", func(m *Model, src string) tea.Cmd {
				if src == "" {
					return nil
				}
				m.ask("Remote destination", "/sdcard/Download/", func(m *Model, dst string) tea.Cmd {
					if dst != "" {
						m.push(newTask("Push", [][]string{{"adb", "-s", m.tgt().serial(), "push", core.NormPath(src), dst}}, nil))
					}
					return nil
				})
				return nil
			})
			return nil
		}),
		A("Network", "Port forward / reverse…", "", "online", func(m *Model) tea.Cmd { return m.ports() }),
		A("Connect", "Pair with a QR code (Android 11+)", "p", "any", conn.qrPair),
		A("Connect", "Pair with a pairing code…", "", "any", conn.codePair),
		A("Connect", "Connect to host[:port]…", "c", "any", conn.connectPrompt),
		A("Connect", "Scan the LAN for adb (:5555)", "", "any", conn.scan),
		A("Connect", "Use a remote adb server…", "", "any", conn.remoteServer),
		A("Connect", "Restart the adb server", "", "any", conn.restartServer),
		A("Connect", "Open the Connect tab", "", "any", goTab("connect")),
		A("Host", "Zoom main panel (toggle)", "Z", "any", func(m *Model) tea.Cmd { m.zoom = !m.zoom; m.focus = fMain; return nil }),
		A("Host", "Doctor (KVM, display, SDK, AVDs)", "D", "any", func(m *Model) tea.Cmd { return m.doctor() }),
		A("Host", "SDK updates & maintenance…", "u", "any", func(m *Model) tea.Cmd { return m.maintenance() }),
		A("Host", "Edit android-tui config", "", "any", func(m *Model) tea.Cmd {
			core.MkdirP(core.ConfDir)
			if _, err := os.Stat(core.ConfFile); err != nil {
				_ = os.WriteFile(core.ConfFile, []byte(core.ConfigTemplate), 0o644)
			}
			return m.ext(false, append(core.ShellSplit(orStr(core.PickEditor(), "nano")), core.ConfFile)...)
		}),
		A("Host", "Refresh now", "F5", "any", func(m *Model) tea.Cmd { m.tab().Reload(); return m.refreshNow() }),
	)
	for _, id := range append(append([]string{}, m.order[gEmu]...), "connect") {
		id := id
		L = append(L, A("Go to", m.tabs[id].Name()+" tab", "", "any", goTab(id)))
	}
	L = append(L, A("Go to", "Interactive adb shell (full terminal)", "t", "online", func(m *Model) tea.Cmd { return m.ext(false, "adb", "-s", m.tgt().serial(), "shell") }),
		A("Help", "Keyboard reference", "?", "any", func(m *Model) tea.Cmd { m.showHelp(); return nil }))
	return L
}

func (m *Model) emuCmd(label string, args ...string) tea.Cmd {
	s := m.tgt().serial()
	return m.job(label, false, func() cont {
		r := core.Adb(s, core.Opt{Timeout: 15 * time.Second}, append([]string{"emu"}, args...)...)
		return func(m *Model) tea.Cmd {
			m.report(r.OK() && !strings.Contains(r.Out, "KO"), label+": "+core.AdbMsg(r))
			return nil
		}
	})
}

func (m *Model) hostAdb(label string, args ...string) tea.Cmd {
	s := m.tgt().serial()
	return m.job(label, false, func() cont {
		r := core.Adb(s, core.Opt{Timeout: 60 * time.Second}, args...)
		return func(m *Model) tea.Cmd { m.report(r.OK(), label+": "+core.AdbMsg(r)); return m.refreshNow() }
	})
}

// palette opens the fuzzy action launcher.
func (m *Model) palette() tea.Cmd {
	acts := m.actions()
	var ok, no []action
	for _, a := range acts {
		if m.unmet(a.req) == "" {
			ok = append(ok, a)
		} else {
			no = append(no, a)
		}
	}
	use := core.UsageCounts()
	actKey := func(a action) string { return a.group + "/" + a.title }
	sort.SliceStable(ok, func(i, j int) bool { return use[actKey(ok[i])] > use[actKey(ok[j])] }) // most-used first
	rows := append(ok, no...)
	var items []item
	for _, a := range rows {
		key := ""
		if a.key != "" {
			key = "  (" + a.key + ")"
		}
		t := fmt.Sprintf("%-9s %s%s", a.group, a.title, key)
		why := m.unmet(a.req)
		if why != "" {
			t += "   — " + why
		}
		items = append(items, item{text: t, dim: why != ""})
	}
	c := &chooseModal{title: "Actions — " + orStr(m.tgt().name(), "no target"), items: items, flt: newEdit(""), typeahead: true, filtering: true, marks: map[int]bool{}, fuzzy: true,
		boost: func(i int) int { return min(use[actKey(rows[i])], 10) * 3 }}
	c.pick = func(m *Model, i int) tea.Cmd {
		if why := m.unmet(rows[i].req); why != "" {
			m.say(rows[i].title + ": " + why)
			return nil
		}
		core.BumpUsage(actKey(rows[i]))
		return rows[i].run(m)
	}
	m.push(c)
	return nil
}

// ───────────── emulator flows ─────────────

func (m *Model) stopEmu() tea.Cmd {
	e := m.emuTarget()
	if e == nil {
		return nil
	}
	return m.act("stop "+e.Name, func() (bool, string) { return core.DoStop(*e) })
}

func (m *Model) wipeEmu() tea.Cmd {
	e := m.emuTarget()
	if e == nil {
		return nil
	}
	m.confirmTyped("Factory-reset "+e.Name+"? All apps and data on it will be erased.", "wipe", func(m *Model) tea.Cmd {
		return m.startEmu(e.Name, "wipe")
	})
	return nil
}

func (m *Model) deleteEmu() tea.Cmd {
	e := m.emuTarget()
	if e == nil {
		return nil
	}
	del := func(m *Model) tea.Cmd {
		m.ask("Type the AVD name ("+e.Name+") to confirm", "", func(m *Model, v string) tea.Cmd {
			if v != e.Name {
				m.say("cancelled — nothing was deleted")
				return nil
			}
			return m.act("delete "+e.Name, func() (bool, string) {
				if e.Active() {
					core.DoStop(*e)
					core.WaitGone(e.Name)
				}
				return core.DeleteAVD(e.Name)
			})
		})
		return nil
	}
	if e.Active() {
		m.confirm(e.Name+" is running. Stop it and delete?", true, del)
		return nil
	}
	return del(m)
}

func (m *Model) renameEmu() tea.Cmd {
	e := m.emuTarget()
	if e == nil {
		return nil
	}
	m.ask("New name for "+e.Name, e.Name, func(m *Model, v string) tea.Cmd {
		if v == "" || v == e.Name {
			return nil
		}
		return m.act("rename", func() (bool, string) { return core.RenameAVD(e.Name, v) })
	})
	return nil
}

func (m *Model) cloneEmu() tea.Cmd {
	e := m.emuTarget()
	if e == nil {
		return nil
	}
	m.ask("Clone "+e.Name+" as", e.Name+"-copy", func(m *Model, v string) tea.Cmd {
		if v == "" {
			return nil
		}
		m.confirm("Include snapshots? (can be large)", false, func(m *Model) tea.Cmd {
			return m.act("clone", func() (bool, string) { return core.CloneAVD(e.Name, v, true) })
		})
		return nil
	})
	return nil
}

func (m *Model) rawEdit() tea.Cmd {
	e := m.emuTarget()
	ed := core.PickEditor()
	if e == nil || ed == "" {
		m.say("no editor found — set $EDITOR")
		return nil
	}
	c := core.CfgPath(e.Name)
	_ = core.CopyFile(c, c+".emu-bak")
	return m.extThen(false, func(m *Model, err error) tea.Cmd {
		_ = os.RemoveAll(filepath.Join(core.AVDDir(e.Name), "snapshots", "default_boot"))
		m.say("edited config.ini (backup: config.ini.emu-bak; next boot is cold)")
		return nil
	}, append(core.ShellSplit(ed), c)...)
}

func (m *Model) changeImage(name string) tea.Cmd {
	if core.ActiveNow(name) {
		m.say(name + " is running — stop it first (x)")
		return nil
	}
	rels := core.InstalledImagesForHost()
	if len(rels) == 0 {
		m.say("no installed system images — see the Images tab")
		return nil
	}
	var names []string
	for _, r := range rels {
		names = append(names, core.ImgLabel(r))
	}
	m.choose("System image for "+name, names, func(m *Model, i int) tea.Cmd {
		m.confirm(fmt.Sprintf("Switch %s to %s? This ERASES its user data and snapshots (config.ini is backed up).", name, names[i]), true, func(m *Model) tea.Cmd {
			return m.act("change image", func() (bool, string) { return core.SetImage(name, rels[i]) })
		})
		return nil
	})
	return nil
}

func (m *Model) editFlags(name string) tea.Cmd {
	cur := core.Cfg.AVDFlags[name]
	var items []string
	var pre []int
	rest := cur
	for i, p := range core.FlagPresets {
		items = append(items, fmt.Sprintf("%-30s %s", p[0], p[1]))
		if strings.Contains(cur, p[0]) {
			pre = append(pre, i)
		}
		rest = strings.ReplaceAll(rest, p[0], "")
	}
	m.multi("Launch flags — "+name+"   (Space toggles, Enter continues)", items, pre, true, func(m *Model, idx []int) tea.Cmd {
		m.ask("Extra emulator flags (free text, e.g. -memory 4096)", strings.Join(strings.Fields(rest), " "), func(m *Model, extra string) tea.Cmd {
			var fl []string
			for _, i := range idx {
				fl = append(fl, core.FlagPresets[i][0])
			}
			ok, msg := core.SetAVDFlags(name, strings.Join(append(fl, strings.TrimSpace(extra)), " "))
			m.report(ok, msg)
			return nil
		})
		return nil
	})
	return nil
}

func (m *Model) newAVD() tea.Cmd {
	if msg := core.NeedAndroid(); msg != "" {
		m.report(false, msg)
		return nil
	}
	return m.job("profiles", false, func() cont {
		r := core.Run([]string{"android", "emulator", "create", "--list-profiles"}, core.Opt{Timeout: 60 * time.Second})
		var profiles []string
		for _, l := range strings.Split(r.Out, "\n") {
			l = strings.TrimSpace(strings.TrimLeft(l, " \t-*•"))
			if f := strings.Fields(l); len(f) > 0 && regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(f[0]) {
				profiles = append(profiles, l)
			}
		}
		return func(m *Model) tea.Cmd {
			create := func(m *Model, profile string) tea.Cmd {
				before := map[string]bool{}
				for _, n := range core.ListNames() {
					before[n] = true
				}
				return m.extThen(true, func(m *Model, err error) tea.Cmd {
					var fresh string
					for _, n := range core.ListNames() {
						if !before[n] {
							fresh = n
						}
					}
					if err != nil || fresh == "" {
						m.report(false, "android emulator create failed")
						return nil
					}
					m.ask("Name", fresh, func(m *Model, nn string) tea.Cmd {
						if nn != "" && nn != fresh {
							ok, msg := core.RenameAVD(fresh, nn)
							m.report(ok, msg)
							if ok {
								fresh = nn
							}
						}
						m.confirm("Start "+fresh+" now?", false, func(m *Model) tea.Cmd { return m.startEmu(fresh, "warm") })
						return nil
					})
					return nil
				}, "android", "emulator", "create", profile)
			}
			if len(profiles) == 0 {
				m.ask("Device profile", "medium_phone", func(m *Model, v string) tea.Cmd { return create(m, v) })
				return nil
			}
			m.choose("Device profile", profiles, func(m *Model, i int) tea.Cmd { return create(m, strings.Fields(profiles[i])[0]) })
			return nil
		}
	})
}

func (m *Model) emuLog() tea.Cmd {
	e := m.emuTarget()
	if e == nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(core.CacheDir, e.Name+".log"))
	if err != nil {
		m.say("no log for " + e.Name + " yet")
		return nil
	}
	ls := strings.Split(string(b), "\n")
	if len(ls) > 3000 {
		ls = ls[len(ls)-3000:]
	}
	v := newViewer(e.Name+".log", ls)
	v.top = max(0, len(ls)-20)
	m.push(v)
	return nil
}

func (m *Model) doctor() tea.Cmd {
	return m.job("doctor", false, func() cont {
		L := core.Doctor()
		return func(m *Model) tea.Cmd {
			var out []line
			for _, d := range L {
				switch d.Kind {
				case "h":
					out = append(out, line{"", ""}, line{d.Text, "bold"})
				case "ok":
					out = append(out, line{"  ✓ " + d.Text, "ok"})
				case "warn":
					out = append(out, line{"  ! " + d.Text, "warn"})
				case "bad":
					out = append(out, line{"  ✗ " + d.Text, "err"})
				default:
					out = append(out, line{d.Text, ""})
				}
			}
			m.push(newStyledViewer("android-tui doctor", out))
			return nil
		}
	})
}

func (m *Model) maintenance() tea.Cmd {
	if msg := core.NeedAndroid(); msg != "" {
		m.report(false, msg)
		return nil
	}
	opts := []string{"Check for SDK updates", "Update all SDK packages  (android sdk update)", "Update the emulator only", "Update the Android CLI itself  (android update)", "Install a system image… (Images tab)", "Prune unused system images…"}
	m.choose("Maintenance", opts, func(m *Model, i int) tea.Cmd {
		switch i {
		case 0:
			return m.job("sdk updates", false, func() cont {
				u, ok := core.SDKUpdates()
				return func(m *Model) tea.Cmd {
					if !ok {
						m.report(false, "could not check for updates (offline?)")
						return nil
					}
					core.SaveUpdates(u)
					m.upd = len(u)
					var ls []string
					for _, x := range u {
						ls = append(ls, fmt.Sprintf("⬆ %-46s %s → %s", x.Pkg, x.Old, x.New))
					}
					if len(ls) == 0 {
						ls = []string{"✓ all SDK packages are up to date"}
					}
					m.viewText("SDK updates", ls)
					return nil
				}
			})
		case 1, 2, 3:
			for _, e := range m.sc.Emus {
				if i != 3 && e.Active() {
					m.report(false, "stop all emulators before updating the SDK")
					return nil
				}
			}
			argv := [][]string{{"android", "sdk", "update"}, {"android", "sdk", "update", "emulator"}, {"android", "update"}}[i-1]
			return m.extThen(true, func(m *Model, err error) tea.Cmd { core.SaveUpdates(nil); m.upd = 0; return nil }, argv...)
		case 4:
			m.gotoTab("images")
		case 5:
			return m.pruneImages()
		}
		return nil
	})
	return nil
}

func (m *Model) pruneImages() tea.Cmd {
	var unused []core.Image
	for _, i := range core.ScanImages() {
		if len(i.Used) == 0 {
			unused = append(unused, i)
		}
	}
	if len(unused) == 0 {
		m.say("no unused system images")
		return nil
	}
	var items []string
	for _, i := range unused {
		items = append(items, fmt.Sprintf("%s  (%s)", i.Rel, human(float64(core.DirSize(filepath.Join(core.SDK, i.Rel))))))
	}
	m.multi("Unused system images — Space toggles, a = all, Enter confirms", items, nil, false, func(m *Model, idx []int) tea.Cmd {
		m.confirm(fmt.Sprintf("Remove %d image(s) via 'android sdk remove'?", len(idx)), true, func(m *Model) tea.Cmd {
			var argv []string
			for k, i := range idx {
				if k > 0 {
					argv = append(argv, "&&")
				}
				argv = append(argv, "android sdk remove "+core.Quote(unused[i].Rel))
			}
			return m.ext(true, "sh", "-c", strings.ReplaceAll(strings.Join(argv, " "), "&& android", "; android"))
		})
		return nil
	})
	return nil
}

// ───────────── adb-device flows ─────────────

func (m *Model) logcatFor(pkg string) tea.Cmd {
	m.gotoTab("logcat")
	return m.tabs["logcat"].(*logcatTab).setApp(m, pkg)
}

func (m *Model) installAPK(path string) tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	s := t.serial()
	if path == "" {
		seen := map[string]bool{}
		var cands []string
		wd, _ := os.Getwd()
		for _, d := range []string{core.Cfg.APKDir, wd, filepath.Join(core.Home, "Downloads")} {
			g, _ := filepath.Glob(filepath.Join(d, "*.apk"))
			for _, f := range g {
				if !seen[f] {
					seen[f] = true
					cands = append(cands, f)
				}
			}
		}
		sort.Slice(cands, func(i, j int) bool {
			a, _ := os.Stat(cands[i])
			b, _ := os.Stat(cands[j])
			return a != nil && b != nil && a.ModTime().After(b.ModTime())
		})
		if len(cands) > 20 {
			cands = cands[:20]
		}
		var names []string
		for _, f := range cands {
			st, _ := os.Stat(f)
			names = append(names, fmt.Sprintf("%s   %s  ·  %s", filepath.Base(f), human(float64(st.Size())), filepath.Dir(f)))
		}
		m.choose("Install APK", append(names, "Type or paste a path (file, or folder of split APKs)…"), func(m *Model, i int) tea.Cmd {
			if i < len(cands) {
				return m.installAPK(cands[i])
			}
			m.askPath("APK path (file, or folder of split APKs)", "", "drag & drop a file onto the terminal to paste its path", func(m *Model, v string) tea.Cmd {
				if v != "" {
					return m.installAPK(core.NormPath(v))
				}
				return nil
			})
			return nil
		})
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		m.report(false, "not found: "+path)
		return nil
	}
	files := []string{path}
	if st.IsDir() {
		files, _ = filepath.Glob(filepath.Join(path, "*.apk"))
		sort.Strings(files)
	}
	if len(files) == 0 {
		m.report(false, "no .apk files found there")
		return nil
	}
	opts := [][2]string{{"-r", "Replace existing app (-r)"}, {"-g", "Grant all permissions (-g)"}, {"-d", "Allow version downgrade (-d)"}, {"-t", "Allow test packages (-t)"}}
	var labels []string
	for _, o := range opts {
		labels = append(labels, o[1])
	}
	m.multi("Install options (Space toggles, Enter installs)", labels, []int{0}, true, func(m *Model, idx []int) tea.Cmd {
		verb := "install"
		if len(files) > 1 {
			verb = "install-multiple"
		}
		argv := []string{"adb", "-s", s, verb}
		for _, i := range idx {
			argv = append(argv, opts[i][0])
		}
		argv = append(argv, files...)
		m.push(newTask("adb "+verb+" — "+filepath.Base(files[0]), [][]string{argv}, func(m *Model, rc int, lines []string) tea.Cmd {
			ok := rc == 0 && strings.Contains(strings.Join(lines, "\n"), "Success")
			m.report(ok, map[bool]string{true: "installed " + filepath.Base(files[0]), false: "install failed — see the log (try -r or -d)"}[ok])
			m.tabs["apps"].Reload()
			return nil
		}))
		return nil
	})
	return nil
}

func (m *Model) installURL() tea.Cmd {
	if !m.tgt().online() {
		m.say("select a running device first")
		return nil
	}
	m.ask("URL of an .apk", "https://", func(m *Model, u string) tea.Cmd {
		if u == "" || u == "https://" {
			return nil
		}
		name := filepath.Base(strings.SplitN(u, "?", 2)[0])
		if !strings.HasSuffix(name, ".apk") {
			name += ".apk"
		}
		dest := filepath.Join(core.CacheDir, "dl", name)
		core.MkdirP(filepath.Dir(dest))
		m.push(newWork("Download — "+name, true, func(emit core.Emit, c func() bool) any {
			if _, err := core.Download(u, dest, emit, c); err != nil {
				emit("✗ "+err.Error(), false)
				return nil
			}
			return dest
		}, func(m *Model, v any) tea.Cmd {
			if p, ok := v.(string); ok {
				return m.installAPK(p)
			}
			return nil
		}))
		return nil
	})
	return nil
}

func (m *Model) extractAPKs() tea.Cmd {
	t := m.tgt()
	s, name := t.serial(), t.name()
	m.push(newWork("Extract all user APKs", false, func(emit core.Emit, c func() bool) any {
		r := core.Sh(s, "pm list packages -f -3", core.Opt{Timeout: 30 * time.Second})
		base := filepath.Join(core.Cfg.PullDir, name+"-apks")
		n, total := 0, 0
		for _, l := range strings.Split(r.Out, "\n") {
			if !strings.HasPrefix(l, "package:") {
				continue
			}
			total++
			i := strings.LastIndex(l, "=")
			path, pkg := l[8:i], l[i+1:]
			if c() {
				break
			}
			core.MkdirP(filepath.Join(base, pkg))
			pr := core.Adb(s, core.Opt{Timeout: 300 * time.Second}, "pull", path, filepath.Join(base, pkg)+"/")
			emit(pkg+": "+core.AdbMsg(pr), false)
			if pr.OK() {
				n++
			}
		}
		emit(fmt.Sprintf("%d/%d base APKs saved under %s", n, total, base), false)
		return n
	}, nil))
	return nil
}

func (m *Model) screenshot(mode string) tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	s, name := t.serial(), t.name()
	return m.act("screenshot", func() (bool, string) {
		core.MkdirP(core.Cfg.ShotDir)
		f := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("%s-%s.png", name, time.Now().Format("20060102-150405")))
		b, err := core.Binary([]string{"adb", "-s", s, "exec-out", "screencap", "-p"}, 30*time.Second)
		if err != nil || len(b) < 100 {
			return false, "screenshot failed: " + fmt.Sprint(err)
		}
		if err := os.WriteFile(f, b, 0o644); err != nil {
			return false, err.Error()
		}
		extra := ""
		switch mode {
		case "clip":
			extra = map[bool]string{true: " · copied to clipboard", false: " · no clipboard tool"}[core.CopyImage(b)]
		case "open":
			extra = map[bool]string{true: " · opened", false: " · xdg-open missing"}[core.OpenPath(f)]
		}
		return true, "saved " + f + extra
	})
}

func (m *Model) record(extra ...string) tea.Cmd {
	t := m.tgt()
	if m.rec != nil {
		rec := m.rec
		m.rec = nil
		return m.act("stop recording", func() (bool, string) {
			core.Sh(rec.serial, "kill -2 $(pidof screenrecord)", core.Opt{Timeout: 10 * time.Second})
			done := make(chan struct{})
			go func() { _, _ = rec.proc.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				_ = rec.proc.Kill()
			}
			time.Sleep(time.Second)
			core.MkdirP(core.Cfg.ShotDir)
			dest := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("%s-%s.mp4", rec.name, time.Now().Format("20060102-150405")))
			r := core.Adb(rec.serial, core.Opt{Timeout: 300 * time.Second}, "pull", rec.remote, dest)
			core.Sh(rec.serial, "rm -f "+core.Quote(rec.remote), core.Opt{Quiet: true})
			if r.OK() {
				return true, "saved " + dest
			}
			return false, "pull failed: " + r.Why()
		})
	}
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	remote := fmt.Sprintf("/sdcard/android-tui-rec-%d.mp4", time.Now().Unix())
	argv := []string{"adb", "-s", t.serial(), "shell", "screenrecord"}
	if len(extra) == 0 {
		extra = []string{"--time-limit", "180"}
	}
	argv = append(append(argv, extra...), remote)
	core.Logf("cmd", "$ %s", core.ShellJoin(argv))
	c := exec.Command(argv[0], argv[1:]...)
	if err := c.Start(); err != nil {
		m.report(false, err.Error())
		return nil
	}
	m.rec = &recording{serial: t.serial(), remote: remote, name: t.name(), proc: c.Process, t0: time.Now()}
	m.say("recording (max 3 min) — run 'Screen record' again to stop")
	return nil
}

func (m *Model) scrcpy() tea.Cmd {
	t := m.tgt()
	if !core.Which("scrcpy") {
		m.say("scrcpy not installed (pacman -S scrcpy)")
		return nil
	}
	core.Logf("cmd", "$ scrcpy -s %s", t.serial())
	if err := exec.Command("scrcpy", "-s", t.serial()).Start(); err != nil {
		m.report(false, err.Error())
		return nil
	}
	m.say("scrcpy started for " + t.serial())
	return nil
}

func (m *Model) layoutDump() tea.Cmd {
	s := m.tgt().serial()
	return m.job("UI layout", false, func() cont {
		var out string
		if core.HaveAndroid {
			if r := core.Run([]string{"android", "layout", "--pretty", "--device", s}, core.Opt{Timeout: time.Minute}); r.OK() && strings.TrimSpace(r.Out) != "" {
				out = r.Out
			}
		}
		if out == "" {
			r := core.Sh(s, "uiautomator dump /sdcard/android-tui-ui.xml >/dev/null 2>&1; cat /sdcard/android-tui-ui.xml; rm -f /sdcard/android-tui-ui.xml", core.Opt{Timeout: 40 * time.Second})
			out = strings.ReplaceAll(r.Out, "><", ">\n<")
		}
		return func(m *Model) tea.Cmd { m.viewText("UI layout", strings.Split(out, "\n")); return nil }
	})
}

func (m *Model) sensors() tea.Cmd {
	opts := [][3]string{{"acceleration", "x:y:z  m/s²  (gravity pointing down is 0:9.8:0)", "0:9.8:0"}, {"magnetic-field", "x:y:z  µT", "0:30:0"},
		{"proximity", "distance cm (0 = near)", "0"}, {"light", "lux", "400"}, {"temperature", "°C", "22"}, {"humidity", "%", "50"}, {"pressure", "hPa", "1013"}}
	var names []string
	for _, o := range opts {
		names = append(names, fmt.Sprintf("%-16s %s", o[0], o[1]))
	}
	m.choose("Virtual sensor", names, func(m *Model, i int) tea.Cmd {
		m.ask(opts[i][0]+" value", opts[i][2], func(m *Model, v string) tea.Cmd { return m.emuCmd("sensor set", "sensor", "set", opts[i][0], v) })
		return nil
	})
	return nil
}

func (m *Model) dumpsysPicker() tea.Cmd {
	s := m.tgt().serial()
	return m.job("dumpsys -l", false, func() cont {
		r := core.Sh(s, "dumpsys -l", core.Opt{Timeout: 20 * time.Second})
		var names []string
		for _, l := range strings.Split(r.Out, "\n") {
			if strings.HasPrefix(l, "  ") && strings.TrimSpace(l) != "" {
				names = append(names, strings.TrimSpace(l))
			}
		}
		return func(m *Model) tea.Cmd {
			if len(names) == 0 {
				m.say("could not list services")
				return nil
			}
			var its []item
			for _, n := range names {
				its = append(its, item{text: n})
			}
			m.push(&chooseModal{title: "dumpsys …", items: its, flt: newEdit(""), typeahead: true, filtering: true, marks: map[int]bool{},
				pick: func(m *Model, i int) tea.Cmd { return m.showAdb("dumpsys "+names[i], "dumpsys "+core.Quote(names[i])) }})
			return nil
		}
	})
}

func (m *Model) ports() tea.Cmd {
	s := m.tgt().serial()
	opts := []string{"List forwards + reverses", "Forward  host → device", "Reverse  device → host", "Remove all forwards", "Remove all reverses"}
	m.choose("Port forwarding", opts, func(m *Model, i int) tea.Cmd {
		switch i {
		case 0:
			return m.job("ports", false, func() cont {
				f := core.Adb(s, core.Opt{}, "forward", "--list")
				r := core.Adb(s, core.Opt{}, "reverse", "--list")
				ls := append(append([]string{"forward:"}, splitLines(strings.TrimSpace(orStr(f.Out, "  (none)")))...), "", "reverse:")
				ls = append(ls, splitLines(strings.TrimSpace(orStr(r.Out, "  (none)")))...)
				return func(m *Model) tea.Cmd { m.viewText("forward / reverse", ls); return nil }
			})
		case 1, 2:
			verb := []string{"", "forward", "reverse"}[i]
			m.ask("local → remote  (e.g. tcp:8080 tcp:8080)", "tcp:8080 tcp:8080", func(m *Model, v string) tea.Cmd {
				if len(strings.Fields(v)) == 0 {
					return nil
				}
				return m.hostAdb(verb, append([]string{verb}, strings.Fields(v)...)...)
			})
		default:
			verb := map[int]string{3: "forward", 4: "reverse"}[i]
			return m.hostAdb("remove "+verb+"s", verb, "--remove-all")
		}
		return nil
	})
	return nil
}

// startShizuku runs the adb bootstrap for Shizuku and re-probes the profile afterwards.
func (m *Model) startShizuku() tea.Cmd {
	s := m.tgt().serial()
	return m.job("start Shizuku", false, func() cont {
		ok, msg := core.ShizukuBootstrap(s)
		return func(m *Model) tea.Cmd { delete(m.profiles, s); m.report(ok, msg); return nil }
	})
}
