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
	cfgT   map[string]time.Time
	cfgV   map[string]map[string]string
	jT     time.Time // journal count cache (don't re-read the file on every frame)
	jN     map[string]int
	jBad   map[string]int // applied but did not verify ("didn't stick")
	scroll int            // first visible line when the content is taller than the panel
	page   int
}

func newOverview() Tab {
	return &overviewTab{baseTab: baseTab{ident: "overview", name: "Overview"}, cfgT: map[string]time.Time{}, cfgV: map[string]map[string]string{}, jN: map[string]int{}, jBad: map[string]int{}}
}

func (o *overviewTab) Hints() []hint {
	return []hint{{"s", "start"}, {"S", "screenshot"}, {"I", "install apk"}, {"t", "shell"}, {"b/o/j/z", "next steps"}, {"↑↓", "scroll"}, {":", "actions"}}
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
	switch k.String() {
	case "up":
		o.scroll = max(0, o.scroll-1)
		return true, nil
	case "down":
		o.scroll++
		return true, nil
	case "pgup", "ctrl+u":
		o.scroll = max(0, o.scroll-max(1, o.page))
		return true, nil
	case "pgdown", "ctrl+d":
		o.scroll += max(1, o.page)
		return true, nil
	case "home":
		o.scroll = 0
		return true, nil
	case "end":
		o.scroll = 1 << 20 // View clamps to the last line
		return true, nil
	}
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

// View scrolls: the page is built at full height, then windowed (↑/↓, PgUp/PgDn, Home/End, wheel).
func (o *overviewTab) View(m *Model, w, h int, focused bool) []string {
	out := o.build(m, w, 400, focused)
	o.page = max(1, h-1)
	o.scroll = max(0, min(o.scroll, len(out)-h))
	if len(out) <= h {
		o.scroll = 0
		return out
	}
	view := append([]string(nil), out[o.scroll:o.scroll+h]...)
	if o.scroll > 0 {
		view[0] = " " + sDim.Render(fmt.Sprintf("↑ %d line(s) above", o.scroll))
	}
	if below := len(out) - o.scroll - h; below > 0 {
		view[h-1] = " " + sDim.Render(fmt.Sprintf("↓ %d more — ↑/↓ PgUp/PgDn Home/End or the wheel", below))
	}
	return view
}

func (o *overviewTab) Scroll(d int) { o.scroll = max(0, o.scroll+d) }

// ovLines collects the Overview's lines (each indented one space) section by section.
type ovLines struct{ out []string }

func (l *ovLines) add(s string) { l.out = append(l.out, " "+s) }
func (l *ovLines) blank()       { l.out = append(l.out, "") }

func (o *overviewTab) build(m *Model, w, h int, focused bool) []string {
	t := m.tgt()
	if !t.valid() {
		return []string{"", "  " + sDim.Render("Nothing selected. Press n for a new AVD, or p to pair a phone.")}
	}
	w -= 2
	L := &ovLines{out: []string{o.header(m, t), ""}}
	info, st := m.devInfo(), m.stat()
	switch {
	case t.online():
		if info != nil {
			L.out[1] = " " + sDim.Render(infoLine(info))
		}
		o.telemetry(m, L, st, w, h)
		L.blank()
		if info != nil && len(L.out)+6 < h {
			deviceSection(L, info, w)
		}
		if pr := m.profile(); pr != nil && len(L.out)+6 < h {
			o.profileSection(L, pr, w)
		}
		if pr := m.profile(); pr != nil && len(L.out)+4 < h {
			o.nextStepsSection(m, L, pr, st, w)
		}
	case t.isEmu():
		if t.emu.State == "booting" {
			L.add(sWarn.Render(spinner(m.frame) + " booting… telemetry starts when Android finishes starting."))
		} else {
			L.add(sDim.Render("Stopped.  s start · c cold boot · H headless · e edit hardware · : all actions"))
		}
		L.blank()
	case t.conn != nil && t.conn.State == "saved":
		L.add(sDim.Render("Saved connection — press Enter in the connections panel to connect."))
		L.blank()
	}
	if t.isEmu() && len(L.out)+4 < h {
		o.avdConfigSection(L, t, w)
	}
	return L.out
}

// header is the first line: state glyph, name, state, serial and (emulator) image info or (adb) transport.
func (o *overviewTab) header(m *Model, t target) string {
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
	return head
}

func infoLine(info map[string]string) string {
	return fmt.Sprintf("%s %s · Android %s (API %s) · %s", info["ro.product.manufacturer"], info["ro.product.model"],
		info["ro.build.version.release"], info["ro.build.version.sdk"], info["ro.product.cpu.abi"])
}

// graphHeight is how tall the CPU/MEM graphs may be in a panel of height h (0 hides them).
func graphHeight(h int) int {
	switch {
	case h >= 38:
		return 3
	case h >= 28:
		return 2
	case h < 22:
		return 0
	}
	return 1
}

// telemetry draws the live meters: CPU, memory, network, battery, disk and uptime.
func (o *overviewTab) telemetry(m *Model, L *ovLines, st *core.DevStats, w, h int) {
	if st == nil || len(st.CPU) == 0 {
		L.add(sDim.Render(spinner(m.frame) + " sampling device…"))
		return
	}
	gh := graphHeight(h)
	cpuMemMeters(L, st, w, gh)
	netMeters(L, st, w, gh)
	if st.Bat != nil {
		batteryMeter(L, st.Bat, w)
	}
	if st.DFTotal > 0 {
		p := 100 * float64(st.DFUsed) / float64(st.DFTotal)
		L.add(meter("DISK", p/100, fmt.Sprintf("%s/%s /data", human(float64(st.DFUsed)), human(float64(st.DFTotal))), loadStyle(p), w))
	}
	up := int(st.Up)
	L.add(sBold.Render("UP   ") + sDim.Render(fmt.Sprintf("%dh %dm   (sampled every %gs)", up/3600, up%3600/60, core.Cfg.RefreshSecs)))
}

func cpuMemMeters(L *ovLines, st *core.DevStats, w, gh int) {
	cpu := st.CPU[len(st.CPU)-1]
	L.add(meter("CPU", cpu/100, fmt.Sprintf("%5.1f%%", cpu), loadStyle(cpu), w))
	for _, l := range graph(st.CPU, w-5, gh, 100) {
		L.add("     " + loadStyle(cpu).Render(l))
	}
	if st.MemTotal > 0 {
		mp := 100 * float64(st.MemUsed) / float64(st.MemTotal)
		L.add(meter("MEM", mp/100, fmt.Sprintf("%s/%s %3.0f%%", human(float64(st.MemUsed)), human(float64(st.MemTotal)), mp), loadStyle(mp), w))
		for _, l := range graph(st.Mem, w-5, gh, 100) {
			L.add("     " + loadStyle(mp).Render(l))
		}
	}
}

func netMeters(L *ovLines, st *core.DevStats, w, gh int) {
	var rx, tx float64
	if n := len(st.RX); n > 0 {
		rx, tx = st.RX[n-1], st.TX[n-1]
	}
	L.add(sBold.Render("NET  ") + sOK.Render("↓ "+human(rx)+"/s") + "     " + sAcc.Render("↑ "+human(tx)+"/s"))
	if gh > 0 {
		vm := 1024.0
		for _, v := range append(append([]float64{}, st.RX...), st.TX...) {
			vm = max(vm, v)
		}
		L.add("   " + sOK.Render("↓ "+spark(st.RX, w-5, vm)))
		L.add("   " + sAcc.Render("↑ "+spark(st.TX, w-5, vm)))
	}
}

func batteryMeter(L *ovLines, b *core.Battery, w int) {
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
	L.add(meter("BAT", float64(b.Level)/100, fmt.Sprintf("%3d%%", b.Level), bs, w) + sDim.Render(fmt.Sprintf("  %s%s · %.1f°C", status, pw, b.Temp)))
}

func deviceSection(L *ovLines, info map[string]string, w int) {
	L.add(section("Device", w))
	L.add(kv("Build", info["ro.build.display.id"]))
	L.add(kv("Screen", strings.TrimPrefix(info["wm"], "Physical size: ")+" @ "+strings.TrimPrefix(info["dpi"], "Physical density: ")+"dpi"))
	loc := info["persist.sys.locale"]
	if loc == "" {
		loc = info["ro.product.locale"]
	}
	L.add(kv("Locale", loc))
	L.add(kv("Type", info["ro.build.type"]+"  (adb root works on 'userdebug'/'eng' images)"))
	L.blank()
}

func (o *overviewTab) profileSection(L *ovLines, pr *core.Profile, w int) {
	L.add(section("Profile", w))
	L.add(kv("Class", pr.Summary()))
	L.add(kv("Brands", strings.Join(pr.Brands, ", ")))
	L.add(kv("Changes", fmt.Sprintf("%d revertible  (see the Journal tab)", o.revertible(pr.Serial))))
	L.blank()
}

func (o *overviewTab) nextStepsSection(m *Model, L *ovLines, pr *core.Profile, st *core.DevStats, w int) {
	ss := o.steps(m, pr, st)
	if len(ss) == 0 {
		return
	}
	L.add(section("Next steps", w))
	for _, s := range ss {
		mark, sty := "•", sDim
		if s.warn {
			mark, sty = "!", sWarn
		}
		line := sty.Render(mark + " " + s.text)
		if s.key != "" {
			line += "  " + sAccB.Render("["+s.key+"]")
		}
		L.add(line)
	}
	L.blank()
}

func (o *overviewTab) avdConfigSection(L *ovLines, t target, w int) {
	f := o.facts(t.emu.Name)
	L.add(section("AVD config", w))
	L.add(kv("Device", fmt.Sprintf("%s · %sx%s @ %sdpi", orStr(f["hw.device.name"], "custom"), f["hw.lcd.width"], f["hw.lcd.height"], f["hw.lcd.density"])))
	L.add(kv("Hardware", fmt.Sprintf("%s MB RAM · %s cores · gpu %s", f["hw.ramSize"], f["hw.cpu.ncore"], orStr(f["hw.gpu.mode"], "auto"))))
	L.add(kv("Storage", fmt.Sprintf("%s data · %s sdcard · %s snapshot(s)", orStr(f["disk.dataPartition.size"], "?"), orStr(f["sdcard.size"], "no"), f["_snaps"])))
	if fl := core.Cfg.AVDFlags[t.emu.Name]; fl != "" {
		L.add(kv("Flags", fl))
	}
	if t.emu.RSS > 0 {
		L.add(kv("Host RSS", human(float64(t.emu.RSS)*1024)))
	}
	L.add(kv("Path", sDim.Render(filepath.ToSlash(f["_path"]))))
}
