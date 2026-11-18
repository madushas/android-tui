package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

func (m *Model) startEmu(name, mode string) tea.Cmd {
	return m.act("start "+name, func() (bool, string) { return core.StartMode(name, mode, "") })
}

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
