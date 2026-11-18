package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

func New() *Model {
	ApplyTheme(themeFromEnv(core.Cfg.Theme))
	adbOK := core.Which("adb")
	m := &Model{stats: map[string]*core.DevStats{}, info: map[string]map[string]string{}, infoLoading: map[string]bool{},
		profiles: map[string]*core.Profile{}, profLoading: map[string]bool{},
		rects: map[string]rect{}, logMode: 1, focus: fEmu, grp: gEmu, selChanged: time.Now(), haveAdb: adbOK}
	m.tabs = map[string]Tab{}
	for _, t := range []Tab{newOverview(), newAVD(), newApps(), newFiles(), newProps(), newLogcat(), newShell(), newControl(), newTweaks(), newDebloat(), newProcs(), newScreen(), newDiff(), newJournal(), newTools(), newSystem(), newImages(), newConnect()} {
		m.tabs[t.id()] = t
	}
	device := modeT{"Device", []string{"overview", "apps", "files", "screen", "shell"}}
	optimize := modeT{"Optimize", []string{"tweaks", "debloat", "procs", "control", "journal"}}
	inspect := modeT{"Inspect", []string{"logcat", "props", "system", "diff", "tools"}}
	m.modes[gEmu] = []modeT{device, optimize, inspect, {"Emulator", []string{"avd", "images"}}}
	m.modes[gConn] = []modeT{device, optimize, inspect, {"Connect", []string{"connect"}}}
	for g := range m.modes {
		for _, md := range m.modes[g] {
			m.order[g] = append(m.order[g], md.ids...)
		}
		m.lastTab[g] = make([]int, len(m.modes[g]))
	}
	m.upd = core.CachedUpdateCount()
	return m
}

func (m *Model) Init() tea.Cmd {
	m.maybeIntro()
	if !m.haveAdb {
		m.report(false, "adb not found on PATH — install Android platform-tools (Doctor: D)")
	}
	core.Logf("info", "android-tui %s · adb: %v · SDK: %s · ? help · : actions", core.Version, core.Which("adb"), orStr(core.SDK, "not found"))
	cmds := []tea.Cmd{m.pollCmd(0), tickCmd(), m.updateCheck()}
	return tea.Batch(cmds...)
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func tickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) updateCheck() tea.Cmd {
	return m.job("sdk updates", true, func() cont {
		n := core.CheckUpdatesIfStale()
		return func(m *Model) tea.Cmd {
			if n >= 0 {
				m.upd = n
			}
			return nil
		}
	})
}

// pollCmd scans devices (and samples the selected device) after delay.
// sampleHost takes one host reading on the poll goroutine. The sampler keeps its own state (the previous CPU totals) and
// hands out a copy; m.host is the snapshot the UI goroutine displays and is never touched from here.
func (m *Model) sampleHost() core.Host {
	m.hostMu.Lock()
	defer m.hostMu.Unlock()
	m.sampler.Sample()
	return m.sampler
}

func (m *Model) pollCmd(delay time.Duration) tea.Cmd {
	gen, serial := m.pollGen, m.pollSerial
	full := m.pollN%5 == 0
	m.pollN++
	return func() tea.Msg {
		time.Sleep(delay)
		sc := core.Scn.Scan()
		msg := scanMsg{sc: sc, host: m.sampleHost(), gen: gen, serial: serial}
		if serial != "" {
			on := false
			for _, e := range sc.Emus {
				on = on || (e.Serial == serial && e.Online())
			}
			for _, c := range sc.Conns {
				on = on || (c.Serial == serial && c.Online())
			}
			if on {
				s := core.ParseStats(core.Sh(serial, core.StatScript(full), core.Opt{Quiet: true, Timeout: 6 * time.Second}).Out)
				msg.sample = &s
			}
		}
		return msg
	}
}

func (m *Model) refreshNow() tea.Cmd {
	m.pollGen++
	return m.pollCmd(0)
}

// job runs fn off the UI goroutine and then executes the returned continuation on it.
func (m *Model) job(label string, quiet bool, fn func() cont) tea.Cmd {
	if !quiet {
		m.busy++
	}
	return func() tea.Msg { return jobDone{quiet: quiet, k: fn()} }
}

func (m *Model) say(msg string) { m.toastWith(msg, "info") }
func (m *Model) report(ok bool, msg string) {
	m.toastWith(msg, map[bool]string{true: "ok", false: "err"}[ok])
}
func (m *Model) toastWith(msg, kind string) {
	m.toast, m.toastK, m.until = msg, kind, time.Now().Add(7*time.Second)
	core.CmdLog.Add(kind, msg)
}

func (m *Model) push(md modal) { m.modals = append(m.modals, md) }

// ─────────────── target / selection ───────────────

func (m *Model) settled() bool { return time.Since(m.selChanged) > 250*time.Millisecond }

func (m *Model) checkSelect() {
	t := m.tgt()
	if k := t.key(); k != m.selKey {
		m.selKey, m.selChanged = k, time.Now()
		m.snaps, m.snapsFor, m.snapSel, m.snapTop = nil, "", 0, 0
	}
	if t.online() {
		m.pollSerial = t.serial()
	} else {
		m.pollSerial = ""
	}
}

func (m *Model) stat() *core.DevStats {
	if s := m.stats[m.tgt().serial()]; s != nil {
		return s
	}
	return nil
}

func (m *Model) devInfo() map[string]string { return m.info[m.tgt().serial()] }

// profile returns the probed device profile (nil until the probe finishes).
func (m *Model) profile() *core.Profile { return m.profiles[m.tgt().serial()] }

// apply runs one journalled change in the background; then(entry) runs on the UI goroutine.
func (m *Model) apply(label string, o core.Op, val string, unset bool, then func(m *Model, e core.JEntry) tea.Cmd) tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	s, prof := t.serial(), m.profile()
	return m.job(label, false, func() cont {
		p := prof
		if p == nil {
			p = core.ProbeDevice(s)
		}
		core.EnsureBaseline(p)
		e := core.ApplyOp(p, fmt.Sprintf("ui@%d", time.Now().UnixNano()), label, o, val, unset)
		return func(m *Model) tea.Cmd {
			m.profiles[s] = p
			switch {
			case e.Err != "":
				m.report(false, label+": "+e.Err)
			case !e.Verified:
				m.report(false, label+": applied, but the value did not stick (a Google flag sync or the OEM may have reverted it)")
			default:
				m.report(true, label+" ✓  (journalled — Ctrl-Z to undo)")
			}
			if then != nil {
				return then(m, e)
			}
			return nil
		}
	})
}

func (m *Model) applyScan(s scanMsg) {
	t := m.tgt()
	key := t.key()
	m.sc, m.host, m.scanned = s.sc, s.host, true
	for i, e := range m.sc.Emus {
		if "e:"+e.Name == key {
			m.emuSel = i
		}
	}
	for i, c := range m.sc.Conns {
		if "c:"+c.Serial == key {
			m.connSel = i
		}
	}
	m.emuSel = max(0, min(m.emuSel, len(m.sc.Emus)-1))
	m.connSel = max(0, min(m.connSel, len(m.sc.Conns)-1))
	live := map[string]bool{}
	for _, e := range m.sc.Emus {
		if e.Online() {
			live[e.Serial] = true
		}
	}
	for _, c := range m.sc.Conns {
		if c.Online() {
			live[c.Serial] = true
		}
	}
	for k := range m.info {
		if !live[k] {
			delete(m.info, k)
		}
	}
	for k := range m.stats {
		if !live[k] {
			delete(m.stats, k)
		}
	}
	for k := range m.profiles {
		if !live[k] {
			delete(m.profiles, k)
		}
	}
	if s.sample != nil && live[s.serial] {
		d := m.stats[s.serial]
		if d == nil {
			d = &core.DevStats{}
			m.stats[s.serial] = d
		}
		d.Apply(*s.sample)
	}
	m.checkSelect()
}

// ─────────────── update ───────────────

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tickMsg:
		m.frame++
		if m.toast != "" && time.Now().After(m.until) {
			m.toast = ""
		}
		cmds = append(cmds, m.tickModals()...)
		cmds = append(cmds, m.tick()...)
		cmds = append(cmds, tickCmd())
	case scanMsg:
		m.applyScan(msg)
		if msg.gen == m.pollGen {
			cmds = append(cmds, m.pollCmd(time.Duration(core.Cfg.RefreshSecs*float64(time.Second))))
		}
	case jobDone:
		if !msg.quiet {
			m.busy--
		}
		if msg.k != nil {
			cmds = append(cmds, msg.k(m))
		}
	case execThen:
		cmds = append(cmds, m.refreshNow())
		if msg.then != nil {
			cmds = append(cmds, msg.then(m, msg.err))
		}
	case execDone:
		cmds = append(cmds, m.refreshNow())
		if msg.err != nil {
			m.report(false, msg.err.Error())
		}
	case tea.KeyMsg:
		if len(m.modals) > 0 {
			cmds = append(cmds, m.modalMsg(msg))
		} else {
			cmds = append(cmds, m.handleKey(msg))
		}
	case tea.MouseMsg:
		if len(m.modals) > 0 {
			cmds = append(cmds, m.modalMouse(msg))
		} else {
			cmds = append(cmds, m.handleMouse(msg))
		}
	}
	if m.quitting {
		m.cleanup()
		return m, tea.Quit
	}
	return m, tea.Batch(cmds...)
}

// tickModals gives streaming modals their per-frame update (autoclose, QR polling).
func (m *Model) tickModals() []tea.Cmd {
	var cmds []tea.Cmd
	if n := len(m.modals); n > 0 {
		if closed, then := m.modals[n-1].update(m, tickMsg{}); closed {
			m.modals = m.modals[:n-1]
			if then != nil {
				cmds = append(cmds, then(m))
			}
		}
	}
	return cmds
}

func (m *Model) modalMsg(msg tea.Msg) tea.Cmd {
	n := len(m.modals)
	closed, then := m.modals[n-1].update(m, msg)
	if closed {
		m.modals = m.modals[:n-1]
		if then != nil {
			return then(m)
		}
	}
	return nil
}

// modalMouse gives the top modal a normalised gesture when it understands them, the raw message otherwise
// (the text viewer scrolls on raw wheel events).
func (m *Model) modalMouse(msg tea.MouseMsg) tea.Cmd {
	n := len(m.modals)
	var closed bool
	var then cont
	if mm, ok := m.modals[n-1].(mouseModal); ok {
		ev, ok := m.normMouse(msg)
		if !ok {
			return nil
		}
		closed, then = mm.mouse(m, ev, m.topBox)
	} else {
		closed, then = m.modals[n-1].update(m, msg)
	}
	if closed {
		m.modals = m.modals[:n-1]
		if then != nil {
			return then(m)
		}
	}
	return nil
}

// tick runs ~8x/s: lazy loads, stream draining, housekeeping.
func (m *Model) tick() []tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range m.tabs {
		t.Bg(m)
	}
	if m.frame%2 == 0 && len(m.modals) == 0 {
		if c := m.tab().Tick(m); c != nil {
			cmds = append(cmds, c)
		}
	}
	t := m.tgt()
	if t.online() && m.info[t.serial()] == nil && !m.infoLoading[t.serial()] && m.settled() {
		s := t.serial()
		m.infoLoading[s] = true
		cmds = append(cmds, m.job("device info", true, func() cont {
			info := core.ReadDeviceInfo(s)
			return func(m *Model) tea.Cmd { delete(m.infoLoading, s); m.info[s] = info; return nil }
		}))
	}
	if t.online() && m.profiles[t.serial()] == nil && !m.profLoading[t.serial()] && m.settled() {
		s := t.serial()
		m.profLoading[s] = true
		cmds = append(cmds, m.job("probe device", true, func() cont {
			p := core.ProbeDevice(s)
			return func(m *Model) tea.Cmd { delete(m.profLoading, s); m.profiles[s] = p; return nil }
		}))
	}
	if t.isEmu() && m.settled() && (m.snapsFor != t.emu.Name || time.Since(m.snapsT) > 8*time.Second) {
		name := t.emu.Name
		m.snapsFor, m.snapsT = name, time.Now()
		cmds = append(cmds, m.job("snapshots", true, func() cont {
			var out []snapEnt
			for _, n := range core.SnapList(name) {
				out = append(out, snapEnt{n, human(float64(core.DirSize(filepath.Join(core.AVDDir(name), "snapshots", n))))})
			}
			return func(m *Model) tea.Cmd {
				if m.snapsFor == name {
					m.snaps = out
				}
				return nil
			}
		}))
	}
	return cmds
}

func (m *Model) cleanup() {
	if lc, ok := m.tabs["logcat"].(*logcatTab); ok {
		lc.stop()
	}
	if m.rec != nil && m.rec.proc != nil {
		_ = m.rec.proc.Kill()
	}
}

// extThen is ext plus a continuation that runs when the program exits.
func (m *Model) extThen(pause bool, then func(m *Model, err error) tea.Cmd, argv ...string) tea.Cmd {
	core.Logf("cmd", "$ %s", core.ShellJoin(argv))
	c := exec.Command(argv[0], argv[1:]...)
	if pause {
		c = exec.Command("sh", append([]string{"-c", `"$@"; rc=$?; printf '\nPress Enter to continue… '; read _x; exit $rc`, "sh"}, argv...)...)
	}
	return tea.ExecProcess(c, func(err error) tea.Msg { return execThen{then, err} })
}

// ext suspends the TUI and runs an interactive program (shell, editor, installers).
func (m *Model) ext(pause bool, argv ...string) tea.Cmd {
	core.Logf("cmd", "$ %s", core.ShellJoin(argv))
	c := exec.Command(argv[0], argv[1:]...)
	if pause {
		c = exec.Command("sh", append([]string{"-c", `"$@"; rc=$?; printf '\nPress Enter to continue… '; read _x; exit $rc`, "sh"}, argv...)...)
	}
	return tea.ExecProcess(c, func(err error) tea.Msg { return execDone{err} })
}
