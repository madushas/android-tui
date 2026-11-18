package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ───────────────────────────── the menu popup ─────────────────────────────

// menuItem is one row of a context menu. key is shown on the right and doubles as an accelerator when it is a single
// letter that does not clash with navigation. off, when set, is the reason the item cannot run (it is drawn dim).
type menuItem struct {
	label string
	key   string
	run   func(m *Model) tea.Cmd
	off   string
	sep   bool // draw a separator above
}

type ctxMenuModal struct {
	title  string
	items  []menuItem
	sel    int
	ax, ay int   // anchor in screen cells; ax < 0 centres the menu
	rowOf  []int // box row of each item, filled in by view
}

func newCtxMenu(title string, items []menuItem, x, y int) *ctxMenuModal {
	c := &ctxMenuModal{title: title, items: items, ax: x, ay: y}
	for i, it := range items {
		if it.off == "" {
			c.sel = i
			break
		}
	}
	return c
}

func (c *ctxMenuModal) move(d int) {
	n := len(c.items)
	if n == 0 {
		return
	}
	c.sel = ((c.sel+d)%n + n) % n
}

func (c *ctxMenuModal) activate(m *Model) (bool, cont) {
	if c.sel < 0 || c.sel >= len(c.items) {
		return false, nil
	}
	it := c.items[c.sel]
	if it.off != "" {
		m.say(it.off)
		return false, nil
	}
	if it.run == nil {
		return true, nil
	}
	return true, it.run
}

// navKeys cannot be accelerators because the menu uses them for movement.
const navKeys = "jkhlgGq"

func (c *ctxMenuModal) update(m *Model, msg tea.Msg) (bool, cont) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch s := k.String(); s {
	case "up", "k", "ctrl+p", "shift+tab":
		c.move(-1)
	case "down", "j", "ctrl+n", "tab":
		c.move(1)
	case "pgup", "home", "g":
		c.sel = 0
	case "pgdown", "end", "G":
		c.sel = len(c.items) - 1
	case "enter", " ", "right", "l":
		return c.activate(m)
	case "esc", "q", "left", "h", "ctrl+c", ".":
		return true, nil
	default:
		if len(s) == 1 && !strings.Contains(navKeys, s) {
			for i, it := range c.items {
				if it.key == s {
					c.sel = i
					return c.activate(m)
				}
			}
		}
	}
	return false, nil
}

func (c *ctxMenuModal) mouse(m *Model, ev mouseEv, box rect) (bool, cont) {
	in := box.in(ev.x, ev.y)
	switch ev.kind {
	case mWheel:
		c.move(ev.dy)
		return false, nil
	case mRight:
		return !in, nil // right-click outside dismisses; inside does nothing
	}
	if !in {
		return true, nil
	}
	for i := range c.items {
		if i < len(c.rowOf) && box.y+c.rowOf[i] == ev.y {
			c.sel = i
			return c.activate(m)
		}
	}
	return false, nil
}

func (c *ctxMenuModal) place(box []string, W, H int) (int, int) {
	bw, bh := 0, len(box)
	if bh > 0 {
		bw = sw(box[0])
	}
	if c.ax < 0 || c.ay < 0 {
		return centerXY(box, W, H)
	}
	x, y := c.ax, c.ay
	if x+bw > W {
		x = W - bw
	}
	if y+bh > H-1 {
		y = H - 1 - bh
	}
	return max(0, x), max(0, y)
}

func (c *ctxMenuModal) view(w, h int) []string {
	iw := 26
	for _, it := range c.items {
		iw = max(iw, sw(it.label)+sw(it.key)+6)
	}
	iw = min(iw, w-4)
	var body []string
	c.rowOf = c.rowOf[:0]
	for i, it := range c.items {
		if it.sep && i > 0 {
			body = append(body, sDim.Render(strings.Repeat("─", iw)))
		}
		c.rowOf = append(c.rowOf, len(body)+1) // +1: the frame's top border
		pad := max(1, iw-sw(it.label)-sw(it.key)-2)
		line := fit(" "+clipS(it.label, iw-sw(it.key)-3)+strings.Repeat(" ", pad)+it.key+" ", iw)
		switch {
		case i == c.sel:
			line = sSel.Render(line)
		case it.off != "":
			line = sDim.Render(line)
		}
		body = append(body, line)
	}
	foot := "↑↓ ⏎ · Esc"
	if c.sel >= 0 && c.sel < len(c.items) && c.items[c.sel].off != "" {
		foot = clipS(c.items[c.sel].off, iw)
	}
	return frame(" "+clipS(c.title, iw-2)+" ", body, iw+2, foot, false)
}

// ───────────────────────────── opening menus ─────────────────────────────

// openContext shows the context menu for whatever has focus, anchored at a screen position (x < 0 centres it).
func (m *Model) openContext(x, y int, onRow bool) tea.Cmd {
	title, items := m.contextItems(onRow)
	if len(items) == 0 {
		return nil
	}
	m.push(newCtxMenu(title, items, x, y))
	return nil
}

// openContextAtSelection is the keyboard route: "." or Shift-F10 opens the menu next to the selected row.
func (m *Model) openContextAtSelection() tea.Cmd {
	x, y := -1, -1
	switch m.focus {
	case fEmu:
		r := m.rects["emu"]
		x, y = r.x+4, r.y+1+m.emuSel-m.emuTop
	case fConn:
		r := m.rects["conn"]
		x, y = r.x+4, r.y+1+m.connSel-m.connTop
	case fSnap:
		r := m.rects["snap"]
		x, y = r.x+4, r.y+1+m.snapSel-m.snapTop
	default:
		r := m.rects["main"]
		if h, ok := m.tab().(rowHitter); ok {
			if a := h.anchorRow(); a >= 0 {
				x, y = r.x+6, r.y+2+a+1
			}
		}
	}
	return m.openContext(x, y, true)
}

func (m *Model) contextItems(onRow bool) (string, []menuItem) {
	var title string
	var items []menuItem
	grouped := false
	switch m.focus {
	case fEmu:
		title, items = m.emuMenu()
	case fConn:
		title, items = m.connMenu()
	case fSnap:
		title, items = m.snapMenu()
	default:
		tab := m.tab()
		title = tab.Name()
		if t, ok := tab.(interface{ ContextTitle() string }); ok {
			if s := t.ContextTitle(); s != "" {
				title = s
			}
		}
		if p, ok := tab.(ctxProvider); ok {
			items = p.Context(m, onRow)
		} else {
			items = hintItems(tab.Hints())
		}
		items = append(items, menuItem{label: "Zoom main panel", key: "Z", sep: len(items) > 0, run: func(m *Model) tea.Cmd {
			m.zoom, m.focus = !m.zoom, fMain
			return nil
		}})
		grouped = true // the palette and help share the Zoom group
	}
	items = append(items,
		menuItem{label: "Actions palette…", key: ":", sep: len(items) > 0 && !grouped, run: func(m *Model) tea.Cmd { return m.palette() }},
		menuItem{label: "Help", key: "?", run: func(m *Model) tea.Cmd { m.showHelp(); return nil }})
	return title, items
}

// mainKey sends a key to the focused tab, exactly as if it had been typed (menus can never drift from the keyboard).
func mainKey(msg tea.KeyMsg) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		m.focus = fMain
		_, c := m.tab().Key(m, msg)
		return c
	}
}

// hintItems derives a menu from a tab's key hints: every hint with a key we can synthesise becomes an item.
// "d/e" + "disable/enable" becomes two items.
func hintItems(hs []hint) []menuItem {
	var out []menuItem
	for _, h := range hs {
		keys, labels := strings.Split(h.k, "/"), strings.Split(h.l, "/")
		for i, k := range keys {
			msg, ok := keyMsgFor(strings.TrimSpace(k))
			if !ok {
				continue
			}
			label := h.l
			switch {
			case len(labels) == len(keys):
				label = labels[i]
			case i > 0:
				continue // "⏎/l open": one action with an alternative key; list it once
			}
			out = append(out, menuItem{label: tidy(label), key: strings.TrimSpace(k), run: mainKey(msg)})
		}
	}
	return out
}

// tidy normalises a hint label for a menu: "REVERT ALL" → "Revert all", "disable" → "Disable".
func tidy(s string) string {
	s = strings.TrimSpace(s)
	if s == strings.ToUpper(s) {
		s = strings.ToLower(s)
	}
	return capital(s)
}

func capital(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

// ───────────────────────────── pane menus ─────────────────────────────

// spec is "label|key" or "label|key|reason-it-is-off"; "-" starts a new group.
func paneItems(specs [][3]string, run func(key string) func(m *Model) tea.Cmd) []menuItem {
	var out []menuItem
	sep := false
	for _, s := range specs {
		if s[0] == "-" {
			sep = true
			continue
		}
		out = append(out, menuItem{label: s[0], key: s[1], off: s[2], sep: sep, run: run(s[1])})
		sep = false
	}
	return out
}

func (m *Model) emuMenu() (string, []menuItem) {
	run := func(key string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { m.focus = fEmu; return m.keyEmu(key) }
	}
	e := m.emuTarget()
	if e == nil {
		return "Emulators", paneItems([][3]string{{"New AVD…", "n", ""}, {"SDK updates & maintenance…", "u", ""}, {"Doctor", "D", ""}}, run)
	}
	running, online := e.Active(), e.Online()
	off := func(cond bool, why string) string {
		if cond {
			return why
		}
		return ""
	}
	notRunning := off(!online, "needs a running emulator")
	power := []menuItem{
		{label: "Root with rootAVD… (power users)", sep: true, off: notRunning, run: func(m *Model) tea.Cmd { return m.rootAVD() }},
		{label: "Restore stock ramdisk…", off: off(running, "stop the emulator first"), run: func(m *Model) tea.Cmd { return m.restoreRamdisk() }},
	}
	return e.Name, append(paneItems([][3]string{
		{"Start", "s", off(running, "already "+e.State)},
		{"Cold boot", "c", off(running, "already "+e.State)},
		{"Start headless", "H", off(running, "already "+e.State)},
		{"Stop", "x", off(!running, "not running")},
		{"-", "", ""},
		{"Edit hardware…", "e", ""},
		{"Change system image…", "i", ""},
		{"Launch flags…", "F", ""},
		{"Rename…", "R", ""},
		{"Clone…", "C", ""},
		{"Edit raw config…", "E", ""},
		{"-", "", ""},
		{"Screenshot", "S", notRunning},
		{"Install APK…", "I", notRunning},
		{"Open adb shell", "t", notRunning},
		{"Mirror screen (scrcpy)", "M", notRunning},
		{"Emulator log", "o", ""},
		{"-", "", ""},
		{"Factory reset…", "W", ""},
		{"Delete…", "d", ""},
		{"-", "", ""},
		{"New AVD…", "n", ""},
	}, run), power...)
}

func (m *Model) connMenu() (string, []menuItem) {
	run := func(key string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { m.focus = fConn; return m.keyConn(key) }
	}
	pairing := [][3]string{{"-", "", ""}, {"Pair by QR code…", "p", ""}, {"Connect by address…", "c", ""}, {"Add saved connection…", "a", ""}}
	if m.connSel < 0 || m.connSel >= len(m.sc.Conns) {
		return "Connections", paneItems(pairing[1:], run)
	}
	c := m.sc.Conns[m.connSel]
	notOnline := ""
	if !c.Online() {
		notOnline = "device is not online"
	}
	first, drop := [3]string{"Open in main panel", "enter", ""}, [3]string{"Disconnect", "d", ""}
	switch {
	case c.State == "saved":
		first, drop = [3]string{"Connect", "enter", ""}, [3]string{"Forget saved connection", "d", ""}
	case !strings.HasPrefix(c.Transport, "wifi"):
		drop[2] = "USB devices can't be disconnected — unplug the cable"
	}
	save := [3]string{"Save for later", "s", ""}
	if c.State == "saved" || !strings.HasPrefix(c.Transport, "wifi") {
		save[2] = "only Wi-Fi devices can be saved"
	}
	specs := [][3]string{first, drop, save, {"-", "", ""},
		{"Screenshot", "S", notOnline}, {"Install APK…", "I", notOnline}, {"Open adb shell", "t", notOnline}, {"Mirror screen (scrcpy)", "M", notOnline}}
	return orStr(c.Model, c.Serial), paneItems(append(specs, pairing...), run)
}

func (m *Model) snapMenu() (string, []menuItem) {
	run := func(key string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { m.focus = fSnap; return m.keySnap(key) }
	}
	e := m.emuTarget()
	if e == nil {
		return "Snapshots", nil
	}
	none, notRunning := "", ""
	if m.snapSel < 0 || m.snapSel >= len(m.snaps) {
		none = "no snapshot selected"
	}
	if !e.Online() {
		notRunning = "needs a running emulator"
	}
	return "Snapshots", paneItems([][3]string{
		{"Boot from this snapshot", "enter", none},
		{"Load into running emulator", "l", orStr(none, notRunning)},
		{"Delete…", "d", none},
		{"-", "", ""},
		{"Save new snapshot…", "n", notRunning},
	}, run)
}
