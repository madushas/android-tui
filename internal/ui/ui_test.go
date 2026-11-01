package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

func TestBoxWidths(t *testing.T) {
	for _, w := range []int{10, 30, 80} {
		b := box(sAccB.Render("Title"), []string{"short", sOK.Render("styled text that is much longer than the box can hold at small widths")}, w, 6, true, "1 of 2")
		if len(b) != 6 {
			t.Fatalf("height %d", len(b))
		}
		for i, l := range b {
			if sw(l) != w {
				t.Fatalf("w=%d line %d has width %d: %q", w, i, sw(l), l)
			}
		}
	}
}

func TestOverlayKeepsWidth(t *testing.T) {
	bg := []string{sOK.Render("0123456789abcdef"), "0123456789abcdef", "short"}
	out := overlay(bg, []string{"XX", "YY"}, 4, 0)
	if sw(out[0]) != 16 || sw(out[1]) != 16 {
		t.Fatalf("widths %d %d", sw(out[0]), sw(out[1]))
	}
	if cutLeft("héllo wörld", 6) != "wörld" {
		t.Fatalf("cutLeft: %q", cutLeft("héllo wörld", 6))
	}
}

func TestGraph(t *testing.T) {
	g := graph([]float64{0, 50, 100}, 5, 2, 100)
	if len(g) != 2 || len([]rune(g[0])) != 5 || []rune(g[0])[4] != '█' {
		t.Fatalf("graph: %q", g)
	}
}

func TestFieldsN(t *testing.T) {
	f := fieldsN("global key some value here", 3)
	if len(f) != 3 || f[2] != "some value here" {
		t.Fatalf("%q", f)
	}
}

// Every non-online state must say what is wrong and what to do about it.
func TestOfflineHelp(t *testing.T) {
	cases := []struct {
		t    target
		want []string
	}{
		{target{}, []string{"No target selected", "QR"}},
		{target{conn: &core.Conn{Serial: "X1", Model: "Pixel", State: "unauthorized"}}, []string{"unauthorized", "Allow USB debugging"}},
		{target{conn: &core.Conn{Serial: "X1", Model: "Pixel", State: "offline"}}, []string{"offline", "kill-server"}},
		{target{conn: &core.Conn{Serial: "192.0.2.5:5555", State: "saved"}}, []string{"saved connection", "Enter"}},
		{target{emu: &core.Emu{Name: "Pixel_9", State: "stopped"}}, []string{"Pixel_9 is stopped", "cold boot"}},
		{target{emu: &core.Emu{Name: "Pixel_9", State: "booting"}}, []string{"booting", "finishes starting"}},
	}
	if txt := strings.Join(offlineHelp(target{}, false), "\n"); !strings.Contains(txt, "adb was not found") || !strings.Contains(txt, "brew install android-platform-tools") {
		t.Errorf("missing adb hint:\n%s", txt)
	}
	for _, c := range cases {
		txt := strings.Join(offlineHelp(c.t, true), "\n")
		for _, w := range c.want {
			if !strings.Contains(txt, w) {
				t.Errorf("%v: missing %q in:\n%s", c.t.state(), w, txt)
			}
		}
	}
}

func TestThemes(t *testing.T) {
	defer ApplyTheme("default")
	for _, name := range []string{"default", "contrast", "mono", "bogus"} {
		ApplyTheme(name)
		if sty("okb").Render("x") == "" || sty("dim").Render("y") == "" {
			t.Fatalf("%s: style renders empty", name)
		}
	}
	t.Setenv("NO_COLOR", "1")
	t.Setenv("ANDROID_TUI_THEME", "")
	if themeFromEnv("contrast") != "mono" {
		t.Fatal("NO_COLOR should select mono")
	}
	t.Setenv("ANDROID_TUI_THEME", "contrast")
	if themeFromEnv("default") != "contrast" {
		t.Fatal("ANDROID_TUI_THEME must win")
	}
}

func TestTypedConfirm(t *testing.T) {
	ran := false
	c := &typedConfirmModal{msg: "Wipe?", word: "wipe", yes: func(m *Model) tea.Cmd { ran = true; return nil }}
	key := func(s string) tea.KeyMsg {
		switch s {
		case "enter":
			return tea.KeyMsg{Type: tea.KeyEnter}
		case "backspace":
			return tea.KeyMsg{Type: tea.KeyBackspace}
		case "esc":
			return tea.KeyMsg{Type: tea.KeyEsc}
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	for _, s := range []string{"w", "i"} {
		c.update(nil, key(s))
	}
	if closed, _ := c.update(nil, key("enter")); closed {
		t.Fatal("Enter must not confirm before the word is typed")
	}
	c.update(nil, key("backspace"))
	for _, s := range []string{"i", "p", "e"} { // "wipe" with an extra char typed then fixed
		c.update(nil, key(s))
	}
	c.update(nil, key("backspace"))
	if closed, _ := c.update(nil, key("enter")); closed {
		t.Fatal("'wip' is not the word")
	}
	c.update(nil, key("e"))
	closed, then := c.update(nil, key("enter"))
	if !closed || then == nil {
		t.Fatal("typing the word then Enter must confirm")
	}
	then(nil)
	if !ran {
		t.Fatal("confirm callback not run")
	}
	d := &typedConfirmModal{word: "wipe", yes: func(m *Model) tea.Cmd { return nil }}
	if closed, then := d.update(nil, key("esc")); !closed || then != nil {
		t.Fatal("Esc must cancel without running")
	}
	if got := strings.Join(c.view(80, 24), "\n"); !strings.Contains(got, "WIPE") {
		t.Fatalf("view should name the word: %s", got)
	}
}

// Narrow terminals show one column at a time; wide ones show both.
func TestNarrowLayout(t *testing.T) {
	m := New()
	m.w, m.h = 80, 30
	m.focus = fEmu
	if L := m.layout(); L["main"].w != 0 || L["emu"].w != 80 {
		t.Fatalf("narrow + list focus: want lists only, got %+v", L)
	}
	m.focus = fMain
	if L := m.layout(); L["main"].w != 80 || L["emu"].w != 0 {
		t.Fatalf("narrow + main focus: want main only, got %+v", L)
	}
	m.w = 140
	if L := m.layout(); L["main"].w == 0 || L["emu"].w == 0 {
		t.Fatalf("wide: want both columns, got %+v", L)
	}
}

// Pickers must confirm on Enter. (A mis-grouped boolean here once made Enter a silent no-op in most pickers.)
func TestChooseModalEnter(t *testing.T) {
	enter, down := tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyDown}
	picked := -1
	c := &chooseModal{items: []item{{text: "alpha"}, {text: "beta"}}, flt: newEdit(""), marks: map[int]bool{},
		pick: func(m *Model, i int) tea.Cmd { picked = i; return nil }}
	c.update(nil, down)
	closed, then := c.update(nil, enter)
	if !closed || then == nil {
		t.Fatal("Enter on a non-empty single-select list must choose")
	}
	then(nil)
	if picked != 1 {
		t.Fatalf("picked %d, want 1", picked)
	}

	empty := &chooseModal{flt: newEdit(""), marks: map[int]bool{}, pick: func(*Model, int) tea.Cmd { t.Fatal("picked from an empty list"); return nil }}
	if closed, _ := empty.update(nil, enter); closed {
		t.Fatal("Enter on an empty list must not close it")
	}

	multi := &chooseModal{multi: true, allowEmpty: true, flt: newEdit(""), marks: map[int]bool{}, picks: func(*Model, []int) tea.Cmd { return nil }}
	if closed, then := multi.update(nil, enter); !closed || then == nil {
		t.Fatal("multi-select with allowEmpty must accept an empty selection")
	}
}
