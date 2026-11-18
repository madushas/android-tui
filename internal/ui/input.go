package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// One input model for every list, tab and window:
//
//	keyboard  ↑/↓ j/k  PgUp/PgDn  Home/End g/G   move · Enter activate · "." (or Shift-F10) context menu · Esc back
//	mouse     click = select · double-click = activate (Enter) · right-click = select + context menu · wheel = scroll
//
// Mouse messages are normalised here once, so panes, tabs and modals all see the same four gestures.

const (
	wheelStep   = 3                      // lines per wheel notch
	doubleClick = 450 * time.Millisecond // two clicks on the same cell within this window = double-click
)

type mouseKind int

const (
	mClick  mouseKind = iota // left button
	mDouble                  // second left click on the same cell
	mRight                   // right button
	mWheel                   // dy < 0 up, dy > 0 down (in notches)
)

// mouseEv is a normalised mouse gesture in screen coordinates.
type mouseEv struct {
	kind  mouseKind
	x, y  int
	dy    int
	shift bool
}

type clickMemo struct {
	at   time.Time
	x, y int
	ok   bool
}

// normMouse turns a raw message into a gesture. Releases and motion are ignored.
func (m *Model) normMouse(msg tea.MouseMsg) (mouseEv, bool) {
	ev := mouseEv{x: msg.X, y: msg.Y, shift: msg.Shift}
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		ev.kind, ev.dy = mWheel, -1
		return ev, true
	case msg.Button == tea.MouseButtonWheelDown:
		ev.kind, ev.dy = mWheel, 1
		return ev, true
	case msg.Action != tea.MouseActionPress:
		return ev, false
	case msg.Button == tea.MouseButtonRight:
		ev.kind = mRight
		return ev, true
	case msg.Button == tea.MouseButtonLeft:
		now := time.Now()
		if c := m.lastClick; c.ok && now.Sub(c.at) < doubleClick && c.x == msg.X && c.y == msg.Y {
			m.lastClick.ok = false // a third click starts over
			ev.kind = mDouble
			return ev, true
		}
		m.lastClick = clickMemo{at: now, x: msg.X, y: msg.Y, ok: true}
		ev.kind = mClick
		return ev, true
	}
	return ev, false
}

// Optional capabilities. A modal or tab opts in by implementing the method; everything else keeps working unchanged.

// mouseModal receives normalised gestures; box is the modal's rectangle on screen.
type mouseModal interface {
	mouse(m *Model, ev mouseEv, box rect) (closed bool, then cont)
}

// placedModal positions itself (popups such as the context menu) instead of being centred.
type placedModal interface {
	place(box []string, W, H int) (x, y int)
}

// ctxProvider supplies a tab's own context menu. onRow is false when the click landed on empty space.
type ctxProvider interface {
	Context(m *Model, onRow bool) []menuItem
}

// rowHitter lets the router tell a click on a row from a click on empty space, and anchor a keyboard-opened menu.
type rowHitter interface {
	hitRow(y int) bool // y is the row inside the tab area
	anchorRow() int    // row inside the tab area of the selection, or -1
}

// keyMsgFor builds the key message a hint label stands for ("d", "⏎", "Space"); ok=false for labels we cannot synthesise.
func keyMsgFor(k string) (tea.KeyMsg, bool) {
	switch k {
	case "⏎", "Enter":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "Space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, true
	case "Esc", "Tab", "":
		return tea.KeyMsg{}, false
	}
	if r := []rune(k); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, true
	}
	return tea.KeyMsg{}, false
}
