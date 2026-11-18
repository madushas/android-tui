package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

// actions is the palette's complete action list. The order is the palette's default order (most-used actions float up
// through frecency): emulator, ROMs/install/capture, input, inspect, system, files/network, connect, host, navigation.
func (m *Model) actions() []action {
	var L []action
	for _, group := range []func() []action{
		m.emulatorActions, m.installActions, m.inputActions, m.inspectActions, m.systemActions,
		m.filesNetworkActions, m.connectActions, m.hostActions, m.navigationActions,
	} {
		L = append(L, group()...)
	}
	return L
}

// Helpers shared by the tables below.
func actA(g, t, k, r string, f func(m *Model) tea.Cmd) action { return action{g, t, k, r, f} }

// actSh runs an adb shell script on the device and reports the result.
func actSh(label, script string) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd { return m.adbDo(label, script, nil) }
}

// actView shows the output of an adb shell script in a viewer.
func actView(title, script string) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd { return m.showAdb(title, script) }
}

func actGoTab(id string) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd { m.gotoTab(id); return nil }
}

// emulatorActions: starting, stopping and editing AVDs, plus the optional rootAVD entries.
func (m *Model) emulatorActions() []action {
	return []action{
		actA("Emulator", "Start", "s", "stopped", func(m *Model) tea.Cmd { return m.startEmu(m.emuTarget().Name, "warm") }),
		actA("Emulator", "Start headless (no window)", "H", "stopped", func(m *Model) tea.Cmd { return m.startEmu(m.emuTarget().Name, "headless") }),
		actA("Emulator", "Stop", "x", "active", func(m *Model) tea.Cmd { return m.stopEmu() }),
		actA("Emulator", "Restart", "r", "active", func(m *Model) tea.Cmd { return m.startEmu(m.emuTarget().Name, "warm") }),
		actA("Emulator", "Cold boot", "c", "emu", func(m *Model) tea.Cmd { return m.startEmu(m.emuTarget().Name, "cold") }),
		actA("Emulator", "Wipe data & boot (factory reset)", "W", "emu", func(m *Model) tea.Cmd { return m.wipeEmu() }),
		actA("Emulator", "Stop all emulators", "", "any", func(m *Model) tea.Cmd { return m.act("stop all", core.StopAll) }),
		actA("Emulator", "New AVD…", "n", "any", func(m *Model) tea.Cmd { return m.newAVD() }),
		actA("Emulator", "Edit hardware (AVD tab)", "e", "emu", actGoTab("avd")),
		actA("Emulator", "Edit config.ini in $EDITOR", "E", "stopped", func(m *Model) tea.Cmd { return m.rawEdit() }),
		actA("Emulator", "Change system image…", "i", "stopped", func(m *Model) tea.Cmd { return m.changeImage(m.emuTarget().Name) }),
		actA("Emulator", "Launch flags (GPU, Vulkan, writable system…)", "F", "emu", func(m *Model) tea.Cmd { return m.editFlags(m.emuTarget().Name) }),
		actA("Emulator", "Rename…", "R", "stopped", func(m *Model) tea.Cmd { return m.renameEmu() }),
		actA("Emulator", "Clone…", "C", "stopped", func(m *Model) tea.Cmd { return m.cloneEmu() }),
		actA("Emulator", "Delete…", "d", "emu", func(m *Model) tea.Cmd { return m.deleteEmu() }),
		actA("Emulator", "View emulator log", "o", "emu", func(m *Model) tea.Cmd { return m.emuLog() }),
		actA("Emulator", "Open AVD folder", "", "emu", func(m *Model) tea.Cmd {
			if !core.OpenPath(core.AVDDir(m.emuTarget().Name)) {
				m.say("xdg-open missing")
			}
			return nil
		}),
		actA("Emulator", "Emulator console command…", "", "console", func(m *Model) tea.Cmd {
			m.ask("Emulator console command  (e.g. 'help', 'avd snapshot list')", "help", func(m *Model, v string) tea.Cmd {
				if v == "" {
					return nil
				}
				return m.showHost("emu "+v, append([]string{"adb", "-s", m.tgt().serial(), "emu"}, strings.Fields(v)...)...)
			})
			return nil
		}),
		actA("Emulator", "Virtual sensors (accelerometer, light, proximity…)", "", "console", func(m *Model) tea.Cmd { return m.sensors() }),
		actA("Emulator", "Root AVD with rootAVD… (power users)", "", "emu", func(m *Model) tea.Cmd { return m.rootAVD() }),
		actA("Emulator", "Restore stock ramdisk (undo rootAVD)", "", "emu", func(m *Model) tea.Cmd { return m.restoreRamdisk() }),
	}
}

// installActions: system images / ROMs, installing and extracting apps, screenshots, recording, layout dumps.
func (m *Model) installActions() []action {
	apps := m.tabs["apps"].(*appsTab)
	imgs := m.tabs["images"].(*imagesTab)
	return []action{
		actA("ROMs", "Open the Images tab (system images & custom ROMs)", "", "any", actGoTab("images")),
		actA("ROMs", "Import ROM from a local .zip…", "", "any", imgs.importLocal),
		actA("ROMs", "Import ROM from a URL…", "", "any", imgs.importURL),
		actA("ROMs", "Guide: custom & open-source ROMs", "", "any", func(m *Model) tea.Cmd { m.viewText("Custom ROMs", strings.Split(romGuide, "\n")); return nil }),
		actA("Install", "Install APK…", "I", "online", func(m *Model) tea.Cmd { return m.installAPK("") }),
		actA("Install", "Install APK from a URL…", "", "online", func(m *Model) tea.Cmd { return m.installURL() }),
		actA("Install", "Uninstall package…", "", "online", func(m *Model) tea.Cmd {
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
		actA("Install", "Extract all user APKs to a folder", "", "online", func(m *Model) tea.Cmd { return m.extractAPKs() }),
		actA("Capture", "Screenshot (save)", "S", "online", func(m *Model) tea.Cmd { return m.screenshot("save") }),
		actA("Capture", "Screenshot → clipboard", "", "online", func(m *Model) tea.Cmd { return m.screenshot("clip") }),
		actA("Capture", "Screenshot & open", "", "online", func(m *Model) tea.Cmd { return m.screenshot("open") }),
		actA("Capture", "Screen record start / stop (max 3 min)", "", "online", func(m *Model) tea.Cmd { return m.record() }),
		actA("Capture", "UI layout dump", "", "online", func(m *Model) tea.Cmd { return m.layoutDump() }),
		actA("Capture", "Mirror & control with scrcpy", "M", "online", func(m *Model) tea.Cmd { return m.scrcpy() }),
	}
}

// inputActions: keys, text, taps, swipes, intents and notifications sent to the device.
func (m *Model) inputActions() []action {
	L := []action{
		actA("Input", "Press key…  (home, back, power, volume…)", "", "online", func(m *Model) tea.Cmd {
			var names []string
			for _, k := range keycodes {
				names = append(names, fmt.Sprintf("%-16s %s", k[0], k[1]))
			}
			m.choose("Press key", names, func(m *Model, i int) tea.Cmd {
				return m.adbDo("key "+keycodes[i][0], "input keyevent "+keycodes[i][1], nil)
			})
			return nil
		}),
		actA("Input", "Type text…", "", "online", func(m *Model) tea.Cmd {
			m.ask("Text to type on the device (ASCII only)", "", func(m *Model, v string) tea.Cmd {
				if v == "" {
					return nil
				}
				return m.adbDo("type text", "input text "+core.Quote(strings.ReplaceAll(v, " ", "%s")), nil)
			})
			return nil
		}),
		actA("Input", "Tap x y…", "", "online", func(m *Model) tea.Cmd {
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
		actA("Input", "Wake & unlock", "", "online", func(m *Model) tea.Cmd {
			w, h := m.screenSize()
			return m.adbDo("wake & unlock", fmt.Sprintf("input keyevent KEYCODE_WAKEUP; wm dismiss-keyguard 2>/dev/null; input swipe %d %d %d %d 200", w/2, h*8/10, w/2, h*2/10), nil)
		}),
		actA("Input", "Open notification shade", "", "online", actSh("notifications", "cmd statusbar expand-notifications")),
		actA("Input", "Open quick settings", "", "online", actSh("quick settings", "cmd statusbar expand-settings")),
		actA("Input", "Close shade", "", "online", actSh("close shade", "cmd statusbar collapse")),
	}
	for _, d := range []string{"up", "down", "left", "right"} {
		d := d
		L = append(L, actA("Input", "Swipe "+d, "", "online", func(m *Model) tea.Cmd {
			w, h := m.screenSize()
			cx, cy := w/2, h/2
			c := map[string][4]int{"up": {cx, h * 3 / 4, cx, h / 4}, "down": {cx, h / 4, cx, h * 3 / 4}, "left": {w * 8 / 10, cy, w * 2 / 10, cy}, "right": {w * 2 / 10, cy, w * 8 / 10, cy}}[d]
			return m.adbDo("swipe "+d, fmt.Sprintf("input swipe %d %d %d %d 250", c[0], c[1], c[2], c[3]), nil)
		}))
	}
	L = append(L,
		actA("Input", "Open URL / deep link…", "", "online", func(m *Model) tea.Cmd {
			m.ask("Open URL or deep link", "https://", func(m *Model, u string) tea.Cmd {
				if u == "" || u == "https://" {
					return nil
				}
				return m.adbDo("open url", "am start -a android.intent.action.VIEW -d "+core.Quote(u), nil)
			})
			return nil
		}),
		actA("Input", "Start activity (am start)…", "", "online", func(m *Model) tea.Cmd {
			m.ask("am start arguments", "-n com.example/.MainActivity", func(m *Model, v string) tea.Cmd { return m.adbDo("am start", "am start "+v, nil) })
			return nil
		}),
		actA("Input", "Send broadcast (am broadcast)…", "", "online", func(m *Model) tea.Cmd {
			m.ask("am broadcast arguments", "-a android.intent.action.BOOT_COMPLETED", func(m *Model, v string) tea.Cmd { return m.adbDo("am broadcast", "am broadcast "+v, nil) })
			return nil
		}),
	)
	return L
}

// inspectActions: read-only views of device state (dumpsys, top, network, features).
func (m *Model) inspectActions() []action {
	return []action{
		actA("Inspect", "Current foreground activity", "", "online", actView("Foreground", "dumpsys window | grep -E 'mCurrentFocus|mFocusedApp|mFocusedWindow'; dumpsys activity activities | grep -E 'topResumedActivity|ResumedActivity' | head -n 5")),
		actA("Inspect", "Processes (top snapshot)", "", "online", actView("top", "top -b -n 1 -m 30")),
		actA("Inspect", "Memory summary (meminfo)", "", "online", actView("meminfo", "dumpsys meminfo | head -n 80")),
		actA("Inspect", "Battery (dumpsys battery)", "", "online", actView("battery", "dumpsys battery")),
		actA("Inspect", "dumpsys <service>…", "", "online", func(m *Model) tea.Cmd { return m.dumpsysPicker() }),
		actA("Inspect", "System features (pm list features)", "", "online", actView("features", "pm list features")),
		actA("Inspect", "Network interfaces & routes", "", "online", actView("network", "ip addr; echo; ip route")),
		actA("Inspect", "adb devices -l", "", "any", func(m *Model) tea.Cmd { return m.showHost("adb devices -l", "adb", "devices", "-l") }),
	}
}

// systemActions: reboot, adb root/remount, Shizuku, bugreport.
func (m *Model) systemActions() []action {
	return []action{
		actA("System", "Reboot…", "", "online", func(m *Model) tea.Cmd {
			m.confirm("Reboot "+m.tgt().name()+"?", false, func(m *Model) tea.Cmd { return m.hostAdb("reboot", "reboot") })
			return nil
		}),
		actA("System", "adb root", "", "online", func(m *Model) tea.Cmd { return m.hostAdb("adb root", "root") }),
		actA("System", "adb unroot", "", "online", func(m *Model) tea.Cmd { return m.hostAdb("adb unroot", "unroot") }),
		actA("System", "Start Shizuku (adb)", "", "online", func(m *Model) tea.Cmd { return m.startShizuku() }),
		actA("System", "adb remount", "", "online", func(m *Model) tea.Cmd { return m.hostAdb("adb remount", "remount") }),
		actA("System", "Bugreport → file", "", "online", func(m *Model) tea.Cmd {
			core.MkdirP(core.Cfg.PullDir)
			m.push(newTask("adb bugreport", [][]string{{"adb", "-s", m.tgt().serial(), "bugreport", core.Cfg.PullDir}}, nil))
			return nil
		}),
	}
}

// filesNetworkActions: pulling and pushing files, port forwarding.
func (m *Model) filesNetworkActions() []action {
	return []action{
		actA("Files", "Pull a path from the device…", "", "online", func(m *Model) tea.Cmd {
			m.ask("Remote path to pull", "/sdcard/", func(m *Model, r string) tea.Cmd {
				if r != "" {
					core.MkdirP(core.Cfg.PullDir)
					m.push(newTask("Pull — "+r, [][]string{{"adb", "-s", m.tgt().serial(), "pull", r, core.Cfg.PullDir + "/"}}, nil))
				}
				return nil
			})
			return nil
		}),
		actA("Files", "Push a local file…", "", "online", func(m *Model) tea.Cmd {
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
		actA("Network", "Port forward / reverse…", "", "online", func(m *Model) tea.Cmd { return m.ports() }),
	}
}

// connectActions: adb connection management.
func (m *Model) connectActions() []action {
	conn := m.tabs["connect"].(*connectTab)
	return []action{
		actA("Connect", "Pair with a QR code (Android 11+)", "p", "any", conn.qrPair),
		actA("Connect", "Pair with a pairing code…", "", "any", conn.codePair),
		actA("Connect", "Connect to host[:port]…", "c", "any", conn.connectPrompt),
		actA("Connect", "Scan the LAN for adb (:5555)", "", "any", conn.scan),
		actA("Connect", "Use a remote adb server…", "", "any", conn.remoteServer),
		actA("Connect", "Restart the adb server", "", "any", conn.restartServer),
		actA("Connect", "Open the Connect tab", "", "any", actGoTab("connect")),
	}
}

// hostActions: things about android-tui and the host machine itself.
func (m *Model) hostActions() []action {
	return []action{
		actA("Host", "Zoom main panel (toggle)", "Z", "any", func(m *Model) tea.Cmd { m.zoom = !m.zoom; m.focus = fMain; return nil }),
		actA("Host", "Doctor (KVM, display, SDK, AVDs)", "D", "any", func(m *Model) tea.Cmd { return m.doctor() }),
		actA("Host", "SDK updates & maintenance…", "u", "any", func(m *Model) tea.Cmd { return m.maintenance() }),
		actA("Host", "Edit android-tui config", "", "any", func(m *Model) tea.Cmd {
			core.MkdirPrivate(core.ConfDir)
			if _, err := os.Stat(core.ConfFile); err != nil {
				_ = core.WritePrivate(core.ConfFile, []byte(core.ConfigTemplate))
			}
			return m.ext(false, append(core.ShellSplit(orStr(core.PickEditor(), "nano")), core.ConfFile)...)
		}),
		actA("Host", "Refresh now", "F5", "any", func(m *Model) tea.Cmd { m.tab().Reload(); return m.refreshNow() }),
	}
}

// navigationActions: jump to a tab, open a full terminal shell, show the keyboard reference.
func (m *Model) navigationActions() []action {
	var L []action
	for _, id := range append(append([]string{}, m.order[gEmu]...), "connect") {
		id := id
		L = append(L, actA("Go to", m.tabs[id].Name()+" tab", "", "any", actGoTab(id)))
	}
	L = append(L, actA("Go to", "Interactive adb shell (full terminal)", "t", "online", func(m *Model) tea.Cmd { return m.ext(false, "adb", "-s", m.tgt().serial(), "shell") }),
		actA("Help", "Keyboard reference", "?", "any", func(m *Model) tea.Cmd { m.showHelp(); return nil }))
	return L
}
