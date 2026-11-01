package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type overviewTab struct {
	baseTab
	cfgT map[string]time.Time
	cfgV map[string]map[string]string
	jT   time.Time // journal count cache (don't re-read the file on every frame)
	jN   map[string]int
	jBad map[string]int // applied but did not verify ("didn't stick")
}

func newOverview() Tab {
	return &overviewTab{baseTab: baseTab{ident: "overview", name: "Overview"}, cfgT: map[string]time.Time{}, cfgV: map[string]map[string]string{}, jN: map[string]int{}, jBad: map[string]int{}}
}

func (o *overviewTab) Hints() []hint {
	return []hint{{"s", "start"}, {"S", "screenshot"}, {"I", "install apk"}, {"t", "shell"}, {"b/o/j/z", "next steps"}, {":", "actions"}}
}

func (o *overviewTab) facts(name string) map[string]string {
	if time.Since(o.cfgT[name]) > 5*time.Second {
		c := core.CfgPath(name)
		v := map[string]string{}
		for _, k := range []string{"hw.device.name", "hw.ramSize", "hw.cpu.ncore", "hw.gpu.mode", "hw.lcd.density", "hw.lcd.width", "hw.lcd.height", "disk.dataPartition.size", "sdcard.size"} {
			v[k] = core.IniGet(c, k)
		}
		v["_snaps"] = fmt.Sprint(len(core.SnapList(name)))
		v["_path"] = core.AVDDir(name)
		o.cfgV[name], o.cfgT[name] = v, time.Now()
	}
	return o.cfgV[name]
}

// revertible counts journal entries for a device that can still be undone (cached for 3s).
func (o *overviewTab) revertible(serial string) int {
	if time.Since(o.jT) > 3*time.Second {
		o.jN, o.jBad = map[string]int{}, map[string]int{}
		for _, e := range core.LoadJournal() {
			if !e.Reverted && !e.Irrev && e.Err == "" {
				o.jN[e.Serial]++
				if !e.Verified {
					o.jBad[e.Serial]++
				}
			}
		}
		o.jT = time.Now()
	}
	return o.jN[serial]
}

type dashStep struct {
	key, text string
	warn      bool
}

// steps are the dashboard's "next best actions": cheap, already-known facts only (no extra adb calls).
func (o *overviewTab) steps(m *Model, pr *core.Profile, st *core.DevStats) []dashStep {
	var ss []dashStep
	rev := o.revertible(pr.Serial)
	if n := o.jBad[pr.Serial]; n > 0 {
		ss = append(ss, dashStep{"j", fmt.Sprintf("%d change(s) didn't stick — a Google flag sync or the OEM may revert them; review in Journal", n), true})
	}
	if st != nil && st.DFTotal > 0 {
		if p := 100 * float64(st.DFUsed) / float64(st.DFTotal); p >= 90 {
			ss = append(ss, dashStep{"", fmt.Sprintf("Storage %.0f%% full (%s free)", p, human(float64(st.DFTotal-st.DFUsed))), true})
		}
	}
	if st != nil && st.Bat != nil && st.Bat.Temp >= 42 {
		ss = append(ss, dashStep{"", fmt.Sprintf("Battery is warm (%.1f°C)", st.Bat.Temp), true})
	}
	if !pr.Root && pr.ShizukuInstalled && !pr.ShizukuRunning {
		ss = append(ss, dashStep{"z", "Shizuku is installed but not running — start it over adb", true})
	}
	if d, ok := m.tabs["debloat"].(*debloatTab); ok && d.loaded {
		if n := len(d.ents); n > 0 {
			safe := 0
			for _, e := range d.ents {
				if e.Risk == "safe" {
					safe++
				}
			}
			ss = append(ss, dashStep{"b", fmt.Sprintf("%d known bloat package(s) found (%d marked safe to disable)", n, safe), false})
		}
	} else {
		ss = append(ss, dashStep{"b", "Debloat: see which known bloat packages are on this device", false})
	}
	if tw, ok := m.tabs["tweaks"].(*tweaksTab); ok && tw.prof != nil && tw.prof.Serial == pr.Serial {
		n := 0
		for _, it := range tw.items {
			if it.OK && it.Risk == "safe" && it.state == "off" {
				n++
			}
		}
		if n > 0 {
			ss = append(ss, dashStep{"o", fmt.Sprintf("%d safe tweak(s) available but not applied (e.g. animation speed, phantom-process limit)", n), false})
		}
	}
	if c := m.tgt().conn; c != nil && c.Transport == "usb" && pr.SDK >= 30 {
		ss = append(ss, dashStep{"", "Wireless debugging is available (Android 11+): Connect mode › pair by QR to drop the cable", false})
	}
	if m.upd > 0 {
		ss = append(ss, dashStep{"", fmt.Sprintf("%d Android SDK update(s) available — palette › SDK updates & maintenance", m.upd), false})
	}
	if rev == 0 {
		ss = append(ss, dashStep{"o", "No optimisations applied yet — Tweaks lists what fits this device (all revertible)", false})
	} else {
		ss = append(ss, dashStep{"j", fmt.Sprintf("%d revertible change(s) — Ctrl-Z undoes the last one", rev), false})
	}
	return ss
}

// Key: dashboard shortcuts for the suggestions above.
func (o *overviewTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	if !m.tgt().online() {
		return false, nil
	}
	switch k.String() {
	case "b":
		m.gotoTab("debloat")
	case "o":
		m.gotoTab("tweaks")
	case "j":
		m.gotoTab("journal")
	case "z":
		return true, m.startShizuku()
	default:
		return false, nil
	}
	return true, nil
}

func kv(k, v string) string { return sDim.Render(padR(k, 9)) + v }

func section(t string, w int) string {
	return sDim.Render("─ " + t + " " + strings.Repeat("─", max(0, w-sw(t)-3)))
}

func meter(label string, frac float64, text string, st lipglossStyle, w int) string {
	bw := max(8, min(w-9-sw(text)-1, 48))
	return sBold.Render(padR(label, 5)) + st.Render(bar(frac, bw)) + " " + sBold.Render(text)
}

func (o *overviewTab) View(m *Model, w, h int, focused bool) []string {
	t := m.tgt()
	if !t.valid() {
		return []string{"", "  " + sDim.Render("Nothing selected. Press n for a new AVD, or p to pair a phone.")}
	}
	w -= 2
	g, gs := m.glyph(t.state(), t.isEmu())
	head := " " + sty(gs).Render(g) + " " + sBold.Render(t.name()) + sty(gs).Render("  "+t.state())
	if s := t.serial(); s != "" {
		head += sDim.Render("   " + s)
	}
	if t.isEmu() {
		head += sDim.Render("   " + t.emu.Info.String())
		if t.emu.Custom {
			head += sMag.Render("   ★ custom ROM")
		}
	} else if t.conn != nil {
		head += sDim.Render("   [" + t.conn.Transport + "]")
	}
	out := []string{head, ""}
	add := func(s string) { out = append(out, " "+s) }
	info := m.devInfo()
	st := m.stat()
	if t.online() {
		if info != nil {
			out[1] = " " + sDim.Render(fmt.Sprintf("%s %s · Android %s (API %s) · %s", info["ro.product.manufacturer"], info["ro.product.model"],
				info["ro.build.version.release"], info["ro.build.version.sdk"], info["ro.product.cpu.abi"]))
		}
		gh := 1
		switch {
		case h >= 38:
			gh = 3
		case h >= 28:
			gh = 2
		case h < 22:
			gh = 0
		}
		if st != nil && len(st.CPU) > 0 {
			cpu := st.CPU[len(st.CPU)-1]
			add(meter("CPU", cpu/100, fmt.Sprintf("%5.1f%%", cpu), loadStyle(cpu), w))
			for _, l := range graph(st.CPU, w-5, gh, 100) {
				add("     " + loadStyle(cpu).Render(l))
			}
			if st.MemTotal > 0 {
				mp := 100 * float64(st.MemUsed) / float64(st.MemTotal)
				add(meter("MEM", mp/100, fmt.Sprintf("%s/%s %3.0f%%", human(float64(st.MemUsed)), human(float64(st.MemTotal)), mp), loadStyle(mp), w))
				for _, l := range graph(st.Mem, w-5, gh, 100) {
					add("     " + loadStyle(mp).Render(l))
				}
			}
			var rx, tx float64
			if n := len(st.RX); n > 0 {
				rx, tx = st.RX[n-1], st.TX[n-1]
			}
			add(sBold.Render("NET  ") + sOK.Render("↓ "+human(rx)+"/s") + "     " + sAcc.Render("↑ "+human(tx)+"/s"))
			if gh > 0 {
				vm := 1024.0
				for _, v := range append(append([]float64{}, st.RX...), st.TX...) {
					vm = max(vm, v)
				}
				add("   " + sOK.Render("↓ "+spark(st.RX, w-5, vm)))
				add("   " + sAcc.Render("↑ "+spark(st.TX, w-5, vm)))
			}
			if b := st.Bat; b != nil {
				status := map[int]string{2: "charging", 3: "discharging", 4: "not charging", 5: "full"}[b.Status]
				if status == "" {
					status = "unknown"
				}
				pw := ""
				if b.AC {
					pw = " · AC"
				} else if b.USB {
					pw = " · USB"
				}
				bs := sOK
				if b.Level <= 20 {
					bs = sErr
				}
				add(meter("BAT", float64(b.Level)/100, fmt.Sprintf("%3d%%", b.Level), bs, w) + sDim.Render(fmt.Sprintf("  %s%s · %.1f°C", status, pw, b.Temp)))
			}
			if st.DFTotal > 0 {
				p := 100 * float64(st.DFUsed) / float64(st.DFTotal)
				add(meter("DISK", p/100, fmt.Sprintf("%s/%s /data", human(float64(st.DFUsed)), human(float64(st.DFTotal))), loadStyle(p), w))
			}
			up := int(st.Up)
			add(sBold.Render("UP   ") + sDim.Render(fmt.Sprintf("%dh %dm   (sampled every %gs)", up/3600, up%3600/60, core.Cfg.RefreshSecs)))
		} else {
			add(sDim.Render(spinner(m.frame) + " sampling device…"))
		}
		out = append(out, "")
		if info != nil && len(out)+6 < h {
			add(section("Device", w))
			add(kv("Build", info["ro.build.display.id"]))
			add(kv("Screen", strings.TrimPrefix(info["wm"], "Physical size: ")+" @ "+strings.TrimPrefix(info["dpi"], "Physical density: ")+"dpi"))
			loc := info["persist.sys.locale"]
			if loc == "" {
				loc = info["ro.product.locale"]
			}
			add(kv("Locale", loc))
			add(kv("Type", info["ro.build.type"]+"  (adb root works on 'userdebug'/'eng' images)"))
			out = append(out, "")
		}
		if pr := m.profile(); pr != nil && len(out)+6 < h {
			add(section("Profile", w))
			add(kv("Class", pr.Summary()))
			add(kv("Brands", strings.Join(pr.Brands, ", ")))
			add(kv("Changes", fmt.Sprintf("%d revertible  (see the Journal tab)", o.revertible(pr.Serial))))
			out = append(out, "")
		}
		if pr := m.profile(); pr != nil && len(out)+4 < h {
			if ss := o.steps(m, pr, st); len(ss) > 0 {
				add(section("Next steps", w))
				for _, s := range ss {
					mark, sty := "•", sDim
					if s.warn {
						mark, sty = "!", sWarn
					}
					line := sty.Render(mark + " " + s.text)
					if s.key != "" {
						line += "  " + sAccB.Render("["+s.key+"]")
					}
					add(line)
				}
				out = append(out, "")
			}
		}
	} else if t.isEmu() {
		if t.emu.State == "booting" {
			add(sWarn.Render(spinner(m.frame) + " booting… telemetry starts when Android finishes starting."))
		} else {
			add(sDim.Render("Stopped.  s start · c cold boot · H headless · e edit hardware · : all actions"))
		}
		out = append(out, "")
	} else if t.conn != nil && t.conn.State == "saved" {
		add(sDim.Render("Saved connection — press Enter in the connections panel to connect."))
		out = append(out, "")
	}
	if t.isEmu() && len(out)+4 < h {
		f := o.facts(t.emu.Name)
		add(section("AVD config", w))
		add(kv("Device", fmt.Sprintf("%s · %sx%s @ %sdpi", orStr(f["hw.device.name"], "custom"), f["hw.lcd.width"], f["hw.lcd.height"], f["hw.lcd.density"])))
		add(kv("Hardware", fmt.Sprintf("%s MB RAM · %s cores · gpu %s", f["hw.ramSize"], f["hw.cpu.ncore"], orStr(f["hw.gpu.mode"], "auto"))))
		add(kv("Storage", fmt.Sprintf("%s data · %s sdcard · %s snapshot(s)", orStr(f["disk.dataPartition.size"], "?"), orStr(f["sdcard.size"], "no"), f["_snaps"])))
		if fl := core.Cfg.AVDFlags[t.emu.Name]; fl != "" {
			add(kv("Flags", fl))
		}
		if t.emu.RSS > 0 {
			add(kv("Host RSS", human(float64(t.emu.RSS)*1024)))
		}
		add(kv("Path", sDim.Render(filepath.ToSlash(f["_path"]))))
	}
	return out
}
