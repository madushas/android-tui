package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func press(btn tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: btn, Action: tea.MouseActionPress}
}

// Four gestures, whatever the terminal sends: click, double-click (same cell, quickly), right-click, wheel.
func TestNormMouse(t *testing.T) {
	m := &Model{}
	ev, ok := m.normMouse(press(tea.MouseButtonLeft, 5, 7))
	if !ok || ev.kind != mClick || ev.x != 5 || ev.y != 7 {
		t.Fatalf("first click: %+v %v", ev, ok)
	}
	if ev, _ = m.normMouse(press(tea.MouseButtonLeft, 5, 7)); ev.kind != mDouble {
		t.Fatalf("second click on the same cell must be a double-click, got %v", ev.kind)
	}
	if ev, _ = m.normMouse(press(tea.MouseButtonLeft, 5, 7)); ev.kind != mClick {
		t.Fatal("a third click starts over as a single click")
	}
	// each case below arms the double-click memory with one click, then probes with a second
	m.normMouse(press(tea.MouseButtonLeft, 1, 1))
	if ev, _ = m.normMouse(press(tea.MouseButtonLeft, 2, 1)); ev.kind != mClick {
		t.Fatal("a click on a different column is not a double-click")
	}
	m.normMouse(press(tea.MouseButtonLeft, 6, 6))
	if ev, _ = m.normMouse(press(tea.MouseButtonLeft, 6, 7)); ev.kind != mClick {
		t.Fatal("a click one row below is not a double-click")
	}
	m.normMouse(press(tea.MouseButtonLeft, 4, 4))
	m.lastClick.at = time.Now().Add(-time.Second)
	if ev, _ = m.normMouse(press(tea.MouseButtonLeft, 4, 4)); ev.kind != mClick {
		t.Fatal("a slow second click on the same cell is not a double-click")
	}
	if ev, ok = m.normMouse(press(tea.MouseButtonRight, 3, 3)); !ok || ev.kind != mRight {
		t.Fatalf("right click: %+v", ev)
	}
	if ev, ok = m.normMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}); !ok || ev.kind != mWheel || ev.dy != 1 {
		t.Fatalf("wheel down: %+v", ev)
	}
	if ev, _ = m.normMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp}); ev.dy != -1 {
		t.Fatalf("wheel up: %+v", ev)
	}
	if _, ok = m.normMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}); ok {
		t.Fatal("releases must be ignored")
	}
	if _, ok = m.normMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}); ok {
		t.Fatal("motion must be ignored")
	}
}

func TestHintItems(t *testing.T) {
	items := hintItems([]hint{
		{"⏎", "launch"}, {"Space", "mark"}, {"d/e", "disable/enable"}, {"⏎/l", "open"}, {"↑↓", "move"}, {"REVERT ALL", "x"},
		{"Esc", "back"}, {"R", "REVERT ALL"}, {"C-n", "next tab"},
	})
	var got []string
	for _, it := range items {
		got = append(got, it.label+"="+it.key)
	}
	want := "Launch=⏎ Mark=Space Disable=d Enable=e Open=⏎ Revert all=R"
	if strings.Join(got, " ") != want {
		t.Fatalf("got  %q\nwant %q", strings.Join(got, " "), want)
	}
}

func menuOf(labels ...string) (*ctxMenuModal, *[]string) {
	var ran []string
	var items []menuItem
	for _, l := range labels {
		l := l
		it := menuItem{label: l, key: strings.ToLower(l[:1]), run: func(*Model) tea.Cmd { ran = append(ran, l); return nil }}
		if strings.HasPrefix(l, "off:") {
			it.off, it.key = "not available", ""
		}
		items = append(items, it)
	}
	return newCtxMenu("t", items, 10, 5), &ran
}

func TestCtxMenuKeyboard(t *testing.T) {
	key := func(s string) tea.KeyMsg {
		switch s {
		case "down":
			return tea.KeyMsg{Type: tea.KeyDown}
		case "enter":
			return tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			return tea.KeyMsg{Type: tea.KeyEsc}
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	m := &Model{}
	c, ran := menuOf("Alpha", "Bravo", "off:Charlie", "Delta")
	c.update(m, key("down"))
	closed, then := c.update(m, key("enter"))
	if !closed || then == nil {
		t.Fatal("Enter must run the selected item")
	}
	then(m)
	if strings.Join(*ran, ",") != "Bravo" {
		t.Fatalf("ran %v", *ran)
	}
	c, ran = menuOf("Alpha", "Bravo", "off:Charlie", "Delta")
	if closed, then = c.update(m, key("d")); !closed || then == nil { // accelerator
		t.Fatal("an accelerator letter must run its item")
	}
	then(m)
	if strings.Join(*ran, ",") != "Delta" {
		t.Fatalf("accelerator ran %v", *ran)
	}
	c, _ = menuOf("Alpha", "Bravo", "off:Charlie")
	c.update(m, key("down"))
	c.update(m, key("down"))
	if closed, then = c.update(m, key("enter")); closed || then != nil {
		t.Fatal("a disabled item must neither run nor close the menu")
	}
	if closed, _ = c.update(m, key("esc")); !closed {
		t.Fatal("Esc closes")
	}
	// the first enabled item is preselected even when the list starts with a disabled one
	c, _ = menuOf("off:Zero", "One")
	if c.sel != 1 {
		t.Fatalf("selection starts at %d, want the first enabled item (1)", c.sel)
	}
}

func TestCtxMenuMouse(t *testing.T) {
	m := &Model{}
	c, ran := menuOf("Alpha", "Bravo", "off:Charlie")
	box := rect{20, 10, 30, 8}
	c.view(80, 24) // fills the row map
	click := func(k mouseKind, row int) (bool, cont) {
		return c.mouse(m, mouseEv{kind: k, x: box.x + 3, y: box.y + c.rowOf[row]}, box)
	}
	closed, then := click(mClick, 1)
	if !closed || then == nil {
		t.Fatal("clicking an item runs it")
	}
	then(m)
	if strings.Join(*ran, ",") != "Bravo" {
		t.Fatalf("ran %v", *ran)
	}
	if closed, then = click(mClick, 2); closed || then != nil {
		t.Fatal("clicking a disabled item must do nothing")
	}
	if closed, _ = c.mouse(m, mouseEv{kind: mClick, x: 0, y: 0}, box); !closed {
		t.Fatal("clicking outside closes the menu")
	}
	if closed, _ = c.mouse(m, mouseEv{kind: mRight, x: 0, y: 0}, box); !closed {
		t.Fatal("right-clicking outside closes the menu")
	}
	c.sel = 0
	c.mouse(m, mouseEv{kind: mWheel, dy: 1}, box)
	if c.sel != 1 {
		t.Fatalf("wheel moves the selection, got %d", c.sel)
	}
}

func TestCtxMenuPlacement(t *testing.T) {
	c, _ := menuOf("Alpha", "Bravo")
	box := c.view(80, 24)
	bw := sw(box[0])
	c.ax, c.ay = 78, 23 // near the bottom-right corner: it must be pulled back inside the screen
	x, y := c.place(box, 80, 24)
	if x+bw > 80 || y+len(box) > 24 || x < 0 || y < 0 {
		t.Fatalf("menu placed off-screen at %d,%d (size %dx%d)", x, y, bw, len(box))
	}
	c.ax, c.ay = -1, -1
	if x, y = c.place(box, 80, 24); x <= 0 || y <= 0 {
		t.Fatalf("an unanchored menu is centred, got %d,%d", x, y)
	}
}

// The palette and every picker accept the mouse: wheel, click, double-click, click-outside.
func TestChooseModalMouse(t *testing.T) {
	m := &Model{}
	picked := -1
	items := []item{{text: "alpha"}, {text: "beta"}, {text: "gamma"}, {text: "delta"}}
	c := &chooseModal{items: items, flt: newEdit(""), marks: map[int]bool{}, pick: func(m *Model, i int) tea.Cmd { picked = i; return nil }}
	lines := c.view(80, 24)
	box := rect{10, 4, sw(lines[0]), len(lines)}
	at := func(k mouseKind, row int) mouseEv { return mouseEv{kind: k, x: box.x + 4, y: box.y + c.listRow + row} }

	if closed, _ := c.mouse(m, at(mClick, 2), box); closed || c.sel != 2 {
		t.Fatalf("click selects row 2 without closing (sel=%d)", c.sel)
	}
	closed, then := c.mouse(m, at(mDouble, 3), box)
	if !closed || then == nil {
		t.Fatal("double-click confirms")
	}
	then(m)
	if picked != 3 {
		t.Fatalf("picked %d, want 3", picked)
	}
	c.sel = 0
	c.mouse(m, mouseEv{kind: mWheel, dy: 1}, box)
	if c.sel != wheelStep {
		t.Fatalf("wheel moves by %d, got sel=%d", wheelStep, c.sel)
	}
	c.mouse(m, mouseEv{kind: mWheel, dy: -1}, box)
	if c.sel != 0 {
		t.Fatalf("wheel up returns to the top, got %d", c.sel)
	}
	if closed, _ = c.mouse(m, mouseEv{kind: mClick, x: 0, y: 0}, box); !closed {
		t.Fatal("click outside closes")
	}
	multi := &chooseModal{multi: true, items: items, flt: newEdit(""), marks: map[int]bool{}, picks: func(*Model, []int) tea.Cmd { return nil }}
	lines = multi.view(80, 24)
	box = rect{10, 4, sw(lines[0]), len(lines)}
	multi.mouse(m, mouseEv{kind: mClick, x: box.x + 4, y: box.y + multi.listRow + 1}, box)
	if !multi.marks[1] {
		t.Fatal("in a multi-select list a click toggles the mark")
	}
}

func TestConfirmModalButtons(t *testing.T) {
	m := &Model{}
	ran := false
	c := &confirmModal{msg: "Do it?", yes: func(*Model) tea.Cmd { ran = true; return nil }}
	lines := c.view(80, 24)
	box := rect{10, 5, sw(lines[0]), len(lines)}
	if closed, then := c.mouse(m, mouseEv{kind: mClick, x: box.x + c.yesA + 1, y: box.y + c.btnRow}, box); !closed || then == nil {
		t.Fatal("clicking Yes confirms")
	} else {
		then(m)
	}
	if !ran {
		t.Fatal("yes callback not run")
	}
	if closed, then := c.mouse(m, mouseEv{kind: mClick, x: box.x + c.noA + 1, y: box.y + c.btnRow}, box); !closed || then != nil {
		t.Fatal("clicking No cancels without running anything")
	}
	if closed, _ := c.mouse(m, mouseEv{kind: mClick, x: box.x + 1, y: box.y + 1}, box); closed {
		t.Fatal("a click elsewhere in the dialog must not close it")
	}
	if closed, _ := c.mouse(m, mouseEv{kind: mClick, x: box.x + c.yesA + 1, y: box.y + 1}, box); closed {
		t.Fatal("a click in the Yes column but on another row must not confirm")
	}
	if closed, _ := c.mouse(m, mouseEv{kind: mClick, x: box.x + c.noA + 1, y: box.y + c.btnRow - 1}, box); closed {
		t.Fatal("a click in the No column but on another row must not cancel")
	}
	if closed, _ := c.mouse(m, mouseEv{kind: mWheel, dy: 1, x: box.x + c.yesA, y: box.y + c.btnRow}, box); closed {
		t.Fatal("the wheel must not press a button")
	}
}

// rowsTab is the shared list behind 13 tabs: its keyboard navigation, click hit-testing and scrolling must stay uniform.
func TestRowsTabNavigation(t *testing.T) {
	mk := func() *rowsTab {
		r := &rowsTab{page: 3}
		r.rows = []row{{hdr: true, text: "A"}, {text: "1"}, {text: "2"}, {hdr: true, text: "B"}, {text: "3"}, {text: "4"}, {text: "5"}}
		return r
	}
	r := mk()
	for _, c := range []struct {
		key  string
		want int
	}{
		{"home", 1}, {"down", 2}, {"down", 4}, // the "B" header is skipped
		{"up", 2}, {"j", 4}, {"k", 2}, {"end", 6}, {"G", 6}, {"g", 1}, {"pgdown", 5}, {"pgup", 1},
	} {
		if !r.nav(c.key) {
			t.Fatalf("%q is not handled", c.key)
		}
		if r.sel != c.want {
			t.Fatalf("after %q: sel=%d, want %d", c.key, r.sel, c.want)
		}
		if r.rows[r.sel].hdr {
			t.Fatalf("after %q the selection sits on a header", c.key)
		}
	}
	if r.nav("x") {
		t.Fatal("unknown keys must not be swallowed")
	}

	r = mk()
	r.listTop = 2 // two lines of chrome above the list
	if !r.hitRow(3) || !r.hitRow(2+6) {
		t.Fatal("hitRow must accept the list's data rows")
	}
	if r.hitRow(2) || r.hitRow(2+3) {
		t.Fatal("a header row is not a selectable row")
	}
	if r.hitRow(0) || r.hitRow(1) || r.hitRow(2+len(r.rows)) {
		t.Fatal("rows above or below the list are not hits")
	}
	r.Click(nil, 2+4) // row index 4 ("3")
	if r.sel != 4 {
		t.Fatalf("click selected %d, want 4", r.sel)
	}
	r.Click(nil, 2+3) // the "B" header: ignored
	if r.sel != 4 {
		t.Fatalf("clicking a header must not move the selection, got %d", r.sel)
	}
	if a := r.anchorRow(); a != 4-r.top+r.listTop {
		t.Fatalf("anchorRow %d", a)
	}
	r.sel = 2
	r.Scroll(1)
	if r.sel != 4 {
		t.Fatalf("wheel step skips headers: sel=%d", r.sel)
	}
}
