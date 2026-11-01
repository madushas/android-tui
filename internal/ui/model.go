package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type (
	tickMsg time.Time
	scanMsg struct {
		sc     core.Scan
		host   core.Host
		gen    int
		serial string
		sample *core.Sample
	}
	jobDone struct {
		quiet bool
		k     cont
	}
	hint struct{ k, l string }
)

type focusT int

const (
	fEmu focusT = iota
	fConn
	fSnap
	fMain
)

// modeT groups up to 5 related tabs so the tab strip stays small; m.order[g] is the flattened list.
type modeT struct {
	name string
	ids  []string
}

type groupT int

const (
	gEmu groupT = iota
	gConn
)

type snapEnt struct{ name, size string }

type rect struct{ x, y, w, h int }

func (r rect) in(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// target is whatever the user has selected: an emulator (AVD) or an adb connection.
type target struct {
	emu  *core.Emu
	conn *core.Conn
}

func (t target) valid() bool { return t.emu != nil || t.conn != nil }
func (t target) isEmu() bool { return t.emu != nil }
func (t target) serial() string {
	switch {
	case t.emu != nil:
		return t.emu.Serial
	case t.conn != nil:
		return t.conn.Serial
	}
	return ""
}
func (t target) online() bool {
	return (t.emu != nil && t.emu.Online()) || (t.conn != nil && t.conn.Online())
}
func (t target) name() string {
	switch {
	case t.emu != nil:
		return t.emu.Name
	case t.conn != nil:
		return t.conn.Name()
	}
	return ""
}
func (t target) state() string {
	switch {
	case t.emu != nil:
		return t.emu.State
	case t.conn != nil:
		return t.conn.State
	}
	return ""
}
func (t target) console() bool { return t.online() && strings.HasPrefix(t.serial(), "emulator-") }
func (t target) key() string {
	switch {
	case t.emu != nil:
		return "e:" + t.emu.Name
	case t.conn != nil:
		return "c:" + t.conn.Serial
	}
	return ""
}

// Model is the root Bubble Tea model.
type Model struct {
	w, h    int
	sc      core.Scan
	host    core.Host
	hostMu  sync.Mutex
	scanned bool

	emuSel, emuTop   int
	connSel, connTop int
	snapSel, snapTop int
	focus            focusT
	grp              groupT
	tabIdx           [2]int // index into order[grp] (flat)
	modes            [2][]modeT
	lastTab          [2][]int // last tab used in each mode (flat index)
	modeHits         [][3]int // x0, x1, mode index
	tabs             map[string]Tab
	order            [2][]string

	modals  []modal
	toast   string
	toastK  string
	until   time.Time
	busy    int
	frame   int
	logMode int

	stats       map[string]*core.DevStats
	info        map[string]map[string]string
	profiles    map[string]*core.Profile
	profLoading map[string]bool
	infoLoading map[string]bool
	snaps       []snapEnt
	snapsFor    string
	snapsT      time.Time
	selKey      string
	selChanged  time.Time
	upd         int
	rec         *recording

	pollGen    int
	pollN      int
	pollSerial string
	rects      map[string]rect
	tabHits    [][3]int // x0, x1, tab index
	haveAdb    bool     // adb is on PATH (checked once at start)
	zoom       bool     // main panel fills the screen (Z); any focus change to another pane leaves zoom
	quitting   bool
}

type recording struct {
	serial, remote, name string
	proc                 *os.Process
	t0                   time.Time
}

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
func (m *Model) pollCmd(delay time.Duration) tea.Cmd {
	gen, serial := m.pollGen, m.pollSerial
	full := m.pollN%5 == 0
	m.pollN++
	return func() tea.Msg {
		time.Sleep(delay)
		sc := core.Scn.Scan()
		m.hostMu.Lock()
		m.host.Sample()
		h := m.host
		m.hostMu.Unlock()
		msg := scanMsg{sc: sc, host: h, gen: gen, serial: serial}
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

func (m *Model) tgt() target {
	if m.grp == gEmu {
		if m.emuSel >= 0 && m.emuSel < len(m.sc.Emus) {
			return target{emu: &m.sc.Emus[m.emuSel]}
		}
		return target{}
	}
	if m.connSel >= 0 && m.connSel < len(m.sc.Conns) {
		return target{conn: &m.sc.Conns[m.connSel]}
	}
	return target{}
}

func (m *Model) curTabs() []Tab {
	var ts []Tab
	for _, id := range m.order[m.grp] {
		ts = append(ts, m.tabs[id])
	}
	return ts
}

// curIdx is the current flat tab index (clamped).
func (m *Model) curIdx() int { return min(m.tabIdx[m.grp], len(m.order[m.grp])-1) }

// modeRange returns the current mode's index and the [lo,hi) range of its tabs in m.order[m.grp].
func (m *Model) modeRange() (mi, lo, hi int) {
	idx, off := m.curIdx(), 0
	for i, md := range m.modes[m.grp] {
		if idx < off+len(md.ids) {
			return i, off, off + len(md.ids)
		}
		off += len(md.ids)
	}
	return 0, 0, len(m.order[m.grp])
}

// selectMode jumps to mode ni, resuming the tab last used there.
func (m *Model) selectMode(ni int) {
	ms := m.modes[m.grp]
	if ni < 0 || ni >= len(ms) {
		return
	}
	mi, _, _ := m.modeRange()
	m.lastTab[m.grp][mi] = m.curIdx()
	start := 0
	for i := 0; i < ni; i++ {
		start += len(ms[i].ids)
	}
	to := start
	if l := m.lastTab[m.grp][ni]; l >= start && l < start+len(ms[ni].ids) {
		to = l
	}
	m.tabIdx[m.grp] = to
	m.checkSelect()
}

func (m *Model) switchMode(d int) {
	mi, _, _ := m.modeRange()
	n := len(m.modes[m.grp])
	m.selectMode((mi + d + n) % n)
}

func (m *Model) clickMode(x int) {
	for _, h := range m.modeHits {
		if x >= h[0] && x < h[1] {
			m.selectMode(h[2])
		}
	}
}

func (m *Model) tab() Tab {
	return m.tabs[m.order[m.grp][m.curIdx()]]
}

func (m *Model) gotoTab(id string) {
	groups := []groupT{m.grp, gEmu, gConn} // prefer the group the user is in
	for _, g := range groups {
		for i, x := range m.order[g] {
			if x == id {
				m.grp, m.tabIdx[g] = g, i
				m.focus = fMain
				m.checkSelect()
				return
			}
		}
	}
}

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
			cmds = append(cmds, m.modalMsg(msg))
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

type execDone struct{ err error }
type execThen struct {
	then func(m *Model, err error) tea.Cmd
	err  error
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

func (m *Model) clickTab(x int) {
	for _, h := range m.tabHits {
		if x >= h[0] && x < h[1] {
			m.tabIdx[m.grp] = h[2]
		}
	}
}

var _ = fmt.Sprint
